package debuglog

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

// recordingHandler keeps the rendered text of every record it receives, the
// attributes attached with WithAttrs included.
type recordingHandler struct {
	mu    *sync.Mutex
	lines *[]string
	attrs []slog.Attr
}

func newRecordingHandler() (*recordingHandler, func() []string) {
	var mu sync.Mutex
	var lines []string
	h := &recordingHandler{mu: &mu, lines: &lines}
	return h, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), lines...)
	}
}

func (h *recordingHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *recordingHandler) Handle(_ context.Context, r slog.Record) error {
	var b strings.Builder
	b.WriteString(r.Message)
	write := func(a slog.Attr) bool {
		b.WriteString(" " + a.Key + "=" + a.Value.String())
		return true
	}
	for _, a := range h.attrs {
		write(a)
	}
	r.Attrs(write)
	h.mu.Lock()
	defer h.mu.Unlock()
	*h.lines = append(*h.lines, b.String())
	return nil
}

func (h *recordingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &recordingHandler{mu: h.mu, lines: h.lines, attrs: append(append([]slog.Attr(nil), h.attrs...), attrs...)}
}

func (h *recordingHandler) WithGroup(string) slog.Handler { return h }

type secretStringer struct{ s string }

func (s secretStringer) String() string { return s.s }

// Every place a record carries text reaches the masker: the message, a string
// attribute, an error, a Stringer, a group member, and an attribute attached
// earlier with With.
func TestMaskingHandler_MasksEveryTextCarrier(t *testing.T) {
	const secret = "SECRETVALUE"
	SetMasker(func(s string) string { return strings.ReplaceAll(s, secret, "[redacted]") })
	t.Cleanup(func() { SetMasker(nil) })

	rec, lines := newRecordingHandler()
	logger := slog.New(maskingHandler{rec}).With("attached", "with "+secret)
	logger.Info("message "+secret,
		"str", "string "+secret,
		"err", errors.New("error "+secret),
		"stringer", secretStringer{"stringer " + secret},
		slog.Group("grp", "member", "group "+secret),
		"count", 7,
	)

	got := lines()
	if len(got) != 1 {
		t.Fatalf("records = %v, want one", got)
	}
	if strings.Contains(got[0], secret) {
		t.Fatalf("the secret survived the handler: %s", got[0])
	}
	for _, want := range []string{"message [redacted]", "attached=with [redacted]", "str=string [redacted]", "err=error [redacted]", "stringer=stringer [redacted]", "count=7"} {
		if !strings.Contains(got[0], want) {
			t.Errorf("record lacks %q: %s", want, got[0])
		}
	}
}

// With no masker installed a record passes through exactly as given, so a
// binary that never calls SetMasker behaves as it did before.
func TestMaskingHandler_PassesThroughWithoutAMasker(t *testing.T) {
	SetMasker(nil)
	rec, lines := newRecordingHandler()
	slog.New(maskingHandler{rec}).Info("message SECRETVALUE", "str", "SECRETVALUE")
	if got := lines(); len(got) != 1 || !strings.Contains(got[0], "message SECRETVALUE str=SECRETVALUE") {
		t.Fatalf("record changed without a masker: %v", got)
	}
}

// The hang this pins: test helpers save slog.Default().Handler() and restore
// it through SetHandler, and in a process that never configured logging that
// is slog's own boot handler, the bridge that writes through the log package.
// Wrapped, slog.SetDefault redirects log back into slog and every record
// re-enters log.Logger.output and blocks on its mutex. The failover suite hung
// for ten minutes on exactly this, and the scope filter wrapped it the same way
// whenever DEBUG_LOG_SCOPES was set.
//
// The invariant is asserted rather than the deadlock reproduced: a reproduced
// deadlock leaves log's mutex held, the cleanup blocks on it, and the whole
// package hangs to the binary timeout instead of failing here.
func TestSetHandler_InstallsTheBootHandlerUnwrapped(t *testing.T) {
	prev := slog.Default()
	prevScopes, prevGlobal := enabledScopes, globalDebug
	t.Cleanup(func() {
		slog.SetDefault(prev)
		enabledScopes, globalDebug = prevScopes, prevGlobal
	})
	SetMasker(func(s string) string { return s })
	t.Cleanup(func() { SetMasker(nil) })

	for _, tc := range []struct {
		name   string
		scopes map[string]bool
	}{
		{"no scopes", nil},
		{"scopes configured", map[string]bool{"proxy": true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			enabledScopes, globalDebug = tc.scopes, false
			SetHandler(bootHandler)
			if got := slog.Default().Handler(); got != bootHandler {
				t.Fatalf("the boot handler was installed wrapped (%T): logging through it deadlocks", got)
			}
		})
	}
}

