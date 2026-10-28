// Package logtest captures what the process-wide logger emits while a test
// runs, so a test can assert on a log line's level and attributes. It is
// imported only by _test.go files, never by production code.
package logtest

import (
	"context"
	"log/slog"
	"sync"
	"testing"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

// Record is one captured log line with its attributes flattened by key.
type Record struct {
	Level slog.Level
	Msg   string
	Attrs map[string]any
}

// Handler is a slog.Handler that keeps every record it is handed.
type Handler struct {
	mu      sync.Mutex
	records []Record
}

// Capture routes debuglog output into a new Handler for the length of the test
// and restores the previous handler afterwards. The logger is process-global,
// so a test that calls this must not call t.Parallel(): a second one running
// alongside it would capture the first's records, or restore the original
// handler out from under it.
func Capture(t testing.TB) *Handler {
	t.Helper()
	prev := slog.Default().Handler()
	t.Cleanup(func() { debuglog.SetHandler(prev) })
	h := &Handler{}
	debuglog.SetHandler(h)
	return h
}

// Enabled implements slog.Handler: every level is kept.
func (h *Handler) Enabled(context.Context, slog.Level) bool { return true }

// Handle implements slog.Handler.
func (h *Handler) Handle(_ context.Context, r slog.Record) error {
	rec := Record{Level: r.Level, Msg: r.Message, Attrs: make(map[string]any, r.NumAttrs())}
	r.Attrs(func(a slog.Attr) bool {
		rec.Attrs[a.Key] = a.Value.Any()
		return true
	})
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, rec)
	return nil
}

// WithAttrs implements slog.Handler; pre-set attributes are not recorded.
func (h *Handler) WithAttrs([]slog.Attr) slog.Handler { return h }

// WithGroup implements slog.Handler; groups are flattened.
func (h *Handler) WithGroup(string) slog.Handler { return h }

// Records returns a copy of everything captured so far, oldest first.
func (h *Handler) Records() []Record {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]Record(nil), h.records...)
}

// Latest returns the most recent record of any message.
func (h *Handler) Latest() (Record, bool) {
	recs := h.Records()
	if len(recs) == 0 {
		return Record{}, false
	}
	return recs[len(recs)-1], true
}

// Find returns the first record carrying msg.
func (h *Handler) Find(msg string) (Record, bool) {
	for _, r := range h.Records() {
		if r.Msg == msg {
			return r, true
		}
	}
	return Record{}, false
}

// Last returns the most recent record carrying msg.
func (h *Handler) Last(msg string) (Record, bool) {
	recs := h.Records()
	for i := len(recs) - 1; i >= 0; i-- {
		if recs[i].Msg == msg {
			return recs[i], true
		}
	}
	return Record{}, false
}

// Has reports whether any record carries msg.
func (h *Handler) Has(msg string) bool {
	_, ok := h.Find(msg)
	return ok
}

// Count reports how many records carry msg.
func (h *Handler) Count(msg string) int {
	n := 0
	for _, r := range h.Records() {
		if r.Msg == msg {
			n++
		}
	}
	return n
}

// Attr returns attribute key of the first record carrying msg, or nil when
// there is no such record or key.
func (h *Handler) Attr(msg, key string) any {
	r, ok := h.Find(msg)
	if !ok {
		return nil
	}
	return r.Attrs[key]
}
