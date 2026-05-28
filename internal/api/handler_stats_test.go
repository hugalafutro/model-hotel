package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func TestGetStats(t *testing.T) {

	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/stats", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}
	// Should return some stats structure (may be empty)
}

func TestGetTimeSeries(t *testing.T) {

	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/stats/timeseries", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}
	// Should return timeseries data (may be empty)
}

func TestGetProviderDistribution(t *testing.T) {

	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/stats/provider-distribution", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}
	// Should return distribution data (may be empty)
}

// System Tests

func TestGetStats_WithLogs(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	// Create a provider first
	providerData := `{"name": "test-stats-provider", "base_url": "https://api.openai.com", "api_key": "test-api-key"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d", rec.Code)
	}

	var providerResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &providerResp); err != nil {
		t.Fatalf("Failed to parse provider response: %v", err)
	}

	// Insert some request logs directly
	now := time.Now().UTC()
	pool := h.Pool().Pool()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO request_logs (
			provider_id, model_id, virtual_key_id, status_code, duration_ms, 
			proxy_overhead_ms, tokens_prompt, tokens_completion, created_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9
		)`,
		providerResp.ID, "gpt-4", nil, 200, 1000, 50, 100, 200, now.Add(-1*time.Hour))
	if err != nil {
		t.Fatalf("Failed to insert request log: %v", err)
	}

	// Test stats endpoint
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/stats", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var stats StatsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &stats); err != nil {
		t.Fatalf("Failed to parse stats response: %v", err)
	}

	// Should have some calculated stats
	if stats.TotalRequestsLast24h == 0 {
		t.Error("Expected TotalRequestsLast24h to be > 0")
	}
	if stats.AvgLatencyMs == 0 {
		t.Error("Expected AvgLatencyMs to be > 0")
	}
	if stats.TotalTokensPrompt == 0 {
		t.Error("Expected TotalTokensPrompt to be > 0")
	}
}

// TestGetTimeSeries_DifferentPeriods tests timeseries with different period parameters

