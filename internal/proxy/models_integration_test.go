package proxy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/auth"
	"github.com/hugalafutro/model-hotel/internal/config"
	"github.com/hugalafutro/model-hotel/internal/failover"
	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/provider"
	"github.com/hugalafutro/model-hotel/internal/ratelimit"
	"github.com/hugalafutro/model-hotel/internal/settings"
	"github.com/hugalafutro/model-hotel/internal/virtualkey"
)

// Test ListModels with multiple providers and models
func TestListModels_MultipleProviders(t *testing.T) {

	pool := testDB.Pool()
	// Clean up any existing test data
	if _, err := pool.Exec(context.Background(), "DELETE FROM models WHERE provider_name LIKE 'test-provider-%'"); err != nil {
		t.Logf("Failed to clean up test models: %v", err)
	}
	if _, err := pool.Exec(context.Background(), "DELETE FROM providers WHERE name LIKE 'test-provider-%'"); err != nil {
		t.Logf("Failed to clean up test providers: %v", err)
	}

	settingsRepo := settings.NewRepository(pool)
	failoverRepo := failover.NewRepository(pool)
	modelRepo := model.NewRepository(pool)
	providerRepo := provider.NewRepository(pool)
	virtualKeyRepo := virtualkey.NewRepository(pool)
	limiter := ratelimit.NewLimiter(settingsRepo)
	ipLimiter := ratelimit.NewIPLimiter(30, 60, nil, nil)

	handler := &Handler{
		cfg:            &config.Config{MasterKey: "test-master-key"},
		settingsRepo:   settingsRepo,
		failoverRepo:   failoverRepo,
		modelRepo:      modelRepo,
		providerRepo:   providerRepo,
		virtualKeyRepo: WrapVirtualKeyRepo(virtualKeyRepo),
		rateLimiter:    limiter,
		ipLimiter:      ipLimiter,
		dbPool:         pool,
		circuitBreaker: failover.NewCircuitBreaker(settingsRepo),
		upstreamTransport: &http.Transport{
			DialContext:           NewSafeDialer(append(config.KnownProviderHosts(), "127.0.0.1")).DialContext,
			ResponseHeaderTimeout: 120 * time.Second,
			IdleConnTimeout:       90 * time.Second,
		},
	}

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

	var response map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Verify response structure
	if response["object"] != "list" {
		t.Errorf("expected object=list, got %v", response["object"])
	}

	data, ok := response["data"].([]interface{})
	if !ok {
		t.Fatal("expected data to be an array")
	}

	if len(data) != 2 {
		t.Errorf("expected 2 models, got %d", len(data))
	}

	// Verify model IDs are in the expected format
	modelIDs := make([]string, 0, len(data))
	for _, item := range data {
		m := item.(map[string]interface{})
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
	if _, err := pool.Exec(context.Background(), "DELETE FROM models WHERE provider_name LIKE 'test-provider-%'"); err != nil {
		t.Logf("Failed to clean up test models: %v", err)
	}
	if _, err := pool.Exec(context.Background(), "DELETE FROM providers WHERE name LIKE 'test-provider-%'"); err != nil {
		t.Logf("Failed to clean up test providers: %v", err)
	}

	settingsRepo := settings.NewRepository(pool)
	failoverRepo := failover.NewRepository(pool)
	modelRepo := model.NewRepository(pool)
	providerRepo := provider.NewRepository(pool)
	virtualKeyRepo := virtualkey.NewRepository(pool)
	limiter := ratelimit.NewLimiter(settingsRepo)
	ipLimiter := ratelimit.NewIPLimiter(30, 60, nil, nil)

	handler := &Handler{
		cfg:            &config.Config{MasterKey: "test-master-key"},
		settingsRepo:   settingsRepo,
		failoverRepo:   failoverRepo,
		modelRepo:      modelRepo,
		providerRepo:   providerRepo,
		virtualKeyRepo: WrapVirtualKeyRepo(virtualKeyRepo),
		rateLimiter:    limiter,
		ipLimiter:      ipLimiter,
		dbPool:         pool,
		circuitBreaker: failover.NewCircuitBreaker(settingsRepo),
		upstreamTransport: &http.Transport{
			DialContext:           NewSafeDialer(append(config.KnownProviderHosts(), "127.0.0.1")).DialContext,
			ResponseHeaderTimeout: 120 * time.Second,
			IdleConnTimeout:       90 * time.Second,
		},
	}

	req := httptest.NewRequest("GET", "/v1/models", http.NoBody)
	req = withAuthContext(req)

	rr := httptest.NewRecorder()
	handler.ListModels(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}

	var response map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if response["object"] != "list" {
		t.Errorf("expected object=list, got %v", response["object"])
	}

	data, ok := response["data"].([]interface{})
	if !ok {
		t.Fatal("expected data to be an array")
	}

	if len(data) != 0 {
		t.Errorf("expected 0 models, got %d", len(data))
	}
}

// Test ListModels with disabled models (should be filtered)
func TestListModels_DisabledModelsFiltered(t *testing.T) {

	pool := testDB.Pool()
	// Clean up any existing test data
	if _, err := pool.Exec(context.Background(), "DELETE FROM models WHERE provider_name LIKE 'test-provider-%'"); err != nil {
		t.Logf("Failed to clean up test models: %v", err)
	}
	if _, err := pool.Exec(context.Background(), "DELETE FROM providers WHERE name LIKE 'test-provider-%'"); err != nil {
		t.Logf("Failed to clean up test providers: %v", err)
	}

	settingsRepo := settings.NewRepository(pool)
	failoverRepo := failover.NewRepository(pool)
	modelRepo := model.NewRepository(pool)
	providerRepo := provider.NewRepository(pool)
	virtualKeyRepo := virtualkey.NewRepository(pool)
	limiter := ratelimit.NewLimiter(settingsRepo)
	ipLimiter := ratelimit.NewIPLimiter(30, 60, nil, nil)

	handler := &Handler{
		cfg:            &config.Config{MasterKey: "test-master-key"},
		settingsRepo:   settingsRepo,
		failoverRepo:   failoverRepo,
		modelRepo:      modelRepo,
		providerRepo:   providerRepo,
		virtualKeyRepo: WrapVirtualKeyRepo(virtualKeyRepo),
		rateLimiter:    limiter,
		ipLimiter:      ipLimiter,
		dbPool:         pool,
		circuitBreaker: failover.NewCircuitBreaker(settingsRepo),
		upstreamTransport: &http.Transport{
			DialContext:           NewSafeDialer(append(config.KnownProviderHosts(), "127.0.0.1")).DialContext,
			ResponseHeaderTimeout: 120 * time.Second,
			IdleConnTimeout:       90 * time.Second,
		},
	}

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

	var response map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	data, ok := response["data"].([]interface{})
	if !ok {
		t.Fatal("expected data to be an array")
	}

	// Should only return the enabled model
	if len(data) != 1 {
		t.Errorf("expected 1 enabled model, got %d", len(data))
	}

	// Verify it's the enabled model
	m := data[0].(map[string]interface{})
	if m["id"] != provider.NormalizeName(providerName)+"/enabled-model" {
		t.Errorf("expected enabled-model, got %v", m["id"])
	}
}

// Test ListModels with failover groups
func TestListModels_WithFailoverGroups(t *testing.T) {

	pool := testDB.Pool()
	// Clean up any existing test data
	if _, err := pool.Exec(context.Background(), "DELETE FROM models WHERE provider_name LIKE 'test-provider-%'"); err != nil {
		t.Logf("Failed to clean up test models: %v", err)
	}
	if _, err := pool.Exec(context.Background(), "DELETE FROM providers WHERE name LIKE 'test-provider-%'"); err != nil {
		t.Logf("Failed to clean up test providers: %v", err)
	}
	if _, err := pool.Exec(context.Background(), "DELETE FROM model_failover_groups WHERE display_model LIKE 'my-failover-model'"); err != nil {
		t.Logf("Failed to clean up test failover groups: %v", err)
	}

	settingsRepo := settings.NewRepository(pool)
	failoverRepo := failover.NewRepository(pool)
	modelRepo := model.NewRepository(pool)
	providerRepo := provider.NewRepository(pool)
	virtualKeyRepo := virtualkey.NewRepository(pool)
	limiter := ratelimit.NewLimiter(settingsRepo)
	ipLimiter := ratelimit.NewIPLimiter(30, 60, nil, nil)

	handler := &Handler{
		cfg:            &config.Config{MasterKey: "test-master-key"},
		settingsRepo:   settingsRepo,
		failoverRepo:   failoverRepo,
		modelRepo:      modelRepo,
		providerRepo:   providerRepo,
		virtualKeyRepo: WrapVirtualKeyRepo(virtualKeyRepo),
		rateLimiter:    limiter,
		ipLimiter:      ipLimiter,
		dbPool:         pool,
		circuitBreaker: failover.NewCircuitBreaker(settingsRepo),
		upstreamTransport: &http.Transport{
			DialContext:           NewSafeDialer(append(config.KnownProviderHosts(), "127.0.0.1")).DialContext,
			ResponseHeaderTimeout: 120 * time.Second,
			IdleConnTimeout:       90 * time.Second,
		},
	}

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

	// Create a model
	modelID := uuid.New()
	testModel := &model.Model{
		ID:               modelID,
		ProviderID:       createdProvider.ID,
		ModelID:          "test-model",
		Name:             "Test Model",
		Description:      "Test model for failover",
		Capabilities:     "{}",
		Params:           "{}",
		Modality:         "chat",
		InputModalities:  "[\"text\"]",
		OutputModalities: "[\"text\"]",
		Enabled:          true,
		ProviderName:     providerName,
		ProviderEnabled:  true,
	}

	if err := modelRepo.Upsert(context.Background(), testModel); err != nil {
		t.Fatalf("failed to create model: %v", err)
	}

	// Create a failover group
	if _, err := failoverRepo.Upsert(context.Background(), "my-failover-model", []uuid.UUID{modelID}); err != nil {
		t.Fatalf("failed to create failover group: %v", err)
	}

	// Test the ListModels endpoint
	req := httptest.NewRequest("GET", "/v1/models", http.NoBody)
	req = withAuthContext(req)

	rr := httptest.NewRecorder()
	handler.ListModels(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}

	var response map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	data, ok := response["data"].([]interface{})
	if !ok {
		t.Fatal("expected data to be an array")
	}

	// Should return both the regular model and the failover model
	if len(data) != 2 {
		t.Errorf("expected 2 models (1 regular + 1 failover), got %d", len(data))
	}

	// Verify the failover model is present
	foundFailover := false
	foundRegular := false
	for _, item := range data {
		m := item.(map[string]interface{})
		modelID := m["id"].(string)
		if modelID == "hotel/my-failover-model" {
			foundFailover = true
		}
		if modelID == provider.NormalizeName(providerName)+"/test-model" {
			foundRegular = true
		}
	}

	if !foundFailover || !foundRegular {
		t.Errorf("expected to find both failover and regular models")
	}
}
