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
			Message:   stripLogTimestamp(line),
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

// stripLogTimestamp removes the Go standard log timestamp prefix (e.g. "2026/04/28 09:55:43 ")
// from a log line so the UI doesn't display the timestamp twice. The captured
// Timestamp field is already in RFC3339Nano and will be shown in the user's
// local timezone by the frontend.
func stripLogTimestamp(line string) string {
	// Go's log package emits timestamps in the format "2006/01/02 15:04:05 "
	// (with a trailing space). If the line starts with that pattern, strip it.
	if len(line) >= 20 &&
		line[4] == '/' && line[7] == '/' && line[10] == ' ' &&
		line[13] == ':' && line[16] == ':' && line[19] == ' ' {
		return line[20:]
	}
	return line
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

// Clear resets the ring buffer, returning the number of entries that were cleared.
func (rb *ringBuffer) Clear() int {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	n := rb.count
	rb.head = 0
	rb.count = 0
	return n
}

// RegisterAppLogs registers the app logs endpoint on the given router.
func (h *Handler) RegisterAppLogs(r chi.Router) {
	r.Get("/logs/app", h.GetAppLogs)
	r.Delete("/logs/app", h.ClearAppLogs)
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

// ClearAppLogs clears the application log ring buffer and returns the count
// of entries that were removed.
func (h *Handler) ClearAppLogs(w http.ResponseWriter, r *http.Request) {
	if appLogBuffer == nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]int{"deleted": 0})
		return
	}
	n := appLogBuffer.Clear()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]int{"deleted": n})
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
