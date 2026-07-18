package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hugalafutro/model-hotel/internal/auth"
	"github.com/hugalafutro/model-hotel/internal/failover"
	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/provider"
)

// TestDiscoverAllModels_AllDisabled tests that DiscoverAllModels skips all
// disabled providers and returns an empty result structure.
func TestDiscoverAllModels_AllDisabled(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Create providers and then disable them (CreateProviderRequest doesn't have enabled field)
	var providerIDs []string
	for i := range 3 {
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
		Results    []any `json:"results"`
		Succeeded  int   `json:"succeeded"`
		Failed     int   `json:"failed"`
		Discovered int   `json:"discovered"`
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
		if r.URL.Path == "/models" && r.Method == "GET" {
			response := map[string]any{
				"data": []map[string]any{
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
		Discovered int   `json:"discovered"`
		Models     []any `json:"models"`
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
		if r.URL.Path == "/models" && r.Method == "GET" {
			response := map[string]any{
				"data": []map[string]any{
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

	// Load models.dev cache with custom client that redirects to mock server.
	// Use a fresh http.Client (not modelsDevServer.Client()) so the inner
	// modelsDevServer.Client().Get call uses the server's own transport,
	// not the mockTransport we're installing here.
	mockServerClient := modelsDevServer.Client()
	httpClient := &http.Client{Transport: &mockTransport{roundTripFunc: func(req *http.Request) (*http.Response, error) {
		if req.URL.String() == "https://models.dev/api.json" {
			return mockServerClient.Get(modelsDevServer.URL + "/api.json")
		}
		return nil, fmt.Errorf("unexpected request to %s", req.URL.String())
	}}}

	ctx := context.Background()
	err := provider.LoadModelsDevWithClient(ctx, httpClient)
	if err != nil {
		t.Fatalf("LoadModelsDevWithClient failed: %v", err)
	}

	// Verify cache is loaded
	cache := provider.GetModelsDevCache()
	if cache == nil {
		t.Fatal("GetModelsDevCache returned nil after loading")
		return
	}

	// Create mock OpenAI-compatible server that returns models matching the cache
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/models" && r.Method == "GET" {
			response := map[string]any{
				"data": []map[string]any{
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
	if gpt4 == nil { //nolint:staticcheck // SA5011
		t.Fatal("gpt-4 model not found in response")
	}
	if gpt4.DisplayName != "GPT-4 Test" { //nolint:staticcheck // SA5011
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

// ---------------------------------------------------------------------------
// Tests moved from discovery_coverage_test.go
// ---------------------------------------------------------------------------

const testMasterKeyForDiscovery = "testmasterkey1234567890abcdef"

// encryptTestKey creates encrypted key material for test providers.
func encryptTestKey(t *testing.T, apiKey, masterKey string) (ek, kn, ks []byte) {
	t.Helper()
	kp, err := auth.Encrypt(apiKey, masterKey)
	if err != nil {
		t.Fatalf("failed to encrypt key: %v", err)
	}
	return kp.Ciphertext, kp.Nonce, kp.Salt
}

// createTestProvider creates a provider with encrypted key material.
func createTestProvider(t *testing.T, name, baseURL, masterKey string) *provider.Provider {
	t.Helper()
	ek, kn, ks := encryptTestKey(t, "test-api-key", masterKey)
	return &provider.Provider{
		ID:           uuid.New(),
		Name:         name,
		BaseURL:      baseURL,
		Enabled:      true,
		EncryptedKey: ek,
		KeyNonce:     kn,
		KeySalt:      ks,
	}
}

// =============================================================================
// DiscoverProviderModels Error Path Tests (Integration with real DB)
// =============================================================================

// TestDiscoverProviderModels_ModelRepoError verifies that a broken model repo
// fails the discovery request with a 500. The snapshot is the first DB touch,
// so the closed pool surfaces there.
func TestDiscoverProviderModels_ModelRepoError(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Create a mock OpenAI-compatible server
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{
					{"id": "test-model-1", "owned_by": "test", "object": "model"},
				},
			})
		}
	}))
	defer mockServer.Close()

	// Create provider via API
	providerData := fmt.Sprintf(`{"name":"upsert-error-test","base_url":"%s/v1","api_key":"sk-test"}`, mockServer.URL)
	req := httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("create provider: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode created provider: %v", err)
	}

	// Override newModelRepo to return a repo backed by a closed pool
	origNewModelRepo := newModelRepo
	defer func() { newModelRepo = origNewModelRepo }()

	closedPool, _ := pgxpool.New(context.Background(), "postgres://invalid:invalid@invalid/invalid")
	closedPool.Close()

	newModelRepo = func(pool *pgxpool.Pool) *model.Repository {
		return model.NewRepository(closedPool)
	}

	// Call discover endpoint
	req = httptest.NewRequest(http.MethodPost, "/providers/"+created.ID+"/discover", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "failed to snapshot models") {
		t.Errorf("expected error about model snapshot, got %q", w.Body.String())
	}
}

// TestDiscoverProviderModels_UpsertError covers the upsert 500 branch: a
// read-only pool lets the pre-scan snapshot (SELECT) succeed while the
// model upsert (INSERT) fails.
func TestDiscoverProviderModels_UpsertError(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Create a mock OpenAI-compatible server
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{
					{"id": "upsert-error-model", "owned_by": "test", "object": "model"},
				},
			})
		}
	}))
	defer mockServer.Close()

	providerData := fmt.Sprintf(`{"name":"upsert-error-branch-test","base_url":"%s/v1","api_key":"sk-test"}`, mockServer.URL)
	req := httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("create provider: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode created provider: %v", err)
	}

	// Override newModelRepo with a repo on a read-only connection: the
	// snapshot SELECT succeeds, the first Upsert INSERT fails.
	roURL := apiTestDBURL
	if strings.Contains(roURL, "?") {
		roURL += "&default_transaction_read_only=on"
	} else {
		roURL += "?default_transaction_read_only=on"
	}
	roPool, err := pgxpool.New(context.Background(), roURL)
	if err != nil {
		t.Fatalf("create read-only pool: %v", err)
	}
	defer roPool.Close()

	origNewModelRepo := newModelRepo
	defer func() { newModelRepo = origNewModelRepo }()
	newModelRepo = func(pool *pgxpool.Pool) *model.Repository {
		return model.NewRepository(roPool)
	}

	req = httptest.NewRequest(http.MethodPost, "/providers/"+created.ID+"/discover", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "failed to upsert model") {
		t.Errorf("expected error about upsert, got %q", w.Body.String())
	}
}

