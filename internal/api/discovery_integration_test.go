package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/provider"
)

// TestDiscoverAllModels_AllDisabled tests that DiscoverAllModels skips all
// disabled providers and returns an empty result structure.
func TestDiscoverAllModels_AllDisabled(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Create providers and then disable them (CreateProviderRequest doesn't have enabled field)
	var providerIDs []string
	for i := 0; i < 3; i++ {
		providerData := fmt.Sprintf(`{"name": "test-disabled-all-%d", "base_url": "https://api.openai.com", "api_key": "sk-test123"}`, i)
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(providerData))
		req.Header.Set("Authorization", "Bearer test-admin-token")
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("Failed to create provider %d: %d: %s", i, rec.Code, rec.Body.String())
		}

		var createResp struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &createResp); err != nil {
			t.Fatalf("Failed to parse create response: %v", err)
		}
		providerIDs = append(providerIDs, createResp.ID)
	}

	// Disable all providers
	for _, id := range providerIDs {
		updateData := `{"enabled": false}`
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPut, "/providers/"+id, strings.NewReader(updateData))
		req.Header.Set("Authorization", "Bearer test-admin-token")
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("Failed to disable provider %s: %d: %s", id, rec.Code, rec.Body.String())
		}
	}

	// Run discover-all
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/providers/discover-all", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response struct {
		Results    []interface{} `json:"results"`
		Succeeded  int           `json:"succeeded"`
		Failed     int           `json:"failed"`
		Discovered int           `json:"discovered"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// All providers should be skipped (disabled), so results should be empty
	if len(response.Results) != 0 {
		t.Errorf("Expected empty results (all providers disabled), got %d", len(response.Results))
	}
	if response.Succeeded != 0 {
		t.Errorf("Expected succeeded=0, got %d", response.Succeeded)
	}
	if response.Failed != 0 {
		t.Errorf("Expected failed=0, got %d", response.Failed)
	}
	if response.Discovered != 0 {
		t.Errorf("Expected discovered=0, got %d", response.Discovered)
	}
}

// TestGetProviderUsage_InvalidUUID tests that GetProviderUsage returns 400 for
// an invalid UUID in the path.
func TestGetProviderUsage_InvalidUUID(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/providers/invalid-uuid/usage", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for invalid UUID, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestGetProviderUsage_NonExistent tests that GetProviderUsage returns 404 for
// a valid but non-existent UUID.
func TestGetProviderUsage_NonExistent(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	nonExistentID := uuid.New().String()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/providers/"+nonExistentID+"/usage", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("Expected 404 for non-existent provider, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestGetProviderBalance_InvalidUUID tests that GetProviderBalance returns 400 for
// an invalid UUID in the path.
func TestGetProviderBalance_InvalidUUID(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/providers/invalid-uuid/balance", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for invalid UUID, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestGetProviderBalance_NonExistent tests that GetProviderBalance returns 404 for
// a valid but non-existent UUID.
func TestGetProviderBalance_NonExistent(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	nonExistentID := uuid.New().String()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/providers/"+nonExistentID+"/balance", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("Expected 404 for non-existent provider, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestGetOllamaCloudAccount_InvalidUUID tests that GetOllamaCloudAccount returns 400 for
// an invalid UUID in the path.
func TestGetOllamaCloudAccount_InvalidUUID(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/providers/invalid-uuid/account", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for invalid UUID, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestGetOllamaCloudAccount_NonExistent tests that GetOllamaCloudAccount returns 404 for
// a valid but non-existent UUID.
func TestGetOllamaCloudAccount_NonExistent(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	nonExistentID := uuid.New().String()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/providers/"+nonExistentID+"/account", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("Expected 404 for non-existent provider, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestRefreshAllQuotas_AllDisabled tests that RefreshAllQuotas skips all
// disabled providers.
func TestRefreshAllQuotas_AllDisabled(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Create multiple disabled providers
	for i := 0; i < 2; i++ {
		providerData := fmt.Sprintf(`{"name": "test-quota-disabled-%d", "base_url": "https://api.nanogpt.com", "api_key": "test-key", "enabled": false}`, i)
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(providerData))
		req.Header.Set("Authorization", "Bearer test-admin-token")
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("Failed to create provider %d: %d: %s", i, rec.Code, rec.Body.String())
		}
	}

	// Run refresh-quotas
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/providers/refresh-quotas", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response struct {
		Results   []interface{} `json:"results"`
		Refreshed int           `json:"refreshed"`
		Failed    int           `json:"failed"`
		Skipped   int           `json:"skipped"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// All providers should be skipped (disabled) - they are silently skipped
	// without incrementing the skipped counter (which is only for unsupported types)
	if len(response.Results) != 0 {
		t.Errorf("Expected empty results (all providers disabled), got %d", len(response.Results))
	}
	if response.Refreshed != 0 {
		t.Errorf("Expected refreshed=0, got %d", response.Refreshed)
	}
	if response.Failed != 0 {
		t.Errorf("Expected failed=0, got %d", response.Failed)
	}
	// Note: skipped counter is only for unsupported provider types, not disabled ones
}

