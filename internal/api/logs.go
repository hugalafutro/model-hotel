package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/util"
)

// LogEntry represents a single request log entry.
type LogEntry struct {
	ID                string    `json:"id"`
	ProviderID        string    `json:"provider_id"`
	ProviderName      string    `json:"provider_name"`
	ModelID           string    `json:"model_id"`
	RequestID         string    `json:"request_id"`
	RequestHash       string    `json:"request_hash"`
	StatusCode        int       `json:"status_code"`
	LatencyMs         float64   `json:"latency_ms"`
	DurationMs        float64   `json:"duration_ms"`
	TTFTMs            float64   `json:"ttft_ms"`
	ProxyOverheadMs   float64   `json:"proxy_overhead_ms"`
	ParseMs           float64   `json:"parse_ms"`
	ModelLookupMs     float64   `json:"model_lookup_ms"`
	ProviderLookupMs  float64   `json:"provider_lookup_ms"`
	KeyDecryptMs      float64   `json:"key_decrypt_ms"`
	SafeDialMs        float64   `json:"safe_dial_ms"`
	SettingsReadMs    float64   `json:"settings_read_ms"`
	TokensPerSecond   float64   `json:"tokens_per_second"`
	TokensPrompt      int       `json:"tokens_prompt"`
	TokensCompletion  int       `json:"tokens_completion"`
	Streaming         bool      `json:"streaming"`
	VirtualKeyName    string    `json:"virtual_key_name"`
	VirtualKeyDeleted bool      `json:"virtual_key_deleted"`
	VirtualKeyID      string    `json:"virtual_key_id"`
	ErrorMessage      string    `json:"error_message"`
	FailoverAttempt   int       `json:"failover_attempt"`
	State             string    `json:"state"`
	CreatedAt         time.Time `json:"created_at"`
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
		r.Delete("/purge", h.PurgeLogs)
	})
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
		"tokens":   {"CASE WHEN rl.tokens_prompt + rl.tokens_completion = 0 THEN CASE WHEN COALESCE(rl.error_message, '') ILIKE '%cancel%' OR COALESCE(rl.error_message, '') ILIKE '%disconnect%' OR COALESCE(rl.error_message, '') ILIKE '%context canceled%' THEN 1 ELSE 2 END ELSE 0 END", "rl.tokens_prompt + rl.tokens_completion"},
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
               rl.model_id, COALESCE(rl.request_id, ''),
               COALESCE(rl.request_hash, ''), COALESCE(rl.status_code, 0),
               COALESCE(rl.latency_ms, 0), COALESCE(rl.duration_ms, 0),
               COALESCE(rl.ttft_ms, 0), COALESCE(rl.proxy_overhead_ms, 0),
               COALESCE(rl.parse_ms, 0), COALESCE(rl.model_lookup_ms, 0), COALESCE(rl.provider_lookup_ms, 0), COALESCE(rl.key_decrypt_ms, 0),
               COALESCE(rl.safe_dial_ms, 0), COALESCE(rl.settings_read_ms, 0),
               COALESCE(rl.tokens_per_second, 0),
               COALESCE(rl.tokens_prompt, 0), COALESCE(rl.tokens_completion, 0),
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
			&entry.ID, &entry.ProviderID, &entry.ProviderName, &entry.ModelID, &entry.RequestID,
			&entry.RequestHash, &entry.StatusCode, &entry.LatencyMs, &entry.DurationMs,
			&entry.TTFTMs, &entry.ProxyOverheadMs,
			&entry.ParseMs, &entry.ModelLookupMs, &entry.ProviderLookupMs, &entry.KeyDecryptMs,
			&entry.SafeDialMs, &entry.SettingsReadMs,
			&entry.TokensPerSecond,
			&entry.TokensPrompt, &entry.TokensCompletion, &entry.Streaming,
			&entry.VirtualKeyName, &entry.VirtualKeyID, &entry.VirtualKeyDeleted,
			&entry.ErrorMessage,
			&entry.FailoverAttempt, &entry.State, &entry.CreatedAt,
		)
		if err != nil {
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
