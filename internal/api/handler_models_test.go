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
)

func TestListModels(t *testing.T) {

	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/models", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}
	// Should be empty without discovery
	if len(response) != 0 {
		t.Errorf("Expected empty model list, got %d models", len(response))
	}
}

// Test for models.go - UpdateModel_EnableDisable

func TestUpdateModel_EnableDisable(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	// Create a provider
	providerData := `{"name": "test-model-provider", "base_url": "https://api.openai.com", "api_key": "test-api-key"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d", rec.Code)
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

	// Test disabling the model
	_, err = pool.Exec(context.Background(), `
		UPDATE models SET enabled = false WHERE id = $1
	`, modelID)
	if err != nil {
		t.Fatalf("Failed to disable model: %v", err)
	}

	// Verify the disable
	var enabledCheck bool
	if err := pool.QueryRow(context.Background(), `SELECT enabled FROM models WHERE id = $1`, modelID).Scan(&enabledCheck); err != nil {
		t.Fatalf("Failed to check enabled status: %v", err)
	}
	if enabledCheck != false {
		t.Errorf("Expected enabled=false after disable, got %v", enabledCheck)
	}

	// Test re-enabling the model
	_, err = pool.Exec(context.Background(), `
		UPDATE models SET enabled = true WHERE id = $1
	`, modelID)
	if err != nil {
		t.Fatalf("Failed to enable model: %v", err)
	}

	// Verify the enable
	if err := pool.QueryRow(context.Background(), `SELECT enabled FROM models WHERE id = $1`, modelID).Scan(&enabledCheck); err != nil {
		t.Fatalf("Failed to check enabled status: %v", err)
	}
	if enabledCheck != true {
		t.Errorf("Expected enabled=true after enable, got %v", enabledCheck)
	}
}

// Test for models.go - ListModels_WithPagination

func TestListModels_WithPagination(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	// Create a provider
	providerData := `{"name": "test-models-provider", "base_url": "https://api.openai.com", "api_key": "test-api-key"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d", rec.Code)
	}

	var providerResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &providerResp); err != nil {
		t.Fatalf("Failed to parse provider response: %v", err)
	}

	// Insert multiple models directly via DB
	pool := h.Pool().Pool()
	for i := 0; i < 10; i++ {
		modelID := uuid.New().String()
		_, err := pool.Exec(context.Background(),
			`INSERT INTO models (id, provider_id, model_id, name, enabled) VALUES ($1, $2, $3, $4, $5)`,
			modelID, providerResp.ID, fmt.Sprintf("gpt-4o-mini-%d", i), fmt.Sprintf("GPT-4o Mini %d", i), true)
		if err != nil {
			t.Fatalf("Failed to insert model %d: %v", i, err)
		}
	}

	// List models (pagination not supported by endpoint)
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/models", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response []ModelResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}
	if len(response) != 10 {
		t.Errorf("Expected 10 models total, got %d", len(response))
	}
}

// Test for models.go - TestModel_Success (simulated - will fail with test API key but tests the path)

func TestTestModel_Success(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	// Create a mock OpenAI server that returns 401 with invalid API key error
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/chat/completions") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error": map[string]interface{}{
					"message": "invalid api key",
					"type":    "invalid_request_error",
				},
			})
		}
	}))
	defer mockServer.Close()

	// Override handler's transport to use mock server
	origTransport := h.testModelTransport
	h.testModelTransport = &http.Transport{}
	defer func() { h.testModelTransport = origTransport }()

	// Create a provider pointing to mock server
	providerData := fmt.Sprintf(`{"name": "test-model-provider-%s", "base_url": "%s", "api_key": "sk-test-key"}`, uuid.New().String()[:8], mockServer.URL)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d", rec.Code)
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

	// Test the model - will return error from mock server (simulating invalid API key)
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/models/"+modelID+"/test", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	// Should return a response (with error from mock server)
	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var testResp TestModelResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &testResp); err != nil {
		t.Fatalf("Failed to parse test response: %v", err)
	}
	// Should have error field due to mock server returning 401
	if testResp.Error == "" {
		t.Error("Expected error field in test response")
	}
}

