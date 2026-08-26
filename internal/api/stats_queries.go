package api

import (
	"context"
	"time"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

// modelKeySQL is the aggregation key for per-model stats: the "Provider/model"
// form the dashboard matches against its model catalog. request_logs.model_id
// holds the model-only component for direct requests — which may itself
// contain slashes (HF-style IDs like "zai-org/glm-5.2"), so its shape says
// nothing about whether the provider prefix is present — and the full
// "hotel/<group>" for failover requests. Rows without a resolved provider
// (validation failures, deleted providers) keep their raw value.
const modelKeySQL = `
			CASE
				WHEN rl.model_id LIKE 'hotel/%' THEN rl.model_id
				WHEN p.name IS NOT NULL AND p.name != '' THEN p.name || '/' || rl.model_id
				ELSE rl.model_id
			END`

// statByModel fills stats.ByModel with the top-10 models by the requested
// metric (Q2). A query failure is fatal (returned); a per-row scan error skips
// the row, matching the original loop.
func (h *StatsHandler) statByModel(ctx context.Context, stats *StatsResponse, vkJoin, vkFilter string, filterArgs []any, metric string, since time.Time) error {
	query := `
		SELECT
			` + modelKeySQL + ` as model_id,
			` + metricValueSelect(metric) + `
		FROM request_logs rl
		LEFT JOIN providers p ON rl.provider_id = p.id` + vkJoin + `
		WHERE rl.created_at >= $1` + vkFilter + `
		GROUP BY 1
		ORDER BY val DESC
		LIMIT 10`

	rows, err := h.dbPool.Query(ctx, query, append([]any{since}, filterArgs...)...)
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
func (h *StatsHandler) statByProvider(ctx context.Context, stats *StatsResponse, vkJoin, vkFilter string, filterArgs []any, metric string, since time.Time) error {
	query := `
		SELECT p.name, ` + metricValueSelect(metric) + `
		FROM request_logs rl
		JOIN providers p ON rl.provider_id = p.id` + vkJoin + `
		WHERE rl.created_at >= $1` + vkFilter + `
		GROUP BY p.name
		ORDER BY val DESC
		LIMIT 10`

	rows, err := h.dbPool.Query(ctx, query, append([]any{since}, filterArgs...)...)
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
func (h *StatsHandler) statByVirtualKey(ctx context.Context, stats *StatsResponse, metric string, since time.Time, excludeDeleted bool, ownerID string) error {
	ownerFrag, ownerArgs := ownerFilterFragment(ownerID, 2)
	virtualKeyQuery := `
		SELECT vk.name, ` + metricValueSelect(metric) + `
		FROM request_logs rl
		JOIN virtual_keys vk ON rl.virtual_key_id = vk.id
		WHERE rl.created_at >= $1` + ownerFrag + `
		GROUP BY vk.name
		ORDER BY val DESC
		LIMIT 10`

	rows, err := h.dbPool.Query(ctx, virtualKeyQuery, append([]any{since}, ownerArgs...)...)
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

	// Queries 4b/4c only make sense unscoped: deleted-key rows cannot be
	// attributed to an owner anymore, and chat/arena rows are admin traffic.
	// This early return is also what keeps Q4c's own $2 (keyName) from
	// colliding with the owner fragment's $2 — anything that lets a scoped
	// caller reach Q4c must renumber it first.
	if ownerID != "" {
		return nil
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

// statScalars fills the scalar aggregate fields: avg latency (Q5), error rate
// (Q6), avg overhead (Q7), total tokens (Q8), avg tokens/request (Q9), rate-limit
// hits (Q10), avg TTFT (Q11), and the always-fresh requests-in-last-1h count.
// Every query is best-effort: on error it logs and leaves the field(s) zeroed,
// never aborting — matching the original inline behavior.
func (h *StatsHandler) statScalars(ctx context.Context, stats *StatsResponse, vkJoin, vkFilter string, filterArgs []any, since, now time.Time) {
	sinceArgs := append([]any{since}, filterArgs...)

	// Query 5: Avg latency
	query := `
		SELECT COALESCE(AVG(rl.duration_ms), 0) as avg_duration
		FROM request_logs rl` + vkJoin + `
		WHERE rl.created_at >= $1 AND rl.status_code > 0 AND rl.status_code < 400` + vkFilter

	err := h.dbPool.QueryRow(ctx, query, sinceArgs...).Scan(&stats.AvgLatencyMs)
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

	err = h.dbPool.QueryRow(ctx, query, sinceArgs...).Scan(&stats.ErrorRate)
	if err != nil {
		debuglog.Error("stats: query failed", "query", "error_rate", "error", err)
		stats.ErrorRate = 0
	}

	// Query 7: Avg overhead
	query = `
		SELECT COALESCE(AVG(rl.proxy_overhead_ms), 0) as avg_overhead
		FROM request_logs rl` + vkJoin + `
		WHERE rl.created_at >= $1 AND rl.proxy_overhead_ms > 0` + vkFilter

	err = h.dbPool.QueryRow(ctx, query, sinceArgs...).Scan(&stats.AvgOverheadMs)
	if err != nil {
		debuglog.Error("stats: query failed", "query", "avg_overhead", "error", err)
		stats.AvgOverheadMs = 0
	}

	// Query 8: Total tokens
	query = `
		SELECT COALESCE(SUM(rl.tokens_prompt), 0) as prompt_tokens, COALESCE(SUM(rl.tokens_completion), 0) as completion_tokens, COALESCE(SUM(rl.tokens_prompt_cache_hit), 0) as cache_hit_tokens
		FROM request_logs rl` + vkJoin + `
		WHERE rl.created_at >= $1` + vkFilter

	err = h.dbPool.QueryRow(ctx, query, sinceArgs...).Scan(&stats.TotalTokensPrompt, &stats.TotalTokensCompletion, &stats.TotalTokensCacheHit)
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

	err = h.dbPool.QueryRow(ctx, query, sinceArgs...).Scan(&stats.AvgTokensPerRequest)
	if err != nil {
		debuglog.Error("stats: query failed", "query", "avg_tokens_per_request", "error", err)
		stats.AvgTokensPerRequest = 0
	}

	// Query 10: Rate limit hits (429 count)
	query = `
		SELECT COUNT(*) FILTER (WHERE rl.status_code = 429)
		FROM request_logs rl` + vkJoin + `
		WHERE rl.created_at >= $1` + vkFilter

	err = h.dbPool.QueryRow(ctx, query, sinceArgs...).Scan(&stats.RateLimitHits)
	if err != nil {
		debuglog.Error("stats: query failed", "query", "rate_limit_hits", "error", err)
		stats.RateLimitHits = 0
	}

	// Query 11: Avg TTFT (streaming only — non-streaming requests have no first token)
	query = `
		SELECT COALESCE(AVG(COALESCE(NULLIF(rl.ttft_ms, 0), rl.response_header_ms)) FILTER (WHERE COALESCE(NULLIF(rl.ttft_ms, 0), rl.response_header_ms) > 0 AND rl.streaming = true), 0) as avg_ttft
		FROM request_logs rl` + vkJoin + `
		WHERE rl.created_at >= $1 AND rl.status_code > 0 AND rl.status_code < 400` + vkFilter

	err = h.dbPool.QueryRow(ctx, query, sinceArgs...).Scan(&stats.AvgTTFTMs)
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
		WHERE rl.created_at >= $1`+vkFilter, append([]any{_1hAgo}, filterArgs...)...).Scan(&requests1h)
	if err != nil {
		debuglog.Error("stats: query failed", "query", "requests_last_1h", "error", err)
		requests1h = 0
	}
	stats.RequestsLast1h = requests1h
}

// statTotals fills TotalRequestsLast24h / TotalRequestsLast7d: the count for the
// requested period plus the cross-fill of the other window (a 24h request also
// fills the 7d total and vice-versa). A query failure here is fatal — returned
// so calculateStats aborts.
func (h *StatsHandler) statTotals(ctx context.Context, stats *StatsResponse, vkJoin, vkFilter string, filterArgs []any, period time.Duration, since, now time.Time) error {
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
	err := h.dbPool.QueryRow(ctx, query, append([]any{since}, filterArgs...)...).Scan(&count)
	if err != nil {
		debuglog.Error("stats: query failed", "query", "total_requests", "error", err)
		return err
	}

	switch period {
	case 7 * 24 * time.Hour:
		stats.TotalRequestsLast7d = count
	default:
		stats.TotalRequestsLast24h = count
	}

	if period == 24*time.Hour {
		_7dAgo := now.Add(-7 * 24 * time.Hour)
		err = h.dbPool.QueryRow(ctx, query, append([]any{_7dAgo}, filterArgs...)...).Scan(&count)
		if err != nil {
			debuglog.Error("stats: query failed", "query", "total_requests_7d", "error", err)
			return err
		}
		stats.TotalRequestsLast7d = count
	} else {
		_24hAgo := now.Add(-24 * time.Hour)
		err = h.dbPool.QueryRow(ctx, query, append([]any{_24hAgo}, filterArgs...)...).Scan(&count)
		if err != nil {
			debuglog.Error("stats: query failed", "query", "total_requests_24h", "error", err)
			return err
		}
		stats.TotalRequestsLast24h = count
	}
	return nil
}

// statLatencyBreakdown fills ByProviderLatency with the top-N provider (Q13)
// latency breakdown. Best-effort: a query failure logs and leaves the slice
// empty; a per-row scan error skips the row. Only invoked when the caller
// requested latency data.
func (h *StatsHandler) statLatencyBreakdown(ctx context.Context, stats *StatsResponse, vkJoin, vkFilter string, filterArgs []any, since time.Time) {
	// Query 13: Per-provider latency breakdown (top 6 by avg total latency).
	query := `
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

	rows, err := h.dbPool.Query(ctx, query, append([]any{since}, filterArgs...)...)
	if err != nil {
		debuglog.Error("stats: query failed", "query", "by_provider_latency", "error", err)
	} else {
		for rows.Next() {
			var entry ProviderLatencyEntry
			if err := rows.Scan(&entry.ProviderName, &entry.RequestCount, &entry.TotalMs, &entry.OverheadMs, &entry.ProviderMs); err != nil {
				continue
			}
			stats.ByProviderLatency = append(stats.ByProviderLatency, entry)
		}
		rows.Close()
	}
}
