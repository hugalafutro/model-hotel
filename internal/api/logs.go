package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// CacheHits is an alias for the shared CacheHits type defined in util.
// The API uses this alias for clarity in LogEntry — the underlying type
// is the same one the proxy produces.
type CacheHits = util.CacheHits

// LogEntry represents a single request log entry.
type LogEntry struct {
	ID                        string     `json:"id"`
	ProviderID                string     `json:"provider_id"`
	ProviderName              string     `json:"provider_name"`
	ModelID                   string     `json:"model_id"`
	RequestHash               string     `json:"request_hash"`
	StatusCode                int        `json:"status_code"`
	LatencyMs                 float64    `json:"latency_ms"`
	DurationMs                float64    `json:"duration_ms"`
	TTFTMs                    float64    `json:"ttft_ms"`
	ResponseHeaderMs          float64    `json:"response_header_ms"`
	ProxyOverheadMs           float64    `json:"proxy_overhead_ms"`
	ParseMs                   float64    `json:"parse_ms"`
	FailoverLookupMs          float64    `json:"failover_lookup_ms"`
	ModelLookupMs             float64    `json:"model_lookup_ms"`
	ProviderLookupMs          float64    `json:"provider_lookup_ms"`
	KeyDecryptMs              float64    `json:"key_decrypt_ms"`
	DialMs                    float64    `json:"dial_ms"`
	SettingsReadMs            float64    `json:"settings_read_ms"`
	CacheHits                 *CacheHits `json:"cache_hits,omitempty"`
	TokensPerSecond           float64    `json:"tokens_per_second"`
	TokensPrompt              int        `json:"tokens_prompt"`
	TokensCompletion          int        `json:"tokens_completion"`
	TokensCompletionReasoning int        `json:"tokens_completion_reasoning"`
	TokensPromptCacheHit      int        `json:"tokens_prompt_cache_hit"`
	TokensPromptCacheMiss     int        `json:"tokens_prompt_cache_miss"`
	Streaming                 bool       `json:"streaming"`
	VirtualKeyName            string     `json:"virtual_key_name"`
	VirtualKeyDeleted         bool       `json:"virtual_key_deleted"`
	VirtualKeyID              string     `json:"virtual_key_id"`
	ErrorMessage              string     `json:"error_message"`
	FailoverAttempt           int        `json:"failover_attempt"`
	State                     string     `json:"state"`
	CreatedAt                 time.Time  `json:"created_at"`
	ResolvedModelID           string     `json:"resolved_model_id"`
}

// LogsResponse is the paginated response for request logs.
type LogsResponse struct {
	Entries []LogEntry `json:"entries"`
	Total   int        `json:"total"`
	Page    int        `json:"page"`
	PerPage int        `json:"per_page"`
}

// RegisterLogs mounts log management routes.
func (h *Handler) RegisterLogs(r chi.Router) {
	r.Route("/logs", func(r chi.Router) {
		r.Get("/", h.ListLogs)
		r.Get("/cursor", h.ListLogsCursor)
		r.Get("/{id}", h.GetLog)
		r.Delete("/purge", h.PurgeLogs)
	})
}

// GetLog returns a single request log entry by ID.
func (h *Handler) GetLog(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUUIDParam(w, r, "id", "log ID")
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	var entry LogEntry
	err := h.dbPool.Pool().QueryRow(ctx, `
		SELECT rl.id, COALESCE(rl.provider_id::text, ''),
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
			COALESCE(rl.resolved_model_id, '')
		FROM request_logs rl LEFT JOIN providers p ON rl.provider_id = p.id
		LEFT JOIN virtual_keys vk ON rl.virtual_key_id = vk.id
		WHERE rl.id = $1`,
		id,
	).Scan(
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
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respondError(w, "log not found", nil, http.StatusNotFound)
		} else {
			respondError(w, "failed to fetch log", err, http.StatusInternalServerError)
		}
		return
	}

	writeJSON(w, entry)
}

// LogsCursorResponse is the cursor-based paginated response for request logs.
type LogsCursorResponse struct {
	Entries   []LogEntry `json:"entries"`
	Total     int        `json:"total"`
	HasBefore bool       `json:"has_before"`
	HasAfter  bool       `json:"has_after"`
}

