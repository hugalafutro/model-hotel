package api

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
)

// AppLogEntry represents a single captured application log line.
type AppLogEntry struct {
	Timestamp string `json:"timestamp"` // RFC3339Nano
	Level     string `json:"level"`     // "info", "warning", "error"
	Message   string `json:"message"`
}

const appLogBufferSize = 500

// appLogBuffer is the global ring buffer that captures log output.
var appLogBuffer *ringBuffer

// ringBuffer is a fixed-size circular buffer of AppLogEntry values.
type ringBuffer struct {
	mu      sync.RWMutex
	entries []AppLogEntry
	head    int // next write position
	count   int // number of entries written (up to capacity)
}

// InitAppLogBuffer creates the global log buffer and redirects log output
// so that all standard library log calls are captured.  Call once at startup.
func InitAppLogBuffer() {
	appLogBuffer = &ringBuffer{
		entries: make([]AppLogEntry, appLogBufferSize),
	}
	log.SetOutput(io.MultiWriter(os.Stderr, appLogBuffer))
}

// Write implements io.Writer so ringBuffer can be used with log.SetOutput.
// It splits multi-line output into individual entries.
func (rb *ringBuffer) Write(p []byte) (n int, err error) {
	text := string(p)
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	now := time.Now().UTC()
	for _, line := range lines {
		if line == "" {
			continue
		}
		entry := AppLogEntry{
			Timestamp: now.Format(time.RFC3339Nano),
			Level:     detectLevel(line),
			Message:   line,
		}
		rb.mu.Lock()
		rb.entries[rb.head] = entry
		rb.head = (rb.head + 1) % appLogBufferSize
		if rb.count < appLogBufferSize {
			rb.count++
		}
		rb.mu.Unlock()
	}
	return len(p), nil
}

// detectLevel attempts to infer a log level from the content of the line.
// Go's standard log package does not emit levels, so we use heuristics.
func detectLevel(line string) string {
	lower := strings.ToLower(line)
	if strings.Contains(lower, "error") || strings.Contains(lower, "fatal") || strings.Contains(lower, "panic") {
		return "error"
	}
	if strings.Contains(lower, "warn") {
		return "warning"
	}
	return "info"
}

// GetEntries returns all buffered entries in chronological order (oldest first).
func (rb *ringBuffer) GetEntries() []AppLogEntry {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	if rb.count == 0 {
		return nil
	}

	result := make([]AppLogEntry, rb.count)
	start := 0
	if rb.count == appLogBufferSize {
		start = rb.head // oldest entry is at head when buffer is full
	}
	for i := 0; i < rb.count; i++ {
		result[i] = rb.entries[(start+i)%appLogBufferSize]
	}
	return result
}

// RegisterAppLogs registers the app logs endpoint on the given router.
func (h *Handler) RegisterAppLogs(r chi.Router) {
	r.Get("/logs/app", h.GetAppLogs)
}

// GetAppLogs returns recent application log entries as a JSON array.
// Supports query parameters:
//   - ?limit=N  — return at most N entries (default 500, max 1000)
//   - ?after=<RFC3339 timestamp> — only return entries after the given time
func (h *Handler) GetAppLogs(w http.ResponseWriter, r *http.Request) {
	if appLogBuffer == nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]AppLogEntry{})
		return
	}

	limit := 500
	if l := r.URL.Query().Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 && v <= 1000 {
			limit = v
		}
	}

	after := r.URL.Query().Get("after")

	entries := appLogBuffer.GetEntries()
	if after != "" {
		entries = filterEntriesAfter(entries, after)
	}
	if len(entries) > limit {
		entries = entries[len(entries)-limit:]
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(entries)
}

// filterEntriesAfter returns only entries whose timestamp is strictly after
// the provided RFC3339Nano timestamp.  On parse failure the original slice
// is returned unchanged.
func filterEntriesAfter(entries []AppLogEntry, after string) []AppLogEntry {
	t, err := time.Parse(time.RFC3339Nano, after)
	if err != nil {
		// Try the more common RFC3339 layout as a fallback.
		t, err = time.Parse(time.RFC3339, after)
		if err != nil {
			return entries
		}
	}
	for i, e := range entries {
		et, err := time.Parse(time.RFC3339Nano, e.Timestamp)
		if err == nil && et.After(t) {
			return entries[i:]
		}
	}
	return nil
}