func TestGetTimeSeries_DifferentPeriods(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	// Create a provider first
	providerData := `{"name": "test-timeseries-provider", "base_url": "https://api.openai.com", "api_key": "test-api-key"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d", rec.Code)
	}

	var providerResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &providerResp); err != nil {
		t.Fatalf("Failed to parse provider response: %v", err)
	}

	// Insert some request logs at different times
	now := time.Now().UTC()
	pool := h.Pool().Pool()
	for i := 0; i < 5; i++ {
		_, err := pool.Exec(context.Background(), `
			INSERT INTO request_logs (
				provider_id, model_id, virtual_key_id, status_code, duration_ms, 
				proxy_overhead_ms, tokens_prompt, tokens_completion, created_at
			) VALUES (
				$1, $2, $3, $4, $5, $6, $7, $8, $9
			)`,
			providerResp.ID, "gpt-4", nil, 200, 1000, 50, 100, 200, now.Add(-time.Duration(i)*time.Hour))
		if err != nil {
			t.Fatalf("Failed to insert request log: %v", err)
		}
	}

	// Test different periods
	testCases := []struct {
		name   string
		period string
	}{
		{"1 hour", "1h"},
		{"1 day", "1d"},
		{"7 days", "7d"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest("GET", "/stats/timeseries?period="+tc.period, http.NoBody)
			req.Header.Set("Authorization", "Bearer test-admin-token")
			r.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("Expected 200 for period %s, got %d: %s", tc.period, rec.Code, rec.Body.String())
			}

			var response TimeSeriesStats
			if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
				t.Fatalf("Failed to parse timeseries response: %v", err)
			}

			// Should have some points
			if len(response.Points) == 0 {
				t.Errorf("Expected some time series points for period %s", tc.period)
			}
		})
	}
}

// TestGetProviderDistribution_WithLogs tests provider distribution with actual logs

func TestGetProviderDistribution_WithLogs(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	var rec *httptest.ResponseRecorder
	var req *http.Request

	// Create multiple providers
	providers := []string{"provider1", "provider2", "provider3"}
	providerIDs := make(map[string]string)

	for _, name := range providers {
		providerData := fmt.Sprintf(`{"name": %q, "base_url": "https://api.openai.com", "api_key": "test-api-key"}`, name)
		rec = httptest.NewRecorder()
		req = httptest.NewRequest("POST", "/providers", strings.NewReader(providerData))
		req.Header.Set("Authorization", "Bearer test-admin-token")
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("Failed to create provider %s: %d", name, rec.Code)
		}

		var providerResp struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &providerResp); err != nil {
			t.Fatalf("Failed to parse provider response: %v", err)
		}
		providerIDs[name] = providerResp.ID
	}

	// Insert request logs for different providers
	now := time.Now().UTC()
	pool := h.Pool().Pool()
	for name, providerID := range providerIDs {
		for i := 0; i < 3; i++ {
			_, err := pool.Exec(context.Background(), `
				INSERT INTO request_logs (
					provider_id, model_id, virtual_key_id, status_code, duration_ms, 
					proxy_overhead_ms, tokens_prompt, tokens_completion, created_at
				) VALUES (
					$1, $2, $3, $4, $5, $6, $7, $8, $9
				)`,
				providerID, "gpt-4", nil, 200, 1000, 50, 100, 200, now.Add(-time.Duration(i)*time.Hour))
			if err != nil {
				t.Fatalf("Failed to insert request log for provider %s: %v", name, err)
			}
		}
	}

	// Test provider distribution
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/stats/provider-distribution", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response ProviderDistributionStats
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse provider distribution response: %v", err)
	}

	// Should have distribution for multiple providers
	if len(response.Items) == 0 {
		t.Error("Expected provider distribution items")
	}

	// Check that shares sum to approximately 100
	totalShare := 0.0
	for _, item := range response.Items {
		totalShare += item.Share
	}

	if totalShare < 99.9 || totalShare > 100.1 {
		t.Errorf("Expected total share to be ~100, got %.1f", totalShare)
	}
}

// TestCalculateStats_Empty tests stats endpoint when there are no logs

func TestCalculateStats_Empty(t *testing.T) {
	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/stats", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var stats StatsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &stats); err != nil {
		t.Fatalf("Failed to parse stats response: %v", err)
	}

	// All stats should be 0 when there are no logs
	if stats.TotalRequestsLast24h != 0 {
		t.Errorf("Expected TotalRequestsLast24h to be 0, got %d", stats.TotalRequestsLast24h)
	}
	if stats.TotalRequestsLast7d != 0 {
		t.Errorf("Expected TotalRequestsLast7d to be 0, got %d", stats.TotalRequestsLast7d)
	}
	if stats.AvgLatencyMs != 0 {
		t.Errorf("Expected AvgLatencyMs to be 0, got %f", stats.AvgLatencyMs)
	}
	if stats.ErrorRate != 0 {
		t.Errorf("Expected ErrorRate to be 0, got %f", stats.ErrorRate)
	}
}

// App Logs Handler Tests

func TestGetStats_Empty(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	req := httptest.NewRequest("GET", "/stats", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
	}

	var response map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Verify response structure exists
	if response["by_model"] == nil {
		t.Error("expected 'by_model' in response")
	}
	if response["by_provider"] == nil {
		t.Error("expected 'by_provider' in response")
	}
	if response["by_virtual_key"] == nil {
		t.Error("expected 'by_virtual_key' in response")
	}
}

// TestListProviders_SearchFilter_Integration tests listing providers (search filter not implemented)

func TestGetStats_WithQueryParams_Integration(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	// Create a provider first (FK constraint)
	providerData := fmt.Sprintf(`{"name":"test-stats-prov-%s","base_url":"https://api.openai.com","api_key":"sk-test"}`, uuid.New().String()[:8])
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d: %s", rec.Code, rec.Body.String())
	}

	var providerResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &providerResp); err != nil {
		t.Fatalf("Failed to parse provider response: %v", err)
	}

	// Insert request_logs with various data
	now := time.Now().UTC()
	pool := h.Pool().Pool()

	// Insert logs for last 24 hours with different metrics
	for i := 0; i < 5; i++ {
		_, err := pool.Exec(context.Background(), `
			INSERT INTO request_logs (
				provider_id, model_id, status_code, duration_ms, 
				tokens_prompt, tokens_completion, created_at, 
				proxy_overhead_ms
			) VALUES (
				$1, $2, $3, $4, $5, $6, $7, $8
			)`,
			providerResp.ID, "gpt-4", 200, 1000.0,
			100+i*10, 200+i*20, now.Add(-time.Duration(i)*time.Hour),
			50.0)
		if err != nil {
			t.Fatalf("Failed to insert request log: %v", err)
		}
	}

	// Test with period=7d&metric=tokens&exclude_deleted=true
	t.Run("period_7d_metric_tokens", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/stats?period=7d&metric=tokens&exclude_deleted=true", http.NoBody)
		req.Header.Set("Authorization", "Bearer test-admin-token")
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
		}

		var stats StatsResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &stats); err != nil {
			t.Fatalf("Failed to parse stats response: %v", err)
		}

		// Should have calculated tokens
		if stats.TotalTokensPrompt == 0 {
			t.Error("Expected TotalTokensPrompt > 0")
		}
		if stats.TotalTokensCompletion == 0 {
			t.Error("Expected TotalTokensCompletion > 0")
		}
	})

	// Test with period=1h
	t.Run("period_1h", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/stats?period=1h", http.NoBody)
		req.Header.Set("Authorization", "Bearer test-admin-token")
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
		}

		var stats StatsResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &stats); err != nil {
			t.Fatalf("Failed to parse stats response: %v", err)
		}

		// 1h period should have fewer or equal requests than 24h
		if stats.TotalRequestsLast24h < stats.RequestsLast1h {
			t.Error("Expected 24h requests >= 1h requests")
		}
	})

	// Test with metric=requests (default)
	t.Run("metric_requests", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/stats?metric=requests", http.NoBody)
		req.Header.Set("Authorization", "Bearer test-admin-token")
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
		}

		var stats StatsResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &stats); err != nil {
			t.Fatalf("Failed to parse stats response: %v", err)
		}

		// Should have request counts
		if stats.TotalRequestsLast24h == 0 {
			t.Error("Expected TotalRequestsLast24h > 0")
		}
	})

	// Test with exclude_deleted=false
	t.Run("exclude_deleted_false", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/stats?exclude_deleted=false", http.NoBody)
		req.Header.Set("Authorization", "Bearer test-admin-token")
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
		}

		var stats StatsResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &stats); err != nil {
			t.Fatalf("Failed to parse stats response: %v", err)
		}

		// Should return stats (exclude_deleted=false is default behavior)
		if stats.TotalRequestsLast24h == 0 {
			t.Error("Expected TotalRequestsLast24h > 0")
		}
	})
}

// TestStreamEvents_WithTypeFilter_Integration tests /events endpoint with type filter

func TestGetStats_WithFilters_Integration(t *testing.T) {
	h, router := newTestHandlerWithRouter(t)

	// Create a provider first
	provBody := fmt.Sprintf(`{"name":"stats-test-%s","base_url":"https://api.example.com/v1","api_key":"sk-test"}`, uuid.New().String()[:8])
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(provBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d", w.Code)
	}

	var createResp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &createResp)
	provIDStr := createResp["id"].(string)
	provUUID, _ := uuid.Parse(provIDStr)

	// Insert request log
	_, _ = h.dbPool.Pool().Exec(context.Background(),
		`INSERT INTO request_logs (id, provider_id, model_id, virtual_key_id, tokens_prompt, tokens_completion, status_code, latency_ms, created_at)
		 VALUES ($1, $2, 'gpt-4', NULL, 50, 25, 200, 100, NOW())`,
		uuid.New(), provUUID)

	// Get stats with metric filter
	req = httptest.NewRequest("GET", "/stats?period=30d&metric=tokens", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// Virtual Key Update Tests

func TestGetStats_WithExcludeDeleted(t *testing.T) {
	h, router := newTestHandlerWithRouter(t)

	// Create a provider first
	provBody := fmt.Sprintf(`{"name":"stats-exclude-deleted-%s","base_url":"https://api.example.com/v1","api_key":"sk-test"}`, uuid.New().String()[:8])
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(provBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d", w.Code)
	}

	var createResp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &createResp)
	provIDStr := createResp["id"].(string)
	provUUID, _ := uuid.Parse(provIDStr)

	// Insert request logs
	pool := h.dbPool.Pool()
	now := time.Now().UTC()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO request_logs (id, provider_id, model_id, virtual_key_id, status_code, duration_ms, proxy_overhead_ms, tokens_prompt, tokens_completion, created_at)
		VALUES ($1, $2, 'gpt-4', NULL, 200, 1000, 50, 100, 200, $3)`,
		uuid.New(), provUUID, now.Add(-1*time.Hour))
	if err != nil {
		t.Fatalf("Failed to insert request log: %v", err)
	}

	// Test with exclude_deleted=true
	req = httptest.NewRequest("GET", "/stats?exclude_deleted=true", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var stats StatsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &stats); err != nil {
		t.Fatalf("Failed to parse stats response: %v", err)
	}
	if stats.TotalRequestsLast24h == 0 {
		t.Error("Expected TotalRequestsLast24h > 0")
	}
}