// A handler that is already masked is handed straight back rather than wrapped
// again, which is what a caller restoring a saved default does.
func TestWithMasking_DoesNotWrapTwice(t *testing.T) {
	rec, _ := newRecordingHandler()
	once := withMasking(rec)
	if twice := withMasking(once); twice != once {
		t.Fatalf("an already-masked handler was wrapped again: %#v", twice)
	}
}

type nilPtrErr struct{ inner *struct{ msg string } }

// Error dereferences a nil field on a non-nil receiver: isTypedNil cannot see
// it, and only the recover in safeText keeps it from crashing the handler.
func (e *nilPtrErr) Error() string { return e.inner.msg }

type typedNilStringer struct{ s string }

func (t *typedNilStringer) String() string { return t.s }

// This handler sits in front of every record in the binary, so a value slog
// itself renders safely must not panic here: a typed-nil error or Stringer,
// and an error whose Error dereferences a nil field. Before the guard both
// panicked, and a (*T)(nil) error logged from a background goroutine would
// have taken the whole process down.
func TestMaskingHandler_SurvivesValuesThatPanicWhenRendered(t *testing.T) {
	SetMasker(func(s string) string { return strings.ReplaceAll(s, "SECRETVALUE", "[redacted]") })
	t.Cleanup(func() { SetMasker(nil) })

	var nilErr *nilPtrErr
	var nilStringer *typedNilStringer
	rec, lines := newRecordingHandler()
	logger := slog.New(maskingHandler{rec})

	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("the masking handler panicked: %v", r)
			}
		}()
		logger.Info("typed nils", "err", error(nilErr), "stringer", fmt.Stringer(nilStringer))
		logger.Info("nil field", "err", error(&nilPtrErr{}))
	}()
	if got := lines(); len(got) != 2 {
		t.Fatalf("records = %v, want both written", got)
	}
}

// The slices and maps a call site assembles (a []string of error texts, the
// Front Desk event metadata map) are walked, so a credential inside one is
// masked like a plain string attribute.
func TestMaskingHandler_MasksInsideSlicesAndMaps(t *testing.T) {
	const secret = "SECRETVALUE"
	SetMasker(func(s string) string { return strings.ReplaceAll(s, secret, "[redacted]") })
	t.Cleanup(func() { SetMasker(nil) })

	rec, lines := newRecordingHandler()
	slog.New(maskingHandler{rec}).Info("collections",
		"strs", []string{"a " + secret, "plain"},
		"anys", []any{"b " + secret, 42, errors.New("c " + secret)},
		"meta", map[string]any{"reason": "d " + secret, "nested": map[string]any{"e": "e " + secret}},
	)
	got := lines()
	if len(got) != 1 {
		t.Fatalf("records = %v, want one", got)
	}
	if strings.Contains(got[0], secret) {
		t.Fatalf("a secret inside a slice or map survived: %s", got[0])
	}
	if !strings.Contains(got[0], "plain") || !strings.Contains(got[0], "42") {
		t.Fatalf("values with no secret in them were altered: %s", got[0])
	}
}

// A logger derived with WithGroup stays behind the masker: if WithGroup
// returned the inner handler, every record logged through the grouped logger
// would skip the mask.
func TestMaskingHandler_WithGroupStaysMasked(t *testing.T) {
	const secret = "SECRETVALUE"
	SetMasker(func(s string) string { return strings.ReplaceAll(s, secret, "[redacted]") })
	t.Cleanup(func() { SetMasker(nil) })

	rec, lines := newRecordingHandler()
	grouped := slog.New(maskingHandler{rec}).WithGroup("req")
	if _, ok := grouped.Handler().(maskingHandler); !ok {
		t.Fatalf("WithGroup returned %T, not the masking wrapper", grouped.Handler())
	}
	grouped.Info("grouped "+secret, "field", "value "+secret)
	if got := lines(); len(got) != 1 || strings.Contains(got[0], secret) {
		t.Fatalf("a record through a grouped logger was not masked: %v", got)
	}
}

type namedStrings []string

