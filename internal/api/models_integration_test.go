package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hugalafutro/model-hotel/internal/admin"
	"github.com/hugalafutro/model-hotel/internal/config"
	"github.com/hugalafutro/model-hotel/internal/db"
	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/provider"
	"github.com/hugalafutro/model-hotel/internal/settings"
	"github.com/hugalafutro/model-hotel/internal/virtualkey"
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

// TestDeleteModel_NotFound tests that DeleteModel returns 204 for
// a non-existent model (idempotent delete).
func TestDeleteModel_NotFound(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	nonExistentID := uuid.New().String()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/models/"+nonExistentID, http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	// SQL DELETE on non-existent rows returns no error, so handler returns 204
	if rec.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d", rec.Code)
	}
}

// TestDeleteModel_DBError tests that DeleteModel returns 500 when
// the database is unavailable during the delete operation.
// Note: SyncForModel and PruneModelUUID error paths (lines 260-265) cannot be
// tested in integration tests because the handler uses context.WithoutCancel
// to ensure these background operations complete even if the request is cancelled.
// In production, DB errors in these paths are logged but don't fail the response.
func TestDeleteModel_DBError(t *testing.T) {
	if apiTestDBURL == "" {
		t.Skip("skipping: test database not available")
	}

	closedPool := newClosedPool(t)
	defer closedPool.Close()

	// We can't easily create a Handler with a closed pool using newTestHandler,
	// so we test the model repository directly instead
	ctx := context.Background()
	modelRepo := model.NewRepository(closedPool)
	err := modelRepo.DeleteByID(ctx, uuid.New())
	if err == nil {
		t.Error("expected error when deleting with closed pool")
	}
}

