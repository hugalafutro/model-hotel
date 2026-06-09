package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/model"
)

func TestDiscoverDeepSeek(t *testing.T) {
	// Create test server with mock DeepSeek response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" || r.Method != "GET" {
			http.NotFound(w, r)
			return
		}

		// Check authorization header
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-api-key" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// Mock DeepSeek models response
		response := OpenAIModelsResponse{
			Object: "list",
			Data: []OpenAIModel{
				{
					ID:      "deepseek-chat",
					Object:  "model",
					Created: 1234567890,
					OwnedBy: "deepseek",
				},
				{
					ID:      "deepseek-coder",
					Object:  "model",
					Created: 1234567890,
					OwnedBy: "deepseek",
				},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	// Create discovery service with test client
	service := &DiscoveryService{
		httpClient: server.Client(),
	}

	// Create test provider with test server URL
	provider := &Provider{
		ID:      uuid.New(),
		BaseURL: server.URL + "/v1", // DeepSeek uses /v1/models
	}

	// Test discovery
	models, err := service.discoverDeepSeek(context.Background(), provider, "test-api-key")
	if err != nil {
		t.Fatalf("discoverDeepSeek failed: %v", err)
	}

	// Verify results
	if len(models) != 2 {
		t.Errorf("Expected 2 models, got %d", len(models))
	}

	// Check first model
	if models[0].ModelID != "deepseek-chat" {
		t.Errorf("Expected model ID 'deepseek-chat', got '%s'", models[0].ModelID)
	}

	if models[0].OwnedBy != "deepseek" {
		t.Errorf("Expected ownedBy 'deepseek', got '%s'", models[0].OwnedBy)
	}

	if !models[0].Enabled {
		t.Error("Expected model to be enabled")
	}

	// Check second model
	if models[1].ModelID != "deepseek-coder" {
		t.Errorf("Expected model ID 'deepseek-coder', got '%s'", models[1].ModelID)
	}
}

func TestDiscoverDeepSeek_Unauthorized(t *testing.T) {
	// Create test server that returns unauthorized
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
	}))
	defer server.Close()

	service := &DiscoveryService{
		httpClient: server.Client(),
	}

	provider := &Provider{
		ID:      uuid.New(),
		BaseURL: server.URL,
	}

	_, err := service.discoverDeepSeek(context.Background(), provider, "wrong-api-key")
	if err == nil {
		t.Error("Expected error for unauthorized request, got nil")
	}
}

func TestDiscoverDeepSeek_InvalidResponse(t *testing.T) {
	// Create test server with invalid JSON response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("{ invalid json "))
	}))
	defer server.Close()

	service := &DiscoveryService{
		httpClient: server.Client(),
	}

	provider := &Provider{
		ID:      uuid.New(),
		BaseURL: server.URL,
	}

	_, err := service.discoverDeepSeek(context.Background(), provider, "test-api-key")
	if err == nil {
		t.Error("Expected error for invalid JSON, got nil")
	}
}

func TestDiscoverDeepSeek_EmptyResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := OpenAIModelsResponse{
			Object: "list",
			Data:   []OpenAIModel{},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	service := &DiscoveryService{
		httpClient: server.Client(),
	}

	provider := &Provider{
		ID:      uuid.New(),
		BaseURL: server.URL,
	}

	models, err := service.discoverDeepSeek(context.Background(), provider, "test-api-key")
	if err != nil {
		t.Fatalf("discoverDeepSeek failed: %v", err)
	}
	if len(models) != 0 {
		t.Errorf("Expected 0 models for empty response, got %d", len(models))
	}
}

