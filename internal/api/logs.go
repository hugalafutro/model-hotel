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
	ModelID           string    `json:"model_id"`
	RequestID         string    `json:"request_id"`
	StatusCode        int       `json:"status_code"`
	LatencyMs         int       `json:"latency_ms"`
	TokensPrompt      int       `json:"tokens_prompt"`
	TokensCompletion  int       `json:"tokens_completion"`
	Streaming         bool      `json:"streaming"`
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
	})
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
		SELECT id, provider_id, model_id, request_id, status_code, latency_ms,
		       tokens_prompt, tokens_completion, streaming, error_message, created_at
		FROM request_logs
		WHERE 1=1
	`

	args := []interface{}{}
	argIndex := 1

	if modelID != "" {
		query += " AND model_id = $" + toString(argIndex)
		args = append(args, modelID)
		argIndex++
	}

	if providerID != "" {
		providerUUID, err := uuid.Parse(providerID)
		if err == nil {
			query += " AND provider_id = $" + toString(argIndex)
			args = append(args, providerUUID)
			argIndex++
		}
	}

	if statusCode > 0 {
		query += " AND status_code = $" + toString(argIndex)
		args = append(args, statusCode)
		argIndex++
	}

	if fromDate != "" {
		parsedFrom, err := time.Parse(time.RFC3339, fromDate)
		if err == nil {
			query += " AND created_at >= $" + toString(argIndex)
			args = append(args, parsedFrom)
			argIndex++
		}
	}

	if toDate != "" {
		parsedTo, err := time.Parse(time.RFC3339, toDate)
		if err == nil {
			query += " AND created_at <= $" + toString(argIndex)
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

	query += " ORDER BY created_at DESC LIMIT $" + toString(argIndex) + " OFFSET $" + toString(argIndex+1)
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
			&entry.ID, &entry.ProviderID, &entry.ModelID, &entry.RequestID,
			&entry.StatusCode, &entry.LatencyMs, &entry.TokensPrompt,
			&entry.TokensCompletion, &entry.Streaming, &entry.ErrorMessage,
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
