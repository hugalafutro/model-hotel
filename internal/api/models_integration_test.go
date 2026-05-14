package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// TestListModels_InvalidProviderID tests that ListModels returns 400 for
// an invalid provider_id query parameter.
func TestListModels_InvalidProviderID(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/models?provider_id=invalid", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for invalid provider_id, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestUpdateModel_InvalidUUID tests that UpdateModel returns 400 for
// an invalid UUID in the path.
func TestUpdateModel_InvalidUUID(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/models/invalid-uuid", strings.NewReader(`{"enabled": true}`))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for invalid UUID, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestUpdateModel_NoFields tests that UpdateModel returns 400 when
// no fields are provided to update.
func TestUpdateModel_NoFields(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	// Create a provider
	providerData := fmt.Sprintf(`{"name": "test-model-provider-%s", "base_url": "https://api.openai.com", "api_key": "test-api-key"}`, uuid.New().String()[:8])
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d: %s", rec.Code, rec.Body.String())
	}

	var providerResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &providerResp); err != nil {
		t.Fatalf("Failed to parse provider response: %v", err)
	}

	// Insert a model directly via DB
	modelID := uuid.New().String()
	pool := h.Pool().Pool()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO models (id, provider_id, model_id, name, enabled) VALUES ($1, $2, $3, $4, $5)`,
		modelID, providerResp.ID, "gpt-4o-mini", "GPT-4o Mini", true)
	if err != nil {
		t.Fatalf("Failed to insert model: %v", err)
	}

	// Try to update with empty body
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPatch, "/models/"+modelID, strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for no fields to update, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "no fields to update") {
		t.Errorf("Expected error about no fields, got: %s", rec.Body.String())
	}
}

// TestUpdateModel_InvalidBody tests that UpdateModel returns 400 for
// invalid JSON body.
func TestUpdateModel_InvalidBody(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	// Create a provider
	providerData := fmt.Sprintf(`{"name": "test-model-provider-%s", "base_url": "https://api.openai.com", "api_key": "test-api-key"}`, uuid.New().String()[:8])
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d: %s", rec.Code, rec.Body.String())
	}

	var providerResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &providerResp); err != nil {
		t.Fatalf("Failed to parse provider response: %v", err)
	}

	// Insert a model directly via DB
	modelID := uuid.New().String()
	pool := h.Pool().Pool()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO models (id, provider_id, model_id, name, enabled) VALUES ($1, $2, $3, $4, $5)`,
		modelID, providerResp.ID, "gpt-4o-mini", "GPT-4o Mini", true)
	if err != nil {
		t.Fatalf("Failed to insert model: %v", err)
	}

	// Try to update with invalid JSON
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPatch, "/models/"+modelID, strings.NewReader(`not json`))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for invalid JSON, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestUpdateModel_InvalidDisplayName tests that UpdateModel returns 400 for
// an empty display name.
func TestUpdateModel_InvalidDisplayName(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	// Create a provider
	providerData := fmt.Sprintf(`{"name": "test-model-provider-%s", "base_url": "https://api.openai.com", "api_key": "test-api-key"}`, uuid.New().String()[:8])
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d: %s", rec.Code, rec.Body.String())
	}

	var providerResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &providerResp); err != nil {
		t.Fatalf("Failed to parse provider response: %v", err)
	}

	// Insert a model directly via DB
	modelID := uuid.New().String()
	pool := h.Pool().Pool()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO models (id, provider_id, model_id, name, enabled) VALUES ($1, $2, $3, $4, $5)`,
		modelID, providerResp.ID, "gpt-4o-mini", "GPT-4o Mini", true)
	if err != nil {
		t.Fatalf("Failed to insert model: %v", err)
	}

	// Try to update with empty display name
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPatch, "/models/"+modelID, strings.NewReader(`{"display_name": ""}`))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for empty display name, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestUpdateModel_InvalidContextLength tests that UpdateModel returns 400 for
// a negative context length.
func TestUpdateModel_InvalidContextLength(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	// Create a provider
	providerData := fmt.Sprintf(`{"name": "test-model-provider-%s", "base_url": "https://api.openai.com", "api_key": "test-api-key"}`, uuid.New().String()[:8])
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d: %s", rec.Code, rec.Body.String())
	}

	var providerResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &providerResp); err != nil {
		t.Fatalf("Failed to parse provider response: %v", err)
	}

	// Insert a model directly via DB
	modelID := uuid.New().String()
	pool := h.Pool().Pool()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO models (id, provider_id, model_id, name, enabled) VALUES ($1, $2, $3, $4, $5)`,
		modelID, providerResp.ID, "gpt-4o-mini", "GPT-4o Mini", true)
	if err != nil {
		t.Fatalf("Failed to insert model: %v", err)
	}

	// Try to update with negative context length
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPatch, "/models/"+modelID, strings.NewReader(`{"context_length": -1}`))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for negative context length, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestTestModel_InvalidUUID tests that TestModel returns 400 for
// an invalid UUID in the path.
func TestTestModel_InvalidUUID(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/models/invalid-uuid/test", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for invalid UUID, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestTestModel_NonExistent tests that TestModel returns 404 for
// a valid but non-existent model UUID.
func TestTestModel_NonExistent(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	nonExistentID := uuid.New().String()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/models/"+nonExistentID+"/test", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("Expected 404 for non-existent model, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestListModels_WithProviderIDFilter tests that ListModels correctly filters by provider_id.
func TestListModels_WithProviderIDFilter(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	// Create first provider
	provider1Data := fmt.Sprintf(`{"name": "test-provider1-%s", "base_url": "https://api.openai.com", "api_key": "test-key1"}`, uuid.New().String()[:8])
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(provider1Data))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider1: %d: %s", rec.Code, rec.Body.String())
	}

	var provider1Resp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &provider1Resp); err != nil {
		t.Fatalf("Failed to parse provider1 response: %v", err)
	}

	// Create second provider
	provider2Data := fmt.Sprintf(`{"name": "test-provider2-%s", "base_url": "https://api.anthropic.com", "api_key": "test-key2"}`, uuid.New().String()[:8])
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(provider2Data))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider2: %d: %s", rec.Code, rec.Body.String())
	}

	var provider2Resp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &provider2Resp); err != nil {
		t.Fatalf("Failed to parse provider2 response: %v", err)
	}

	// Insert 2 models for provider1
	pool := h.Pool().Pool()
	model1ID := uuid.New().String()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO models (id, provider_id, model_id, name, enabled) VALUES ($1, $2, $3, $4, $5)`,
		model1ID, provider1Resp.ID, "gpt-4o", "GPT-4o", true)
	if err != nil {
		t.Fatalf("Failed to insert model1: %v", err)
	}

	model2ID := uuid.New().String()
	_, err = pool.Exec(context.Background(),
		`INSERT INTO models (id, provider_id, model_id, name, enabled) VALUES ($1, $2, $3, $4, $5)`,
		model2ID, provider1Resp.ID, "gpt-4o-mini", "GPT-4o Mini", true)
	if err != nil {
		t.Fatalf("Failed to insert model2: %v", err)
	}

	// Insert 1 model for provider2
	model3ID := uuid.New().String()
	_, err = pool.Exec(context.Background(),
		`INSERT INTO models (id, provider_id, model_id, name, enabled) VALUES ($1, $2, $3, $4, $5)`,
		model3ID, provider2Resp.ID, "claude-3", "Claude 3", true)
	if err != nil {
		t.Fatalf("Failed to insert model3: %v", err)
	}

	// List models filtered by provider1
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/models?provider_id="+provider1Resp.ID, http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var models []ModelResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &models); err != nil {
		t.Fatalf("Failed to parse models response: %v", err)
	}

	if len(models) != 2 {
		t.Errorf("Expected 2 models for provider1, got %d", len(models))
	}
}

// TestListModels_EmptyList tests that ListModels returns an empty array when no models exist.
func TestListModels_EmptyList(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/models", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var models []ModelResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &models); err != nil {
		t.Fatalf("Failed to parse models response: %v", err)
	}

	if len(models) != 0 {
		t.Errorf("Expected empty array, got %d models", len(models))
	}
}

// TestUpdateModel_InvalidMaxOutputTokens tests that UpdateModel returns 400 for negative max_output_tokens.
func TestUpdateModel_InvalidMaxOutputTokens(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	// Create a provider
	providerData := fmt.Sprintf(`{"name": "test-provider-%s", "base_url": "https://api.openai.com", "api_key": "test-key"}`, uuid.New().String()[:8])
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d: %s", rec.Code, rec.Body.String())
	}

	var providerResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &providerResp); err != nil {
		t.Fatalf("Failed to parse provider response: %v", err)
	}

	// Insert a model
	modelID := uuid.New().String()
	pool := h.Pool().Pool()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO models (id, provider_id, model_id, name, enabled) VALUES ($1, $2, $3, $4, $5)`,
		modelID, providerResp.ID, "gpt-4o", "GPT-4o", true)
	if err != nil {
		t.Fatalf("Failed to insert model: %v", err)
	}

	// Try to update with negative max_output_tokens
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPatch, "/models/"+modelID, strings.NewReader(`{"max_output_tokens": -1}`))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for negative max_output_tokens, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestUpdateModel_InvalidInputPrice tests that UpdateModel returns 400 for negative input_price_per_million.
func TestUpdateModel_InvalidInputPrice(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	// Create a provider
	providerData := fmt.Sprintf(`{"name": "test-provider-%s", "base_url": "https://api.openai.com", "api_key": "test-key"}`, uuid.New().String()[:8])
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d: %s", rec.Code, rec.Body.String())
	}

	var providerResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &providerResp); err != nil {
		t.Fatalf("Failed to parse provider response: %v", err)
	}

	// Insert a model
	modelID := uuid.New().String()
	pool := h.Pool().Pool()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO models (id, provider_id, model_id, name, enabled) VALUES ($1, $2, $3, $4, $5)`,
		modelID, providerResp.ID, "gpt-4o", "GPT-4o", true)
	if err != nil {
		t.Fatalf("Failed to insert model: %v", err)
	}

	// Try to update with negative input_price_per_million
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPatch, "/models/"+modelID, strings.NewReader(`{"input_price_per_million": -5}`))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for negative input_price_per_million, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestUpdateModel_InvalidOutputPrice tests that UpdateModel returns 400 for negative output_price_per_million.
func TestUpdateModel_InvalidOutputPrice(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	// Create a provider
	providerData := fmt.Sprintf(`{"name": "test-provider-%s", "base_url": "https://api.openai.com", "api_key": "test-key"}`, uuid.New().String()[:8])
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d: %s", rec.Code, rec.Body.String())
	}

	var providerResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &providerResp); err != nil {
		t.Fatalf("Failed to parse provider response: %v", err)
	}

	// Insert a model
	modelID := uuid.New().String()
	pool := h.Pool().Pool()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO models (id, provider_id, model_id, name, enabled) VALUES ($1, $2, $3, $4, $5)`,
		modelID, providerResp.ID, "gpt-4o", "GPT-4o", true)
	if err != nil {
		t.Fatalf("Failed to insert model: %v", err)
	}

	// Try to update with negative output_price_per_million
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPatch, "/models/"+modelID, strings.NewReader(`{"output_price_per_million": -5}`))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for negative output_price_per_million, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestUpdateModel_Success tests that UpdateModel successfully updates display_name and enabled.
func TestUpdateModel_Success(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	// Create a provider
	providerData := fmt.Sprintf(`{"name": "test-provider-%s", "base_url": "https://api.openai.com", "api_key": "test-key"}`, uuid.New().String()[:8])
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d: %s", rec.Code, rec.Body.String())
	}

	var providerResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &providerResp); err != nil {
		t.Fatalf("Failed to parse provider response: %v", err)
	}

	// Insert a model
	modelID := uuid.New().String()
	pool := h.Pool().Pool()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO models (id, provider_id, model_id, name, enabled) VALUES ($1, $2, $3, $4, $5)`,
		modelID, providerResp.ID, "gpt-4o", "GPT-4o", true)
	if err != nil {
		t.Fatalf("Failed to insert model: %v", err)
	}

	// Update the model
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPatch, "/models/"+modelID, strings.NewReader(`{"display_name": "Updated Name", "enabled": false}`))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var modelResp ModelResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &modelResp); err != nil {
		t.Fatalf("Failed to parse model response: %v", err)
	}

	if modelResp.DisplayName != "Updated Name" {
		t.Errorf("Expected display_name 'Updated Name', got '%s'", modelResp.DisplayName)
	}
	if modelResp.Enabled {
		t.Errorf("Expected enabled to be false, got true")
	}
}

// TestDeleteModel_Success tests that DeleteModel removes a model and returns 204.
func TestDeleteModel_Success(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	// Create a provider
	providerData := fmt.Sprintf(`{"name": "test-provider-%s", "base_url": "https://api.openai.com", "api_key": "test-key"}`, uuid.New().String()[:8])
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d: %s", rec.Code, rec.Body.String())
	}

	var providerResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &providerResp); err != nil {
		t.Fatalf("Failed to parse provider response: %v", err)
	}

	// Insert a model
	modelID := uuid.New().String()
	pool := h.Pool().Pool()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO models (id, provider_id, model_id, name, enabled) VALUES ($1, $2, $3, $4, $5)`,
		modelID, providerResp.ID, "gpt-4o", "GPT-4o", true)
	if err != nil {
		t.Fatalf("Failed to insert model: %v", err)
	}

	// Delete the model
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodDelete, "/models/"+modelID, http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("Expected 204, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify the model is gone by listing all models
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/models", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 for list models, got %d: %s", rec.Code, rec.Body.String())
	}

	var models []ModelResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &models); err != nil {
		t.Fatalf("Failed to parse models response: %v", err)
	}

	for _, m := range models {
		if m.ID == modelID {
			t.Errorf("Deleted model %s still appears in list", modelID)
		}
	}
}