func TestGetStats_WithMetricTokens(t *testing.T) {
	h, router := newTestHandlerWithRouter(t)

	// Create a provider first
	provBody := fmt.Sprintf(`{"name":"stats-metric-tokens-%s","base_url":"https://api.example.com/v1","api_key":"sk-test"}`, uuid.New().String()[:8])
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(provBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d", w.Code)
	}

	var createResp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &createResp)
	provIDStr := createResp["id"].(string)
	provUUID, _ := uuid.Parse(provIDStr)

	// Create a virtual key
	vkBody := `{"name":"test-metric-tokens-key"}`
	req = httptest.NewRequest("POST", "/virtual-keys", strings.NewReader(vkBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("Failed to create virtual key: %d", w.Code)
	}

	var vkResp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &vkResp)
	vkIDStr := vkResp["id"].(string)
	vkUUID, _ := uuid.Parse(vkIDStr)

	// Insert request logs with token counts using the virtual key
	pool := h.dbPool.Pool()
	now := time.Now().UTC()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO request_logs (id, provider_id, model_id, virtual_key_id, status_code, duration_ms, proxy_overhead_ms, tokens_prompt, tokens_completion, created_at)
		VALUES ($1, $2, 'gpt-4', $3, 200, 1000, 50, 100, 200, $4)`,
		uuid.New(), provUUID, vkUUID, now.Add(-1*time.Hour))
	if err != nil {
		t.Fatalf("Failed to insert request log: %v", err)
	}

	// Test with metric=tokens
	req = httptest.NewRequest("GET", "/stats?metric=tokens", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var stats StatsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &stats); err != nil {
		t.Fatalf("Failed to parse stats response: %v", err)
	}
	if len(stats.ByModel) == 0 {
		t.Error("Expected ByModel to be populated with metric=tokens")
	}
	if len(stats.ByProvider) == 0 {
		t.Error("Expected ByProvider to be populated with metric=tokens")
	}
	if len(stats.ByVirtualKey) == 0 {
		t.Error("Expected ByVirtualKey to be populated with metric=tokens")
	}
}

func TestGetStats_Period7d(t *testing.T) {
	h, router := newTestHandlerWithRouter(t)

	// Create a provider first
	provBody := fmt.Sprintf(`{"name":"stats-period-7d-%s","base_url":"https://api.example.com/v1","api_key":"sk-test"}`, uuid.New().String()[:8])
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(provBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d", w.Code)
	}

	var createResp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &createResp)
	provIDStr := createResp["id"].(string)
	provUUID, _ := uuid.Parse(provIDStr)

	// Insert request logs
	pool := h.dbPool.Pool()
	now := time.Now().UTC()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO request_logs (id, provider_id, model_id, virtual_key_id, status_code, duration_ms, proxy_overhead_ms, tokens_prompt, tokens_completion, created_at)
		VALUES ($1, $2, 'gpt-4', NULL, 200, 1000, 50, 100, 200, $3)`,
		uuid.New(), provUUID, now.Add(-2*24*time.Hour))
	if err != nil {
		t.Fatalf("Failed to insert request log: %v", err)
	}

	// Test with period=7d
	req = httptest.NewRequest("GET", "/stats?period=7d", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var stats StatsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &stats); err != nil {
		t.Fatalf("Failed to parse stats response: %v", err)
	}
	if stats.TotalRequestsLast7d == 0 {
		t.Error("Expected TotalRequestsLast7d > 0")
	}
}

