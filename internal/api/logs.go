package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// LogEntry represents a single request log entry.
type LogEntry struct {
	ID                        string    `json:"id"`
	ProviderID                string    `json:"provider_id"`
	ProviderName              string    `json:"provider_name"`
	ModelID                   string    `json:"model_id"`
	RequestHash               string    `json:"request_hash"`
	StatusCode                int       `json:"status_code"`
	LatencyMs                 float64   `json:"latency_ms"`
	DurationMs                float64   `json:"duration_ms"`
	TTFTMs                    float64   `json:"ttft_ms"`
	ProxyOverheadMs           float64   `json:"proxy_overhead_ms"`
	ParseMs                   float64   `json:"parse_ms"`
	FailoverLookupMs          float64   `json:"failover_lookup_ms"`
	ModelLookupMs             float64   `json:"model_lookup_ms"`
	ProviderLookupMs          float64   `json:"provider_lookup_ms"`
	KeyDecryptMs              float64   `json:"key_decrypt_ms"`
	DialMs                    float64   `json:"dial_ms"`
	SettingsReadMs            float64   `json:"settings_read_ms"`
	TokensPerSecond           float64   `json:"tokens_per_second"`
	TokensPrompt              int       `json:"tokens_prompt"`
	TokensCompletion          int       `json:"tokens_completion"`
	TokensCompletionReasoning int       `json:"tokens_completion_reasoning"`
	Streaming                 bool      `json:"streaming"`
	VirtualKeyName            string    `json:"virtual_key_name"`
	VirtualKeyDeleted         bool      `json:"virtual_key_deleted"`
	VirtualKeyID              string    `json:"virtual_key_id"`
	ErrorMessage              string    `json:"error_message"`
	FailoverAttempt           int       `json:"failover_attempt"`
	State                     string    `json:"state"`
	CreatedAt                 time.Time `json:"created_at"`
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
		r.Delete("/purge", h.PurgeLogs)
	})
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
	limit := util.GetIntQueryParam(r, "limit", 20)
	if limit < 1 {
		limit = 1
	}
	if limit > 200 {
		limit = 200
	}
	cursorStr := r.URL.Query().Get("cursor")
	direction := r.URL.Query().Get("direction")
	if direction != "before" && direction != "after" {
		direction = "after"
	}
	sortDir := r.URL.Query().Get("sort_dir")
	if sortDir != "asc" && sortDir != "desc" {
		sortDir = "desc"
	}

	modelID := r.URL.Query().Get("model_id")
	providerID := r.URL.Query().Get("provider_id")
	statusCodeStr := r.URL.Query().Get("status_code")
	fromDate := r.URL.Query().Get("from")
	toDate := r.URL.Query().Get("to")

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	// Parse cursor if provided
	var cursor logCursor
	if cursorStr != "" {
		if err := cursor.decode(cursorStr); err != nil {
			respondBadRequest(w, "invalid cursor", err)
			return
		}
	}

	// Build the base SELECT (same columns as ListLogs minus COUNT(*) OVER())
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
            COALESCE(rl.tokens_per_second, 0),
            COALESCE(rl.tokens_prompt, 0), COALESCE(rl.tokens_completion, 0),
            COALESCE(rl.tokens_completion_reasoning, 0),
            COALESCE(rl.streaming, false), COALESCE(rl.virtual_key_name, ''), COALESCE(rl.virtual_key_id::text, ''),
             CASE
                WHEN rl.virtual_key_id IS NULL OR rl.virtual_key_id::text = '' THEN false
                WHEN vk.id IS NULL THEN true
                ELSE false
            END AS virtual_key_deleted,
            COALESCE(rl.error_message, ''), COALESCE(rl.failover_attempt, 0), COALESCE(rl.state, 'completed'), rl.created_at
        FROM request_logs rl LEFT JOIN providers p ON rl.provider_id = p.id
        LEFT JOIN virtual_keys vk ON rl.virtual_key_id = vk.id
        WHERE 1=1
    `

	args := []interface{}{}
	argIndex := 1

	// Apply filters (same as ListLogs)
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

	// Apply cursor keyset predicate
	// For "time desc" (default): "after" means older (created_at < cursor OR same ts but id < cursor)
	//                              "before" means newer (created_at > cursor OR same ts but id > cursor)
	// For "time asc": the directions invert.
	if cursorStr != "" {
		if direction == "after" {
			if sortDir == "desc" {
				// Scrolling older: (created_at, id) < (cursor.CreatedAt, cursor.ID)
				query += " AND (rl.created_at < $" + util.IntToStr(argIndex) +
					" OR (rl.created_at = $" + util.IntToStr(argIndex+1) +
					" AND rl.id < $" + util.IntToStr(argIndex+2) + "))"
				args = append(args, cursor.CreatedAt, cursor.CreatedAt, cursor.ID)
				argIndex += 3
			} else {
				// Scrolling newer in asc mode: (created_at, id) > cursor
				query += " AND (rl.created_at > $" + util.IntToStr(argIndex) +
					" OR (rl.created_at = $" + util.IntToStr(argIndex+1) +
					" AND rl.id > $" + util.IntToStr(argIndex+2) + "))"
				args = append(args, cursor.CreatedAt, cursor.CreatedAt, cursor.ID)
				argIndex += 3
			}
		} else { // before
			if sortDir == "desc" {
				// Scrolling newer: (created_at, id) > cursor
				query += " AND (rl.created_at > $" + util.IntToStr(argIndex) +
					" OR (rl.created_at = $" + util.IntToStr(argIndex+1) +
					" AND rl.id > $" + util.IntToStr(argIndex+2) + "))"
				args = append(args, cursor.CreatedAt, cursor.CreatedAt, cursor.ID)
				argIndex += 3
			} else {
				// Scrolling older in asc mode: (created_at, id) < cursor
				query += " AND (rl.created_at < $" + util.IntToStr(argIndex) +
					" OR (rl.created_at = $" + util.IntToStr(argIndex+1) +
					" AND rl.id < $" + util.IntToStr(argIndex+2) + "))"
				args = append(args, cursor.CreatedAt, cursor.CreatedAt, cursor.ID)
				argIndex += 3
			}
		}
	}

	// ORDER BY + LIMIT (fetch limit+1 to detect has_more)
	// When paginating backward, invert the sort direction so LIMIT picks from
	// the correct end of the result set, then reverse the slice before returning.
	fetchLimit := limit + 1
	fetchSortDir := sortDir
	if direction == "before" {
		if fetchSortDir == "desc" {
			fetchSortDir = "asc"
		} else {
			fetchSortDir = "desc"
		}
	}
	query += " ORDER BY rl.created_at " + fetchSortDir + ", rl.id " + fetchSortDir
	query += " LIMIT $" + util.IntToStr(argIndex)
	args = append(args, fetchLimit)

	rows, err := h.dbPool.Pool().Query(ctx, query, args...)
	if err != nil {
		debuglog.Error("logs-cursor: failed to query logs", "error", err)
		respondError(w, "failed to query logs", err, http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	entries := make([]LogEntry, 0, limit)
	for rows.Next() {
		var entry LogEntry
		err := rows.Scan(
			&entry.ID, &entry.ProviderID, &entry.ProviderName, &entry.ModelID,
			&entry.RequestHash, &entry.StatusCode, &entry.LatencyMs, &entry.DurationMs,
			&entry.TTFTMs, &entry.ProxyOverheadMs,
			&entry.ParseMs, &entry.FailoverLookupMs, &entry.ModelLookupMs, &entry.ProviderLookupMs, &entry.KeyDecryptMs,
			&entry.DialMs, &entry.SettingsReadMs,
			&entry.TokensPerSecond,
			&entry.TokensPrompt, &entry.TokensCompletion, &entry.TokensCompletionReasoning,
			&entry.Streaming,
			&entry.VirtualKeyName, &entry.VirtualKeyID, &entry.VirtualKeyDeleted,
			&entry.ErrorMessage,
			&entry.FailoverAttempt, &entry.State, &entry.CreatedAt,
		)
		if err != nil {
			debuglog.Error("logs-cursor: row scan failed", "error", err)
			continue
		}
		entries = append(entries, entry)
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
		// For cursor requests, assume there are newer entries until proven otherwise
		// (a fetchBefore returning 0 entries will correct this on the client side)
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

	// Get total count for display (separate lightweight query)
	var total int
	countArgs := []interface{}{}
	countArgIdx := 1
	countQuery := "SELECT COUNT(*) FROM request_logs rl WHERE 1=1"
	if modelID != "" {
		countQuery += " AND rl.model_id ILIKE $" + util.IntToStr(countArgIdx)
		countArgs = append(countArgs, "%"+modelID+"%")
		countArgIdx++
	}
	if providerID != "" {
		providerUUID, err := uuid.Parse(providerID)
		if err == nil {
			countQuery += " AND rl.provider_id = $" + util.IntToStr(countArgIdx)
			countArgs = append(countArgs, providerUUID)
			countArgIdx++
		}
	}
	if statusCodeStr != "" {
		if statusCodeStr == "4xx" {
			countQuery += " AND rl.status_code >= 400 AND rl.status_code < 500"
		} else if statusCodeStr == "5xx" {
			countQuery += " AND rl.status_code >= 500"
		} else if statusCode, err := strconv.Atoi(statusCodeStr); err == nil {
			if statusCode == 0 {
				countQuery += " AND (rl.status_code = 0 OR rl.status_code IS NULL)"
			} else {
				countQuery += " AND rl.status_code = $" + util.IntToStr(countArgIdx)
				countArgs = append(countArgs, statusCode)
				countArgIdx++
			}
		}
	}
	if fromDate != "" {
		if parsedFrom, err := time.Parse(time.RFC3339, fromDate); err == nil {
			countQuery += " AND rl.created_at >= $" + util.IntToStr(countArgIdx)
			countArgs = append(countArgs, parsedFrom)
			countArgIdx++
		}
	}
	if toDate != "" {
		if parsedTo, err := time.Parse(time.RFC3339, toDate); err == nil {
			countQuery += " AND rl.created_at <= $" + util.IntToStr(countArgIdx)
			countArgs = append(countArgs, parsedTo)
		}
	}
	_ = h.dbPool.Pool().QueryRow(ctx, countQuery, countArgs...).Scan(&total)

	response := LogsCursorResponse{
		Entries:   entries,
		Total:     total,
		HasBefore: hasBefore,
		HasAfter:  hasAfter,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		respondError(w, "failed to encode response", err, http.StatusInternalServerError)
	}
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
		"time":     {"", "rl.created_at"},
		"model":    {"", "rl.model_id"},
		"provider": {"CASE WHEN rl.provider_id IS NULL THEN 2 WHEN p.name IS NULL THEN 1 ELSE 0 END", "CASE WHEN rl.provider_id IS NULL THEN '' WHEN p.name IS NOT NULL THEN p.name ELSE 'Deleted' END"},
		"status":   {"", "rl.status_code"},
		"tokens":   {"CASE WHEN rl.tokens_prompt + rl.tokens_completion + COALESCE(rl.tokens_completion_reasoning, 0) = 0 THEN CASE WHEN COALESCE(rl.error_message, '') ILIKE '%cancel%' OR COALESCE(rl.error_message, '') ILIKE '%disconnect%' OR COALESCE(rl.error_message, '') ILIKE '%context canceled%' THEN 1 ELSE 2 END ELSE 0 END", "rl.tokens_prompt + rl.tokens_completion + COALESCE(rl.tokens_completion_reasoning, 0)"},
		"tps":      {"CASE WHEN rl.tokens_per_second = 0 THEN 1 ELSE 0 END", "rl.tokens_per_second"},
		"ttft":     {"CASE WHEN rl.ttft_ms = 0 THEN 1 ELSE 0 END", "rl.ttft_ms"},
		"duration": {"CASE WHEN rl.duration_ms = 0 THEN 1 ELSE 0 END", "rl.duration_ms"},
		"overhead": {"CASE WHEN rl.proxy_overhead_ms = 0 THEN 1 ELSE 0 END", "rl.proxy_overhead_ms"},
		"key":      {"", "CASE WHEN rl.virtual_key_id IS NOT NULL AND rl.virtual_key_id::text != '' AND vk.id IS NULL THEN 'zzzzzzzz' ELSE COALESCE(rl.virtual_key_name, '') END"},
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
               COALESCE(rl.tokens_per_second, 0),
               COALESCE(rl.tokens_prompt, 0), COALESCE(rl.tokens_completion, 0),
               COALESCE(rl.tokens_completion_reasoning, 0),
COALESCE(rl.streaming, false), COALESCE(rl.virtual_key_name, ''), COALESCE(rl.virtual_key_id::text, ''),
                CASE
                    WHEN rl.virtual_key_id IS NULL OR rl.virtual_key_id::text = '' THEN false
                    WHEN vk.id IS NULL THEN true
                    ELSE false
                END AS virtual_key_deleted,
               COALESCE(rl.error_message, ''), COALESCE(rl.failover_attempt, 0), COALESCE(rl.state, 'completed'), rl.created_at
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
			&entry.TokensPerSecond,
			&entry.TokensPrompt, &entry.TokensCompletion, &entry.TokensCompletionReasoning,
			&entry.Streaming,
			&entry.VirtualKeyName, &entry.VirtualKeyID, &entry.VirtualKeyDeleted,
			&entry.ErrorMessage,
			&entry.FailoverAttempt, &entry.State, &entry.CreatedAt,
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