type namedMap map[string]any

// Greptile's case: an exact-type match walked only []string, []any and
// map[string]any, so every other collection a call site might log carried a
// credential straight through. Each of these rendered the secret before.
func TestMaskingHandler_MasksEveryCollectionKind(t *testing.T) {
	const secret = "SECRETVALUE"
	SetMasker(func(s string) string { return strings.ReplaceAll(s, secret, "[redacted]") })
	t.Cleanup(func() { SetMasker(nil) })

	for name, value := range map[string]any{
		"map[string]string":     map[string]string{"k": "v " + secret},
		"[]error":               []error{errors.New("e " + secret)},
		"named []string":        namedStrings{"n " + secret},
		"named map[string]any":  namedMap{"k": "m " + secret},
		"[]byte":                []byte("b " + secret),
		"array of strings":      [2]string{"a " + secret, "plain"},
		"nested map in a slice": []map[string]string{{"k": "x " + secret}},
	} {
		t.Run(name, func(t *testing.T) {
			rec, lines := newRecordingHandler()
			slog.New(maskingHandler{rec}).Info("collection", "v", value)
			got := lines()
			if len(got) != 1 || strings.Contains(got[0], secret) {
				t.Fatalf("the secret survived a %s: %v", name, got)
			}
			// []byte renders as decimal bytes, so check its decoded form too.
			if b, ok := value.([]byte); ok && strings.Contains(string(b), secret) {
				masked, _ := maskAny(func(s string) string { return strings.ReplaceAll(s, secret, "[redacted]") }, b)
				if strings.Contains(string(masked.([]byte)), secret) {
					t.Fatalf("the []byte still decodes to the secret")
				}
			}
		})
	}
}

// A collection that contains itself must not send the walk round forever: the
// depth bound ends it.
func TestMaskAny_EndsOnASelfContainingSlice(t *testing.T) {
	self := make([]any, 1)
	self[0] = self
	done := make(chan struct{})
	go func() {
		maskAny(func(s string) string { return s }, self)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the walk did not end on a self-containing slice")
	}
}

type namedString string

type keyHolder struct{ Key string }

// Codex's cases: a struct field, a named string type and a map with non-string
// keys each carried the secret through, since none is a collection the walk
// knew or a string the switch matched.
func TestMaskingHandler_MasksStructsNamedStringsAndIntKeyedMaps(t *testing.T) {
	const secret = "SECRETVALUE"
	SetMasker(func(s string) string { return strings.ReplaceAll(s, secret, "[redacted]") })
	t.Cleanup(func() { SetMasker(nil) })

	for name, value := range map[string]any{
		"struct":            keyHolder{Key: "k " + secret},
		"pointer to struct": &keyHolder{Key: "p " + secret},
		"named string":      namedString("n " + secret),
		"map[int]string":    map[int]string{1: "i " + secret},
	} {
		t.Run(name, func(t *testing.T) {
			rec, lines := newRecordingHandler()
			slog.New(maskingHandler{rec}).Info("value", "v", value)
			if got := lines(); len(got) != 1 || strings.Contains(got[0], secret) {
				t.Fatalf("the secret survived a %s: %v", name, got)
			}
		})
	}
}

// A struct with no credential in it keeps its own value, so the JSON handler
// still renders it as an object rather than a string.
func TestMaskAny_LeavesACleanStructAlone(t *testing.T) {
	if _, changed := maskAny(func(s string) string { return strings.ReplaceAll(s, "SECRETVALUE", "x") }, keyHolder{Key: "clean"}); changed {
		t.Fatal("a struct with nothing to mask was replaced")
	}
}

// A collection nested past the bound is replaced by a marker, not forwarded
// untouched: a credential nine levels down must not reach the sink.
func TestMaskAny_ReplacesASubtreeNestedPastTheBound(t *testing.T) {
	const secret = "SECRETVALUE"
	var v any = []any{"deep " + secret}
	for range maskDepth {
		v = []any{v}
	}
	masked, changed := maskAny(func(s string) string { return strings.ReplaceAll(s, secret, "[redacted]") }, v)
	if !changed {
		t.Fatal("a nested collection was not walked")
	}
	if got := fmt.Sprint(masked); strings.Contains(got, secret) || !strings.Contains(got, depthMarker) {
		t.Fatalf("a secret past the depth bound survived, or no marker replaced it: %s", got)
	}
}