// TestTestModel_DisabledModel tests that TestModel returns 400 for a disabled model.
func TestTestModel_DisabledModel(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	// Create a provider
	providerData := fmt.Sprintf(`{"name": "test-provider-%s", "base_url": "https://api.openai.com", "api_key": "test-key"}`, uuid.New().String()[:8])
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d: %s", rec.Code, rec.Body.String())
	}

	var providerResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &providerResp); err != nil {
		t.Fatalf("Failed to parse provider response: %v", err)
	}

	// Insert a disabled model
	modelID := uuid.New().String()
	pool := h.Pool().Pool()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO models (id, provider_id, model_id, name, enabled) VALUES ($1, $2, $3, $4, $5)`,
		modelID, providerResp.ID, "gpt-4o", "GPT-4o", false)
	if err != nil {
		t.Fatalf("Failed to insert model: %v", err)
	}

	// Try to test the disabled model
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/models/"+modelID+"/test", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for disabled model, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "model is disabled") {
		t.Errorf("Expected error about disabled model, got: %s", rec.Body.String())
	}
}

// TestTestModel_MockProviderSuccess tests that TestModel successfully processes
// a 200 response from a mock provider.
func TestTestModel_MockProviderSuccess(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	// Create a mock server
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/chat/completions" && r.Method == http.MethodPost {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"Hi"}}],"usage":{"prompt_tokens":5,"completion_tokens":1}}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer mockServer.Close()

	// Create a provider with the mock server URL (test config allows localhost)
	providerData := fmt.Sprintf(`{"name": "test-provider-%s", "base_url": "%s", "api_key": "test-key"}`, uuid.New().String()[:8], mockServer.URL)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d: %s", rec.Code, rec.Body.String())
	}

	var providerResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &providerResp); err != nil {
		t.Fatalf("Failed to parse provider response: %v", err)
	}

	// Insert an enabled model
	pool := h.Pool().Pool()
	modelID := uuid.New().String()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO models (id, provider_id, model_id, name, enabled) VALUES ($1, $2, $3, $4, $5)`,
		modelID, providerResp.ID, "test-model", "Test Model", true)
	if err != nil {
		t.Fatalf("Failed to insert model: %v", err)
	}

	// Test the model
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/models/"+modelID+"/test", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var testResp struct {
		Success  bool   `json:"success"`
		Response string `json:"response"`
		Error    string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &testResp); err != nil {
		t.Fatalf("Failed to parse test response: %v", err)
	}

	if !testResp.Success {
		t.Errorf("Expected success to be true, got false. Error: %s", testResp.Error)
	}
	if testResp.Response == "" {
		t.Errorf("Expected non-empty response, got empty string")
	}
}