func TestGetStats_Period1h(t *testing.T) {
	h, router := newTestHandlerWithRouter(t)

	// Create a provider first
	provBody := fmt.Sprintf(`{"name":"stats-period-1h-%s","base_url":"https://api.example.com/v1","api_key":"sk-test"}`, uuid.New().String()[:8])
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(provBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d", w.Code)
	}

	var createResp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &createResp)
	provIDStr := createResp["id"].(string)
	provUUID, _ := uuid.Parse(provIDStr)

	// Insert request logs
	pool := h.dbPool.Pool()
	now := time.Now().UTC()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO request_logs (id, provider_id, model_id, virtual_key_id, status_code, duration_ms, proxy_overhead_ms, tokens_prompt, tokens_completion, created_at)
		VALUES ($1, $2, 'gpt-4', NULL, 200, 1000, 50, 100, 200, $3)`,
		uuid.New(), provUUID, now.Add(-30*time.Minute))
	if err != nil {
		t.Fatalf("Failed to insert request log: %v", err)
	}

	// Test with period=1h
	req = httptest.NewRequest("GET", "/stats?period=1h", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var stats StatsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &stats); err != nil {
		t.Fatalf("Failed to parse stats response: %v", err)
	}
	if stats.RequestsLast1h == 0 {
		t.Error("Expected RequestsLast1h > 0")
	}
}

func TestGetStats_WithChatLogs(t *testing.T) {
	h, router := newTestHandlerWithRouter(t)

	// Create a provider first
	provBody := fmt.Sprintf(`{"name":"stats-chat-logs-%s","base_url":"https://api.example.com/v1","api_key":"sk-test"}`, uuid.New().String()[:8])
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(provBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d", w.Code)
	}

	var createResp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &createResp)
	provIDStr := createResp["id"].(string)
	provUUID, _ := uuid.Parse(provIDStr)

	// Insert request logs with virtual_key_name = 'chat'
	pool := h.dbPool.Pool()
	now := time.Now().UTC()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO request_logs (id, provider_id, model_id, virtual_key_id, virtual_key_name, status_code, duration_ms, proxy_overhead_ms, tokens_prompt, tokens_completion, created_at)
		VALUES ($1, $2, 'gpt-4', NULL, 'chat', 200, 1000, 50, 100, 200, $3)`,
		uuid.New(), provUUID, now.Add(-1*time.Hour))
	if err != nil {
		t.Fatalf("Failed to insert request log: %v", err)
	}

	// Test stats endpoint
	req = httptest.NewRequest("GET", "/stats", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var stats StatsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &stats); err != nil {
		t.Fatalf("Failed to parse stats response: %v", err)
	}
	if _, ok := stats.ByVirtualKey["chat"]; !ok {
		t.Error("Expected ByVirtualKey to contain 'chat' entry")
	}
}

