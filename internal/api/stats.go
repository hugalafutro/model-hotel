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
	TotalRequestsLast24h int              `json:"total_requests_last_24h"`
	TotalRequestsLast7d  int              `json:"total_requests_last_7d"`
	ByModel              map[string]int   `json:"by_model"`
	ByProvider           map[string]int   `json:"by_provider"`
	ByVirtualKey         map[string]int64 `json:"by_virtual_key"`
	AvgLatencyMs         float64          `json:"avg_latency_ms"`
	ErrorRate            float64          `json:"error_rate"`
	AvgOverheadMs        float64          `json:"avg_overhead_ms"`
	TotalTokensPrompt    int              `json:"total_tokens_prompt"`
	TotalTokensCompletion int             `json:"total_tokens_completion"`
}

// TimeSeriesPoint holds a single bucket of time-series data.
type TimeSeriesPoint struct {
	Bucket  string  `json:"bucket"`
	Count   int     `json:"count"`
	Tokens  int     `json:"tokens"`
	Errors  int     `json:"errors"`
	Latency float64 `json:"latency_ms"`
}

// TimeSeriesStats groups hourly aggregates returned by /api/stats/timeseries.
type TimeSeriesStats struct {
	Points []TimeSeriesPoint `json:"points"`
}

// ProviderDistributionItem holds a single slice of the provider breakdown.
type ProviderDistributionItem struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
	Share int    `json:"share"` // percent 0-100
}

// ProviderDistributionStats holds the provider share pie data.
type ProviderDistributionStats struct {
	Items []ProviderDistributionItem `json:"items"`
}

func (h *StatsHandler) Register(r chi.Router) {
	r.Route("/stats", func(r chi.Router) {
		r.Get("/", h.GetStats)
		r.Get("/timeseries", h.GetTimeSeries)
		r.Get("/provider-distribution", h.GetProviderDistribution)
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
		ByModel:       make(map[string]int),
		ByProvider:    make(map[string]int),
		ByVirtualKey:  make(map[string]int64),
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
		SELECT vk.name, vk.tokens_used
		FROM virtual_keys vk
		WHERE vk.tokens_used > 0
		ORDER BY vk.tokens_used DESC
		LIMIT 10
	`

	rows, err = h.dbPool.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var name string
		var tokens int64
		if err := rows.Scan(&name, &tokens); err != nil {
			continue
		}
		stats.ByVirtualKey[name] = tokens
	}

	query = `
		SELECT COALESCE(AVG(duration_ms), 0) as avg_duration
		FROM request_logs
		WHERE created_at >= $1 AND status_code >= 200 AND status_code < 400
	`

	err = h.dbPool.QueryRow(ctx, query, _24hAgo).Scan(&stats.AvgLatencyMs)
	if err != nil {
		stats.AvgLatencyMs = 0
	}

	query = `
		SELECT
			COALESCE(
				COUNT(*) FILTER (WHERE status_code >= 400)::float / NULLIF(COUNT(*), 0),
				0
			) as error_rate
		FROM request_logs
		WHERE created_at >= $1
	`

	err = h.dbPool.QueryRow(ctx, query, _24hAgo).Scan(&stats.ErrorRate)
	if err != nil {
		stats.ErrorRate = 0
	}

	query = `
		SELECT COALESCE(AVG(proxy_overhead_ms), 0) as avg_overhead
		FROM request_logs
		WHERE created_at >= $1 AND proxy_overhead_ms > 0
	`

	err = h.dbPool.QueryRow(ctx, query, _24hAgo).Scan(&stats.AvgOverheadMs)
	if err != nil {
		stats.AvgOverheadMs = 0
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

func (h *StatsHandler) GetTimeSeries(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	now := time.Now().UTC()
	_24hAgo := now.Add(-24 * time.Hour).Truncate(time.Hour)

	query := `
		SELECT
			date_trunc('hour', created_at)::text as bucket,
			COUNT(*) as count,
			COALESCE(SUM(tokens_prompt + tokens_completion), 0) as tokens,
			COUNT(*) FILTER (WHERE status_code >= 400) as errors,
			COALESCE(AVG(duration_ms) FILTER (WHERE status_code >= 200 AND status_code < 400), 0) as latency
		FROM request_logs
		WHERE created_at >= $1
		GROUP BY 1
		ORDER BY 1
	`

	rows, err := h.dbPool.Query(ctx, query, _24hAgo)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	result := TimeSeriesStats{Points: make([]TimeSeriesPoint, 0, 24)}
	for rows.Next() {
		var p TimeSeriesPoint
		var latency float64
		if err := rows.Scan(&p.Bucket, &p.Count, &p.Tokens, &p.Errors, &latency); err != nil {
			continue
		}
		p.Latency = latency
		result.Points = append(result.Points, p)
	}

	// Fill empty hours so the chart doesn't have gaps
	if len(result.Points) > 0 && len(result.Points) < 24 {
		result.Points = fillEmptyHours(result.Points, _24hAgo, now.Truncate(time.Hour))
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

func fillEmptyHours(points []TimeSeriesPoint, start, end time.Time) []TimeSeriesPoint {
	byBucket := make(map[string]TimeSeriesPoint)
	for _, p := range points {
		byBucket[p.Bucket] = p
	}
	filled := make([]TimeSeriesPoint, 0, 24)
	for h := start; !h.After(end); h = h.Add(time.Hour) {
		bucket := h.Format("2006-01-02T15:04:05") + "Z"
		if p, ok := byBucket[bucket]; ok {
			filled = append(filled, p)
		} else {
			filled = append(filled, TimeSeriesPoint{Bucket: bucket, Count: 0, Tokens: 0, Errors: 0, Latency: 0})
		}
	}
	return filled
}

func (h *StatsHandler) GetProviderDistribution(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	now := time.Now()
	_24hAgo := now.Add(-24 * time.Hour)

	query := `
		SELECT p.name, COUNT(*) as count
		FROM request_logs rl
		JOIN providers p ON rl.provider_id = p.id
		WHERE rl.created_at >= $1
		GROUP BY p.name
		ORDER BY count DESC
		LIMIT 5
	`

	rows, err := h.dbPool.Query(ctx, query, _24hAgo)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	total := 0
	type item struct {
		Name  string
		Count int
	}
	var items []item
	for rows.Next() {
		var i item
		if err := rows.Scan(&i.Name, &i.Count); err != nil {
			continue
		}
		items = append(items, i)
		total += i.Count
	}

	result := ProviderDistributionStats{Items: make([]ProviderDistributionItem, len(items))}
	for i, it := range items {
		var share int
		if total > 0 {
			share = int(float64(it.Count) / float64(total) * 100)
		}
		result.Items[i] = ProviderDistributionItem{
			Name:  it.Name,
			Count: it.Count,
			Share: share,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}