// TestDiscoverProviderModels_InvalidUUID tests that DiscoverProviderModels returns 400 for
// an invalid UUID in the path.
func TestDiscoverProviderModels_InvalidUUID(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/providers/invalid-uuid/discover", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for invalid UUID, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestDiscoverProviderModels_NonExistent tests that DiscoverProviderModels returns 404 for
// a valid but non-existent UUID.
func TestDiscoverProviderModels_NonExistent(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	nonExistentID := uuid.New().String()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/providers/"+nonExistentID+"/discover", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("Expected 404 for non-existent provider, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestDiscoverProviderModels_DisabledProvider tests that DiscoverProviderModels returns 400 for
// a disabled provider.
func TestDiscoverProviderModels_DisabledProvider(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Create a provider
	providerData := `{"name": "test-disabled-discover", "base_url": "https://api.openai.com", "api_key": "sk-test123"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d: %s", rec.Code, rec.Body.String())
	}

	var createResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &createResp); err != nil {
		t.Fatalf("Failed to parse create response: %v", err)
	}

	// Disable the provider
	updateData := `{"enabled": false}`
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPut, "/providers/"+createResp.ID, strings.NewReader(updateData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Failed to disable provider: %d: %s", rec.Code, rec.Body.String())
	}

	// Try to discover models - should fail because provider is disabled
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/providers/"+createResp.ID+"/discover", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for disabled provider, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestGetProviderUsage_UnsupportedType tests that GetProviderUsage returns 400 for
// a provider type that doesn't support usage information.
func TestGetProviderUsage_UnsupportedType(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Create a provider with OpenAI URL (doesn't support usage endpoint)
	providerData := `{"name": "test-usage-unsupported", "base_url": "https://api.openai.com", "api_key": "sk-test123"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d: %s", rec.Code, rec.Body.String())
	}

	var createResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &createResp); err != nil {
		t.Fatalf("Failed to parse create response: %v", err)
	}

	// Try to get usage - should fail because OpenAI doesn't support usage endpoint
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/providers/"+createResp.ID+"/usage", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for unsupported usage type, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "usage information not supported") {
		t.Errorf("Expected error about unsupported usage, got: %s", rec.Body.String())
	}
}