func TestDiscoverDeepSeek_CatalogOverride(t *testing.T) {
	// Test that a known catalog model (deepseek-chat) gets catalog pricing/limits
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" || r.Method != "GET" {
			http.NotFound(w, r)
			return
		}
		response := OpenAIModelsResponse{
			Object: "list",
			Data: []OpenAIModel{
				{
					ID:      "deepseek-chat",
					Object:  "model",
					Created: 1234567890,
					OwnedBy: "deepseek",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	service := &DiscoveryService{
		httpClient: server.Client(),
	}

	provider := &Provider{
		ID:      uuid.New(),
		BaseURL: server.URL,
	}

	models, err := service.discoverDeepSeek(context.Background(), provider, "test-api-key")
	if err != nil {
		t.Fatalf("discoverDeepSeek failed: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("Expected 1 model, got %d", len(models))
	}
	// deepseek-chat should have catalog values for context length and pricing
	m := models[0]
	if m.ContextLength == nil {
		t.Error("Expected ContextLength to be set from catalog for deepseek-chat")
	}
	if m.MaxOutputTokens == nil {
		t.Error("Expected MaxOutputTokens to be set from catalog for deepseek-chat")
	}
	if m.InputPricePerMillion == nil {
		t.Error("Expected InputPricePerMillion to be set from catalog for deepseek-chat")
	}
	if m.OutputPricePerMillion == nil {
		t.Error("Expected OutputPricePerMillion to be set from catalog for deepseek-chat")
	}
	// Catalog should provide InputPricePerMillionCacheHit
	if m.InputPricePerMillionCacheHit == nil {
		t.Error("Expected InputPricePerMillionCacheHit to be set from catalog for deepseek-chat")
	}
}

func TestDiscoverDeepSeek_UnknownModel_DefaultValues(t *testing.T) {
	// Test that a model NOT in the catalog gets safe defaults
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := OpenAIModelsResponse{
			Object: "list",
			Data: []OpenAIModel{
				{
					ID:      "deepseek-unknown-future-model",
					Object:  "model",
					Created: 1234567890,
					OwnedBy: "deepseek",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	service := &DiscoveryService{
		httpClient: server.Client(),
	}

	provider := &Provider{
		ID:      uuid.New(),
		BaseURL: server.URL,
	}

	models, err := service.discoverDeepSeek(context.Background(), provider, "test-api-key")
	if err != nil {
		t.Fatalf("discoverDeepSeek failed: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("Expected 1 model, got %d", len(models))
	}
	// Unknown model should get default values
	m := models[0]
	if m.ContextLength == nil || *m.ContextLength != 128000 {
		t.Errorf("Expected default ContextLength 128000, got %v", m.ContextLength)
	}
	if m.MaxOutputTokens == nil || *m.MaxOutputTokens != 8192 {
		t.Errorf("Expected default MaxOutputTokens 8192, got %v", m.MaxOutputTokens)
	}
}

func TestDiscoverDeepSeek_ConnectionError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	server.Close()

	service := &DiscoveryService{
		httpClient: server.Client(),
	}

	provider := &Provider{
		ID:      uuid.New(),
		BaseURL: server.URL,
	}

	_, err := service.discoverDeepSeek(context.Background(), provider, "test-api-key")
	if err == nil {
		t.Error("Expected error for connection failure, got nil")
	}
}

func TestDiscoverDeepSeek_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}))
	defer server.Close()

	service := &DiscoveryService{
		httpClient: server.Client(),
	}

	provider := &Provider{
		ID:      uuid.New(),
		BaseURL: server.URL,
	}

	_, err := service.discoverDeepSeek(context.Background(), provider, "test-api-key")
	if err == nil {
		t.Error("Expected error for 500 response, got nil")
	}
}

func TestDiscoverDeepSeek_CapabilitiesSet(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := OpenAIModelsResponse{
			Object: "list",
			Data: []OpenAIModel{
				{
					ID:      "deepseek-chat",
					Object:  "model",
					Created: 1234567890,
					OwnedBy: "deepseek",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	service := &DiscoveryService{
		httpClient: server.Client(),
	}

	provider := &Provider{
		ID:      uuid.New(),
		BaseURL: server.URL,
	}

	models, err := service.discoverDeepSeek(context.Background(), provider, "test-api-key")
	if err != nil {
		t.Fatalf("discoverDeepSeek failed: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("Expected 1 model, got %d", len(models))
	}

	var caps model.Capability
	if err := json.Unmarshal([]byte(models[0].Capabilities), &caps); err != nil {
		t.Fatalf("Failed to unmarshal capabilities: %v", err)
	}
	if !caps.Streaming {
		t.Error("Expected Streaming capability to be true")
	}
	if !caps.ToolCalling {
		t.Error("Expected ToolCalling capability to be true")
	}
}

func TestDiscoverDeepSeek_CatalogReasoning(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := OpenAIModelsResponse{
			Object: "list",
			Data: []OpenAIModel{
				{
					ID:      "deepseek-v4-pro",
					Object:  "model",
					Created: 1234567890,
					OwnedBy: "deepseek",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	service := &DiscoveryService{
		httpClient: server.Client(),
	}

	provider := &Provider{
		ID:      uuid.New(),
		BaseURL: server.URL,
	}

	models, err := service.discoverDeepSeek(context.Background(), provider, "test-api-key")
	if err != nil {
		t.Fatalf("discoverDeepSeek failed: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("Expected 1 model, got %d", len(models))
	}

	var caps model.Capability
	if err := json.Unmarshal([]byte(models[0].Capabilities), &caps); err != nil {
		t.Fatalf("Failed to unmarshal capabilities: %v", err)
	}
	if !caps.Reasoning {
		t.Error("Expected Reasoning capability to be true for deepseek-v4-pro from catalog")
	}
}
