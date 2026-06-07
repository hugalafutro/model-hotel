package api

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

// StatsHandler provides statistics and analytics API endpoints.
type StatsHandler struct {
	dbPool   *pgxpool.Pool
	adminMgr interface {
		Validate(token string) bool
	}
}

// NewStatsHandler creates a new statistics handler.
func NewStatsHandler(dbPool *pgxpool.Pool, adminMgr interface {
	Validate(token string) bool
}) *StatsHandler {
	return &StatsHandler{
		dbPool:   dbPool,
		adminMgr: adminMgr,
	}
}

// ModelLatencyEntry holds per-model latency breakdown for the dashboard.
type ModelLatencyEntry struct {
	ModelID      string  `json:"model_id"`
	TotalMs      float64 `json:"total_ms"`
	OverheadMs   float64 `json:"overhead_ms"`
	ProviderMs   float64 `json:"provider_ms"`
	RequestCount int     `json:"request_count"`
}

// ProviderLatencyEntry holds per-provider latency breakdown for the dashboard.
type ProviderLatencyEntry struct {
	ProviderName string  `json:"provider_name"`
	TotalMs      float64 `json:"total_ms"`
	OverheadMs   float64 `json:"overhead_ms"`
	ProviderMs   float64 `json:"provider_ms"`
	RequestCount int     `json:"request_count"`
}

// StatsResponse contains aggregated statistics for the dashboard.
type StatsResponse struct {
	TotalRequestsLast24h  int                    `json:"total_requests_last_24h"`
	TotalRequestsLast7d   int                    `json:"total_requests_last_7d"`
	ByModel               map[string]int64       `json:"by_model"`
	ByProvider            map[string]int64       `json:"by_provider"`
	ByVirtualKey          map[string]int64       `json:"by_virtual_key"`
	AvgLatencyMs          float64                `json:"avg_latency_ms"`
	ErrorRate             float64                `json:"error_rate"`
	AvgOverheadMs         float64                `json:"avg_overhead_ms"`
	TotalTokensPrompt     int                    `json:"total_tokens_prompt"`
	TotalTokensCompletion int                    `json:"total_tokens_completion"`
	TotalTokensCacheHit   int                    `json:"total_tokens_cache_hit"`
	AvgTokensPerRequest   float64                `json:"avg_tokens_per_request"`
	RateLimitHits         int                    `json:"rate_limit_hits"`
	AvgTTFTMs             float64                `json:"avg_ttft_ms"`
	RequestsLast1h        int                    `json:"requests_last_1h"`
	ByModelLatency        []ModelLatencyEntry    `json:"by_model_latency"`
	ByProviderLatency     []ProviderLatencyEntry `json:"by_provider_latency"`
}

// TimeSeriesPoint holds a single bucket of time-series data.
type TimeSeriesPoint struct {
	Bucket            string  `json:"bucket"`
	Count             int     `json:"count"`
	Tokens            int     `json:"tokens"`
	TokensCacheHit    int     `json:"tokens_cache_hit"`
	TokensCacheMiss   int     `json:"tokens_cache_miss"`
	Errors            int     `json:"errors"`
	Latency           float64 `json:"latency_ms"`
	OverheadMs        float64 `json:"overhead_ms"`
	ProviderLatencyMs float64 `json:"provider_latency_ms"`
	RateLimitHits     int     `json:"rate_limit_hits"`
	AvgTTFTMs         float64 `json:"avg_ttft_ms"`
}

// TimeSeriesStats groups hourly aggregates returned by /api/stats/timeseries.
type TimeSeriesStats struct {
	Points []TimeSeriesPoint `json:"points"`
}

// ProviderDistributionItem holds a single slice of the provider breakdown.
type ProviderDistributionItem struct {
	Name   string  `json:"name"`
	Count  int     `json:"count"`
	Tokens int     `json:"tokens"`
	Share  float64 `json:"share"`
}

// ProviderDistributionStats holds the provider share pie data.
type ProviderDistributionStats struct {
	Items []ProviderDistributionItem `json:"items"`
}

// Register mounts statistics API routes.
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

