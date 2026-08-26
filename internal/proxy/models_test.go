package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hugalafutro/model-hotel/internal/auth"
	"github.com/hugalafutro/model-hotel/internal/failover"
	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/provider"
	"github.com/hugalafutro/model-hotel/internal/ratelimit"
	"github.com/hugalafutro/model-hotel/internal/settings"
	"github.com/hugalafutro/model-hotel/internal/virtualkey"
)

// ---------------------------------------------------------------------------
// ListModels integration tests (requires PostgreSQL)
// ---------------------------------------------------------------------------

func TestListModels_EmptyDB(t *testing.T) {
	h := newIntegrationHandler()

	// ListModels returns all enabled models; with no specific test data
	// we just verify the endpoint works and returns valid JSON.
	req := httptest.NewRequest("GET", "/models", http.NoBody)
	rr := httptest.NewRecorder()
	h.ListModels(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}

	contentType := rr.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("expected Content-Type application/json, got %s", contentType)
	}

	var resp map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	data, ok := resp["data"]
	if !ok {
		t.Error("response should contain 'data' key")
	}
	// data can be an empty array when no models are enabled
	_ = data
}

func TestListModels_WithProviderAndModel(t *testing.T) {
	h := newIntegrationHandler()

	// Create a provider with an encrypted key
	masterKey := h.cfg.MasterKey
	kp, err := auth.Encrypt("sk-test-api-key-for-models-test", masterKey)
	if err != nil {
		t.Fatalf("failed to encrypt key: %v", err)
	}

	prov, err := h.providerRepo.Create(context.Background(), provider.CreateProviderRequest{
		Name:    "test-list-models-provider",
		BaseURL: "https://api.example.com",
		APIKey:  "sk-test-api-key-for-models-test",
	}, kp.Ciphertext, kp.Nonce, kp.Salt)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	defer func() {
		_ = h.providerRepo.Delete(context.Background(), prov.ID)
	}()

	// Create a model under this provider
	modelID := uuid.New()
	ctx := context.Background()
	m := &model.Model{
		ID:               modelID,
		ProviderID:       prov.ID,
		ModelID:          "gpt-test-model",
		Name:             "GPT Test Model",
		DisplayName:      "GPT Test Display",
		Description:      "A test model for ListModels",
		Capabilities:     "{}",
		Params:           "{}",
		Modality:         "text",
		InputModalities:  "[]",
		OutputModalities: "[]",
		Enabled:          true,
		CreatedAt:        time.Now(),
		LastSeenAt:       time.Now(),
	}
	if err := h.modelRepo.Upsert(ctx, m); err != nil {
		t.Fatalf("failed to upsert model: %v", err)
	}

	// Now call ListModels
	req := httptest.NewRequest("GET", "/models", http.NoBody)
	rr := httptest.NewRecorder()
	h.ListModels(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}

	var resp map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	data, ok := resp["data"].([]any)
	if !ok {
		t.Fatal("response 'data' should be an array")
	}

	// Find our model in the response
	found := false
	for _, item := range data {
		itemMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if id, _ := itemMap["id"].(string); id == "test-list-models-provider/gpt-test-model" {
			found = true
			if itemMap["object"] != "model" {
				t.Error("model object should be 'model'")
			}
			if itemMap["provider"] != "test-list-models-provider" {
				t.Errorf("provider = %v, want 'test-list-models-provider'", itemMap["provider"])
			}
			break
		}
	}
	if !found {
		t.Error("expected to find 'test-list-models-provider/gpt-test-model' in response")
	}
}

// TestListModels_RepoError tests the error path when modelRepo.ListEnabled fails.
// This covers the error handling at lines 13-18 in models.go.
func TestListModels_RepoError(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	// Replace modelRepo with a mock that returns an error
	h.modelRepo = &mockModelRepo{listEnabledErr: fmt.Errorf("db connection failed")}

	req := httptest.NewRequest("GET", "/models", http.NoBody)
	rr := httptest.NewRecorder()
	h.ListModels(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rr.Code)
	}

	// Verify response body contains error message
	body := rr.Body.String()
	if !strings.Contains(body, "failed to list models") {
		t.Errorf("expected response to contain 'failed to list models', got: %s", body)
	}
}

