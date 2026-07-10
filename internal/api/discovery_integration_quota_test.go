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
	if !strings.Contains(rec.Body.String(), "failed to fetch key balance") {
		t.Errorf("Expected error about failed to fetch key balance, got: %s", rec.Body.String())
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
	if !strings.Contains(rec.Body.String(), "failed to fetch ollama cloud account") {
		t.Errorf("Expected error about failed to fetch ollama cloud account, got: %s", rec.Body.String())
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

// =============================================================================
// GetProviderUsage Tests (Unit tests with mock transport)
// =============================================================================

func TestGetProviderUsage_ZAICodingQuotaError(t *testing.T) {
	// Override newDiscoveryService with mock transport returning 500
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

	// Create handler with mock provider store
	prov := createTestProvider(t, "zai-test", "https://api.z.ai/v1", testMasterKeyForDiscovery)
	mockProv := &mockProviderStore{
		getFn: func(ctx context.Context, id uuid.UUID) (*provider.Provider, error) {
			if id == prov.ID {
				return prov, nil
			}
			return nil, errors.New("provider not found")
		},
	}
	mockAuth := &mockAdminAuth{validateFn: func(token string) bool { return true }}
	h := testHandler(mockProv, nil, nil, mockAuth, nil)
	h.cfg.MasterKey = testMasterKeyForDiscovery

	// Set up chi router
	r := chi.NewRouter()
	r.Get("/providers/{id}/usage", h.GetProviderUsage)

	req := httptest.NewRequest(http.MethodGet, "/providers/"+prov.ID.String()+"/usage", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "failed to fetch usage") {
		t.Errorf("expected error about fetch usage, got %q", w.Body.String())
	}
}

func TestGetProviderUsage_NanoGPTSuccess(t *testing.T) {
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

	// Create handler with mock provider store - use nano-gpt.com (with hyphen) for detection
	prov := createTestProvider(t, "nanogpt-test", "https://api.nano-gpt.com/v1", testMasterKeyForDiscovery)
	mockProv := &mockProviderStore{
		getFn: func(ctx context.Context, id uuid.UUID) (*provider.Provider, error) {
			if id == prov.ID {
				return prov, nil
			}
			return nil, errors.New("provider not found")
		},
	}
	mockAuth := &mockAdminAuth{validateFn: func(token string) bool { return true }}
	h := testHandler(mockProv, nil, nil, mockAuth, nil)
	h.cfg.MasterKey = testMasterKeyForDiscovery

	// Set up chi router
	r := chi.NewRouter()
	r.Get("/providers/{id}/usage", h.GetProviderUsage)

	req := httptest.NewRequest(http.MethodGet, "/providers/"+prov.ID.String()+"/usage", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["provider"] != "nanogpt" {
		t.Errorf("expected provider='nanogpt', got %q", resp["provider"])
	}
}

func TestGetProviderUsage_OpenRouterSuccess(t *testing.T) {
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

	// Create handler with mock provider store
	prov := createTestProvider(t, "openrouter-test", "https://openrouter.ai/api/v1", testMasterKeyForDiscovery)
	mockProv := &mockProviderStore{
		getFn: func(ctx context.Context, id uuid.UUID) (*provider.Provider, error) {
			if id == prov.ID {
				return prov, nil
			}
			return nil, errors.New("provider not found")
		},
	}
	mockAuth := &mockAdminAuth{validateFn: func(token string) bool { return true }}
	h := testHandler(mockProv, nil, nil, mockAuth, nil)
	h.cfg.MasterKey = testMasterKeyForDiscovery

	// Set up chi router
	r := chi.NewRouter()
	r.Get("/providers/{id}/usage", h.GetProviderUsage)

	req := httptest.NewRequest(http.MethodGet, "/providers/"+prov.ID.String()+"/usage", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
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

	// Create handler with mock provider store
	prov := createTestProvider(t, "deepseek-test", "https://api.deepseek.com/v1", testMasterKeyForDiscovery)
	mockProv := &mockProviderStore{
		getFn: func(ctx context.Context, id uuid.UUID) (*provider.Provider, error) {
			if id == prov.ID {
				return prov, nil
			}
			return nil, errors.New("provider not found")
		},
	}
	mockAuth := &mockAdminAuth{validateFn: func(token string) bool { return true }}
	h := testHandler(mockProv, nil, nil, mockAuth, nil)
	h.cfg.MasterKey = testMasterKeyForDiscovery

	// Set up chi router
	r := chi.NewRouter()
	r.Get("/providers/{id}/balance", h.GetProviderBalance)

	req := httptest.NewRequest(http.MethodGet, "/providers/"+prov.ID.String()+"/balance", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
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

	// Create handler with mock provider store - use ollama.com hostname for detection
	prov := createTestProvider(t, "ollama-cloud-test", "https://api.ollama.com/v1", testMasterKeyForDiscovery)
	mockProv := &mockProviderStore{
		getFn: func(ctx context.Context, id uuid.UUID) (*provider.Provider, error) {
			if id == prov.ID {
				return prov, nil
			}
			return nil, errors.New("provider not found")
		},
	}
	mockAuth := &mockAdminAuth{validateFn: func(token string) bool { return true }}
	h := testHandler(mockProv, nil, nil, mockAuth, nil)
	h.cfg.MasterKey = testMasterKeyForDiscovery

	// Set up chi router
	r := chi.NewRouter()
	r.Get("/providers/{id}/account", h.GetOllamaCloudAccount)

	req := httptest.NewRequest(http.MethodGet, "/providers/"+prov.ID.String()+"/account", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
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

	// Create handler with mock provider store - use nano-gpt.com (with hyphen) for detection
	prov := createTestProvider(t, "refresh-nanogpt", "https://api.nano-gpt.com/v1", testMasterKeyForDiscovery)
	mockProv := &mockProviderStore{
		listFn: func(ctx context.Context) ([]*provider.Provider, error) {
			return []*provider.Provider{prov}, nil
		},
	}
	mockAuth := &mockAdminAuth{validateFn: func(token string) bool { return true }}
	h := testHandler(mockProv, nil, nil, mockAuth, nil)
	h.cfg.MasterKey = testMasterKeyForDiscovery

	// Set up chi router
	r := chi.NewRouter()
	r.Post("/providers/refresh-quotas", h.RefreshAllQuotas)

	req := httptest.NewRequest(http.MethodPost, "/providers/refresh-quotas", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["refreshed"].(float64) < 1 {
		t.Errorf("expected at least 1 refreshed, got %v", resp["refreshed"])
	}
}

func TestRefreshAllQuotas_ZAICodingError(t *testing.T) {
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

	// Create handler with mock provider store
	prov := createTestProvider(t, "refresh-zai-err", "https://api.z.ai/v1", testMasterKeyForDiscovery)
	mockProv := &mockProviderStore{
		listFn: func(ctx context.Context) ([]*provider.Provider, error) {
			return []*provider.Provider{prov}, nil
		},
	}
	mockAuth := &mockAdminAuth{validateFn: func(token string) bool { return true }}
	h := testHandler(mockProv, nil, nil, mockAuth, nil)
	h.cfg.MasterKey = testMasterKeyForDiscovery

	// Set up chi router
	r := chi.NewRouter()
	r.Post("/providers/refresh-quotas", h.RefreshAllQuotas)

	req := httptest.NewRequest(http.MethodPost, "/providers/refresh-quotas", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["failed"].(float64) < 1 {
		t.Errorf("expected at least 1 failed, got %v", resp["failed"])
	}
}

func TestRefreshAllQuotas_ZAICodingSuccess(t *testing.T) {
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

	// Create handler with mock provider store
	prov := createTestProvider(t, "refresh-zai", "https://api.z.ai/v1", testMasterKeyForDiscovery)
	mockProv := &mockProviderStore{
		listFn: func(ctx context.Context) ([]*provider.Provider, error) {
			return []*provider.Provider{prov}, nil
		},
	}
	mockAuth := &mockAdminAuth{validateFn: func(token string) bool { return true }}
	h := testHandler(mockProv, nil, nil, mockAuth, nil)
	h.cfg.MasterKey = testMasterKeyForDiscovery

	// Set up chi router
	r := chi.NewRouter()
	r.Post("/providers/refresh-quotas", h.RefreshAllQuotas)

	req := httptest.NewRequest(http.MethodPost, "/providers/refresh-quotas", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["refreshed"].(float64) < 1 {
		t.Errorf("expected at least 1 refreshed, got %v", resp["refreshed"])
	}
}

func TestRefreshAllQuotas_OpenRouterSuccess(t *testing.T) {
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

	// Create handler with mock provider store
	prov := createTestProvider(t, "refresh-openrouter", "https://openrouter.ai/api/v1", testMasterKeyForDiscovery)
	_ = prov // provider type detection uses hostname
	mockProv := &mockProviderStore{
		listFn: func(ctx context.Context) ([]*provider.Provider, error) {
			return []*provider.Provider{prov}, nil
		},
	}
	mockAuth := &mockAdminAuth{validateFn: func(token string) bool { return true }}
	h := testHandler(mockProv, nil, nil, mockAuth, nil)
	h.cfg.MasterKey = testMasterKeyForDiscovery

	// Set up chi router
	r := chi.NewRouter()
	r.Post("/providers/refresh-quotas", h.RefreshAllQuotas)

	req := httptest.NewRequest(http.MethodPost, "/providers/refresh-quotas", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["refreshed"].(float64) < 1 {
		t.Errorf("expected at least 1 refreshed, got %v", resp["refreshed"])
	}
}

func TestRefreshAllQuotas_DeepSeekSuccess(t *testing.T) {
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

	// Create handler with mock provider store
	prov := createTestProvider(t, "refresh-deepseek", "https://api.deepseek.com/v1", testMasterKeyForDiscovery)
	mockProv := &mockProviderStore{
		listFn: func(ctx context.Context) ([]*provider.Provider, error) {
			return []*provider.Provider{prov}, nil
		},
	}
	mockAuth := &mockAdminAuth{validateFn: func(token string) bool { return true }}
	h := testHandler(mockProv, nil, nil, mockAuth, nil)
	h.cfg.MasterKey = testMasterKeyForDiscovery

	// Set up chi router
	r := chi.NewRouter()
	r.Post("/providers/refresh-quotas", h.RefreshAllQuotas)

	req := httptest.NewRequest(http.MethodPost, "/providers/refresh-quotas", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["refreshed"].(float64) < 1 {
		t.Errorf("expected at least 1 refreshed, got %v", resp["refreshed"])
	}
}

func TestRefreshAllQuotas_OllamaCloudSuccess(t *testing.T) {
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

	// Create handler with mock provider store - use ollama.com hostname for detection
	prov := createTestProvider(t, "refresh-ollama-cloud", "https://api.ollama.com/v1", testMasterKeyForDiscovery)
	mockProv := &mockProviderStore{
		listFn: func(ctx context.Context) ([]*provider.Provider, error) {
			return []*provider.Provider{prov}, nil
		},
	}
	mockAuth := &mockAdminAuth{validateFn: func(token string) bool { return true }}
	h := testHandler(mockProv, nil, nil, mockAuth, nil)
	h.cfg.MasterKey = testMasterKeyForDiscovery

	// Set up chi router
	r := chi.NewRouter()
	r.Post("/providers/refresh-quotas", h.RefreshAllQuotas)

	req := httptest.NewRequest(http.MethodPost, "/providers/refresh-quotas", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
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

	prov := createTestProvider(t, "neuralwatt-test", "https://api.neuralwatt.com", testMasterKeyForDiscovery)
	mockProv := &mockProviderStore{
		getFn: func(ctx context.Context, id uuid.UUID) (*provider.Provider, error) {
			if id == prov.ID {
				return prov, nil
			}
			return nil, errors.New("provider not found")
		},
	}
	mockAuth := &mockAdminAuth{validateFn: func(token string) bool { return true }}
	h := testHandler(mockProv, nil, nil, mockAuth, nil)
	h.cfg.MasterKey = testMasterKeyForDiscovery

	r := chi.NewRouter()
	r.Get("/providers/{id}/usage", h.GetProviderUsage)

	req := httptest.NewRequest(http.MethodGet, "/providers/"+prov.ID.String()+"/usage", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["snapshot_at"] != "2026-06-02T17:42:29Z" {
		t.Errorf("expected snapshot_at field, got %v", resp["snapshot_at"])
	}
}

func TestGetProviderUsage_NeuralWattFreeTier(t *testing.T) {
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

	prov := createTestProvider(t, "neuralwatt-freetier", "https://api.neuralwatt.com", testMasterKeyForDiscovery)
	mockProv := &mockProviderStore{
		getFn: func(ctx context.Context, id uuid.UUID) (*provider.Provider, error) {
			if id == prov.ID {
				return prov, nil
			}
			return nil, errors.New("provider not found")
		},
	}
	mockAuth := &mockAdminAuth{validateFn: func(token string) bool { return true }}
	h := testHandler(mockProv, nil, nil, mockAuth, nil)
	h.cfg.MasterKey = testMasterKeyForDiscovery

	r := chi.NewRouter()
	r.Get("/providers/{id}/usage", h.GetProviderUsage)

	req := httptest.NewRequest(http.MethodGet, "/providers/"+prov.ID.String()+"/usage", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Free tier returns 204 No Content (nil quota, nil error)
	if w.Code != http.StatusNoContent {
		t.Errorf("expected status 204 for free tier NeuralWatt, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGetProviderUsage_NeuralWattError(t *testing.T) {
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

	prov := createTestProvider(t, "neuralwatt-err", "https://api.neuralwatt.com", testMasterKeyForDiscovery)
	mockProv := &mockProviderStore{
		getFn: func(ctx context.Context, id uuid.UUID) (*provider.Provider, error) {
			if id == prov.ID {
				return prov, nil
			}
			return nil, errors.New("provider not found")
		},
	}
	mockAuth := &mockAdminAuth{validateFn: func(token string) bool { return true }}
	h := testHandler(mockProv, nil, nil, mockAuth, nil)
	h.cfg.MasterKey = testMasterKeyForDiscovery

	r := chi.NewRouter()
	r.Get("/providers/{id}/usage", h.GetProviderUsage)

	req := httptest.NewRequest(http.MethodGet, "/providers/"+prov.ID.String()+"/usage", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500 for NeuralWatt error, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "failed to fetch quota") {
		t.Errorf("expected error about fetch quota, got %q", w.Body.String())
	}
}

// TestRefreshAllQuotas_MixedResults tests that RefreshAllQuotas continues
// processing all providers even when one fails, returning partial results.
func TestRefreshAllQuotas_MixedResults(t *testing.T) {
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

	nanoProv := createTestProvider(t, "mixed-nanogpt", "https://api.nano-gpt.com/v1", testMasterKeyForDiscovery)
	dsProv := createTestProvider(t, "mixed-deepseek", "https://api.deepseek.com/v1", testMasterKeyForDiscovery)
	mockProv := &mockProviderStore{
		listFn: func(ctx context.Context) ([]*provider.Provider, error) {
			return []*provider.Provider{nanoProv, dsProv}, nil
		},
	}
	mockAuth := &mockAdminAuth{validateFn: func(token string) bool { return true }}
	h := testHandler(mockProv, nil, nil, mockAuth, nil)
	h.cfg.MasterKey = testMasterKeyForDiscovery

	r := chi.NewRouter()
	r.Post("/providers/refresh-quotas", h.RefreshAllQuotas)

	req := httptest.NewRequest(http.MethodPost, "/providers/refresh-quotas", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
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

	results := resp["results"].([]interface{})
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
}

func TestRefreshAllQuotas_NeuralWattSuccess(t *testing.T) {
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

	prov := createTestProvider(t, "refresh-neuralwatt", "https://api.neuralwatt.com/v1", testMasterKeyForDiscovery)
	mockProv := &mockProviderStore{
		listFn: func(ctx context.Context) ([]*provider.Provider, error) {
			return []*provider.Provider{prov}, nil
		},
	}
	mockAuth := &mockAdminAuth{validateFn: func(token string) bool { return true }}
	h := testHandler(mockProv, nil, nil, mockAuth, nil)
	h.cfg.MasterKey = testMasterKeyForDiscovery

	r := chi.NewRouter()
	r.Post("/providers/refresh-quotas", h.RefreshAllQuotas)

	req := httptest.NewRequest(http.MethodPost, "/providers/refresh-quotas", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["refreshed"].(float64) < 1 {
		t.Errorf("expected at least 1 refreshed, got %v", resp["refreshed"])
	}
}

func TestRefreshAllQuotas_NeuralWattError(t *testing.T) {
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

	prov := createTestProvider(t, "refresh-neuralwatt-err", "https://api.neuralwatt.com/v1", testMasterKeyForDiscovery)
	mockProv := &mockProviderStore{
		listFn: func(ctx context.Context) ([]*provider.Provider, error) {
			return []*provider.Provider{prov}, nil
		},
	}
	mockAuth := &mockAdminAuth{validateFn: func(token string) bool { return true }}
	h := testHandler(mockProv, nil, nil, mockAuth, nil)
	h.cfg.MasterKey = testMasterKeyForDiscovery

	r := chi.NewRouter()
	r.Post("/providers/refresh-quotas", h.RefreshAllQuotas)

	req := httptest.NewRequest(http.MethodPost, "/providers/refresh-quotas", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["failed"].(float64) < 1 {
		t.Errorf("expected at least 1 failed, got %v", resp["failed"])
	}
}