// TestGetProviderBalance_UnsupportedType tests that GetProviderBalance returns 400 for
// a provider type that doesn't support balance information.
func TestGetProviderBalance_UnsupportedType(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Create a provider with non-DeepSeek URL
	providerData := `{"name": "test-balance-unsupported", "base_url": "https://api.openai.com", "api_key": "sk-test123"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d: %s", rec.Code, rec.Body.String())
	}

	var createResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &createResp); err != nil {
		t.Fatalf("Failed to parse create response: %v", err)
	}

	// Try to get balance - should fail because only DeepSeek supports balance
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/providers/"+createResp.ID+"/balance", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for unsupported balance type, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "balance information not supported") {
		t.Errorf("Expected error about unsupported balance, got: %s", rec.Body.String())
	}
}

// TestGetOllamaCloudAccount_NonOllamaCloud tests that GetOllamaCloudAccount returns 400 for
// a provider that is not Ollama Cloud.
func TestGetOllamaCloudAccount_NonOllamaCloud(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Create a provider with non-OllamaCloud URL
	providerData := `{"name": "test-account-non-ollama", "base_url": "https://api.openai.com", "api_key": "sk-test123"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d: %s", rec.Code, rec.Body.String())
	}

	var createResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &createResp); err != nil {
		t.Fatalf("Failed to parse create response: %v", err)
	}

	// Try to get account - should fail because only Ollama Cloud supports account endpoint
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/providers/"+createResp.ID+"/account", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for non-OllamaCloud provider, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "account information not supported") {
		t.Errorf("Expected error about unsupported account, got: %s", rec.Body.String())
	}
}

// TestRefreshAllQuotas_UnsupportedType tests that RefreshAllQuotas skips providers
// that don't support quota refresh.
func TestRefreshAllQuotas_UnsupportedType(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Create a provider with OpenAI URL (doesn't support quota refresh)
	providerData := `{"name": "test-quota-unsupported", "base_url": "https://api.openai.com", "api_key": "sk-test123"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d: %s", rec.Code, rec.Body.String())
	}

	// Run refresh-quotas
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/providers/refresh-quotas", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response struct {
		Results   []interface{} `json:"results"`
		Refreshed int           `json:"refreshed"`
		Failed    int           `json:"failed"`
		Skipped   int           `json:"skipped"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// The OpenAI provider should be skipped (unsupported type)
	if response.Skipped != 1 {
		t.Errorf("Expected skipped=1, got %d", response.Skipped)
	}
	if response.Refreshed != 0 {
		t.Errorf("Expected refreshed=0, got %d", response.Refreshed)
	}
}

// TestDiscoverAllModels_DiscoveryError tests that DiscoverAllModels handles
// discovery errors gracefully.
func TestDiscoverAllModels_DiscoveryError(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Create a provider with unreachable URL (connection refused immediately)
	providerData := `{"name": "test-discovery-error", "base_url": "http://127.0.0.1:1", "api_key": "sk-test123"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d: %s", rec.Code, rec.Body.String())
	}

	// Run discover-all - should complete with failed=1
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/providers/discover-all", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response struct {
		Results    []DiscoverAllResult `json:"results"`
		Succeeded  int                 `json:"succeeded"`
		Failed     int                 `json:"failed"`
		Discovered int                 `json:"discovered"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// Should have one failed result
	if response.Failed != 1 {
		t.Errorf("Expected failed=1, got %d", response.Failed)
	}
	if response.Succeeded != 0 {
		t.Errorf("Expected succeeded=0, got %d", response.Succeeded)
	}
	if len(response.Results) != 1 {
		t.Errorf("Expected 1 result, got %d", len(response.Results))
	}
	if response.Results[0].Error == "" {
		t.Error("Expected error message in result")
	}
}

// TestDiscoverProviderModels_SuccessWithMockServer tests the happy path where model discovery
// succeeds with a mock OpenAI-compatible server, models are upserted, missing models are
// disabled, and failover groups are synced.
func TestDiscoverProviderModels_SuccessWithMockServer(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Create mock OpenAI-compatible server
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" && r.Method == "GET" {
			response := map[string]interface{}{
				"data": []map[string]interface{}{
					{"id": "gpt-4-test", "owned_by": "openai"},
					{"id": "gpt-3.5-test", "owned_by": "openai"},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)
			return
		}
		http.NotFound(w, r)
	}))
	defer mockServer.Close()

	// Create provider with mock server URL
	providerData := fmt.Sprintf(`{"name": "test-discover-success", "base_url": "%s", "api_key": "sk-test123"}`, mockServer.URL)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d: %s", rec.Code, rec.Body.String())
	}

	var createResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &createResp); err != nil {
		t.Fatalf("Failed to parse create response: %v", err)
	}

	// Discover models for this provider
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/providers/"+createResp.ID+"/discover", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response struct {
		Discovered int           `json:"discovered"`
		Models     []interface{} `json:"models"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if response.Discovered != 2 {
		t.Errorf("Expected discovered=2, got %d", response.Discovered)
	}
	if len(response.Models) != 2 {
		t.Errorf("Expected 2 models, got %d", len(response.Models))
	}
}

// TestDiscoverAllModels_WithEnabledProvider tests the happy path where discover-all
// successfully discovers models from enabled providers.
func TestDiscoverAllModels_WithEnabledProvider(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Create mock OpenAI-compatible server
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" && r.Method == "GET" {
			response := map[string]interface{}{
				"data": []map[string]interface{}{
					{"id": "model-1", "owned_by": "test"},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)
			return
		}
		http.NotFound(w, r)
	}))
	defer mockServer.Close()

	// Create enabled provider with mock server URL
	providerData := fmt.Sprintf(`{"name": "test-discover-all-enabled", "base_url": "%s", "api_key": "sk-test123"}`, mockServer.URL)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d: %s", rec.Code, rec.Body.String())
	}

	// Run discover-all
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/providers/discover-all", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response struct {
		Results    []DiscoverAllResult `json:"results"`
		Succeeded  int                 `json:"succeeded"`
		Failed     int                 `json:"failed"`
		Discovered int                 `json:"discovered"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if response.Succeeded != 1 {
		t.Errorf("Expected succeeded=1, got %d", response.Succeeded)
	}
	if response.Failed != 0 {
		t.Errorf("Expected failed=0, got %d", response.Failed)
	}
	if response.Discovered != 1 {
		t.Errorf("Expected discovered=1, got %d", response.Discovered)
	}
	if len(response.Results) != 1 {
		t.Errorf("Expected 1 result, got %d", len(response.Results))
	}
	if response.Results[0].Discovered != 1 {
		t.Errorf("Expected result discovered=1, got %d", response.Results[0].Discovered)
	}
	if response.Results[0].Error != "" {
		t.Errorf("Expected no error, got %s", response.Results[0].Error)
	}
}

