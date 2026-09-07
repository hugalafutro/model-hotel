package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// logEntrySelectColumns is the shared 38-column request_logs projection plus the
// FROM/JOIN/WHERE 1=1 tail. The cursor list prefixes it with "SELECT "; the
// offset list (ListLogs) prefixes it with the windowed total count. Its column
// order matches logEntryScanDests exactly.
const logEntrySelectColumns = `rl.id, COALESCE(rl.provider_id::text, ''),
            CASE
                WHEN rl.provider_id IS NULL THEN ''
                WHEN p.name IS NOT NULL THEN p.name
                ELSE 'Deleted'
            END,
            rl.model_id,
            COALESCE(rl.request_hash, ''), COALESCE(rl.status_code, 0),
            COALESCE(rl.latency_ms, 0), COALESCE(rl.duration_ms, 0),
            COALESCE(rl.ttft_ms, 0), COALESCE(rl.proxy_overhead_ms, 0),
            COALESCE(rl.parse_ms, 0), COALESCE(rl.failover_lookup_ms, 0), COALESCE(rl.model_lookup_ms, 0), COALESCE(rl.provider_lookup_ms, 0), COALESCE(rl.key_decrypt_ms, 0),
            COALESCE(rl.dial_ms, 0), COALESCE(rl.settings_read_ms, 0),
            rl.cache_hits,
            COALESCE(rl.tokens_per_second, 0),
            COALESCE(rl.tokens_prompt, 0), COALESCE(rl.tokens_completion, 0),
            COALESCE(rl.tokens_completion_reasoning, 0),
            COALESCE(rl.tokens_prompt_cache_hit, 0), COALESCE(rl.tokens_prompt_cache_miss, 0),
            COALESCE(rl.streaming, false), COALESCE(rl.virtual_key_name, ''), COALESCE(rl.virtual_key_id::text, ''),
             CASE
                WHEN rl.virtual_key_id IS NULL OR rl.virtual_key_id::text = '' THEN false
                WHEN vk.id IS NULL THEN true
                ELSE false
            END AS virtual_key_deleted,
            COALESCE(rl.error_message, ''), COALESCE(rl.failover_attempt, 0), COALESCE(rl.state, 'completed'), rl.created_at,
            COALESCE(rl.response_header_ms, 0),
            COALESCE(rl.resolved_model_id, ''),
            COALESCE(rl.endpoint_type, 'chat'),
            COALESCE(rl.error_kind, ''),
            COALESCE(rl.client_ip, ''),
            rl.attempts
        FROM request_logs rl LEFT JOIN providers p ON rl.provider_id = p.id
        LEFT JOIN virtual_keys vk ON rl.virtual_key_id = vk.id
        WHERE 1=1
    `

// buildLogListQuery assembles the cursor data query: the column projection, the
// shared filters, the keyset predicate (when a cursor is present), and the ORDER
// BY + LIMIT. It fetches limit+1 to detect has_more, with the sort inverted for
// backward pagination so LIMIT picks from the correct end.
func buildLogListQuery(p logListParams) (string, []any) {
	query := "SELECT " + logEntrySelectColumns

	args := []any{}
	argIndex := 1
	query, args, argIndex = appendLogFilters(query, args, argIndex, p.logFilters)
	if p.cursorStr != "" {
		query, args, argIndex = appendKeysetPredicate(query, args, argIndex, p.cursor, p.direction, p.sortDir)
	}

	fetchSortDir := p.sortDir
	if p.direction == "before" {
		if fetchSortDir == "desc" {
			fetchSortDir = "asc"
		} else {
			fetchSortDir = "desc"
		}
	}
	query += " ORDER BY rl.created_at " + fetchSortDir + ", rl.id " + fetchSortDir
	query += " LIMIT $" + strconv.Itoa(argIndex)
	args = append(args, p.limit+1)
	return query, args
}

// countLogs returns the total row count for the same filters as the data query.
// Best-effort: returns 0 on error (the cursor response is still useful without
// an accurate total).
func (h *Handler) countLogs(ctx context.Context, p logListParams) int {
	query := "SELECT COUNT(*) FROM request_logs rl WHERE 1=1"
	args := []any{}
	query, args, _ = appendLogFilters(query, args, 1, p.logFilters)
	var total int
	_ = h.dbPool.Pool().QueryRow(ctx, query, args...).Scan(&total)
	return total
}