// TestTestModel_Non200Response tests that TestModel handles non-200 responses from the provider.
func TestTestModel_Non200Response(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	// Create a mock server that returns 429
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/chat/completions" && r.Method == http.MethodPost {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error": "rate limited"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer mockServer.Close()

	// Create a provider
	providerData := fmt.Sprintf(`{"name": "test-provider-%s", "base_url": "%s", "api_key": "test-key"}`, uuid.New().String()[:8], mockServer.URL)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d: %s", rec.Code, rec.Body.String())
	}

	var providerResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &providerResp); err != nil {
		t.Fatalf("Failed to parse provider response: %v", err)
	}

	// Insert an enabled model
	modelID := uuid.New().String()
	pool := h.Pool().Pool()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO models (id, provider_id, model_id, name, enabled) VALUES ($1, $2, $3, $4, $5)`,
		modelID, providerResp.ID, "test-model", "Test Model", true)
	if err != nil {
		t.Fatalf("Failed to insert model: %v", err)
	}

	// Test the model
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/models/"+modelID+"/test", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var testResp struct {
		Success bool   `json:"success"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &testResp); err != nil {
		t.Fatalf("Failed to parse test response: %v", err)
	}

	if testResp.Error == "" {
		t.Errorf("Expected non-empty error field, got empty string")
	}
	if !strings.Contains(testResp.Error, "429") {
		t.Errorf("Expected error to contain '429', got: %s", testResp.Error)
	}
}