// TestGetProviderUsage_ZAICodingError tests that GetProviderUsage handles
// z.ai API errors (note: z.ai returns 200 with error JSON for invalid keys).
func TestGetProviderUsage_ZAICodingError(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Create a provider with z.ai URL and fake key
	providerName := fmt.Sprintf("test-zai-error-%s", uuid.New().String()[:8])
	providerData := fmt.Sprintf(`{"name": "%s", "base_url": "https://api.z.ai", "api_key": "fake-key"}`, providerName)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d: %s", rec.Code, rec.Body.String())
	}

	var createResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &createResp); err != nil {
		t.Fatalf("Failed to parse create response: %v", err)
	}

	// Try to get usage - z.ai returns 200 with error JSON for invalid keys
	// This exercises the zai-coding case in GetProviderUsage
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/providers/"+createResp.ID+"/usage", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	// Note: z.ai API returns 200 with error JSON body for invalid keys
	// The response should contain quota-related fields or error indication
	var quotaResp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &quotaResp); err != nil {
		t.Fatalf("Failed to parse quota response: %v", err)
	}
	// Verify the response was processed (either success or error JSON)
	if rec.Code != http.StatusOK && rec.Code != http.StatusInternalServerError {
		t.Errorf("Expected 200 or 500 for z.ai API call, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestGetProviderUsage_NanoGPTError tests that GetProviderUsage returns 500
// when the NanoGPT API call fails with an invalid key.
func TestGetProviderUsage_NanoGPTError(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Create a provider with NanoGPT URL (with hyphen) and fake key
	providerName := fmt.Sprintf("test-nanogpt-error-%s", uuid.New().String()[:8])
	providerData := fmt.Sprintf(`{"name": "%s", "base_url": "https://api.nano-gpt.com", "api_key": "fake-key"}`, providerName)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d: %s", rec.Code, rec.Body.String())
	}

	var createResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &createResp); err != nil {
		t.Fatalf("Failed to parse create response: %v", err)
	}

	// Try to get usage - should fail with 500 due to fake key
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/providers/"+createResp.ID+"/usage", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("Expected 500 for NanoGPT API error, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "failed to fetch usage") {
		t.Errorf("Expected error about failed to fetch usage, got: %s", rec.Body.String())
	}
}

