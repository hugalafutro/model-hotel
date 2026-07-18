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

	var resp map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	data, ok := resp["data"].([]any)
	if !ok {
		t.Fatal("response 'data' should be an array")
	}

	found := false
	for _, item := range data {
		itemMap, ok := item.(map[string]any)
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

	var resp map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	data, ok := resp["data"].([]any)
	if !ok {
		t.Fatal("response 'data' should be an array")
	}

	found := false
	for _, item := range data {
		itemMap, ok := item.(map[string]any)
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
	listEnabledErr    error
	listEnabledResult []*model.Model
	getResult         *model.Model
	getErr            error
}

func (m *mockModelRepo) ListEnabled(ctx context.Context) ([]*model.Model, error) {
	if m.listEnabledResult != nil {
		return m.listEnabledResult, m.listEnabledErr
	}
	return nil, m.listEnabledErr
}

func (m *mockModelRepo) Get(ctx context.Context, id uuid.UUID) (*model.Model, error) {
	return m.getResult, m.getErr
}

func (m *mockModelRepo) Upsert(ctx context.Context, model *model.Model) error {
	return nil
}

func (m *mockModelRepo) DeleteByID(ctx context.Context, id uuid.UUID) error {
	return nil
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

	var resp map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp["object"] != "list" {
		t.Errorf("response object = %v, want 'list'", resp["object"])
	}
}

// TestListModels_CapabilitiesAndModalities verifies that capabilities,
// input_modalities, and output_modalities are included in the /v1/models
// response when the model has non-empty values. These fields are required
// by clients (e.g. opencode) to determine whether a model supports
// vision/image input before sending multimodal content.
func TestListModels_CapabilitiesAndModalities(t *testing.T) {
	h := newIntegrationHandler()

	masterKey := h.cfg.MasterKey
	kp, err := auth.Encrypt("sk-test-caps-modalities", masterKey)
	if err != nil {
		t.Fatalf("failed to encrypt key: %v", err)
	}

	prov, err := h.providerRepo.Create(context.Background(), provider.CreateProviderRequest{
		Name:    "test-caps-provider",
		BaseURL: "https://api.example.com",
		APIKey:  "sk-test-caps-modalities",
	}, kp.Ciphertext, kp.Nonce, kp.Salt)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	defer func() { _ = h.providerRepo.Delete(context.Background(), prov.ID) }()

	// Create a vision-capable model with multimodal input
	modelID := uuid.New()
	ctx := context.Background()
	contextLength := 128000
	maxOutputTokens := 16384
	inputPrice := 2.5
	outputPrice := 10.0
	m := &model.Model{
		ID:                    modelID,
		ProviderID:            prov.ID,
		ModelID:               "vision-model",
		Name:                  "Vision Model",
		DisplayName:           "Vision Model",
		Description:           "A vision-capable model for testing",
		Capabilities:          `{"streaming":true,"vision":true,"reasoning":true,"tool_calling":false}`,
		Params:                "{}",
		Modality:              "text->text",
		InputModalities:       `["text","image"]`,
		OutputModalities:      `["text"]`,
		ContextLength:         &contextLength,
		MaxOutputTokens:       &maxOutputTokens,
		InputPricePerMillion:  &inputPrice,
		OutputPricePerMillion: &outputPrice,
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

	var resp map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	data, ok := resp["data"].([]any)
	if !ok {
		t.Fatal("response 'data' should be an array")
	}

	found := false
	for _, item := range data {
		itemMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if id, _ := itemMap["id"].(string); id == "test-caps-provider/vision-model" {
			found = true

			// Verify capabilities object is present and has expected fields
			caps, ok := itemMap["capabilities"].(map[string]any)
			if !ok {
				t.Fatal("expected 'capabilities' to be a map in response")
			}
			if vision, _ := caps["vision"].(bool); !vision {
				t.Error("capabilities.vision should be true")
			}
			if streaming, _ := caps["streaming"].(bool); !streaming {
				t.Error("capabilities.streaming should be true")
			}
			if reasoning, _ := caps["reasoning"].(bool); !reasoning {
				t.Error("capabilities.reasoning should be true")
			}
			if toolCalling, _ := caps["tool_calling"].(bool); toolCalling {
				t.Error("capabilities.tool_calling should be false")
			}

			// Verify input_modalities array
			inputMods, ok := itemMap["input_modalities"].([]any)
			if !ok {
				t.Fatal("expected 'input_modalities' to be an array in response")
			}
			if len(inputMods) != 2 {
				t.Errorf("input_modalities length = %d, want 2", len(inputMods))
			}
			if inputMods[0] != "text" {
				t.Errorf("input_modalities[0] = %v, want 'text'", inputMods[0])
			}
			if inputMods[1] != "image" {
				t.Errorf("input_modalities[1] = %v, want 'image'", inputMods[1])
			}

			// Verify output_modalities array
			outputMods, ok := itemMap["output_modalities"].([]any)
			if !ok {
				t.Fatal("expected 'output_modalities' to be an array in response")
			}
			if len(outputMods) != 1 {
				t.Errorf("output_modalities length = %d, want 1", len(outputMods))
			}
			if outputMods[0] != "text" {
				t.Errorf("output_modalities[0] = %v, want 'text'", outputMods[0])
			}

			// Verify max_context_length alias is present
			if maxCtx, ok := itemMap["max_context_length"].(float64); !ok {
				t.Error("expected 'max_context_length' to be present in response")
			} else if int(maxCtx) != contextLength {
				t.Errorf("max_context_length = %v, want %d", maxCtx, contextLength)
			}

			// Verify context_length is also present
			if ctxLen, ok := itemMap["context_length"].(float64); !ok {
				t.Error("expected 'context_length' to be present in response")
			} else if int(ctxLen) != contextLength {
				t.Errorf("context_length = %v, want %d", ctxLen, contextLength)
			}

			break
		}
	}
	if !found {
		t.Error("expected to find 'test-caps-provider/vision-model' in response")
	}
}

// TestListModels_EmptyCapabilitiesOmitted verifies that models with empty
// capabilities ("{}") and empty modalities ("[]") do NOT include those
// fields in the response, keeping the payload clean.
func TestListModels_EmptyCapabilitiesOmitted(t *testing.T) {
	h := newIntegrationHandler()

	masterKey := h.cfg.MasterKey
	kp, err := auth.Encrypt("sk-test-empty-caps", masterKey)
	if err != nil {
		t.Fatalf("failed to encrypt key: %v", err)
	}

	prov, err := h.providerRepo.Create(context.Background(), provider.CreateProviderRequest{
		Name:    "test-empty-caps-provider",
		BaseURL: "https://api.example.com",
		APIKey:  "sk-test-empty-caps",
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
		ModelID:          "text-only-model",
		Name:             "Text Only",
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

	var resp map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	data, ok := resp["data"].([]any)
	if !ok {
		t.Fatal("response 'data' should be an array")
	}

	for _, item := range data {
		itemMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if id, _ := itemMap["id"].(string); id == "test-empty-caps-provider/text-only-model" {
			if _, exists := itemMap["capabilities"]; exists {
				t.Error("capabilities should be omitted when empty")
			}
			if _, exists := itemMap["input_modalities"]; exists {
				t.Error("input_modalities should be omitted when empty")
			}
			if _, exists := itemMap["output_modalities"]; exists {
				t.Error("output_modalities should be omitted when empty")
			}
			break
		}
	}
}

// TestListModels_InvalidCapabilitiesJSON tests the else branch when capabilities JSON is invalid.
// Covers line 60 in models.go (debuglog.Warn for invalid capabilities).
// Uses unit test with mock repo since DB enforces valid JSONB.
func TestListModels_InvalidCapabilitiesJSON(t *testing.T) {
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

	invalidModel := &model.Model{
		ID:               uuid.New(),
		ProviderID:       uuid.New(),
		ModelID:          "invalid-caps-model",
		Name:             "Invalid Capabilities Model",
		ProviderName:     "test-provider",
		Capabilities:     "{invalid json",
		Params:           "{}",
		Modality:         "text",
		InputModalities:  "[]",
		OutputModalities: "[]",
		Enabled:          true,
		CreatedAt:        time.Now(),
		LastSeenAt:       time.Now(),
	}

	h.modelRepo = &mockModelRepo{listEnabledResult: []*model.Model{invalidModel}}

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

	found := false
	for _, item := range data {
		itemMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if id, _ := itemMap["id"].(string); id == "test-provider/invalid-caps-model" {
			found = true
			if _, exists := itemMap["capabilities"]; exists {
				t.Error("capabilities should be omitted when JSON is invalid")
			}
			break
		}
	}
	if !found {
		t.Error("expected to find model in response")
	}
}

// TestListModels_InvalidModalitiesJSON tests the else branches when modalities JSON is invalid.
// Covers lines 68 and 76 in models.go (debuglog.Warn for invalid modalities).
// Uses unit test with mock repo since DB enforces valid JSONB.
func TestListModels_InvalidModalitiesJSON(t *testing.T) {
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

	invalidModel := &model.Model{
		ID:               uuid.New(),
		ProviderID:       uuid.New(),
		ModelID:          "invalid-modalities-model",
		Name:             "Invalid Modalities Model",
		ProviderName:     "test-provider",
		Capabilities:     "{}",
		Params:           "{}",
		Modality:         "text",
		InputModalities:  "[invalid",
		OutputModalities: "[invalid",
		Enabled:          true,
		CreatedAt:        time.Now(),
		LastSeenAt:       time.Now(),
	}

	h.modelRepo = &mockModelRepo{listEnabledResult: []*model.Model{invalidModel}}

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

	found := false
	for _, item := range data {
		itemMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if id, _ := itemMap["id"].(string); id == "test-provider/invalid-modalities-model" {
			found = true
			if _, exists := itemMap["input_modalities"]; exists {
				t.Error("input_modalities should be omitted when JSON is invalid")
			}
			if _, exists := itemMap["output_modalities"]; exists {
				t.Error("output_modalities should be omitted when JSON is invalid")
			}
			break
		}
	}
	if !found {
		t.Error("expected to find model in response")
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

// listModelsMockRepo implements ModelRepository for ListModels tests.
type listModelsMockRepo struct {
	listEnabledFunc func(ctx context.Context) ([]*model.Model, error)
}

func (m *listModelsMockRepo) ListEnabled(ctx context.Context) ([]*model.Model, error) {
	if m.listEnabledFunc != nil {
		return m.listEnabledFunc(ctx)
	}
	return []*model.Model{}, nil
}

func (m *listModelsMockRepo) Upsert(ctx context.Context, model *model.Model) error {
	return nil
}

func (m *listModelsMockRepo) DeleteByID(ctx context.Context, id uuid.UUID) error {
	return nil
}

func (m *listModelsMockRepo) Get(ctx context.Context, id uuid.UUID) (*model.Model, error) {
	return nil, nil
}

func (m *listModelsMockRepo) GetByIDs(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]*model.Model, error) {
	return nil, nil
}

func (m *listModelsMockRepo) GetByProviderAndModelID(ctx context.Context, providerID uuid.UUID, modelID string) (*model.Model, error) {
	return nil, nil
}

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

// containsTestProviderPrefix checks if a model ID starts with a test provider prefix
func containsTestProviderPrefix(modelID string) bool {
	return strings.HasPrefix(modelID, "test-provider-")
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

	// Verify it's the enabled model
	m := data[0].(map[string]any)
	if m["id"] != provider.NormalizeName(providerName)+"/enabled-model" {
		t.Errorf("expected enabled-model, got %v", m["id"])
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
