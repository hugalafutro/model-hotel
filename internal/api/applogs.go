package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

// AppLogEntry represents a single captured application log line.
type AppLogEntry struct {
	Timestamp string `json:"timestamp"` // RFC3339Nano
	Level     string `json:"level"`     // "info", "warning", "error"
	Source    string `json:"source"`    // "proxy", "auth", "discovery", etc. (without brackets)
	Message   string `json:"message"`
}

// parseLogLine extracts the structured fields from a raw log line.
// It strips the Go log timestamp, extracts the [source] tag, detects the
// level, and strips any level prefix from the message. This is the shared
// parsing logic used by both the ring buffer (UI) and stderr filter.
func parseLogLine(line string) (source, level, msg string) {
	stripped := stripLogTimestamp(line)
	source, msg = extractSource(stripped)
	level = detectLevel(msg)
	msg = stripLevelPrefix(msg)
	return source, level, msg
}

// stderrLogFilter is an io.Writer that forwards log lines to an underlying
// writer (os.Stderr) only when they look like errors — this keeps docker logs
// clean while the ring buffer still captures everything for the DB.
//
// Sources listed in stderrSuppressSources are completely suppressed from
// docker logs (all levels). This is useful for noisy sources whose errors
// are not operationally useful in docker logs but still need to be captured
// in the database for full visibility in the app UI. Add sources here when
// you decide certain errors should not clutter docker logs.
type stderrLogFilter struct {
	dst io.Writer
}

// stderrSuppressSources is the set of log sources that should be completely
// suppressed from docker logs (stderr), regardless of level. Entries from
// these sources still flow to the ring buffer and database for full visibility
// in the app UI. Start empty — add sources when you decide their errors
// should not appear in docker logs.
var stderrSuppressSources = map[string]bool{}

func (f *stderrLogFilter) Write(p []byte) (n int, err error) {
	text := string(p)
	for _, line := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		if line == "" {
			continue
		}
		src, lvl, _ := parseLogLine(line)
		if stderrSuppressSources[src] {
			continue
		}
		if lvl == "error" || lvl == "warning" {
			if _, err := f.dst.Write([]byte(line + "\n")); err != nil {
				return 0, err
			}
		}
	}
	return len(p), nil
}

const appLogBufferSize = 500

// appLogCountCache caches unfiltered level/source counts with a short TTL.
// Pill badge counts don't need to be real-time — a few seconds of staleness is fine.
var (
	appLogCountCache struct {
		sync.RWMutex
		levelCounts  map[string]int
		sourceCounts map[string]int
		fetchedAt    time.Time
	}
	appLogCountCacheTTL = 5 * time.Second
)

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

// dbLogChannelSize is the buffered channel capacity for the async DB log
// writer. At ~200 log lines/sec throughput with 50-row batches flushed every
// 500ms, a buffer of 5000 can absorb ~25 seconds of DB unavailability before
// backpressure is applied to the caller.
const dbLogChannelSize = 5000

// dbLogSendTimeout is how long the DB log writer will block trying to enqueue
// an entry before giving up. This prevents a slow or unreachable database
// from stalling the hot path (log.Printf) indefinitely.
const dbLogSendTimeout = 5 * time.Second

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
		fmt.Fprintf(&builder, "($%d, $%d, $%d, $%d)", offset+1, offset+2, offset+3, offset+4)
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
	defer func() { recover() }()
	timer := time.NewTimer(dbLogSendTimeout)
	defer timer.Stop()
	select {
	case w.ch <- entry:
		return
	case <-timer.C:
		// DB writer is backed up — drop the entry rather than blocking the
		// caller. The ring buffer still has it for live UI, and this only
		// happens if the DB is unreachable for >25 seconds.
	}
}

func (w *dbLogWriter) stop() {
	close(w.ch)
	<-w.done
}

func InitAppLogBuffer(pool *pgxpool.Pool) {
	appLogBuffer = &ringBuffer{
		entries: make([]AppLogEntry, appLogBufferSize),
	}
	if pool != nil {
		dbWriter = newDBLogWriter(pool)
	}
	log.SetOutput(io.MultiWriter(&stderrLogFilter{dst: os.Stderr}, appLogBuffer))
}