// TestGetProviderUsage_OpenRouterError tests that GetProviderUsage returns 500
// when the OpenRouter API call fails with an invalid key.
func TestGetProviderUsage_OpenRouterError(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Create a provider with OpenRouter URL and fake key
	providerName := fmt.Sprintf("test-openrouter-error-%s", uuid.New().String()[:8])
	providerData := fmt.Sprintf(`{"name": "%s", "base_url": "https://openrouter.ai", "api_key": "fake-key"}`, providerName)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d: %s", rec.Code, rec.Body.String())
	}

	var createResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &createResp); err != nil {
		t.Fatalf("Failed to parse create response: %v", err)
	}

	// Try to get usage - should fail with 500 due to fake key
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/providers/"+createResp.ID+"/usage", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("Expected 500 for OpenRouter API error, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "failed to fetch key balance") {
		t.Errorf("Expected error about failed to fetch key balance, got: %s", rec.Body.String())
	}
}

// TestGetProviderBalance_DeepSeekError tests that GetProviderBalance returns 500
// when the DeepSeek API call fails with an invalid key.
func TestGetProviderBalance_DeepSeekError(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Create a provider with DeepSeek URL and fake key
	providerName := fmt.Sprintf("test-deepseek-error-%s", uuid.New().String()[:8])
	providerData := fmt.Sprintf(`{"name": "%s", "base_url": "https://api.deepseek.com", "api_key": "fake-key"}`, providerName)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d: %s", rec.Code, rec.Body.String())
	}

	var createResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &createResp); err != nil {
		t.Fatalf("Failed to parse create response: %v", err)
	}

	// Try to get balance - should fail with 500 due to fake key
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/providers/"+createResp.ID+"/balance", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("Expected 500 for DeepSeek API error, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "failed to fetch balance") {
		t.Errorf("Expected error about failed to fetch balance, got: %s", rec.Body.String())
	}
}

// TestGetOllamaCloudAccount_Error tests that GetOllamaCloudAccount returns 500
// when the Ollama Cloud API call fails with an invalid key.
func TestGetOllamaCloudAccount_Error(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Create a provider with Ollama Cloud URL and fake key
	providerName := fmt.Sprintf("test-ollama-error-%s", uuid.New().String()[:8])
	providerData := fmt.Sprintf(`{"name": "%s", "base_url": "https://ollama.com", "api_key": "fake-key"}`, providerName)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d: %s", rec.Code, rec.Body.String())
	}

	var createResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &createResp); err != nil {
		t.Fatalf("Failed to parse create response: %v", err)
	}

	// Try to get account - should fail with 500 due to fake key
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/providers/"+createResp.ID+"/account", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("Expected 500 for Ollama Cloud API error, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "failed to fetch ollama cloud account") {
		t.Errorf("Expected error about failed to fetch ollama cloud account, got: %s", rec.Body.String())
	}
}

