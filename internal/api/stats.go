package api

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hugalafutro/model-hotel/internal/util"
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

// ownerFilterFragment returns a WHERE fragment restricting rows to traffic
// belonging to ownerID, plus the bind args it needs, or ("", nil) when unscoped
// (empty ownerID). It matches the two disjoint row shapes the same way
// appendLogFilters does: keyed rows through the key's CURRENT owner (so
// reassigning a key moves its history), keyless rows (dashboard chat/arena)
// through the request-time owner_user_id stamped on the row itself; see
// migration 067. The id binds through the $argIdx placeholder, so the caller
// must state how many args the consuming query already carries; the stats
// queries take a leading timestamp and pass 2, the provider-list token totals
// carry no other args and pass 1. The parse
// stays as defense in depth: a non-empty id that is not a valid UUID fails
// CLOSED to a no-match fragment (" AND 1=0"), never to "" — dropping the
// filter there would silently widen a scoped query to every owner's rows, an
// authorization leak.
func ownerFilterFragment(ownerID string, argIdx int) (string, []any) {
	if ownerID == "" {
		return "", nil
	}
	u, err := uuid.Parse(ownerID)
	if err != nil {
		return " AND 1=0", nil
	}
	ph := "$" + util.IntToStr(argIdx)
	return " AND (rl.virtual_key_id IN (SELECT vko.id FROM virtual_keys vko WHERE vko.owner_user_id = " + ph + ")" +
		" OR (rl.virtual_key_id IS NULL AND rl.owner_user_id = " + ph + "))", []any{u}
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
	stats, err := h.calculateStats(r.Context(), period, excludeDeleted, metric, includeLatency, logOwnerScope(r))
	if err != nil {
		respondError(w, "failed to calculate stats", err, http.StatusInternalServerError)
		return
	}

	writeJSON(w, stats)
}

func (h *StatsHandler) calculateStats(ctx context.Context, period time.Duration, excludeDeleted bool, metric string, includeLatency bool, ownerID string) (*StatsResponse, error) {
	stats := &StatsResponse{
		ByModel:      make(map[string]int64),
		ByProvider:   make(map[string]int64),
		ByVirtualKey: make(map[string]int64),
	}

	vkJoin, vkFilter := vkScope(excludeDeleted)
	// Owner scope rides the same filter seam as the deleted-key toggle: for
	// non-admins it is mandatory row-level security, for admins an optional
	// dashboard filter. Every consuming query binds $1 = timestamp, so the
	// owner id rides $2.
	ownerFrag, filterArgs := ownerFilterFragment(ownerID, 2)
	vkFilter += ownerFrag

	now := time.Now().UTC()
	since := now.Add(-period)

	// Query 1 + cross-fill: total request counts (fatal on error).
	if err := h.statTotals(ctx, stats, vkJoin, vkFilter, filterArgs, period, since, now); err != nil {
		return nil, err
	}

	// Queries 2–4c: dimension breakdowns (top-10 by model / provider / virtual key).
	if err := h.statByModel(ctx, stats, vkJoin, vkFilter, filterArgs, metric, since); err != nil {
		return nil, err
	}
	if err := h.statByProvider(ctx, stats, vkJoin, vkFilter, filterArgs, metric, since); err != nil {
		return nil, err
	}
	if err := h.statByVirtualKey(ctx, stats, metric, since, excludeDeleted, ownerID); err != nil {
		return nil, err
	}

	// Queries 5–11 + requests-in-last-1h: scalar aggregates (best-effort).
	h.statScalars(ctx, stats, vkJoin, vkFilter, filterArgs, since, now)

	// Queries 12–13: per-model / per-provider latency breakdown (best-effort,
	// only when the caller requested latency data).
	if includeLatency {
		h.statLatencyBreakdown(ctx, stats, vkJoin, vkFilter, filterArgs, since)
	}

	return stats, nil
}