// paginateCursor applies has_after/has_before detection from the fetched-one-extra
// signal and cursor presence, trims to limit, and reverses the slice for backward
// pagination, which fetched in inverted sort order. It is the keyset-pagination
// tail shared by the request-log, app-log, and model cursor endpoints
// (T = LogEntry | AppLogEntry | ModelResponse).
func paginateCursor[T any](entries []T, direction string, limit int, hasCursor bool) ([]T, bool, bool) {
	var hasAfter, hasBefore bool
	switch direction {
	case "after":
		// Fetching older entries (scroll down or initial load).
		if len(entries) > limit {
			hasAfter = true
			entries = entries[:limit]
		}
		// An initial request (no cursor) is at the newest, so nothing is before it.
		// A cursor request assumes newer entries exist until proven otherwise; a
		// fetchBefore returning 0 corrects that client-side.
		if hasCursor {
			hasBefore = true
		}
	case "before":
		// Fetching newer entries (scroll up).
		if len(entries) > limit {
			hasBefore = true
			entries = entries[:limit]
		}
		// Items exist after the cursor by definition.
		if hasCursor {
			hasAfter = true
		}
	}

	if direction == "before" {
		slices.Reverse(entries)
	}
	return entries, hasAfter, hasBefore
}

// logEntryScanDests returns the ordered Scan() targets for the shared 38-column
// request_logs projection (logEntrySelectColumns). The cursor list scans these
// directly; the offset list (ListLogs) prepends its windowed total count.
func logEntryScanDests(entry *LogEntry) []any {
	return []any{
		&entry.ID, &entry.ProviderID, &entry.ProviderName, &entry.ModelID,
		&entry.RequestHash, &entry.StatusCode, &entry.LatencyMs, &entry.DurationMs,
		&entry.TTFTMs, &entry.ProxyOverheadMs,
		&entry.ParseMs, &entry.FailoverLookupMs, &entry.ModelLookupMs, &entry.ProviderLookupMs, &entry.KeyDecryptMs,
		&entry.DialMs, &entry.SettingsReadMs,
		&entry.CacheHits,
		&entry.TokensPerSecond,
		&entry.TokensPrompt, &entry.TokensCompletion, &entry.TokensCompletionReasoning,
		&entry.TokensPromptCacheHit, &entry.TokensPromptCacheMiss,
		&entry.Streaming,
		&entry.VirtualKeyName, &entry.VirtualKeyID, &entry.VirtualKeyDeleted,
		&entry.ErrorMessage,
		&entry.FailoverAttempt, &entry.State, &entry.CreatedAt,
		&entry.ResponseHeaderMs,
		&entry.ResolvedModelID,
		&entry.EndpointType,
		&entry.ErrorKind,
		&entry.ClientIP,
		&entry.Attempts,
	}
}

// scanLogEntry scans one request_logs row (the 38-column projection shared by
// ListLogsCursor and ListLogs) into a LogEntry.
func scanLogEntry(rows pgx.CollectableRow) (LogEntry, error) {
	var entry LogEntry
	err := rows.Scan(logEntryScanDests(&entry)...)
	return entry, err
}

