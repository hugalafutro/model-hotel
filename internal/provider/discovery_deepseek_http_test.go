package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

func TestDiscoverDeepSeek(t *testing.T) {
	// Create test server with mock DeepSeek response
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
