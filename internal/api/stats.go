package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type StatsHandler struct {
	dbPool *pgxpool.Pool
	adminMgr interface {
		Validate(token string) bool
	}
}

func NewStatsHandler(dbPool *pgxpool.Pool, adminMgr interface {
	Validate(token string) bool
}) *StatsHandler {
	return &StatsHandler{
		dbPool:   dbPool,
		adminMgr: adminMgr,
	}
}

type StatsResponse struct {
	TotalRequestsLast24h int                    `json:"total_requests_last_24h"`
	TotalRequestsLast7d  int                    `json:"total_requests_last_7d"`
	ByModel              map[string]int         `json:"by_model"`
	ByProvider           map[string]int         `json:"by_provider"`
	AvgLatencyMs         int                    `json:"avg_latency_ms"`
	ErrorRate            float64                `json:"error_rate"`
	TotalTokensPrompt    int                    `json:"total_tokens_prompt"`
	TotalTokensCompletion int                   `json:"total_tokens_completion"`
}

func (h *StatsHandler) Register(r chi.Router) {
	r.Route("/stats", func(r chi.Router) {
		r.Get("/", h.GetStats)
	})
}

func (h *StatsHandler) GetStats(w http.ResponseWriter, r *http.Request) {
	stats, err := h.calculateStats(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

func (h *StatsHandler) calculateStats(ctx context.Context) (*StatsResponse, error) {
	stats := &StatsResponse{
		ByModel:    make(map[string]int),
		ByProvider: make(map[string]int),
	}

	now := time.Now()
	_24hAgo := now.Add(-24 * time.Hour)
	_7dAgo := now.Add(-7 * 24 * time.Hour)

	query := `
		SELECT COUNT(*) as count
		FROM request_logs
		WHERE created_at >= $1
	`

	var count int
	err := h.dbPool.QueryRow(ctx, query, _24hAgo).Scan(&count)
	if err != nil {
		return nil, err
	}
	stats.TotalRequestsLast24h = count

	query = `
		SELECT COUNT(*) as count
		FROM request_logs
		WHERE created_at >= $1
	`

	err = h.dbPool.QueryRow(ctx, query, _7dAgo).Scan(&count)
	if err != nil {
		return nil, err
	}
	stats.TotalRequestsLast7d = count

	query = `
		SELECT model_id, COUNT(*) as count
		FROM request_logs
		WHERE created_at >= $1
		GROUP BY model_id
		ORDER BY count DESC
		LIMIT 10
	`

	rows, err := h.dbPool.Query(ctx, query, _24hAgo)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var modelID string
		var count int
		if err := rows.Scan(&modelID, &count); err != nil {
			continue
		}
		stats.ByModel[modelID] = count
	}

	query = `
		SELECT p.name, COUNT(*) as count
		FROM request_logs rl
		JOIN providers p ON rl.provider_id = p.id
		WHERE rl.created_at >= $1
		GROUP BY p.name
		ORDER BY count DESC
		LIMIT 10
	`

	rows, err = h.dbPool.Query(ctx, query, _24hAgo)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var providerName string
		var count int
		if err := rows.Scan(&providerName, &count); err != nil {
			continue
		}
		stats.ByProvider[providerName] = count
	}

	query = `
		SELECT AVG(latency_ms) as avg_latency
		FROM request_logs
		WHERE created_at >= $1 AND status_code >= 200 AND status_code < 400
	`

	err = h.dbPool.QueryRow(ctx, query, _24hAgo).Scan(&stats.AvgLatencyMs)
	if err != nil {
		stats.AvgLatencyMs = 0
	}

	query = `
		SELECT
			COUNT(*) FILTER (WHERE status_code >= 400)::float / NULLIF(COUNT(*), 0) as error_rate
		FROM request_logs
		WHERE created_at >= $1
	`

	err = h.dbPool.QueryRow(ctx, query, _24hAgo).Scan(&stats.ErrorRate)
	if err != nil {
		stats.ErrorRate = 0
	}

	query = `
		SELECT SUM(tokens_prompt) as prompt_tokens, SUM(tokens_completion) as completion_tokens
		FROM request_logs
		WHERE created_at >= $1
	`

	err = h.dbPool.QueryRow(ctx, query, _24hAgo).Scan(&stats.TotalTokensPrompt, &stats.TotalTokensCompletion)
	if err != nil {
		stats.TotalTokensPrompt = 0
		stats.TotalTokensCompletion = 0
	}

	return stats, nil
}