func parseExcludeDeleted(r *http.Request) bool {
	return r.URL.Query().Get("exclude_deleted") == "true"
}

func parseMetric(r *http.Request) string {
	m := r.URL.Query().Get("metric")
	if m == "tokens" {
		return "tokens"
	}
	return "requests"
}

func parseIncludeLatency(r *http.Request) bool {
	return r.URL.Query().Get("include_latency") == "true"
}

// vkScope returns the LEFT JOIN and WHERE fragments that restrict a stats query
// to non-deleted virtual keys (rows whose virtual_key_id still resolves, or is
// NULL). Both are empty when excludeDeleted is false. Single source of truth for
// the fragment pasted into nearly every stats query.
func vkScope(excludeDeleted bool) (join, filter string) {
	if excludeDeleted {
		return " LEFT JOIN virtual_keys vk ON rl.virtual_key_id = vk.id",
			" AND (rl.virtual_key_id IS NULL OR vk.id IS NOT NULL)"
	}
	return "", ""
}

// metricValueSelect returns the aggregate column expression (aliased "val") for
// the requested metric: summed tokens vs request count. Single source of truth
// for the SELECT used by the by-model/provider/virtual-key breakdowns.
func metricValueSelect(metric string) string {
	if metric == "tokens" {
		return "SUM(COALESCE(rl.tokens_prompt, 0) + COALESCE(rl.tokens_completion, 0)) as val"
	}
	return "COUNT(*) as val"
}