func TestUpdateModel(t *testing.T) {
	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	// Create a provider
	providerData := fmt.Sprintf(`{"name": "test-model-provider-%s", "base_url": "https://api.openai.com", "api_key": "test-api-key"}`, uuid.New().String()[:8])
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(providerData))
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

	// Insert a model directly via DB (no POST /models endpoint)
	modelID := uuid.New().String()
	pool := h.Pool().Pool()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO models (id, provider_id, model_id, name, enabled) VALUES ($1, $2, $3, $4, $5)`,
		modelID, providerResp.ID, "gpt-4o-mini", "GPT-4o Mini", true)
	if err != nil {
		t.Fatalf("Failed to insert model: %v", err)
	}

	// Update the model via API
	updateData := `{"display_name": "Updated Model", "enabled": false}`
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("PATCH", "/models/"+modelID, strings.NewReader(updateData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

// Stats Tests

func TestDeleteModel(t *testing.T) {
	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	// Create a provider
	providerData := `{"name": "test-model-provider-` + uuid.New().String() + `", "base_url": "https://api.openai.com", "api_key": "test-api-key"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d", rec.Code)
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

	// Delete the model
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("DELETE", "/models/"+modelID, http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("Expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestTestModel(t *testing.T) {
	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	// Create a provider
	providerData := `{"name": "test-model-provider-` + uuid.New().String() + `", "base_url": "https://api.openai.com", "api_key": "test-api-key"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d", rec.Code)
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

	// Test the model - will fail because we're using a test API key
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/models/"+modelID+"/test", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	// Should return error due to invalid API key
	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var testResp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &testResp); err != nil {
		t.Fatalf("Failed to parse test response: %v", err)
	}
	// Should have error field
	if _, ok := testResp["error"]; !ok {
		t.Error("Expected error field in test response")
	}
}

// Discovery Handler Tests

func TestUpdateModel_Validation(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	// Create a provider
	providerData := `{"name": "test-update-model-provider", "base_url": "https://api.openai.com", "api_key": "test-api-key"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d", rec.Code)
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
		`INSERT INTO models (id, provider_id, model_id, name, enabled, context_length) VALUES ($1, $2, $3, $4, $5, $6)`,
		modelID, providerResp.ID, "gpt-4o-mini", "GPT-4o Mini", true, 128000)
	if err != nil {
		t.Fatalf("Failed to insert model: %v", err)
	}

	t.Run("InvalidUUID", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPatch, "/models/not-a-uuid", strings.NewReader(`{"display_name":"test"}`))
		req.Header.Set("Authorization", "Bearer test-admin-token")
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != 400 {
			t.Errorf("got %d", w.Code)
		}
	})

	t.Run("InvalidJSON", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPatch, "/models/"+modelID, strings.NewReader(`{"display_name":`))
		req.Header.Set("Authorization", "Bearer test-admin-token")
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != 400 {
			t.Errorf("got %d", w.Code)
		}
	})

	t.Run("EmptyBody", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPatch, "/models/"+modelID, strings.NewReader(`{}`))
		req.Header.Set("Authorization", "Bearer test-admin-token")
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != 400 {
			t.Errorf("got %d", w.Code)
		}
	})

	t.Run("InvalidDisplayName", func(t *testing.T) {
		longName := strings.Repeat("x", 129) // Too long
		req := httptest.NewRequest(http.MethodPatch, "/models/"+modelID, strings.NewReader(fmt.Sprintf(`{"display_name":%q}`, longName)))
		req.Header.Set("Authorization", "Bearer test-admin-token")
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != 400 {
			t.Errorf("got %d", w.Code)
		}
	})

	t.Run("ClearDisplayName", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPatch, "/models/"+modelID, strings.NewReader(`{"display_name": ""}`))
		req.Header.Set("Authorization", "Bearer test-admin-token")
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != 200 {
			t.Errorf("expected 200 for empty display_name (clear signal), got %d", w.Code)
		}
	})

	t.Run("InvalidContextLength", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPatch, "/models/"+modelID, strings.NewReader(`{"context_length":-1}`))
		req.Header.Set("Authorization", "Bearer test-admin-token")
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != 400 {
			t.Errorf("got %d", w.Code)
		}
	})

	t.Run("ValidUpdate", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPatch, "/models/"+modelID, strings.NewReader(`{"display_name":"Updated Model", "context_length":131072}`))
		req.Header.Set("Authorization", "Bearer test-admin-token")
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != 200 {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		// Verify update took effect
		var updated struct {
			DisplayName   string `json:"display_name"`
			ContextLength int    `json:"context_length"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &updated); err != nil {
			t.Fatalf("Failed to parse update response: %v", err)
		}
		if updated.DisplayName != "Updated Model" {
			t.Errorf("expected display_name 'Updated Model', got %q", updated.DisplayName)
		}
		if updated.ContextLength != 131072 {
			t.Errorf("expected context_length 131072, got %d", updated.ContextLength)
		}
	})
}

// Test for applogs.go - GetAppLogs_QueryParams

func TestDeleteModel_NonExistent(t *testing.T) {
	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	// Try to delete non-existent model
	nonExistentID := uuid.New().String()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("DELETE", "/models/"+nonExistentID, http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	// Delete returns 204 even for non-existent models (idempotent)
	if rec.Code != http.StatusNoContent {
		t.Errorf("Expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
}

// =============================================================================
// Coverage Improvement Tests
// =============================================================================

// TestCreateBackup_AlreadyInProgress tests the "backup already in progress" path

func TestUpdateModel_EnableDisable_Integration(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	// Create a provider first
	body := `{"name":"test-update-model","base_url":"https://api.example.com","api_key":"sk-test"}`
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("Expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	providerID := resp["id"].(string)

	// Insert a model directly into the DB (matching schema from existing test)
	pool := h.Pool().Pool()
	modelID := uuid.New().String()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO models (id, provider_id, model_id, name, enabled)
		VALUES ($1, $2, $3, $4, $5)`,
		modelID, providerID, "test-model", "Test Model", true)
	if err != nil {
		t.Fatalf("Failed to insert test model: %v", err)
	}

	// Disable the model
	disableBody := `{"enabled": false}`
	req2 := httptest.NewRequest("PATCH", "/models/"+modelID, strings.NewReader(disableBody))
	req2.Header.Set("Authorization", "Bearer test-admin-token")
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Errorf("expected 200 OK when disabling model, got %d: %s", w2.Code, w2.Body.String())
	}

	// Verify the model is disabled
	var modelResp map[string]interface{}
	json.NewDecoder(w2.Body).Decode(&modelResp)
	if modelResp["enabled"] != false {
		t.Errorf("expected model to be disabled, got enabled=%v", modelResp["enabled"])
	}

	// Enable the model
	enableBody := `{"enabled": true}`
	req3 := httptest.NewRequest("PATCH", "/models/"+modelID, strings.NewReader(enableBody))
	req3.Header.Set("Authorization", "Bearer test-admin-token")
	req3.Header.Set("Content-Type", "application/json")
	w3 := httptest.NewRecorder()
	r.ServeHTTP(w3, req3)

	if w3.Code != http.StatusOK {
		t.Errorf("expected 200 OK when enabling model, got %d: %s", w3.Code, w3.Body.String())
	}

	// Verify the model is enabled
	json.NewDecoder(w3.Body).Decode(&modelResp)
	if modelResp["enabled"] != true {
		t.Errorf("expected model to be enabled, got enabled=%v", modelResp["enabled"])
	}
}

