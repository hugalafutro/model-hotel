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

	var vkJoin, vkFilter string
	if excludeDeleted {
		vkJoin = " LEFT JOIN virtual_keys vk ON rl.virtual_key_id = vk.id"
		vkFilter = " AND (rl.virtual_key_id IS NULL OR vk.id IS NOT NULL)"
	}
	ownerFrag, ownerArgs := ownerFilterFragment(logOwnerScope(r), 2)
	vkFilter += ownerFrag

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

	rows, err := h.dbPool.Query(ctx, query, append([]any{since}, ownerArgs...)...)
	if err != nil {
		respondError(w, "failed to query time series", err, http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	result := TimeSeriesStats{Points: make([]TimeSeriesPoint, 0, expectedBuckets)}
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

	writeJSON(w, result)
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
	ownerFrag, ownerArgs := ownerFilterFragment(logOwnerScope(r), 2)
	vkFilter += ownerFrag

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

	writeJSON(w, result)
}