// TestDiscoverProviderModels_DoesNotRecordMisses guards the contract that the
// interactive single-provider handler never records misses (that moved to the
// background sweep so the confirmation-probe backoff cannot overrun the route's
// 60s timeout). modelRepoRecordMissing is forced to error: if the handler were
// to start recording again it would surface as a 500, so a green 200 here proves
// the handler bypasses recording entirely.
func TestDiscoverProviderModels_DoesNotRecordMisses(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Create a mock OpenAI-compatible server
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{
					{"id": "test-model-1", "owned_by": "test", "object": "model"},
				},
			})
		}
	}))
	defer mockServer.Close()

	// Create provider via API
	providerData := fmt.Sprintf(`{"name":"disable-missing-error-test","base_url":"%s/v1","api_key":"sk-test"}`, mockServer.URL)
	req := httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("create provider: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode created provider: %v", err)
	}

	// Override modelRepoRecordMissing to return error
	origModelRepoRecordMissing := modelRepoRecordMissing
	defer func() { modelRepoRecordMissing = origModelRepoRecordMissing }()

	modelRepoRecordMissing = func(repo *model.Repository, ctx context.Context, providerID uuid.UUID, providerName string, modelIDs []string) ([]model.DisabledModelRef, []model.DisabledModelRef, error) {
		return nil, nil, errors.New("record missing models error")
	}

	// Call discover endpoint. The interactive handler must not touch the miss
	// recorder, so the forced record error is never reached and the scan returns
	// 200. A 500 here would mean recording crept back onto the request path.
	req = httptest.NewRequest(http.MethodPost, "/providers/"+created.ID+"/discover", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200 (handler bypasses miss recording), got %d: %s", w.Code, w.Body.String())
	}
}

