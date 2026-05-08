package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/model"
)

func TestDiscoverOllama_HTTP(t *testing.T) {
	// Create test server with mock Ollama tags response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" && r.Method == "GET" {
			response := OllamaTagsResponse{
				Models: []OllamaTagsModel{
					{Name: "llama3.2"},
					{Name: "mistral"},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)
		} else if r.URL.Path == "/api/show" && r.Method == "POST" {
			// Mock show response
			response := OllamaShowResponse{
				Capabilities: []string{"tools"},
				ModelInfo: map[string]interface{}{
					"llama.context_length": float64(8192),
				},
				Details: OllamaShowDetails{
					Family: "llama",
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)
		} else {
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	service := &DiscoveryService{
		httpClient: server.Client(),
	}

	provider := &Provider{
		ID:      uuid.New(),
		BaseURL: server.URL,
	}

	models, err := service.discoverOllama(context.Background(), provider, "test-api-key")
	if err != nil {
		t.Fatalf("discoverOllama failed: %v", err)
	}

	if len(models) != 2 {
		t.Errorf("Expected 2 models, got %d", len(models))
	}

	// Check first model
	if models[0].ModelID != "llama3.2" {
		t.Errorf("Expected model ID 'llama3.2', got '%s'", models[0].ModelID)
	}

	if models[0].OwnedBy != "llama" {
		t.Errorf("Expected ownedBy 'llama', got '%s'", models[0].OwnedBy)
	}

	if *models[0].ContextLength != 8192 {
		t.Errorf("Expected context length 8192, got %d", *models[0].ContextLength)
	}

	// Check capabilities
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
	}
}

func TestDiscoverOllama_Non200Status(t *testing.T) {
	// Create test server that returns 500 for tags endpoint
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

	_, err := service.discoverOllama(context.Background(), provider, "test-api-key")
	if err == nil {
		t.Error("Expected error for non-200 status, got nil")
	}
}

func TestDiscoverOllama_InvalidJSON(t *testing.T) {
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

	_, err := service.discoverOllama(context.Background(), provider, "test-api-key")
	if err == nil {
		t.Error("Expected error for invalid JSON, got nil")
	}
}

func TestDiscoverOllama_ContextCancelled(t *testing.T) {
	// Create test server that delays response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		switch r.URL.Path {
		case "/api/tags":
			response := OllamaTagsResponse{
				Models: []OllamaTagsModel{
					{Name: "llama3.2"},
				},
			}
			json.NewEncoder(w).Encode(response)
		case "/api/show":
			response := OllamaShowResponse{
				Capabilities: []string{"tools"},
				ModelInfo: map[string]interface{}{
					"llama.context_length": float64(8192),
				},
				Details: OllamaShowDetails{
					Family: "llama",
				},
			}
			json.NewEncoder(w).Encode(response)
		}
	}))
	defer server.Close()

	service := &DiscoveryService{
		httpClient: server.Client(),
	}

	provider := &Provider{
		ID:      uuid.New(),
		BaseURL: server.URL,
	}

	// Create cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := service.discoverOllama(ctx, provider, "test-api-key")
	if err == nil {
		t.Error("Expected error for cancelled context, got nil")
	}
}

func TestOllamaShowModel_Non200Status(t *testing.T) {
	// Create test server that returns 404
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Model not found", http.StatusNotFound)
	}))
	defer server.Close()

	service := &DiscoveryService{
		httpClient: server.Client(),
	}

	_, err := service.ollamaShowModel(context.Background(), server.URL, "test-api-key", "nonexistent-model")
	if err == nil {
		t.Error("Expected error for non-200 status, got nil")
	}
}

func TestOllamaShowModel_InvalidJSON(t *testing.T) {
	// Create test server with invalid JSON response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("{ invalid json "))
	}))
	defer server.Close()

	service := &DiscoveryService{
		httpClient: server.Client(),
	}

	_, err := service.ollamaShowModel(context.Background(), server.URL, "test-api-key", "test-model")
	if err == nil {
		t.Error("Expected error for invalid JSON, got nil")
	}
}

func TestOllamaShowModel_Success(t *testing.T) {
	// Create test server with valid response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := OllamaShowResponse{
			Capabilities: []string{"tools", "vision"},
			ModelInfo: map[string]interface{}{
				"llama.context_length": float64(16384),
			},
			Details: OllamaShowDetails{
				Family: "mistral",
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	service := &DiscoveryService{
		httpClient: server.Client(),
	}

	show, err := service.ollamaShowModel(context.Background(), server.URL, "test-api-key", "test-model")
	if err != nil {
		t.Fatalf("ollamaShowModel failed: %v", err)
	}

	if len(show.Capabilities) != 2 {
		t.Errorf("Expected 2 capabilities, got %d", len(show.Capabilities))
	}

	if show.Details.Family != "mistral" {
		t.Errorf("Expected family 'mistral', got '%s'", show.Details.Family)
	}

	ctxLen := show.ModelInfo["llama.context_length"]
	if ctxLen != float64(16384) {
		t.Errorf("Expected context length 16384, got %v", ctxLen)
	}
}
