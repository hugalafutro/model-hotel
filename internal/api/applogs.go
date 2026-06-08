package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/go-chi/chi/v5"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

// AppLogEntry represents a single captured application log line.
type AppLogEntry struct {
	ID        string `json:"id,omitempty"`
	CreatedAt string `json:"created_at,omitempty"` // RFC3339Nano, DB insertion time (keyset sort key)
	Timestamp string `json:"timestamp"`            // RFC3339Nano, event time
	Level     string `json:"level"`                // "info", "warning", "error"
	Source    string `json:"source"`               // "proxy", "auth", "discovery", etc. (without brackets)
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
	if line != "" && line[0] == '[' {
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
	if wordMatch(lower, "debug") {
		return "debug"
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
	for _, prefix := range []string{"level=DEBUG ", "level=INFO ", "level=WARN ", "level=ERROR ", "DEBUG  ", "INFO  ", "WARN  ", "ERROR "} {
		if after, ok := strings.CutPrefix(msg, prefix); ok {
			return after
		}
	}
	return msg
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

// RegisterAppLogs registers the app logs endpoint on the given router.
func (h *Handler) RegisterAppLogs(r chi.Router) {
	r.Get("/logs/app", h.GetAppLogs)
	r.Get("/logs/app/cursor", h.GetAppLogsCursor)
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
//
//nolint:revive // result names not needed for internal API types
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

// appendAppLogFilters appends the shared level/source/search/from/to WHERE
// conditions for app_logs (no table alias — app_logs is queried without joins).
// It is the single source of truth for getAppLogsHistory and both
// GetAppLogsCursor queries (data + total count).
//
// Date-range filters use created_at (DB insertion time), not timestamp (event
// time), for consistency with the cursor endpoint's keyset pagination; app logs
// are ingested in real-time so the two columns fall within the same filter window.
func appendAppLogFilters(conditions []string, args []any, argIdx int, level, source, search, from, to string) ([]string, []any, int) {
	if level != "" {
		conditions = append(conditions, fmt.Sprintf("level = $%d", argIdx))
		args = append(args, level)
		argIdx++
	}
	if source != "" {
		conditions = append(conditions, fmt.Sprintf("source = $%d", argIdx))
		args = append(args, source)
		argIdx++
	}
	if search != "" {
		conditions = append(conditions, fmt.Sprintf("message ILIKE $%d", argIdx))
		args = append(args, "%"+search+"%")
		argIdx++
	}
	if from != "" {
		if t, err := time.Parse(time.RFC3339, from); err == nil {
			conditions = append(conditions, fmt.Sprintf("created_at >= $%d", argIdx))
			args = append(args, t.UTC())
			argIdx++
		}
	}
	if to != "" {
		if t, err := time.Parse(time.RFC3339, to); err == nil {
			conditions = append(conditions, fmt.Sprintf("created_at <= $%d", argIdx))
			args = append(args, t.UTC())
			argIdx++
		}
	}
	return conditions, args, argIdx
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
	conditions, args, argIdx := appendAppLogFilters(nil, nil, 1,
		q.Get("level"), q.Get("source"), q.Get("search"), q.Get("from"), q.Get("to"))

	// Sort
	// "time" maps to created_at for consistency with the cursor endpoint.
	// The timestamp column (event time) is still returned for display.
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

// appLogCursor is the keyset cursor for cursor-based app log pagination.
type appLogCursor struct {
	CreatedAt time.Time `json:"created_at"`
	ID        string    `json:"id"`
}

func (c *appLogCursor) encode() string {
	b, _ := json.Marshal(c)
	return base64.StdEncoding.EncodeToString(b)
}

func (c *appLogCursor) decode(s string) error {
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return fmt.Errorf("invalid base64: %w", err)
	}
	return json.Unmarshal(b, c)
}

// AppLogsCursorResponse is the cursor-based paginated response for app logs.
type AppLogsCursorResponse struct {
	Entries      []AppLogEntry  `json:"entries"`
	Total        int            `json:"total"`
	HasBefore    bool           `json:"has_before"`
	HasAfter     bool           `json:"has_after"`
	LevelCounts  map[string]int `json:"level_counts"`
	SourceCounts map[string]int `json:"source_counts"`
}

// GetAppLogsCursor returns app logs using keyset (cursor) pagination.
//
// Query parameters:
//   - cursor: encoded cursor from a previous response
//   - direction: "after" (default) or "before"
//   - limit: page size (default 20, max 200)
//   - level, source, search, from, to: same filters as getAppLogsHistory
//   - sort_dir: "desc" (default) or "asc"
func (h *Handler) GetAppLogsCursor(w http.ResponseWriter, r *http.Request) {
	if h.dbPool == nil {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(AppLogsCursorResponse{})
		return
	}

	q := r.URL.Query()
	limit := 20
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 && n <= 200 {
			limit = n
		}
	}
	cursorStr := q.Get("cursor")
	direction := q.Get("direction")
	if direction != "before" && direction != "after" {
		direction = "after"
	}
	sortDir := "DESC"
	if q.Get("sort_dir") == "asc" {
		sortDir = "ASC"
	}

	// Parse cursor
	var cursor appLogCursor
	if cursorStr != "" {
		if err := cursor.decode(cursorStr); err != nil {
			respondBadRequest(w, "invalid cursor", err)
			return
		}
	}

	// Build WHERE clause (same filters as getAppLogsHistory)
	conditions, args, argIdx := appendAppLogFilters(nil, nil, 1,
		q.Get("level"), q.Get("source"), q.Get("search"), q.Get("from"), q.Get("to"))

	// Apply cursor keyset predicate
	if cursorStr != "" {
		if direction == "after" {
			if sortDir == "DESC" {
				// Scrolling older: (created_at, id) < cursor
				conditions = append(conditions, fmt.Sprintf(
					"(created_at < $%d OR (created_at = $%d AND id < $%d))",
					argIdx, argIdx+1, argIdx+2,
				))
				args = append(args, cursor.CreatedAt, cursor.CreatedAt, cursor.ID)
				argIdx += 3
			} else {
				// Asc mode, after = newer
				conditions = append(conditions, fmt.Sprintf(
					"(created_at > $%d OR (created_at = $%d AND id > $%d))",
					argIdx, argIdx+1, argIdx+2,
				))
				args = append(args, cursor.CreatedAt, cursor.CreatedAt, cursor.ID)
				argIdx += 3
			}
		} else { // before
			if sortDir == "DESC" {
				// Scrolling newer: (created_at, id) > cursor
				conditions = append(conditions, fmt.Sprintf(
					"(created_at > $%d OR (created_at = $%d AND id > $%d))",
					argIdx, argIdx+1, argIdx+2,
				))
				args = append(args, cursor.CreatedAt, cursor.CreatedAt, cursor.ID)
				argIdx += 3
			} else {
				// Asc mode, before = older
				conditions = append(conditions, fmt.Sprintf(
					"(created_at < $%d OR (created_at = $%d AND id < $%d))",
					argIdx, argIdx+1, argIdx+2,
				))
				args = append(args, cursor.CreatedAt, cursor.CreatedAt, cursor.ID)
				argIdx += 3
			}
		}
	}

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = " WHERE " + strings.Join(conditions, " AND ")
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	// Fetch entries (limit+1 to detect has_more)
	// When paginating backward, invert the sort direction so LIMIT picks from
	// the correct end of the result set, then reverse the slice before returning.
	fetchLimit := limit + 1
	fetchSortDir := sortDir
	if direction == "before" {
		if fetchSortDir == "DESC" {
			fetchSortDir = "ASC"
		} else {
			fetchSortDir = "DESC"
		}
	}
	dataSQL := fmt.Sprintf(
		"SELECT id, created_at, timestamp, level, source, message FROM app_logs%s ORDER BY created_at %s, id %s LIMIT $%d",
		whereClause, fetchSortDir, fetchSortDir, argIdx,
	)
	args = append(args, fetchLimit)

	rows, err := h.dbPool.Pool().Query(ctx, dataSQL, args...)
	if err != nil {
		respondError(w, "failed to query app logs", err, http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	entries := make([]AppLogEntry, 0, limit)
	for rows.Next() {
		var id string
		var e AppLogEntry
		var ts time.Time
		var cat time.Time
		if err := rows.Scan(&id, &cat, &ts, &e.Level, &e.Source, &e.Message); err != nil {
			continue
		}
		e.ID = id
		e.CreatedAt = cat.UTC().Format(time.RFC3339Nano)
		e.Timestamp = ts.UTC().Format(time.RFC3339Nano)
		entries = append(entries, e)
	}

	// Determine has_after / has_before based on direction and fetched rows
	var hasAfter, hasBefore bool
	switch direction {
	case "after":
		// Fetching older entries (scroll down or initial load)
		if len(entries) > limit {
			hasAfter = true
			entries = entries[:limit]
		}
		// For initial request (no cursor), we're at the newest — nothing before
		// For cursor requests, assume newer entries exist until fetchBefore proves otherwise
		if cursorStr != "" {
			hasBefore = true
		}
	case "before":
		// Fetching newer entries (scroll up)
		if len(entries) > limit {
			hasBefore = true
			entries = entries[:limit]
		}
		// Items exist after the cursor by definition
		if cursorStr != "" {
			hasAfter = true
		}
	}

	// Reverse entries for backward pagination: we fetched in inverted sort order
	// to get the correct window, but must return in the user's requested sort order.
	if direction == "before" {
		slices.Reverse(entries)
	}

	// Get cached counts
	levelCounts, sourceCounts := h.getAppLogCounts(ctx)

	// Total count (with filters applied, but without cursor predicate).
	totalCountConditions, totalCountArgs, _ := appendAppLogFilters(nil, nil, 1,
		q.Get("level"), q.Get("source"), q.Get("search"), q.Get("from"), q.Get("to"))

	totalWhereClause := ""
	if len(totalCountConditions) > 0 {
		totalWhereClause = " WHERE " + strings.Join(totalCountConditions, " AND ")
	}
	var total int
	_ = h.dbPool.Pool().QueryRow(ctx, "SELECT COUNT(*) FROM app_logs"+totalWhereClause, totalCountArgs...).Scan(&total)

	response := AppLogsCursorResponse{
		Entries:      entries,
		Total:        total,
		HasBefore:    hasBefore,
		HasAfter:     hasAfter,
		LevelCounts:  levelCounts,
		SourceCounts: sourceCounts,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		debuglog.Error("applogs-cursor: failed to encode response", "error", err)
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