// TestListModels_JSONEncodeError tests the error path when JSON encoding fails.
// Covers line 183 in models.go (debuglog.Error for encode failure).
func TestListModels_JSONEncodeError(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	// Initialize failoverRepo with a pool that will fail gracefully
	ctx := context.Background()
	poolCfg, err := pgxpool.ParseConfig("postgres://invalid:invalid@localhost:59999/testdb?sslmode=disable&connect_timeout=1")
	if err != nil {
		t.Fatalf("failed to parse pool config: %v", err)
	}
	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}
	h.failoverRepo = failover.NewRepository(pool)
	defer pool.Close()

	h.modelRepo = &mockModelRepo{listEnabledResult: []*model.Model{}}

	failingWriter := &failingResponseWriter{
		failAfter: 0,
		failErr:   fmt.Errorf("write failed"),
	}

	req := httptest.NewRequest("GET", "/models", http.NoBody)
	h.ListModels(failingWriter, req)

	// Verify the code reached the encoding stage: Content-Type header must be set
	// and WriteHeader called before the encode attempt.
	if ct := failingWriter.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected Content-Type application/json, got %q", ct)
	}
	if failingWriter.code != 0 {
		t.Errorf("expected no explicit WriteHeader call (code=0), got code=%d", failingWriter.code)
	}
}

// ---------------------------------------------------------------------------
// Tests moved from coverage_test.go
// ---------------------------------------------------------------------------

// TestListModels_DBError tests that when modelRepo.ListEnabled returns error,
// ListModels returns 500 with JSON error.
func TestListModels_DBError(t *testing.T) {
	t.Helper()
	dbErr := errors.New("database query failed")
	mockRepo := &coverageMockModelRepo{
		listEnabledFunc: func(ctx context.Context) ([]*model.Model, error) {
			return nil, dbErr
		},
	}
	h := &Handler{
		modelRepo: mockRepo,
	}

	req := httptest.NewRequest("GET", "/models", http.NoBody)
	rr := httptest.NewRecorder()
	h.ListModels(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rr.Code)
	}

	// Verify response is JSON with expected message
	var resp map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Errorf("response should be valid JSON: %v", err)
	}
	if msg, ok := resp["error"].(map[string]any); !ok {
		t.Error("response should have error object")
	} else if msg["message"] != "failed to list models" {
		t.Errorf("expected error message 'failed to list models', got %v", msg["message"])
	}
}

// ---------------------------------------------------------------------------
// Tests moved from coverage_gap2_test.go
// ---------------------------------------------------------------------------

// TestListModels_MockListEnabledError verifies that when modelRepo.ListEnabled returns
// an error, ListModels returns HTTP 500 Internal Server Error.
func TestListModels_MockListEnabledError(t *testing.T) {
	t.Helper()

	dbErr := errors.New("database connection failed")
	mockModelRepo := &listModelsMockRepo{
		listEnabledFunc: func(ctx context.Context) ([]*model.Model, error) {
			return nil, dbErr
		},
	}

	h := newUnitHandler()
	defer stopUnitHandler(h)
	h.modelRepo = mockModelRepo

	req := httptest.NewRequest("GET", "/models", http.NoBody)
	rr := httptest.NewRecorder()
	h.ListModels(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rr.Code)
	}

	// Verify response is JSON
	var resp map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Errorf("response should be valid JSON: %v", err)
	}
}

// TestListModels_WithCanceledContext verifies that using a canceled
// context triggers a DB error path.
func TestListModels_WithCanceledContext(t *testing.T) {
	t.Helper()

	h := newIntegrationHandler()
	defer stopUnitHandler(h)

	// Create a request with a canceled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	req := httptest.NewRequest("GET", "/models", http.NoBody).WithContext(ctx)
	rr := httptest.NewRecorder()
	h.ListModels(rr, req)

	// Should return 500 due to DB error from canceled context
	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 from canceled context, got %d", rr.Code)
	}
}