// TestRefreshAllQuotas_WithSupportedTypes tests that RefreshAllQuotas handles
// multiple provider types with errors for unsupported types.
func TestRefreshAllQuotas_WithSupportedTypes(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Create Provider A: nanogpt type (will fail with fake key)
	// Note: z.ai returns 200 with error JSON for invalid keys, so it may succeed
	providerAName := fmt.Sprintf("test-quota-nanogpt-%s", uuid.New().String()[:8])
	providerAData := fmt.Sprintf(`{"name": "%s", "base_url": "https://api.nano-gpt.com", "api_key": "fake-key"}`, providerAName)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(providerAData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider A: %d: %s", rec.Code, rec.Body.String())
	}

	// Create Provider B: zai-coding type (may return 200 with error JSON)
	providerBName := fmt.Sprintf("test-quota-zai-%s", uuid.New().String()[:8])
	providerBData := fmt.Sprintf(`{"name": "%s", "base_url": "https://api.z.ai", "api_key": "fake-key"}`, providerBName)
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(providerBData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider B: %d: %s", rec.Code, rec.Body.String())
	}

	// Create Provider C: openai type (unsupported for quota - will be skipped)
	providerCName := fmt.Sprintf("test-quota-openai-%s", uuid.New().String()[:8])
	providerCData := fmt.Sprintf(`{"name": "%s", "base_url": "https://api.openai.com", "api_key": "fake-key"}`, providerCName)
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(providerCData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider C: %d: %s", rec.Code, rec.Body.String())
	}

	// Run refresh-quotas
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/providers/refresh-quotas", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response struct {
		Results   []QuotaRefreshResult `json:"results"`
		Refreshed int                  `json:"refreshed"`
		Failed    int                  `json:"failed"`
		Skipped   int                  `json:"skipped"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// nanogpt should fail, zai-coding may succeed with error JSON, openai should be skipped
	// At minimum: nanogpt fails, openai skipped
	if response.Failed < 1 {
		t.Errorf("Expected failed >= 1 (nanogpt), got %d", response.Failed)
	}
	if response.Skipped < 1 {
		t.Errorf("Expected skipped >= 1 (openai), got %d", response.Skipped)
	}

	// Verify results array has entries for supported types
	var nanogptFound, zaiFound bool
	for _, result := range response.Results {
		if result.ProviderType == "nanogpt" {
			nanogptFound = true
		}
		if result.ProviderType == "zai-coding" {
			zaiFound = true
		}
	}
	if !nanogptFound {
		t.Error("Expected nanogpt result in results")
	}
	if !zaiFound {
		t.Error("Expected zai-coding result in results")
	}
}

// TestRefreshAllQuotas_DeepSeekError tests that RefreshAllQuotas handles
// DeepSeek API errors correctly.
func TestRefreshAllQuotas_DeepSeekError(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Create a provider with DeepSeek URL and fake key
	providerName := fmt.Sprintf("test-quota-deepseek-%s", uuid.New().String()[:8])
	providerData := fmt.Sprintf(`{"name": "%s", "base_url": "https://api.deepseek.com", "api_key": "fake-key"}`, providerName)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d: %s", rec.Code, rec.Body.String())
	}

	// Run refresh-quotas
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/providers/refresh-quotas", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response struct {
		Results   []QuotaRefreshResult `json:"results"`
		Refreshed int                  `json:"refreshed"`
		Failed    int                  `json:"failed"`
		Skipped   int                  `json:"skipped"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if response.Failed < 1 {
		t.Errorf("Expected failed >= 1, got %d", response.Failed)
	}

	// Verify the result has provider_type: "deepseek" and non-empty error
	var deepSeekFound bool
	for _, result := range response.Results {
		if result.ProviderType == "deepseek" && result.Error != "" {
			deepSeekFound = true
			break
		}
	}
	if !deepSeekFound {
		t.Error("Expected deepseek result with error in results")
	}
}

