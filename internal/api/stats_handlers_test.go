package api

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func TestGetStats_24h(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	// Set up test data
	providerID := uuid.New()
	logID := uuid.New()
	insertTestProvider(t, pool, providerID, "test-provider-24h", "https://api.example.com/v1")
	insertTestRequestLog(t, pool, logID, providerID, "test-model", 200, 100, 10, 20)

	// Create router and call handler
	r := chi.NewRouter()
	handler.Register(r)

	req := httptest.NewRequest(http.MethodGet, "/stats?period=24h", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response StatsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if response.TotalRequestsLast24h != 1 {
		t.Errorf("Expected TotalRequestsLast24h=1, got %d", response.TotalRequestsLast24h)
	}
}

func TestGetStats_7d(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	// Set up test data
	providerID := uuid.New()
	logID := uuid.New()
	insertTestProvider(t, pool, providerID, "test-provider-7d", "https://api.example.com/v1")
	insertTestRequestLog(t, pool, logID, providerID, "test-model", 200, 100, 10, 20)

	// Create router and call handler
	r := chi.NewRouter()
	handler.Register(r)

	req := httptest.NewRequest(http.MethodGet, "/stats?period=7d", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response StatsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if response.TotalRequestsLast7d != 1 {
		t.Errorf("Expected TotalRequestsLast7d=1, got %d", response.TotalRequestsLast7d)
	}
}

func TestGetStats_TokensMetric(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	// Set up test data
	providerID := uuid.New()
	logID := uuid.New()
	insertTestProvider(t, pool, providerID, "test-provider-tokens", "https://api.example.com/v1")
	insertTestRequestLog(t, pool, logID, providerID, "test-model", 200, 100, 100, 200)

	// Create router and call handler
	r := chi.NewRouter()
	handler.Register(r)

	req := httptest.NewRequest(http.MethodGet, "/stats?metric=tokens", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response StatsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// With metric=tokens, ByModel and ByProvider should contain token counts
	if len(response.ByModel) == 0 {
		t.Error("Expected ByModel to have entries with metric=tokens")
	}
	if len(response.ByProvider) == 0 {
		t.Error("Expected ByProvider to have entries with metric=tokens")
	}
}

func TestGetStats_ExcludeDeleted(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	// Set up test data
	providerID := uuid.New()
	logID := uuid.New()
	insertTestProvider(t, pool, providerID, "test-provider-exclude", "https://api.example.com/v1")
	insertTestRequestLog(t, pool, logID, providerID, "test-model", 200, 100, 10, 20)

	// Create router and call handler
	r := chi.NewRouter()
	handler.Register(r)

	req := httptest.NewRequest(http.MethodGet, "/stats?exclude_deleted=true", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response StatsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// Should return stats successfully
	if response.TotalRequestsLast24h != 1 {
		t.Errorf("Expected TotalRequestsLast24h=1, got %d", response.TotalRequestsLast24h)
	}
}

func TestGetTimeSeries_24h(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	// Set up test data
	providerID := uuid.New()
	logID := uuid.New()
	insertTestProvider(t, pool, providerID, "test-provider-ts24h", "https://api.example.com/v1")
	insertTestRequestLog(t, pool, logID, providerID, "test-model", 200, 100, 10, 20)

	// Create router and call handler
	r := chi.NewRouter()
	handler.Register(r)

	req := httptest.NewRequest(http.MethodGet, "/stats/timeseries?period=24h", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response TimeSeriesStats
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// Should have time series points (may be filled with zeros for missing hours)
	if len(response.Points) == 0 {
		t.Error("Expected time series points")
	}
}

func TestGetTimeSeries_7d(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	// Set up test data
	providerID := uuid.New()
	logID := uuid.New()
	insertTestProvider(t, pool, providerID, "test-provider-ts7d", "https://api.example.com/v1")
	insertTestRequestLog(t, pool, logID, providerID, "test-model", 200, 100, 10, 20)

	// Create router and call handler
	r := chi.NewRouter()
	handler.Register(r)

	req := httptest.NewRequest(http.MethodGet, "/stats/timeseries?period=7d", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response TimeSeriesStats
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// With 7d period, should have daily buckets (up to 31 for 30d of panning data, inclusive range)
	if len(response.Points) == 0 {
		t.Error("Expected time series points for 7d period")
	}
	if len(response.Points) > 31 {
		t.Errorf("Expected at most 31 daily buckets, got %d", len(response.Points))
	}
}

func TestGetTimeSeries_CacheTokens(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	// Set up test data with cache hit/miss tokens
	providerID := uuid.New()
	insertTestProvider(t, pool, providerID, "test-provider-cache", "https://api.example.com/v1")

	logID := uuid.New()
	ctx := context.Background()
	_, err := pool.Exec(ctx, `
		INSERT INTO request_logs (id, provider_id, model_id, status_code, duration_ms, tokens_prompt, tokens_completion, tokens_prompt_cache_hit, tokens_prompt_cache_miss, created_at)
		VALUES ($1, $2, 'test-model', 200, 100, 50, 30, 40, 10, NOW())`,
		logID, providerID)
	if err != nil {
		t.Fatalf("Failed to insert test request log with cache tokens: %v", err)
	}

	// Create router and call handler
	r := chi.NewRouter()
	handler.Register(r)

	req := httptest.NewRequest(http.MethodGet, "/stats/timeseries?period=24h", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response TimeSeriesStats
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if len(response.Points) == 0 {
		t.Fatal("Expected time series points")
	}

	// Find the point with cache hit data
	var found bool
	for _, p := range response.Points {
		if p.TokensCacheHit == 40 && p.TokensCacheMiss == 10 {
			found = true
			break
		}
	}
	if !found {
		var hit, miss int
		for _, p := range response.Points {
			hit += p.TokensCacheHit
			miss += p.TokensCacheMiss
		}
		t.Errorf("Expected a point with tokens_cache_hit=40, tokens_cache_miss=10; got totals hit=%d miss=%d", hit, miss)
	}
}

func TestGetTimeSeries_CacheTokens_ZeroValues(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	providerID := uuid.New()
	insertTestProvider(t, pool, providerID, "test-provider-zero-cache", "https://api.example.com/v1")

	// Insert row with zero cache tokens
	logID := uuid.New()
	ctx := context.Background()
	_, err := pool.Exec(ctx, `
		INSERT INTO request_logs (id, provider_id, model_id, status_code, duration_ms, tokens_prompt, tokens_completion, tokens_prompt_cache_hit, tokens_prompt_cache_miss, created_at)
		VALUES ($1, $2, 'test-model', 200, 100, 50, 30, 0, 0, NOW())`,
		logID, providerID)
	if err != nil {
		t.Fatalf("Failed to insert test request log: %v", err)
	}

	r := chi.NewRouter()
	handler.Register(r)

	req := httptest.NewRequest(http.MethodGet, "/stats/timeseries?period=24h", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response TimeSeriesStats
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if len(response.Points) == 0 {
		t.Fatal("Expected time series points")
	}

	// Find the point with the log entry — cache fields should be 0
	var found bool
	for _, p := range response.Points {
		if p.Count > 0 && p.TokensCacheHit == 0 && p.TokensCacheMiss == 0 {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected a point with zero cache hit/miss tokens and count > 0")
	}
}

func TestGetTimeSeries_CacheTokens_MultiRowAggregation(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	providerID := uuid.New()
	insertTestProvider(t, pool, providerID, "test-provider-multi", "https://api.example.com/v1")

	// Insert three rows in the same bucket (same NOW() timestamp)
	ctx := context.Background()
	type cacheRow struct{ hit, miss int }
	for i, row := range []cacheRow{
		{40, 10},
		{20, 5},
		{0, 15},
	} {
		logID := uuid.New()
		_, err := pool.Exec(ctx, `
			INSERT INTO request_logs (id, provider_id, model_id, status_code, duration_ms, tokens_prompt, tokens_completion, tokens_prompt_cache_hit, tokens_prompt_cache_miss, created_at)
			VALUES ($1, $2, 'test-model', 200, 100, 50, 30, $3, $4, NOW())`,
			logID, providerID, row.hit, row.miss)
		if err != nil {
			t.Fatalf("Failed to insert test request log %d: %v", i, err)
		}
	}

	r := chi.NewRouter()
	handler.Register(r)

	req := httptest.NewRequest(http.MethodGet, "/stats/timeseries?period=24h", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response TimeSeriesStats
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// SUM(hit) = 40+20+0 = 60, SUM(miss) = 10+5+15 = 30
	var found bool
	for _, p := range response.Points {
		if p.TokensCacheHit == 60 && p.TokensCacheMiss == 30 {
			found = true
			break
		}
	}
	if !found {
		var hit, miss int
		for _, p := range response.Points {
			hit += p.TokensCacheHit
			miss += p.TokensCacheMiss
		}
		t.Errorf("Expected a point with cache_hit=60, cache_miss=30; got totals hit=%d miss=%d", hit, miss)
	}
}

func TestGetTimeSeries_CacheTokens_JSONRoundTrip(t *testing.T) {
	original := TimeSeriesPoint{
		Bucket:          "2025-06-01T12:00:00Z",
		Count:           3,
		Tokens:          150,
		TokensCacheHit:  60,
		TokensCacheMiss: 30,
		Errors:          0,
		Latency:         100,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	var decoded TimeSeriesPoint
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if decoded.TokensCacheHit != original.TokensCacheHit {
		t.Errorf("TokensCacheHit: got %d, want %d", decoded.TokensCacheHit, original.TokensCacheHit)
	}
	if decoded.TokensCacheMiss != original.TokensCacheMiss {
		t.Errorf("TokensCacheMiss: got %d, want %d", decoded.TokensCacheMiss, original.TokensCacheMiss)
	}

	// Verify JSON keys match the struct tags
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Failed to unmarshal to raw map: %v", err)
	}
	if _, ok := raw["tokens_cache_hit"]; !ok {
		t.Error("Missing tokens_cache_hit key in JSON output")
	}
	if _, ok := raw["tokens_cache_miss"]; !ok {
		t.Error("Missing tokens_cache_miss key in JSON output")
	}
}

func TestGetProviderDistribution_Integration(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	// Set up test data
	providerID := uuid.New()
	logID := uuid.New()
	insertTestProvider(t, pool, providerID, "test-provider-dist", "https://api.example.com/v1")
	insertTestRequestLog(t, pool, logID, providerID, "test-model", 200, 100, 10, 20)

	// Create router and call handler
	r := chi.NewRouter()
	handler.Register(r)

	req := httptest.NewRequest(http.MethodGet, "/stats/provider-distribution", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response ProviderDistributionStats
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// Should have provider distribution items
	if len(response.Items) == 0 {
		t.Error("Expected provider distribution items")
	}

	// Check that our test provider is in the results
	found := false
	for _, item := range response.Items {
		if item.Name == "test-provider-dist" {
			found = true
			if item.Count != 1 {
				t.Errorf("Expected Count=1 for test-provider-dist, got %d", item.Count)
			}
			break
		}
	}
	if !found {
		t.Error("Expected test-provider-dist in provider distribution")
	}
}

func TestGetStats_DeletedVirtualKey(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	providerID := uuid.New()
	insertTestProvider(t, pool, providerID, "test-provider-delvk", "https://api.example.com/v1")

	// Insert request log with a virtual_key_id that doesn't exist in virtual_keys table
	deletedVKID := uuid.New()
	logID := uuid.New()
	insertRichTestRequestLog(t, pool, logID, providerID, "test-model", 200, 100, 10, 20, requestLogOpts{
		VirtualKeyID: &deletedVKID,
	})

	r := chi.NewRouter()
	handler.Register(r)

	req := httptest.NewRequest(http.MethodGet, "/stats?period=24h", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response StatsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if response.ByVirtualKey["Deleted"] != 1 {
		t.Errorf("Expected ByVirtualKey['Deleted']=1, got %d", response.ByVirtualKey["Deleted"])
	}
}

func TestGetStats_ChatArenaKeys(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	providerID := uuid.New()
	insertTestProvider(t, pool, providerID, "test-provider-chat", "https://api.example.com/v1")

	// Insert request logs with chat and arena virtual_key_name
	chatLogID := uuid.New()
	insertRichTestRequestLog(t, pool, chatLogID, providerID, "test-model", 200, 100, 10, 20, requestLogOpts{
		VirtualKeyName: "chat",
	})
	arenaLogID := uuid.New()
	insertRichTestRequestLog(t, pool, arenaLogID, providerID, "test-model", 200, 100, 5, 10, requestLogOpts{
		VirtualKeyName: "arena",
	})

	r := chi.NewRouter()
	handler.Register(r)

	req := httptest.NewRequest(http.MethodGet, "/stats?period=24h", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response StatsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if response.ByVirtualKey["chat"] != 1 {
		t.Errorf("Expected ByVirtualKey['chat']=1, got %d", response.ByVirtualKey["chat"])
	}
	if response.ByVirtualKey["arena"] != 1 {
		t.Errorf("Expected ByVirtualKey['arena']=1, got %d", response.ByVirtualKey["arena"])
	}
}

func TestGetTimeSeries_ExcludeDeleted(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	providerID := uuid.New()
	insertTestProvider(t, pool, providerID, "test-provider-ts-del", "https://api.example.com/v1")

	// Insert one log with deleted VK and one without
	deletedVKID := uuid.New()
	logID1 := uuid.New()
	insertRichTestRequestLog(t, pool, logID1, providerID, "test-model", 200, 100, 10, 20, requestLogOpts{
		VirtualKeyID: &deletedVKID,
	})
	logID2 := uuid.New()
	insertTestRequestLog(t, pool, logID2, providerID, "test-model", 200, 100, 5, 10)

	r := chi.NewRouter()
	handler.Register(r)

	req := httptest.NewRequest(http.MethodGet, "/stats/timeseries?period=24h&exclude_deleted=true", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response TimeSeriesStats
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// With exclude_deleted=true, only the log without a deleted VK should be counted.
	// Total count across all points should be 1 (logID2), not 2.
	var totalCount int
	for _, p := range response.Points {
		totalCount += p.Count
	}
	if totalCount != 1 {
		t.Errorf("Expected total count=1 (deleted VK excluded), got %d", totalCount)
	}
}

func TestGetProviderDistribution_ExcludeDeleted(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	providerID := uuid.New()
	insertTestProvider(t, pool, providerID, "test-provider-pd-del", "https://api.example.com/v1")

	// Insert one log with deleted VK and one without
	deletedVKID := uuid.New()
	logID1 := uuid.New()
	insertRichTestRequestLog(t, pool, logID1, providerID, "test-model", 200, 100, 10, 20, requestLogOpts{
		VirtualKeyID: &deletedVKID,
	})
	logID2 := uuid.New()
	insertTestRequestLog(t, pool, logID2, providerID, "test-model", 200, 100, 5, 10)

	r := chi.NewRouter()
	handler.Register(r)

	req := httptest.NewRequest(http.MethodGet, "/stats/provider-distribution?period=24h&exclude_deleted=true", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response ProviderDistributionStats
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// With exclude_deleted=true, only the log without a deleted VK should be counted.
	// The provider should have Count=1, not 2.
	if len(response.Items) == 0 {
		t.Fatal("Expected provider distribution items")
	}
	for _, item := range response.Items {
		if item.Name == "test-provider-pd-del" {
			if item.Count != 1 {
				t.Errorf("Expected Count=1 (deleted VK excluded), got %d", item.Count)
			}
			break
		}
	}
}

func TestGetProviderDistribution_TokensMetric(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	providerID := uuid.New()
	insertTestProvider(t, pool, providerID, "test-provider-pd-tok", "https://api.example.com/v1")

	logID := uuid.New()
	insertTestRequestLog(t, pool, logID, providerID, "test-model", 200, 100, 10, 20)

	r := chi.NewRouter()
	handler.Register(r)

	req := httptest.NewRequest(http.MethodGet, "/stats/provider-distribution?period=24h&metric=tokens", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response ProviderDistributionStats
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if len(response.Items) == 0 {
		t.Fatal("Expected provider distribution items")
	}

	// With metric=tokens, Count should be 0 and Tokens should be > 0
	for _, item := range response.Items {
		if item.Name == "test-provider-pd-tok" {
			if item.Count != 0 {
				t.Errorf("Expected Count=0 for tokens metric, got %d", item.Count)
			}
			if item.Tokens <= 0 {
				t.Errorf("Expected Tokens>0 for tokens metric, got %d", item.Tokens)
			}
			break
		}
	}
}

func TestGetStats_ClosedPool(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	// Close pool before making request to trigger query error
	pool.Close()

	r := chi.NewRouter()
	handler.Register(r)

	req := httptest.NewRequest(http.MethodGet, "/stats?period=24h", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("Expected 500, got %d", rec.Code)
	}
}

func TestGetTimeSeries_ClosedPool(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	pool.Close()

	r := chi.NewRouter()
	handler.Register(r)

	req := httptest.NewRequest(http.MethodGet, "/stats/timeseries?period=24h", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("Expected 500, got %d", rec.Code)
	}
}

func TestGetProviderDistribution_ClosedPool(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	pool.Close()

	r := chi.NewRouter()
	handler.Register(r)

	req := httptest.NewRequest(http.MethodGet, "/stats/provider-distribution?period=24h", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("Expected 500, got %d", rec.Code)
	}
}

func TestGetStats_JSONEncodeError(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	// Insert data so calculateStats succeeds
	providerID := uuid.New()
	insertTestProvider(t, pool, providerID, "test-provider-json", "https://api.example.com/v1")
	logID := uuid.New()
	insertTestRequestLog(t, pool, logID, providerID, "test-model", 200, 100, 10, 20)

	w := &brokenResponseWriter{}
	r := httptest.NewRequest(http.MethodGet, "/stats?period=24h", http.NoBody)
	r.Header.Set("Authorization", "Bearer test-admin-token")

	handler.GetStats(w, r)

	if w.code != http.StatusInternalServerError {
		t.Errorf("Expected 500, got %d", w.code)
	}
}

func TestGetTimeSeries_JSONEncodeError(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	providerID := uuid.New()
	insertTestProvider(t, pool, providerID, "test-provider-ts-json", "https://api.example.com/v1")
	logID := uuid.New()
	insertTestRequestLog(t, pool, logID, providerID, "test-model", 200, 100, 10, 20)

	w := &brokenResponseWriter{}
	r := httptest.NewRequest(http.MethodGet, "/stats/timeseries?period=24h", http.NoBody)
	r.Header.Set("Authorization", "Bearer test-admin-token")

	handler.GetTimeSeries(w, r)

	if w.code != http.StatusInternalServerError {
		t.Errorf("Expected 500, got %d", w.code)
	}
}

func TestGetProviderDistribution_JSONEncodeError(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	providerID := uuid.New()
	insertTestProvider(t, pool, providerID, "test-provider-pd-json", "https://api.example.com/v1")
	logID := uuid.New()
	insertTestRequestLog(t, pool, logID, providerID, "test-model", 200, 100, 10, 20)

	w := &brokenResponseWriter{}
	r := httptest.NewRequest(http.MethodGet, "/stats/provider-distribution?period=24h", http.NoBody)
	r.Header.Set("Authorization", "Bearer test-admin-token")

	handler.GetProviderDistribution(w, r)

	if w.code != http.StatusInternalServerError {
		t.Errorf("Expected 500, got %d", w.code)
	}
}

// ---------------------------------------------------------------------------
// Additional test coverage for stats.go
// ---------------------------------------------------------------------------

func TestGetStats_MultipleProviders(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	// Insert 3 providers
	providerA := uuid.New()
	providerB := uuid.New()
	providerC := uuid.New()
	insertTestProvider(t, pool, providerA, "provider-a", "https://api.a.com/v1")
	insertTestProvider(t, pool, providerB, "provider-b", "https://api.b.com/v1")
	insertTestProvider(t, pool, providerC, "provider-c", "https://api.c.com/v1")

	// Insert request logs: 2 for A, 3 for B, 5 for C (total=10)
	for i := 0; i < 2; i++ {
		insertTestRequestLog(t, pool, uuid.New(), providerA, "model-a", 200, 100, 10, 20)
	}
	for i := 0; i < 3; i++ {
		insertTestRequestLog(t, pool, uuid.New(), providerB, "model-b", 200, 100, 10, 20)
	}
	for i := 0; i < 5; i++ {
		insertTestRequestLog(t, pool, uuid.New(), providerC, "model-c", 200, 100, 10, 20)
	}

	r := chi.NewRouter()
	handler.Register(r)

	req := httptest.NewRequest(http.MethodGet, "/stats?period=24h", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response StatsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if response.TotalRequestsLast24h != 10 {
		t.Errorf("Expected TotalRequestsLast24h=10, got %d", response.TotalRequestsLast24h)
	}

	if len(response.ByProvider) != 3 {
		t.Errorf("Expected ByProvider to have 3 entries, got %d", len(response.ByProvider))
	}

	// Check individual provider counts
	expectedCounts := map[string]int64{
		"provider-a": 2,
		"provider-b": 3,
		"provider-c": 5,
	}
	for name, expected := range expectedCounts {
		if response.ByProvider[name] != int64(expected) {
			t.Errorf("Expected ByProvider[%q]=%d, got %d", name, expected, response.ByProvider[name])
		}
	}
}

func TestGetStats_MultipleModels(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	providerID := uuid.New()
	insertTestProvider(t, pool, providerID, "test-provider", "https://api.example.com/v1")

	// Insert request logs for 3 different models
	insertTestRequestLog(t, pool, uuid.New(), providerID, "model-a", 200, 100, 10, 20)
	insertTestRequestLog(t, pool, uuid.New(), providerID, "model-b", 200, 100, 10, 20)
	insertTestRequestLog(t, pool, uuid.New(), providerID, "model-c", 200, 100, 10, 20)

	r := chi.NewRouter()
	handler.Register(r)

	req := httptest.NewRequest(http.MethodGet, "/stats?period=24h", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response StatsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if len(response.ByModel) != 3 {
		t.Errorf("Expected ByModel to have 3 entries, got %d", len(response.ByModel))
	}

	// ByModel uses format "provider_name/model_id"
	expectedModels := map[string]bool{
		"test-provider/model-a": true,
		"test-provider/model-b": true,
		"test-provider/model-c": true,
	}
	for model := range expectedModels {
		if _, ok := response.ByModel[model]; !ok {
			t.Errorf("Expected ByModel to contain %q, got keys: %v", model, response.ByModel)
		}
	}
}

func TestGetStats_RateLimitHits(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	providerID := uuid.New()
	insertTestProvider(t, pool, providerID, "test-provider", "https://api.example.com/v1")

	// Insert 3 request logs: 2 with status 200, 1 with status 429
	insertTestRequestLog(t, pool, uuid.New(), providerID, "model-a", 200, 100, 10, 20)
	insertTestRequestLog(t, pool, uuid.New(), providerID, "model-a", 200, 100, 10, 20)
	insertTestRequestLog(t, pool, uuid.New(), providerID, "model-a", 429, 100, 0, 0)

	r := chi.NewRouter()
	handler.Register(r)

	req := httptest.NewRequest(http.MethodGet, "/stats?period=24h", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response StatsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if response.TotalRequestsLast24h != 3 {
		t.Errorf("Expected TotalRequestsLast24h=3, got %d", response.TotalRequestsLast24h)
	}

	if response.RateLimitHits != 1 {
		t.Errorf("Expected RateLimitHits=1, got %d", response.RateLimitHits)
	}
}

func TestGetStats_ModelLatency(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	providerID := uuid.New()
	insertTestProvider(t, pool, providerID, "test-provider-latency", "https://api.example.com/v1")

	// Insert request logs for 3 different models
	// Model A: 3 requests, high latency (should rank #1)
	// Model B: 3 requests, medium latency (should rank #2)
	// Model C: 3 requests, low latency (should rank #3)
	// Model D: 2 requests only (should NOT appear due to HAVING COUNT(*) >= 3)

	// Model A - 3 requests with high latency
	for i := 0; i < 3; i++ {
		insertRichTestRequestLog(t, pool, uuid.New(), providerID, "model-a", 200, 300, 10, 20, requestLogOpts{
			ProxyOverheadMs: 30.0,
			LatencyMs:       270.0, // Provider latency = 270ms
		})
	}

	// Model B - 3 requests with medium latency
	for i := 0; i < 3; i++ {
		insertRichTestRequestLog(t, pool, uuid.New(), providerID, "model-b", 200, 200, 10, 20, requestLogOpts{
			ProxyOverheadMs: 20.0,
			LatencyMs:       180.0, // Provider latency = 180ms
		})
	}

	// Model C - 3 requests with low latency
	for i := 0; i < 3; i++ {
		insertRichTestRequestLog(t, pool, uuid.New(), providerID, "model-c", 200, 100, 10, 20, requestLogOpts{
			ProxyOverheadMs: 10.0,
			LatencyMs:       90.0, // Provider latency = 90ms
		})
	}

	// Model D - Only 2 requests (should be excluded by HAVING COUNT(*) >= 3)
	for i := 0; i < 2; i++ {
		insertRichTestRequestLog(t, pool, uuid.New(), providerID, "model-d", 200, 500, 10, 20, requestLogOpts{
			ProxyOverheadMs: 50.0,
			LatencyMs:       450.0,
		})
	}

	r := chi.NewRouter()
	handler.Register(r)

	req := httptest.NewRequest(http.MethodGet, "/stats?period=24h&include_latency=true", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response StatsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// Verify ByModelLatency is returned
	if len(response.ByModelLatency) == 0 {
		t.Fatal("Expected ByModelLatency to have entries")
	}

	// Should have exactly 3 models (model-a, model-b, model-c), not model-d
	if len(response.ByModelLatency) != 3 {
		t.Errorf("Expected ByModelLatency to have 3 entries (models with >= 3 requests), got %d", len(response.ByModelLatency))
	}

	// Verify sorted by total_ms descending: model-a (300ms) > model-b (200ms) > model-c (100ms)
	expectedOrder := []string{
		"test-provider-latency/model-a",
		"test-provider-latency/model-b",
		"test-provider-latency/model-c",
	}

	for i, expectedModel := range expectedOrder {
		if i >= len(response.ByModelLatency) {
			break
		}
		actualModel := response.ByModelLatency[i].ModelID
		if actualModel != expectedModel {
			t.Errorf("ByModelLatency[%d].ModelID = %q, want %q", i, actualModel, expectedModel)
		}
	}

	// Verify model-a entry details
	var modelA *ModelLatencyEntry
	for i := range response.ByModelLatency {
		if response.ByModelLatency[i].ModelID == "test-provider-latency/model-a" {
			modelA = &response.ByModelLatency[i]
			break
		}
	}

	if modelA == nil {
		t.Fatal("model-a not found in ByModelLatency")
	}

	// Verify request count
	if modelA.RequestCount != 3 {
		t.Errorf("model-a RequestCount = %d, want 3", modelA.RequestCount)
	}

	// Verify total_ms is approximately 300 (allowing for small timing variations)
	if modelA.TotalMs < 290 || modelA.TotalMs > 310 {
		t.Errorf("model-a TotalMs = %f, want ~300", modelA.TotalMs)
	}

	// Verify overhead_ms is approximately 30
	if modelA.OverheadMs < 25 || modelA.OverheadMs > 35 {
		t.Errorf("model-a OverheadMs = %f, want ~30", modelA.OverheadMs)
	}

	// Verify provider_ms is approximately 270
	if modelA.ProviderMs < 260 || modelA.ProviderMs > 280 {
		t.Errorf("model-a ProviderMs = %f, want ~270", modelA.ProviderMs)
	}

	// Verify overhead + provider ≈ total
	expectedTotal := modelA.OverheadMs + modelA.ProviderMs
	if math.Abs(expectedTotal-modelA.TotalMs) > 5 {
		t.Errorf("model-a: OverheadMs + ProviderMs = %f, but TotalMs = %f", expectedTotal, modelA.TotalMs)
	}
}

func TestGetStats_ErrorRate(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	providerID := uuid.New()
	insertTestProvider(t, pool, providerID, "test-provider", "https://api.example.com/v1")

	// Insert request logs: 3 with status 200, 1 with status 400, 1 with status 500
	insertTestRequestLog(t, pool, uuid.New(), providerID, "model-a", 200, 100, 10, 20)
	insertTestRequestLog(t, pool, uuid.New(), providerID, "model-a", 200, 100, 10, 20)
	insertTestRequestLog(t, pool, uuid.New(), providerID, "model-a", 200, 100, 10, 20)
	insertTestRequestLog(t, pool, uuid.New(), providerID, "model-a", 400, 100, 0, 0)
	insertTestRequestLog(t, pool, uuid.New(), providerID, "model-a", 500, 100, 0, 0)

	r := chi.NewRouter()
	handler.Register(r)

	req := httptest.NewRequest(http.MethodGet, "/stats?period=24h", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response StatsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// ErrorRate should be 0.4 (2 errors out of 5 requests)
	if response.ErrorRate <= 0 {
		t.Errorf("Expected ErrorRate > 0, got %f", response.ErrorRate)
	}
	// Allow some tolerance for floating point
	if response.ErrorRate < 0.35 || response.ErrorRate > 0.45 {
		t.Errorf("Expected ErrorRate around 0.4, got %f", response.ErrorRate)
	}
}

func TestGetStats_TTFTAndOverhead(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	providerID := uuid.New()
	insertTestProvider(t, pool, providerID, "test-provider", "https://api.example.com/v1")

	// Insert rich request logs with TTFT and overhead values (streaming=true for TTFT)
	insertRichTestRequestLog(t, pool, uuid.New(), providerID, "model-a", 200, 100, 10, 20, requestLogOpts{
		ResponseHeaderMs: 50.0,
		ProxyOverheadMs:  5.0,
		Streaming:        true,
	})
	insertRichTestRequestLog(t, pool, uuid.New(), providerID, "model-a", 200, 100, 10, 20, requestLogOpts{
		ResponseHeaderMs: 100.0,
		ProxyOverheadMs:  10.0,
		Streaming:        true,
	})
	insertRichTestRequestLog(t, pool, uuid.New(), providerID, "model-a", 200, 100, 10, 20, requestLogOpts{
		ResponseHeaderMs: 75.0,
		ProxyOverheadMs:  7.5,
		Streaming:        true,
	})

	r := chi.NewRouter()
	handler.Register(r)

	req := httptest.NewRequest(http.MethodGet, "/stats?period=24h", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response StatsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if response.AvgTTFTMs <= 0 {
		t.Errorf("Expected AvgTTFTMs > 0, got %f", response.AvgTTFTMs)
	}

	if response.AvgOverheadMs <= 0 {
		t.Errorf("Expected AvgOverheadMs > 0, got %f", response.AvgOverheadMs)
	}
}

func TestGetProviderDistribution_MultipleProviders_ShareRounding(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	// Insert 3 providers
	providerA := uuid.New()
	providerB := uuid.New()
	providerC := uuid.New()
	insertTestProvider(t, pool, providerA, "provider-a", "https://api.a.com/v1")
	insertTestProvider(t, pool, providerB, "provider-b", "https://api.b.com/v1")
	insertTestProvider(t, pool, providerC, "provider-c", "https://api.c.com/v1")

	// Insert request logs: 7 for A, 2 for B, 1 for C (total=10)
	for i := 0; i < 7; i++ {
		insertTestRequestLog(t, pool, uuid.New(), providerA, "model-a", 200, 100, 10, 20)
	}
	for i := 0; i < 2; i++ {
		insertTestRequestLog(t, pool, uuid.New(), providerB, "model-b", 200, 100, 10, 20)
	}
	insertTestRequestLog(t, pool, uuid.New(), providerC, "model-c", 200, 100, 10, 20)

	r := chi.NewRouter()
	handler.Register(r)

	req := httptest.NewRequest(http.MethodGet, "/stats/provider-distribution?period=24h", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response ProviderDistributionStats
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if len(response.Items) != 3 {
		t.Errorf("Expected 3 items, got %d", len(response.Items))
	}

	// Check share values sum to approximately 100.0
	var totalShare float64
	for _, item := range response.Items {
		totalShare += item.Share
	}
	if totalShare < 99.9 || totalShare > 100.1 {
		t.Errorf("Expected shares to sum to ~100.0, got %f", totalShare)
	}

	// Find provider-a and verify it has the largest share (~70%)
	var providerAShare float64
	for _, item := range response.Items {
		if item.Name == "provider-a" {
			providerAShare = item.Share
			if item.Count != 7 {
				t.Errorf("Expected provider-a Count=7, got %d", item.Count)
			}
			// Verify Count field for requests metric (Count > 0, Tokens == 0)
			if item.Tokens != 0 {
				t.Errorf("Expected provider-a Tokens=0 for requests metric, got %d", item.Tokens)
			}
			break
		}
	}
	if providerAShare < 65 || providerAShare > 75 {
		t.Errorf("Expected provider-a share around 70%%, got %f%%", providerAShare)
	}
}

func TestGetTimeSeries_TokensMetric(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	providerID := uuid.New()
	insertTestProvider(t, pool, providerID, "test-provider", "https://api.example.com/v1")

	// Insert request logs with prompt/completion tokens
	insertTestRequestLog(t, pool, uuid.New(), providerID, "model-a", 200, 100, 100, 200)
	insertTestRequestLog(t, pool, uuid.New(), providerID, "model-a", 200, 100, 150, 250)
	insertTestRequestLog(t, pool, uuid.New(), providerID, "model-a", 200, 100, 50, 100)

	r := chi.NewRouter()
	handler.Register(r)

	req := httptest.NewRequest(http.MethodGet, "/stats/timeseries?period=24h&metric=tokens", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response TimeSeriesStats
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if len(response.Points) == 0 {
		t.Fatal("Expected response.Points to have entries")
	}

	// Verify some points have Tokens > 0
	var hasTokens bool
	for _, p := range response.Points {
		if p.Tokens > 0 {
			hasTokens = true
			break
		}
	}
	if !hasTokens {
		t.Error("Expected some points to have Tokens > 0")
	}
}

func TestGetStats_VirtualKeyAggregation(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	providerID := uuid.New()
	insertTestProvider(t, pool, providerID, "test-provider", "https://api.example.com/v1")

	// Insert a virtual key
	vkID := uuid.New()
	ctx := context.Background()
	_, err := pool.Exec(ctx, `
		INSERT INTO virtual_keys (id, name, key_hash, key_preview, tokens_used, last_used_at, created_at)
		VALUES ($1, $2, $3, $4, 0, NOW(), NOW())`,
		vkID, "test-vk-name", "fakehash123", "preview...")
	if err != nil {
		t.Fatalf("Failed to insert virtual key: %v", err)
	}

	// Insert request logs: some with VK, some without
	insertRichTestRequestLog(t, pool, uuid.New(), providerID, "model-a", 200, 100, 10, 20, requestLogOpts{
		VirtualKeyID: &vkID,
	})
	insertRichTestRequestLog(t, pool, uuid.New(), providerID, "model-a", 200, 100, 10, 20, requestLogOpts{
		VirtualKeyID: &vkID,
	})
	insertTestRequestLog(t, pool, uuid.New(), providerID, "model-a", 200, 100, 10, 20) // no VK

	r := chi.NewRouter()
	handler.Register(r)

	req := httptest.NewRequest(http.MethodGet, "/stats?period=24h", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response StatsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if len(response.ByVirtualKey) == 0 {
		t.Error("Expected ByVirtualKey to have entries")
	}
}

func TestGetStats_MultipleVirtualKeys(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	providerID := uuid.New()
	insertTestProvider(t, pool, providerID, "test-provider", "https://api.example.com/v1")

	// Insert 2 virtual keys
	vk1ID := uuid.New()
	vk2ID := uuid.New()
	ctx := context.Background()
	_, err := pool.Exec(ctx, `
		INSERT INTO virtual_keys (id, name, key_hash, key_preview, tokens_used, last_used_at, created_at)
		VALUES ($1, $2, $3, $4, 0, NOW(), NOW())`,
		vk1ID, "vk-one", "hash1", "pre1")
	if err != nil {
		t.Fatalf("Failed to insert virtual key 1: %v", err)
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO virtual_keys (id, name, key_hash, key_preview, tokens_used, last_used_at, created_at)
		VALUES ($1, $2, $3, $4, 0, NOW(), NOW())`,
		vk2ID, "vk-two", "hash2", "pre2")
	if err != nil {
		t.Fatalf("Failed to insert virtual key 2: %v", err)
	}

	// Insert request logs for each VK
	insertRichTestRequestLog(t, pool, uuid.New(), providerID, "model-a", 200, 100, 10, 20, requestLogOpts{
		VirtualKeyID:   &vk1ID,
		VirtualKeyName: "vk-one",
	})
	insertRichTestRequestLog(t, pool, uuid.New(), providerID, "model-a", 200, 100, 10, 20, requestLogOpts{
		VirtualKeyID:   &vk2ID,
		VirtualKeyName: "vk-two",
	})

	r := chi.NewRouter()
	handler.Register(r)

	req := httptest.NewRequest(http.MethodGet, "/stats?period=24h", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response StatsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// Verify both VKs are in ByVirtualKey
	if response.ByVirtualKey["vk-one"] != 1 {
		t.Errorf("Expected ByVirtualKey['vk-one']=1, got %d", response.ByVirtualKey["vk-one"])
	}
	if response.ByVirtualKey["vk-two"] != 1 {
		t.Errorf("Expected ByVirtualKey['vk-two']=1, got %d", response.ByVirtualKey["vk-two"])
	}
}

// ---------------------------------------------------------------------------
// Additional coverage for remaining uncovered lines
// ---------------------------------------------------------------------------

// TestGetTimeSeries_1hPeriod tests the 5min bucket query path (period < 24h).
func TestGetTimeSeries_1hPeriod(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	providerID := uuid.New()
	insertTestProvider(t, pool, providerID, "test-provider-1h-ts", "https://api.example.com/v1")
	insertTestRequestLog(t, pool, uuid.New(), providerID, "test-model", 200, 100, 10, 20)

	r := chi.NewRouter()
	handler.Register(r)

	req := httptest.NewRequest(http.MethodGet, "/stats/timeseries?period=1h", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response TimeSeriesStats
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// With 1h period, should have 5min buckets (up to 288 buckets for 24h panning)
	if len(response.Points) == 0 {
		t.Error("Expected time series points for 1h period")
	}
}

// TestGetStats_1hPeriod_HTTP tests GetStats with ?period=1h through HTTP handler.
func TestGetStats_1hPeriod_HTTP(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	providerID := uuid.New()
	insertTestProvider(t, pool, providerID, "test-provider-1h-http", "https://api.example.com/v1")
	insertTestRequestLog(t, pool, uuid.New(), providerID, "test-model", 200, 100, 10, 20)

	r := chi.NewRouter()
	handler.Register(r)

	req := httptest.NewRequest(http.MethodGet, "/stats?period=1h", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response StatsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// Stats should be populated
	if response.TotalRequestsLast24h < 1 {
		t.Errorf("Expected TotalRequestsLast24h>=1, got %d", response.TotalRequestsLast24h)
	}
}

// TestGetProviderDistribution_RequestsMetric tests provider distribution with requests metric (default).
func TestGetProviderDistribution_RequestsMetric(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	providerID := uuid.New()
	insertTestProvider(t, pool, providerID, "test-provider-req-metric", "https://api.example.com/v1")
	insertTestRequestLog(t, pool, uuid.New(), providerID, "test-model", 200, 100, 10, 20)

	r := chi.NewRouter()
	handler.Register(r)

	// Default metric=requests (no metric param)
	req := httptest.NewRequest(http.MethodGet, "/stats/provider-distribution", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response ProviderDistributionStats
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if len(response.Items) == 0 {
		t.Fatal("Expected provider distribution items")
	}

	// With requests metric, Count should be > 0 and Tokens should be 0
	for _, item := range response.Items {
		if item.Name == "test-provider-req-metric" {
			if item.Count != 1 {
				t.Errorf("Expected Count=1 for requests metric, got %d", item.Count)
			}
			if item.Tokens != 0 {
				t.Errorf("Expected Tokens=0 for requests metric, got %d", item.Tokens)
			}
			break
		}
	}
}

// TestGetStats_EmptyDB tests stats with no request_logs (zero-result paths).
func TestGetStats_EmptyDB(t *testing.T) {
	handler, _, cleanup := newStatsHandler(t)
	defer cleanup()

	// No data inserted - all queries return zero/empty results

	r := chi.NewRouter()
	handler.Register(r)

	req := httptest.NewRequest(http.MethodGet, "/stats?period=24h", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response StatsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// All stats should be zero/empty
	if response.TotalRequestsLast24h != 0 {
		t.Errorf("Expected TotalRequestsLast24h=0, got %d", response.TotalRequestsLast24h)
	}
	if response.AvgLatencyMs != 0 {
		t.Errorf("Expected AvgLatencyMs=0, got %f", response.AvgLatencyMs)
	}
	if response.ErrorRate != 0 {
		t.Errorf("Expected ErrorRate=0, got %f", response.ErrorRate)
	}
	if response.AvgOverheadMs != 0 {
		t.Errorf("Expected AvgOverheadMs=0, got %f", response.AvgOverheadMs)
	}
	if response.TotalTokensPrompt != 0 {
		t.Errorf("Expected TotalTokensPrompt=0, got %d", response.TotalTokensPrompt)
	}
	if response.AvgTokensPerRequest != 0 {
		t.Errorf("Expected AvgTokensPerRequest=0, got %f", response.AvgTokensPerRequest)
	}
	if response.RateLimitHits != 0 {
		t.Errorf("Expected RateLimitHits=0, got %d", response.RateLimitHits)
	}
	if response.AvgTTFTMs != 0 {
		t.Errorf("Expected AvgTTFTMs=0, got %f", response.AvgTTFTMs)
	}
	if response.RequestsLast1h != 0 {
		t.Errorf("Expected RequestsLast1h=0, got %d", response.RequestsLast1h)
	}
}

// TestGetStats_RequestsLast1h tests the requests_last_1h field with recent data.
func TestGetStats_RequestsLast1h(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	providerID := uuid.New()
	insertTestProvider(t, pool, providerID, "test-provider-1h", "https://api.example.com/v1")

	// Insert a request log within the last hour (NOW())
	insertTestRequestLog(t, pool, uuid.New(), providerID, "test-model", 200, 100, 10, 20)

	r := chi.NewRouter()
	handler.Register(r)

	req := httptest.NewRequest(http.MethodGet, "/stats?period=24h", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response StatsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if response.RequestsLast1h < 1 {
		t.Errorf("Expected RequestsLast1h>=1, got %d", response.RequestsLast1h)
	}
}

// TestGetProviderDistribution_ShareRounding verifies the share rounding
// logic in GetProviderDistribution (lines 673-682).
func TestGetProviderDistribution_ShareRounding(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	// Insert 3 providers with equal request counts so each share is 33.3%,
	// which rounds to sum=99.9 and exercises the compensation logic
	providers := []struct {
		name  string
		count int
	}{
		{"prov-round-a", 1},
		{"prov-round-b", 1},
		{"prov-round-c", 1}, // 3 equal shares (33.3% each) → sum=99.9, exercises rounding compensation
	}

	for _, p := range providers {
		pid := uuid.New()
		insertTestProvider(t, pool, pid, p.name, "https://api.example.com/v1")
		for i := 0; i < p.count; i++ {
			insertTestRequestLog(t, pool, uuid.New(), pid, "model-x", 200, 100, 1, 1)
		}
	}

	r := chi.NewRouter()
	handler.Register(r)

	req := httptest.NewRequest(http.MethodGet, "/stats/provider-distribution?period=24h", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response ProviderDistributionStats
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if len(response.Items) != 3 {
		t.Fatalf("Expected 3 items, got %d", len(response.Items))
	}

	// Verify share sums to exactly 100.0
	var sum float64
	for _, item := range response.Items {
		sum += item.Share
	}
	if math.Abs(sum-100.0) > 0.01 {
		t.Errorf("Expected share sum=100.0, got %f", sum)
	}

	// Each share should be ~33.3%, and the first item should have the
	// rounding compensation applied so the total sums to 100.0.
	for _, item := range response.Items {
		if item.Share < 33.0 || item.Share > 34.0 {
			t.Errorf("Expected share ~33.3%%, got %f for %q", item.Share, item.Name)
		}
	}
}

// TestGetTimeSeries_CancelledContext tests the query error path (lines 525-528)
// and scan error path (lines 535-536) in GetTimeSeries.
func TestGetTimeSeries_CancelledContext(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	providerID := uuid.New()
	insertTestProvider(t, pool, providerID, "test-provider-ts-cancel", "https://api.example.com/v1")
	insertTestRequestLog(t, pool, uuid.New(), providerID, "test-model", 200, 100, 10, 20)

	r := chi.NewRouter()
	handler.Register(r)

	req := httptest.NewRequest(http.MethodGet, "/stats/timeseries?period=24h", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")

	// Use a context that is already cancelled
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	// Should get 500 because the query fails with cancelled context
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("Expected 500 for cancelled context, got %d", rec.Code)
	}
}

// TestGetProviderDistribution_CancelledContext tests the query error path
// (lines 632-635) and scan error path (lines 646-647) in GetProviderDistribution.
func TestGetProviderDistribution_CancelledContext(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	providerID := uuid.New()
	insertTestProvider(t, pool, providerID, "test-provider-pd-cancel", "https://api.example.com/v1")
	insertTestRequestLog(t, pool, uuid.New(), providerID, "test-model", 200, 100, 10, 20)

	r := chi.NewRouter()
	handler.Register(r)

	req := httptest.NewRequest(http.MethodGet, "/stats/provider-distribution?period=24h", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")

	// Use a context that is already cancelled
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	// Should get 500 because the query fails with cancelled context
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("Expected 500 for cancelled context, got %d", rec.Code)
	}
}

func TestGetTimeSeries_5minBucket(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	providerID := uuid.New()
	insertTestProvider(t, pool, providerID, "test-provider-5min", "https://api.example.com/v1")
	insertTestRequestLog(t, pool, uuid.New(), providerID, "test-model", 200, 100, 10, 20)

	r := chi.NewRouter()
	handler.Register(r)

	// 1h period triggers 5min bucket format
	req := httptest.NewRequest(http.MethodGet, "/stats/timeseries?period=1h", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response TimeSeriesStats
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// Verify bucket format matches YYYY-MM-DDTHH:MM:SSZ (5min truncated)
	for _, p := range response.Points {
		if p.Count > 0 {
			// Bucket should end with 'Z' and have format YYYY-MM-DDTHH:MM:SSZ
			if len(p.Bucket) != 20 || p.Bucket[19] != 'Z' {
				t.Errorf("Expected bucket format YYYY-MM-DDTHH:MM:SSZ, got %q", p.Bucket)
			}
		}
	}
}
