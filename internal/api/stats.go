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
	AvgTokensPerRequest  float64          `json:"avg_tokens_per_request"`
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

func parsePeriod(r *http.Request) time.Duration {
	p := r.URL.Query().Get("period")
	switch p {
	case "1h":
		return 1 * time.Hour
	case "7d":
		return 7 * 24 * time.Hour
	default:
		return 24 * time.Hour
	}
}

func (h *StatsHandler) GetStats(w http.ResponseWriter, r *http.Request) {
	period := parsePeriod(r)
	stats, err := h.calculateStats(r.Context(), period)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

func (h *StatsHandler) calculateStats(ctx context.Context, period time.Duration) (*StatsResponse, error) {
	stats := &StatsResponse{
		ByModel:      make(map[string]int),
		ByProvider:   make(map[string]int),
		ByVirtualKey: make(map[string]int64),
	}

	now := time.Now()
	since := now.Add(-period)

	switch period {
	case 7 * 24 * time.Hour:
		stats.TotalRequestsLast7d = 0
	default:
		stats.TotalRequestsLast24h = 0
	}

	query := `
		SELECT COUNT(*) as count
		FROM request_logs
		WHERE created_at >= $1
	`

	var count int
	err := h.dbPool.QueryRow(ctx, query, since).Scan(&count)
	if err != nil {
		return nil, err
	}

	switch period {
	case 7 * 24 * time.Hour:
		stats.TotalRequestsLast7d = count
	default:
		stats.TotalRequestsLast24h = count
	}

	if period == 24*time.Hour {
		_7dAgo := now.Add(-7 * 24 * time.Hour)
		err = h.dbPool.QueryRow(ctx, query, _7dAgo).Scan(&count)
		if err != nil {
			return nil, err
		}
		stats.TotalRequestsLast7d = count
	} else {
		_24hAgo := now.Add(-24 * time.Hour)
		err = h.dbPool.QueryRow(ctx, query, _24hAgo).Scan(&count)
		if err != nil {
			return nil, err
		}
		stats.TotalRequestsLast24h = count
	}

	query = `
		SELECT model_id, COUNT(*) as count
		FROM request_logs
		WHERE created_at >= $1
		GROUP BY model_id
		ORDER BY count DESC
		LIMIT 10
	`

	rows, err := h.dbPool.Query(ctx, query, since)
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

	rows, err = h.dbPool.Query(ctx, query, since)
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

	virtualKeyQuery := `
		SELECT rl.virtual_key_name, SUM(rl.tokens_prompt + rl.tokens_completion) as token_count
		FROM request_logs rl
		WHERE rl.created_at >= $1 AND rl.virtual_key_name IS NOT NULL AND rl.virtual_key_name != ''
		GROUP BY rl.virtual_key_name
		ORDER BY token_count DESC
		LIMIT 10
	`

	rows, err = h.dbPool.Query(ctx, virtualKeyQuery, since)
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

	err = h.dbPool.QueryRow(ctx, query, since).Scan(&stats.AvgLatencyMs)
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

	err = h.dbPool.QueryRow(ctx, query, since).Scan(&stats.ErrorRate)
	if err != nil {
		stats.ErrorRate = 0
	}

	query = `
		SELECT COALESCE(AVG(proxy_overhead_ms), 0) as avg_overhead
		FROM request_logs
		WHERE created_at >= $1 AND proxy_overhead_ms > 0
	`

	err = h.dbPool.QueryRow(ctx, query, since).Scan(&stats.AvgOverheadMs)
	if err != nil {
		stats.AvgOverheadMs = 0
	}

	query = `
		SELECT SUM(tokens_prompt) as prompt_tokens, SUM(tokens_completion) as completion_tokens
		FROM request_logs
		WHERE created_at >= $1
	`

	err = h.dbPool.QueryRow(ctx, query, since).Scan(&stats.TotalTokensPrompt, &stats.TotalTokensCompletion)
	if err != nil {
		stats.TotalTokensPrompt = 0
		stats.TotalTokensCompletion = 0
	}

	query = `
		SELECT COALESCE(
			SUM(tokens_prompt + tokens_completion)::float / NULLIF(COUNT(*), 0),
			0
		) as avg_tokens
		FROM request_logs
		WHERE created_at >= $1 AND status_code >= 200 AND status_code < 400
	`

	err = h.dbPool.QueryRow(ctx, query, since).Scan(&stats.AvgTokensPerRequest)
	if err != nil {
		stats.AvgTokensPerRequest = 0
	}

	return stats, nil
}

func (h *StatsHandler) GetTimeSeries(w http.ResponseWriter, r *http.Request) {
	period := parsePeriod(r)
	ctx := r.Context()
	now := time.Now().UTC()

	bucketSize := "hour"
	expectedBuckets := 24
	since := now.Add(-period).Truncate(time.Hour)

	if period >= 7*24*time.Hour {
		bucketSize = "day"
		expectedBuckets = 7
		since = now.Add(-period).Truncate(24 * time.Hour)
	}

	query := `
		SELECT
			to_char(date_trunc('` + bucketSize + `', created_at), 'YYYY-MM-DD"T"HH24:MI:SS') || 'Z' as bucket,
			COUNT(*) as count,
			COALESCE(SUM(tokens_prompt + tokens_completion), 0) as tokens,
			COUNT(*) FILTER (WHERE status_code >= 400) as errors,
			COALESCE(AVG(duration_ms) FILTER (WHERE status_code >= 200 AND status_code < 400), 0) as latency
		FROM request_logs
		WHERE created_at >= $1
		GROUP BY 1
		ORDER BY 1
	`

	rows, err := h.dbPool.Query(ctx, query, since)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	result := TimeSeriesStats{Points: make([]TimeSeriesPoint, 0, expectedBuckets)}
	for rows.Next() {
		var p TimeSeriesPoint
		var latency float64
		if err := rows.Scan(&p.Bucket, &p.Count, &p.Tokens, &p.Errors, &latency); err != nil {
			continue
		}
		p.Latency = latency
		result.Points = append(result.Points, p)
	}

	if len(result.Points) > 0 && len(result.Points) < expectedBuckets {
		result.Points = fillEmptyBuckets(result.Points, since, now.Truncate(time.Hour), bucketSize)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

func fillEmptyBuckets(points []TimeSeriesPoint, start, end time.Time, bucketSize string) []TimeSeriesPoint {
	byBucket := make(map[string]TimeSeriesPoint)
	for _, p := range points {
		byBucket[p.Bucket] = p
	}

	step := time.Hour
	expected := 24
	if bucketSize == "day" {
		step = 24 * time.Hour
		expected = 7
		end = end.Truncate(24 * time.Hour)
	}

	filled := make([]TimeSeriesPoint, 0, expected)
	for t := start; !t.After(end); t = t.Add(step) {
		bucket := t.Format("2006-01-02T15:04:05") + "Z"
		if p, ok := byBucket[bucket]; ok {
			filled = append(filled, p)
		} else {
			filled = append(filled, TimeSeriesPoint{Bucket: bucket, Count: 0, Tokens: 0, Errors: 0, Latency: 0})
		}
	}
	return filled
}

func (h *StatsHandler) GetProviderDistribution(w http.ResponseWriter, r *http.Request) {
	period := parsePeriod(r)
	ctx := r.Context()
	now := time.Now()
	since := now.Add(-period)

	query := `
		SELECT p.name, COUNT(*) as count
		FROM request_logs rl
		JOIN providers p ON rl.provider_id = p.id
		WHERE rl.created_at >= $1
		GROUP BY p.name
		ORDER BY count DESC
		LIMIT 5
	`

	rows, err := h.dbPool.Query(ctx, query, since)
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