// GetStats returns aggregated statistics for the specified period.
func (h *StatsHandler) GetStats(w http.ResponseWriter, r *http.Request) {
	period := parsePeriod(r)
	excludeDeleted := parseExcludeDeleted(r)
	metric := parseMetric(r)
	includeLatency := parseIncludeLatency(r)
	stats, err := h.calculateStats(r.Context(), period, excludeDeleted, metric, includeLatency)
	if err != nil {
		respondError(w, "failed to calculate stats", err, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(stats); err != nil {
		respondError(w, "failed to encode response", err, http.StatusInternalServerError)
	}
}

// statByModel fills stats.ByModel with the top-10 models by the requested
// metric (Q2). A query failure is fatal (returned); a per-row scan error skips
// the row, matching the original loop.
func (h *StatsHandler) statByModel(ctx context.Context, stats *StatsResponse, vkJoin, vkFilter, metric string, since time.Time) error {
	query := `
		SELECT
			CASE
				WHEN rl.model_id LIKE '%/%' THEN rl.model_id
				WHEN p.name IS NOT NULL AND p.name != '' THEN p.name || '/' || rl.model_id
				ELSE rl.model_id
			END as model_id,
			` + metricValueSelect(metric) + `
		FROM request_logs rl
		LEFT JOIN providers p ON rl.provider_id = p.id` + vkJoin + `
		WHERE rl.created_at >= $1` + vkFilter + `
		GROUP BY
			CASE
				WHEN rl.model_id LIKE '%/%' THEN rl.model_id
				WHEN p.name IS NOT NULL AND p.name != '' THEN p.name || '/' || rl.model_id
				ELSE rl.model_id
			END
		ORDER BY val DESC
		LIMIT 10`

	rows, err := h.dbPool.Query(ctx, query, since)
	if err != nil {
		debuglog.Error("stats: query failed", "query", "by_model", "error", err)
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var modelID string
		var val int64
		if err := rows.Scan(&modelID, &val); err != nil {
			continue
		}
		stats.ByModel[modelID] = val
	}
	return nil
}

// statByProvider fills stats.ByProvider with the top-10 providers by the
// requested metric (Q3). Query failure fatal; per-row scan error skips the row.
func (h *StatsHandler) statByProvider(ctx context.Context, stats *StatsResponse, vkJoin, vkFilter, metric string, since time.Time) error {
	query := `
		SELECT p.name, ` + metricValueSelect(metric) + `
		FROM request_logs rl
		JOIN providers p ON rl.provider_id = p.id` + vkJoin + `
		WHERE rl.created_at >= $1` + vkFilter + `
		GROUP BY p.name
		ORDER BY val DESC
		LIMIT 10`

	rows, err := h.dbPool.Query(ctx, query, since)
	if err != nil {
		debuglog.Error("stats: query failed", "query", "by_provider", "error", err)
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var providerName string
		var val int64
		if err := rows.Scan(&providerName, &val); err != nil {
			continue
		}
		stats.ByProvider[providerName] = val
	}
	return nil
}

// statByVirtualKey fills stats.ByVirtualKey from live virtual keys (Q4), plus
// the deleted-key aggregate under the "Deleted" key (Q4b, only when not
// excluding deleted keys) and the chat/arena admin routes keyed by
// virtual_key_name (Q4c). The main query failure is fatal; the two aggregates
// are best-effort (logged via their nil-error guards, never abort).
func (h *StatsHandler) statByVirtualKey(ctx context.Context, stats *StatsResponse, metric string, since time.Time, excludeDeleted bool) error {
	virtualKeyQuery := `
		SELECT vk.name, ` + metricValueSelect(metric) + `
		FROM request_logs rl
		JOIN virtual_keys vk ON rl.virtual_key_id = vk.id
		WHERE rl.created_at >= $1
		GROUP BY vk.name
		ORDER BY val DESC
		LIMIT 10`

	rows, err := h.dbPool.Query(ctx, virtualKeyQuery, since)
	if err != nil {
		debuglog.Error("stats: query failed", "query", "by_virtual_key", "error", err)
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var name string
		var val int64
		if err := rows.Scan(&name, &val); err != nil {
			continue
		}
		stats.ByVirtualKey[name] = val
	}

	// Query 4b: Deleted virtual keys aggregate — only when not excluding deleted keys
	if !excludeDeleted {
		deletedKeyQuery := `
			SELECT ` + metricValueSelect(metric) + `
			FROM request_logs rl
			WHERE rl.created_at >= $1
			  AND rl.virtual_key_id IS NOT NULL
			  AND NOT EXISTS (SELECT 1 FROM virtual_keys vk WHERE vk.id = rl.virtual_key_id)`

		var deletedVal int64
		err = h.dbPool.QueryRow(ctx, deletedKeyQuery, since).Scan(&deletedVal)
		if err == nil && deletedVal > 0 {
			stats.ByVirtualKey["Deleted"] = deletedVal
		}
	}

	// Query 4c: Chat and Arena -- stored via virtual_key_name for admin chat/arena routes
	for _, keyName := range []string{"chat", "arena"} {
		var val int64
		if metric == "tokens" {
			err = h.dbPool.QueryRow(ctx, `
				SELECT SUM(COALESCE(rl.tokens_prompt, 0) + COALESCE(rl.tokens_completion, 0))
				FROM request_logs rl
				WHERE rl.created_at >= $1 AND rl.virtual_key_name = $2`,
				since, keyName).Scan(&val)
		} else {
			err = h.dbPool.QueryRow(ctx, `
				SELECT COUNT(*)
				FROM request_logs rl
				WHERE rl.created_at >= $1 AND rl.virtual_key_name = $2`,
				since, keyName).Scan(&val)
		}
		if err == nil && val > 0 {
			stats.ByVirtualKey[keyName] = val
		}
	}
	return nil
}

func (h *StatsHandler) calculateStats(ctx context.Context, period time.Duration, excludeDeleted bool, metric string, includeLatency bool) (*StatsResponse, error) {
	stats := &StatsResponse{
		ByModel:      make(map[string]int64),
		ByProvider:   make(map[string]int64),
		ByVirtualKey: make(map[string]int64),
	}

	vkJoin, vkFilter := vkScope(excludeDeleted)

	now := time.Now().UTC()
	since := now.Add(-period)

	switch period {
	case 7 * 24 * time.Hour:
		stats.TotalRequestsLast7d = 0
	default:
		stats.TotalRequestsLast24h = 0
	}

	// Query 1: Total request count
	query := `
		SELECT COUNT(*) as count
		FROM request_logs rl` + vkJoin + `
		WHERE rl.created_at >= $1` + vkFilter

	var count int
	err := h.dbPool.QueryRow(ctx, query, since).Scan(&count)
	if err != nil {
		debuglog.Error("stats: query failed", "query", "total_requests", "error", err)
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
			debuglog.Error("stats: query failed", "query", "total_requests_7d", "error", err)
			return nil, err
		}
		stats.TotalRequestsLast7d = count
	} else {
		_24hAgo := now.Add(-24 * time.Hour)
		err = h.dbPool.QueryRow(ctx, query, _24hAgo).Scan(&count)
		if err != nil {
			debuglog.Error("stats: query failed", "query", "total_requests_24h", "error", err)
			return nil, err
		}
		stats.TotalRequestsLast24h = count
	}

	// Queries 2–4c: dimension breakdowns (top-10 by model / provider / virtual key).
	if err := h.statByModel(ctx, stats, vkJoin, vkFilter, metric, since); err != nil {
		return nil, err
	}
	if err := h.statByProvider(ctx, stats, vkJoin, vkFilter, metric, since); err != nil {
		return nil, err
	}
	if err := h.statByVirtualKey(ctx, stats, metric, since, excludeDeleted); err != nil {
		return nil, err
	}

	// Query 5: Avg latency
	query = `
		SELECT COALESCE(AVG(rl.duration_ms), 0) as avg_duration
		FROM request_logs rl` + vkJoin + `
		WHERE rl.created_at >= $1 AND rl.status_code > 0 AND rl.status_code < 400` + vkFilter

	err = h.dbPool.QueryRow(ctx, query, since).Scan(&stats.AvgLatencyMs)
	if err != nil {
		debuglog.Error("stats: query failed", "query", "avg_latency", "error", err)
		stats.AvgLatencyMs = 0
	}

	// Query 6: Error rate
	query = `
		SELECT
			COALESCE(
				COUNT(*) FILTER (WHERE rl.status_code >= 400 OR rl.status_code = 0)::float / NULLIF(COUNT(*), 0),
				0
			) as error_rate
		FROM request_logs rl` + vkJoin + `
		WHERE rl.created_at >= $1` + vkFilter

	err = h.dbPool.QueryRow(ctx, query, since).Scan(&stats.ErrorRate)
	if err != nil {
		debuglog.Error("stats: query failed", "query", "error_rate", "error", err)
		stats.ErrorRate = 0
	}

	// Query 7: Avg overhead
	query = `
		SELECT COALESCE(AVG(rl.proxy_overhead_ms), 0) as avg_overhead
		FROM request_logs rl` + vkJoin + `
		WHERE rl.created_at >= $1 AND rl.proxy_overhead_ms > 0` + vkFilter

	err = h.dbPool.QueryRow(ctx, query, since).Scan(&stats.AvgOverheadMs)
	if err != nil {
		debuglog.Error("stats: query failed", "query", "avg_overhead", "error", err)
		stats.AvgOverheadMs = 0
	}

	// Query 8: Total tokens
	query = `
		SELECT COALESCE(SUM(rl.tokens_prompt), 0) as prompt_tokens, COALESCE(SUM(rl.tokens_completion), 0) as completion_tokens, COALESCE(SUM(rl.tokens_prompt_cache_hit), 0) as cache_hit_tokens
		FROM request_logs rl` + vkJoin + `
		WHERE rl.created_at >= $1` + vkFilter

	err = h.dbPool.QueryRow(ctx, query, since).Scan(&stats.TotalTokensPrompt, &stats.TotalTokensCompletion, &stats.TotalTokensCacheHit)
	if err != nil {
		debuglog.Error("stats: query failed", "query", "total_tokens", "error", err)
		stats.TotalTokensPrompt = 0
		stats.TotalTokensCompletion = 0
		stats.TotalTokensCacheHit = 0
	}

	// Query 9: Avg tokens per request
	query = `
		SELECT COALESCE(
			SUM(COALESCE(rl.tokens_prompt, 0) + COALESCE(rl.tokens_completion, 0))::float / NULLIF(COUNT(*), 0),
			0
		) as avg_tokens
		FROM request_logs rl` + vkJoin + `
		WHERE rl.created_at >= $1 AND rl.status_code > 0 AND rl.status_code < 400` + vkFilter

	err = h.dbPool.QueryRow(ctx, query, since).Scan(&stats.AvgTokensPerRequest)
	if err != nil {
		debuglog.Error("stats: query failed", "query", "avg_tokens_per_request", "error", err)
		stats.AvgTokensPerRequest = 0
	}

	// Query 10: Rate limit hits (429 count)
	query = `
		SELECT COUNT(*) FILTER (WHERE rl.status_code = 429)
		FROM request_logs rl` + vkJoin + `
		WHERE rl.created_at >= $1` + vkFilter

	err = h.dbPool.QueryRow(ctx, query, since).Scan(&stats.RateLimitHits)
	if err != nil {
		debuglog.Error("stats: query failed", "query", "rate_limit_hits", "error", err)
		stats.RateLimitHits = 0
	}

	// Query 11: Avg TTFT (streaming only — non-streaming requests have no first token)
	query = `
		SELECT COALESCE(AVG(COALESCE(NULLIF(rl.ttft_ms, 0), rl.response_header_ms)) FILTER (WHERE COALESCE(NULLIF(rl.ttft_ms, 0), rl.response_header_ms) > 0 AND rl.streaming = true), 0) as avg_ttft
		FROM request_logs rl` + vkJoin + `
		WHERE rl.created_at >= $1 AND rl.status_code > 0 AND rl.status_code < 400` + vkFilter

	err = h.dbPool.QueryRow(ctx, query, since).Scan(&stats.AvgTTFTMs)
	if err != nil {
		debuglog.Error("stats: query failed", "query", "avg_ttft", "error", err)
		stats.AvgTTFTMs = 0
	}

	// Requests in last 1h — always query fresh, regardless of period,
	// because the `count` variable may have been overwritten by earlier queries.
	_1hAgo := now.Add(-1 * time.Hour)
	var requests1h int
	err = h.dbPool.QueryRow(ctx, `
		SELECT COUNT(*) as count
		FROM request_logs rl`+vkJoin+`
		WHERE rl.created_at >= $1`+vkFilter, _1hAgo).Scan(&requests1h)
	if err != nil {
		debuglog.Error("stats: query failed", "query", "requests_last_1h", "error", err)
		requests1h = 0
	}
	stats.RequestsLast1h = requests1h

	// Query 12: Per-model latency breakdown (top 5 by avg total latency).
	// Only runs when the caller requests it to avoid unnecessary work
	// on stats calls from non-latency dashboard panels.
	if includeLatency {
		query = `
			WITH model_latency AS (
				SELECT
					CASE
						WHEN rl.model_id LIKE '%/%' THEN rl.model_id
						WHEN p.name IS NOT NULL AND p.name != '' THEN p.name || '/' || rl.model_id
						ELSE rl.model_id
					END as model_id,
					COUNT(*) as req_count,
					COALESCE(AVG(rl.duration_ms), 0) as avg_total,
					COALESCE(AVG(COALESCE(rl.proxy_overhead_ms, 0)), 0) as avg_overhead
				FROM request_logs rl
				LEFT JOIN providers p ON rl.provider_id = p.id` + vkJoin + `
				WHERE rl.created_at >= $1 AND rl.status_code > 0 AND rl.status_code < 400` + vkFilter + `
				GROUP BY 1
				HAVING COUNT(*) >= 3
				ORDER BY avg_total DESC
				LIMIT 6
			)
			SELECT model_id, req_count, avg_total, avg_overhead,
				GREATEST(0, avg_total - avg_overhead) as avg_provider
			FROM model_latency`

		rows, err := h.dbPool.Query(ctx, query, since)
		if err != nil {
			debuglog.Error("stats: query failed", "query", "by_model_latency", "error", err)
		} else {
			defer rows.Close()
			for rows.Next() {
				var entry ModelLatencyEntry
				if err := rows.Scan(&entry.ModelID, &entry.RequestCount, &entry.TotalMs, &entry.OverheadMs, &entry.ProviderMs); err != nil {
					continue
				}
				stats.ByModelLatency = append(stats.ByModelLatency, entry)
			}
		}

		// Query 13: Per-provider latency breakdown (top 6 by avg total latency).
		query = `
			WITH provider_latency AS (
				SELECT
					p.name as provider_name,
					COUNT(*) as req_count,
					COALESCE(AVG(rl.duration_ms), 0) as avg_total,
					COALESCE(AVG(COALESCE(rl.proxy_overhead_ms, 0)), 0) as avg_overhead
				FROM request_logs rl
				INNER JOIN providers p ON rl.provider_id = p.id` + vkJoin + `
				WHERE rl.created_at >= $1 AND rl.status_code > 0 AND rl.status_code < 400` + vkFilter + `
				GROUP BY p.name
				HAVING COUNT(*) >= 3
				ORDER BY avg_total DESC
				LIMIT 6
			)
			SELECT provider_name, req_count, avg_total, avg_overhead,
				GREATEST(0, avg_total - avg_overhead) as avg_provider
			FROM provider_latency`

		rows, err = h.dbPool.Query(ctx, query, since)
		if err != nil {
			debuglog.Error("stats: query failed", "query", "by_provider_latency", "error", err)
		} else {
			defer rows.Close()
			for rows.Next() {
				var entry ProviderLatencyEntry
				if err := rows.Scan(&entry.ProviderName, &entry.RequestCount, &entry.TotalMs, &entry.OverheadMs, &entry.ProviderMs); err != nil {
					continue
				}
				stats.ByProviderLatency = append(stats.ByProviderLatency, entry)
			}
		}
	}

	return stats, nil
}

// GetTimeSeries returns time-series statistics with hourly or daily buckets.
func (h *StatsHandler) GetTimeSeries(w http.ResponseWriter, r *http.Request) {
	period := parsePeriod(r)
	excludeDeleted := parseExcludeDeleted(r)
	ctx := r.Context()
	now := time.Now().UTC()

	var vkJoin, vkFilter string
	if excludeDeleted {
		vkJoin = " LEFT JOIN virtual_keys vk ON rl.virtual_key_id = vk.id"
		vkFilter = " AND (rl.virtual_key_id IS NULL OR vk.id IS NOT NULL)"
	}

	bucketSize := "5min"
	expectedBuckets := 288
	since := now.Add(-24 * time.Hour).Truncate(5 * time.Minute)

	if period >= 24*time.Hour {
		bucketSize = "hour"
		expectedBuckets = 168
		since = now.Add(-7 * 24 * time.Hour).Truncate(time.Hour)
	}

	if period >= 7*24*time.Hour {
		bucketSize = "day"
		expectedBuckets = 30
		since = now.Add(-30 * 24 * time.Hour).Truncate(24 * time.Hour)
	}

	query := ""
	switch bucketSize {
	case "5min":
		query = `
		SELECT
			to_char(date_bin('5 minutes', rl.created_at, '2000-01-01'), 'YYYY-MM-DD"T"HH24:MI:SS') || 'Z' as bucket,
			COUNT(*) as count,
			SUM(COALESCE(rl.tokens_prompt, 0) + COALESCE(rl.tokens_completion, 0)) as tokens,
			SUM(COALESCE(rl.tokens_prompt_cache_hit, 0)) as tokens_cache_hit,
			SUM(COALESCE(rl.tokens_prompt_cache_miss, 0)) as tokens_cache_miss,
			COUNT(*) FILTER (WHERE rl.status_code >= 400 OR rl.status_code = 0) as errors,
			COALESCE(AVG(rl.duration_ms) FILTER (WHERE rl.status_code > 0 AND rl.status_code < 400), 0) as latency,
			COALESCE(AVG(COALESCE(rl.proxy_overhead_ms, 0)) FILTER (WHERE rl.status_code > 0 AND rl.status_code < 400), 0) as overhead_ms,
			COALESCE(AVG(rl.latency_ms) FILTER (WHERE rl.status_code > 0 AND rl.status_code < 400), 0) as provider_latency_ms,
			COUNT(*) FILTER (WHERE rl.status_code = 429) as rate_limit_hits,
			COALESCE(AVG(COALESCE(NULLIF(rl.ttft_ms, 0), rl.response_header_ms)) FILTER (WHERE COALESCE(NULLIF(rl.ttft_ms, 0), rl.response_header_ms) > 0 AND rl.status_code > 0 AND rl.status_code < 400 AND rl.streaming = true), 0) as avg_ttft_ms
		FROM request_logs rl` + vkJoin + `
		WHERE rl.created_at >= $1` + vkFilter + `
		GROUP BY 1
		ORDER BY 1`
	default:
		query = `
		SELECT
			to_char(date_trunc('` + bucketSize + `', rl.created_at), 'YYYY-MM-DD"T"HH24:MI:SS') || 'Z' as bucket,
			COUNT(*) as count,
			SUM(COALESCE(rl.tokens_prompt, 0) + COALESCE(rl.tokens_completion, 0)) as tokens,
			SUM(COALESCE(rl.tokens_prompt_cache_hit, 0)) as tokens_cache_hit,
			SUM(COALESCE(rl.tokens_prompt_cache_miss, 0)) as tokens_cache_miss,
			COUNT(*) FILTER (WHERE rl.status_code >= 400 OR rl.status_code = 0) as errors,
			COALESCE(AVG(rl.duration_ms) FILTER (WHERE rl.status_code > 0 AND rl.status_code < 400), 0) as latency,
			COALESCE(AVG(COALESCE(rl.proxy_overhead_ms, 0)) FILTER (WHERE rl.status_code > 0 AND rl.status_code < 400), 0) as overhead_ms,
			COALESCE(AVG(rl.latency_ms) FILTER (WHERE rl.status_code > 0 AND rl.status_code < 400), 0) as provider_latency_ms,
			COUNT(*) FILTER (WHERE rl.status_code = 429) as rate_limit_hits,
			COALESCE(AVG(COALESCE(NULLIF(rl.ttft_ms, 0), rl.response_header_ms)) FILTER (WHERE COALESCE(NULLIF(rl.ttft_ms, 0), rl.response_header_ms) > 0 AND rl.status_code > 0 AND rl.status_code < 400 AND rl.streaming = true), 0) as avg_ttft_ms
		FROM request_logs rl` + vkJoin + `
		WHERE rl.created_at >= $1` + vkFilter + `
		GROUP BY 1
		ORDER BY 1`
	}

	rows, err := h.dbPool.Query(ctx, query, since)
	if err != nil {
		respondError(w, "failed to query time series", err, http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	result := TimeSeriesStats{Points: make([]TimeSeriesPoint, 0, expectedBuckets)}
	for rows.Next() {
		var p TimeSeriesPoint
		var latency, overheadMs, providerLatencyMs, avgTTFTMs float64
		var cacheHit, cacheMiss int
		if err := rows.Scan(&p.Bucket, &p.Count, &p.Tokens, &cacheHit, &cacheMiss, &p.Errors, &latency, &overheadMs, &providerLatencyMs, &p.RateLimitHits, &avgTTFTMs); err != nil {
			continue
		}
		p.Latency = latency
		p.OverheadMs = overheadMs
		p.ProviderLatencyMs = providerLatencyMs
		p.AvgTTFTMs = avgTTFTMs
		p.TokensCacheHit = cacheHit
		p.TokensCacheMiss = cacheMiss
		result.Points = append(result.Points, p)
	}

	if len(result.Points) > 0 && len(result.Points) < expectedBuckets {
		// Fill up to the current time bucket so the chart always
		// shows the present, even with zero-count periods.
		var endTrunc time.Time
		switch bucketSize {
		case "5min":
			endTrunc = now.Truncate(5 * time.Minute)
		case "day":
			endTrunc = now.Truncate(24 * time.Hour)
		default:
			endTrunc = now.Truncate(time.Hour)
		}
		result.Points = fillEmptyBuckets(result.Points, since, endTrunc, bucketSize)
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(result); err != nil {
		respondError(w, "failed to encode response", err, http.StatusInternalServerError)
	}
}

func fillEmptyBuckets(points []TimeSeriesPoint, start, end time.Time, bucketSize string) []TimeSeriesPoint {
	byBucket := make(map[string]TimeSeriesPoint)
	for _, p := range points {
		byBucket[p.Bucket] = p
	}

	var step time.Duration
	var expected int
	switch bucketSize {
	case "5min":
		step = 5 * time.Minute
		expected = 288
	case "day":
		step = 24 * time.Hour
		expected = 30
	default: // "hour"
		step = time.Hour
		expected = 168
	}

	filled := make([]TimeSeriesPoint, 0, expected)
	for t := start; !t.After(end); t = t.Add(step) {
		bucket := t.Format("2006-01-02T15:04:05") + "Z"
		if p, ok := byBucket[bucket]; ok {
			filled = append(filled, p)
		} else {
			filled = append(filled, TimeSeriesPoint{Bucket: bucket, Count: 0, Tokens: 0, TokensCacheHit: 0, TokensCacheMiss: 0, Errors: 0, Latency: 0, RateLimitHits: 0, AvgTTFTMs: 0})
		}
	}
	return filled
}

// GetProviderDistribution returns request/token distribution by provider.
func (h *StatsHandler) GetProviderDistribution(w http.ResponseWriter, r *http.Request) {
	period := parsePeriod(r)
	excludeDeleted := parseExcludeDeleted(r)
	metric := parseMetric(r)
	ctx := r.Context()
	now := time.Now().UTC()
	since := now.Add(-period)

	var vkJoin, vkFilter string
	if excludeDeleted {
		vkJoin = " LEFT JOIN virtual_keys vk ON rl.virtual_key_id = vk.id"
		vkFilter = " AND (rl.virtual_key_id IS NULL OR vk.id IS NOT NULL)"
	}

	var selectCol string
	var havingClause string
	if metric == "tokens" {
		selectCol = "SUM(COALESCE(rl.tokens_prompt, 0) + COALESCE(rl.tokens_completion, 0)) as val"
		havingClause = " HAVING SUM(COALESCE(rl.tokens_prompt, 0) + COALESCE(rl.tokens_completion, 0)) > 0"
	} else {
		selectCol = "COUNT(*) as val"
	}

	query := `
		SELECT p.name, ` + selectCol + `
		FROM request_logs rl
		JOIN providers p ON rl.provider_id = p.id` + vkJoin + `
		WHERE rl.created_at >= $1` + vkFilter + `
		GROUP BY p.name` + havingClause + `
		ORDER BY val DESC
		LIMIT 5`

	rows, err := h.dbPool.Query(ctx, query, since)
	if err != nil {
		respondError(w, "failed to query provider distribution", err, http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type item struct {
		Name string
		Val  int
	}
	var items []item
	total := 0
	for rows.Next() {
		var i item
		if err := rows.Scan(&i.Name, &i.Val); err != nil {
			continue
		}
		total += i.Val
		items = append(items, i)
	}

	result := ProviderDistributionStats{Items: make([]ProviderDistributionItem, len(items))}
	rawShares := make([]float64, len(items))
	for i, it := range items {
		if total > 0 {
			rawShares[i] = float64(it.Val) / float64(total) * 100
		}
		result.Items[i] = ProviderDistributionItem{
			Name:   it.Name,
			Count:  it.Val,
			Tokens: it.Val,
		}
		if metric != "tokens" {
			result.Items[i].Tokens = 0
		} else {
			result.Items[i].Count = 0
		}
	}

	// Round each share to 1 decimal place, then adjust the largest item
	// to compensate for accumulated rounding error so total == 100.0.
	for i := range result.Items {
		result.Items[i].Share = math.Round(rawShares[i]*10) / 10
	}
	if len(result.Items) > 0 {
		var roundedSum float64
		for _, item := range result.Items {
			roundedSum += item.Share
		}
		result.Items[0].Share = math.Round((100-roundedSum+result.Items[0].Share)*10) / 10
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(result); err != nil {
		respondError(w, "failed to encode response", err, http.StatusInternalServerError)
	}
}
