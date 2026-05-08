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

func TestDiscoverXAILanguageModels(t *testing.T) {
	// Create test server with mock XAI language models response
	languageServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/language-models" || r.Method != "GET" {
			http.NotFound(w, r)
			return
		}

		// Check authorization header
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-api-key" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// Mock XAI language models response
		response := XAILanguageModelsResponse{
			Models: []XAILanguageModel{
				{
					ID:                         "grogu-1",
					Object:                     "model",
					OwnedBy:                    "xai",
					Version:                    "1.0",
					InputModalities:            []string{"text"},
					OutputModalities:           []string{"text"},
					PromptTextTokenPrice:       50,  // cents per 100M tokens
					CachedPromptTextTokenPrice: 25,  // cents per 100M tokens
					CompletionTextTokenPrice:   150, // cents per 100M tokens
				},
				{
					ID:                         "grogu-2",
					Object:                     "model",
					OwnedBy:                    "xai",
					Version:                    "2.0",
					InputModalities:            []string{"text", "image"},
					OutputModalities:           []string{"text"},
					PromptTextTokenPrice:       100, // cents per 100M tokens
					CachedPromptTextTokenPrice: 50,  // cents per 100M tokens
					CompletionTextTokenPrice:   300, // cents per 100M tokens
				},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer languageServer.Close()

	// Create discovery service with test client
	service := &DiscoveryService{
		httpClient: languageServer.Client(),
	}

	// Create test provider
	provider := &Provider{
		ID:      uuid.New(),
		BaseURL: languageServer.URL,
	}

	// Test language models discovery
	models, err := service.discoverXAILanguageModels(context.Background(), provider, "test-api-key", languageServer.URL)
	if err != nil {
		t.Fatalf("discoverXAILanguageModels failed: %v", err)
	}

	// Verify results
	if len(models) != 2 {
		t.Errorf("Expected 2 models, got %d", len(models))
	}

	// Check first model
	if models[0].ModelID != "grogu-1" {
		t.Errorf("Expected model ID 'grogu-1', got '%s'", models[0].ModelID)
	}

	if models[0].OwnedBy != "xai" {
		t.Errorf("Expected ownedBy 'xai', got '%s'", models[0].OwnedBy)
	}

	// Check pricing conversion: cents per 100M -> dollars per 1M
	// 50 cents per 100M = $0.50 per 1M = $0.50
	if *models[0].InputPricePerMillion != 0.50 {
		t.Errorf("Expected input price 0.50, got %f", *models[0].InputPricePerMillion)
	}

	// 25 cents per 100M = $0.25 per 1M = $0.25
	if models[0].InputPricePerMillionCacheHit == nil || *models[0].InputPricePerMillionCacheHit != 0.25 {
		t.Errorf("Expected cache input price 0.25, got %v", models[0].InputPricePerMillionCacheHit)
	}

	// 150 cents per 100M = $1.50 per 1M = $1.50
	if *models[0].OutputPricePerMillion != 1.50 {
		t.Errorf("Expected output price 1.50, got %f", *models[0].OutputPricePerMillion)
	}

	// Check capabilities - should have streaming, tool calling, structured output
	var caps model.Capability
	if err := json.Unmarshal([]byte(models[0].Capabilities), &caps); err != nil {
		t.Errorf("Failed to unmarshal capabilities: %v", err)
	} else {
		if !caps.Streaming {
			t.Error("Expected streaming capability to be true")
		}
		if !caps.ToolCalling {
			t.Error("Expected tool calling capability to be true")
		}
		if !caps.StructuredOutput {
			t.Error("Expected structured output capability to be true")
		}
		if caps.Vision {
			t.Error("Expected vision capability to be false for text-only model")
		}
	}

	// Check second model - should have vision capability
	if models[1].ModelID != "grogu-2" {
		t.Errorf("Expected model ID 'grogu-2', got '%s'", models[1].ModelID)
	}

	if err := json.Unmarshal([]byte(models[1].Capabilities), &caps); err != nil {
		t.Errorf("Failed to unmarshal capabilities for grogu-2: %v", err)
	} else {
		if !caps.Vision {
			t.Error("Expected vision capability to be true for multimodal model")
		}
	}
}

func TestDiscoverXAIMinimalModels(t *testing.T) {
	// Create test server with mock XAI minimal models response
	minimalServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" || r.Method != "GET" {
			http.NotFound(w, r)
			return
		}

		// Check authorization header
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-api-key" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// Mock XAI minimal models response (OpenAI-compatible format)
		response := XAIModelsResponse{
			Object: "list",
			Data: []XAIModel{
				{
					ID:      "grogu-minimal",
					Object:  "model",
					OwnedBy: "xai",
				},
				{
					ID:      "grogu-tiny",
					Object:  "model",
					OwnedBy: "xai",
				},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer minimalServer.Close()

	// Create discovery service with test client
	service := &DiscoveryService{
		httpClient: minimalServer.Client(),
	}

	// Create test provider
	provider := &Provider{
		ID:      uuid.New(),
		BaseURL: minimalServer.URL,
	}

	// Test minimal models discovery
	models, err := service.discoverXAIMinimalModels(context.Background(), provider, "test-api-key", minimalServer.URL)
	if err != nil {
		t.Fatalf("discoverXAIMinimalModels failed: %v", err)
	}

	// Verify results - should have 2 minimal models
	if len(models) != 2 {
		t.Errorf("Expected 2 models, got %d", len(models))
	}

	// Check first model
	if models[0].ModelID != "grogu-minimal" {
		t.Errorf("Expected model ID 'grogu-minimal', got '%s'", models[0].ModelID)
	}

	if models[0].OwnedBy != "xai" {
		t.Errorf("Expected ownedBy 'xai', got '%s'", models[0].OwnedBy)
	}

	// Check capabilities - should have streaming by default
	var caps model.Capability
	if err := json.Unmarshal([]byte(models[0].Capabilities), &caps); err != nil {
		t.Errorf("Failed to unmarshal capabilities: %v", err)
	} else {
		if !caps.Streaming {
			t.Error("Expected streaming capability to be true")
		}
	}
}

func TestDiscoverXAIFromCatalog(t *testing.T) {
	// Create discovery service
	service := &DiscoveryService{
		httpClient: &http.Client{},
	}

	// Create test provider
	provider := &Provider{
		ID: uuid.New(),
	}

	// Test catalog discovery
	models := service.discoverXAIFromCatalog(provider)

	// Should return some models from the catalog
	if len(models) == 0 {
		t.Error("Expected at least one model from catalog, got 0")
	}

	// Check that all models have basic properties
	for _, m := range models {
		if m.ProviderID != provider.ID {
			t.Errorf("Expected provider ID to match, got %s vs %s", m.ProviderID, provider.ID)
		}
		if m.OwnedBy != "xai" {
			t.Errorf("Expected ownedBy 'xai', got '%s'", m.OwnedBy)
		}
		if !m.Enabled {
			t.Error("Expected model to be enabled")
		}
	}
}

func TestDiscoverXAI_FallbackLogic(t *testing.T) {
	// Create test server that returns 403 Forbidden for language models
	forbiddenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/language-models" {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		if r.URL.Path == "/models" {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		http.NotFound(w, r)
	}))
	defer forbiddenServer.Close()

	// Create discovery service with test client
	service := &DiscoveryService{
		httpClient: forbiddenServer.Client(),
	}

	// Create test provider
	provider := &Provider{
		ID:      uuid.New(),
		BaseURL: forbiddenServer.URL,
	}

	// Test discovery - should fall back to catalog when both endpoints return 403
	models, err := service.discoverXAI(context.Background(), provider, "test-api-key")
	if err != nil {
		t.Fatalf("discoverXAI failed: %v", err)
	}

	// Should return catalog models when API access is forbidden
	if len(models) == 0 {
		t.Error("Expected catalog models when API returns 403, got 0")
	}
}

func TestDiscoverXAILanguageModels_Unauthorized(t *testing.T) {
	// Create test server that returns unauthorized
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

	_, err := service.discoverXAILanguageModels(context.Background(), provider, "wrong-api-key", server.URL)
	if err == nil {
		t.Error("Expected error for unauthorized request, got nil")
	}
}

func TestDiscoverXAILanguageModels_InvalidResponse(t *testing.T) {
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

	_, err := service.discoverXAILanguageModels(context.Background(), provider, "test-api-key", server.URL)
	if err == nil {
		t.Error("Expected error for invalid JSON, got nil")
	}
}
