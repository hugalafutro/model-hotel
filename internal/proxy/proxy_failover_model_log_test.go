package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/provider"
)

// logRecorder keeps every record at or above its level, message and attributes
// together. The attributes are the point: the leak these tests guard against
// travels in an attribute value, so a recorder that kept only the message would
// assert nothing. Enabled answers the level question the same way a production
// handler configured at that level does, which is what the debug gate reads.
type logRecorder struct {
	level slog.Level
	mu    *sync.Mutex
	lines *[]string
	attrs []slog.Attr
}

func (h *logRecorder) Enabled(_ context.Context, l slog.Level) bool { return l >= h.level }

func (h *logRecorder) Handle(_ context.Context, r slog.Record) error {
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

func (h *logRecorder) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &logRecorder{level: h.level, mu: h.mu, lines: h.lines, attrs: append(append([]slog.Attr{}, h.attrs...), attrs...)}
}

func (h *logRecorder) WithGroup(string) slog.Handler { return h }

// captureLogsAt installs a recording handler at the given level through
// debuglog, the way the binaries install theirs, and returns a lookup for the
// captured lines whose message starts with the given prefix. The process-wide
// default logger is restored when the test ends, so a test using this must not
// run in parallel.
func captureLogsAt(t *testing.T, level slog.Level) func(prefix string) []string {
	t.Helper()
	var lines []string
	var mu sync.Mutex
	original := slog.Default()
	t.Cleanup(func() { slog.SetDefault(original) })
	debuglog.SetHandler(&logRecorder{level: level, mu: &mu, lines: &lines})
	return func(prefix string) []string {
		mu.Lock()
		defer mu.Unlock()
		var got []string
		for _, l := range lines {
			if strings.HasPrefix(l, prefix) {
				got = append(got, l)
			}
		}
		return got
	}
}

// TestUpstreamModelAttr_CarriesTheModelNotTheBody pins the no-content-logging
// invariant on the one debug line that reads the upstream body. The value used
// to be the body sliced up to its first comma, logged whenever that slice held
// the token "model", and a caller controls their own field names: a nested
// object with a model key put the caller's text in the log verbatim.
func TestUpstreamModelAttr_CarriesTheModelNotTheBody(t *testing.T) {
	t.Parallel()
	// A nested model key before the first comma is the shape the old slice
	// logged, caller text and all. Marshal sorts the keys, so metadata leads.
	const secret = "caller text with no comma at all"
	body, err := json.Marshal(map[string]any{
		"metadata": map[string]any{"model": secret},
		"model":    "gpt-5",
	})
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}

	got, ok := upstreamModelAttr(body)
	if !ok {
		t.Fatal("a body naming a model should log one")
	}
	if got != "gpt-5" {
		t.Errorf("logged value = %q, want the model alone", got)
	}
	if strings.Contains(got, secret) {
		t.Errorf("request content reached the log: %q", got)
	}
}

// A body carrying no model, or one that does not decode at all, logs nothing
// rather than falling back to raw bytes. The second case is what keeps a
// multipart body out of the debug log.
func TestUpstreamModelAttr_SilentWithoutAModel(t *testing.T) {
	t.Parallel()
	for _, body := range []string{
		`{"messages":[{"role":"user","content":"secret"}]}`,
		`not json at all, secret`,
		"--boundary\r\nContent-Disposition: form-data; name=\"model\"\r\n\r\nsecret\r\n",
	} {
		if got, ok := upstreamModelAttr([]byte(body)); ok {
			t.Errorf("body %q logged %q, want nothing", body, got)
		}
	}
}

// A model value is caller text, so it is bounded rather than logged at whatever
// length the caller chose.
func TestUpstreamModelAttr_BoundsTheValue(t *testing.T) {
	t.Parallel()
	body, err := json.Marshal(map[string]any{"model": strings.Repeat("m", 4096)})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got, ok := upstreamModelAttr(body)
	if !ok {
		t.Fatal("a body naming a model should log one")
	}
	// The sanitizer marks a value it truncated, so the result is the cap plus
	// that marker rather than exactly the cap.
	if len(got) > shortLogValueCap+16 {
		t.Errorf("logged value is %d bytes, want the cap plus a truncation marker", len(got))
	}
	if !strings.HasPrefix(got, strings.Repeat("m", 64)) {
		t.Errorf("logged value = %q, want the head of the model kept", got)
	}
}

// With a handler that accepts Debug records the line is written, and it carries
// the model alone.
func TestLogUpstreamModel_WritesTheModelAtDebug(t *testing.T) {
	const secret = "caller text with no comma at all"
	body, err := json.Marshal(map[string]any{
		"metadata": map[string]any{"model": secret},
		"model":    "gpt-5",
	})
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	captured := captureLogsAt(t, slog.LevelDebug)

	logUpstreamModel(body)

	got := captured("proxy: upstream body model")
	if len(got) != 1 {
		t.Fatalf("expected one model line, got %v", got)
	}
	if !strings.Contains(got[0], "upstream_model=gpt-5") {
		t.Errorf("the line should carry the model under upstream_model, got %q", got[0])
	}
	if strings.Contains(got[0], secret) {
		t.Errorf("request content reached the log: %q", got[0])
	}
}

// With a handler that refuses Debug records nothing is written, and the body is
// never parsed: a body that is not JSON at all is handed over, which the decode
// would have to reach to reject. Bodies are tens of megabytes on the image
// endpoints, so the parse skipped here is what the gate exists for.
func TestLogUpstreamModel_SilentBelowDebug(t *testing.T) {
	captured := captureLogsAt(t, slog.LevelInfo)

	logUpstreamModel([]byte(`{"model":"gpt-5"}`))
	logUpstreamModel([]byte(`not json at all, secret`))

	if got := captured("proxy: upstream body model"); len(got) != 0 {
		t.Errorf("expected nothing logged below debug, got %v", got)
	}
}

// The image rewrite reports the "size" it dropped so the change is debuggable,
// and that value is whatever JSON the caller put under the key: any type, any
// length. It reaches the log bounded and sanitized, not verbatim.
func TestBuildCandidateRequest_BoundsTheDroppedImageSize(t *testing.T) {
	huge := strings.Repeat("z", 100<<10)
	body, err := json.Marshal(map[string]any{
		"model":  "grok-imagine",
		"prompt": "a cat",
		"size":   huge,
	})
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	h := &Handler{}
	st := &requestState{
		bodyBytes:        body,
		reqModel:         "grok-imagine",
		endpointPath:     "/images/generations",
		logData:          &requestLogData{endpointType: endpointTypeImage},
		makeUpstreamBody: func(string) ([]byte, string, error) { return body, "application/json", nil },
	}
	candidate := modelCandidate{
		model:    &model.Model{ModelID: "grok-imagine"},
		provider: &provider.Provider{Name: "xAI", BaseURL: "https://api.x.ai/v1", ProviderType: "xai"},
	}
	captured := captureLogsAt(t, slog.LevelDebug)

	if _, _, _, err := h.buildCandidateRequest(context.Background(), st, candidate); err != nil {
		t.Fatalf("buildCandidateRequest: %v", err)
	}

	got := captured("proxy: image size rewritten for the provider")
	if len(got) != 1 {
		t.Fatalf("expected one rewrite line, got %v lines", len(got))
	}
	if strings.Contains(got[0], huge) {
		t.Error("the caller's size reached the log verbatim")
	}
	if len(got[0]) > 1024 {
		t.Errorf("rewrite line is %d bytes, want the dropped size bounded", len(got[0]))
	}
}
