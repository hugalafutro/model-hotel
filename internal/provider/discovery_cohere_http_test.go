package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

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
		//nolint:gosec // test-only
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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
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

func TestDiscoverCohere_EmptyModelList(t *testing.T) {
	// Create test server with empty models response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		response := CohereModelsResponse{
			Models: []CohereNativeModel{},
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

	models, err := service.discoverCohere(context.Background(), provider, "test-api-key")
	if err != nil {
		t.Fatalf("discoverCohere failed: %v", err)
	}

	if len(models) != 0 {
		t.Errorf("Expected 0 models, got %d", len(models))
	}
}

func TestDiscoverCohere_ContextCancelled(t *testing.T) {
	// Create test server that delays response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(100 * time.Millisecond)
		response := CohereModelsResponse{
			Models: []CohereNativeModel{
				{
					Name:          "command-r",
					Endpoints:     []string{"chat"},
					ContextLength: 128000,
				},
			},
		}
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

	// Create cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := service.discoverCohere(ctx, provider, "test-api-key")
	if err == nil {
		t.Error("Expected error for cancelled context, got nil")
	}
}

func TestDiscoverCohere_Non200Status(t *testing.T) {
	// Create test server that returns 500
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

	_, err := service.discoverCohere(context.Background(), provider, "test-api-key")
	if err == nil {
		t.Error("Expected error for non-200 status, got nil")
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

func TestDiscoverCohere_FiltersDeprecatedModels(t *testing.T) {
	t.Parallel()

	// Create test server with mix of deprecated and active models
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			if r.URL.Query().Get("endpoint") != "chat" {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(CohereModelsResponse{})
				return
			}
			response := CohereModelsResponse{
				Models: []CohereNativeModel{
					{
						Name:          "command-r-plus",
						Endpoints:     []string{"chat"},
						ContextLength: 128000,
						Features:      []string{"tools"},
						IsDeprecated:  false,
					},
					{
						Name:          "command-old",
						Endpoints:     []string{"chat"},
						ContextLength: 4096,
						Features:      []string{},
						IsDeprecated:  true,
					},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	service := &DiscoveryService{
		httpClient: server.Client(),
	}

	provider := &Provider{
		ID:      uuid.New(),
		BaseURL: server.URL,
	}

	models, err := service.discoverCohere(context.Background(), provider, "test-api-key")
	if err != nil {
		t.Fatalf("discoverCohere failed: %v", err)
	}

	// Should only return non-deprecated models
	if len(models) != 1 {
		t.Errorf("Expected 1 model (filtered deprecated), got %d", len(models))
	}
	if models[0].ModelID != "command-r-plus" {
		t.Errorf("Expected command-r-plus, got %s", models[0].ModelID)
	}
}

func TestDiscoverCohere_Pagination(t *testing.T) {
	t.Parallel()

	var callCount atomic.Int32
	// Create test server that returns paginated results
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			if r.URL.Query().Get("endpoint") != "chat" {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(CohereModelsResponse{})
				return
			}
			callCount.Add(1)
			pageToken := r.URL.Query().Get("page_token")

			var response CohereModelsResponse
			switch pageToken {
			case "":
				// First page
				response = CohereModelsResponse{
					Models: []CohereNativeModel{
						{
							Name:          "command-r-plus",
							Endpoints:     []string{"chat"},
							ContextLength: 128000,
							Features:      []string{"tools"},
						},
					},
					NextPageToken: "page2",
				}
			case "page2":
				// Second page
				response = CohereModelsResponse{
					Models: []CohereNativeModel{
						{
							Name:          "command-r",
							Endpoints:     []string{"chat"},
							ContextLength: 128000,
							Features:      []string{"tools"},
						},
					},
					NextPageToken: "", // No more pages
				}
			}

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	service := &DiscoveryService{
		httpClient: server.Client(),
	}

	provider := &Provider{
		ID:      uuid.New(),
		BaseURL: server.URL,
	}

	models, err := service.discoverCohere(context.Background(), provider, "test-api-key")
	if err != nil {
		t.Fatalf("discoverCohere failed: %v", err)
	}

	// Should return models from both pages
	if len(models) != 2 {
		t.Errorf("Expected 2 models from paginated results, got %d", len(models))
	}

	// Verify both pages were fetched
	if callCount.Load() != 2 {
		t.Errorf("Expected 2 API calls for pagination, got %d", callCount.Load())
	}
}

func TestDiscoverCohere_ModelNotInPricingCatalog(t *testing.T) {
	t.Parallel()

	// Create test server with a model not in pricing catalog
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			if r.URL.Query().Get("endpoint") != "chat" {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(CohereModelsResponse{})
				return
			}
			response := CohereModelsResponse{
				Models: []CohereNativeModel{
					{
						Name:          "unknown-model-xyz",
						Endpoints:     []string{"chat"},
						ContextLength: 8192,
						Features:      []string{},
					},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	service := &DiscoveryService{
		httpClient: server.Client(),
	}

	provider := &Provider{
		ID:      uuid.New(),
		BaseURL: server.URL,
	}

	models, err := service.discoverCohere(context.Background(), provider, "test-api-key")
	if err != nil {
		t.Fatalf("discoverCohere failed: %v", err)
	}

	if len(models) != 1 {
		t.Fatalf("Expected 1 model, got %d", len(models))
	}

	// Model not in pricing catalog should use model name as display name
	if models[0].DisplayName != "unknown-model-xyz" {
		t.Errorf("Expected DisplayName to be model name, got %s", models[0].DisplayName)
	}

	// Should not have pricing set
	if models[0].InputPricePerMillion != nil {
		t.Error("Expected InputPricePerMillion to be nil for unknown model")
	}
}

func TestDiscoverCohere_RerankModels(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		var response CohereModelsResponse
		switch r.URL.Query().Get("endpoint") {
		case "chat":
			response = CohereModelsResponse{
				Models: []CohereNativeModel{
					{Name: "command-r-plus", Endpoints: []string{"chat"}, ContextLength: 128000, Features: []string{"tools"}},
				},
			}
		case "rerank":
			response = CohereModelsResponse{
				Models: []CohereNativeModel{
					{Name: "rerank-v3.5", Endpoints: []string{"rerank"}, ContextLength: 4096},
					{Name: "rerank-english-v2.0", Endpoints: []string{"rerank"}, ContextLength: 512, IsDeprecated: true},
				},
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	service := &DiscoveryService{httpClient: server.Client()}
	provider := &Provider{ID: uuid.New(), BaseURL: server.URL}

	models, err := service.discoverCohere(context.Background(), provider, "test-api-key")
	if err != nil {
		t.Fatalf("discoverCohere failed: %v", err)
	}

	// One chat model + one non-deprecated rerank model.
	if len(models) != 2 {
		t.Fatalf("Expected 2 models, got %d", len(models))
	}

	rr := models[1]
	if rr.ModelID != "rerank-v3.5" {
		t.Fatalf("Expected rerank-v3.5 second, got %s", rr.ModelID)
	}
	if rr.Modality != "rerank" {
		t.Errorf("Expected modality rerank, got %q", rr.Modality)
	}
	if *rr.ContextLength != 4096 {
		t.Errorf("Expected context length 4096, got %d", *rr.ContextLength)
	}
	// Search-unit billing: no per-token prices.
	if rr.InputPricePerMillion != nil || rr.OutputPricePerMillion != nil {
		t.Error("Expected nil per-token prices for a rerank model")
	}
	var caps model.Capability
	if err := json.Unmarshal([]byte(rr.Capabilities), &caps); err != nil {
		t.Errorf("Failed to unmarshal capabilities: %v", err)
	} else if caps.Streaming || caps.ToolCalling || caps.Vision {
		t.Errorf("Expected empty capabilities for a rerank model, got %+v", caps)
	}
	if rr.OwnedBy != "cohere" || !rr.Enabled {
		t.Errorf("Expected enabled cohere-owned model, got owned_by=%q enabled=%v", rr.OwnedBy, rr.Enabled)
	}

	// Chat model construction is unchanged.
	if models[0].ModelID != "command-r-plus" || models[0].Modality != "text" {
		t.Errorf("Chat model unexpectedly altered: %s / %s", models[0].ModelID, models[0].Modality)
	}
}

func TestDiscoverCohere_RerankFetchFailureKeepsChatModels(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("endpoint") != "chat" {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		response := CohereModelsResponse{
			Models: []CohereNativeModel{
				{Name: "command-r", Endpoints: []string{"chat"}, ContextLength: 128000},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	service := &DiscoveryService{httpClient: server.Client()}
	provider := &Provider{ID: uuid.New(), BaseURL: server.URL}

	models, err := service.discoverCohere(context.Background(), provider, "test-api-key")
	if err != nil {
		t.Fatalf("discoverCohere must not fail when only the rerank fetch fails: %v", err)
	}
	if len(models) != 1 || models[0].ModelID != "command-r" {
		t.Fatalf("Expected the chat model to survive, got %v", models)
	}
}