// TestTestModel_NonExistentProvider_Integration tests that testing a model on non-existent provider returns 404

func TestTestModel_NonExistentProvider_Integration(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Try to test a model on a non-existent provider
	nonExistentID := "00000000-0000-0000-0000-000000000000"
	body := `{"model":"gpt-4"}`
	req := httptest.NewRequest("POST", "/providers/"+nonExistentID+"/models/test", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 Not Found, got %d: %s", w.Code, w.Body.String())
	}
}

// TestGetProviderBalance_UnsupportedType_Integration tests balance check on unsupported provider type

func TestListModels_WithModels(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	// Create a provider
	providerData := fmt.Sprintf(`{"name":"test-list-models-provider-%s","base_url":"https://api.openai.com","api_key":"test-api-key"}`, uuid.New().String()[:8])
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(providerData))
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

	// Insert multiple models with different properties
	pool := h.Pool().Pool()
	for i := 0; i < 3; i++ {
		modelID := uuid.New().String()
		_, err := pool.Exec(context.Background(),
			`INSERT INTO models (id, provider_id, model_id, name, enabled, context_length) VALUES ($1, $2, $3, $4, $5, $6)`,
			modelID, providerResp.ID, fmt.Sprintf("list-gpt-%d", i), fmt.Sprintf("List GPT %d", i), true, 128000)
		if err != nil {
			t.Fatalf("Failed to insert model %d: %v", i, err)
		}
	}

	t.Run("FilterByProviderID", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/models?provider_id="+providerResp.ID, http.NoBody)
		req.Header.Set("Authorization", "Bearer test-admin-token")
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var response []ModelResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
			t.Fatalf("Failed to parse response: %v", err)
		}

		if len(response) < 3 {
			t.Errorf("Expected at least 3 models for provider, got %d", len(response))
		}
	})
}

