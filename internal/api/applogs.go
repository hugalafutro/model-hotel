package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// AppLogEntry represents a single captured application log line.
type AppLogEntry struct {
	Timestamp string `json:"timestamp"` // RFC3339Nano
	Level     string `json:"level"`     // "info", "warning", "error"
	Source    string `json:"source"`    // "proxy", "auth", "discovery", etc. (without brackets)
	Message   string `json:"message"`
}

const appLogBufferSize = 500

// appLogBuffer is the global ring buffer that captures log output.
var appLogBuffer *ringBuffer

// dbWriter is the asynchronous database log writer (nil if no pool).
var dbWriter *dbLogWriter

// ringBuffer is a fixed-size circular buffer of AppLogEntry values.
type ringBuffer struct {
	mu      sync.RWMutex
	entries []AppLogEntry
	head    int // next write position
	count   int // number of entries written (up to capacity)
}

const dbLogChannelSize = 1000

type dbLogWriter struct {
	pool *pgxpool.Pool
	ch   chan AppLogEntry
	done chan struct{}
}

func newDBLogWriter(pool *pgxpool.Pool) *dbLogWriter {
	w := &dbLogWriter{
		pool: pool,
		ch:   make(chan AppLogEntry, dbLogChannelSize),
		done: make(chan struct{}),
	}
	go w.run()
	return w
}

func (w *dbLogWriter) run() {
	batch := make([]AppLogEntry, 0, 50)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case entry, ok := <-w.ch:
			if !ok {
				// Channel closed — flush remaining
				if len(batch) > 0 {
					w.flush(batch)
				}
				close(w.done)
				return
			}
			batch = append(batch, entry)
			if len(batch) >= 50 {
				w.flush(batch)
				batch = batch[:0]
			}
		case <-ticker.C:
			if len(batch) > 0 {
				w.flush(batch)
				batch = batch[:0]
			}
		}
	}
}

func (w *dbLogWriter) flush(entries []AppLogEntry) {
	if w.pool == nil || len(entries) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Build batch INSERT
	builder := strings.Builder{}
	builder.WriteString("INSERT INTO app_logs (timestamp, level, source, message) VALUES ")
	args := make([]interface{}, 0, len(entries)*4)
	for i, e := range entries {
		if i > 0 {
			builder.WriteString(", ")
		}
		offset := i * 4
		builder.WriteString(fmt.Sprintf("($%d, $%d, $%d, $%d)", offset+1, offset+2, offset+3, offset+4))
		args = append(args, e.Timestamp, e.Level, e.Source, e.Message)
	}
	_, err := w.pool.Exec(ctx, builder.String(), args...)
	if err != nil {
		// Don't log with log.Printf here — that would cause infinite recursion!
		// Just silently drop — the ring buffer still has the data for live view.
		_ = err
	}
}

func (w *dbLogWriter) write(entry AppLogEntry) {
	select {
	case w.ch <- entry:
	default:
		// Channel full — drop the entry (ring buffer still has it)
	}
}

func (w *dbLogWriter) stop() {
	close(w.ch)
	<-w.done
}

// InitAppLogBuffer creates the global log buffer and redirects log output
// so that all standard library log calls are captured.  Call once at startup.
func InitAppLogBuffer(pool *pgxpool.Pool) {
	appLogBuffer = &ringBuffer{
		entries: make([]AppLogEntry, appLogBufferSize),
	}
	if pool != nil {
		dbWriter = newDBLogWriter(pool)
	}
	log.SetOutput(io.MultiWriter(os.Stderr, appLogBuffer))
}

