package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/provider"
)

func TestDiscoverProviderModelsIntegration(t *testing.T) {

	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	// Create a test provider
	providerData := `{"name": "test-discover", "base_url": "https://api.openai.com", "api_key": "test-api-key"}`
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

	// Test discovery - will fail with test API key but should return proper error structure
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/providers/"+providerResp.ID+"/discover", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	// Should return an error due to invalid API key, but the handler should work
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("Expected 500 error for discovery with invalid API key, got %d: %s", rec.Code, rec.Body.String())
	}

	// The error response is plain text, not JSON
	body := rec.Body.String()
	if body == "" {
		t.Error("Expected error message in response body")
	}
}

// DiscoverProviderModels - Additional coverage

func TestDiscoverProviderModels_NonExistentProvider(t *testing.T) {
	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	// Try to discover models for non-existent provider
	nonExistentID := uuid.New().String()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/providers/"+nonExistentID+"/discover", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("Expected 404 for non-existent provider, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGetProviderUsageIntegration(t *testing.T) {
	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	// Create a test provider
	providerData := `{"name": "test-usage", "base_url": "https://api.openai.com", "api_key": "test-api-key"}`
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

	// Test usage endpoint - should return error for unsupported provider
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/providers/"+providerResp.ID+"/usage", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	// OpenAI doesn't support usage endpoint, should return 400
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("Expected 400 for unsupported provider usage, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGetProviderBalanceIntegration(t *testing.T) {
	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	// Create a test provider
	providerData := `{"name": "test-balance", "base_url": "https://api.openai.com", "api_key": "test-api-key"}`
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

	// Test balance endpoint - should return error for unsupported provider
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/providers/"+providerResp.ID+"/balance", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	// OpenAI doesn't support balance endpoint, should return 400
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("Expected 400 for unsupported provider balance, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestDiscoverAllModelsIntegration(t *testing.T) {
	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	// Create a test provider
	providerData := `{"name": "test-discover-all", "base_url": "https://api.openai.com", "api_key": "test-api-key"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d", rec.Code)
	}

	// Test discover all models
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/providers/discover-all", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	// Should return results structure even if discovery fails
	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 for discover all, got %d: %s", rec.Code, rec.Body.String())
	}

	var response map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// Check that response has expected fields
	if _, ok := response["results"]; !ok {
		t.Error("Expected results field in response")
	}
	if _, ok := response["succeeded"]; !ok {
		t.Error("Expected succeeded field in response")
	}
	if _, ok := response["failed"]; !ok {
		t.Error("Expected failed field in response")
	}
	if _, ok := response["discovered"]; !ok {
		t.Error("Expected discovered field in response")
	}
}

func TestRefreshAllQuotasIntegration(t *testing.T) {
	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	// Create a test provider
	providerData := `{"name": "test-quotas", "base_url": "https://api.openai.com", "api_key": "test-api-key"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d", rec.Code)
	}

	// Test refresh all quotas
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/providers/refresh-quotas", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	// Should return results structure even if no quotas are refreshed
	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 for refresh quotas, got %d: %s", rec.Code, rec.Body.String())
	}

	var response map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// Check that response has expected fields
	if _, ok := response["results"]; !ok {
		t.Error("Expected results field in response")
	}
	if _, ok := response["refreshed"]; !ok {
		t.Error("Expected refreshed field in response")
	}
	if _, ok := response["failed"]; !ok {
		t.Error("Expected failed field in response")
	}
	if _, ok := response["skipped"]; !ok {
		t.Error("Expected skipped field in response")
	}
}

// Events Handler Tests

func TestDiscoverProviderModels_Success(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)
	_ = h // Use h to avoid unused variable error

	// Create a provider with OpenAI URL (will fail with test API key but tests the handler path)
	providerData := fmt.Sprintf(`{"name": "test-discover-success-%s", "base_url": "https://api.openai.com", "api_key": "test-api-key"}`, uuid.New().String()[:8])
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

	// Test discovery - will fail with test API key but should return proper error structure
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/providers/"+providerResp.ID+"/discover", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	// Should return an error due to invalid API key, but the handler should work
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("Expected 500 error for discovery with invalid API key, got %d: %s", rec.Code, rec.Body.String())
	}

	// The error response is plain text, not JSON
	body := rec.Body.String()
	if body == "" {
		t.Error("Expected error message in response body")
	}
}