// TestRefreshAllQuotas_OllamaCloudError tests that RefreshAllQuotas handles
// Ollama Cloud API errors correctly.
func TestRefreshAllQuotas_OllamaCloudError(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Create a provider with Ollama Cloud URL and fake key
	providerName := fmt.Sprintf("test-quota-ollama-%s", uuid.New().String()[:8])
	providerData := fmt.Sprintf(`{"name": "%s", "base_url": "https://ollama.com", "api_key": "fake-key"}`, providerName)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d: %s", rec.Code, rec.Body.String())
	}

	// Run refresh-quotas
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/providers/refresh-quotas", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response struct {
		Results   []QuotaRefreshResult `json:"results"`
		Refreshed int                  `json:"refreshed"`
		Failed    int                  `json:"failed"`
		Skipped   int                  `json:"skipped"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if response.Failed < 1 {
		t.Errorf("Expected failed >= 1, got %d", response.Failed)
	}

	// Verify the result has provider_type: "ollama-cloud" and non-empty error
	var ollamaFound bool
	for _, result := range response.Results {
		if result.ProviderType == "ollama-cloud" && result.Error != "" {
			ollamaFound = true
			break
		}
	}
	if !ollamaFound {
		t.Error("Expected ollama-cloud result with error in results")
	}
}

// TestRefreshAllQuotas_OpenRouterError tests that RefreshAllQuotas handles
// OpenRouter API errors correctly.
func TestRefreshAllQuotas_OpenRouterError(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Create a provider with OpenRouter URL and fake key
	providerName := fmt.Sprintf("test-quota-openrouter-%s", uuid.New().String()[:8])
	providerData := fmt.Sprintf(`{"name": "%s", "base_url": "https://openrouter.ai", "api_key": "fake-key"}`, providerName)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d: %s", rec.Code, rec.Body.String())
	}

	// Run refresh-quotas
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/providers/refresh-quotas", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response struct {
		Results   []QuotaRefreshResult `json:"results"`
		Refreshed int                  `json:"refreshed"`
		Failed    int                  `json:"failed"`
		Skipped   int                  `json:"skipped"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if response.Failed < 1 {
		t.Errorf("Expected failed >= 1, got %d", response.Failed)
	}

	// Verify the result has provider_type: "openrouter" and non-empty error
	var openrouterFound bool
	for _, result := range response.Results {
		if result.ProviderType == "openrouter" && result.Error != "" {
			openrouterFound = true
			break
		}
	}
	if !openrouterFound {
		t.Error("Expected openrouter result with error in results")
	}
}

// TestDiscoverProviderModels_DiscoveryError tests that DiscoverProviderModels
// returns 500 when model discovery fails due to unreachable URL.
func TestDiscoverProviderModels_DiscoveryError(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Create a provider with unreachable URL (localhost port 1, refuses immediately)
	providerName := fmt.Sprintf("test-discover-error-%s", uuid.New().String()[:8])
	providerData := fmt.Sprintf(`{"name": "%s", "base_url": "http://127.0.0.1:1", "api_key": "sk-test123"}`, providerName)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d: %s", rec.Code, rec.Body.String())
	}

	var createResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &createResp); err != nil {
		t.Fatalf("Failed to parse create response: %v", err)
	}

	// Try to discover models - should fail with 500 due to unreachable URL
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/providers/"+createResp.ID+"/discover", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("Expected 500 for discovery error, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "failed to discover models") {
		t.Errorf("Expected error about failed to discover models, got: %s", rec.Body.String())
	}
}