// TestListModels_CancelledContext tests that ListModels returns 500 when
// the database query fails due to a cancelled context (covers lines 101-104).
func TestListModels_CancelledContext(t *testing.T) {
	if apiTestDBURL == "" {
		t.Skip("skipping: test database not available")
	}

	_, r := newTestHandlerWithRouter(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/models", http.NoBody)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately to cause DB error
	req = req.WithContext(ctx)
	req.Header.Set("Authorization", "Bearer test-admin-token")

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("Expected 500 for cancelled context, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestUpdateModel_CancelledContext tests that UpdateModel returns 500 when
// the database update fails due to a cancelled context (covers lines 164-167).
func TestUpdateModel_CancelledContext(t *testing.T) {
	if apiTestDBURL == "" {
		t.Skip("skipping: test database not available")
	}

	_, r := newTestHandlerWithRouter(t)

	id := uuid.New()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/models/"+id.String(), strings.NewReader(`{"enabled":true}`))
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately to cause DB error
	req = req.WithContext(ctx)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("Expected 500 for cancelled context, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestDeleteModel_CancelledContext tests that DeleteModel returns 500 when
// the database delete fails due to a cancelled context (covers lines 181-184).
func TestDeleteModel_CancelledContext(t *testing.T) {
	if apiTestDBURL == "" {
		t.Skip("skipping: test database not available")
	}

	_, r := newTestHandlerWithRouter(t)

	id := uuid.New()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/models/"+id.String(), http.NoBody)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately to cause DB error
	req = req.WithContext(ctx)
	req.Header.Set("Authorization", "Bearer test-admin-token")

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("Expected 500 for cancelled context, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestDeleteModel_LookupDBError tests that DeleteModel returns 500 when
// the initial model lookup query fails (covers lines 235-246).
func TestDeleteModel_LookupDBError(t *testing.T) {
	if apiTestDBURL == "" {
		t.Skip("skipping: test database not available")
	}

	_, r := newTestHandlerWithRouter(t)

	// Use a valid UUID but cancel context to cause DB error during lookup
	id := uuid.New()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/models/"+id.String(), http.NoBody)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately to cause DB error on lookup query
	req = req.WithContext(ctx)
	req.Header.Set("Authorization", "Bearer test-admin-token")

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("Expected 500 for lookup DB error, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestDeleteModel_WithFailoverSync tests that DeleteModel successfully deletes
// a model and calls SyncForModel and PruneModelUUID (covers lines 254-265).
// The handler logs errors from these calls but still returns 204.
func TestDeleteModel_WithFailoverSync(t *testing.T) {
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

	// Insert a model with a common base name that would trigger failover sync
	// (using "gpt-4o-mini" which is a common model that might have failover groups)
	modelID := uuid.New()
	pool := h.Pool().Pool()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO models (id, provider_id, model_id, name, enabled) VALUES ($1, $2, $3, $4, $5)`,
		modelID, providerResp.ID, "gpt-4o-mini", "GPT-4o Mini", true)
	if err != nil {
		t.Fatalf("Failed to insert model: %v", err)
	}

	// Delete the model - this will trigger SyncForModel and PruneModelUUID
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodDelete, "/models/"+modelID.String(), http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	// Should return 204 even if SyncForModel/PruneModelUUID would fail
	// (they're logged but don't affect response)
	if rec.Code != http.StatusNoContent {
		t.Errorf("Expected 204, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify the model is deleted
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/models", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	var models []ModelResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &models); err != nil {
		t.Fatalf("Failed to parse models response: %v", err)
	}

	for _, m := range models {
		if m.ID == modelID.String() {
			t.Errorf("Deleted model %s still appears in list", modelID)
		}
	}
}

// ---------------------------------------------------------------------------
// Tests moved from coverage_gap3_test.go
// ---------------------------------------------------------------------------

// TestListModels_ValidProviderIDFilter tests ListModels with valid UUID provider_id
// to cover the providerID filter path.
func TestListModels_ValidProviderIDFilter(t *testing.T) {
	if apiTestDBURL == "" {
		t.Skip("skipping: test database not available")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, apiTestDBURL)
	if err != nil {
		t.Skip("skipping: test database not available")
	}
	defer pool.Close()

	// Clean test data
	pool.Exec(ctx, `TRUNCATE models, providers CASCADE`)

	// Create admin manager
	tmpDir := t.TempDir()
	adminMgr, _, err := admin.New(tmpDir, "test-admin-token")
	if err != nil {
		t.Fatalf("failed to create admin manager: %v", err)
	}

	// Create handler
	cfg := &config.Config{
		MasterKey:          "testmasterkey1234567890abcdef",
		AllowHTTPProviders: true,
		DataDir:            tmpDir,
	}
	providerRepo := provider.NewRepository(pool)
	vkRepo := virtualkey.NewRepository(pool)
	settingsRepo := settings.NewRepository(pool)
	dbInst, err := db.New(ctx, apiTestDBURL, 25, 5)
	if err != nil {
		t.Fatalf("failed to create db instance: %v", err)
	}
	defer dbInst.Close()

	h := NewHandler(cfg, providerRepo, dbInst, adminMgr, vkRepo, settingsRepo, "test", nil)
	r := chi.NewRouter()
	r.Use(h.AuthMiddleware)
	h.Register(r)

	// Create two providers
	createBody1 := `{"name":"provider-filter-1","base_url":"https://api.example.com/v1","provider_type":"openai","api_key":"sk-testkey1234567890abcdef"}`
	req := httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(createBody1))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("create provider 1: expected status %d, got %d: %s", http.StatusCreated, w.Code, w.Body.String())
	}

	var created1 struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(w.Body).Decode(&created1); err != nil {
		t.Fatalf("failed to decode created provider: %v", err)
	}

	createBody2 := `{"name":"provider-filter-2","base_url":"https://api.example.com/v2","provider_type":"openai","api_key":"sk-testkey1234567890abcdef"}`
	req = httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(createBody2))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("create provider 2: expected status %d, got %d: %s", http.StatusCreated, w.Code, w.Body.String())
	}

	var created2 struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(w.Body).Decode(&created2); err != nil {
		t.Fatalf("failed to decode created provider: %v", err)
	}

	providerUUID1, _ := uuid.Parse(created1.ID)
	providerUUID2, _ := uuid.Parse(created2.ID)

	// Insert models for each provider
	_, err = pool.Exec(ctx, `
		INSERT INTO models (id, model_id, name, provider_id, enabled, created_at, last_seen_at)
		VALUES ($1, 'model-1', 'Model 1', $2, true, NOW(), NOW()),
		       ($3, 'model-2', 'Model 2', $4, true, NOW(), NOW())`,
		uuid.New(), providerUUID1,
		uuid.New(), providerUUID2)
	if err != nil {
		t.Fatalf("Failed to insert models: %v", err)
	}

	// Request with provider_id filter for provider 1
	req = httptest.NewRequest(http.MethodGet, "/models?provider_id="+created1.ID, http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("list models: expected status %d, got %d: %s", http.StatusOK, w.Code, w.Body.String())
	}

	var models []ModelResponse
	if err := json.NewDecoder(w.Body).Decode(&models); err != nil {
		t.Fatalf("failed to decode models: %v", err)
	}

	// Should only return models for provider 1
	if len(models) != 1 {
		t.Errorf("Expected 1 model, got %d", len(models))
	}
	if len(models) > 0 && models[0].ProviderID != created1.ID {
		t.Errorf("Expected provider_id=%s, got %s", created1.ID, models[0].ProviderID)
	}
}

// TestListModels_RepoError tests ListModels when modelRepo.List returns an error
// (using closed pool) to cover the repository error path.
func TestListModels_RepoError(t *testing.T) {
	if apiTestDBURL == "" {
		t.Skip("skipping: test database not available")
	}

	ctx := context.Background()

	// Create a closed pool to trigger query errors
	closedPool, err := pgxpool.New(ctx, apiTestDBURL)
	if err != nil {
		t.Skip("skipping: test database not available")
	}
	closedPool.Close()

	// Create admin manager
	tmpDir := t.TempDir()
	adminMgr, _, err := admin.New(tmpDir, "test-admin-token")
	if err != nil {
		t.Fatalf("failed to create admin manager: %v", err)
	}

	// Create handler with closed pool
	cfg := &config.Config{
		MasterKey:          "testmasterkey1234567890abcdef",
		AllowHTTPProviders: true,
		DataDir:            tmpDir,
	}

	// Create db.DB with closed pool
	dbInst, err := db.New(ctx, apiTestDBURL, 25, 5)
	if err != nil {
		t.Fatalf("failed to create db instance: %v", err)
	}
	dbInst.Close()

	providerRepo := provider.NewRepository(closedPool)
	vkRepo := virtualkey.NewRepository(closedPool)
	settingsRepo := settings.NewRepository(closedPool)

	h := NewHandler(cfg, providerRepo, dbInst, adminMgr, vkRepo, settingsRepo, "test", nil)
	r := chi.NewRouter()
	r.Use(h.AuthMiddleware)
	h.Register(r)

	// Request should fail with 500
	req := httptest.NewRequest(http.MethodGet, "/models", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("Expected status 500, got %d: %s", w.Code, w.Body.String())
	}
}
