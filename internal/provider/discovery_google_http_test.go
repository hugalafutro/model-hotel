package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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
		BaseURL: server.URL + "/v1beta/openai",
	}

	_, err := service.discoverGoogleAIStudio(context.Background(), provider, "wrong-api-key")
	if err == nil {
		t.Error("Expected error for unauthorized request, got nil")
	}
}

func TestDiscoverGoogleAIStudio_EmptyModelList(t *testing.T) {
	// Create test server with empty models response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := GoogleModelsResponse{
			Models: []GoogleModel{},
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
		BaseURL: server.URL + "/v1beta/openai",
	}

	models, err := service.discoverGoogleAIStudio(context.Background(), provider, "test-api-key")
	if err != nil {
		t.Fatalf("discoverGoogleAIStudio failed: %v", err)
	}

	if len(models) != 0 {
		t.Errorf("Expected 0 models, got %d", len(models))
	}
}

func TestDiscoverGoogleAIStudio_ContextCancelled(t *testing.T) {
	// Create test server that delays response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		response := GoogleModelsResponse{
			Models: []GoogleModel{
				{
					Name:                       "models/gemini-1.5-flash",
					DisplayName:                "Gemini 1.5 Flash",
					Description:                "Lightweight multimodal model",
					InputTokenLimit:            1000000,
					OutputTokenLimit:           8192,
					SupportedGenerationMethods: []string{"generateContent"},
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
		BaseURL: server.URL + "/v1beta/openai",
	}

	// Create cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := service.discoverGoogleAIStudio(ctx, provider, "test-api-key")
	if err == nil {
		t.Error("Expected error for cancelled context, got nil")
	}
}

func TestDiscoverGoogleAIStudio_Non200Status(t *testing.T) {
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
		BaseURL: server.URL + "/v1beta/openai",
	}

	_, err := service.discoverGoogleAIStudio(context.Background(), provider, "test-api-key")
	if err == nil {
		t.Error("Expected error for non-200 status, got nil")
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
		BaseURL: server.URL + "/v1beta/openai",
	}

	_, err := service.discoverGoogleAIStudio(context.Background(), provider, "test-api-key")
	if err == nil {
		t.Error("Expected error for invalid JSON, got nil")
	}
}