// TestTestModel_ConnectionError tests that TestModel handles connection errors gracefully.
func TestTestModel_ConnectionError(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	// Create a provider with an unreachable URL (localhost port 1 should refuse immediately)
	providerData := fmt.Sprintf(`{"name": "test-provider-%s", "base_url": "http://127.0.0.1:1", "api_key": "test-key"}`, uuid.New().String()[:8])
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d: %s", rec.Code, rec.Body.String())
	}

	var providerResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &providerResp); err != nil {
		t.Fatalf("Failed to parse provider response: %v", err)
	}

	// Insert a model for this provider
	modelID := uuid.New().String()
	pool := h.Pool().Pool()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO models (id, provider_id, model_id, name, enabled) VALUES ($1, $2, $3, $4, $5)`,
		modelID, providerResp.ID, "test-model", "Test Model", true)
	if err != nil {
		t.Fatalf("Failed to insert model: %v", err)
	}

	// Try to test the model - should fail with connection error but return 200 with error in body
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/models/"+modelID+"/test", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 with error in body, got %d: %s", rec.Code, rec.Body.String())
	}

	var testResp struct {
		Success bool   `json:"success"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &testResp); err != nil {
		t.Fatalf("Failed to parse test response: %v", err)
	}

	if testResp.Error == "" {
		t.Errorf("Expected non-empty error field for unreachable provider, got empty string")
	}
}
