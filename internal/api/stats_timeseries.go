package api

import (
	"math"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
)

// GetTimeSeries returns time-series statistics with hourly or daily buckets.
func (h *StatsHandler) GetTimeSeries(w http.ResponseWriter, r *http.Request) {
	period := parsePeriod(r)
	excludeDeleted := parseExcludeDeleted(r)
	ctx := r.Context()
	now := time.Now().UTC()

	vkJoin, vkFilter := vkScope(excludeDeleted)
	ownerFrag, ownerArgs := ownerFilterFragment(logOwnerScope(r), 2)
	vkFilter += ownerFrag

	spec := timeSeriesBucket(period)
	since := now.Add(-spec.window).Truncate(spec.step)

	query := `
		SELECT
			to_char(` + spec.expr + `, 'YYYY-MM-DD"T"HH24:MI:SS') || 'Z' as bucket,
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

	rows, err := h.dbPool.Query(ctx, query, append([]any{since}, ownerArgs...)...)
	if err != nil {
		respondError(w, "failed to query time series", err, http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	result := TimeSeriesStats{Points: make([]TimeSeriesPoint, 0, spec.expected)}
	var p TimeSeriesPoint
	var latency, overheadMs, providerLatencyMs, avgTTFTMs float64
	var cacheHit, cacheMiss int
	if _, err := pgx.ForEachRow(rows, []any{&p.Bucket, &p.Count, &p.Tokens, &cacheHit, &cacheMiss, &p.Errors, &latency, &overheadMs, &providerLatencyMs, &p.RateLimitHits, &avgTTFTMs}, func() error {
		p.Latency = latency
		p.OverheadMs = overheadMs
		p.ProviderLatencyMs = providerLatencyMs
		p.AvgTTFTMs = avgTTFTMs
		p.TokensCacheHit = cacheHit
		p.TokensCacheMiss = cacheMiss
		result.Points = append(result.Points, p)
		return nil
	}); err != nil {
		// Missing buckets are synthesized as zeros below, so an interrupted
		// query must fail here rather than render as a quiet period.
		respondError(w, "failed to read time series", err, http.StatusInternalServerError)
		return
	}

	if len(result.Points) > 0 && len(result.Points) < spec.expected {
		// Fill up to the current time bucket so the chart always
		// shows the present, even with zero-count periods.
		result.Points = fillEmptyBuckets(result.Points, since, now.Truncate(spec.step), spec)
	}

	writeJSON(w, result)
}

// bucketSpec is one time-series granularity: the SQL expression that buckets
// created_at, the distance between buckets, how many the chart expects, and
// how far back the series reaches.
type bucketSpec struct {
	expr     string
	step     time.Duration
	expected int
	window   time.Duration
}

// timeSeriesBucket picks the granularity for a requested period: 5-minute
// buckets over a day, hourly over a week, daily over a month.
func timeSeriesBucket(period time.Duration) bucketSpec {
	switch {
	case period >= 7*24*time.Hour:
		return bucketSpec{"date_trunc('day', rl.created_at)", 24 * time.Hour, 30, 30 * 24 * time.Hour}
	case period >= 24*time.Hour:
		return bucketSpec{"date_trunc('hour', rl.created_at)", time.Hour, 168, 7 * 24 * time.Hour}
	default:
		return bucketSpec{"date_bin('5 minutes', rl.created_at, '2000-01-01')", 5 * time.Minute, 288, 24 * time.Hour}
	}
}

func fillEmptyBuckets(points []TimeSeriesPoint, start, end time.Time, spec bucketSpec) []TimeSeriesPoint {
	byBucket := make(map[string]TimeSeriesPoint)
	for _, p := range points {
		byBucket[p.Bucket] = p
	}

	filled := make([]TimeSeriesPoint, 0, spec.expected)
	for t := start; !t.After(end); t = t.Add(spec.step) {
		bucket := t.Format("2006-01-02T15:04:05") + "Z"
		if p, ok := byBucket[bucket]; ok {
			filled = append(filled, p)
		} else {
			filled = append(filled, TimeSeriesPoint{Bucket: bucket})
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

	vkJoin, vkFilter := vkScope(excludeDeleted)
	ownerFrag, ownerArgs := ownerFilterFragment(logOwnerScope(r), 2)
	vkFilter += ownerFrag

	selectCol := metricValueSelect(metric)
	havingClause := ""
	if metric == "tokens" {
		havingClause = " HAVING SUM(COALESCE(rl.tokens_prompt, 0) + COALESCE(rl.tokens_completion, 0)) > 0"
	}

	query := `
		SELECT p.name, ` + selectCol + `
		FROM request_logs rl
		JOIN providers p ON rl.provider_id = p.id` + vkJoin + `
		WHERE rl.created_at >= $1` + vkFilter + `
		GROUP BY p.name` + havingClause + `
		ORDER BY val DESC
		LIMIT 5`

	rows, err := h.dbPool.Query(ctx, query, append([]any{since}, ownerArgs...)...)
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
	var i item
	if _, err := pgx.ForEachRow(rows, []any{&i.Name, &i.Val}, func() error {
		total += i.Val
		items = append(items, i)
		return nil
	}); err != nil {
		respondError(w, "failed to read provider distribution", err, http.StatusInternalServerError)
		return
	}

	result := ProviderDistributionStats{Items: make([]ProviderDistributionItem, len(items))}
	rawShares := make([]float64, len(items))
	for i, it := range items {
		if total > 0 {
			rawShares[i] = float64(it.Val) / float64(total) * 100
		}
		item := ProviderDistributionItem{Name: it.Name, Share: math.Round(rawShares[i]*10) / 10}
		if metric == "tokens" {
			item.Tokens = it.Val
		} else {
			item.Count = it.Val
		}
		result.Items[i] = item
	}

	if len(result.Items) > 0 {
		var roundedSum float64
		for _, item := range result.Items {
			roundedSum += item.Share
		}
		result.Items[0].Share = math.Round((100-roundedSum+result.Items[0].Share)*10) / 10
	}

	writeJSON(w, result)
}