// TestDiscoverProviderModels_WithModelsDevCache tests that model discovery
// successfully enriches models with data from the models.dev cache.
func TestDiscoverProviderModels_WithModelsDevCache(t *testing.T) {
	defer provider.ResetModelsDevCache()

	// Create mock models.dev server
	modelsDevResponse := `{
		"openai": {
			"id": "openai",
			"name": "OpenAI",
			"api": "openai",
			"doc": "https://platform.openai.com/docs",
			"models": {
				"gpt-4": {
					"id": "gpt-4",
					"name": "GPT-4 Test",
					"family": "gpt-4",
					"attachment": true,
					"reasoning": true,
					"tool_call": true,
					"modalities": {"input": ["text", "image"], "output": ["text"]},
					"open_weights": false,
					"cost": {"input": 0.03, "output": 0.06},
					"limit": {"context": 8192, "output": 4096}
				}
			}
		}
	}`

	modelsDevServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api.json" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(modelsDevResponse))
			return
		}
		http.NotFound(w, r)
	}))
	defer modelsDevServer.Close()

	// Load models.dev cache with custom client that redirects to mock server
	httpClient := modelsDevServer.Client()
	httpClient.Transport = &mockTransport{roundTripFunc: func(req *http.Request) (*http.Response, error) {
		if req.URL.String() == "https://models.dev/api.json" {
			return http.Get(modelsDevServer.URL + "/api.json")
		}
		return nil, fmt.Errorf("unexpected request to %s", req.URL.String())
	}}

	ctx := context.Background()
	err := provider.LoadModelsDevWithClient(ctx, httpClient)
	if err != nil {
		t.Fatalf("LoadModelsDevWithClient failed: %v", err)
	}

	// Verify cache is loaded
	cache := provider.GetModelsDevCache()
	if cache == nil {
		t.Fatal("GetModelsDevCache returned nil after loading")
	}

	// Create mock OpenAI-compatible server that returns models matching the cache
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" && r.Method == "GET" {
			response := map[string]interface{}{
				"data": []map[string]interface{}{
					{"id": "gpt-4", "owned_by": "openai"},
					{"id": "gpt-3.5-test", "owned_by": "openai"},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)
			return
		}
		http.NotFound(w, r)
	}))
	defer mockServer.Close()

	_, r := newTestHandlerWithRouter(t)

	// Create provider with mock server URL
	providerData := fmt.Sprintf(`{"name": "test-discover-cache", "base_url": "%s", "api_key": "sk-test123"}`, mockServer.URL)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d: %s", rec.Code, rec.Body.String())
	}

	var createResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &createResp); err != nil {
		t.Fatalf("Failed to parse create response: %v", err)
	}

	// Discover models for this provider
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/providers/"+createResp.ID+"/discover", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response struct {
		Discovered int `json:"discovered"`
		Models     []struct {
			ModelID       string `json:"model_id"`
			DisplayName   string `json:"display_name"`
			ContextLength *int   `json:"context_length"`
			OwnedBy       string `json:"owned_by"`
		} `json:"models"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if response.Discovered != 2 {
		t.Errorf("Expected discovered=2, got %d", response.Discovered)
	}
	if len(response.Models) != 2 {
		t.Fatalf("Expected 2 models, got %d", len(response.Models))
	}

	// Verify gpt-4 was enriched from models.dev cache
	var gpt4 *struct {
		ModelID       string `json:"model_id"`
		DisplayName   string `json:"display_name"`
		ContextLength *int   `json:"context_length"`
		OwnedBy       string `json:"owned_by"`
	}
	for i := range response.Models {
		if response.Models[i].ModelID == "gpt-4" {
			gpt4 = &response.Models[i]
			break
		}
	}
	if gpt4 == nil {
		t.Fatal("gpt-4 model not found in response")
	}
	if gpt4.DisplayName != "GPT-4 Test" {
		t.Errorf("Expected display_name='GPT-4 Test' from models.dev enrichment, got %q", gpt4.DisplayName)
	}
	if gpt4.ContextLength == nil || *gpt4.ContextLength != 8192 {
		t.Errorf("Expected context_length=8192 from models.dev enrichment, got %v", gpt4.ContextLength)
	}
	if gpt4.OwnedBy != "openai" {
		t.Errorf("Expected owned_by='openai' from discovery catalog, got %q", gpt4.OwnedBy)
	}
}

// mockTransport implements http.RoundTripper for test request interception.
type mockTransport struct {
	roundTripFunc func(req *http.Request) (*http.Response, error)
}

func (m *mockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if m.roundTripFunc != nil {
		return m.roundTripFunc(req)
	}
	return nil, fmt.Errorf("unexpected request to %s", req.URL.String())
}
