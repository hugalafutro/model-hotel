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

func TestDiscoverOpenRouter(t *testing.T) {
	// Create test server with mock OpenRouter response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

		// Mock OpenRouter models response
		response := OpenRouterModelsResponse{
			Data: []OpenRouterModel{
				{
					ID:            "openai/gpt-4",
					Name:          "GPT-4",
					Description:   "OpenAI's flagship model",
					ContextLength: 8192,
					Architecture: OpenRouterArchitecture{
						Modality:         "chat",
						InputModalities:  []string{"text"},
						OutputModalities: []string{"text"},
						Tokenizer:        "cl100k_base",
					},
					Pricing: OpenRouterPricing{
						Prompt:     "0.01",
						Completion: "0.03",
					},
					TopProvider: OpenRouterTopProvider{
						ContextLength:       8192,
						MaxCompletionTokens: 4096,
						IsModerated:         true,
					},
					SupportedParameters: []string{"tools", "reasoning"},
				},
				{
					ID:            "anthropic/claude-3-haiku",
					Name:          "Claude 3 Haiku",
					Description:   "Anthropic's lightweight model",
					ContextLength: 200000,
					Architecture: OpenRouterArchitecture{
						Modality:         "chat",
						InputModalities:  []string{"text"},
						OutputModalities: []string{"text"},
						Tokenizer:        "claude-2",
					},
					Pricing: OpenRouterPricing{
						Prompt:     "0.0025",
						Completion: "0.0125",
					},
					TopProvider: OpenRouterTopProvider{
						ContextLength:       200000,
						MaxCompletionTokens: 4096,
						IsModerated:         true,
					},
					SupportedParameters: []string{"tools"},
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

	// Create test provider
	provider := &Provider{
		ID:      uuid.New(),
		BaseURL: server.URL,
	}

	// Test discovery
	models, err := service.discoverOpenRouter(context.Background(), provider, "test-api-key")
	if err != nil {
		t.Fatalf("discoverOpenRouter failed: %v", err)
	}

	// Verify results - should have 2 models (both are chat models with text output)
	if len(models) != 2 {
		t.Errorf("Expected 2 models, got %d", len(models))
	}

	// Check first model
	if models[0].ModelID != "openai/gpt-4" {
		t.Errorf("Expected model ID 'openai/gpt-4', got '%s'", models[0].ModelID)
	}

	if models[0].OwnedBy != "openai" {
		t.Errorf("Expected ownedBy 'openai', got '%s'", models[0].OwnedBy)
	}

	if models[0].DisplayName != "GPT-4" {
		t.Errorf("Expected display name 'GPT-4', got '%s'", models[0].DisplayName)
	}

	if *models[0].ContextLength != 8192 {
		t.Errorf("Expected context length 8192, got %d", *models[0].ContextLength)
	}

	if *models[0].MaxOutputTokens != 4096 {
		t.Errorf("Expected max output tokens 4096, got %d", *models[0].MaxOutputTokens)
	}

	if *models[0].InputPricePerMillion != 10000.0 {
		t.Errorf("Expected input price 10000.0, got %f", *models[0].InputPricePerMillion)
	}

	if *models[0].OutputPricePerMillion != 30000.0 {
		t.Errorf("Expected output price 30000.0, got %f", *models[0].OutputPricePerMillion)
	}

	// Check capabilities - should have streaming, tool calling, and reasoning
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
		if !caps.Reasoning {
			t.Error("Expected reasoning capability to be true")
		}
	}

	// Check second model
	if models[1].ModelID != "anthropic/claude-3-haiku" {
		t.Errorf("Expected model ID 'anthropic/claude-3-haiku', got '%s'", models[1].ModelID)
	}

	if models[1].OwnedBy != "anthropic" {
		t.Errorf("Expected ownedBy 'anthropic', got '%s'", models[1].OwnedBy)
	}

	if *models[1].ContextLength != 200000 {
		t.Errorf("Expected context length 200000, got %d", *models[1].ContextLength)
	}
}

func TestDiscoverOpenRouter_Unauthorized(t *testing.T) {
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

	_, err := service.discoverOpenRouter(context.Background(), provider, "wrong-api-key")
	if err == nil {
		t.Error("Expected error for unauthorized request, got nil")
	}
}

func TestDiscoverOpenRouter_InvalidResponse(t *testing.T) {
	// Create test server with invalid JSON response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
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

	_, err := service.discoverOpenRouter(context.Background(), provider, "test-api-key")
	if err == nil {
		t.Error("Expected error for invalid JSON, got nil")
	}
}

func TestDiscoverOpenRouter_SkipsAliasPrefix(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" || r.Method != "GET" {
			http.NotFound(w, r)
			return
		}
		response := OpenRouterModelsResponse{
			Data: []OpenRouterModel{
				{
					ID:            "~anthropic/claude-latest",
					Name:          "Claude Latest Alias",
					ContextLength: 200000,
					Architecture: OpenRouterArchitecture{
						Modality:         "chat",
						InputModalities:  []string{"text"},
						OutputModalities: []string{"text"},
					},
					Pricing: OpenRouterPricing{
						Prompt:     "0",
						Completion: "0",
					},
				},
				{
					ID:            "anthropic/claude-3-opus",
					Name:          "Claude 3 Opus",
					ContextLength: 200000,
					Architecture: OpenRouterArchitecture{
						Modality:         "chat",
						InputModalities:  []string{"text"},
						OutputModalities: []string{"text"},
					},
					Pricing: OpenRouterPricing{
						Prompt:     "0.015",
						Completion: "0.075",
					},
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

	models, err := service.discoverOpenRouter(context.Background(), provider, "test-api-key")
	if err != nil {
		t.Fatalf("discoverOpenRouter failed: %v", err)
	}
	// The ~alias model should be filtered out
	if len(models) != 1 {
		t.Fatalf("Expected 1 model (alias skipped), got %d", len(models))
	}
	if models[0].ModelID != "anthropic/claude-3-opus" {
		t.Errorf("Expected ModelID 'anthropic/claude-3-opus', got '%s'", models[0].ModelID)
	}
}

func TestDiscoverOpenRouter_CachePricing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" || r.Method != "GET" {
			http.NotFound(w, r)
			return
		}
		response := OpenRouterModelsResponse{
			Data: []OpenRouterModel{
				{
					ID:            "anthropic/claude-3-haiku",
					Name:          "Claude 3 Haiku",
					ContextLength: 200000,
					Architecture: OpenRouterArchitecture{
						Modality:         "chat",
						InputModalities:  []string{"text"},
						OutputModalities: []string{"text"},
					},
					Pricing: OpenRouterPricing{
						Prompt:         "0.000001",
						Completion:     "0.000004",
						InputCacheRead: "0.0000001",
					},
					TopProvider: OpenRouterTopProvider{
						MaxCompletionTokens: 4096,
					},
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

	models, err := service.discoverOpenRouter(context.Background(), provider, "test-api-key")
	if err != nil {
		t.Fatalf("discoverOpenRouter failed: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("Expected 1 model, got %d", len(models))
	}
	// Verify cache pricing was parsed
	if models[0].InputPricePerMillionCacheHit == nil {
		t.Error("Expected InputPricePerMillionCacheHit to be set")
	} else {
		cacheVal := *models[0].InputPricePerMillionCacheHit
		if cacheVal < 0.09 || cacheVal > 0.11 {
			t.Errorf("Expected cache price ~0.1, got %f", cacheVal)
		}
	}
}

func TestDiscoverOpenRouter_EmptyResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := OpenRouterModelsResponse{
			Data: []OpenRouterModel{},
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

	models, err := service.discoverOpenRouter(context.Background(), provider, "test-api-key")
	if err != nil {
		t.Fatalf("discoverOpenRouter failed: %v", err)
	}
	if len(models) != 0 {
		t.Errorf("Expected 0 models for empty response, got %d", len(models))
	}
}

func TestDiscoverOpenRouter_NonChatModelsSkipped(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := OpenRouterModelsResponse{
			Data: []OpenRouterModel{
				{
					ID:            "image-only-model",
					Name:          "Image Gen Only",
					ContextLength: 0,
					Architecture: OpenRouterArchitecture{
						Modality:         "image",
						InputModalities:  []string{"text"},
						OutputModalities: []string{"image"},
					},
					Pricing: OpenRouterPricing{},
				},
				{
					ID:            "text-model",
					Name:          "Text Model",
					ContextLength: 8192,
					Architecture: OpenRouterArchitecture{
						Modality:         "chat",
						InputModalities:  []string{"text"},
						OutputModalities: []string{"text"},
					},
					Pricing: OpenRouterPricing{
						Prompt:     "0.001",
						Completion: "0.002",
					},
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

	models, err := service.discoverOpenRouter(context.Background(), provider, "test-api-key")
	if err != nil {
		t.Fatalf("discoverOpenRouter failed: %v", err)
	}
	// Only the text model should be included
	if len(models) != 1 {
		t.Fatalf("Expected 1 model (image-only skipped), got %d", len(models))
	}
	if models[0].ModelID != "text-model" {
		t.Errorf("Expected ModelID 'text-model', got '%s'", models[0].ModelID)
	}
}

func TestDiscoverOpenRouter_ContextLengthFallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := OpenRouterModelsResponse{
			Data: []OpenRouterModel{
				{
					ID:            "test/model",
					Name:          "Test Model",
					ContextLength: 0, // Zero on model, should fall back to TopProvider
					Architecture: OpenRouterArchitecture{
						Modality:         "chat",
						InputModalities:  []string{"text"},
						OutputModalities: []string{"text"},
					},
					Pricing: OpenRouterPricing{
						Prompt:     "0.001",
						Completion: "0.002",
					},
					TopProvider: OpenRouterTopProvider{
						ContextLength:       32768,
						MaxCompletionTokens: 4096,
					},
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

	models, err := service.discoverOpenRouter(context.Background(), provider, "test-api-key")
	if err != nil {
		t.Fatalf("discoverOpenRouter failed: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("Expected 1 model, got %d", len(models))
	}
	// ContextLength should fall back to TopProvider.ContextLength
	if models[0].ContextLength == nil || *models[0].ContextLength != 32768 {
		t.Errorf("Expected ContextLength 32768 from TopProvider fallback, got %v", models[0].ContextLength)
	}
}

// When OpenRouter omits context_length on both the model and top_provider, and
// the pricing strings are missing/unparseable, those fields must stay nil and
// unmarked-live — otherwise markLiveMeta would flag a non-nil zero as
// provider-reported and Upsert would overwrite a stored real value with 0,
// reporting a bogus metadata change.
func TestDiscoverOpenRouter_MissingMetadataStaysNil(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		response := OpenRouterModelsResponse{
			Data: []OpenRouterModel{
				{
					ID:            "test/no-meta",
					Name:          "No Metadata Model",
					ContextLength: 0, // absent on the model
					Architecture: OpenRouterArchitecture{
						Modality:         "chat",
						InputModalities:  []string{"text"},
						OutputModalities: []string{"text"},
					},
					Pricing: OpenRouterPricing{
						Prompt:     "",    // missing
						Completion: "n/a", // unparseable
					},
					TopProvider: OpenRouterTopProvider{
						ContextLength: 0, // absent here too
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	service := &DiscoveryService{httpClient: server.Client()}
	provider := &Provider{ID: uuid.New(), BaseURL: server.URL}

	models, err := service.discoverOpenRouter(context.Background(), provider, "test-api-key")
	if err != nil {
		t.Fatalf("discoverOpenRouter failed: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("Expected 1 model, got %d", len(models))
	}
	m := models[0]
	if m.ContextLength != nil {
		t.Errorf("ContextLength: expected nil for absent value, got %v", *m.ContextLength)
	}
	if m.InputPricePerMillion != nil {
		t.Errorf("InputPricePerMillion: expected nil for missing price, got %v", *m.InputPricePerMillion)
	}
	if m.OutputPricePerMillion != nil {
		t.Errorf("OutputPricePerMillion: expected nil for unparseable price, got %v", *m.OutputPricePerMillion)
	}
	// Nil fields must not be marked live, so a later scan can't be tricked into
	// overwriting a stored value with zero.
	if m.LiveMeta.ContextLength || m.LiveMeta.InputPrice || m.LiveMeta.OutputPrice {
		t.Errorf("absent fields must not be marked live: %+v", m.LiveMeta)
	}
}
