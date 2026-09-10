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
	if !strings.Contains(got, "gpt-5") {
		t.Errorf("the line should name the model, got %q", got)
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
