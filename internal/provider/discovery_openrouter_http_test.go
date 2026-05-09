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