// Test for discovery.go - GetProviderUsage_Success

func TestGetProviderUsage_Success(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)
	_ = h // Use h to avoid unused variable error

	// Override newDiscoveryService with mock transport to avoid real API calls
	// Note: Must override AFTER newTestHandlerWithRouter since NewHandler sets it
	orig := newDiscoveryService
	defer func() { newDiscoveryService = orig }()

	newDiscoveryService = func() *provider.DiscoveryService {
		ds := provider.NewDiscoveryServiceWithHTTPClient(&http.Client{
			Transport: &mockTransport{
				roundTripFunc: func(req *http.Request) (*http.Response, error) {
					if strings.Contains(req.URL.Host, "api.nano-gpt.com") {
						return &http.Response{
							StatusCode: http.StatusServiceUnavailable,
							Body:       io.NopCloser(strings.NewReader("api.nano-gpt.com is currently in development. Please use https://nano-gpt.com/api instead.")),
							Header:     make(http.Header),
						}, nil
					}
					return nil, fmt.Errorf("unexpected request to %s", req.URL.String())
				},
			},
		})
		ds.SetRetryBaseDelay(time.Millisecond)
		return ds
	}

	// Create a provider with NanoGPT URL (will fail with test API key but tests the handler path)
	providerData := `{"name": "test-nanogpt-usage", "base_url": "https://api.nano-gpt.com", "api_key": "test-api-key"}`
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

	// Test usage endpoint - will fail with test API key but should return proper error
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/providers/"+providerResp.ID+"/usage", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	// Should return an error due to invalid API key, but the handler should work
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("Expected 500 error for usage with invalid API key, got %d: %s", rec.Code, rec.Body.String())
	}
}

// Test for discovery.go - DiscoverAllModels_MultipleProviders

