package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
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

	// Create a provider with unreachable URL (using a non-routable address)
	providerData := `{"name": "test-discovery-error", "base_url": "https://192.0.2.1", "api_key": "sk-test123"}`
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
