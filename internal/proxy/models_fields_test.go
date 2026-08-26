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
)

// /v1/models cases about how a model's own fields are rendered:
// capabilities, modalities, optional fields and the response shape.

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