// appendLogFilters appends the shared modelID/providerID/statusCode/from/to WHERE
// fragments, returning the extended query, args, and next placeholder index. It
// is the single source of truth for both the data and count queries in
// ListLogsCursor, so a negative status_code is ignored uniformly.
func appendLogFilters(query string, args []any, argIndex int, f logFilters) (string, []any, int) {
	// Owner scope first: mandatory row-level security for non-admins, an optional
	// dashboard filter for admins. The two branches cover the two disjoint row
	// shapes. A KEYED row resolves through the key's CURRENT owner, so reassigning
	// a key moves its whole history with it. A KEYLESS row (dashboard chat/arena,
	// which have no key to join through) carries the owner stamped at request time
	// in request_logs.owner_user_id, written only for that shape. Rows with NULL on
	// both sides stay admin-only.
	if f.ownerUserID != "" {
		ph := strconv.Itoa(argIndex)
		query += " AND (rl.virtual_key_id IN (SELECT vko.id FROM virtual_keys vko WHERE vko.owner_user_id = $" + ph + ")" +
			" OR (rl.virtual_key_id IS NULL AND rl.owner_user_id = $" + ph + "))"
		args = append(args, f.ownerUserID)
		argIndex++
	}
	if f.modelID != "" {
		query += " AND rl.model_id ILIKE $" + strconv.Itoa(argIndex)
		args = append(args, "%"+f.modelID+"%")
		argIndex++
	}
	if isValidEndpointType(f.endpointType) {
		query += " AND COALESCE(rl.endpoint_type, 'chat') = $" + strconv.Itoa(argIndex)
		args = append(args, f.endpointType)
		argIndex++
	}
	if f.providerID != "" {
		providerUUID, err := uuid.Parse(f.providerID)
		if err == nil {
			query += " AND rl.provider_id = $" + strconv.Itoa(argIndex)
			args = append(args, providerUUID)
			argIndex++
		}
	}
	if f.virtualKeyID != "" {
		vkUUID, err := uuid.Parse(f.virtualKeyID)
		if err == nil {
			query += " AND rl.virtual_key_id = $" + strconv.Itoa(argIndex)
			args = append(args, vkUUID)
			argIndex++
		}
	}
	if f.clientIP != "" {
		query += " AND rl.client_ip = $" + strconv.Itoa(argIndex)
		args = append(args, f.clientIP)
		argIndex++
	}
	if f.statusCode != "" {
		if f.statusCode == "4xx" {
			query += " AND rl.status_code >= 400 AND rl.status_code < 500"
		} else if f.statusCode == "5xx" {
			query += " AND rl.status_code >= 500"
		} else if statusCode, err := strconv.Atoi(f.statusCode); err == nil && statusCode >= 0 {
			if statusCode == 0 {
				query += " AND (rl.status_code = 0 OR rl.status_code IS NULL)"
			} else {
				query += " AND rl.status_code = $" + strconv.Itoa(argIndex)
				args = append(args, statusCode)
				argIndex++
			}
		}
	}
	if f.fromDate != "" {
		if parsedFrom, err := time.Parse(time.RFC3339, f.fromDate); err == nil {
			query += " AND rl.created_at >= $" + strconv.Itoa(argIndex)
			args = append(args, parsedFrom)
			argIndex++
		}
	}
	if f.toDate != "" {
		if parsedTo, err := time.Parse(time.RFC3339, f.toDate); err == nil {
			query += " AND rl.created_at <= $" + strconv.Itoa(argIndex)
			args = append(args, parsedTo)
			argIndex++
		}
	}
	return appendAttemptFilter(query, args, argIndex, f.attemptProviderID, f.attemptStatus)
}

// appendAttemptFilter adds the per-attempt trail filter: "every request in which
// THIS provider answered THIS status, whoever served it in the end". One
// containment predicate carries both keys, so the two must hold on the same
// element rather than on two different attempts; jsonb_path_ops on the attempts
// index serves it. Either key alone is allowed. An unparseable provider id or
// status is ignored, matching the other lenient filters.
func appendAttemptFilter(query string, args []any, argIndex int, attemptProviderID, attemptStatus string) (string, []any, int) {
	element := map[string]any{}
	if attemptProviderID != "" {
		if id, err := uuid.Parse(attemptProviderID); err == nil {
			element["provider_id"] = id.String()
		}
	}
	if attemptStatus != "" {
		if status, err := strconv.Atoi(attemptStatus); err == nil && status > 0 {
			element["status"] = status
		}
	}
	if len(element) == 0 {
		return query, args, argIndex
	}
	needle, err := json.Marshal([]map[string]any{element})
	if err != nil {
		return query, args, argIndex
	}
	query += " AND rl.attempts @> $" + strconv.Itoa(argIndex) + "::jsonb"
	args = append(args, string(needle))
	argIndex++
	return query, args, argIndex
}

