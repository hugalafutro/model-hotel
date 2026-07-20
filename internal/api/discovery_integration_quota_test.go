package api

import (
	"context"
	"encoding/json"
	"errors"
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
	"github.com/hugalafutro/model-hotel/internal/quota"
)

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
	for i := range 2 {
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
		Results   []any `json:"results"`
		Refreshed int   `json:"refreshed"`
		Failed    int   `json:"failed"`
		Skipped   int   `json:"skipped"`
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
	// serveQuota emits a single generic unsupported-type message for all three endpoints.
	if !strings.Contains(rec.Body.String(), "not supported for this provider type") {
		t.Errorf("Expected error about unsupported type, got: %s", rec.Body.String())
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
		Results   []any `json:"results"`
		Refreshed int   `json:"refreshed"`
		Failed    int   `json:"failed"`
		Skipped   int   `json:"skipped"`
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

// TestGetProviderUsage_ZAICodingError tests that GetProviderUsage handles
// z.ai API errors (note: z.ai returns 200 with error JSON for invalid keys).
func TestGetProviderUsage_ZAICodingError(t *testing.T) {
	// Override newDiscoveryService with mock transport to avoid real API calls
	// Note: Must override AFTER newTestHandlerWithRouter since NewHandler sets it
	_, r := newTestHandlerWithRouter(t)

	orig := newDiscoveryService
	defer func() { newDiscoveryService = orig }()

	newDiscoveryService = func() *provider.DiscoveryService {
		ds := provider.NewDiscoveryServiceWithHTTPClient(&http.Client{
			Transport: &mockTransport{
				roundTripFunc: func(req *http.Request) (*http.Response, error) {
					if strings.Contains(req.URL.Host, "api.z.ai") {
						return &http.Response{
							StatusCode: http.StatusInternalServerError,
							Body:       io.NopCloser(strings.NewReader(`{"error":"invalid key"}`)),
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

	// The handler returns 500 with plain text error message when API call fails
	// Verify the response code is 500 (error case)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("Expected 500 for z.ai API error, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "failed to fetch usage") {
		t.Errorf("Expected error about failed to fetch usage, got: %s", rec.Body.String())
	}
}

// TestGetProviderUsage_KimiCodeError tests that GetProviderUsage handles
// Kimi Code API errors. Unlike z.ai (hardcoded quota URL), Kimi Code builds
// its quota URL from the provider's own base_url + "/usages", but
// DetectProviderType still routes purely by hostname, so the provider row's
// base_url must be an api.kimi.com URL to select the kimi-code arm.
func TestGetProviderUsage_KimiCodeError(t *testing.T) {
	// Override newDiscoveryService with mock transport to avoid real API calls
	// Note: Must override AFTER newTestHandlerWithRouter since NewHandler sets it
	_, r := newTestHandlerWithRouter(t)

	orig := newDiscoveryService
	defer func() { newDiscoveryService = orig }()

	newDiscoveryService = func() *provider.DiscoveryService {
		ds := provider.NewDiscoveryServiceWithHTTPClient(&http.Client{
			Transport: &mockTransport{
				roundTripFunc: func(req *http.Request) (*http.Response, error) {
					if strings.Contains(req.URL.Host, "api.kimi.com") {
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

	// Create a provider with a Kimi Code URL and fake key
	providerName := fmt.Sprintf("test-kimi-code-error-%s", uuid.New().String()[:8])
	providerData := fmt.Sprintf(`{"name": "%s", "base_url": "https://api.kimi.com/coding/v1", "api_key": "fake-key"}`, providerName)
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

	// Try to get usage - the fake key causes a 500 from the mock transport,
	// which exercises the kimi-code case in GetProviderUsage.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/providers/"+createResp.ID+"/usage", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("Expected 500 for Kimi Code API error, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "failed to fetch usage") {
		t.Errorf("Expected error about failed to fetch usage, got: %s", rec.Body.String())
	}
}

// TestGetProviderUsage_NanoGPTError tests that GetProviderUsage returns 500
// when the NanoGPT API call fails with an invalid key.
func TestGetProviderUsage_NanoGPTError(t *testing.T) {
	// Override newDiscoveryService with mock transport to avoid real API calls
	// Note: Must override AFTER newTestHandlerWithRouter since NewHandler sets it
	_, r := newTestHandlerWithRouter(t)

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
	// Override newDiscoveryService with mock transport to avoid real API calls
	// Note: Must override AFTER newTestHandlerWithRouter since NewHandler sets it
	_, r := newTestHandlerWithRouter(t)

	orig := newDiscoveryService
	defer func() { newDiscoveryService = orig }()

	newDiscoveryService = func() *provider.DiscoveryService {
		ds := provider.NewDiscoveryServiceWithHTTPClient(&http.Client{
			Transport: &mockTransport{
				roundTripFunc: func(req *http.Request) (*http.Response, error) {
					if strings.Contains(req.URL.Host, "openrouter.ai") {
						return &http.Response{
							StatusCode: http.StatusInternalServerError,
							Body:       io.NopCloser(strings.NewReader(`{"error":"invalid key"}`)),
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
	// Read-through reports the snapshot kind ("usage") as the failed resource.
	if !strings.Contains(rec.Body.String(), "failed to fetch usage") {
		t.Errorf("Expected error about failed to fetch usage, got: %s", rec.Body.String())
	}
}

// TestGetProviderBalance_DeepSeekError tests that GetProviderBalance returns 500
// when the DeepSeek API call fails with an invalid key.
func TestGetProviderBalance_DeepSeekError(t *testing.T) {
	// Override newDiscoveryService with mock transport to avoid real API calls
	// Note: Must override AFTER newTestHandlerWithRouter since NewHandler sets it
	_, r := newTestHandlerWithRouter(t)

	orig := newDiscoveryService
	defer func() { newDiscoveryService = orig }()

	newDiscoveryService = func() *provider.DiscoveryService {
		ds := provider.NewDiscoveryServiceWithHTTPClient(&http.Client{
			Transport: &mockTransport{
				roundTripFunc: func(req *http.Request) (*http.Response, error) {
					if strings.Contains(req.URL.Host, "api.deepseek.com") {
						return &http.Response{
							StatusCode: http.StatusInternalServerError,
							Body:       io.NopCloser(strings.NewReader(`{"error":"invalid key"}`)),
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
	// Override newDiscoveryService with mock transport to avoid real API calls
	// Note: Must override AFTER newTestHandlerWithRouter since NewHandler sets it
	_, r := newTestHandlerWithRouter(t)

	orig := newDiscoveryService
	defer func() { newDiscoveryService = orig }()

	newDiscoveryService = func() *provider.DiscoveryService {
		ds := provider.NewDiscoveryServiceWithHTTPClient(&http.Client{
			Transport: &mockTransport{
				roundTripFunc: func(req *http.Request) (*http.Response, error) {
					if strings.Contains(req.URL.Host, "ollama.com") {
						return &http.Response{
							StatusCode: http.StatusInternalServerError,
							Body:       io.NopCloser(strings.NewReader(`{"error":"invalid key"}`)),
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
	// Read-through reports the snapshot kind ("account") as the failed resource.
	if !strings.Contains(rec.Body.String(), "failed to fetch account") {
		t.Errorf("Expected error about failed to fetch account, got: %s", rec.Body.String())
	}
}

// TestRefreshAllQuotas_WithSupportedTypes tests that RefreshAllQuotas handles
// multiple provider types with errors for unsupported types.
func TestRefreshAllQuotas_WithSupportedTypes(t *testing.T) {
	// Override newDiscoveryService with mock transport to avoid real API calls
	// Note: Must override AFTER newTestHandlerWithRouter since NewHandler sets it
	_, r := newTestHandlerWithRouter(t)

	orig := newDiscoveryService
	defer func() { newDiscoveryService = orig }()

	newDiscoveryService = func() *provider.DiscoveryService {
		ds := provider.NewDiscoveryServiceWithHTTPClient(&http.Client{
			Transport: &mockTransport{
				roundTripFunc: func(req *http.Request) (*http.Response, error) {
					// NanoGPT returns 503
					if strings.Contains(req.URL.Host, "api.nano-gpt.com") {
						return &http.Response{
							StatusCode: http.StatusServiceUnavailable,
							Body:       io.NopCloser(strings.NewReader("api.nano-gpt.com is currently in development. Please use https://nano-gpt.com/api instead.")),
							Header:     make(http.Header),
						}, nil
					}
					// z.ai returns 500 for fake keys
					if strings.Contains(req.URL.Host, "api.z.ai") {
						return &http.Response{
							StatusCode: http.StatusInternalServerError,
							Body:       io.NopCloser(strings.NewReader(`{"error":"invalid key"}`)),
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
	// Override newDiscoveryService with mock transport to avoid real API calls
	// Note: Must override AFTER newTestHandlerWithRouter since NewHandler sets it
	_, r := newTestHandlerWithRouter(t)

	orig := newDiscoveryService
	defer func() { newDiscoveryService = orig }()

	newDiscoveryService = func() *provider.DiscoveryService {
		ds := provider.NewDiscoveryServiceWithHTTPClient(&http.Client{
			Transport: &mockTransport{
				roundTripFunc: func(req *http.Request) (*http.Response, error) {
					if strings.Contains(req.URL.Host, "api.deepseek.com") {
						return &http.Response{
							StatusCode: http.StatusInternalServerError,
							Body:       io.NopCloser(strings.NewReader(`{"error":"invalid key"}`)),
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
	// Override newDiscoveryService with mock transport to avoid real API calls
	// Note: Must override AFTER newTestHandlerWithRouter since NewHandler sets it
	_, r := newTestHandlerWithRouter(t)

	orig := newDiscoveryService
	defer func() { newDiscoveryService = orig }()

	newDiscoveryService = func() *provider.DiscoveryService {
		ds := provider.NewDiscoveryServiceWithHTTPClient(&http.Client{
			Transport: &mockTransport{
				roundTripFunc: func(req *http.Request) (*http.Response, error) {
					if strings.Contains(req.URL.Host, "ollama.com") {
						return &http.Response{
							StatusCode: http.StatusInternalServerError,
							Body:       io.NopCloser(strings.NewReader(`{"error":"invalid key"}`)),
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
	// Override newDiscoveryService with mock transport to avoid real API calls
	// Note: Must override AFTER newTestHandlerWithRouter since NewHandler sets it
	_, r := newTestHandlerWithRouter(t)

	orig := newDiscoveryService
	defer func() { newDiscoveryService = orig }()

	newDiscoveryService = func() *provider.DiscoveryService {
		ds := provider.NewDiscoveryServiceWithHTTPClient(&http.Client{
			Transport: &mockTransport{
				roundTripFunc: func(req *http.Request) (*http.Response, error) {
					if strings.Contains(req.URL.Host, "openrouter.ai") {
						return &http.Response{
							StatusCode: http.StatusInternalServerError,
							Body:       io.NopCloser(strings.NewReader(`{"error":"invalid key"}`)),
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

// TestRefreshAllQuotas_KimiCodeError tests that RefreshAllQuotas handles
// Kimi Code API errors correctly, exercising the kimi-code arm of the
// provider-type switch in RefreshAllQuotas.
func TestRefreshAllQuotas_KimiCodeError(t *testing.T) {
	// Override newDiscoveryService with mock transport to avoid real API calls
	// Note: Must override AFTER newTestHandlerWithRouter since NewHandler sets it
	_, r := newTestHandlerWithRouter(t)

	orig := newDiscoveryService
	defer func() { newDiscoveryService = orig }()

	newDiscoveryService = func() *provider.DiscoveryService {
		ds := provider.NewDiscoveryServiceWithHTTPClient(&http.Client{
			Transport: &mockTransport{
				roundTripFunc: func(req *http.Request) (*http.Response, error) {
					if strings.Contains(req.URL.Host, "api.kimi.com") {
						return &http.Response{
							StatusCode: http.StatusInternalServerError,
							Body:       io.NopCloser(strings.NewReader(`{"error":"invalid key"}`)),
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

	// Create a provider with a Kimi Code URL and fake key
	providerName := fmt.Sprintf("test-quota-kimi-code-%s", uuid.New().String()[:8])
	providerData := fmt.Sprintf(`{"name": "%s", "base_url": "https://api.kimi.com/coding/v1", "api_key": "fake-key"}`, providerName)
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

	// Verify the result has provider_type: "kimi-code" and non-empty error
	var kimiCodeFound bool
	for _, result := range response.Results {
		if result.ProviderType == "kimi-code" && result.Error != "" {
			kimiCodeFound = true
			break
		}
	}
	if !kimiCodeFound {
		t.Error("Expected kimi-code result with error in results")
	}
}

// kimiCodeUsageSuccessBody is a well-formed /usages payload used to drive the
// kimi-code success arms.
const kimiCodeUsageSuccessBody = `{
	"user": {"userId": "u-1", "region": "REGION_OVERSEA", "membership": {"level": "LEVEL_BASIC"}},
	"usage": {"limit": "100", "remaining": "42", "resetTime": "2026-07-26T12:10:02Z"},
	"limits": [{"window": {"duration": 300, "timeUnit": "TIME_UNIT_MINUTE"}, "detail": {"limit": "100", "remaining": "42", "resetTime": "2026-07-19T17:10:02Z"}}],
	"parallel": {"limit": "10"},
	"totalQuota": {"limit": "100", "remaining": "99"},
	"subType": "TYPE_PURCHASE"
}`

// TestGetProviderUsage_KimiCodeSuccess exercises the success arm of the
// kimi-code case in GetProviderUsage: a 200 /usages response is decoded and
// written back as JSON. The mock transport intercepts the api.kimi.com request
// so no real network call is made while DetectProviderType still routes by host.
func TestGetProviderUsage_KimiCodeSuccess(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	orig := newDiscoveryService
	defer func() { newDiscoveryService = orig }()

	newDiscoveryService = func() *provider.DiscoveryService {
		ds := provider.NewDiscoveryServiceWithHTTPClient(&http.Client{
			Transport: &mockTransport{
				roundTripFunc: func(req *http.Request) (*http.Response, error) {
					if strings.Contains(req.URL.Host, "api.kimi.com") {
						return &http.Response{
							StatusCode: http.StatusOK,
							Body:       io.NopCloser(strings.NewReader(kimiCodeUsageSuccessBody)),
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

	providerName := fmt.Sprintf("test-kimi-code-ok-%s", uuid.New().String()[:8])
	providerData := fmt.Sprintf(`{"name": "%s", "base_url": "https://api.kimi.com/coding/v1", "api_key": "fake-key"}`, providerName)
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

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/providers/"+createResp.ID+"/usage", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 for Kimi Code usage success, got %d: %s", rec.Code, rec.Body.String())
	}
	var quota provider.KimiCodeQuotaResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &quota); err != nil {
		t.Fatalf("Failed to parse usage response: %v", err)
	}
	if quota.Usage.Remaining != "42" {
		t.Errorf("Expected usage remaining 42, got %q", quota.Usage.Remaining)
	}
	if len(quota.Limits) != 1 || quota.Limits[0].Window.Duration != 300 {
		t.Errorf("Expected one 300-minute window, got %+v", quota.Limits)
	}
}

// TestRefreshAllQuotas_KimiCodeSuccess exercises the success arm of the
// kimi-code case in RefreshAllQuotas: a 200 /usages response records the
// provider as refreshed rather than failed.
func TestRefreshAllQuotas_KimiCodeSuccess(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	orig := newDiscoveryService
	defer func() { newDiscoveryService = orig }()

	newDiscoveryService = func() *provider.DiscoveryService {
		ds := provider.NewDiscoveryServiceWithHTTPClient(&http.Client{
			Transport: &mockTransport{
				roundTripFunc: func(req *http.Request) (*http.Response, error) {
					if strings.Contains(req.URL.Host, "api.kimi.com") {
						return &http.Response{
							StatusCode: http.StatusOK,
							Body:       io.NopCloser(strings.NewReader(kimiCodeUsageSuccessBody)),
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

	providerName := fmt.Sprintf("test-quota-kimi-ok-%s", uuid.New().String()[:8])
	providerData := fmt.Sprintf(`{"name": "%s", "base_url": "https://api.kimi.com/coding/v1", "api_key": "fake-key"}`, providerName)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d: %s", rec.Code, rec.Body.String())
	}

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

	if response.Refreshed < 1 {
		t.Errorf("Expected refreshed >= 1, got %d", response.Refreshed)
	}

	var kimiCodeRefreshed bool
	for _, result := range response.Results {
		if result.ProviderType == "kimi-code" && result.Refreshed && result.Error == "" {
			kimiCodeRefreshed = true
			break
		}
	}
	if !kimiCodeRefreshed {
		t.Error("Expected kimi-code result marked refreshed with no error")
	}
}

// minimaxQuotaSuccessBody is the reference /token_plan/remains payload
// (live-captured) used to drive the minimax success arms.
const minimaxQuotaSuccessBody = `{"model_remains":[{"start_time":1784473200000,"end_time":1784491200000,"remains_time":16420081,"current_interval_total_count":0,"current_interval_usage_count":0,"model_name":"general","current_weekly_total_count":0,"current_weekly_usage_count":0,"weekly_start_time":1783900800000,"weekly_end_time":1784505600000,"weekly_remains_time":30820081,"current_interval_status":1,"current_interval_remaining_percent":100,"current_weekly_status":1,"current_weekly_remaining_percent":100},{"start_time":1784419200000,"end_time":1784505600000,"remains_time":30820081,"current_interval_total_count":0,"current_interval_usage_count":0,"model_name":"video","current_weekly_total_count":0,"current_weekly_usage_count":0,"weekly_start_time":1783900800000,"weekly_end_time":1784505600000,"weekly_remains_time":30820081,"current_interval_status":3,"current_interval_remaining_percent":100,"current_weekly_status":3,"current_weekly_remaining_percent":100}],"base_resp":{"status_code":0,"status_msg":"success"}}`

// TestGetProviderUsage_MiniMaxError tests that GetProviderUsage handles a
// MiniMax API key rejection. DetectProviderType routes purely by hostname
// (api.minimax.io), so the provider row's base_url selects the minimax arm;
// a 401 upstream response is classified by quotaAuthError into the
// dependency-failure envelope rather than the generic 500 error path.
func TestGetProviderUsage_MiniMaxError(t *testing.T) {
	// Override newDiscoveryService with mock transport to avoid real API calls
	// Note: Must override AFTER newTestHandlerWithRouter since NewHandler sets it
	_, r := newTestHandlerWithRouter(t)

	orig := newDiscoveryService
	defer func() { newDiscoveryService = orig }()

	newDiscoveryService = func() *provider.DiscoveryService {
		ds := provider.NewDiscoveryServiceWithHTTPClient(&http.Client{
			Transport: &mockTransport{
				roundTripFunc: func(req *http.Request) (*http.Response, error) {
					if strings.Contains(req.URL.Host, "api.minimax.io") {
						return &http.Response{
							StatusCode: http.StatusUnauthorized,
							Body:       io.NopCloser(strings.NewReader(`{"base_resp":{"status_code":1004,"status_msg":"invalid api key"}}`)),
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

	// Create a provider with a MiniMax URL and fake key
	providerName := fmt.Sprintf("test-minimax-error-%s", uuid.New().String()[:8])
	providerData := fmt.Sprintf(`{"name": "%s", "base_url": "https://api.minimax.io/v1", "api_key": "fake-key"}`, providerName)
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

	// Try to get usage - the fake key causes a 401 from the mock transport,
	// which exercises the minimax case in GetProviderUsage.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/providers/"+createResp.ID+"/usage", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	// Read-through cold-fill maps ErrProviderKeyInvalid to a persisted 424
	// snapshot and reproduces it with an empty body (the client only keys off
	// the status; the badge hides on any non-2xx).
	if rec.Code != http.StatusFailedDependency {
		t.Errorf("Expected 424 for MiniMax key rejection, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestGetProviderUsage_MiniMaxSuccess exercises the success arm of the
// minimax case in GetProviderUsage: a 200 /token_plan/remains response is
// decoded and written back as JSON, passing model_remains through untouched.
// The mock transport intercepts the api.minimax.io request so no real
// network call is made while DetectProviderType still routes by host.
func TestGetProviderUsage_MiniMaxSuccess(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	orig := newDiscoveryService
	defer func() { newDiscoveryService = orig }()

	newDiscoveryService = func() *provider.DiscoveryService {
		ds := provider.NewDiscoveryServiceWithHTTPClient(&http.Client{
			Transport: &mockTransport{
				roundTripFunc: func(req *http.Request) (*http.Response, error) {
					if strings.Contains(req.URL.Host, "api.minimax.io") {
						return &http.Response{
							StatusCode: http.StatusOK,
							Body:       io.NopCloser(strings.NewReader(minimaxQuotaSuccessBody)),
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

	providerName := fmt.Sprintf("test-minimax-ok-%s", uuid.New().String()[:8])
	providerData := fmt.Sprintf(`{"name": "%s", "base_url": "https://api.minimax.io/v1", "api_key": "fake-key"}`, providerName)
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

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/providers/"+createResp.ID+"/usage", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 for MiniMax usage success, got %d: %s", rec.Code, rec.Body.String())
	}
	var quota provider.MiniMaxQuotaResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &quota); err != nil {
		t.Fatalf("Failed to parse usage response: %v", err)
	}
	if len(quota.ModelRemains) != 2 {
		t.Errorf("Expected 2 model_remains entries, got %d", len(quota.ModelRemains))
	}
	if quota.BaseResp.StatusCode != 0 {
		t.Errorf("Expected base_resp.status_code 0, got %d", quota.BaseResp.StatusCode)
	}
}

// TestRefreshAllQuotas_MiniMaxError verifies that a MiniMax API-key rejection
// is unified through fetchQuotaSnapshot: a dead credential (ErrProviderKeyInvalid)
// is persisted as a source="manual" 424 snapshot and reported as refreshed
// rather than surfacing a Go error, matching the read-through model where a 424
// is a valid stored state served back to the dashboard.
func TestRefreshAllQuotas_MiniMaxError(t *testing.T) {
	// Override newDiscoveryService with mock transport to avoid real API calls
	// Note: Must override AFTER newTestHandlerWithRouter since NewHandler sets it
	h, r := newTestHandlerWithRouter(t)

	orig := newDiscoveryService
	defer func() { newDiscoveryService = orig }()

	newDiscoveryService = func() *provider.DiscoveryService {
		ds := provider.NewDiscoveryServiceWithHTTPClient(&http.Client{
			Transport: &mockTransport{
				roundTripFunc: func(req *http.Request) (*http.Response, error) {
					if strings.Contains(req.URL.Host, "api.minimax.io") {
						return &http.Response{
							StatusCode: http.StatusUnauthorized,
							Body:       io.NopCloser(strings.NewReader(`{"base_resp":{"status_code":1004,"status_msg":"invalid api key"}}`)),
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

	// Create provider with MiniMax URL and fake key
	provID, _ := createQuotaProvider(t, r, "https://api.minimax.io/v1")

	// Run refresh-quotas
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/providers/refresh-quotas", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response struct {
		Results []QuotaRefreshResult `json:"results"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// A dead key is refreshed to a 424 snapshot, not surfaced as an error.
	var minimaxFound bool
	for _, result := range response.Results {
		if result.ProviderType == "minimax" {
			minimaxFound = true
			if result.Error != "" {
				t.Errorf("dead key should not surface an error, got %q", result.Error)
			}
			if !result.Refreshed {
				t.Error("dead key should be reported as refreshed (424 snapshot)")
			}
			break
		}
	}
	if !minimaxFound {
		t.Error("Expected minimax result in results")
	}

	// The persisted snapshot is a source="manual" dependency-failure (424).
	snap, err := h.quotaRepo.Get(context.Background(), provID, "usage")
	if err != nil {
		t.Fatalf("get persisted snapshot: %v", err)
	}
	if snap == nil || snap.Source != "manual" || snap.HTTPStatus != http.StatusFailedDependency {
		t.Fatalf("want manual 424 snapshot, got %+v", snap)
	}
}

// TestRefreshAllQuotas_MiniMaxSuccess exercises the success arm of the
// minimax case in RefreshAllQuotas: a 200 /token_plan/remains response
// records the provider as refreshed, not failed.
func TestRefreshAllQuotas_MiniMaxSuccess(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	orig := newDiscoveryService
	defer func() { newDiscoveryService = orig }()

	newDiscoveryService = func() *provider.DiscoveryService {
		ds := provider.NewDiscoveryServiceWithHTTPClient(&http.Client{
			Transport: &mockTransport{
				roundTripFunc: func(req *http.Request) (*http.Response, error) {
					if strings.Contains(req.URL.Host, "api.minimax.io") {
						return &http.Response{
							StatusCode: http.StatusOK,
							Body:       io.NopCloser(strings.NewReader(minimaxQuotaSuccessBody)),
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

	providerName := fmt.Sprintf("test-quota-minimax-ok-%s", uuid.New().String()[:8])
	providerData := fmt.Sprintf(`{"name": "%s", "base_url": "https://api.minimax.io/v1", "api_key": "fake-key"}`, providerName)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d: %s", rec.Code, rec.Body.String())
	}

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

	if response.Refreshed < 1 {
		t.Errorf("Expected refreshed >= 1, got %d", response.Refreshed)
	}

	var minimaxRefreshed bool
	for _, result := range response.Results {
		if result.ProviderType == "minimax" && result.Refreshed && result.Error == "" {
			minimaxRefreshed = true
			break
		}
	}
	if !minimaxRefreshed {
		t.Error("Expected minimax result marked refreshed with no error")
	}
}

// =============================================================================
// GetProviderUsage Tests (Unit tests with mock transport)
// =============================================================================

func TestGetProviderUsage_ZAICodingQuotaError(t *testing.T) {
	// DB-backed handler so the read-through cold-fill has a real quota store and
	// the provider row satisfies the snapshot FK.
	_, r := newTestHandlerWithRouter(t)
	orig := newDiscoveryService
	defer func() { newDiscoveryService = orig }()

	newDiscoveryService = func() *provider.DiscoveryService {
		ds := provider.NewDiscoveryServiceWithHTTPClient(&http.Client{
			Transport: &mockTransport{
				roundTripFunc: func(req *http.Request) (*http.Response, error) {
					// ZAI Coding uses hardcoded URL
					if strings.Contains(req.URL.Host, "api.z.ai") {
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

	_, idStr := createQuotaProvider(t, r, "https://api.z.ai/v1")
	w := doQuotaGet(t, r, "/providers/"+idStr+"/usage")

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "failed to fetch usage") {
		t.Errorf("expected error about fetch usage, got %q", w.Body.String())
	}
}

func TestGetProviderUsage_NanoGPTSuccess(t *testing.T) {
	// DB-backed handler: read-through cold-fill needs a real quota store + FK row.
	_, r := newTestHandlerWithRouter(t)
	// Override newDiscoveryService with mock transport returning valid NanoGPT JSON
	orig := newDiscoveryService
	defer func() { newDiscoveryService = orig }()

	newDiscoveryService = func() *provider.DiscoveryService {
		ds := provider.NewDiscoveryServiceWithHTTPClient(&http.Client{
			Transport: &mockTransport{
				roundTripFunc: func(req *http.Request) (*http.Response, error) {
					if strings.HasSuffix(req.URL.Path, "/usage") {
						resp := `{"active":true,"provider":"nanogpt","providerStatus":"active","providerStatusRaw":"active","limits":{},"dailyInputTokens":{"used":100,"limit":1000},"weeklyInputTokens":{"used":500,"limit":5000},"state":"active"}`
						return &http.Response{
							StatusCode: http.StatusOK,
							Body:       io.NopCloser(strings.NewReader(resp)),
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

	_, idStr := createQuotaProvider(t, r, "https://api.nano-gpt.com/v1")
	w := doQuotaGet(t, r, "/providers/"+idStr+"/usage")

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["provider"] != "nanogpt" {
		t.Errorf("expected provider='nanogpt', got %q", resp["provider"])
	}
}

func TestGetProviderUsage_OpenRouterSuccess(t *testing.T) {
	// DB-backed handler: read-through cold-fill needs a real quota store + FK row.
	_, r := newTestHandlerWithRouter(t)
	// Override newDiscoveryService with mock transport returning valid OpenRouter JSON
	orig := newDiscoveryService
	defer func() { newDiscoveryService = orig }()

	newDiscoveryService = func() *provider.DiscoveryService {
		ds := provider.NewDiscoveryServiceWithHTTPClient(&http.Client{
			Transport: &mockTransport{
				roundTripFunc: func(req *http.Request) (*http.Response, error) {
					if strings.HasSuffix(req.URL.Path, "/credits") {
						resp := `{"data":{"total_credits":10.0,"total_usage":2.5}}`
						return &http.Response{
							StatusCode: http.StatusOK,
							Body:       io.NopCloser(strings.NewReader(resp)),
							Header:     make(http.Header),
						}, nil
					}
					if strings.HasSuffix(req.URL.Path, "/key") {
						resp := `{"data":{"label":"test-key","limit":null,"limit_reset":"","limit_remaining":null,"usage":1.5,"usage_daily":0.1,"usage_weekly":0.5,"usage_monthly":1.0,"is_free_tier":false}}`
						return &http.Response{
							StatusCode: http.StatusOK,
							Body:       io.NopCloser(strings.NewReader(resp)),
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

	_, idStr := createQuotaProvider(t, r, "https://openrouter.ai/api/v1")
	w := doQuotaGet(t, r, "/providers/"+idStr+"/usage")

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	// OpenRouter returns flattened key balance response
	if resp["label"] != "test-key" {
		t.Errorf("expected label='test-key', got %q", resp["label"])
	}
}

// =============================================================================
// GetProviderBalance Tests
// =============================================================================

func TestGetProviderBalance_DeepSeekSuccess(t *testing.T) {
	// DB-backed handler: read-through cold-fill needs a real quota store + FK row.
	_, r := newTestHandlerWithRouter(t)
	// Override newDiscoveryService with mock transport returning valid DeepSeek JSON
	orig := newDiscoveryService
	defer func() { newDiscoveryService = orig }()

	newDiscoveryService = func() *provider.DiscoveryService {
		ds := provider.NewDiscoveryServiceWithHTTPClient(&http.Client{
			Transport: &mockTransport{
				roundTripFunc: func(req *http.Request) (*http.Response, error) {
					if strings.HasSuffix(req.URL.Path, "/user/balance") {
						resp := `{"is_available":true,"balance_infos":[{"currency":"USD","total_balance":"10.00","granted_balance":"5.00","topped_up_balance":"5.00"}]}`
						return &http.Response{
							StatusCode: http.StatusOK,
							Body:       io.NopCloser(strings.NewReader(resp)),
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

	_, idStr := createQuotaProvider(t, r, "https://api.deepseek.com/v1")
	w := doQuotaGet(t, r, "/providers/"+idStr+"/balance")

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["is_available"] != true {
		t.Errorf("expected is_available=true, got %v", resp["is_available"])
	}
}

// =============================================================================
// GetOllamaCloudAccount Tests
// =============================================================================

func TestGetOllamaCloudAccount_Success(t *testing.T) {
	// DB-backed handler: read-through cold-fill needs a real quota store + FK row.
	_, r := newTestHandlerWithRouter(t)
	// Override newDiscoveryService with mock transport returning valid Ollama Cloud JSON
	orig := newDiscoveryService
	defer func() { newDiscoveryService = orig }()

	newDiscoveryService = func() *provider.DiscoveryService {
		ds := provider.NewDiscoveryServiceWithHTTPClient(&http.Client{
			Transport: &mockTransport{
				roundTripFunc: func(req *http.Request) (*http.Response, error) {
					if strings.HasSuffix(req.URL.Path, "/api/me") {
						resp := `{"id":"acct-123","email":"test@example.com","name":"Test User","plan":"free","customer_id":{"string":"","valid":false},"subscription_id":{"string":"","valid":false},"subscription_period_start":{"time":"0001-01-01T00:00:00Z","valid":false},"subscription_period_end":{"time":"0001-01-01T00:00:00Z","valid":false},"suspended_at":{"time":"0001-01-01T00:00:00Z","valid":false}}`
						return &http.Response{
							StatusCode: http.StatusOK,
							Body:       io.NopCloser(strings.NewReader(resp)),
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

	_, idStr := createQuotaProvider(t, r, "https://api.ollama.com/v1")
	w := doQuotaGet(t, r, "/providers/"+idStr+"/account")

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["id"] != "acct-123" {
		t.Errorf("expected id='acct-123', got %q", resp["id"])
	}
}

// =============================================================================
// RefreshAllQuotas Tests
// =============================================================================

func TestRefreshAllQuotas_ListError(t *testing.T) {
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
	r.Post("/providers/refresh-quotas", h.RefreshAllQuotas)

	req := httptest.NewRequest(http.MethodPost, "/providers/refresh-quotas", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "failed to list providers") {
		t.Errorf("expected error about list providers, got %q", w.Body.String())
	}
}

func TestRefreshAllQuotas_NanoGPTSuccess(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Override newDiscoveryService with mock transport returning valid NanoGPT JSON
	orig := newDiscoveryService
	defer func() { newDiscoveryService = orig }()

	newDiscoveryService = func() *provider.DiscoveryService {
		ds := provider.NewDiscoveryServiceWithHTTPClient(&http.Client{
			Transport: &mockTransport{
				roundTripFunc: func(req *http.Request) (*http.Response, error) {
					// NanoGPT usage endpoint
					resp := `{"active":true,"provider":"nanogpt","providerStatus":"active","providerStatusRaw":"active","limits":{},"dailyInputTokens":{"used":100,"limit":1000},"weeklyInputTokens":{"used":500,"limit":5000},"state":"active"}`
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       io.NopCloser(strings.NewReader(resp)),
						Header:     make(http.Header),
					}, nil
				},
			},
		})
		ds.SetRetryBaseDelay(time.Millisecond)
		return ds
	}

	// Provider is created in the real test DB so the quota snapshot FK is satisfied.
	createQuotaProvider(t, r, "https://api.nano-gpt.com/v1")

	req := httptest.NewRequest(http.MethodPost, "/providers/refresh-quotas", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["refreshed"].(float64) < 1 {
		t.Errorf("expected at least 1 refreshed, got %v", resp["refreshed"])
	}
}

func TestRefreshAllQuotas_ZAICodingError(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Override newDiscoveryService with mock transport returning error for z.ai
	orig := newDiscoveryService
	defer func() { newDiscoveryService = orig }()

	newDiscoveryService = func() *provider.DiscoveryService {
		ds := provider.NewDiscoveryServiceWithHTTPClient(&http.Client{
			Transport: &mockTransport{
				roundTripFunc: func(req *http.Request) (*http.Response, error) {
					if strings.Contains(req.URL.Host, "api.z.ai") {
						return &http.Response{
							StatusCode: http.StatusInternalServerError,
							Body:       io.NopCloser(strings.NewReader(`{"error":"internal"}`)),
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

	// Provider is created in the real test DB so the quota snapshot FK is satisfied.
	createQuotaProvider(t, r, "https://api.z.ai/v1")

	req := httptest.NewRequest(http.MethodPost, "/providers/refresh-quotas", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["failed"].(float64) < 1 {
		t.Errorf("expected at least 1 failed, got %v", resp["failed"])
	}
}

func TestRefreshAllQuotas_ZAICodingSuccess(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Override newDiscoveryService with mock transport returning valid ZAI JSON
	orig := newDiscoveryService
	defer func() { newDiscoveryService = orig }()

	newDiscoveryService = func() *provider.DiscoveryService {
		ds := provider.NewDiscoveryServiceWithHTTPClient(&http.Client{
			Transport: &mockTransport{
				roundTripFunc: func(req *http.Request) (*http.Response, error) {
					if strings.Contains(req.URL.Host, "api.z.ai") {
						resp := `{"code":0,"msg":"ok","data":{"limits":[],"level":"free"},"success":true}`
						return &http.Response{
							StatusCode: http.StatusOK,
							Body:       io.NopCloser(strings.NewReader(resp)),
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

	// Provider is created in the real test DB so the quota snapshot FK is satisfied.
	createQuotaProvider(t, r, "https://api.z.ai/v1")

	req := httptest.NewRequest(http.MethodPost, "/providers/refresh-quotas", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["refreshed"].(float64) < 1 {
		t.Errorf("expected at least 1 refreshed, got %v", resp["refreshed"])
	}
}

func TestRefreshAllQuotas_OpenRouterSuccess(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Override newDiscoveryService with mock transport returning valid OpenRouter JSON
	orig := newDiscoveryService
	defer func() { newDiscoveryService = orig }()

	newDiscoveryService = func() *provider.DiscoveryService {
		ds := provider.NewDiscoveryServiceWithHTTPClient(&http.Client{
			Transport: &mockTransport{
				roundTripFunc: func(req *http.Request) (*http.Response, error) {
					// OpenRouter credits endpoint
					if strings.Contains(req.URL.Path, "/credits") {
						resp := `{"data":{"total_credits":10.0,"total_usage":2.5}}`
						return &http.Response{
							StatusCode: http.StatusOK,
							Body:       io.NopCloser(strings.NewReader(resp)),
							Header:     make(http.Header),
						}, nil
					}
					// OpenRouter key endpoint
					resp := `{"data":{"label":"test-key","limit":null,"limit_reset":"","limit_remaining":null,"usage":1.5,"usage_daily":0.1,"usage_weekly":0.5,"usage_monthly":1.0,"is_free_tier":false}}`
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       io.NopCloser(strings.NewReader(resp)),
						Header:     make(http.Header),
					}, nil
				},
			},
		})
		ds.SetRetryBaseDelay(time.Millisecond)
		return ds
	}

	// Provider is created in the real test DB so the quota snapshot FK is satisfied.
	createQuotaProvider(t, r, "https://openrouter.ai/api/v1")

	req := httptest.NewRequest(http.MethodPost, "/providers/refresh-quotas", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["refreshed"].(float64) < 1 {
		t.Errorf("expected at least 1 refreshed, got %v", resp["refreshed"])
	}
}

func TestRefreshAllQuotas_DeepSeekSuccess(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Override newDiscoveryService with mock transport returning valid DeepSeek JSON
	orig := newDiscoveryService
	defer func() { newDiscoveryService = orig }()

	newDiscoveryService = func() *provider.DiscoveryService {
		ds := provider.NewDiscoveryServiceWithHTTPClient(&http.Client{
			Transport: &mockTransport{
				roundTripFunc: func(req *http.Request) (*http.Response, error) {
					if strings.HasSuffix(req.URL.Path, "/user/balance") {
						resp := `{"is_available":true,"balance_infos":[{"currency":"USD","total_balance":"10.00","granted_balance":"5.00","topped_up_balance":"5.00"}]}`
						return &http.Response{
							StatusCode: http.StatusOK,
							Body:       io.NopCloser(strings.NewReader(resp)),
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

	// Provider is created in the real test DB so the quota snapshot FK is satisfied.
	createQuotaProvider(t, r, "https://api.deepseek.com/v1")

	req := httptest.NewRequest(http.MethodPost, "/providers/refresh-quotas", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["refreshed"].(float64) < 1 {
		t.Errorf("expected at least 1 refreshed, got %v", resp["refreshed"])
	}
}

func TestRefreshAllQuotas_OllamaCloudSuccess(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Override newDiscoveryService with mock transport returning valid Ollama Cloud JSON
	orig := newDiscoveryService
	defer func() { newDiscoveryService = orig }()

	newDiscoveryService = func() *provider.DiscoveryService {
		ds := provider.NewDiscoveryServiceWithHTTPClient(&http.Client{
			Transport: &mockTransport{
				roundTripFunc: func(req *http.Request) (*http.Response, error) {
					if strings.HasSuffix(req.URL.Path, "/api/me") {
						resp := `{"id":"acct-123","email":"test@example.com","name":"Test User","plan":"free","customer_id":{"string":"","valid":false},"subscription_id":{"string":"","valid":false},"subscription_period_start":{"time":"0001-01-01T00:00:00Z","valid":false},"subscription_period_end":{"time":"0001-01-01T00:00:00Z","valid":false},"suspended_at":{"time":"0001-01-01T00:00:00Z","valid":false}}`
						return &http.Response{
							StatusCode: http.StatusOK,
							Body:       io.NopCloser(strings.NewReader(resp)),
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

	// Provider is created in the real test DB so the quota snapshot FK is satisfied.
	createQuotaProvider(t, r, "https://api.ollama.com/v1")

	req := httptest.NewRequest(http.MethodPost, "/providers/refresh-quotas", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["refreshed"].(float64) < 1 {
		t.Errorf("expected at least 1 refreshed, got %v", resp["refreshed"])
	}
}

// =============================================================================
// GetProviderUsage - NeuralWatt Tests
// =============================================================================

func TestGetProviderUsage_NeuralWattSuccess(t *testing.T) {
	// DB-backed handler: read-through cold-fill needs a real quota store + FK row.
	_, r := newTestHandlerWithRouter(t)
	orig := newDiscoveryService
	defer func() { newDiscoveryService = orig }()

	newDiscoveryService = func() *provider.DiscoveryService {
		ds := provider.NewDiscoveryServiceWithHTTPClient(&http.Client{
			Transport: &mockTransport{
				roundTripFunc: func(req *http.Request) (*http.Response, error) {
					if strings.HasSuffix(req.URL.Path, "/quota") {
						resp := `{"snapshot_at":"2026-06-02T17:42:29Z","balance":{"credits_remaining_usd":23.9,"total_credits_usd":23.9,"credits_used_usd":0,"accounting_method":"energy"},"usage":{"lifetime":{"cost_usd":1.0,"requests":100,"tokens":1000,"energy_kwh":0.5},"current_month":{"cost_usd":0.5,"requests":50,"tokens":500,"energy_kwh":0.25}},"limits":{"overage_limit_usd":null,"rate_limit_tier":"standard"},"subscription":{"plan":"standard","status":"active","billing_interval":"month","current_period_start":"2026-05-28T00:00:00Z","current_period_end":"2026-06-28T00:00:00Z","auto_renew":true,"kwh_included":16,"kwh_used":2,"kwh_remaining":14},"key":{"label":"test-key","usage_usd":0.5,"is_free_tier":false}}`
						return &http.Response{
							StatusCode: http.StatusOK,
							Body:       io.NopCloser(strings.NewReader(resp)),
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

	_, idStr := createQuotaProvider(t, r, "https://api.neuralwatt.com")
	w := doQuotaGet(t, r, "/providers/"+idStr+"/usage")

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["snapshot_at"] != "2026-06-02T17:42:29Z" {
		t.Errorf("expected snapshot_at field, got %v", resp["snapshot_at"])
	}
}

func TestGetProviderUsage_NeuralWattFreeTier(t *testing.T) {
	// DB-backed handler: read-through cold-fill needs a real quota store + FK row.
	_, r := newTestHandlerWithRouter(t)
	orig := newDiscoveryService
	defer func() { newDiscoveryService = orig }()

	newDiscoveryService = func() *provider.DiscoveryService {
		ds := provider.NewDiscoveryServiceWithHTTPClient(&http.Client{
			Transport: &mockTransport{
				roundTripFunc: func(req *http.Request) (*http.Response, error) {
					// NeuralWatt returns 404 for free tier keys (no quota endpoint)
					return &http.Response{
						StatusCode: http.StatusNotFound,
						Body:       io.NopCloser(strings.NewReader(`{"error":"not found"}`)),
						Header:     make(http.Header),
					}, nil
				},
			},
		})
		ds.SetRetryBaseDelay(time.Millisecond)
		return ds
	}

	_, idStr := createQuotaProvider(t, r, "https://api.neuralwatt.com")
	w := doQuotaGet(t, r, "/providers/"+idStr+"/usage")

	// Free tier returns 204 No Content (nil quota, nil error)
	if w.Code != http.StatusNoContent {
		t.Errorf("expected status 204 for free tier NeuralWatt, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGetProviderUsage_NeuralWattError(t *testing.T) {
	// DB-backed handler: read-through cold-fill needs a real quota store + FK row.
	_, r := newTestHandlerWithRouter(t)
	orig := newDiscoveryService
	defer func() { newDiscoveryService = orig }()

	newDiscoveryService = func() *provider.DiscoveryService {
		ds := provider.NewDiscoveryServiceWithHTTPClient(&http.Client{
			Transport: &mockTransport{
				roundTripFunc: func(req *http.Request) (*http.Response, error) {
					return &http.Response{
						StatusCode: http.StatusInternalServerError,
						Body:       io.NopCloser(strings.NewReader(`{"error":"internal"}`)),
						Header:     make(http.Header),
					}, nil
				},
			},
		})
		ds.SetRetryBaseDelay(time.Millisecond)
		return ds
	}

	_, idStr := createQuotaProvider(t, r, "https://api.neuralwatt.com")
	w := doQuotaGet(t, r, "/providers/"+idStr+"/usage")

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500 for NeuralWatt error, got %d: %s", w.Code, w.Body.String())
	}
	// Read-through reports the snapshot kind ("usage") as the failed resource.
	if !strings.Contains(w.Body.String(), "failed to fetch usage") {
		t.Errorf("expected error about fetch usage, got %q", w.Body.String())
	}
}

// TestRefreshAllQuotas_MixedResults tests that RefreshAllQuotas continues
// processing all providers even when one fails, returning partial results.
func TestRefreshAllQuotas_MixedResults(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	orig := newDiscoveryService
	defer func() { newDiscoveryService = orig }()

	newDiscoveryService = func() *provider.DiscoveryService {
		ds := provider.NewDiscoveryServiceWithHTTPClient(&http.Client{
			Transport: &mockTransport{
				roundTripFunc: func(req *http.Request) (*http.Response, error) {
					// NanoGPT succeeds
					if strings.Contains(req.URL.Host, "api.nano-gpt.com") || strings.Contains(req.URL.Host, "nano-gpt.com") {
						resp := `{"active":true,"provider":"nanogpt","providerStatus":"active","providerStatusRaw":"active","limits":{},"dailyInputTokens":{"used":100,"limit":1000},"state":"active"}`
						return &http.Response{
							StatusCode: http.StatusOK,
							Body:       io.NopCloser(strings.NewReader(resp)),
							Header:     make(http.Header),
						}, nil
					}
					// DeepSeek fails
					if strings.Contains(req.URL.Host, "api.deepseek.com") {
						return &http.Response{
							StatusCode: http.StatusInternalServerError,
							Body:       io.NopCloser(strings.NewReader(`{"error":"internal"}`)),
							Header:     make(http.Header),
						}, nil
					}
					return &http.Response{
						StatusCode: http.StatusNotFound,
						Body:       io.NopCloser(strings.NewReader(`not found`)),
						Header:     make(http.Header),
					}, nil
				},
			},
		})
		ds.SetRetryBaseDelay(time.Millisecond)
		return ds
	}

	// Providers are created in the real test DB so the quota snapshot FKs are satisfied.
	createQuotaProvider(t, r, "https://api.nano-gpt.com/v1")
	createQuotaProvider(t, r, "https://api.deepseek.com/v1")

	req := httptest.NewRequest(http.MethodPost, "/providers/refresh-quotas", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	// One succeeded, one failed
	if resp["refreshed"].(float64) != 1 {
		t.Errorf("expected 1 refreshed, got %v", resp["refreshed"])
	}
	if resp["failed"].(float64) != 1 {
		t.Errorf("expected 1 failed, got %v", resp["failed"])
	}

	results := resp["results"].([]any)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
}

func TestRefreshAllQuotas_NeuralWattSuccess(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	orig := newDiscoveryService
	defer func() { newDiscoveryService = orig }()

	newDiscoveryService = func() *provider.DiscoveryService {
		ds := provider.NewDiscoveryServiceWithHTTPClient(&http.Client{
			Transport: &mockTransport{
				roundTripFunc: func(req *http.Request) (*http.Response, error) {
					resp := `{"active":true,"total_credits":100.0,"total_usage":10.0,"credits_remaining":90.0}`
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       io.NopCloser(strings.NewReader(resp)),
						Header:     make(http.Header),
					}, nil
				},
			},
		})
		ds.SetRetryBaseDelay(time.Millisecond)
		return ds
	}

	// Provider is created in the real test DB so the quota snapshot FK is satisfied.
	createQuotaProvider(t, r, "https://api.neuralwatt.com/v1")

	req := httptest.NewRequest(http.MethodPost, "/providers/refresh-quotas", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["refreshed"].(float64) < 1 {
		t.Errorf("expected at least 1 refreshed, got %v", resp["refreshed"])
	}
}

func TestRefreshAllQuotas_NeuralWattError(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	orig := newDiscoveryService
	defer func() { newDiscoveryService = orig }()

	newDiscoveryService = func() *provider.DiscoveryService {
		ds := provider.NewDiscoveryServiceWithHTTPClient(&http.Client{
			Transport: &mockTransport{
				roundTripFunc: func(req *http.Request) (*http.Response, error) {
					return &http.Response{
						StatusCode: http.StatusInternalServerError,
						Body:       io.NopCloser(strings.NewReader(`{"error":"internal"}`)),
						Header:     make(http.Header),
					}, nil
				},
			},
		})
		ds.SetRetryBaseDelay(time.Millisecond)
		return ds
	}

	// Provider is created in the real test DB so the quota snapshot FK is satisfied.
	createQuotaProvider(t, r, "https://api.neuralwatt.com/v1")

	req := httptest.NewRequest(http.MethodPost, "/providers/refresh-quotas", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["failed"].(float64) < 1 {
		t.Errorf("expected at least 1 failed, got %v", resp["failed"])
	}
}

// createQuotaProvider creates a provider (in the real test DB, so the quota
// snapshot FK is satisfied) via the router and returns its parsed UUID plus the
// raw string form for path building. The key is encrypted under the handler's
// MasterKey, so the read-through cold-fill can decrypt it.
func createQuotaProvider(t *testing.T, r chi.Router, baseURL string) (uuid.UUID, string) {
	t.Helper()
	providerName := fmt.Sprintf("test-quota-%s", uuid.New().String()[:8])
	body := fmt.Sprintf(`{"name": "%s", "base_url": "%s", "api_key": "fake-key"}`, providerName, baseURL)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("failed to create provider: %d: %s", rec.Code, rec.Body.String())
	}
	var createResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &createResp); err != nil {
		t.Fatalf("failed to parse create response: %v", err)
	}
	id, err := uuid.Parse(createResp.ID)
	if err != nil {
		t.Fatalf("invalid provider id %q: %v", createResp.ID, err)
	}
	return id, createResp.ID
}

// doQuotaGet issues an authenticated GET to a quota endpoint path.
func doQuotaGet(t *testing.T, r chi.Router, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, path, http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)
	return rec
}

// TestGetProviderUsage_ServesStoredSnapshot proves the read-through serves a
// stored snapshot verbatim without any upstream call: the discovery service
// fails the test if it is ever invoked.
func TestGetProviderUsage_ServesStoredSnapshot(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)
	provID, idStr := createQuotaProvider(t, r, "https://nano-gpt.com")

	if err := h.quotaRepo.Upsert(context.Background(), quota.Snapshot{
		ProviderID: provID, Kind: "usage",
		Payload: json.RawMessage(`{"used":42}`), HTTPStatus: 200, Source: "poll",
	}); err != nil {
		t.Fatalf("seed snapshot: %v", err)
	}

	// Any upstream call means the read-through incorrectly fell through to a
	// live fetch — fail loudly. This replaces the invented panicOnCallDiscovery.
	orig := newDiscoveryService
	defer func() { newDiscoveryService = orig }()
	newDiscoveryService = func() *provider.DiscoveryService {
		return provider.NewDiscoveryServiceWithHTTPClient(&http.Client{
			Transport: &mockTransport{roundTripFunc: func(req *http.Request) (*http.Response, error) {
				t.Fatalf("unexpected upstream call to %s", req.URL.String())
				return nil, fmt.Errorf("unexpected upstream call")
			}},
		})
	}

	rr := doQuotaGet(t, r, "/providers/"+idStr+"/usage")
	// Compare semantically: Postgres JSONB canonicalizes the payload (e.g.
	// {"used":42} round-trips as {"used": 42}), so never byte-compare it.
	var got map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("want stored snapshot served, got %d %s (decode: %v)", rr.Code, rr.Body.String(), err)
	}
	if rr.Code != http.StatusOK || got["used"] != float64(42) {
		t.Fatalf("want stored snapshot served, got %d %s", rr.Code, rr.Body.String())
	}
	if rr.Header().Get("X-Quota-Fetched-At") == "" {
		t.Fatal("want X-Quota-Fetched-At header")
	}
}

// TestGetProviderUsage_Reproduces424 confirms a stored 424 snapshot is served
// as 424 (the store-seed path deferred from the Task 3 review).
func TestGetProviderUsage_Reproduces424(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)
	provID, idStr := createQuotaProvider(t, r, "https://nano-gpt.com")

	if err := h.quotaRepo.Upsert(context.Background(), quota.Snapshot{
		ProviderID: provID, Kind: "usage", HTTPStatus: 424, Source: "poll",
	}); err != nil {
		t.Fatalf("seed snapshot: %v", err)
	}

	rr := doQuotaGet(t, r, "/providers/"+idStr+"/usage")
	if rr.Code != http.StatusFailedDependency {
		t.Fatalf("want 424, got %d: %s", rr.Code, rr.Body.String())
	}
}

// TestGetProviderUsage_ColdLazyFill verifies that a first view with no stored
// snapshot performs one live fetch, persists it, and serves the fetched body.
func TestGetProviderUsage_ColdLazyFill(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)
	provID, idStr := createQuotaProvider(t, r, "https://nano-gpt.com")

	orig := newDiscoveryService
	defer func() { newDiscoveryService = orig }()
	newDiscoveryService = func() *provider.DiscoveryService {
		ds := provider.NewDiscoveryServiceWithHTTPClient(&http.Client{
			Transport: &mockTransport{roundTripFunc: func(req *http.Request) (*http.Response, error) {
				if strings.Contains(req.URL.Host, "nano-gpt.com") {
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       io.NopCloser(strings.NewReader(`{"active":true,"provider":"coldfill-marker"}`)),
						Header:     make(http.Header),
					}, nil
				}
				return nil, fmt.Errorf("unexpected request to %s", req.URL.String())
			}},
		})
		ds.SetRetryBaseDelay(time.Millisecond)
		return ds
	}

	rr := doQuotaGet(t, r, "/providers/"+idStr+"/usage")
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "coldfill-marker") {
		t.Fatalf("cold fill should fetch+serve, got %d %s", rr.Code, rr.Body.String())
	}

	snap, err := h.quotaRepo.Get(context.Background(), provID, "usage")
	if err != nil {
		t.Fatalf("get persisted snapshot: %v", err)
	}
	if snap == nil {
		t.Fatal("cold fill should persist a snapshot")
	}
}

// TestRefreshAllQuotas_PersistsSnapshot verifies the manual refresh endpoint
// writes a source="manual" snapshot per supported provider via the shared
// fetchQuotaSnapshot helper, rather than fetching and discarding the result.
func TestRefreshAllQuotas_PersistsSnapshot(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)
	provID, _ := createQuotaProvider(t, r, "https://nano-gpt.com")

	orig := newDiscoveryService
	defer func() { newDiscoveryService = orig }()
	newDiscoveryService = func() *provider.DiscoveryService {
		ds := provider.NewDiscoveryServiceWithHTTPClient(&http.Client{
			Transport: &mockTransport{roundTripFunc: func(req *http.Request) (*http.Response, error) {
				if strings.Contains(req.URL.Host, "nano-gpt.com") {
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       io.NopCloser(strings.NewReader(`{"active":true,"provider":"manual-refresh-marker"}`)),
						Header:     make(http.Header),
					}, nil
				}
				// Other providers present in the shared test DB simply fail;
				// this test only asserts our own provider's snapshot.
				return nil, fmt.Errorf("unexpected request to %s", req.URL.String())
			}},
		})
		ds.SetRetryBaseDelay(time.Millisecond)
		return ds
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/providers/refresh-quotas", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}

	snap, err := h.quotaRepo.Get(context.Background(), provID, "usage")
	if err != nil {
		t.Fatalf("get persisted snapshot: %v", err)
	}
	if snap == nil {
		t.Fatal("manual refresh should persist a snapshot")
	}
	if snap.Source != "manual" {
		t.Fatalf("want Source=manual, got %q", snap.Source)
	}
	// Decode semantically: Postgres JSONB canonicalizes the stored payload, so
	// never byte-compare it.
	var decoded provider.NanoGPTUsageResponse
	if err := json.Unmarshal(snap.Payload, &decoded); err != nil {
		t.Fatalf("decode persisted payload: %v (%s)", err, string(snap.Payload))
	}
	if decoded.Provider != "manual-refresh-marker" {
		t.Fatalf("want persisted payload from live fetch, got provider=%q", decoded.Provider)
	}
}