func TestDiscoverProviderModels_SyncForModelError(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Create a mock OpenAI-compatible server
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{
					{"id": "test-model-1", "owned_by": "test", "object": "model"},
				},
			})
		}
	}))
	defer mockServer.Close()

	// Create provider via API
	providerData := fmt.Sprintf(`{"name":"sync-for-model-error-test","base_url":"%s/v1","api_key":"sk-test"}`, mockServer.URL)
	req := httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("create provider: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode created provider: %v", err)
	}

	// Override failoverRepoSyncForModel to return error
	origFailoverRepoSyncForModel := failoverRepoSyncForModel
	defer func() { failoverRepoSyncForModel = origFailoverRepoSyncForModel }()

	failoverRepoSyncForModel = func(repo *failover.Repository, ctx context.Context, modelID string) (*failover.SyncResult, error) {
		return nil, errors.New("sync for model error")
	}

	// Call discover endpoint
	req = httptest.NewRequest(http.MethodPost, "/providers/"+created.ID+"/discover", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "failed to sync failover") {
		t.Errorf("expected error about sync failover, got %q", w.Body.String())
	}
}

func TestDiscoverProviderModels_DBExecError(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Create a mock OpenAI-compatible server
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{
					{"id": "test-model-1", "owned_by": "test", "object": "model"},
				},
			})
		}
	}))
	defer mockServer.Close()

	// Create provider via API
	providerData := fmt.Sprintf(`{"name":"dbexec-error-test","base_url":"%s/v1","api_key":"sk-test"}`, mockServer.URL)
	req := httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("create provider: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode created provider: %v", err)
	}

	// Override dbExec to return error
	origDBExec := dbExec
	defer func() { dbExec = origDBExec }()

	dbExec = func(pool *pgxpool.Pool, ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
		return pgconn.CommandTag{}, errors.New("dbexec error")
	}

	// Call discover endpoint
	req = httptest.NewRequest(http.MethodPost, "/providers/"+created.ID+"/discover", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "failed to update provider") {
		t.Errorf("expected error about update provider, got %q", w.Body.String())
	}
}

// =============================================================================
// DiscoverAllModels Tests
// =============================================================================

func TestDiscoverAllModels_ListError(t *testing.T) {
	// Use testHandler with mock provider store returning error on List
	mockProv := &mockProviderStore{
		listFn: func(ctx context.Context) ([]*provider.Provider, error) {
			return nil, errors.New("list providers error")
		},
	}
	mockAuth := &mockAdminAuth{validateFn: func(token string) bool { return true }}
	h := testHandler(mockProv, nil, nil, mockAuth, nil)

	// Set up chi router
	r := chi.NewRouter()
	r.Post("/providers/discover-all", h.DiscoverAllModels)

	req := httptest.NewRequest(http.MethodPost, "/providers/discover-all", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "failed to list providers") {
		t.Errorf("expected error about list providers, got %q", w.Body.String())
	}
}

func TestDiscoverAllModels_ModelsDevCacheEnrichment(t *testing.T) {
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
	mockServerClient := modelsDevServer.Client()
	httpClient := &http.Client{Transport: &mockTransport{roundTripFunc: func(req *http.Request) (*http.Response, error) {
		if req.URL.String() == "https://models.dev/api.json" {
			return mockServerClient.Get(modelsDevServer.URL + "/api.json")
		}
		return nil, fmt.Errorf("unexpected request to %s", req.URL.String())
	}}}

	ctx := context.Background()
	err := provider.LoadModelsDevWithClient(ctx, httpClient)
	if err != nil {
		t.Fatalf("LoadModelsDevWithClient failed: %v", err)
	}

	// Verify cache is loaded
	cache := provider.GetModelsDevCache()
	if cache == nil {
		t.Fatal("GetModelsDevCache returned nil after loading")
		return
	}

	// Create a mock OpenAI-compatible server that returns models matching the cache
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{
					{"id": "gpt-4", "owned_by": "openai", "object": "model"},
				},
			})
		}
	}))
	defer mockServer.Close()

	_, r := newTestHandlerWithRouter(t)

	// Create provider via API
	providerData := fmt.Sprintf(`{"name":"models-dev-enrich-test","base_url":"%s/v1","api_key":"sk-test"}`, mockServer.URL)
	req := httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("create provider: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// Call discover-all endpoint
	req = httptest.NewRequest(http.MethodPost, "/providers/discover-all", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["succeeded"].(float64) < 1 {
		t.Errorf("expected at least 1 succeeded, got %v", resp["succeeded"])
	}
}

func TestDiscoverAllModels_UpsertError(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Create a mock OpenAI-compatible server
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{
					{"id": "test-model-1", "owned_by": "test", "object": "model"},
				},
			})
		}
	}))
	defer mockServer.Close()

	// Create provider via API
	providerData := fmt.Sprintf(`{"name":"discover-all-upsert-error","base_url":"%s/v1","api_key":"sk-test"}`, mockServer.URL)
	req := httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("create provider: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// Override newModelRepo to return a repo backed by a closed pool
	origNewModelRepo := newModelRepo
	defer func() { newModelRepo = origNewModelRepo }()

	closedPool, _ := pgxpool.New(context.Background(), "postgres://invalid:invalid@invalid/invalid")
	closedPool.Close()

	newModelRepo = func(pool *pgxpool.Pool) *model.Repository {
		return model.NewRepository(closedPool)
	}

	// Call discover-all endpoint (should still return 200, just log warning)
	req = httptest.NewRequest(http.MethodPost, "/providers/discover-all", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// DiscoverAllModels logs and continues, so response should still be 200
	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}
}

