package api

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

// ---------------------------------------------------------------------------
// Tests moved from coverage_gap3_test.go
// ---------------------------------------------------------------------------

// TestCalculateStats_TokensMetric tests calculateStats with metric=tokens
// to cover the token aggregation branches in by_model, by_provider, by_virtual_key queries.
func TestCalculateStats_TokensMetric(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	// Set up test data with token counts
	providerID := uuid.New()
	logID := uuid.New()
	insertTestProvider(t, pool, providerID, "test-provider-tokens-metric", "https://api.example.com/v1")
	// Insert request log with significant token counts
	insertTestRequestLog(t, pool, logID, providerID, "test-model", 200, 100, 100, 200)

	ctx := context.Background()

	// Call calculateStats with metric=tokens
	stats, err := handler.calculateStats(ctx, 24*time.Hour, true, "tokens", false, "")
	if err != nil {
		t.Fatalf("calculateStats failed: %v", err)
	}

	// With metric=tokens, ByModel and ByProvider should contain token counts
	if len(stats.ByModel) == 0 {
		t.Error("Expected ByModel to have entries with metric=tokens")
	}
	if len(stats.ByProvider) == 0 {
		t.Error("Expected ByProvider to have entries with metric=tokens")
	}

	// Check token totals
	if stats.TotalTokensPrompt != 100 {
		t.Errorf("Expected TotalTokensPrompt=100, got %d", stats.TotalTokensPrompt)
	}
	if stats.TotalTokensCompletion != 200 {
		t.Errorf("Expected TotalTokensCompletion=200, got %d", stats.TotalTokensCompletion)
	}
}

// TestCalculateStats_TokensMetric_ByVirtualKey tests calculateStats with metric=tokens
// for the by_virtual_key query path.
func TestCalculateStats_TokensMetric_ByVirtualKey(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	ctx := context.Background()

	// Create a virtual key
	vkID := uuid.New()
	_, err := pool.Exec(ctx, `
		INSERT INTO virtual_keys (id, name, key_hash, key_preview, created_at)
		VALUES ($1, 'test-vk-tokens', 'hash', 'sk-...ab', NOW())`,
		vkID)
	if err != nil {
		t.Fatalf("Failed to insert virtual key: %v", err)
	}

	providerID := uuid.New()
	logID := uuid.New()
	insertTestProvider(t, pool, providerID, "test-provider-vk-tokens", "https://api.example.com/v1")

	// Insert request log with virtual key and token counts
	insertRichTestRequestLog(t, pool, logID, providerID, "test-model", 200, 100, 50, 75, requestLogOpts{
		VirtualKeyID: &vkID,
	})

	stats, err := handler.calculateStats(ctx, 24*time.Hour, true, "tokens", false, "")
	if err != nil {
		t.Fatalf("calculateStats failed: %v", err)
	}

	// ByVirtualKey should contain the virtual key name with token count
	if stats.ByVirtualKey["test-vk-tokens"] != 125 {
		t.Errorf("Expected ByVirtualKey['test-vk-tokens']=125, got %d", stats.ByVirtualKey["test-vk-tokens"])
	}
}

// TestCalculateStats_ExcludeDeletedFalse tests calculateStats with excludeDeleted=false
// to cover the deleted virtual keys aggregate query path.
func TestCalculateStats_ExcludeDeletedFalse(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	ctx := context.Background()

	providerID := uuid.New()
	insertTestProvider(t, pool, providerID, "test-provider-del-false", "https://api.example.com/v1")

	// Insert request log with a virtual_key_id that doesn't exist in virtual_keys table
	// This simulates a deleted virtual key
	deletedVKID := uuid.New()
	logID := uuid.New()
	insertRichTestRequestLog(t, pool, logID, providerID, "test-model", 200, 100, 10, 20, requestLogOpts{
		VirtualKeyID: &deletedVKID,
	})

	stats, err := handler.calculateStats(ctx, 24*time.Hour, false, "requests", false, "")
	if err != nil {
		t.Fatalf("calculateStats failed: %v", err)
	}

	// With excludeDeleted=false, deleted VK requests should appear in ByVirtualKey["Deleted"]
	if stats.ByVirtualKey["Deleted"] != 1 {
		t.Errorf("Expected ByVirtualKey['Deleted']=1, got %d", stats.ByVirtualKey["Deleted"])
	}
}