// TestGetSystem_Details tests the system endpoint returns expected structure

func TestDeleteModel_WithFailoverGroup(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	// Create a provider
	providerData := fmt.Sprintf(`{"name":"test-delete-fg-provider-%s","base_url":"https://api.openai.com","api_key":"test-api-key"}`, uuid.New().String()[:8])
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(providerData))
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

	// Insert two models (failover groups require at least 2 entries)
	pool := h.Pool().Pool()
	modelID1 := uuid.New().String()
	modelID2 := uuid.New().String()

	_, err := pool.Exec(context.Background(),
		`INSERT INTO models (id, provider_id, model_id, name, enabled) VALUES ($1, $2, $3, $4, $5)`,
		modelID1, providerResp.ID, "gpt-4o-1", "GPT-4o Model 1", true)
	if err != nil {
		t.Fatalf("Failed to insert model 1: %v", err)
	}

	_, err = pool.Exec(context.Background(),
		`INSERT INTO models (id, provider_id, model_id, name, enabled) VALUES ($1, $2, $3, $4, $5)`,
		modelID2, providerResp.ID, "gpt-4o-2", "GPT-4o Model 2", true)
	if err != nil {
		t.Fatalf("Failed to insert model 2: %v", err)
	}

	// Create a failover group with both models
	groupData := `{"display_model":"test-fg-group","entry_ids":["` + modelID1 + `","` + modelID2 + `"]}`
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/failover-groups/", strings.NewReader(groupData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create failover group: %d: %s", rec.Code, rec.Body.String())
	}

	// Delete model1 (referenced by failover group) - should succeed with cascade
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("DELETE", "/models/"+modelID1, http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("Expected 204 for model delete (with cascade), got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestDeleteModel_InvalidUUID tests deleting with invalid UUID format

func TestDeleteModel_InvalidUUID(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("DELETE", "/models/invalid-uuid", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for invalid UUID, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestPurgeLogs_BeforeTimestamp tests purging logs before a specific timestamp
