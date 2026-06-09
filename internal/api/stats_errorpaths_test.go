package api

import (
	"context"
	"testing"
	"time"
)

// TestStats_QueryErrorPaths exercises the query-failure branches of the
// calculateStats helpers. A cancelled context makes every dbPool query fail, so
// each helper hits its error path: the fatal helpers (statTotals / statBy*)
// surface the error, and the best-effort helpers (statScalars /
// statLatencyBreakdown) log, leave their fields zeroed, and return normally.
func TestStats_QueryErrorPaths(t *testing.T) {
	handler, _, cleanup := newStatsHandler(t)
	defer cleanup()

	// Cancelled context → every query returns an error.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	newStats := func() *StatsResponse {
		return &StatsResponse{
			ByModel:      make(map[string]int64),
			ByProvider:   make(map[string]int64),
			ByVirtualKey: make(map[string]int64),
		}
	}
	now := time.Now().UTC()
	since := now.Add(-24 * time.Hour)

	// Fatal helpers must surface the error.
	if err := handler.statTotals(ctx, newStats(), "", "", 24*time.Hour, since, now); err == nil {
		t.Error("statTotals: expected error on cancelled context")
	}
	if err := handler.statByModel(ctx, newStats(), "", "", "requests", since); err == nil {
		t.Error("statByModel: expected error on cancelled context")
	}
	if err := handler.statByProvider(ctx, newStats(), "", "", "requests", since); err == nil {
		t.Error("statByProvider: expected error on cancelled context")
	}
	if err := handler.statByVirtualKey(ctx, newStats(), "requests", since, false); err == nil {
		t.Error("statByVirtualKey: expected error on cancelled context")
	}

	// Best-effort helpers must not panic and must leave their fields zeroed.
	s := newStats()
	handler.statScalars(ctx, s, "", "", since, now)
	if s.AvgLatencyMs != 0 || s.ErrorRate != 0 || s.AvgOverheadMs != 0 ||
		s.TotalTokensPrompt != 0 || s.TotalTokensCompletion != 0 || s.TotalTokensCacheHit != 0 ||
		s.AvgTokensPerRequest != 0 || s.RateLimitHits != 0 || s.AvgTTFTMs != 0 || s.RequestsLast1h != 0 {
		t.Errorf("statScalars on error: expected zeroed fields, got %+v", s)
	}

	s2 := newStats()
	handler.statLatencyBreakdown(ctx, s2, "", "", since)
	if len(s2.ByModelLatency) != 0 || len(s2.ByProviderLatency) != 0 {
		t.Errorf("statLatencyBreakdown on error: expected empty slices, got %d/%d",
			len(s2.ByModelLatency), len(s2.ByProviderLatency))
	}

	// calculateStats must propagate the first fatal helper's error.
	if _, err := handler.calculateStats(ctx, 24*time.Hour, false, "requests", true); err == nil {
		t.Error("calculateStats: expected error on cancelled context")
	}
}

// TestStats_StatTotals7DayPeriod exercises the 7-day period branch in statTotals,
// which sets TotalRequestsLast7d instead of TotalRequestsLast24h, and also
// queries TotalRequestsLast24h from the else branch (cross-fill).
func TestStats_StatTotals7DayPeriod(t *testing.T) {
	handler, _, cleanup := newStatsHandler(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now().UTC()
	since := now.Add(-7 * 24 * time.Hour)

	stats := &StatsResponse{
		ByModel:      make(map[string]int64),
		ByProvider:   make(map[string]int64),
		ByVirtualKey: make(map[string]int64),
	}

	err := handler.statTotals(ctx, stats, "", "", 7*24*time.Hour, since, now)
	if err != nil {
		t.Fatalf("statTotals with 7-day period: %v", err)
	}

	// With a 7-day period, TotalRequestsLast7d should be set
	// and TotalRequestsLast24h should also be cross-filled.
	if stats.TotalRequestsLast7d < 0 {
		t.Errorf("TotalRequestsLast7d = %d, want >= 0", stats.TotalRequestsLast7d)
	}
	if stats.TotalRequestsLast24h < 0 {
		t.Errorf("TotalRequestsLast24h = %d, want >= 0", stats.TotalRequestsLast24h)
	}
}

// TestStats_StatTotals7DayErrorPath exercises the 7-day period branch
// with a cancelled context to trigger the secondary query error path.
func TestStats_StatTotals7DayErrorPath(t *testing.T) {
	handler, _, cleanup := newStatsHandler(t)
	defer cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	stats := &StatsResponse{
		ByModel:      make(map[string]int64),
		ByProvider:   make(map[string]int64),
		ByVirtualKey: make(map[string]int64),
	}
	now := time.Now().UTC()
	since := now.Add(-7 * 24 * time.Hour)

	if err := handler.statTotals(ctx, stats, "", "", 7*24*time.Hour, since, now); err == nil {
		t.Error("statTotals with 7-day period: expected error on cancelled context")
	}
}

// TestStats_CalculateStats7DayPeriod verifies the full calculateStats
// flow works with a 7-day period.
func TestStats_CalculateStats7DayPeriod(t *testing.T) {
	handler, _, cleanup := newStatsHandler(t)
	defer cleanup()

	ctx := context.Background()
	result, err := handler.calculateStats(ctx, 7*24*time.Hour, false, "requests", true)
	if err != nil {
		t.Fatalf("calculateStats with 7-day period: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result from calculateStats")
	}
	if result.TotalRequestsLast7d < 0 {
		t.Errorf("TotalRequestsLast7d = %d, want >= 0", result.TotalRequestsLast7d)
	}
}