func TestDiscoverGoogleAIStudio_FiltersNonRelevantModels(t *testing.T) {
	t.Parallel()

	// Create test server with mix of relevant and non-relevant models
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1beta/models" {
			response := GoogleModelsResponse{
				Models: []GoogleModel{
					{
						// Relevant model (has generateContent)
						Name:                       "models/gemini-2.0-flash",
						DisplayName:                "Gemini 2.0 Flash",
						Description:                "Test model",
						InputTokenLimit:            1000000,
						OutputTokenLimit:           8192,
						SupportedGenerationMethods: []string{"generateContent"},
						Thinking:                   false,
					},
					{
						// Non-relevant model (video generation only)
						Name:                       "models/veo-2.0",
						DisplayName:                "Veo 2.0",
						Description:                "Video generation",
						InputTokenLimit:            1000,
						OutputTokenLimit:           100,
						SupportedGenerationMethods: []string{"generateVideo"},
						Thinking:                   false,
					},
					{
						// Non-relevant model (embedding only)
						Name:                       "models/text-embedding-004",
						DisplayName:                "Text Embedding",
						Description:                "Embeddings",
						InputTokenLimit:            2048,
						OutputTokenLimit:           768,
						SupportedGenerationMethods: []string{"embedContent"},
						Thinking:                   false,
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
		BaseURL: server.URL + "/v1beta/openai",
	}

	models, err := service.discoverGoogleAIStudio(context.Background(), provider, "test-api-key")
	if err != nil {
		t.Fatalf("discoverGoogleAIStudio failed: %v", err)
	}

	// Should only return relevant models (gemini and embedding)
	// Note: embedding is considered relevant due to embedContent method
	if len(models) != 2 {
		t.Errorf("Expected 2 models (filtered non-relevant), got %d", len(models))
	}

	// Verify gemini model is included
	found := false
	for _, m := range models {
		if m.ModelID == "gemini-2.0-flash" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected gemini-2.0-flash to be included")
	}
}

func TestDiscoverGoogleAIStudio_WithPricingEnrichment(t *testing.T) {
	t.Parallel()

	// Create test server with a model that has pricing in catalog
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1beta/models" {
			response := GoogleModelsResponse{
				Models: []GoogleModel{
					{
						Name:                       "models/gemini-2.5-flash",
						DisplayName:                "Gemini 2.5 Flash",
						Description:                "Lightweight model",
						InputTokenLimit:            1000000,
						OutputTokenLimit:           8192,
						SupportedGenerationMethods: []string{"generateContent"},
						Thinking:                   false,
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
		BaseURL: server.URL + "/v1beta/openai",
	}

	models, err := service.discoverGoogleAIStudio(context.Background(), provider, "test-api-key")
	if err != nil {
		t.Fatalf("discoverGoogleAIStudio failed: %v", err)
	}

	if len(models) != 1 {
		t.Fatalf("Expected 1 model, got %d", len(models))
	}

	// Verify pricing was enriched
	if models[0].InputPricePerMillion == nil {
		t.Error("Expected InputPricePerMillion to be set from pricing catalog")
	}
	if models[0].OutputPricePerMillion == nil {
		t.Error("Expected OutputPricePerMillion to be set from pricing catalog")
	}
}

func TestDiscoverGoogleAIStudio_VisionModel(t *testing.T) {
	t.Parallel()

	// Create test server with vision-capable model
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1beta/models" {
			response := GoogleModelsResponse{
				Models: []GoogleModel{
					{
						Name:                       "models/gemini-2.0-flash",
						DisplayName:                "Gemini 2.0 Flash",
						Description:                "Vision model",
						InputTokenLimit:            1000000,
						OutputTokenLimit:           8192,
						SupportedGenerationMethods: []string{"generateContent"},
						Thinking:                   false,
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
		BaseURL: server.URL + "/v1beta/openai",
	}

	models, err := service.discoverGoogleAIStudio(context.Background(), provider, "test-api-key")
	if err != nil {
		t.Fatalf("discoverGoogleAIStudio failed: %v", err)
	}

	if len(models) != 1 {
		t.Fatalf("Expected 1 model, got %d", len(models))
	}

	var caps model.Capability
	if err := json.Unmarshal([]byte(models[0].Capabilities), &caps); err != nil {
		t.Fatalf("Failed to unmarshal capabilities: %v", err)
	}

	if !caps.Vision {
		t.Error("Expected Vision capability for gemini-2.0-flash")
	}

	// Check input modalities include image
	if !strings.Contains(models[0].InputModalities, "image") {
		t.Errorf("Expected image in InputModalities, got %s", models[0].InputModalities)
	}
}

func TestDiscoverGoogleAIStudio_EmbeddingModel(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1beta/models" {
			response := GoogleModelsResponse{
				Models: []GoogleModel{
					{
						Name:                       "models/text-embedding-004",
						DisplayName:                "Text Embedding 004",
						Description:                "Embedding model",
						InputTokenLimit:            2048,
						OutputTokenLimit:           768,
						SupportedGenerationMethods: []string{"embedContent"},
						Thinking:                   false,
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
		BaseURL: server.URL + "/v1beta/openai",
	}

	models, err := service.discoverGoogleAIStudio(context.Background(), provider, "test-api-key")
	if err != nil {
		t.Fatalf("discoverGoogleAIStudio failed: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("Expected 1 model, got %d", len(models))
	}
	NormalizeModels(models)
	if models[0].Modality != "embedding" {
		t.Errorf("Expected Modality 'embedding', got '%s'", models[0].Modality)
	}
}

func TestDiscoverGoogleAIStudio_AudioModel(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1beta/models" {
			response := GoogleModelsResponse{
				Models: []GoogleModel{
					{
						Name:                       "models/gemini-2.0-flash-tts",
						DisplayName:                "Gemini 2.0 Flash TTS",
						Description:                "Audio model",
						InputTokenLimit:            1000000,
						OutputTokenLimit:           8192,
						SupportedGenerationMethods: []string{"generateContent"},
						Thinking:                   false,
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
		BaseURL: server.URL + "/v1beta/openai",
	}

	models, err := service.discoverGoogleAIStudio(context.Background(), provider, "test-api-key")
	if err != nil {
		t.Fatalf("discoverGoogleAIStudio failed: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("Expected 1 model, got %d", len(models))
	}
	if !strings.Contains(models[0].InputModalities, "audio") {
		t.Errorf("Expected audio in InputModalities for TTS model, got %s", models[0].InputModalities)
	}
	if !strings.Contains(models[0].OutputModalities, "audio") {
		t.Errorf("Expected audio in OutputModalities for TTS model, got %s", models[0].OutputModalities)
	}
}

func TestDiscoverGoogleAIStudio_ImageGenModel(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1beta/models" {
			response := GoogleModelsResponse{
				Models: []GoogleModel{
					{
						Name:                       "models/imagen-3.0-generate",
						DisplayName:                "Imagen 3.0",
						Description:                "Image generation model",
						InputTokenLimit:            1000,
						OutputTokenLimit:           100,
						SupportedGenerationMethods: []string{"generateContent"},
						Thinking:                   false,
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
		BaseURL: server.URL + "/v1beta/openai",
	}

	models, err := service.discoverGoogleAIStudio(context.Background(), provider, "test-api-key")
	if err != nil {
		t.Fatalf("discoverGoogleAIStudio failed: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("Expected 1 model, got %d", len(models))
	}
	if !strings.Contains(models[0].OutputModalities, "image") {
		t.Errorf("Expected image in OutputModalities for image gen model, got %s", models[0].OutputModalities)
	}
}
