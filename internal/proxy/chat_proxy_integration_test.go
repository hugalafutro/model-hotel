//go:build integration

package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

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

// Test ListModels with model filtering by provider
func TestListModels_FilterByProvider(t *testing.T) {
	if testDB == nil {
		t.Skip("database not available")
	}

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
	ipLimiter := ratelimit.NewIPLimiter(30, 60, nil)

	handler := &Handler{
		cfg:            &config.Config{MasterKey: "test-master-key"},
		settingsRepo:   settingsRepo,
		failoverRepo:   failoverRepo,
		modelRepo:      modelRepo,
		providerRepo:   providerRepo,
		virtualKeyRepo: virtualKeyRepo,
		rateLimiter:    limiter,
		ipLimiter:      ipLimiter,
		dbPool:         pool,
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
	req := httptest.NewRequest("GET", "/v1/models", nil)
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

	// Should only return enabled models
	if len(data) != 2 {
		t.Errorf("expected 2 enabled models, got %d", len(data))
	}

	// Verify model IDs
	foundModels := make(map[string]bool)
	for _, item := range data {
		m := item.(map[string]interface{})
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

// Test ChatCompletions with error handling
func TestChatCompletions_ErrorHandling(t *testing.T) {
	if testDB == nil {
		t.Skip("database not available")
	}

	pool := testDB.Pool()

	settingsRepo := settings.NewRepository(pool)
	failoverRepo := failover.NewRepository(pool)
	modelRepo := model.NewRepository(pool)
	providerRepo := provider.NewRepository(pool)
	virtualKeyRepo := virtualkey.NewRepository(pool)
	limiter := ratelimit.NewLimiter(settingsRepo)
	ipLimiter := ratelimit.NewIPLimiter(30, 60, nil)

	handler := &Handler{
		cfg:            &config.Config{MasterKey: "test-master-key"},
		settingsRepo:   settingsRepo,
		failoverRepo:   failoverRepo,
		modelRepo:      modelRepo,
		providerRepo:   providerRepo,
		virtualKeyRepo: virtualKeyRepo,
		rateLimiter:    limiter,
		ipLimiter:      ipLimiter,
		dbPool:         pool,
	}

	// Test with empty model name
	reqBody := map[string]interface{}{
		"model": "",
		"messages": []map[string]interface{}{
			{"role": "user", "content": "Hello"},
		},
	}
	reqJSON, _ := json.Marshal(reqBody)

	req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(reqJSON))
	req.Header.Set("Content-Type", "application/json")
	req = withAuthContext(req)

	rr := httptest.NewRecorder()
	handler.ChatCompletions(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty model, got %d", rr.Code)
	}

	var errorResponse map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&errorResponse); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}

	if errorResponse["error"] == nil {
		t.Error("expected error message in response")
	}
}

// Test ChatCompletions with invalid request body
func TestChatCompletions_InvalidRequestBody(t *testing.T) {
	if testDB == nil {
		t.Skip("database not available")
	}

	pool := testDB.Pool()

	settingsRepo := settings.NewRepository(pool)
	failoverRepo := failover.NewRepository(pool)
	modelRepo := model.NewRepository(pool)
	providerRepo := provider.NewRepository(pool)
	virtualKeyRepo := virtualkey.NewRepository(pool)
	limiter := ratelimit.NewLimiter(settingsRepo)
	ipLimiter := ratelimit.NewIPLimiter(30, 60, nil)

	handler := &Handler{
		cfg:            &config.Config{MasterKey: "test-master-key"},
		settingsRepo:   settingsRepo,
		failoverRepo:   failoverRepo,
		modelRepo:      modelRepo,
		providerRepo:   providerRepo,
		virtualKeyRepo: virtualKeyRepo,
		rateLimiter:    limiter,
		ipLimiter:      ipLimiter,
		dbPool:         pool,
	}

	// Test with invalid JSON
	req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader([]byte("{invalid json")))
	req.Header.Set("Content-Type", "application/json")
	req = withAuthContext(req)

	rr := httptest.NewRecorder()
	handler.ChatCompletions(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid JSON, got %d", rr.Code)
	}
}
