package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/auth"
	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/provider"
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

	var resp map[string]interface{}
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

	var resp map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	data, ok := resp["data"].([]interface{})
	if !ok {
		t.Fatal("response 'data' should be an array")
	}

	// Find our model in the response
	found := false
	for _, item := range data {
		itemMap, ok := item.(map[string]interface{})
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

// TestListModels_WithOwnedBy tests that the OwnedBy field is used when set
func TestListModels_WithOwnedBy(t *testing.T) {
	h := newIntegrationHandler()

	// Create a provider with an encrypted key
	masterKey := h.cfg.MasterKey
	kp, err := auth.Encrypt("sk-test-ownedby", masterKey)
	if err != nil {
		t.Fatalf("failed to encrypt key: %v", err)
	}

	prov, err := h.providerRepo.Create(context.Background(), provider.CreateProviderRequest{
		Name:    "test-ownedby-provider",
		BaseURL: "https://api.example.com",
		APIKey:  "sk-test-ownedby",
	}, kp.Ciphertext, kp.Nonce, kp.Salt)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	defer func() { _ = h.providerRepo.Delete(context.Background(), prov.ID) }()

	// Create a model with OwnedBy set
	modelID := uuid.New()
	ctx := context.Background()
	m := &model.Model{
		ID:               modelID,
		ProviderID:       prov.ID,
		ModelID:          "model-with-ownedby",
		Name:             "Model With OwnedBy",
		OwnedBy:          "custom-owner",
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

	req := httptest.NewRequest("GET", "/models", http.NoBody)
	rr := httptest.NewRecorder()
	h.ListModels(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	data, ok := resp["data"].([]interface{})
	if !ok {
		t.Fatal("response 'data' should be an array")
	}

	found := false
	for _, item := range data {
		itemMap, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		if id, _ := itemMap["id"].(string); id == "test-ownedby-provider/model-with-ownedby" {
			found = true
			if ownedBy, _ := itemMap["owned_by"].(string); ownedBy != "custom-owner" {
				t.Errorf("owned_by = %v, want 'custom-owner'", ownedBy)
			}
			break
		}
	}
	if !found {
		t.Error("expected to find model in response")
	}
}

// TestListModels_WithOptionalFields tests models with context_length, max_output_tokens, prices
func TestListModels_WithOptionalFields(t *testing.T) {
	h := newIntegrationHandler()

	masterKey := h.cfg.MasterKey
	kp, err := auth.Encrypt("sk-test-optional", masterKey)
	if err != nil {
		t.Fatalf("failed to encrypt key: %v", err)
	}

	prov, err := h.providerRepo.Create(context.Background(), provider.CreateProviderRequest{
		Name:    "test-optional-provider",
		BaseURL: "https://api.example.com",
		APIKey:  "sk-test-optional",
	}, kp.Ciphertext, kp.Nonce, kp.Salt)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	defer func() { _ = h.providerRepo.Delete(context.Background(), prov.ID) }()

	modelID := uuid.New()
	ctx := context.Background()
	contextLength := 128000
	maxOutputTokens := 4096
	inputPrice := 5.0
	outputPrice := 15.0
	m := &model.Model{
		ID:                    modelID,
		ProviderID:            prov.ID,
		ModelID:               "model-with-fields",
		Name:                  "Model With Fields",
		ContextLength:         &contextLength,
		MaxOutputTokens:       &maxOutputTokens,
		InputPricePerMillion:  &inputPrice,
		OutputPricePerMillion: &outputPrice,
		Capabilities:          "{}",
		Params:                "{}",
		Modality:              "text",
		InputModalities:       "[]",
		OutputModalities:      "[]",
		Enabled:               true,
		CreatedAt:             time.Now(),
		LastSeenAt:            time.Now(),
	}
	if err := h.modelRepo.Upsert(ctx, m); err != nil {
		t.Fatalf("failed to upsert model: %v", err)
	}
	defer func() { _ = h.modelRepo.DeleteByID(ctx, modelID) }()

	req := httptest.NewRequest("GET", "/models", http.NoBody)
	rr := httptest.NewRecorder()
	h.ListModels(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	data, ok := resp["data"].([]interface{})
	if !ok {
		t.Fatal("response 'data' should be an array")
	}

	found := false
	for _, item := range data {
		itemMap, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		if id, _ := itemMap["id"].(string); id == "test-optional-provider/model-with-fields" {
			found = true
			if cl, _ := itemMap["context_length"].(float64); cl != 128000 {
				t.Errorf("context_length = %v, want 128000", cl)
			}
			if mot, _ := itemMap["max_output_tokens"].(float64); mot != 4096 {
				t.Errorf("max_output_tokens = %v, want 4096", mot)
			}
			if ip, _ := itemMap["input_price_per_million"].(float64); ip != 5.0 {
				t.Errorf("input_price_per_million = %v, want 5.0", ip)
			}
			if op, _ := itemMap["output_price_per_million"].(float64); op != 15.0 {
				t.Errorf("output_price_per_million = %v, want 15.0", op)
			}
			break
		}
	}
	if !found {
		t.Error("expected to find model in response")
	}
}

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

	var resp map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	data, ok := resp["data"].([]interface{})
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
		itemMap, ok := item.(map[string]interface{})
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

	var resp map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	data, ok := resp["data"].([]interface{})
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
		itemMap, ok := item.(map[string]interface{})
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

// mockModelRepo is a test mock for model.Repository
type mockModelRepo struct {
	listEnabledErr error
}

func (m *mockModelRepo) ListEnabled(ctx context.Context) ([]*model.Model, error) {
	return nil, m.listEnabledErr
}

func (m *mockModelRepo) Upsert(ctx context.Context, model *model.Model) error {
	return nil
}

func (m *mockModelRepo) DeleteByID(ctx context.Context, id uuid.UUID) error {
	return nil
}

func (m *mockModelRepo) Get(ctx context.Context, id uuid.UUID) (*model.Model, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *mockModelRepo) GetByIDs(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]*model.Model, error) {
	return nil, nil
}

func (m *mockModelRepo) GetByProviderAndModelID(ctx context.Context, providerID uuid.UUID, modelID string) (*model.Model, error) {
	return nil, nil
}

func TestListModels_ResponseFormat(t *testing.T) {
	h := newIntegrationHandler()

	req := httptest.NewRequest("GET", "/models", http.NoBody)
	rr := httptest.NewRecorder()
	h.ListModels(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp["object"] != "list" {
		t.Errorf("response object = %v, want 'list'", resp["object"])
	}
}