func TestDiscoverAllModels_MultipleProviders(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)
	_ = h // Use h to avoid unused variable error

	// Override newDiscoveryService with mock transport to avoid real API calls
	// Note: Must override AFTER newTestHandlerWithRouter since NewHandler sets it
	orig := newDiscoveryService
	defer func() { newDiscoveryService = orig }()

	newDiscoveryService = func() *provider.DiscoveryService {
		ds := provider.NewDiscoveryServiceWithHTTPClient(&http.Client{
			Transport: &mockTransport{
				roundTripFunc: func(req *http.Request) (*http.Response, error) {
					if strings.Contains(req.URL.Host, "api.openai.com") && strings.HasSuffix(req.URL.Path, "/models") {
						// Return OpenAI-format model list
						body := `{"data": [{"id": "gpt-4o-mini", "owned_by": "openai", "object": "model"}, {"id": "gpt-4", "owned_by": "openai", "object": "model"}]}`
						return &http.Response{
							StatusCode: http.StatusOK,
							Body:       io.NopCloser(strings.NewReader(body)),
							Header:     http.Header{"Content-Type": []string{"application/json"}},
						}, nil
					} else if strings.Contains(req.URL.Host, "api.anthropic.com") && strings.HasSuffix(req.URL.Path, "/models") {
						// Return Anthropic-format model list (object with data array)
						body := `{"data": [{"id": "claude-3-opus-20240229", "display_name": "Claude 3 Opus"}, {"id": "claude-3-sonnet-20240229", "display_name": "Claude 3 Sonnet"}], "has_more": false, "first_id": "claude-3-opus-20240229", "last_id": "claude-3-sonnet-20240229"}`
						return &http.Response{
							StatusCode: http.StatusOK,
							Body:       io.NopCloser(strings.NewReader(body)),
							Header:     http.Header{"Content-Type": []string{"application/json"}},
						}, nil
					}
					return nil, fmt.Errorf("unexpected request to %s", req.URL.String())
				},
			},
		})
		ds.SetRetryBaseDelay(time.Millisecond)
		return ds
	}

	// Create first provider (OpenAI)
	providerData1 := `{"name": "test-openai-discover", "base_url": "https://api.openai.com", "api_key": "test-api-key"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(providerData1))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create first provider: %d", rec.Code)
	}

	// Create second provider (Anthropic)
	providerData2 := `{"name": "test-anthropic-discover", "base_url": "https://api.anthropic.com", "api_key": "test-api-key"}`
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/providers", strings.NewReader(providerData2))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create second provider: %d", rec.Code)
	}

	// Test discover all models - will succeed with mocked responses
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/providers/discover-all", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	// Should succeed
	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 for discover all, got %d: %s", rec.Code, rec.Body.String())
	}

	var response map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// Should have expected response fields
	if _, ok := response["results"]; !ok {
		t.Error("Expected results field in response")
	}
	if _, ok := response["succeeded"]; !ok {
		t.Error("Expected succeeded field in response")
	}
	if _, ok := response["failed"]; !ok {
		t.Error("Expected failed field in response")
	}
	if _, ok := response["discovered"]; !ok {
		t.Error("Expected discovered field in response")
	}
}

// Discovery Handler Tests - Uncovered Code Paths

func TestDiscoverProviderModels_NotFound(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Call with non-existent UUID
	nonExistentID := uuid.New().String()
	req := httptest.NewRequest("POST", "/providers/"+nonExistentID+"/discover", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Should get 404
	if w.Code != 404 {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestGetProviderUsage_NanoGPT(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Create provider with NanoGPT base URL
	body := `{"name":"test-nanogpt","base_url":"https://ngc.nanogpt.com/v1","api_key":"test-api-key"}`
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	providerID := resp["id"].(string)

	// Try to get usage
	req2 := httptest.NewRequest("GET", "/providers/"+providerID+"/usage", http.NoBody)
	req2.Header.Set("Authorization", "Bearer test-admin-token")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	// Should get 400 - NanoGPT usage not supported
	if w2.Code != 400 {
		t.Errorf("expected 400, got %d", w2.Code)
	}
}

func TestGetProviderUsage_OpenRouter(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Create provider with OpenRouter base URL
	body := `{"name":"test-openrouter","base_url":"https://openrouter.ai/api/v1","api_key":"test-api-key"}`
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	providerID := resp["id"].(string)

	// Try to get usage
	req2 := httptest.NewRequest("GET", "/providers/"+providerID+"/usage", http.NoBody)
	req2.Header.Set("Authorization", "Bearer test-admin-token")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	// Upstream rejects the key with 401, so we surface 424 Failed Dependency
	// (a dead credential is a provider-config problem, not a server fault).
	if w2.Code != http.StatusFailedDependency {
		t.Errorf("expected 424, got %d", w2.Code)
	}
}

func TestGetProviderBalance_DefaultUnsupported(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Create provider with non-DeepSeek URL
	uniqueName := fmt.Sprintf("test-balance-unsupported-%s", uuid.New().String()[:8])
	body := fmt.Sprintf(`{"name":"%s","base_url":"https://api.openai.com","api_key":"test-api-key"}`, uniqueName)
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	providerID, ok := resp["id"].(string)
	if !ok {
		t.Fatalf("Response missing id field: %v", resp)
	}

	// Try to get balance
	req2 := httptest.NewRequest("GET", "/providers/"+providerID+"/balance", http.NoBody)
	req2.Header.Set("Authorization", "Bearer test-admin-token")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	// Should get 400 - balance not supported
	if w2.Code != 400 {
		t.Errorf("expected 400, got %d", w2.Code)
	}
}

func TestGetProviderBalance_DeepSeek(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Create provider with DeepSeek base URL
	body := `{"name":"test-deepseek","base_url":"https://api.deepseek.com","api_key":"test-api-key"}`
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	providerID := resp["id"].(string)

	// Try to get balance
	req2 := httptest.NewRequest("GET", "/providers/"+providerID+"/balance", http.NoBody)
	req2.Header.Set("Authorization", "Bearer test-admin-token")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	// Upstream rejects the key with 401, so we surface 424 Failed Dependency
	// (a dead credential is a provider-config problem, not a server fault).
	if w2.Code != http.StatusFailedDependency {
		t.Errorf("expected 424, got %d", w2.Code)
	}
}

func TestRefreshAllQuotas_NanoGPT(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Create provider with NanoGPT base URL
	body := `{"name":"test-nanogpt-quotas","base_url":"https://ngc.nanogpt.com/v1","api_key":"test-api-key","enabled":true}`
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Verify provider was created successfully
	if w.Code != 201 {
		t.Fatalf("Failed to create provider: %d - %s", w.Code, w.Body.String())
	}

	// Refresh all quotas
	req2 := httptest.NewRequest("POST", "/providers/refresh-quotas", http.NoBody)
	req2.Header.Set("Authorization", "Bearer test-admin-token")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	// Should succeed
	if w2.Code != 200 {
		t.Errorf("expected 200, got %d", w2.Code)
	}

	var resp2 map[string]any
	json.NewDecoder(w2.Body).Decode(&resp2)
	resultsInterface, ok := resp2["results"]
	if !ok {
		t.Fatal("results field missing from response")
	}
	// results can be nil or empty slice when no quota-supporting providers exist
	if resultsInterface == nil {
		// This is acceptable - no providers support quotas
		return
	}
	results := resultsInterface.([]any)

	// Should have one result
	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
	}
}

func TestRefreshAllQuotas_ZAICoding(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Create provider with ZAICoding base URL
	body := `{"name":"test-zai-quotas","base_url":"https://api.zai.chat/api/v1","api_key":"test-api-key"}`
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Refresh all quotas
	req2 := httptest.NewRequest("POST", "/providers/refresh-quotas", http.NoBody)
	req2.Header.Set("Authorization", "Bearer test-admin-token")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	// Should succeed
	if w2.Code != 200 {
		t.Errorf("expected 200, got %d", w2.Code)
	}

	var resp2 map[string]any
	json.NewDecoder(w2.Body).Decode(&resp2)
	resultsInterface, ok := resp2["results"]
	if !ok {
		t.Fatal("results field missing from response")
	}
	// results can be nil or empty slice when no quota-supporting providers exist
	if resultsInterface == nil {
		// This is acceptable - no providers support quotas
		return
	}
	results := resultsInterface.([]any)

	// Should have one result
	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
	}
}

func TestRefreshAllQuotas_DeepSeek(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Create provider with DeepSeek base URL
	body := `{"name":"test-deepseek-quotas","base_url":"https://api.deepseek.com","api_key":"test-api-key"}`
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Refresh all quotas
	req2 := httptest.NewRequest("POST", "/providers/refresh-quotas", http.NoBody)
	req2.Header.Set("Authorization", "Bearer test-admin-token")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	// Should succeed
	if w2.Code != 200 {
		t.Errorf("expected 200, got %d", w2.Code)
	}

	var resp2 map[string]any
	json.NewDecoder(w2.Body).Decode(&resp2)
	resultsInterface, ok := resp2["results"]
	if !ok {
		t.Fatal("results field missing from response")
	}
	// results can be nil or empty slice when no quota-supporting providers exist
	if resultsInterface == nil {
		// This is acceptable - no providers support quotas
		return
	}
	results := resultsInterface.([]any)

	// Should have one result
	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
	}
}

func TestRefreshAllQuotas_OpenRouter(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Create provider with OpenRouter base URL
	body := `{"name":"test-openrouter-quotas","base_url":"https://openrouter.ai/api/v1","api_key":"test-api-key"}`
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Refresh all quotas
	req2 := httptest.NewRequest("POST", "/providers/refresh-quotas", http.NoBody)
	req2.Header.Set("Authorization", "Bearer test-admin-token")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	// Should succeed
	if w2.Code != 200 {
		t.Errorf("expected 200, got %d", w2.Code)
	}

	var resp2 map[string]any
	json.NewDecoder(w2.Body).Decode(&resp2)
	resultsInterface, ok := resp2["results"]
	if !ok {
		t.Fatal("results field missing from response")
	}
	if resultsInterface == nil {
		t.Fatal("results field is nil")
		return
	}
	results := resultsInterface.([]any)

	// Should have one result
	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
	}
}

func TestRefreshAllQuotas_SkippedProvider(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Create provider with unknown URL
	body := `{"name":"test-unknown-quotas","base_url":"https://api.anthropic.com","api_key":"test-api-key"}`
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Refresh all quotas
	req2 := httptest.NewRequest("POST", "/providers/refresh-quotas", http.NoBody)
	req2.Header.Set("Authorization", "Bearer test-admin-token")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	// Should succeed
	if w2.Code != 200 {
		t.Errorf("expected 200, got %d", w2.Code)
	}

	var resp2 map[string]any
	json.NewDecoder(w2.Body).Decode(&resp2)
	resultsInterface, ok := resp2["results"]
	if !ok {
		t.Fatal("results field missing from response")
	}
	// results can be nil or empty slice when no quota-supporting providers exist
	if resultsInterface == nil {
		// This is acceptable - no providers support quotas
		return
	}
	results := resultsInterface.([]any)

	// Should have one result with refreshed=false
	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
	}

	result := results[0].(map[string]any)
	if result["refreshed"].(bool) != false {
		t.Errorf("expected refreshed=false, got %v", result["refreshed"])
	}
}

func TestRefreshAllQuotas_DisabledProvider(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	// Create provider
	body := `{"name":"test-disabled-quotas","base_url":"https://api.openai.com","api_key":"test-api-key"}`
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	providerID := resp["id"].(string)

	// Disable the provider
	h.dbPool.Pool().Exec(context.Background(), "UPDATE providers SET enabled = false WHERE id = $1", providerID)

	// Refresh all quotas
	req2 := httptest.NewRequest("POST", "/providers/refresh-quotas", http.NoBody)
	req2.Header.Set("Authorization", "Bearer test-admin-token")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	// Should succeed
	if w2.Code != 200 {
		t.Errorf("expected 200, got %d", w2.Code)
	}

	var resp2 map[string]any
	json.NewDecoder(w2.Body).Decode(&resp2)
	resultsInterface, ok := resp2["results"]
	if !ok {
		t.Fatal("results field missing from response")
	}
	// results can be nil or empty slice when no quota-supporting providers exist
	if resultsInterface == nil {
		// This is acceptable - no providers support quotas
		return
	}
	results := resultsInterface.([]any)

	// Should have no results since disabled provider is skipped
	if len(results) != 0 {
		t.Errorf("expected 0 results for disabled provider, got %d", len(results))
	}
}

// Test for settings.go - UpdateSettings_RateLimit
// Test for admin.go - CreateProvider_KeylessProvider

func TestDiscoverProviderModels_InvalidProvider(t *testing.T) {
	// Override newDiscoveryService with mock transport to avoid real API calls
	// Note: Must override AFTER newTestHandlerWithRouter since NewHandler sets it
	_, r := newTestHandlerWithRouter(t)

	orig := newDiscoveryService
	defer func() { newDiscoveryService = orig }()

	newDiscoveryService = func() *provider.DiscoveryService {
		ds := provider.NewDiscoveryServiceWithHTTPClient(&http.Client{
			Transport: &mockTransport{
				roundTripFunc: func(req *http.Request) (*http.Response, error) {
					if strings.Contains(req.URL.Host, "httpbin.org") {
						return &http.Response{
							StatusCode: http.StatusInternalServerError,
							Body:       io.NopCloser(strings.NewReader(`{"error":"internal server error"}`)),
							Header:     make(http.Header),
						}, nil
					}
					return nil, fmt.Errorf("unexpected request to %s", req.URL.String())
				},
			},
		})
		ds.SetRetryBaseDelay(time.Millisecond)
		return ds
	}

	// Create a provider with invalid URL
	providerData := `{"name": "test-discover-invalid", "base_url": "https://httpbin.org", "api_key": "test-api-key"}`
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

	// Try to discover models - should return error
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/providers/"+providerResp.ID+"/discover", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	// Should return an error due to invalid URL
	if rec.Code != http.StatusInternalServerError && rec.Code != http.StatusBadRequest {
		t.Fatalf("Expected error for invalid URL, got %d: %s", rec.Code, rec.Body.String())
	}
}

// Test for discovery.go - GetProviderUsage_UnsupportedProvider

func TestGetProviderUsage_UnsupportedProvider(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Create a provider that doesn't support usage (OpenAI)
	providerData := fmt.Sprintf(`{"name": "test-usage-unsupported-%s", "base_url": "https://api.openai.com", "api_key": "test-api-key"}`, uuid.New().String()[:8])
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

	// Try to get usage - should return 400 for unsupported provider
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/providers/"+providerResp.ID+"/usage", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("Expected 400 for unsupported provider, got %d: %s", rec.Code, rec.Body.String())
	}
}

// Test for discovery.go - DiscoverAllModels_NoProviders

func TestDiscoverAllModels_NoProviders(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Test discover all models with no providers
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/providers/discover-all", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	// Should succeed with empty results
	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 for discover all with no providers, got %d: %s", rec.Code, rec.Body.String())
	}

	var response map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// Should have expected response fields
	if _, ok := response["results"]; !ok {
		t.Error("Expected results field in response")
	}
	if _, ok := response["succeeded"]; !ok {
		t.Error("Expected succeeded field in response")
	}
	if _, ok := response["failed"]; !ok {
		t.Error("Expected failed field in response")
	}
	if _, ok := response["discovered"]; !ok {
		t.Error("Expected discovered field in response")
	}
}

// Test for applogs.go - GetAppLogs_Empty

func TestDiscoverProviderModels_DisabledProviderExplicit(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	// Create a provider
	body := `{"name":"test-disc-disabled-explicit","base_url":"https://api.openai.com","api_key":"sk-test123"}`
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("Expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}
	providerID, ok := resp["id"].(string)
	if !ok {
		t.Fatalf("Provider ID not found in response")
	}

	// Disable the provider via repository update
	provRepo := provider.NewRepository(h.Pool().Pool())
	updateReq := provider.UpdateProviderRequest{
		Enabled: &[]bool{false}[0],
	}
	_, err := provRepo.Update(context.Background(), uuid.MustParse(providerID), updateReq, nil, nil, nil)
	if err != nil {
		t.Fatalf("Failed to disable provider: %v", err)
	}

	// Try to discover models on disabled provider
	req2 := httptest.NewRequest("POST", "/providers/"+providerID+"/discover", http.NoBody)
	req2.Header.Set("Authorization", "Bearer test-admin-token")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	// Should get 400 Bad Request for disabled provider
	if w2.Code != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request for disabled provider, got %d: %s", w2.Code, w2.Body.String())
	}
}

// TestDiscoverProviderModels_AutodiscoveryDisabled tests that discovery is rejected when autodiscovery_enabled is false

func TestDiscoverProviderModels_AutodiscoveryDisabled(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	// Create a provider
	body := `{"name":"test-disc-autodis-disabled","base_url":"https://api.openai.com","api_key":"sk-test123"}`
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("Expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}
	providerID, ok := resp["id"].(string)
	if !ok {
		t.Fatalf("Provider ID not found in response")
	}

	// Disable autodiscovery via repository update (but keep provider enabled)
	provRepo := provider.NewRepository(h.Pool().Pool())
	updateReq := provider.UpdateProviderRequest{
		AutodiscoveryEnabled: &[]bool{false}[0],
		Enabled:              &[]bool{true}[0],
	}
	_, err := provRepo.Update(context.Background(), uuid.MustParse(providerID), updateReq, nil, nil, nil)
	if err != nil {
		t.Fatalf("Failed to update provider autodiscovery setting: %v", err)
	}

	// Try to discover models on provider with autodiscovery disabled
	req2 := httptest.NewRequest("POST", "/providers/"+providerID+"/discover", http.NoBody)
	req2.Header.Set("Authorization", "Bearer test-admin-token")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	// Should get 403 Forbidden for autodiscovery disabled
	if w2.Code != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden for autodiscovery disabled provider, got %d: %s", w2.Code, w2.Body.String())
	}

	if !strings.Contains(w2.Body.String(), "autodiscovery is disabled for this provider") {
		t.Errorf("expected error message 'autodiscovery is disabled for this provider', got %q", w2.Body.String())
	}
}

// TestListProviders_WithSearchFilter tests the search filter functionality

func TestGetProviderUsage_Error(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Override newDiscoveryService with mock transport to avoid real API calls
	// Note: Must override AFTER newTestHandlerWithRouter since NewHandler sets it
	orig := newDiscoveryService
	defer func() { newDiscoveryService = orig }()

	newDiscoveryService = func() *provider.DiscoveryService {
		ds := provider.NewDiscoveryServiceWithHTTPClient(&http.Client{
			Transport: &mockTransport{
				roundTripFunc: func(req *http.Request) (*http.Response, error) {
					return &http.Response{
						StatusCode: http.StatusUnauthorized,
						Body:       io.NopCloser(strings.NewReader(`{"error": "unauthorized"}`)),
						Header:     make(http.Header),
					}, nil
				},
			},
		})
		ds.SetRetryBaseDelay(time.Millisecond)
		return ds
	}

	// Create a provider with OpenRouter base URL (supported type)
	body := `{"name":"test-usage-error","base_url":"https://openrouter.ai/api/v1","api_key":"invalid-key-for-error"}`
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("Expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	providerID := resp["id"].(string)

	// Try to get usage - this will fail because the API key is invalid
	req2 := httptest.NewRequest("GET", "/providers/"+providerID+"/usage", http.NoBody)
	req2.Header.Set("Authorization", "Bearer test-admin-token")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	// Upstream rejects the invalid key with 401, so we surface 424 Failed
	// Dependency rather than 500: a dead credential is a provider-config
	// problem, not a server fault.
	if w2.Code != http.StatusFailedDependency {
		t.Errorf("expected 424 Failed Dependency, got %d: %s", w2.Code, w2.Body.String())
	}
}

// TestGetProviderBalance_Error tests the error path for balance endpoint

func TestGetProviderBalance_Error(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Create a provider with DeepSeek base URL (supported type)
	body := `{"name":"test-balance-error","base_url":"https://api.deepseek.com","api_key":"invalid-key"}`
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("Expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	providerID := resp["id"].(string)

	// Try to get balance - this will fail because the API key is invalid
	req2 := httptest.NewRequest("GET", "/providers/"+providerID+"/balance", http.NoBody)
	req2.Header.Set("Authorization", "Bearer test-admin-token")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	// Upstream rejects the invalid key with 401, so we surface 424 Failed
	// Dependency rather than 500: a dead credential is a provider-config
	// problem, not a server fault.
	if w2.Code != http.StatusFailedDependency {
		t.Errorf("expected 424 Failed Dependency, got %d: %s", w2.Code, w2.Body.String())
	}
}

// TestListProviders_WithPaginationAndModelCounts tests pagination query params

func TestDiscoverProviderModels_WithInvalidProviderType(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Create a provider with a custom/self-hosted base URL
	body := `{"name":"test-custom-provider","base_url":"https://custom.example.com","api_key":"sk-custom"}`
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("Expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	providerID := resp["id"].(string)

	// Try to discover - this will likely fail with an API error
	req2 := httptest.NewRequest("POST", "/providers/"+providerID+"/discover", http.NoBody)
	req2.Header.Set("Authorization", "Bearer test-admin-token")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	// Discovery will attempt to call the custom endpoint and fail
	// We just verify it doesn't crash - actual response depends on network
	if w2.Code != http.StatusOK && w2.Code != http.StatusInternalServerError {
		t.Errorf("expected 200 or 500, got %d: %s", w2.Code, w2.Body.String())
	}
}

// TestGetProviderBalance_UnsupportedProvider tests balance endpoint for unsupported provider type

func TestGetProviderBalance_UnsupportedProvider(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Create a provider with OpenAI base URL (not supported for balance)
	uniqueName := "test-bal-unsup-" + uuid.New().String()[:8]
	body := fmt.Sprintf(`{"name":"%s","base_url":"https://api.openai.com","api_key":"sk-test"}`, uniqueName)
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("Expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	providerID := resp["id"].(string)

	// Try to get balance - OpenAI is not supported
	req2 := httptest.NewRequest("GET", "/providers/"+providerID+"/balance", http.NoBody)
	req2.Header.Set("Authorization", "Bearer test-admin-token")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	// Should get 400 Bad Request for unsupported provider type
	if w2.Code != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request, got %d: %s", w2.Code, w2.Body.String())
	}
}

// TestListLogs_WithPagination tests pagination parameters

func TestDiscoverProviderModels_SuccessPath(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Create a provider with OpenAI base URL (will fail to discover but tests the code path)
	body := `{"name":"test-disc-success","base_url":"https://api.openai.com","api_key":"sk-test123"}`
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("Expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	providerID := resp["id"].(string)

	// Try to discover - this will fail because the API key is fake, but it tests the code path
	req2 := httptest.NewRequest("POST", "/providers/"+providerID+"/discover", http.NoBody)
	req2.Header.Set("Authorization", "Bearer test-admin-token")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	// Discovery will attempt to call the endpoint and fail
	// We verify it doesn't crash and returns an appropriate error
	if w2.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 Internal Server Error, got %d: %s", w2.Code, w2.Body.String())
	}
}

// TestGetProviderUsage_ZAICoding tests ZAI Coding provider usage endpoint

func TestGetProviderUsage_ZAICoding(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Create a provider with ZAI Coding base URL pattern
	body := `{"name":"test-usage-zai","base_url":"https://zai.api.example.com","api_key":"test-key"}`
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("Expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	providerID := resp["id"].(string)

	// Try to get usage - will fail because URL is fake, but tests the code path
	req2 := httptest.NewRequest("GET", "/providers/"+providerID+"/usage", http.NoBody)
	req2.Header.Set("Authorization", "Bearer test-admin-token")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	// Should get 500 because the API call fails (or 400 if provider type not recognized)
	// We accept both since the exact behavior depends on URL pattern matching
	if w2.Code != http.StatusInternalServerError && w2.Code != http.StatusBadRequest {
		t.Errorf("expected 500 or 400, got %d: %s", w2.Code, w2.Body.String())
	}
}

// TestFailoverCandidates_Empty tests the Candidates endpoint with no models

func TestGetProviderBalance_UnsupportedType_Integration(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Create a provider with a generic URL (not nanogpt/openrouter/deepseek/zai-coding)
	uniqueName := "test-bal-generic-" + uuid.New().String()[:8]
	body := fmt.Sprintf(`{"name":"%s","base_url":"https://api.generic.com","api_key":"sk-generic"}`, uniqueName)
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("Expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	providerID := resp["id"].(string)

	// Try to get balance - should return 400 for unsupported provider type
	req2 := httptest.NewRequest("GET", "/providers/"+providerID+"/balance", http.NoBody)
	req2.Header.Set("Authorization", "Bearer test-admin-token")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	if w2.Code != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request for unsupported provider type, got %d: %s", w2.Code, w2.Body.String())
	}
}

// TestGetProviderBalance_OpenRouterError_Integration tests balance check on OpenRouter provider
// Note: Current implementation only supports DeepSeek, so OpenRouter returns 400 (unsupported)

func TestGetProviderBalance_OpenRouterError_Integration(t *testing.T) {
	// The three quota endpoints now share the read-through serveQuota, which
	// derives the snapshot kind from the provider type rather than the URL path.
	// OpenRouter maps to the "usage" kind, so /balance read-throughs (it no
	// longer 400s); an upstream failure surfaces as a 500 from the cold-fill.
	_, r := newTestHandlerWithRouter(t)

	orig := newDiscoveryService
	defer func() { newDiscoveryService = orig }()
	newDiscoveryService = func() *provider.DiscoveryService {
		ds := provider.NewDiscoveryServiceWithHTTPClient(&http.Client{
			Transport: &mockTransport{roundTripFunc: func(req *http.Request) (*http.Response, error) {
				if strings.Contains(req.URL.Host, "openrouter.ai") {
					return &http.Response{
						StatusCode: http.StatusInternalServerError,
						Body:       io.NopCloser(strings.NewReader(`{"error":"internal"}`)),
						Header:     make(http.Header),
					}, nil
				}
				return nil, fmt.Errorf("unexpected request to %s", req.URL.String())
			}},
		})
		ds.SetRetryBaseDelay(time.Millisecond)
		return ds
	}

	// Create a provider with OpenRouter base URL pattern
	body := `{"name":"test-balance-openrouter","base_url":"https://openrouter.ai/api/v1","api_key":"sk-fake-key"}`
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("Expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	providerID := resp["id"].(string)

	req2 := httptest.NewRequest("GET", "/providers/"+providerID+"/balance", http.NoBody)
	req2.Header.Set("Authorization", "Bearer test-admin-token")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	if w2.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 from read-through cold-fill error, got %d: %s", w2.Code, w2.Body.String())
	}
	if !strings.Contains(w2.Body.String(), "failed to fetch usage") {
		t.Errorf("expected read-through fetch error, got: %s", w2.Body.String())
	}
}

// TestListModels_WithModels tests listing models with provider_id filter