// NewAppSlogHandler returns a slog.Handler that writes structured log entries
// through the app log pipeline (ring buffer + DB writer + filtered stderr).
// Call after InitAppLogBuffer and pass to debuglog.SetHandler to route all
// slog output through the app logging system.
func NewAppSlogHandler(level slog.Level) slog.Handler {
	return &appSlogHandler{
		level:  level,
		stderr: &stderrLogFilter{dst: os.Stderr},
	}
}

// appSlogHandler implements slog.Handler by creating AppLogEntry values
// directly from slog records, routing them through the ring buffer and DB
// writer, and conditionally forwarding to stderr for docker logs.
type appSlogHandler struct {
	level  slog.Level
	stderr *stderrLogFilter
	group  string
	attrs  []slog.Attr
}

func (h *appSlogHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level
}

func (h *appSlogHandler) Handle(_ context.Context, r slog.Record) error {
	// Build message: prepend group prefix if set, then append any attrs.
	var msg strings.Builder
	if h.group != "" {
		fmt.Fprintf(&msg, "[%s] ", h.group)
	}
	msg.WriteString(r.Message)

	// Append any handler-level attrs.
	for _, a := range h.attrs {
		fmt.Fprintf(&msg, " %s=%v", a.Key, a.Value)
	}
	// Append per-record attrs.
	r.Attrs(func(a slog.Attr) bool {
		fmt.Fprintf(&msg, " %s=%v", a.Key, a.Value)
		return true
	})

	// Map slog level to app level.
	appLevel := "info"
	switch {
	case r.Level >= slog.LevelError:
		appLevel = "error"
	case r.Level >= slog.LevelWarn:
		appLevel = "warning"
	}

	// Extract source from "[source]" prefix in message, same as parseLogLine.
	source, msgStr := extractSource(msg.String())
	// For slog entries, the level is authoritative — do not let the text
	// heuristic (detectLevel) override it.  Field values like "error_chunks=0"
	// or "has_error=false" would falsely trigger detectLevel's "error" match.
	// The heuristic remains useful for legacy log.Printf lines (Write path).
	msgStr = stripLevelPrefix(msgStr)

	entry := AppLogEntry{
		Timestamp: r.Time.UTC().Format(time.RFC3339Nano),
		Level:     appLevel,
		Source:    source,
		Message:   msgStr,
	}

	// Write to ring buffer and DB.
	if appLogBuffer != nil {
		appLogBuffer.writeEntry(entry)
	}
	if w := dbWriter; w != nil {
		w.write(entry)
	}

	// Forward to stderr filter for docker logs (only errors/warnings).
	_, _ = fmt.Fprintf(h.stderr, "%s %s\n",
		r.Time.Format("2006/01/02 15:04:05"),
		msg.String())

	return nil
}

func (h *appSlogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &appSlogHandler{
		level:  h.level,
		stderr: h.stderr,
		group:  h.group,
		attrs:  append(slices.Clone(h.attrs), attrs...),
	}
}

func (h *appSlogHandler) WithGroup(name string) slog.Handler {
	if h.group != "" {
		name = h.group + "." + name
	}
	return &appSlogHandler{
		level:  h.level,
		stderr: h.stderr,
		group:  name,
		attrs:  slices.Clone(h.attrs),
	}
}

// levelSeverity maps app log levels to numeric severity for comparison.
var levelSeverity = map[string]int{"error": 3, "warning": 2, "info": 1}

// maxLevel returns the higher-severity of two app log levels.
func maxLevel(a, b string) string {
	if levelSeverity[a] > levelSeverity[b] {
		return a
	}
	return b
}

// writeEntry adds a pre-built AppLogEntry to the ring buffer (no text parsing).
func (rb *ringBuffer) writeEntry(entry AppLogEntry) {
	rb.mu.Lock()
	rb.entries[rb.head] = entry
	rb.head = (rb.head + 1) % appLogBufferSize
	if rb.count < appLogBufferSize {
		rb.count++
	}
	rb.mu.Unlock()
}