func TestGetProviderDistribution_WithMetricTokens(t *testing.T) {
	h, router := newTestHandlerWithRouter(t)

	// Create providers
	provBody := fmt.Sprintf(`{"name":"prov-dist-tokens-%s","base_url":"https://api.example.com/v1","api_key":"sk-test"}`, uuid.New().String()[:8])
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(provBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d", w.Code)
	}

	var createResp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &createResp)
	provIDStr := createResp["id"].(string)
	provUUID, _ := uuid.Parse(provIDStr)

	// Insert request logs with token counts
	pool := h.dbPool.Pool()
	now := time.Now().UTC()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO request_logs (id, provider_id, model_id, virtual_key_id, status_code, duration_ms, proxy_overhead_ms, tokens_prompt, tokens_completion, created_at)
		VALUES ($1, $2, 'gpt-4', NULL, 200, 1000, 50, 100, 200, $3)`,
		uuid.New(), provUUID, now.Add(-1*time.Hour))
	if err != nil {
		t.Fatalf("Failed to insert request log: %v", err)
	}

	// Test with metric=tokens
	req = httptest.NewRequest("GET", "/stats/provider-distribution?metric=tokens", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var dist ProviderDistributionStats
	if err := json.Unmarshal(w.Body.Bytes(), &dist); err != nil {
		t.Fatalf("Failed to parse distribution response: %v", err)
	}
	if len(dist.Items) == 0 {
		t.Fatal("Expected items in distribution response")
	}
	// With metric=tokens, Tokens should be > 0
	if dist.Items[0].Tokens == 0 {
		t.Error("Expected Tokens > 0 with metric=tokens")
	}
}

func TestGetTimeSeries_WithExcludeDeleted(t *testing.T) {
	h, router := newTestHandlerWithRouter(t)

	// Create a provider first
	provBody := fmt.Sprintf(`{"name":"timeseries-exclude-deleted-%s","base_url":"https://api.example.com/v1","api_key":"sk-test"}`, uuid.New().String()[:8])
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(provBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d", w.Code)
	}

	var createResp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &createResp)
	provIDStr := createResp["id"].(string)
	provUUID, _ := uuid.Parse(provIDStr)

	// Insert request logs
	pool := h.dbPool.Pool()
	now := time.Now().UTC()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO request_logs (id, provider_id, model_id, virtual_key_id, status_code, duration_ms, proxy_overhead_ms, tokens_prompt, tokens_completion, created_at)
		VALUES ($1, $2, 'gpt-4', NULL, 200, 1000, 50, 100, 200, $3)`,
		uuid.New(), provUUID, now.Add(-1*time.Hour))
	if err != nil {
		t.Fatalf("Failed to insert request log: %v", err)
	}

	// Test with exclude_deleted=true
	req = httptest.NewRequest("GET", "/stats/timeseries?exclude_deleted=true", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var ts TimeSeriesStats
	if err := json.Unmarshal(w.Body.Bytes(), &ts); err != nil {
		t.Fatalf("Failed to parse time series response: %v", err)
	}
	if len(ts.Points) == 0 {
		t.Error("Expected time series points")
	}
}

