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

func TestDiscoverCohere(t *testing.T) {
	// Create test server with mock Cohere response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check if it's the models endpoint
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

		// Check query parameters
		endpoint := r.URL.Query().Get("endpoint")
		if endpoint != "chat" {
			http.Error(w, "Bad Request", http.StatusBadRequest)
			return
		}

		// Mock Cohere models response
		response := CohereModelsResponse{
			Models: []CohereNativeModel{
				{
					Name:          "command-r-plus",
					Endpoints:     []string{"chat"},
					ContextLength: 128000,
					Features:      []string{"tools", "vision", "reasoning"},
				},
				{
					Name:          "command-r",
					Endpoints:     []string{"chat"},
					ContextLength: 128000,
					Features:      []string{"tools", "reasoning"},
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
	// Use a URL that won't trigger conversion to real Cohere API
	provider := &Provider{
		ID:      uuid.New(),
		BaseURL: server.URL, // Use the actual test server URL
	}

	// Test discovery
	models, err := service.discoverCohere(context.Background(), provider, "test-api-key")
	if err != nil {
		t.Fatalf("discoverCohere failed: %v", err)
	}

	// Verify results
	if len(models) != 2 {
		t.Errorf("Expected 2 models, got %d", len(models))
	}

	// Check first model
	if models[0].ModelID != "command-r-plus" {
		t.Errorf("Expected model ID 'command-r-plus', got '%s'", models[0].ModelID)
	}

	if models[0].OwnedBy != "cohere" {
		t.Errorf("Expected ownedBy 'cohere', got '%s'", models[0].OwnedBy)
	}

	if *models[0].ContextLength != 128000 {
		t.Errorf("Expected context length 128000, got %d", *models[0].ContextLength)
	}

	// Check capabilities - should have vision, tool calling, and reasoning
	var caps model.Capability
	if err := json.Unmarshal([]byte(models[0].Capabilities), &caps); err != nil {
		t.Errorf("Failed to unmarshal capabilities: %v", err)
	} else {
		if !caps.Vision {
			t.Error("Expected vision capability to be true")
		}
		if !caps.ToolCalling {
			t.Error("Expected tool calling capability to be true")
		}
		if !caps.Reasoning {
			t.Error("Expected reasoning capability to be true")
		}
	}

	// Check second model
	if models[1].ModelID != "command-r" {
		t.Errorf("Expected model ID 'command-r', got '%s'", models[1].ModelID)
	}
}

func TestDiscoverCohere_Unauthorized(t *testing.T) {
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
		BaseURL: "https://api.cohere.ai/compatibility/v1",
	}

	_, err := service.discoverCohere(context.Background(), provider, "wrong-api-key")
	if err == nil {
		t.Error("Expected error for unauthorized request, got nil")
	}
}

func TestDiscoverCohere_InvalidResponse(t *testing.T) {
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
		BaseURL: "https://api.cohere.ai/compatibility/v1",
	}

	_, err := service.discoverCohere(context.Background(), provider, "test-api-key")
	if err == nil {
		t.Error("Expected error for invalid JSON, got nil")
	}
}