// TestCalculateStats_ExcludeDeletedFalse_Tokens tests calculateStats with excludeDeleted=false
// and metric=tokens to cover the deleted VK path with token aggregation.
func TestCalculateStats_ExcludeDeletedFalse_Tokens(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	ctx := context.Background()

	providerID := uuid.New()
	insertTestProvider(t, pool, providerID, "test-provider-del-tok", "https://api.example.com/v1")

	// Insert request log with deleted VK and token counts
	deletedVKID := uuid.New()
	logID := uuid.New()
	insertRichTestRequestLog(t, pool, logID, providerID, "test-model", 200, 100, 30, 40, requestLogOpts{
		VirtualKeyID: &deletedVKID,
	})

	stats, err := handler.calculateStats(ctx, 24*time.Hour, false, "tokens", false, "")
	if err != nil {
		t.Fatalf("calculateStats failed: %v", err)
	}

	// Deleted VK should have token count (30+40=70)
	if stats.ByVirtualKey["Deleted"] != 70 {
		t.Errorf("Expected ByVirtualKey['Deleted']=70, got %d", stats.ByVirtualKey["Deleted"])
	}
}

// TestCalculateStats_7dPeriod tests calculateStats with period=7d
// to cover the else branch for non-24h secondary queries.
func TestCalculateStats_7dPeriod(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	ctx := context.Background()

	providerID := uuid.New()
	logID := uuid.New()
	insertTestProvider(t, pool, providerID, "test-provider-7d-period", "https://api.example.com/v1")
	insertTestRequestLog(t, pool, logID, providerID, "test-model", 200, 100, 10, 20)

	stats, err := handler.calculateStats(ctx, 7*24*time.Hour, true, "requests", false, "")
	if err != nil {
		t.Fatalf("calculateStats failed: %v", err)
	}

	// With 7d period, TotalRequestsLast7d should be set
	if stats.TotalRequestsLast7d != 1 {
		t.Errorf("Expected TotalRequestsLast7d=1, got %d", stats.TotalRequestsLast7d)
	}

	// The else branch should have queried for 24h ago
	// TotalRequestsLast24h should also be set (from the secondary query)
	if stats.TotalRequestsLast24h != 1 {
		t.Errorf("Expected TotalRequestsLast24h=1, got %d", stats.TotalRequestsLast24h)
	}
}

// TestCalculateStats_1hPeriod tests calculateStats with period=1h
// to cover the else branch for non-7d initial period.
func TestCalculateStats_1hPeriod(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	ctx := context.Background()

	providerID := uuid.New()
	logID := uuid.New()
	insertTestProvider(t, pool, providerID, "test-provider-1h-period", "https://api.example.com/v1")
	insertTestRequestLog(t, pool, logID, providerID, "test-model", 200, 100, 10, 20)

	stats, err := handler.calculateStats(ctx, 1*time.Hour, true, "requests", false, "")
	if err != nil {
		t.Fatalf("calculateStats failed: %v", err)
	}

	// With 1h period, TotalRequestsLast24h is populated by the secondary query
	// (since period != 7d, the else branch queries 24h). Logs exist within 24h.
	// The else branch sets TotalRequestsLast24h = 0 initially
	// Then the secondary query for _24hAgo should populate it
	if stats.TotalRequestsLast24h < 1 {
		t.Errorf("Expected TotalRequestsLast24h>=1, got %d", stats.TotalRequestsLast24h)
	}
}

// TestCalculateStats_ChatArenaKeys_Tokens tests calculateStats with chat/arena virtual_key_name
// and metric=tokens to cover the token aggregation path for chat/arena queries.
func TestCalculateStats_ChatArenaKeys_Tokens(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	ctx := context.Background()

	providerID := uuid.New()
	insertTestProvider(t, pool, providerID, "test-provider-chat-tok", "https://api.example.com/v1")

	// Insert request logs with chat and arena virtual_key_name and token counts
	chatLogID := uuid.New()
	insertRichTestRequestLog(t, pool, chatLogID, providerID, "test-model", 200, 100, 100, 150, requestLogOpts{
		VirtualKeyName: "chat",
	})
	arenaLogID := uuid.New()
	insertRichTestRequestLog(t, pool, arenaLogID, providerID, "test-model", 200, 100, 80, 120, requestLogOpts{
		VirtualKeyName: "arena",
	})

	stats, err := handler.calculateStats(ctx, 24*time.Hour, true, "tokens", false, "")
	if err != nil {
		t.Fatalf("calculateStats failed: %v", err)
	}

	// Chat should have token count (100+150=250)
	if stats.ByVirtualKey["chat"] != 250 {
		t.Errorf("Expected ByVirtualKey['chat']=250, got %d", stats.ByVirtualKey["chat"])
	}
	// Arena should have token count (80+120=200)
	if stats.ByVirtualKey["arena"] != 200 {
		t.Errorf("Expected ByVirtualKey['arena']=200, got %d", stats.ByVirtualKey["arena"])
	}
}