// StopAppLogWriter flushes pending DB log writes and stops the background
// writer goroutine. Call before closing the database pool on shutdown.
func StopAppLogWriter() {
	if dbWriter != nil {
		dbWriter.stop()
	}
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
		stripped := stripLogTimestamp(line)
		source, msg := extractSource(stripped)
		entry := AppLogEntry{
			Timestamp: now.Format(time.RFC3339Nano),
			Level:     detectLevel(msg),
			Source:    source,
			Message:   msg,
		}
		rb.mu.Lock()
		rb.entries[rb.head] = entry
		rb.head = (rb.head + 1) % appLogBufferSize
		if rb.count < appLogBufferSize {
			rb.count++
		}
		rb.mu.Unlock()
		if dbWriter != nil {
			dbWriter.write(entry)
		}
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

// extractSource parses a "[prefix]" tag from the beginning of a log message.
// Returns the source (without brackets) and the remaining message.
// If no bracketed prefix is found, returns ("", line).
func extractSource(line string) (string, string) {
	if len(line) > 0 && line[0] == '[' {
		end := strings.Index(line, "]")
		if end > 0 && end < len(line)-1 && line[end+1] == ' ' {
			return line[1:end], line[end+2:]
		}
	}
	return "", line
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

// appLogsHistoryResponse is the JSON structure returned when history mode is active.
type appLogsHistoryResponse struct {
	Entries []AppLogEntry `json:"entries"`
	Total   int           `json:"total"`
	Page    int           `json:"page"`
	PerPage int           `json:"per_page"`
}

// GetAppLogs returns recent application log entries as a JSON array.
// Supports query parameters:
//   - ?history=true — query from DB with filtering/pagination (returns paginated response)
//   - ?limit=N  — return at most N entries from ring buffer (default 500, max 1000)
//   - ?after=<RFC3339 timestamp> — only return entries after the given time (ring buffer mode)
//
// When history=true, additional query parameters are supported:
//   - ?level=info|warning|error — filter by log level
//   - ?source=proxy|auth|... — filter by source
//   - ?search=text — text search in message (ILIKE)
//   - ?from=<RFC3339> — start timestamp
//   - ?to=<RFC3339> — end timestamp
//   - ?page=N — page number (default 1)
//   - ?per_page=N — page size (default 20, max 100)
func (h *Handler) GetAppLogs(w http.ResponseWriter, r *http.Request) {
	// History mode: query from DB with filtering/pagination
	if r.URL.Query().Get("history") == "true" {
		h.getAppLogsHistory(w, r)
		return
	}

	// Ring buffer mode (default, backward compatible)
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

// getAppLogsHistory queries app_logs from the database with filtering and pagination.
func (h *Handler) getAppLogsHistory(w http.ResponseWriter, r *http.Request) {
	if h.dbPool == nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(appLogsHistoryResponse{})
		return
	}

	q := r.URL.Query()

	// Pagination
	page := 1
	if v := q.Get("page"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 {
			page = n
		}
	}
	perPage := 20
	if v := q.Get("per_page"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 && n <= 100 {
			perPage = n
		}
	}

	// Build WHERE clause
	conditions := []string{}
	args := []interface{}{}
	argIdx := 1

	if level := q.Get("level"); level != "" {
		conditions = append(conditions, fmt.Sprintf("level = $%d", argIdx))
		args = append(args, level)
		argIdx++
	}
	if source := q.Get("source"); source != "" {
		conditions = append(conditions, fmt.Sprintf("source = $%d", argIdx))
		args = append(args, source)
		argIdx++
	}
	if search := q.Get("search"); search != "" {
		conditions = append(conditions, fmt.Sprintf("message ILIKE $%d", argIdx))
		args = append(args, "%"+search+"%")
		argIdx++
	}
	if from := q.Get("from"); from != "" {
		if t, err := time.Parse(time.RFC3339, from); err == nil {
			conditions = append(conditions, fmt.Sprintf("timestamp >= $%d", argIdx))
			args = append(args, t.UTC())
			argIdx++
		}
	}
	if to := q.Get("to"); to != "" {
		if t, err := time.Parse(time.RFC3339, to); err == nil {
			conditions = append(conditions, fmt.Sprintf("timestamp <= $%d", argIdx))
			args = append(args, t.UTC())
			argIdx++
		}
	}

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = " WHERE " + strings.Join(conditions, " AND ")
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	// Count total
	countSQL := "SELECT COUNT(*) FROM app_logs" + whereClause
	var total int
	err := h.dbPool.Pool().QueryRow(ctx, countSQL, args...).Scan(&total)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to count logs"})
		return
	}

	// Fetch page
	offset := (page - 1) * perPage
	dataSQL := fmt.Sprintf(
		"SELECT timestamp, level, source, message FROM app_logs%s ORDER BY created_at DESC LIMIT $%d OFFSET $%d",
		whereClause, argIdx, argIdx+1,
	)
	args = append(args, perPage, offset)

	rows, err := h.dbPool.Pool().Query(ctx, dataSQL, args...)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to query logs"})
		return
	}
	defer rows.Close()

	entries := make([]AppLogEntry, 0, perPage)
	for rows.Next() {
		var e AppLogEntry
		var ts time.Time
		if err := rows.Scan(&ts, &e.Level, &e.Source, &e.Message); err != nil {
			continue
		}
		e.Timestamp = ts.UTC().Format(time.RFC3339Nano)
		entries = append(entries, e)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(appLogsHistoryResponse{
		Entries: entries,
		Total:   total,
		Page:    page,
		PerPage: perPage,
	})
}

// ClearAppLogs clears the application log ring buffer and DB, returning the count
// of entries that were removed.
func (h *Handler) ClearAppLogs(w http.ResponseWriter, r *http.Request) {
	var deleted int
	if appLogBuffer != nil {
		deleted = appLogBuffer.Clear()
	}
	// Also delete from DB
	if h.dbPool != nil {
		tag, err := h.dbPool.Pool().Exec(r.Context(), `DELETE FROM app_logs`)
		if err == nil {
			deleted += int(tag.RowsAffected())
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]int{"deleted": deleted})
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
