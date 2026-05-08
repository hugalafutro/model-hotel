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

func TestDiscoverGoogleAIStudio(t *testing.T) {
	// Create test server with mock Google AI Studio response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check if it's the models endpoint with key parameter
		if r.URL.Path != "/v1beta/models" || r.Method != "GET" {
			http.NotFound(w, r)
			return
		}

		// Check key parameter
		key := r.URL.Query().Get("key")
		if key != "test-api-key" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// Mock Google AI Studio models response
		response := GoogleModelsResponse{
			Models: []GoogleModel{
				{
					Name:                       "models/gemini-1.5-flash",
					DisplayName:                "Gemini 1.5 Flash",
					Description:                "Lightweight multimodal model",
					InputTokenLimit:            1000000,
					OutputTokenLimit:           8192,
					SupportedGenerationMethods: []string{"generateContent"},
					Temperature:                0.9,
					Thinking:                   true,
				},
				{
					Name:                       "models/gemini-1.5-pro",
					DisplayName:                "Gemini 1.5 Pro",
					Description:                "Advanced multimodal model",
					InputTokenLimit:            2000000,
					OutputTokenLimit:           8192,
					SupportedGenerationMethods: []string{"generateContent"},
					Temperature:                0.9,
					Thinking:                   true,
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
	// The discovery function converts /v1beta/openai to /v1beta, so we need to use the native URL directly
	provider := &Provider{
		ID:      uuid.New(),
		BaseURL: server.URL + "/v1beta/openai",
	}

	// Test discovery
	models, err := service.discoverGoogleAIStudio(context.Background(), provider, "test-api-key")
	if err != nil {
		t.Fatalf("discoverGoogleAIStudio failed: %v", err)
	}

	// Verify results
	if len(models) != 2 {
		t.Errorf("Expected 2 models, got %d", len(models))
	}

	// Check first model
	if models[0].ModelID != "gemini-1.5-flash" {
		t.Errorf("Expected model ID 'gemini-1.5-flash', got '%s'", models[0].ModelID)
	}

	if models[0].OwnedBy != "google" {
		t.Errorf("Expected ownedBy 'google', got '%s'", models[0].OwnedBy)
	}

	if models[0].DisplayName != "Gemini 1.5 Flash" {
		t.Errorf("Expected display name 'Gemini 1.5 Flash', got '%s'", models[0].DisplayName)
	}

	if *models[0].ContextLength != 1000000 {
		t.Errorf("Expected context length 1000000, got %d", *models[0].ContextLength)
	}

	if *models[0].MaxOutputTokens != 8192 {
		t.Errorf("Expected max output tokens 8192, got %d", *models[0].MaxOutputTokens)
	}

	// Check capabilities - should have streaming, reasoning, and tool calling
	var caps model.Capability
	if err := json.Unmarshal([]byte(models[0].Capabilities), &caps); err != nil {
		t.Errorf("Failed to unmarshal capabilities: %v", err)
	} else {
		if !caps.Streaming {
			t.Error("Expected streaming capability to be true")
		}
		if !caps.Reasoning {
			t.Error("Expected reasoning capability to be true")
		}
		if !caps.ToolCalling {
			t.Error("Expected tool calling capability to be true")
		}
	}

	// Check second model
	if models[1].ModelID != "gemini-1.5-pro" {
		t.Errorf("Expected model ID 'gemini-1.5-pro', got '%s'", models[1].ModelID)
	}

	if *models[1].ContextLength != 2000000 {
		t.Errorf("Expected context length 2000000, got %d", *models[1].ContextLength)
	}
}

func TestDiscoverGoogleAIStudio_Unauthorized(t *testing.T) {
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
		BaseURL: "https://generativelanguage.googleapis.com/v1beta/openai",
	}

	_, err := service.discoverGoogleAIStudio(context.Background(), provider, "wrong-api-key")
	if err == nil {
		t.Error("Expected error for unauthorized request, got nil")
	}
}

func TestDiscoverGoogleAIStudio_InvalidResponse(t *testing.T) {
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
		BaseURL: "https://generativelanguage.googleapis.com/v1beta/openai",
	}

	_, err := service.discoverGoogleAIStudio(context.Background(), provider, "test-api-key")
	if err == nil {
		t.Error("Expected error for invalid JSON, got nil")
	}
}
