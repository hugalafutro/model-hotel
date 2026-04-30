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
	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/provider"
)

// ---------------------------------------------------------------------------
// ListModels integration tests (requires PostgreSQL)
// ---------------------------------------------------------------------------

func TestListModels_EmptyDB(t *testing.T) {
	h := newIntegrationHandler()
	if h == nil {
		t.Skip("database not available")
	}

	// ListModels returns all enabled models; with no specific test data
	// we just verify the endpoint works and returns valid JSON.
	req := httptest.NewRequest("GET", "/models", nil)
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
	if h == nil {
		t.Skip("database not available")
	}

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
		ID:             modelID,
		ProviderID:     prov.ID,
		ModelID:        "gpt-test-model",
		Name:           "GPT Test Model",
		DisplayName:    "GPT Test Display",
		Description:    "A test model for ListModels",
		Capabilities:   "{}",
		Params:         "{}",
		Modality:       "text",
		InputModalities:  "[]",
		OutputModalities: "[]",
		Enabled:        true,
		CreatedAt:      time.Now(),
		LastSeenAt:     time.Now(),
	}
	if err := h.modelRepo.Upsert(ctx, m); err != nil {
		t.Fatalf("failed to upsert model: %v", err)
	}

	// Now call ListModels
	req := httptest.NewRequest("GET", "/models", nil)
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

func TestListModels_ResponseFormat(t *testing.T) {
	h := newIntegrationHandler()
	if h == nil {
		t.Skip("database not available")
	}

	req := httptest.NewRequest("GET", "/models", nil)
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