// isValidEndpointType reports whether s is a known endpoint family for the
// endpoint_type log filter. Unknown values are ignored (no filter applied) rather
// than rejected, matching the other filters.
func isValidEndpointType(s string) bool {
	switch s {
	case "chat", "messages", "embeddings", "rerank", "image", "tts", "stt":
		return true
	default:
		return false
	}
}

// appendKeysetPredicate appends the (created_at, id) keyset comparison relative
// to the cursor. The comparison operator is "<" when scrolling toward older rows
// ((after, desc) or (before, asc)) and ">" otherwise, collapsing the four
// direction/sort branches into one template.
func appendKeysetPredicate(query string, args []any, argIndex int, cursor logCursor, direction, sortDir string) (string, []any, int) {
	op := ">"
	if (direction == "after") == (sortDir == "desc") {
		op = "<"
	}
	query += " AND (rl.created_at " + op + " $" + strconv.Itoa(argIndex) +
		" OR (rl.created_at = $" + strconv.Itoa(argIndex+1) +
		" AND rl.id " + op + " $" + strconv.Itoa(argIndex+2) + "))"
	args = append(args, cursor.CreatedAt, cursor.CreatedAt, cursor.ID)
	argIndex += 3
	return query, args, argIndex
}

// logListParams holds the parsed, validated query inputs for the cursor log
// endpoint: limit clamped to [1,200], direction/sortDir defaulted, filters, and
// the decoded cursor.
type logListParams struct {
	limit     int
	cursorStr string
	cursor    logCursor
	direction string
	sortDir   string
	logFilters
}

// logFilters is the set of request_logs WHERE fragments both log endpoints
// accept. It travels as one value so the offset list, the cursor list and the
// count query cannot drift apart on which filters they honour.
type logFilters struct {
	ownerUserID  string
	modelID      string
	providerID   string
	virtualKeyID string
	clientIP     string
	statusCode   string
	fromDate     string
	toDate       string
	endpointType string
	// attemptProviderID / attemptStatus filter on the per-attempt trail
	// rather than the terminal columns (see appendAttemptFilter).
	attemptProviderID string
	attemptStatus     string
}

// parseLogFilters reads every request_logs filter the log endpoints accept,
// including the mandatory owner scope for non-admins.
func parseLogFilters(r *http.Request) logFilters {
	q := r.URL.Query()
	return logFilters{
		ownerUserID:       logOwnerScope(r),
		modelID:           q.Get("model_id"),
		providerID:        q.Get("provider_id"),
		virtualKeyID:      q.Get("virtual_key_id"),
		clientIP:          q.Get("client_ip"),
		statusCode:        q.Get("status_code"),
		fromDate:          q.Get("from"),
		toDate:            q.Get("to"),
		endpointType:      q.Get("endpoint_type"),
		attemptProviderID: q.Get("attempt_provider_id"),
		attemptStatus:     q.Get("attempt_status"),
	}
}

// cursorPageParams reads the pagination inputs every keyset endpoint shares:
// limit clamped to [1, maxLimit] (an absent or unparseable value takes
// defaultLimit), direction defaulted to "after", and the decoded cursor. An
// undecodable cursor writes a 400 and returns ok=false.
func cursorPageParams(w http.ResponseWriter, q url.Values, defaultLimit, maxLimit int) (limit int, cursorStr, direction string, cursor logCursor, ok bool) {
	limit = defaultLimit
	if n, err := strconv.Atoi(q.Get("limit")); err == nil {
		limit = n
	}
	limit = min(max(limit, 1), maxLimit)

	direction = q.Get("direction")
	if direction != "before" {
		direction = "after"
	}

	cursorStr = q.Get("cursor")
	if cursorStr != "" {
		if err := cursor.decode(cursorStr); err != nil {
			respondBadRequest(w, "invalid cursor", err)
			return limit, cursorStr, direction, cursor, false
		}
	}
	return limit, cursorStr, direction, cursor, true
}