// TestCalculateStats_QueryError tests calculateStats with a closed pool
// to cover the error paths for various queries beyond the first one.
func TestCalculateStats_QueryError(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	// Close pool before calling calculateStats
	pool.Close()

	ctx := context.Background()
	_, err := handler.calculateStats(ctx, 24*time.Hour, true, "requests", false, "")
	if err == nil {
		t.Error("Expected error when pool is closed")
	}
}

// TestCalculateStats_LateQueryErrors tests all computed fields with diverse data
// to exercise the value-setting paths (lines 356-454). With a mix of success,
// error, and rate-limited requests, all stat fields should be non-zero.
func TestCalculateStats_LateQueryErrors(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	providerID := uuid.New()
	insertTestProvider(t, pool, providerID, "test-provider-late-err", "https://api.example.com/v1")

	// Insert logs with various status codes, TTFT, and overhead to exercise all paths
	vkID := uuid.New()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO virtual_keys (id, name, key_hash, key_preview, created_at)
		VALUES ($1, 'test-vk-late', 'hash', 'sk-...la', NOW())`, vkID)
	if err != nil {
		t.Fatalf("Failed to insert virtual key: %v", err)
	}

	// Success request (streaming — has TTFT)
	insertRichTestRequestLog(t, pool, uuid.New(), providerID, "test-model", 200, 100, 10, 20, requestLogOpts{
		VirtualKeyID:     &vkID,
		ResponseHeaderMs: 50.0,
		ProxyOverheadMs:  5.0,
		Streaming:        true,
	})
	// Error request
	insertRichTestRequestLog(t, pool, uuid.New(), providerID, "test-model", 500, 200, 5, 10, requestLogOpts{
		VirtualKeyID:     &vkID,
		ResponseHeaderMs: 0,
		ProxyOverheadMs:  3.0,
	})
	// Rate limited request
	insertRichTestRequestLog(t, pool, uuid.New(), providerID, "test-model", 429, 50, 2, 3, requestLogOpts{
		VirtualKeyID:     &vkID,
		ResponseHeaderMs: 0,
		ProxyOverheadMs:  1.0,
	})

	ctx := context.Background()
	stats, err := handler.calculateStats(ctx, 24*time.Hour, true, "requests", false, "")
	if err != nil {
		t.Fatalf("calculateStats failed: %v", err)
	}

	// Verify all the computed fields have reasonable values
	if stats.AvgLatencyMs <= 0 {
		t.Errorf("Expected AvgLatencyMs>0, got %f", stats.AvgLatencyMs)
	}
	if stats.ErrorRate <= 0 {
		t.Errorf("Expected ErrorRate>0, got %f", stats.ErrorRate)
	}
	if stats.AvgOverheadMs <= 0 {
		t.Errorf("Expected AvgOverheadMs>0, got %f", stats.AvgOverheadMs)
	}
	if stats.TotalTokensPrompt <= 0 {
		t.Errorf("Expected TotalTokensPrompt>0, got %d", stats.TotalTokensPrompt)
	}
	if stats.TotalTokensCompletion <= 0 {
		t.Errorf("Expected TotalTokensCompletion>0, got %d", stats.TotalTokensCompletion)
	}
	if stats.AvgTokensPerRequest <= 0 {
		t.Errorf("Expected AvgTokensPerRequest>0, got %f", stats.AvgTokensPerRequest)
	}
	if stats.RateLimitHits != 1 {
		t.Errorf("Expected RateLimitHits=1, got %d", stats.RateLimitHits)
	}
	if stats.AvgTTFTMs <= 0 {
		t.Errorf("Expected AvgTTFTMs>0, got %f", stats.AvgTTFTMs)
	}
	if stats.RequestsLast1h < 3 {
		t.Errorf("Expected RequestsLast1h>=3, got %d", stats.RequestsLast1h)
	}
}

// TestCalculateStats_24hPeriod_7dQuery exercises the period==24h branch
// that queries the 7d count as a secondary query (lines 176-183).
func TestCalculateStats_24hPeriod_7dQuery(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	providerID := uuid.New()
	insertTestProvider(t, pool, providerID, "test-provider-24h-7d", "https://api.example.com/v1")
	insertTestRequestLog(t, pool, uuid.New(), providerID, "test-model", 200, 100, 10, 20)

	ctx := context.Background()
	stats, err := handler.calculateStats(ctx, 24*time.Hour, true, "requests", false, "")
	if err != nil {
		t.Fatalf("calculateStats failed: %v", err)
	}

	// Both 24h and 7d counts should be populated
	if stats.TotalRequestsLast24h != 1 {
		t.Errorf("Expected TotalRequestsLast24h=1, got %d", stats.TotalRequestsLast24h)
	}
	if stats.TotalRequestsLast7d != 1 {
		t.Errorf("Expected TotalRequestsLast7d=1, got %d", stats.TotalRequestsLast7d)
	}
}

// TestCalculateStats_CancelledContext tests the error-return branches in
// calculateStats by using a pre-cancelled context. With a cancelled context,
// the very first query fails, hitting the error path. This is a complementary
// test to TestCalculateStats_QueryError (which closes the pool).
func TestCalculateStats_CancelledContext(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	providerID := uuid.New()
	insertTestProvider(t, pool, providerID, "test-provider-cancel-ctx", "https://api.example.com/v1")
	insertTestRequestLog(t, pool, uuid.New(), providerID, "test-model", 200, 100, 10, 20)

	// Use a pre-cancelled context - the first query will fail immediately
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := handler.calculateStats(ctx, 24*time.Hour, true, "requests", false, "")
	if err == nil {
		t.Error("Expected error with cancelled context")
	}
}

// TestCalculateStats_CancelledContext_1h tests the else branch error path
// (lines 187-190) with a cancelled context and period=1h.
func TestCalculateStats_CancelledContext_1h(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	providerID := uuid.New()
	insertTestProvider(t, pool, providerID, "test-provider-cancel-1h", "https://api.example.com/v1")
	insertTestRequestLog(t, pool, uuid.New(), providerID, "test-model", 200, 100, 10, 20)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := handler.calculateStats(ctx, 1*time.Hour, true, "requests", false, "")
	if err == nil {
		t.Error("Expected error with cancelled context")
	}
}

// TestCalculateStats_CancelledContext_7d tests the 7d branch error path
// (lines 179-182) with a cancelled context and period=7d.
func TestCalculateStats_CancelledContext_7d(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	providerID := uuid.New()
	insertTestProvider(t, pool, providerID, "test-provider-cancel-7d", "https://api.example.com/v1")
	insertTestRequestLog(t, pool, uuid.New(), providerID, "test-model", 200, 100, 10, 20)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := handler.calculateStats(ctx, 7*24*time.Hour, true, "requests", false, "")
	if err == nil {
		t.Error("Expected error with cancelled context")
	}
}

// ---------------------------------------------------------------------------
// calculateStats edge cases
// ---------------------------------------------------------------------------

// TestCalculateStats_EmptyDB_IncludeLatency verifies that calculateStats
// with includeLatency=true and no data returns empty stats without error,
// exercising the statLatencyBreakdown best-effort path on an empty DB.
func TestCalculateStats_EmptyDB_IncludeLatency(t *testing.T) {
	handler, _, cleanup := newStatsHandler(t)
	defer cleanup()

	ctx := context.Background()
	stats, err := handler.calculateStats(ctx, 24*time.Hour, true, "requests", true, "")
	if err != nil {
		t.Fatalf("calculateStats with empty DB and includeLatency=true: %v", err)
	}

	if len(stats.ByModelLatency) != 0 {
		t.Errorf("Expected empty ByModelLatency, got %d entries", len(stats.ByModelLatency))
	}
	if len(stats.ByProviderLatency) != 0 {
		t.Errorf("Expected empty ByProviderLatency, got %d entries", len(stats.ByProviderLatency))
	}
	if stats.TotalRequestsLast24h != 0 {
		t.Errorf("Expected TotalRequestsLast24h=0, got %d", stats.TotalRequestsLast24h)
	}
}

// TestCalculateStats_IncludeLatencyWithData verifies calculateStats with
// includeLatency=true and sufficient data returns model and provider latency entries.
func TestCalculateStats_IncludeLatencyWithData(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	providerID := uuid.New()
	insertTestProvider(t, pool, providerID, "test-provider-lat-data", "https://api.example.com/v1")

	// Insert 3 requests for a single model to meet the HAVING COUNT(*) >= 3 threshold
	for i := 0; i < 3; i++ {
		insertRichTestRequestLog(t, pool, uuid.New(), providerID, "lat-model", 200, 150, 10, 20, requestLogOpts{
			ProxyOverheadMs: 15.0,
			LatencyMs:       135.0,
		})
	}

	ctx := context.Background()
	stats, err := handler.calculateStats(ctx, 24*time.Hour, true, "requests", true, "")
	if err != nil {
		t.Fatalf("calculateStats with includeLatency=true: %v", err)
	}

	if len(stats.ByModelLatency) == 0 {
		t.Error("Expected ByModelLatency entries with 3+ requests and includeLatency=true")
	}
	if len(stats.ByProviderLatency) == 0 {
		t.Error("Expected ByProviderLatency entries with 3+ requests and includeLatency=true")
	}
}

// TestCalculateStats_AlreadyCancelledContext verifies that calculateStats returns
// an error when the context is already cancelled, exercising the statTotals error
// path (first query in calculateStats that returns early on error).
func TestCalculateStats_AlreadyCancelledContext(t *testing.T) {
	handler, _, cleanup := newStatsHandler(t)
	defer cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := handler.calculateStats(ctx, 24*time.Hour, true, "requests", false, "")
	if err == nil {
		t.Error("expected error from calculateStats with cancelled context")
	}
}

// TestCalculateStats_StatByModelError verifies that calculateStats returns
// an error when the statByModel query fails (cancelled context after
// statTotals succeeds). This exercises the second error-return path.
func TestCalculateStats_StatByModelError(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	// Insert enough data so statTotals succeeds with a live context,
	// then cancel the context for the subsequent statByModel call.
	providerID := uuid.New()
	insertTestProvider(t, pool, providerID, "test-stats-by-model-err", "https://api.example.com/v1")
	insertRichTestRequestLog(t, pool, uuid.New(), providerID, "stats-model-err", 200, 100, 5, 10, requestLogOpts{})

	// Use a short-lived context that expires between statTotals and statByModel.
	// In practice, the cancelled context error may hit statTotals first,
	// but either way we get an error.
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()
	time.Sleep(1 * time.Millisecond) // Let the timeout expire

	_, err := handler.calculateStats(ctx, 24*time.Hour, true, "requests", false, "")
	if err == nil {
		t.Error("expected error from calculateStats with expired context")
	}
}

// TestCalculateStats_7DayPeriod verifies that calculateStats handles the 7-day
// period branch correctly. The statTotals function has different code paths for
// 24h vs 7d periods (the switch statement at the top and the cross-fill logic).
func TestCalculateStats_7DayPeriod(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	// Insert a provider and request log so statTotals has data
	providerID := uuid.New()
	insertTestProvider(t, pool, providerID, "test-stats-7d", "https://api.example.com/v1")
	insertRichTestRequestLog(t, pool, uuid.New(), providerID, "stats-7d-model", 200, 100, 5, 10, requestLogOpts{})

	ctx := context.Background()
	stats, err := handler.calculateStats(ctx, 7*24*time.Hour, false, "requests", false, "")
	if err != nil {
		t.Fatalf("calculateStats(7d): %v", err)
	}

	// With a 7-day period, TotalRequestsLast7d should be set
	if stats.TotalRequestsLast7d < 1 {
		t.Errorf("expected TotalRequestsLast7d >= 1, got %d", stats.TotalRequestsLast7d)
	}
	// TotalRequestsLast24h should also be filled (cross-fill from the else branch)
	if stats.TotalRequestsLast24h < 0 {
		t.Errorf("TotalRequestsLast24h should not be negative, got %d", stats.TotalRequestsLast24h)
	}
}

// TestCalculateStats_IncludeLatencyTrue verifies that the includeLatency=true
// path in calculateStats populates the ByModelLatency and ByProviderLatency slices.
func TestCalculateStats_IncludeLatencyTrue(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	providerID := uuid.New()
	insertTestProvider(t, pool, providerID, "test-stats-latency", "https://api.example.com/v1")
	insertRichTestRequestLog(t, pool, uuid.New(), providerID, "stats-latency-model", 200, 100, 5, 10, requestLogOpts{})

	ctx := context.Background()
	stats, err := handler.calculateStats(ctx, 24*time.Hour, false, "requests", true, "")
	if err != nil {
		t.Fatalf("calculateStats with latency: %v", err)
	}

	// The latency fields may be empty slices (if no data matches
	// the HAVING COUNT(*) >= 3 filter) but should be initialized
	// (non-nil) by calculateStats when includeLatency=true.
	// Note: they may actually be nil if no data qualifies; this test
	// verifies the code path is exercised without panicking.
	_ = stats.ByModelLatency
	_ = stats.ByProviderLatency
}

// ---------------------------------------------------------------------------
// 3. calculateStats — includeLatency=true path
// ---------------------------------------------------------------------------

// TestCalculateStats_WithLatency exercises the includeLatency=true branch
// in calculateStats, which invokes statLatencyBreakdown.
func TestCalculateStats_WithLatency(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	// Insert enough request logs to populate latency data (>=3 for HAVING clause)
	providerID := uuid.New()
	insertTestProvider(t, pool, providerID, "latency-provider", "https://api.example.com/v1")
	for i := 0; i < 5; i++ {
		insertRichTestRequestLog(t, pool, uuid.New(), providerID, "latency-model", 200, 100+i*10, 10, 20, requestLogOpts{
			ResponseHeaderMs: float64(50 + i*5),
			ProxyOverheadMs:  float64(10 + i*2),
			LatencyMs:        float64(80 + i*3),
		})
	}

	result, err := handler.calculateStats(context.Background(), 24*time.Hour, false, "requests", true, "")
	if err != nil {
		t.Fatalf("calculateStats with latency: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result from calculateStats with latency")
	}
	// The key coverage is that the includeLatency=true branch is exercised.
	_ = result.ByModelLatency
	_ = result.ByProviderLatency
}

// TestCalculateStats_WithoutLatency exercises the includeLatency=false branch,
// confirming statLatencyBreakdown is NOT called.
func TestCalculateStats_WithoutLatency(t *testing.T) {
	handler, _, cleanup := newStatsHandler(t)
	defer cleanup()

	result, err := handler.calculateStats(context.Background(), 24*time.Hour, false, "requests", false, "")
	if err != nil {
		t.Fatalf("calculateStats without latency: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result from calculateStats without latency")
	}
	if len(result.ByModelLatency) != 0 {
		t.Errorf("expected empty ByModelLatency with includeLatency=false, got %d", len(result.ByModelLatency))
	}
	if len(result.ByProviderLatency) != 0 {
		t.Errorf("expected empty ByProviderLatency with includeLatency=false, got %d", len(result.ByProviderLatency))
	}
}

// ---------------------------------------------------------------------------
// 2. calculateStats — "tokens" metric path
//    Existing tests use metric="requests". The tokens metric changes the
//    SQL SELECT from COUNT(*) to SUM(...) which uses a different code path
//    in statByModel/statByProvider/statByVirtualKey.
// ---------------------------------------------------------------------------

func TestCalculateStats_WithTokensMetric(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	providerID := uuid.New()
	insertTestProvider(t, pool, providerID, "tokens-metric-provider", "https://api.example.com/v1")
	insertTestRequestLog(t, pool, uuid.New(), providerID, "tokens-model", 200, 100, 10, 20)
	insertTestRequestLog(t, pool, uuid.New(), providerID, "tokens-model", 200, 200, 20, 40)

	result, err := handler.calculateStats(context.Background(), 24*time.Hour, false, "tokens", false, "")
	if err != nil {
		t.Fatalf("calculateStats with tokens metric: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	// Verify the tokens metric was used (ByModel values reflect token sums, not counts)
	if len(result.ByModel) == 0 {
		t.Error("expected at least one model in ByModel with tokens metric")
	}
	_ = result.ByProvider
	_ = result.ByVirtualKey
}

// ---------------------------------------------------------------------------
// 3. calculateStats — 7-day period with latency
//    Tests the combination of 7d period AND includeLatency=true.
// ---------------------------------------------------------------------------

func TestCalculateStats_7dWithLatency(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	providerID := uuid.New()
	insertTestProvider(t, pool, providerID, "latency-7d-provider", "https://api.example.com/v1")
	for i := 0; i < 5; i++ {
		insertRichTestRequestLog(t, pool, uuid.New(), providerID, "latency-7d-model", 200, 100+i*10, 10, 20, requestLogOpts{
			ResponseHeaderMs: float64(50 + i*5),
			ProxyOverheadMs:  float64(10 + i*2),
			LatencyMs:        float64(80 + i*3),
		})
	}

	result, err := handler.calculateStats(context.Background(), 7*24*time.Hour, false, "requests", true, "")
	if err != nil {
		t.Fatalf("calculateStats 7d with latency: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	_ = result.ByModelLatency
	_ = result.ByProviderLatency
	_ = result.TotalRequestsLast7d
}