// TestDiscoverAllModels_DisableMissingError drives the background sweep
// (recordMisses=true) with the miss recorder forced to error, and asserts the
// sweep tolerates it: a per-provider DB hiccup must be logged and skipped, never
// abort the scan. This is the path that actually records misses now, so it is
// driven directly rather than through the interactive discover-all handler
// (which passes recordMisses=false).
func TestDiscoverAllModels_DisableMissingError(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	// Create a mock OpenAI-compatible server
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{
					{"id": "test-model-1", "owned_by": "test", "object": "model"},
				},
			})
		}
	}))
	defer mockServer.Close()

	// Create provider via API
	providerData := fmt.Sprintf(`{"name":"discover-all-disable-error","base_url":"%s/v1","api_key":"sk-test"}`, mockServer.URL)
	req := httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("create provider: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// Override modelRepoRecordMissing to return error
	origModelRepoRecordMissing := modelRepoRecordMissing
	defer func() { modelRepoRecordMissing = origModelRepoRecordMissing }()

	modelRepoRecordMissing = func(repo *model.Repository, ctx context.Context, providerID uuid.UUID, providerName string, modelIDs []string) ([]model.DisabledModelRef, []model.DisabledModelRef, error) {
		return nil, nil, errors.New("record missing models error")
	}

	// Drive the recording path directly; a per-provider record error must be
	// swallowed (logged at debug) rather than abort the sweep.
	results, _, _, _, err := h.discoverAllProviders(context.Background(), true)
	if err != nil {
		t.Fatalf("sweep must tolerate a record-missing error, got %v", err)
	}
	if len(results) == 0 {
		t.Fatalf("expected at least one provider result despite the record error")
	}
	for i := range results {
		if results[i].Error != "" {
			t.Errorf("provider %s: scan must complete despite record error, got %q", results[i].ProviderName, results[i].Error)
		}
	}
}

func TestDiscoverAllModels_SyncForModelError(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Create a mock OpenAI-compatible server
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{
					{"id": "test-model-1", "owned_by": "test", "object": "model"},
				},
			})
		}
	}))
	defer mockServer.Close()

	// Create provider via API
	providerData := fmt.Sprintf(`{"name":"discover-all-sync-error","base_url":"%s/v1","api_key":"sk-test"}`, mockServer.URL)
	req := httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("create provider: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// Override failoverRepoSyncForModel to return error
	origFailoverRepoSyncForModel := failoverRepoSyncForModel
	defer func() { failoverRepoSyncForModel = origFailoverRepoSyncForModel }()

	failoverRepoSyncForModel = func(repo *failover.Repository, ctx context.Context, modelID string) (*failover.SyncResult, error) {
		return nil, errors.New("sync for model error")
	}

	// Call discover-all endpoint (should still return 200, just log debug)
	req = httptest.NewRequest(http.MethodPost, "/providers/discover-all", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDiscoverAllModels_DBExecError(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Create a mock OpenAI-compatible server
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{
					{"id": "test-model-1", "owned_by": "test", "object": "model"},
				},
			})
		}
	}))
	defer mockServer.Close()

	// Create provider via API
	providerData := fmt.Sprintf(`{"name":"discover-all-dbexec-error","base_url":"%s/v1","api_key":"sk-test"}`, mockServer.URL)
	req := httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("create provider: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// Override dbExec to return error
	origDBExec := dbExec
	defer func() { dbExec = origDBExec }()

	dbExec = func(pool *pgxpool.Pool, ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
		return pgconn.CommandTag{}, errors.New("dbexec error")
	}

	// Call discover-all endpoint (should still return 200, just log debug)
	req = httptest.NewRequest(http.MethodPost, "/providers/discover-all", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}
}