// ListLogsCursor returns request logs using keyset (cursor) pagination.
//
// Query parameters:
//   - cursor: encoded cursor from a previous response (base64 JSON of {created_at, id})
//   - direction: "after" (default) or "before" — which way to scroll from cursor
//   - limit: page size (default 20, max 200)
//   - model_id, provider_id, status_code, from, to: same filters as ListLogs
//   - sort_by: only "time" is supported for cursor pagination (default "time")
//   - sort_dir: "desc" (default, newest first) or "asc"
//
// The first request omits cursor to get the newest entries.
// Subsequent requests pass the cursor from the response boundary and
// direction to scroll older ("before") or newer ("after").
func (h *Handler) ListLogsCursor(w http.ResponseWriter, r *http.Request) {
	p, ok := parseLogListParams(w, r)
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	query, args := buildLogListQuery(p)

	rows, err := h.dbPool.Pool().Query(ctx, query, args...)
	if err != nil {
		debuglog.Error("logs-cursor: failed to query logs", "error", err)
		respondError(w, "failed to query logs", err, http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	// limit is clamped to [1, 200] above; prealloc with the hard upper bound
	// to satisfy CodeQL's uncontrolled-allocation-size check (user input must
	// not flow into make() capacity even after clamping).
	entries := make([]LogEntry, 0, 201) // limit+1 for has_more detection
	for rows.Next() {
		entry, err := scanLogEntry(rows)
		if err != nil {
			debuglog.Error("logs-cursor: row scan failed", "error", err)
			continue
		}
		entries = append(entries, entry)
	}

	entries, hasAfter, hasBefore := paginate(entries, p)

	response := LogsCursorResponse{
		Entries:   entries,
		Total:     h.countLogs(ctx, p),
		HasBefore: hasBefore,
		HasAfter:  hasAfter,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		respondError(w, "failed to encode response", err, http.StatusInternalServerError)
	}
}

// buildLogListQuery assembles the cursor data query: the column projection, the
// shared filters, the keyset predicate (when a cursor is present), and the
// ORDER BY + LIMIT — fetching limit+1 to detect has_more, with the sort
// inverted for backward pagination so LIMIT picks from the correct end.
func buildLogListQuery(p logListParams) (string, []any) {
	query := `
        SELECT rl.id, COALESCE(rl.provider_id::text, ''),
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
            COALESCE(rl.resolved_model_id, '')
        FROM request_logs rl LEFT JOIN providers p ON rl.provider_id = p.id
        LEFT JOIN virtual_keys vk ON rl.virtual_key_id = vk.id
        WHERE 1=1
    `

	args := []any{}
	argIndex := 1
	query, args, argIndex = appendLogFilters(query, args, argIndex, p.modelID, p.providerID, p.statusCode, p.fromDate, p.toDate)
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
	query += " LIMIT $" + util.IntToStr(argIndex)
	args = append(args, p.limit+1)
	return query, args
}

// countLogs returns the total row count for the same filters as the data query.
// Best-effort: returns 0 on error (the cursor response is still useful without
// an accurate total).
func (h *Handler) countLogs(ctx context.Context, p logListParams) int {
	query := "SELECT COUNT(*) FROM request_logs rl WHERE 1=1"
	args := []any{}
	query, args, _ = appendLogFilters(query, args, 1, p.modelID, p.providerID, p.statusCode, p.fromDate, p.toDate)
	var total int
	_ = h.dbPool.Pool().QueryRow(ctx, query, args...).Scan(&total)
	return total
}

// paginate applies has_after/has_before detection (using the fetched-one-extra
// signal and cursor presence), trims to p.limit, and reverses the slice for
// backward pagination (which fetched in inverted sort order).
func paginate(entries []LogEntry, p logListParams) ([]LogEntry, bool, bool) {
	var hasAfter, hasBefore bool
	switch p.direction {
	case "after":
		// Fetching older entries (scroll down or initial load).
		if len(entries) > p.limit {
			hasAfter = true
			entries = entries[:p.limit]
		}
		// For an initial request (no cursor) we're at the newest — nothing
		// before. For cursor requests, assume newer entries exist until proven
		// otherwise (a fetchBefore returning 0 corrects this client-side).
		if p.cursorStr != "" {
			hasBefore = true
		}
	case "before":
		// Fetching newer entries (scroll up).
		if len(entries) > p.limit {
			hasBefore = true
			entries = entries[:p.limit]
		}
		// Items exist after the cursor by definition.
		if p.cursorStr != "" {
			hasAfter = true
		}
	}

	if p.direction == "before" {
		slices.Reverse(entries)
	}
	return entries, hasAfter, hasBefore
}

// scanLogEntry scans one request_logs row (the 30-column projection shared by
// ListLogsCursor and ListLogs) into a LogEntry.
func scanLogEntry(rows pgx.Rows) (LogEntry, error) {
	var entry LogEntry
	err := rows.Scan(
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
	)
	return entry, err
}

// appendLogFilters appends the shared modelID/providerID/statusCode/from/to
// WHERE fragments, returning the extended query, args, and next placeholder
// index. The single source of truth used by both the data and count queries
// in ListLogsCursor (previously two copy-pasted blocks that had drifted: the
// count copy lacked the `statusCode >= 0` guard the data copy has; both now use
// the guard, so an invalid negative status_code is uniformly ignored — a
// behaviour-neutral fix since status codes are always >= 0).
func appendLogFilters(query string, args []any, argIndex int, modelID, providerID, statusCodeStr, fromDate, toDate string) (string, []any, int) {
	if modelID != "" {
		query += " AND rl.model_id ILIKE $" + util.IntToStr(argIndex)
		args = append(args, "%"+modelID+"%")
		argIndex++
	}
	if providerID != "" {
		providerUUID, err := uuid.Parse(providerID)
		if err == nil {
			query += " AND rl.provider_id = $" + util.IntToStr(argIndex)
			args = append(args, providerUUID)
			argIndex++
		}
	}
	if statusCodeStr != "" {
		if statusCodeStr == "4xx" {
			query += " AND rl.status_code >= 400 AND rl.status_code < 500"
		} else if statusCodeStr == "5xx" {
			query += " AND rl.status_code >= 500"
		} else if statusCode, err := strconv.Atoi(statusCodeStr); err == nil && statusCode >= 0 {
			if statusCode == 0 {
				query += " AND (rl.status_code = 0 OR rl.status_code IS NULL)"
			} else {
				query += " AND rl.status_code = $" + util.IntToStr(argIndex)
				args = append(args, statusCode)
				argIndex++
			}
		}
	}
	if fromDate != "" {
		if parsedFrom, err := time.Parse(time.RFC3339, fromDate); err == nil {
			query += " AND rl.created_at >= $" + util.IntToStr(argIndex)
			args = append(args, parsedFrom)
			argIndex++
		}
	}
	if toDate != "" {
		if parsedTo, err := time.Parse(time.RFC3339, toDate); err == nil {
			query += " AND rl.created_at <= $" + util.IntToStr(argIndex)
			args = append(args, parsedTo)
			argIndex++
		}
	}
	return query, args, argIndex
}

// appendKeysetPredicate appends the (created_at, id) keyset comparison relative
// to the cursor. The comparison operator is "<" when scrolling toward older
// rows — (after, desc) or (before, asc) — and ">" otherwise, collapsing the
// four direction/sort branches into one template. SQL is byte-identical to the
// per-branch form.
func appendKeysetPredicate(query string, args []any, argIndex int, cursor logCursor, direction, sortDir string) (string, []any, int) {
	op := ">"
	if (direction == "after") == (sortDir == "desc") {
		op = "<"
	}
	query += " AND (rl.created_at " + op + " $" + util.IntToStr(argIndex) +
		" OR (rl.created_at = $" + util.IntToStr(argIndex+1) +
		" AND rl.id " + op + " $" + util.IntToStr(argIndex+2) + "))"
	args = append(args, cursor.CreatedAt, cursor.CreatedAt, cursor.ID)
	argIndex += 3
	return query, args, argIndex
}

// logListParams holds the parsed, validated query inputs for the cursor log
// endpoint: limit clamped to [1,200], direction/sortDir defaulted, filters, and
// the decoded cursor.
type logListParams struct {
	limit      int
	cursorStr  string
	cursor     logCursor
	direction  string
	sortDir    string
	modelID    string
	providerID string
	statusCode string
	fromDate   string
	toDate     string
}

// parseLogListParams reads and validates the pagination/filter query params. On
// an undecodable cursor it writes a 400 response and returns ok=false.
func parseLogListParams(w http.ResponseWriter, r *http.Request) (logListParams, bool) {
	p := logListParams{
		limit:      util.GetIntQueryParam(r, "limit", 20),
		cursorStr:  r.URL.Query().Get("cursor"),
		direction:  r.URL.Query().Get("direction"),
		sortDir:    r.URL.Query().Get("sort_dir"),
		modelID:    r.URL.Query().Get("model_id"),
		providerID: r.URL.Query().Get("provider_id"),
		statusCode: r.URL.Query().Get("status_code"),
		fromDate:   r.URL.Query().Get("from"),
		toDate:     r.URL.Query().Get("to"),
	}
	if p.limit < 1 {
		p.limit = 1
	}
	if p.limit > 200 {
		p.limit = 200
	}
	if p.direction != "before" && p.direction != "after" {
		p.direction = "after"
	}
	if p.sortDir != "asc" && p.sortDir != "desc" {
		p.sortDir = "desc"
	}
	if p.cursorStr != "" {
		if err := p.cursor.decode(p.cursorStr); err != nil {
			respondBadRequest(w, "invalid cursor", err)
			return p, false
		}
	}
	return p, true
}

// logCursor is the keyset cursor for cursor-based log pagination.
// It encodes the created_at and id of a boundary row so the next page
// can be fetched relative to it.
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

// PurgeLogsRequest is the request body for purging logs.
type PurgeLogsRequest struct {
	OlderThan string `json:"older_than"`
}

// PurgeLogs deletes old request logs based on the specified time range.
func (h *Handler) PurgeLogs(w http.ResponseWriter, r *http.Request) {
	var req PurgeLogsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondBadRequest(w, "invalid request body", err)
		return
	}

	var cutoff time.Time
	switch req.OlderThan {
	case "1h":
		cutoff = time.Now().Add(-1 * time.Hour)
	case "1d":
		cutoff = time.Now().Add(-24 * time.Hour)
	case "1w":
		cutoff = time.Now().Add(-7 * 24 * time.Hour)
	case "1m":
		cutoff = time.Now().Add(-30 * 24 * time.Hour)
	case "all":
		_, err := h.dbPool.Pool().Exec(r.Context(), `DELETE FROM request_logs`)
		if err != nil {
			respondError(w, "failed to purge logs", err, http.StatusInternalServerError)
			return
		}
		debuglog.Info("logs: purged all logs")
		w.WriteHeader(http.StatusNoContent)
		return
	default:
		http.Error(w, "invalid older_than value, use: 1h, 1d, 1w, 1m, all", http.StatusBadRequest)
		return
	}

	_, err := h.dbPool.Pool().Exec(r.Context(),
		`DELETE FROM request_logs WHERE created_at < $1`, cutoff)
	if err != nil {
		respondError(w, "failed to purge old logs", err, http.StatusInternalServerError)
		return
	}
	debuglog.Info("logs: purged old logs", "cutoff", cutoff)

	w.WriteHeader(http.StatusNoContent)
}

// ListLogs returns paginated request logs with filtering and sorting.
func (h *Handler) ListLogs(w http.ResponseWriter, r *http.Request) {
	page := util.GetIntQueryParam(r, "page", 1)
	if page < 1 {
		page = 1
	}
	perPage := util.GetIntQueryParam(r, "per_page", 20)
	if perPage < 1 {
		perPage = 1
	}
	if perPage > 200 {
		perPage = 200
	}
	cacheKey := r.URL.RawQuery
	modelID := r.URL.Query().Get("model_id")
	providerID := r.URL.Query().Get("provider_id")
	statusCodeStr := r.URL.Query().Get("status_code")
	fromDate := r.URL.Query().Get("from")
	toDate := r.URL.Query().Get("to")
	sortBy := r.URL.Query().Get("sort_by")
	sortDir := r.URL.Query().Get("sort_dir")

	type sortDef struct {
		tierExpr  string
		valueExpr string
	}

	sortColumns := map[string]sortDef{
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
	}

	if _, ok := sortColumns[sortBy]; !ok {
		sortBy = "time"
	}
	if sortDir != "asc" && sortDir != "desc" {
		sortDir = "desc"
	}

	offset := (page - 1) * perPage

	if cached, ok := globalLogsCache.get(cacheKey); ok {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Cache", "HIT")
		_ = json.NewEncoder(w).Encode(cached)
		return
	}

	query := `
        SELECT COUNT(*) OVER() AS total_count,
               rl.id, COALESCE(rl.provider_id::text, ''),
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
            COALESCE(rl.resolved_model_id, '')
        FROM request_logs rl LEFT JOIN providers p ON rl.provider_id = p.id
        LEFT JOIN virtual_keys vk ON rl.virtual_key_id = vk.id
        WHERE 1=1
    `

	args := []interface{}{}
	argIndex := 1

	if modelID != "" {
		query += " AND rl.model_id ILIKE $" + util.IntToStr(argIndex)
		args = append(args, "%"+modelID+"%")
		argIndex++
	}

	if providerID != "" {
		providerUUID, err := uuid.Parse(providerID)
		if err == nil {
			query += " AND rl.provider_id = $" + util.IntToStr(argIndex)
			args = append(args, providerUUID)
			argIndex++
		}
	}

	if statusCodeStr != "" {
		if statusCodeStr == "4xx" {
			query += " AND rl.status_code >= 400 AND rl.status_code < 500"
		} else if statusCodeStr == "5xx" {
			query += " AND rl.status_code >= 500"
		} else if statusCode, err := strconv.Atoi(statusCodeStr); err == nil && statusCode >= 0 {
			if statusCode == 0 {
				// COALESCE presents NULL status_code as 0 to the frontend,
				// so "0 No Response" must match both actual 0 and NULL.
				query += " AND (rl.status_code = 0 OR rl.status_code IS NULL)"
			} else {
				query += " AND rl.status_code = $" + util.IntToStr(argIndex)
				args = append(args, statusCode)
				argIndex++
			}
		}
	}

	if fromDate != "" {
		parsedFrom, err := time.Parse(time.RFC3339, fromDate)
		if err == nil {
			query += " AND rl.created_at >= $" + util.IntToStr(argIndex)
			args = append(args, parsedFrom)
			argIndex++
		}
	}

	if toDate != "" {
		parsedTo, err := time.Parse(time.RFC3339, toDate)
		if err == nil {
			query += " AND rl.created_at <= $" + util.IntToStr(argIndex)
			args = append(args, parsedTo)
			argIndex++
		}
	}

	sd := sortColumns[sortBy]
	orderClause := " ORDER BY "
	if sd.tierExpr != "" {
		orderClause += sd.tierExpr + " ASC, "
	}
	orderClause += sd.valueExpr + " " + sortDir

	if sortBy == "status" {
		orderClause += ", CASE WHEN COALESCE(rl.error_message, '') ILIKE '%cancel%' OR COALESCE(rl.error_message, '') ILIKE '%disconnect%' OR COALESCE(rl.error_message, '') ILIKE '%context canceled%' THEN 1 ELSE 0 END ASC"
	}

	orderClause += " LIMIT $" + util.IntToStr(argIndex) + " OFFSET $" + util.IntToStr(argIndex+1)
	query += orderClause
	args = append(args, perPage, offset)

	rows, err := h.dbPool.Pool().Query(r.Context(), query, args...)
	if err != nil {
		debuglog.Error("logs: failed to query logs", "error", err)
		respondError(w, "failed to query logs", err, http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	entries := make([]LogEntry, 0)
	var total int
	for rows.Next() {
		var entry LogEntry
		var totalCount int
		err := rows.Scan(
			&totalCount,
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
		)
		if err != nil {
			debuglog.Error("logs: row scan failed", "error", err)
			continue
		}
		if total == 0 {
			total = totalCount
		}
		entries = append(entries, entry)
	}

	response := LogsResponse{
		Entries: entries,
		Total:   total,
		Page:    page,
		PerPage: perPage,
	}

	globalLogsCache.set(cacheKey, &response)
	w.Header().Set("X-Cache", "MISS")
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		respondError(w, "failed to encode response", err, http.StatusInternalServerError)
	}
}