func TestGetProviderDistribution_WithExcludeDeleted(t *testing.T) {
	h, router := newTestHandlerWithRouter(t)

	// Create a provider first
	provBody := fmt.Sprintf(`{"name":"prov-dist-exclude-deleted-%s","base_url":"https://api.example.com/v1","api_key":"sk-test"}`, uuid.New().String()[:8])
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(provBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d", w.Code)
	}

	var createResp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &createResp)
	provIDStr := createResp["id"].(string)
	provUUID, _ := uuid.Parse(provIDStr)

	// Insert request logs
	pool := h.dbPool.Pool()
	now := time.Now().UTC()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO request_logs (id, provider_id, model_id, virtual_key_id, status_code, duration_ms, proxy_overhead_ms, tokens_prompt, tokens_completion, created_at)
		VALUES ($1, $2, 'gpt-4', NULL, 200, 1000, 50, 100, 200, $3)`,
		uuid.New(), provUUID, now.Add(-1*time.Hour))
	if err != nil {
		t.Fatalf("Failed to insert request log: %v", err)
	}

	// Test with exclude_deleted=true
	req = httptest.NewRequest("GET", "/stats/provider-distribution?exclude_deleted=true", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var dist ProviderDistributionStats
	if err := json.Unmarshal(w.Body.Bytes(), &dist); err != nil {
		t.Fatalf("Failed to parse distribution response: %v", err)
	}
	if len(dist.Items) == 0 {
		t.Error("Expected items in distribution response")
	}
}

// Ollama Cloud Account Tests