// TestListModels_ValidProviderIDQuery documents that provider_id query
// parameter is accepted but not used in proxy package (it's used in api package).
func TestListModels_ValidProviderIDQuery(t *testing.T) {
	t.Helper()

	h := newIntegrationHandler()
	defer stopUnitHandler(h)

	// Valid UUID but proxy ListModels doesn't use it
	validUUID := uuid.New().String()
	req := httptest.NewRequest("GET", "/models?provider_id="+validUUID, http.NoBody)
	rr := httptest.NewRecorder()
	h.ListModels(rr, req)

	// Returns 200 since provider_id is ignored in proxy package
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 (provider_id ignored), got %d", rr.Code)
	}

	// Verify response is valid JSON
	var resp map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Errorf("response should be valid JSON: %v", err)
	}
}

// TestListModels_InvalidProviderIDQuery documents that invalid provider_id
// query parameter is accepted but not validated in proxy package.
func TestListModels_InvalidProviderIDQuery(t *testing.T) {
	t.Helper()

	h := newIntegrationHandler()
	defer stopUnitHandler(h)

	// Invalid UUID format - proxy ListModels ignores it
	req := httptest.NewRequest("GET", "/models?provider_id=not-a-uuid", http.NoBody)
	rr := httptest.NewRecorder()
	h.ListModels(rr, req)

	// Returns 200 since provider_id is ignored in proxy package
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 (invalid provider_id ignored), got %d", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// Tests moved from models_integration_test.go
// ---------------------------------------------------------------------------

// Test ListModels with multiple providers and models
func TestListModels_MultipleProviders(t *testing.T) {

	pool := testDB.Pool()
	// Clean up any existing test data
	if _, err := pool.Exec(context.Background(), "DELETE FROM providers WHERE name LIKE 'test-provider-%'"); err != nil {
		t.Logf("Failed to clean up test providers: %v", err)
	}
	model.InvalidateModelCache()

	settingsRepo := settings.NewRepository(pool)
	failoverRepo := failover.NewRepository(pool)
	modelRepo := model.NewRepository(pool)
	providerRepo := provider.NewRepository(pool)
	virtualKeyRepo := virtualkey.NewRepository(pool)
	limiter := ratelimit.NewLimiter(settingsRepo)
	ipLimiter := ratelimit.NewIPLimiter(30, 60, nil, nil)

	handler := newCanonicalHandler(t, "test-master-key", pool, settingsRepo, failoverRepo, modelRepo, providerRepo, virtualKeyRepo, limiter, ipLimiter)

	// Create two providers
	keyPair1, err := auth.Encrypt("test-api-key-1", "test-master-key")
	if err != nil {
		t.Fatalf("failed to encrypt API key: %v", err)
	}

	providerName1 := "test-provider-1-" + uuid.New().String()[:8]
	createdProvider1, err := providerRepo.Create(context.Background(), provider.CreateProviderRequest{
		Name:    providerName1,
		BaseURL: "https://api.provider1.com",
		APIKey:  "test-api-key-1",
	}, keyPair1.Ciphertext, keyPair1.Nonce, keyPair1.Salt)
	if err != nil {
		t.Fatalf("failed to create provider 1: %v", err)
	}

	keyPair2, err := auth.Encrypt("test-api-key-2", "test-master-key")
	if err != nil {
		t.Fatalf("failed to encrypt API key: %v", err)
	}

	providerName2 := "test-provider-2-" + uuid.New().String()[:8]
	createdProvider2, err := providerRepo.Create(context.Background(), provider.CreateProviderRequest{
		Name:    providerName2,
		BaseURL: "https://api.provider2.com",
		APIKey:  "test-api-key-2",
	}, keyPair2.Ciphertext, keyPair2.Nonce, keyPair2.Salt)
	if err != nil {
		t.Fatalf("failed to create provider 2: %v", err)
	}

	// Create models for both providers
	modelID1 := uuid.New()
	testModel1 := &model.Model{
		ID:               modelID1,
		ProviderID:       createdProvider1.ID,
		ModelID:          "model-1",
		Name:             "Model 1",
		Description:      "Test model 1",
		Capabilities:     "{}",
		Params:           "{}",
		Modality:         "chat",
		InputModalities:  "[\"text\"]",
		OutputModalities: "[\"text\"]",
		Enabled:          true,
		ProviderName:     providerName1,
		ProviderEnabled:  true,
	}

	if err := modelRepo.Upsert(context.Background(), testModel1); err != nil {
		t.Fatalf("failed to create model 1: %v", err)
	}

	modelID2 := uuid.New()
	testModel2 := &model.Model{
		ID:               modelID2,
		ProviderID:       createdProvider2.ID,
		ModelID:          "model-2",
		Name:             "Model 2",
		Description:      "Test model 2",
		Capabilities:     "{}",
		Params:           "{}",
		Modality:         "chat",
		InputModalities:  "[\"text\"]",
		OutputModalities: "[\"text\"]",
		Enabled:          true,
		ProviderName:     providerName2,
		ProviderEnabled:  true,
	}

	if err := modelRepo.Upsert(context.Background(), testModel2); err != nil {
		t.Fatalf("failed to create model 2: %v", err)
	}

	// Test the ListModels endpoint
	req := httptest.NewRequest("GET", "/v1/models", http.NoBody)
	req = withAuthContext(req)

	rr := httptest.NewRecorder()
	handler.ListModels(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}

	var response map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Verify response structure
	if response["object"] != "list" {
		t.Errorf("expected object=list, got %v", response["object"])
	}

	data, ok := response["data"].([]any)
	if !ok {
		t.Fatal("expected data to be an array")
	}

	// Check that both specific models are present (exact count is fragile in parallel test suite)
	if len(data) < 2 {
		t.Errorf("expected at least 2 models, got %d", len(data))
	}

	// Verify model IDs are in the expected format
	modelIDs := make([]string, 0, len(data))
	for _, item := range data {
		m := item.(map[string]any)
		modelIDs = append(modelIDs, m["id"].(string))
	}

	// Check that both models are present
	foundModel1 := false
	foundModel2 := false
	for _, id := range modelIDs {
		if id == provider.NormalizeName(providerName1)+"/model-1" {
			foundModel1 = true
		}
		if id == provider.NormalizeName(providerName2)+"/model-2" {
			foundModel2 = true
		}
	}

	if !foundModel1 || !foundModel2 {
		t.Errorf("expected to find both models in response")
	}
}

// Test ListModels with no models
func TestListModels_NoModels(t *testing.T) {

	pool := testDB.Pool()
	// Clean up any existing test data
	if _, err := pool.Exec(context.Background(), "DELETE FROM providers WHERE name LIKE 'test-provider-%'"); err != nil {
		t.Logf("Failed to clean up test providers: %v", err)
	}
	model.InvalidateModelCache()

	settingsRepo := settings.NewRepository(pool)
	failoverRepo := failover.NewRepository(pool)
	modelRepo := model.NewRepository(pool)
	providerRepo := provider.NewRepository(pool)
	virtualKeyRepo := virtualkey.NewRepository(pool)
	limiter := ratelimit.NewLimiter(settingsRepo)
	ipLimiter := ratelimit.NewIPLimiter(30, 60, nil, nil)

	handler := newCanonicalHandler(t, "test-master-key", pool, settingsRepo, failoverRepo, modelRepo, providerRepo, virtualKeyRepo, limiter, ipLimiter)

	req := httptest.NewRequest("GET", "/v1/models", http.NoBody)
	req = withAuthContext(req)

	rr := httptest.NewRecorder()
	handler.ListModels(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}

	var response map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if response["object"] != "list" {
		t.Errorf("expected object=list, got %v", response["object"])
	}

	data, ok := response["data"].([]any)
	if !ok {
		t.Fatal("expected data to be an array")
	}

	// Verify no test provider models are present (exact count is fragile in parallel test suite)
	for _, item := range data {
		m := item.(map[string]any)
		modelID := m["id"].(string)
		if containsTestProviderPrefix(modelID) {
			t.Errorf("unexpected test provider model in response: %s", modelID)
		}
	}
}

// Test ListModels with disabled models (should be filtered)
func TestListModels_DisabledModelsFiltered(t *testing.T) {

	pool := testDB.Pool()
	// Clean up any existing test data
	if _, err := pool.Exec(context.Background(), "DELETE FROM providers WHERE name LIKE 'test-provider-%'"); err != nil {
		t.Logf("Failed to clean up test providers: %v", err)
	}
	model.InvalidateModelCache()

	settingsRepo := settings.NewRepository(pool)
	failoverRepo := failover.NewRepository(pool)
	modelRepo := model.NewRepository(pool)
	providerRepo := provider.NewRepository(pool)
	virtualKeyRepo := virtualkey.NewRepository(pool)
	limiter := ratelimit.NewLimiter(settingsRepo)
	ipLimiter := ratelimit.NewIPLimiter(30, 60, nil, nil)

	handler := newCanonicalHandler(t, "test-master-key", pool, settingsRepo, failoverRepo, modelRepo, providerRepo, virtualKeyRepo, limiter, ipLimiter)

	// Create a provider
	keyPair, err := auth.Encrypt("test-api-key", "test-master-key")
	if err != nil {
		t.Fatalf("failed to encrypt API key: %v", err)
	}

	providerName := "test-provider-" + uuid.New().String()[:8]
	createdProvider, err := providerRepo.Create(context.Background(), provider.CreateProviderRequest{
		Name:    providerName,
		BaseURL: "https://api.provider.com",
		APIKey:  "test-api-key",
	}, keyPair.Ciphertext, keyPair.Nonce, keyPair.Salt)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	// Create an enabled model
	modelID1 := uuid.New()
	enabledModel := &model.Model{
		ID:               modelID1,
		ProviderID:       createdProvider.ID,
		ModelID:          "enabled-model",
		Name:             "Enabled Model",
		Description:      "Enabled test model",
		Capabilities:     "{}",
		Params:           "{}",
		Modality:         "chat",
		InputModalities:  "[\"text\"]",
		OutputModalities: "[\"text\"]",
		Enabled:          true,
		ProviderName:     providerName,
		ProviderEnabled:  true,
	}

	if err := modelRepo.Upsert(context.Background(), enabledModel); err != nil {
		t.Fatalf("failed to create enabled model: %v", err)
	}

	// Create a disabled model
	modelID2 := uuid.New()
	disabledModel := &model.Model{
		ID:               modelID2,
		ProviderID:       createdProvider.ID,
		ModelID:          "disabled-model",
		Name:             "Disabled Model",
		Description:      "Disabled test model",
		Capabilities:     "{}",
		Params:           "{}",
		Modality:         "chat",
		InputModalities:  "[\"text\"]",
		OutputModalities: "[\"text\"]",
		Enabled:          false,
		ProviderName:     providerName,
		ProviderEnabled:  true,
	}

	if err := modelRepo.Upsert(context.Background(), disabledModel); err != nil {
		t.Fatalf("failed to create disabled model: %v", err)
	}

	// Test the ListModels endpoint
	req := httptest.NewRequest("GET", "/v1/models", http.NoBody)
	req = withAuthContext(req)

	rr := httptest.NewRecorder()
	handler.ListModels(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}

	var response map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	data, ok := response["data"].([]any)
	if !ok {
		t.Fatal("expected data to be an array")
	}

	// Should contain the enabled model (exact count is fragile in parallel test suite)
	if len(data) < 1 {
		t.Errorf("expected at least 1 enabled model, got %d", len(data))
	}

	// Verify the enabled model is present and disabled model is NOT present
	foundEnabled := false
	foundDisabled := false
	for _, item := range data {
		m := item.(map[string]any)
		modelID := m["id"].(string)
		if modelID == provider.NormalizeName(providerName)+"/enabled-model" {
			foundEnabled = true
		}
		if modelID == provider.NormalizeName(providerName)+"/disabled-model" {
			foundDisabled = true
		}
	}
	if !foundEnabled {
		t.Error("expected enabled-model to be present")
	}
	if foundDisabled {
		t.Error("expected disabled-model to NOT be present")
	}

	// Deliberately no assertion about data[0]. The listing is not scoped to this
	// test's provider and the suite shares one Postgres, so which row lands first
	// depends on what else has been inserted by the time this runs — the reason
	// the count assertion above was already relaxed. The loop is the whole test:
	// what filtering has to guarantee is that the enabled model is present and
	// the disabled one is not, at any position.
}

func TestListModels_FilterByProvider(t *testing.T) {

	pool := testDB.Pool()
	// Clean up any existing test data
	if _, err := pool.Exec(context.Background(), "DELETE FROM providers WHERE name LIKE 'test-provider-%'"); err != nil {
		t.Logf("Failed to clean up test providers: %v", err)
	}
	model.InvalidateModelCache()

	settingsRepo := settings.NewRepository(pool)
	failoverRepo := failover.NewRepository(pool)
	modelRepo := model.NewRepository(pool)
	providerRepo := provider.NewRepository(pool)
	virtualKeyRepo := virtualkey.NewRepository(pool)
	limiter := ratelimit.NewLimiter(settingsRepo)
	ipLimiter := ratelimit.NewIPLimiter(30, 60, nil, nil)

	handler := newCanonicalHandler(t, "test-master-key", pool, settingsRepo, failoverRepo, modelRepo, providerRepo, virtualKeyRepo, limiter, ipLimiter)

	// Create a provider
	keyPair, err := auth.Encrypt("test-api-key", "test-master-key")
	if err != nil {
		t.Fatalf("failed to encrypt API key: %v", err)
	}

	providerName := "test-provider-" + uuid.New().String()[:8]
	createdProvider, err := providerRepo.Create(context.Background(), provider.CreateProviderRequest{
		Name:    providerName,
		BaseURL: "https://api.provider.com",
		APIKey:  "test-api-key",
	}, keyPair.Ciphertext, keyPair.Nonce, keyPair.Salt)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	// Create multiple models
	modelsToCreate := []struct {
		modelID string
		enabled bool
	}{
		{"model-1", true},
		{"model-2", true},
		{"model-3", false},
	}

	for _, tc := range modelsToCreate {
		modelID := uuid.New()
		testModel := &model.Model{
			ID:               modelID,
			ProviderID:       createdProvider.ID,
			ModelID:          tc.modelID,
			Name:             tc.modelID,
			Description:      "Test model " + tc.modelID,
			Capabilities:     "{}",
			Params:           "{}",
			Modality:         "chat",
			InputModalities:  `["text"]`,
			OutputModalities: `["text"]`,
			Enabled:          tc.enabled,
			ProviderName:     providerName,
			ProviderEnabled:  true,
		}

		if err := modelRepo.Upsert(context.Background(), testModel); err != nil {
			t.Fatalf("failed to create model %s: %v", tc.modelID, err)
		}
	}

	// Test the ListModels endpoint
	req := httptest.NewRequest("GET", "/v1/models", http.NoBody)
	req = withAuthContext(req)

	rr := httptest.NewRecorder()
	handler.ListModels(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}

	var response map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	data, ok := response["data"].([]any)
	if !ok {
		t.Fatal("expected data to be an array")
	}

	// Should contain both enabled models (exact count is fragile in parallel test suite)
	if len(data) < 2 {
		t.Errorf("expected at least 2 enabled models, got %d", len(data))
	}

	// Verify model IDs
	foundModels := make(map[string]bool)
	for _, item := range data {
		m := item.(map[string]any)
		modelID := m["id"].(string)
		foundModels[modelID] = true
	}

	if !foundModels[provider.NormalizeName(providerName)+"/model-1"] {
		t.Error("expected to find model-1")
	}
	if !foundModels[provider.NormalizeName(providerName)+"/model-2"] {
		t.Error("expected to find model-2")
	}
	if foundModels[provider.NormalizeName(providerName)+"/model-3"] {
		t.Error("should not find disabled model-3")
	}
}
