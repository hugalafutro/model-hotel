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
