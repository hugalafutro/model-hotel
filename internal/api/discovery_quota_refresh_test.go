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
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hugalafutro/model-hotel/internal/provider"
	"github.com/hugalafutro/model-hotel/internal/quota"
)

// RefreshAllQuotas: what one pass over the fleet reports per provider type,
// and how a failing provider or a failing upsert is surfaced.

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

// TestRefreshAllQuotas_UpsertErrorReportsFailure verifies that a persist failure
// is reported as failed rather than a false "refreshed", so callers do not treat
// stale stored quota as freshly updated.
func TestRefreshAllQuotas_UpsertErrorReportsFailure(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	// Mock a successful DeepSeek balance fetch so the flow reaches the Upsert.
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

	createQuotaProvider(t, r, "https://api.deepseek.com/v1")

	// Point the quota repo at a closed pool so the Upsert fails, while the
	// provider list (its own healthy repo) still returns the provider.
	brokenPool, err := pgxpool.New(context.Background(), apiTestDBURL)
	if err != nil {
		t.Fatalf("failed to open pool: %v", err)
	}
	brokenPool.Close()
	h.quotaRepo = quota.NewRepository(brokenPool)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/providers/refresh-quotas", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response struct {
		Results []struct {
			Refreshed bool   `json:"refreshed"`
			Error     string `json:"error"`
		} `json:"results"`
		Refreshed int `json:"refreshed"`
		Failed    int `json:"failed"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if response.Refreshed != 0 {
		t.Errorf("expected refreshed=0 when persist fails, got %d", response.Refreshed)
	}
	if response.Failed != 1 {
		t.Errorf("expected failed=1 when persist fails, got %d", response.Failed)
	}
	if len(response.Results) != 1 || response.Results[0].Refreshed || response.Results[0].Error == "" {
		t.Errorf("expected the provider reported as failed with an error, got %+v", response.Results)
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
