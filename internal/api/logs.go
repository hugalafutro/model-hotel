package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type LogEntry struct {
	ID                string    `json:"id"`
	ProviderID        string    `json:"provider_id"`
	ProviderName      string    `json:"provider_name"`
	ModelID           string    `json:"model_id"`
	RequestID         string    `json:"request_id"`
	RequestHash       string    `json:"request_hash"`
	StatusCode        int       `json:"status_code"`
	LatencyMs         int       `json:"latency_ms"`
	DurationMs        float64   `json:"duration_ms"`
	TTFTMs            int       `json:"ttft_ms"`
	ProxyOverheadMs   float64   `json:"proxy_overhead_ms"`
	ParseMs           float64   `json:"parse_ms"`
	ModelLookupMs     float64   `json:"model_lookup_ms"`
	ProviderLookupMs  float64   `json:"provider_lookup_ms"`
	KeyDecryptMs      float64   `json:"key_decrypt_ms"`
	TokensPerSecond   *float64  `json:"tokens_per_second"`
	TokensPrompt      int       `json:"tokens_prompt"`
	TokensCompletion  int       `json:"tokens_completion"`
	Streaming         bool      `json:"streaming"`
	VirtualKeyName    string    `json:"virtual_key_name"`
	ErrorMessage      string    `json:"error_message"`
	CreatedAt         time.Time `json:"created_at"`
}

type LogsResponse struct {
	Entries []LogEntry `json:"entries"`
	Total   int        `json:"total"`
	Page    int        `json:"page"`
	PerPage int        `json:"per_page"`
}

func (h *Handler) RegisterLogs(r chi.Router) {
	r.Route("/logs", func(r chi.Router) {
		r.Get("/", h.ListLogs)
		r.Delete("/purge", h.PurgeLogs)
	})
}

type PurgeLogsRequest struct {
	OlderThan string `json:"older_than"`
}

func (h *Handler) PurgeLogs(w http.ResponseWriter, r *http.Request) {
	var req PurgeLogsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
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
			http.Error(w, err.Error(), http.StatusInternalServerError)
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
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) ListLogs(w http.ResponseWriter, r *http.Request) {
	page := getIntQueryParam(r, "page", 1)
	perPage := getIntQueryParam(r, "per_page", 20)
	modelID := r.URL.Query().Get("model_id")
	providerID := r.URL.Query().Get("provider_id")
	statusCode := getIntQueryParam(r, "status_code", 0)
	fromDate := r.URL.Query().Get("from")
	toDate := r.URL.Query().Get("to")

	offset := (page - 1) * perPage

	query := `
		SELECT rl.id, COALESCE(rl.provider_id::text, ''), COALESCE(p.name, 'Deleted'),
		       rl.model_id, rl.request_id,
		       COALESCE(rl.request_hash, ''), rl.status_code,
		       COALESCE(rl.latency_ms, 0), COALESCE(rl.duration_ms, 0),
		       COALESCE(rl.ttft_ms, 0), COALESCE(rl.proxy_overhead_ms, 0),
		       COALESCE(rl.parse_ms, 0), COALESCE(rl.model_lookup_ms, 0), COALESCE(rl.provider_lookup_ms, 0), COALESCE(rl.key_decrypt_ms, 0),
		       rl.tokens_per_second,
		       COALESCE(rl.tokens_prompt, 0), COALESCE(rl.tokens_completion, 0),
		       COALESCE(rl.streaming, false), COALESCE(rl.virtual_key_name, ''),
		       COALESCE(rl.error_message, ''), rl.created_at
		FROM request_logs rl LEFT JOIN providers p ON rl.provider_id = p.id
		WHERE 1=1
	`

	args := []interface{}{}
	argIndex := 1

	if modelID != "" {
		query += " AND rl.model_id = $" + toString(argIndex)
		args = append(args, modelID)
		argIndex++
	}

	if providerID != "" {
		providerUUID, err := uuid.Parse(providerID)
		if err == nil {
			query += " AND rl.provider_id = $" + toString(argIndex)
			args = append(args, providerUUID)
			argIndex++
		}
	}

	if statusCode > 0 {
		query += " AND rl.status_code = $" + toString(argIndex)
		args = append(args, statusCode)
		argIndex++
	}

	if fromDate != "" {
		parsedFrom, err := time.Parse(time.RFC3339, fromDate)
		if err == nil {
			query += " AND rl.created_at >= $" + toString(argIndex)
			args = append(args, parsedFrom)
			argIndex++
		}
	}

	if toDate != "" {
		parsedTo, err := time.Parse(time.RFC3339, toDate)
		if err == nil {
			query += " AND rl.created_at <= $" + toString(argIndex)
			args = append(args, parsedTo)
			argIndex++
		}
	}

	var total int
	countQuery := "SELECT COUNT(*) FROM (" + query + ") as count_query"
	err := h.dbPool.Pool().QueryRow(r.Context(), countQuery, args...).Scan(&total)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	query += " ORDER BY rl.created_at DESC LIMIT $" + toString(argIndex) + " OFFSET $" + toString(argIndex+1)
	args = append(args, perPage, offset)

	rows, err := h.dbPool.Pool().Query(r.Context(), query, args...)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	entries := make([]LogEntry, 0)
	for rows.Next() {
		var entry LogEntry
		err := rows.Scan(
			&entry.ID, &entry.ProviderID, &entry.ProviderName, &entry.ModelID, &entry.RequestID,
			&entry.RequestHash, &entry.StatusCode, &entry.LatencyMs, &entry.DurationMs,
			&entry.TTFTMs, &entry.ProxyOverheadMs,
			&entry.ParseMs, &entry.ModelLookupMs, &entry.ProviderLookupMs, &entry.KeyDecryptMs,
			&entry.TokensPerSecond,
			&entry.TokensPrompt, &entry.TokensCompletion, &entry.Streaming,
			&entry.VirtualKeyName, &entry.ErrorMessage,
			&entry.CreatedAt,
		)
		if err != nil {
			continue
		}
		entries = append(entries, entry)
	}

	response := LogsResponse{
		Entries: entries,
		Total:   total,
		Page:    page,
		PerPage: perPage,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func getIntQueryParam(r *http.Request, key string, defaultValue int) int {
	val := r.URL.Query().Get(key)
	if val == "" {
		return defaultValue
	}

	var result int
	if _, err := fmt.Sscanf(val, "%d", &result); err != nil {
		return defaultValue
	}
	return result
}

func toString(i int) string {
	return fmt.Sprintf("%d", i)
}