// parseLogListParams reads and validates the pagination/filter query params. On
// an undecodable cursor it writes a 400 response and returns ok=false.
func parseLogListParams(w http.ResponseWriter, r *http.Request) (logListParams, bool) {
	limit, cursorStr, direction, cursor, ok := cursorPageParams(w, r.URL.Query(), 20, 200)
	p := logListParams{
		limit:      limit,
		cursorStr:  cursorStr,
		cursor:     cursor,
		direction:  direction,
		sortDir:    r.URL.Query().Get("sort_dir"),
		logFilters: parseLogFilters(r),
	}
	if p.sortDir != "asc" && p.sortDir != "desc" {
		p.sortDir = "desc"
	}
	return p, ok
}

// logCursor is the keyset cursor for cursor-based log pagination. It encodes the
// created_at and id of a boundary row so the next page can be fetched relative
// to it.
type logCursor struct {
	CreatedAt time.Time `json:"created_at"`
	ID        string    `json:"id"`
}

func (c *logCursor) encode() string {
	b, _ := json.Marshal(c)
	return base64.StdEncoding.EncodeToString(b)
}

func (c *logCursor) decode(s string) error {
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return fmt.Errorf("invalid base64: %w", err)
	}
	return json.Unmarshal(b, c)
}

// logsSortDef resolves a user-supplied sort_by value to its ORDER BY
// expressions, normalizing anything outside the whitelist to "time". Every
// expression is a fixed compile-time constant; user input only selects a map
// key and never reaches the SQL.
func logsSortDef(sortBy string) (string, logSortDef) {
	if _, ok := logSortColumns[sortBy]; !ok {
		sortBy = "time"
	}
	return sortBy, logSortColumns[sortBy]
}

// logSortColumns is the sort_by whitelist: a fixed table of compile-time
// constant ORDER BY expressions, so user input only ever selects a key.
var logSortColumns = map[string]logSortDef{
	"time":               {"", "rl.created_at"},
	"model":              {"", "rl.model_id"},
	"provider":           {"CASE WHEN rl.provider_id IS NULL THEN 2 WHEN p.name IS NULL THEN 1 ELSE 0 END", "CASE WHEN rl.provider_id IS NULL THEN '' WHEN p.name IS NOT NULL THEN p.name ELSE 'Deleted' END"},
	"status":             {"", "rl.status_code"},
	"tokens":             {"CASE WHEN rl.tokens_prompt + rl.tokens_completion + COALESCE(rl.tokens_completion_reasoning, 0) = 0 THEN CASE WHEN COALESCE(rl.error_message, '') ILIKE '%cancel%' OR COALESCE(rl.error_message, '') ILIKE '%disconnect%' OR COALESCE(rl.error_message, '') ILIKE '%context canceled%' THEN 1 ELSE 2 END ELSE 0 END", "rl.tokens_prompt + rl.tokens_completion + COALESCE(rl.tokens_completion_reasoning, 0)"},
	"tps":                {"CASE WHEN rl.tokens_per_second = 0 THEN 1 ELSE 0 END", "rl.tokens_per_second"},
	"ttft":               {"CASE WHEN rl.ttft_ms = 0 THEN 1 ELSE 0 END", "rl.ttft_ms"},
	"response_header_ms": {"CASE WHEN rl.response_header_ms = 0 THEN 1 ELSE 0 END", "rl.response_header_ms"},
	"duration":           {"CASE WHEN rl.duration_ms = 0 THEN 1 ELSE 0 END", "rl.duration_ms"},
	"overhead":           {"CASE WHEN rl.proxy_overhead_ms = 0 THEN 1 ELSE 0 END", "rl.proxy_overhead_ms"},
	"key":                {"", "CASE WHEN rl.virtual_key_id IS NOT NULL AND rl.virtual_key_id::text != '' AND vk.id IS NULL THEN 'zzzzzzzz' ELSE COALESCE(rl.virtual_key_name, '') END"},
	// client_ip is TEXT, so this orders lexicographically (10.* before 9.*),
	// which is enough for grouping same-address rows. Rows without an address
	// sort last.
	"ip": {"CASE WHEN COALESCE(rl.client_ip, '') = '' THEN 1 ELSE 0 END", "COALESCE(rl.client_ip, '')"},
}

// logSortDef holds the tier and value ORDER BY expressions for one sort key.
type logSortDef struct {
	tierExpr  string
	valueExpr string
}
