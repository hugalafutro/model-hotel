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

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hugalafutro/model-hotel/internal/auth"
	"github.com/hugalafutro/model-hotel/internal/failover"
	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/provider"
)

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

func TestDiscoverProviderModels_UpsertError(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Create a mock OpenAI-compatible server
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": []map[string]interface{}{
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
	if !strings.Contains(w.Body.String(), "failed to upsert model") {
		t.Errorf("expected error about upsert, got %q", w.Body.String())
	}
}

func TestDiscoverProviderModels_DisableMissingError(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Create a mock OpenAI-compatible server
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": []map[string]interface{}{
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

	// Override modelRepoDisableMissing to return error
	origModelRepoDisableMissing := modelRepoDisableMissing
	defer func() { modelRepoDisableMissing = origModelRepoDisableMissing }()

	modelRepoDisableMissing = func(repo *model.Repository, ctx context.Context, providerID uuid.UUID, modelIDs []string) (int64, error) {
		return 0, errors.New("disable missing models error")
	}

	// Call discover endpoint
	req = httptest.NewRequest(http.MethodPost, "/providers/"+created.ID+"/discover", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "failed to disable missing models") {
		t.Errorf("expected error about disable missing models, got %q", w.Body.String())
	}
}

func TestDiscoverProviderModels_SyncForModelError(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Create a mock OpenAI-compatible server
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": []map[string]interface{}{
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

	failoverRepoSyncForModel = func(repo *failover.Repository, ctx context.Context, modelID string) error {
		return errors.New("sync for model error")
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
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": []map[string]interface{}{
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
// GetProviderUsage Tests (Unit tests with mock transport)
// =============================================================================

func TestGetProviderUsage_ZAICodingQuotaError(t *testing.T) {
	// Override newDiscoveryService with mock transport returning 500
	orig := newDiscoveryService
	defer func() { newDiscoveryService = orig }()

	newDiscoveryService = func() *provider.DiscoveryService {
		return provider.NewDiscoveryServiceWithHTTPClient(&http.Client{
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
		return provider.NewDiscoveryServiceWithHTTPClient(&http.Client{
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
		return provider.NewDiscoveryServiceWithHTTPClient(&http.Client{
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
		return provider.NewDiscoveryServiceWithHTTPClient(&http.Client{
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
		return provider.NewDiscoveryServiceWithHTTPClient(&http.Client{
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
	}

	// Create a mock OpenAI-compatible server that returns models matching the cache
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": []map[string]interface{}{
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

	var resp map[string]interface{}
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
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": []map[string]interface{}{
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

func TestDiscoverAllModels_DisableMissingError(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Create a mock OpenAI-compatible server
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": []map[string]interface{}{
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

	// Override modelRepoDisableMissing to return error
	origModelRepoDisableMissing := modelRepoDisableMissing
	defer func() { modelRepoDisableMissing = origModelRepoDisableMissing }()

	modelRepoDisableMissing = func(repo *model.Repository, ctx context.Context, providerID uuid.UUID, modelIDs []string) (int64, error) {
		return 0, errors.New("disable missing models error")
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

func TestDiscoverAllModels_SyncForModelError(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Create a mock OpenAI-compatible server
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": []map[string]interface{}{
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

	failoverRepoSyncForModel = func(repo *failover.Repository, ctx context.Context, modelID string) error {
		return errors.New("sync for model error")
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
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": []map[string]interface{}{
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
		return provider.NewDiscoveryServiceWithHTTPClient(&http.Client{
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
		return provider.NewDiscoveryServiceWithHTTPClient(&http.Client{
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
		return provider.NewDiscoveryServiceWithHTTPClient(&http.Client{
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
		return provider.NewDiscoveryServiceWithHTTPClient(&http.Client{
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
		return provider.NewDiscoveryServiceWithHTTPClient(&http.Client{
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
		return provider.NewDiscoveryServiceWithHTTPClient(&http.Client{
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
