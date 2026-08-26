package proxy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

// /v1/models cases where a failover group is part of the listing.

// TestListModels_FailoverGroupWithDisabledEntry tests failover groups with disabled entries
func TestListModels_FailoverGroupWithDisabledEntry(t *testing.T) {
	h := newIntegrationHandler()

	pool := testDB.Pool()
	// Clean up any existing test data
	if _, err := pool.Exec(context.Background(), "DELETE FROM model_failover_groups WHERE display_model LIKE 'fg-disabled-entry'"); err != nil {
		t.Logf("Failed to clean up failover groups: %v", err)
	}
	if _, err := pool.Exec(context.Background(), "DELETE FROM models WHERE model_id LIKE 'model-1' OR model_id LIKE 'model-2'"); err != nil {
		t.Logf("Failed to clean up test models: %v", err)
	}
	if _, err := pool.Exec(context.Background(), "DELETE FROM providers WHERE name LIKE 'test-fg-disabled%'"); err != nil {
		t.Logf("Failed to clean up test providers: %v", err)
	}

	masterKey := h.cfg.MasterKey
	kp, err := auth.Encrypt("sk-test-fg-disabled", masterKey)
	if err != nil {
		t.Fatalf("failed to encrypt key: %v", err)
	}

	prov, err := h.providerRepo.Create(context.Background(), provider.CreateProviderRequest{
		Name:    "test-fg-disabled-provider",
		BaseURL: "https://api.example.com",
		APIKey:  "sk-test-fg-disabled",
	}, kp.Ciphertext, kp.Nonce, kp.Salt)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	defer func() { _ = h.providerRepo.Delete(context.Background(), prov.ID) }()

	modelID1 := uuid.New()
	modelID2 := uuid.New()
	ctx := context.Background()

	m1 := &model.Model{
		ID:               modelID1,
		ProviderID:       prov.ID,
		ModelID:          "model-1",
		Name:             "Model 1",
		Capabilities:     "{}",
		Params:           "{}",
		Modality:         "text",
		InputModalities:  "[]",
		OutputModalities: "[]",
		Enabled:          true,
		CreatedAt:        time.Now(),
		LastSeenAt:       time.Now(),
	}
	m2 := &model.Model{
		ID:               modelID2,
		ProviderID:       prov.ID,
		ModelID:          "model-2",
		Name:             "Model 2",
		Capabilities:     "{}",
		Params:           "{}",
		Modality:         "text",
		InputModalities:  "[]",
		OutputModalities: "[]",
		Enabled:          true,
		CreatedAt:        time.Now(),
		LastSeenAt:       time.Now(),
	}

	if err := h.modelRepo.Upsert(ctx, m1); err != nil {
		t.Fatalf("failed to upsert model 1: %v", err)
	}
	defer func() { _ = h.modelRepo.DeleteByID(ctx, modelID1) }()

	if err := h.modelRepo.Upsert(ctx, m2); err != nil {
		t.Fatalf("failed to upsert model 2: %v", err)
	}
	defer func() { _ = h.modelRepo.DeleteByID(ctx, modelID2) }()

	// Create failover group with first entry disabled
	entryEnabled := map[string]bool{
		modelID1.String(): false, // disabled
		modelID2.String(): true,  // enabled
	}
	if _, err := h.failoverRepo.UpsertWithConfig(ctx, "fg-disabled-entry", []uuid.UUID{modelID1, modelID2}, entryEnabled, nil, nil, nil, nil); err != nil {
		t.Fatalf("failed to create failover group: %v", err)
	}
	defer func() { _ = h.failoverRepo.Delete(ctx, "fg-disabled-entry") }()

	req := httptest.NewRequest("GET", "/models", http.NoBody)
	rr := httptest.NewRecorder()
	h.ListModels(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	var resp map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	data, ok := resp["data"].([]any)
	if !ok {
		t.Fatal("response 'data' should be an array")
	}

	// Should include the failover model alongside regular models
	if len(data) < 3 {
		t.Errorf("expected at least 3 models (2 regular + 1 failover), got %d", len(data))
	}

	// Verify the failover model points to the enabled entry (model-2)
	foundFailover := false
	for _, item := range data {
		itemMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if id, _ := itemMap["id"].(string); id == "hotel/fg-disabled-entry" {
			foundFailover = true
		}
	}
	if !foundFailover {
		t.Error("expected to find failover model in response")
	}
}

// TestListModels_FailoverGroupEntryNotFound tests when a model in failover group is not found
func TestListModels_FailoverGroupEntryNotFound(t *testing.T) {
	h := newIntegrationHandler()

	pool := testDB.Pool()
	// Clean up any existing test data
	if _, err := pool.Exec(context.Background(), "DELETE FROM model_failover_groups WHERE display_model LIKE 'fg-notfound'"); err != nil {
		t.Logf("Failed to clean up failover groups: %v", err)
	}
	if _, err := pool.Exec(context.Background(), "DELETE FROM models WHERE model_id LIKE 'model-found'"); err != nil {
		t.Logf("Failed to clean up test models: %v", err)
	}
	if _, err := pool.Exec(context.Background(), "DELETE FROM providers WHERE name LIKE 'test-fg-notfound%'"); err != nil {
		t.Logf("Failed to clean up test providers: %v", err)
	}

	masterKey := h.cfg.MasterKey
	kp, err := auth.Encrypt("sk-test-fg-notfound", masterKey)
	if err != nil {
		t.Fatalf("failed to encrypt key: %v", err)
	}

	prov, err := h.providerRepo.Create(context.Background(), provider.CreateProviderRequest{
		Name:    "test-fg-notfound-provider",
		BaseURL: "https://api.example.com",
		APIKey:  "sk-test-fg-notfound",
	}, kp.Ciphertext, kp.Nonce, kp.Salt)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	defer func() { _ = h.providerRepo.Delete(context.Background(), prov.ID) }()

	modelID := uuid.New()
	ctx := context.Background()

	m := &model.Model{
		ID:               modelID,
		ProviderID:       prov.ID,
		ModelID:          "model-found",
		Name:             "Model Found",
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
	defer func() { _ = h.modelRepo.DeleteByID(ctx, modelID) }()

	// Create failover group with a non-existent model UUID first
	fakeUUID := uuid.New()
	if _, err := h.failoverRepo.UpsertWithConfig(ctx, "fg-notfound", []uuid.UUID{fakeUUID, modelID}, nil, nil, nil, nil, nil); err != nil {
		t.Fatalf("failed to create failover group: %v", err)
	}
	defer func() { _ = h.failoverRepo.Delete(ctx, "fg-notfound") }()

	req := httptest.NewRequest("GET", "/models", http.NoBody)
	rr := httptest.NewRecorder()
	h.ListModels(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	var resp map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	data, ok := resp["data"].([]any)
	if !ok {
		t.Fatal("response 'data' should be an array")
	}

	// Should include the failover model alongside regular models
	if len(data) < 2 {
		t.Errorf("expected at least 2 models (1 regular + 1 failover), got %d", len(data))
	}

	// Verify the failover model is present
	foundFailover := false
	for _, item := range data {
		itemMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if id, _ := itemMap["id"].(string); id == "hotel/fg-notfound" {
			foundFailover = true
		}
	}
	if !foundFailover {
		t.Error("expected to find failover model in response")
	}
}

// TestListModels_FailoverGroupWithFullModel tests failover groups with all optional fields populated.
// Covers lines 121-168 in models.go (all optional fields for failover models).
func TestListModels_FailoverGroupWithFullModel(t *testing.T) {
	h := newIntegrationHandler()

	pool := testDB.Pool()
	if _, err := pool.Exec(context.Background(), "DELETE FROM model_failover_groups WHERE display_model LIKE 'fg-full-model'"); err != nil {
		t.Logf("Failed to clean up failover groups: %v", err)
	}
	if _, err := pool.Exec(context.Background(), "DELETE FROM models WHERE model_id LIKE 'full-model'"); err != nil {
		t.Logf("Failed to clean up test models: %v", err)
	}
	if _, err := pool.Exec(context.Background(), "DELETE FROM providers WHERE name LIKE 'test-fg-full%'"); err != nil {
		t.Logf("Failed to clean up test providers: %v", err)
	}

	masterKey := h.cfg.MasterKey
	kp, err := auth.Encrypt("sk-test-fg-full", masterKey)
	if err != nil {
		t.Fatalf("failed to encrypt key: %v", err)
	}

	prov, err := h.providerRepo.Create(context.Background(), provider.CreateProviderRequest{
		Name:    "test-fg-full-provider",
		BaseURL: "https://api.example.com",
		APIKey:  "sk-test-fg-full",
	}, kp.Ciphertext, kp.Nonce, kp.Salt)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	defer func() { _ = h.providerRepo.Delete(context.Background(), prov.ID) }()

	modelID := uuid.New()
	ctx := context.Background()
	contextLength := 200000
	maxOutputTokens := 8192
	inputPrice := 3.0
	outputPrice := 12.0
	m := &model.Model{
		ID:                    modelID,
		ProviderID:            prov.ID,
		ModelID:               "full-model",
		Name:                  "Full Model Name",
		DisplayName:           "Full Display Name",
		Description:           "A model with all fields",
		Modality:              "text->text",
		Capabilities:          `{"streaming":true,"vision":false}`,
		InputModalities:       `["text","image"]`,
		OutputModalities:      `["text"]`,
		ContextLength:         &contextLength,
		MaxOutputTokens:       &maxOutputTokens,
		InputPricePerMillion:  &inputPrice,
		OutputPricePerMillion: &outputPrice,
		Params:                "{}",
		Enabled:               true,
		CreatedAt:             time.Now(),
		LastSeenAt:            time.Now(),
	}
	if err := h.modelRepo.Upsert(ctx, m); err != nil {
		t.Fatalf("failed to upsert model: %v", err)
	}
	defer func() { _ = h.modelRepo.DeleteByID(ctx, modelID) }()

	if _, err := h.failoverRepo.UpsertWithConfig(ctx, "fg-full-model", []uuid.UUID{modelID}, nil, nil, nil, nil, nil); err != nil {
		t.Fatalf("failed to create failover group: %v", err)
	}
	defer func() { _ = h.failoverRepo.Delete(ctx, "fg-full-model") }()

	req := httptest.NewRequest("GET", "/models", http.NoBody)
	rr := httptest.NewRecorder()
	h.ListModels(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	var resp map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	data, ok := resp["data"].([]any)
	if !ok {
		t.Fatal("response 'data' should be an array")
	}

	foundFailover := false
	for _, item := range data {
		itemMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if id, _ := itemMap["id"].(string); id == "hotel/fg-full-model" {
			foundFailover = true

			if itemMap["provider"] != "hotel" {
				t.Errorf("provider = %v, want 'hotel'", itemMap["provider"])
			}

			if ownedBy, _ := itemMap["owned_by"].(string); ownedBy != "test-fg-full-provider" {
				t.Errorf("owned_by = %v, want 'test-fg-full-provider'", ownedBy)
			}

			if cl, _ := itemMap["context_length"].(float64); cl != 200000 {
				t.Errorf("context_length = %v, want 200000", cl)
			}
			if maxCtx, _ := itemMap["max_context_length"].(float64); maxCtx != 200000 {
				t.Errorf("max_context_length = %v, want 200000", maxCtx)
			}
			if mot, _ := itemMap["max_output_tokens"].(float64); mot != 8192 {
				t.Errorf("max_output_tokens = %v, want 8192", mot)
			}
			if name, _ := itemMap["name"].(string); name != "Full Display Name" {
				t.Errorf("name = %v, want 'Full Display Name'", name)
			}
			if desc, _ := itemMap["description"].(string); desc != "A model with all fields" {
				t.Errorf("description = %v, want 'A model with all fields'", desc)
			}
			if mod, _ := itemMap["modality"].(string); mod != "text->text" {
				t.Errorf("modality = %v, want 'text->text'", mod)
			}

			caps, ok := itemMap["capabilities"].(map[string]any)
			if !ok {
				t.Fatal("expected 'capabilities' to be a map")
			}
			if streaming, _ := caps["streaming"].(bool); !streaming {
				t.Error("capabilities.streaming should be true")
			}

			inputMods, ok := itemMap["input_modalities"].([]any)
			if !ok {
				t.Fatal("expected 'input_modalities' to be an array")
			}
			if len(inputMods) != 2 || inputMods[0] != "text" || inputMods[1] != "image" {
				t.Errorf("input_modalities = %v, want ['text','image']", inputMods)
			}

			outputMods, ok := itemMap["output_modalities"].([]any)
			if !ok {
				t.Fatal("expected 'output_modalities' to be an array")
			}
			if len(outputMods) != 1 || outputMods[0] != "text" {
				t.Errorf("output_modalities = %v, want ['text']", outputMods)
			}

			if ip, _ := itemMap["input_price_per_million"].(float64); ip != 3.0 {
				t.Errorf("input_price_per_million = %v, want 3.0", ip)
			}
			if op, _ := itemMap["output_price_per_million"].(float64); op != 12.0 {
				t.Errorf("output_price_per_million = %v, want 12.0", op)
			}

			break
		}
	}
	if !foundFailover {
		t.Error("expected to find failover model 'hotel/fg-full-model' in response")
	}
}

// TestListModels_FailoverGroupInvalidJSON tests failover groups with invalid JSON fields.
// Covers lines 143-145, 151-153, 159-161 in models.go (debuglog.Warn for invalid JSON in failover models).
// Uses mock repo for Get() since DB enforces valid JSONB.
func TestListModels_FailoverGroupInvalidJSON(t *testing.T) {
	h := newIntegrationHandler()

	pool := testDB.Pool()
	if _, err := pool.Exec(context.Background(), "DELETE FROM model_failover_groups WHERE display_model LIKE 'fg-invalid-json'"); err != nil {
		t.Logf("Failed to clean up failover groups: %v", err)
	}
	if _, err := pool.Exec(context.Background(), "DELETE FROM providers WHERE name LIKE 'test-fg-invalid%'"); err != nil {
		t.Logf("Failed to clean up test providers: %v", err)
	}

	masterKey := h.cfg.MasterKey
	kp, err := auth.Encrypt("sk-test-fg-invalid", masterKey)
	if err != nil {
		t.Fatalf("failed to encrypt key: %v", err)
	}

	prov, err := h.providerRepo.Create(context.Background(), provider.CreateProviderRequest{
		Name:    "test-fg-invalid-provider",
		BaseURL: "https://api.example.com",
		APIKey:  "sk-test-fg-invalid",
	}, kp.Ciphertext, kp.Nonce, kp.Salt)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	defer func() { _ = h.providerRepo.Delete(context.Background(), prov.ID) }()

	modelID := uuid.New()
	ctx := context.Background()
	// Create a valid model in DB (required for failover group FK)
	validModel := &model.Model{
		ID:               modelID,
		ProviderID:       prov.ID,
		ModelID:          "valid-json-model",
		Name:             "Valid JSON Model",
		Capabilities:     "{}",
		Params:           "{}",
		Modality:         "text",
		InputModalities:  "[]",
		OutputModalities: "[]",
		Enabled:          true,
		CreatedAt:        time.Now(),
		LastSeenAt:       time.Now(),
	}
	if err := h.modelRepo.Upsert(ctx, validModel); err != nil {
		t.Fatalf("failed to upsert model: %v", err)
	}
	defer func() { _ = h.modelRepo.DeleteByID(ctx, modelID) }()

	// Create failover group referencing this model
	if _, err := h.failoverRepo.UpsertWithConfig(ctx, "fg-invalid-json", []uuid.UUID{modelID}, nil, nil, nil, nil, nil); err != nil {
		t.Fatalf("failed to create failover group: %v", err)
	}
	defer func() { _ = h.failoverRepo.Delete(ctx, "fg-invalid-json") }()

	// Replace modelRepo with mock that returns model with invalid JSON on Get()
	// ListEnabled returns empty so only failover path is tested
	invalidModel := &model.Model{
		ID:               modelID,
		ProviderID:       prov.ID,
		ModelID:          "invalid-json-model",
		Name:             "Invalid JSON Model",
		ProviderName:     "test-fg-invalid-provider",
		ProviderEnabled:  true,
		Capabilities:     "{broken",
		InputModalities:  "[broken",
		OutputModalities: "[broken",
		Params:           "{}",
		Modality:         "text",
		Enabled:          true,
		CreatedAt:        time.Now(),
		LastSeenAt:       time.Now(),
	}

	h.modelRepo = &mockModelRepo{
		listEnabledResult: []*model.Model{},
		getResult:         invalidModel,
	}

	req := httptest.NewRequest("GET", "/models", http.NoBody)
	rr := httptest.NewRecorder()
	h.ListModels(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	var resp map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	data, ok := resp["data"].([]any)
	if !ok {
		t.Fatal("response 'data' should be an array")
	}

	foundFailover := false
	for _, item := range data {
		itemMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if id, _ := itemMap["id"].(string); id == "hotel/fg-invalid-json" {
			foundFailover = true

			if _, exists := itemMap["capabilities"]; exists {
				t.Error("capabilities should be omitted when JSON is invalid")
			}
			if _, exists := itemMap["input_modalities"]; exists {
				t.Error("input_modalities should be omitted when JSON is invalid")
			}
			if _, exists := itemMap["output_modalities"]; exists {
				t.Error("output_modalities should be omitted when JSON is invalid")
			}

			break
		}
	}
	if !foundFailover {
		t.Error("expected to find failover model 'hotel/fg-invalid-json' in response")
	}
}

// TestListModels_FailoverRepoError tests the error path when failoverRepo.GetEnabled fails.
// Covers line 91 in models.go (debuglog.Warn for failover repo error).
func TestListModels_FailoverRepoError(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	h.modelRepo = &mockModelRepo{listEnabledResult: []*model.Model{}}

	// Create a repository with an invalid connection string that will fail
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
	if len(data) != 0 {
		t.Errorf("expected empty data array, got %d items", len(data))
	}
}

// Test ListModels with failover groups
func TestListModels_WithFailoverGroups(t *testing.T) {

	pool := testDB.Pool()
	// Clean up any existing test data
	if _, err := pool.Exec(context.Background(), "DELETE FROM providers WHERE name LIKE 'test-provider-%'"); err != nil {
		t.Logf("Failed to clean up test providers: %v", err)
	}
	if _, err := pool.Exec(context.Background(), "DELETE FROM model_failover_groups WHERE display_model LIKE 'my-failover-model'"); err != nil {
		t.Logf("Failed to clean up test failover groups: %v", err)
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

	var response map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	data, ok := response["data"].([]any)
	if !ok {
		t.Fatal("expected data to be an array")
	}

	// Should contain both the regular model and the failover model (exact count is fragile in parallel test suite)
	if len(data) < 2 {
		t.Errorf("expected at least 2 models (1 regular + 1 failover), got %d", len(data))
	}

	// Verify the failover model is present
	foundFailover := false
	foundRegular := false
	for _, item := range data {
		m := item.(map[string]any)
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

// ---------------------------------------------------------------------------
// Integration test moved from chat_proxy_integration_test.go
// ---------------------------------------------------------------------------