func StopAppLogWriter() {
	if dbWriter != nil {
		w := dbWriter
		dbWriter = nil
		w.stop()
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
		source, level, msg := parseLogLine(line)
		entry := AppLogEntry{
			Timestamp: now.Format(time.RFC3339Nano),
			Level:     level,
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
		if w := dbWriter; w != nil {
			w.write(entry)
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

// extractSource parses a source tag from the beginning of a log message.
// It supports two formats:
//   - Bracketed: "[proxy] message" → source="proxy", msg="message"
//   - Colon-separated: "proxy: message" → source="proxy", msg="message"
//
// If no source prefix is found, returns ("", line).
func extractSource(line string) (string, string) {
	// Bracketed format: [source] message
	if len(line) > 0 && line[0] == '[' {
		end := strings.Index(line, "]")
		if end > 0 && end < len(line)-1 && line[end+1] == ' ' {
			return line[1:end], line[end+2:]
		}
	}
	// Colon-separated format: source: message
	// Source must be at least 2 chars and match [a-zA-Z_][a-zA-Z0-9._-]*
	if colon := strings.Index(line, ": "); colon >= 2 {
		candidate := line[:colon]
		valid := true
		for i, ch := range candidate {
			if i == 0 {
				if !unicode.IsLetter(ch) && ch != '_' {
					valid = false
					break
				}
			} else {
				if !unicode.IsLetter(ch) && !unicode.IsDigit(ch) && ch != '_' && ch != '.' && ch != '-' {
					valid = false
					break
				}
			}
		}
		if valid {
			return candidate, line[colon+2:]
		}
	}
	return "", line
}

// detectLevel attempts to infer a log level from the content of the line.
// Go's standard log package does not emit levels, so we use heuristics.
// We use word-boundary matching to avoid false positives from field names
// like "error_chunks" or "has_error" which are structured key=value attrs,
// not actual error conditions.
func detectLevel(line string) string {
	lower := strings.ToLower(line)
	if wordMatch(lower, "error") || wordMatch(lower, "errors") || wordMatch(lower, "fatal") || wordMatch(lower, "panic") {
		return "error"
	}
	if wordMatch(lower, "warn") || wordMatch(lower, "warning") || wordMatch(lower, "warnings") {
		return "warning"
	}
	return "info"
}

// wordMatch reports whether word appears as a whole word in s (case-insensitive
// input expected). A "whole word" means the word is preceded and followed by a
// non-alphanumeric, non-underscore character (or string boundaries). This
// prevents "error" from matching inside "error_chunks" or "has_error".
func wordMatch(s, word string) bool {
	for {
		i := strings.Index(s, word)
		if i < 0 {
			return false
		}
		beforeOK := i == 0 || !isWordChar(s[i-1])
		after := i + len(word)
		afterOK := after >= len(s) || !isWordChar(s[after])
		if beforeOK && afterOK {
			return true
		}
		// Advance past this match and keep searching.
		s = s[after:]
	}
}

func isWordChar(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_'
}

// stripLevelPrefix removes a leading level indicator from a log message so the
// UI table doesn't show a redundant level string — the level is stored separately
// in the AppLogEntry.Level field. Handles both bare prefixes ("INFO ", "WARN ",
// "ERROR ") and key=value-style prefixes emitted by slog attrs ("level=INFO ",
// "level=WARN ", "level=ERROR ").
func stripLevelPrefix(msg string) string {
	for _, prefix := range []string{"level=INFO ", "level=WARN ", "level=ERROR ", "INFO  ", "WARN  ", "ERROR "} {
		if after, ok := strings.CutPrefix(msg, prefix); ok {
			return after
		}
	}
	return msg
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
	Entries      []AppLogEntry  `json:"entries"`
	Total        int            `json:"total"`
	Page         int            `json:"page"`
	PerPage      int            `json:"per_page"`
	LevelCounts  map[string]int `json:"level_counts"`
	SourceCounts map[string]int `json:"source_counts"`
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
//   - ?sort_by=time|level|source|message — sort column (default: time)
//   - ?sort_dir=asc|desc — sort direction (default: desc)
func (h *Handler) GetAppLogs(w http.ResponseWriter, r *http.Request) {
	// History mode: query from DB with filtering/pagination
	if r.URL.Query().Get("history") == "true" {
		h.getAppLogsHistory(w, r)
		return
	}

	// Ring buffer mode (default, backward compatible)
	if appLogBuffer == nil {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode([]AppLogEntry{}); err != nil {
			debuglog.Error("applogs: failed to encode empty response", "error", err)
		}
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
	if err := json.NewEncoder(w).Encode(entries); err != nil {
		debuglog.Error("applogs: failed to encode entries", "error", err)
	}
}

// getAppLogCounts returns cached unfiltered level and source counts.
// The cache refreshes every appLogCountCacheTTL to avoid running GROUP BY
// queries on every paginated history request (which polls every 2s in live mode).
func (h *Handler) getAppLogCounts(ctx context.Context) (map[string]int, map[string]int) {
	appLogCountCache.RLock()
	if time.Since(appLogCountCache.fetchedAt) < appLogCountCacheTTL && appLogCountCache.levelCounts != nil {
		lc := appLogCountCache.levelCounts
		sc := appLogCountCache.sourceCounts
		appLogCountCache.RUnlock()
		return lc, sc
	}
	appLogCountCache.RUnlock()

	if h.dbPool == nil {
		return map[string]int{"info": 0, "warning": 0, "error": 0}, map[string]int{}
	}

	levelCounts := map[string]int{"info": 0, "warning": 0, "error": 0}
	sourceCounts := map[string]int{}

	// Single query combining both aggregations via UNION ALL.
	const countsSQL = `
		SELECT 'level' AS kind, level AS key, COUNT(*) FROM app_logs GROUP BY level
		UNION ALL
		SELECT 'source' AS kind, source AS key, COUNT(*) FROM app_logs GROUP BY source
	`
	rows, err := h.dbPool.Pool().Query(ctx, countsSQL)
	if err == nil {
		for rows.Next() {
			var kind, key string
			var cnt int
			if rows.Scan(&kind, &key, &cnt) == nil {
				if kind == "level" {
					levelCounts[key] = cnt
				} else {
					sourceCounts[key] = cnt
				}
			}
		}
		rows.Close()
	}

	appLogCountCache.Lock()
	appLogCountCache.levelCounts = levelCounts
	appLogCountCache.sourceCounts = sourceCounts
	appLogCountCache.fetchedAt = time.Now()
	appLogCountCache.Unlock()

	return levelCounts, sourceCounts
}

// getAppLogsHistory queries app_logs from the database with filtering and pagination.
func (h *Handler) getAppLogsHistory(w http.ResponseWriter, r *http.Request) {
	if h.dbPool == nil {
		if err := json.NewEncoder(w).Encode(appLogsHistoryResponse{}); err != nil {
			debuglog.Error("applogs: failed to encode response", "error", err)
		}
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

	// Sort
	allowedSortCols := map[string]string{
		"time":    "created_at",
		"level":   "level",
		"source":  "source",
		"message": "message",
	}
	sortCol := "created_at"
	if v := q.Get("sort_by"); v != "" {
		if col, ok := allowedSortCols[v]; ok {
			sortCol = col
		}
	}
	sortDir := "DESC"
	if v := q.Get("sort_dir"); v == "asc" {
		sortDir = "ASC"
	}

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = " WHERE " + strings.Join(conditions, " AND ")
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	// Retrieve cached level/source counts (refreshed every appLogCountCacheTTL).
	levelCounts, sourceCounts := h.getAppLogCounts(ctx)

	// Count total (with filters applied)
	countSQL := "SELECT COUNT(*) FROM app_logs" + whereClause
	var total int
	if err := h.dbPool.Pool().QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
		if encErr := json.NewEncoder(w).Encode(map[string]string{"error": "failed to count logs"}); encErr != nil {
			debuglog.Error("applogs: failed to encode error response", "error", encErr)
		}
		return
	}

	// Fetch page
	offset := (page - 1) * perPage
	dataSQL := fmt.Sprintf(
		"SELECT timestamp, level, source, message FROM app_logs%s ORDER BY %s %s LIMIT $%d OFFSET $%d",
		whereClause, sortCol, sortDir, argIdx, argIdx+1,
	)
	args = append(args, perPage, offset)

	rows, err := h.dbPool.Pool().Query(ctx, dataSQL, args...)
	if err != nil {
		if err := json.NewEncoder(w).Encode(map[string]string{"error": "failed to query logs"}); err != nil {
			debuglog.Error("applogs: failed to encode error response", "error", err)
		}
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
	if err := json.NewEncoder(w).Encode(appLogsHistoryResponse{
		Entries:      entries,
		Total:        total,
		Page:         page,
		PerPage:      perPage,
		LevelCounts:  levelCounts,
		SourceCounts: sourceCounts,
	}); err != nil {
		debuglog.Error("applogs: failed to encode history response", "error", err)
	}
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
	if err := json.NewEncoder(w).Encode(map[string]int{"deleted": deleted}); err != nil {
		debuglog.Error("applogs: failed to encode delete response", "error", err)
	}
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
