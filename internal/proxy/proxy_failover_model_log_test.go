package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
)

// These tests replace the process-wide slog default, so they must not run in
// parallel with anything else in this package that logs.
//
// attrCaptureHandler records the message AND its attributes, which is the whole
// point here: the leak this guards against travels in an attribute value, so a
// handler that keeps only the message would assert nothing.
type attrCaptureHandler struct {
	mu    *sync.Mutex
	lines *[]string
	attrs []slog.Attr
}

func (h *attrCaptureHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *attrCaptureHandler) Handle(_ context.Context, r slog.Record) error {
	var b strings.Builder
	b.WriteString(r.Message)
	for _, a := range h.attrs {
		fmt.Fprintf(&b, " %s=%v", a.Key, a.Value)
	}
	r.Attrs(func(a slog.Attr) bool {
		fmt.Fprintf(&b, " %s=%v", a.Key, a.Value)
		return true
	})
	h.mu.Lock()
	defer h.mu.Unlock()
	*h.lines = append(*h.lines, b.String())
	return nil
}

func (h *attrCaptureHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &attrCaptureHandler{mu: h.mu, lines: h.lines, attrs: append(append([]slog.Attr{}, h.attrs...), attrs...)}
}

func (h *attrCaptureHandler) WithGroup(string) slog.Handler { return h }

// TestLogUpstreamModel_LogsTheModelNotTheBody pins the no-content-logging
// invariant on the one debug line that reads the upstream body. The value used
// to be the body sliced up to its first comma, logged whenever that slice held
// the token "model", and a caller controls their own field names: a nested
// object with a model key put the caller's text in the log verbatim, and a
// comma-free model value was logged whole.
func TestLogUpstreamModel_LogsTheModelNotTheBody(t *testing.T) {
	var logged []string
	var mu sync.Mutex
	original := slog.Default()
	t.Cleanup(func() { slog.SetDefault(original) })
	slog.SetDefault(slog.New(&attrCaptureHandler{mu: &mu, lines: &logged}))

	// A nested model key before the first comma: this is the shape the old slice
	// logged, caller text and all. Marshal sorts the keys, so metadata leads.
	const secret = "caller text with no comma at all"
	body, err := json.Marshal(map[string]any{
		"metadata": map[string]any{"model": secret},
		"model":    "gpt-5",
	})
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	logUpstreamModel(body)

	mu.Lock()
	got := strings.Join(logged, "\n")
	mu.Unlock()
	if !strings.Contains(got, "upstream body model") {
		t.Fatalf("expected the model line to be logged, got %q", got)
	}
	if !strings.Contains(got, "upstream_model=gpt-5") {
		t.Errorf("the line should carry the model under upstream_model, got %q", got)
	}
	if strings.Contains(got, secret) {
		t.Errorf("request content reached the log: %q", got)
	}
}

// A body carrying no model, or one that does not decode at all, logs nothing
// rather than falling back to raw bytes.
func TestLogUpstreamModel_SilentWithoutAModel(t *testing.T) {
	var logged []string
	var mu sync.Mutex
	original := slog.Default()
	t.Cleanup(func() { slog.SetDefault(original) })
	slog.SetDefault(slog.New(&attrCaptureHandler{mu: &mu, lines: &logged}))

	logUpstreamModel([]byte(`{"messages":[{"role":"user","content":"secret"}]}`))
	logUpstreamModel([]byte(`not json at all, secret`))

	mu.Lock()
	defer mu.Unlock()
	if len(logged) != 0 {
		t.Errorf("expected no log lines, got %v", logged)
	}
}

// The decode is the expensive half, and bodies reach tens of megabytes, so it
// must not run at all when no debug record would be emitted.
func TestLogUpstreamModel_SilentWhenDebugIsOff(t *testing.T) {
	var logged []string
	var mu sync.Mutex
	original := slog.Default()
	t.Cleanup(func() { slog.SetDefault(original) })
	slog.SetDefault(slog.New(&levelGate{
		inner: &attrCaptureHandler{mu: &mu, lines: &logged},
		level: slog.LevelInfo,
	}))

	logUpstreamModel([]byte(`{"model":"gpt-5"}`))

	mu.Lock()
	defer mu.Unlock()
	if len(logged) != 0 {
		t.Errorf("expected nothing logged with debug off, got %v", logged)
	}
}

// A model value is caller text, so it is bounded like any other upstream string
// rather than logged at whatever length the caller chose.
func TestLogUpstreamModel_BoundsTheValue(t *testing.T) {
	var logged []string
	var mu sync.Mutex
	original := slog.Default()
	t.Cleanup(func() { slog.SetDefault(original) })
	slog.SetDefault(slog.New(&attrCaptureHandler{mu: &mu, lines: &logged}))

	body, err := json.Marshal(map[string]any{"model": strings.Repeat("m", 4096)})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	logUpstreamModel(body)

	mu.Lock()
	defer mu.Unlock()
	if len(logged) != 1 {
		t.Fatalf("expected one line, got %v", len(logged))
	}
	if len(logged[0]) > 512 {
		t.Errorf("logged line is %d bytes, want the model bounded", len(logged[0]))
	}
}

// levelGate refuses records below its level, the way a production handler
// configured at Info does.
type levelGate struct {
	inner slog.Handler
	level slog.Level
}

func (h *levelGate) Enabled(_ context.Context, l slog.Level) bool { return l >= h.level }

func (h *levelGate) Handle(ctx context.Context, r slog.Record) error {
	return h.inner.Handle(ctx, r)
}

func (h *levelGate) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &levelGate{inner: h.inner.WithAttrs(attrs), level: h.level}
}

func (h *levelGate) WithGroup(name string) slog.Handler {
	return &levelGate{inner: h.inner.WithGroup(name), level: h.level}
}
