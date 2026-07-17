package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
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
	} else if !caps.Vision {
		t.Error("Expected vision capability to be true for multimodal model")
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
	} else if !caps.Streaming {
		t.Error("Expected streaming capability to be true")
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

	_, err := service.discoverXAILanguageModels(context.Background(), provider, "wrong-api-key", server.URL)
	if err == nil {
		t.Error("Expected error for unauthorized request, got nil")
	}
}

func TestDiscoverXAILanguageModels_InvalidResponse(t *testing.T) {
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

	_, err := service.discoverXAILanguageModels(context.Background(), provider, "test-api-key", server.URL)
	if err == nil {
		t.Error("Expected error for invalid JSON, got nil")
	}
}

// Test discoverXAI main function - success with language models
func TestDiscoverXAI_SuccessLanguageModels(t *testing.T) {
	// Server that returns language models successfully
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/language-models" {
			response := XAILanguageModelsResponse{
				Models: []XAILanguageModel{
					{
						ID:                         "test-model",
						Object:                     "model",
						OwnedBy:                    "xai",
						Version:                    "1.0",
						InputModalities:            []string{"text"},
						OutputModalities:           []string{"text"},
						PromptTextTokenPrice:       50,
						CachedPromptTextTokenPrice: 25,
						CompletionTextTokenPrice:   150,
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

	svc := &DiscoveryService{httpClient: server.Client()}
	provider := &Provider{
		ID:      uuid.New(),
		BaseURL: server.URL,
	}

	ctx := context.Background()
	models, err := svc.discoverXAI(ctx, provider, "test-api-key")
	if err != nil {
		t.Fatalf("discoverXAI failed: %v", err)
	}
	// Live model "test-model" is unioned with the catalog.
	if len(models) != len(GetXAICatalog())+1 {
		t.Errorf("expected catalog+1 merged models, got %d", len(models))
	}
	var foundLive bool
	for _, m := range models {
		if m.ModelID == "test-model" {
			foundLive = true
		}
	}
	if !foundLive {
		t.Error("expected live 'test-model' present in merged results")
	}
}

// Test discoverXAI fallback to minimal models when language models returns empty
func TestDiscoverXAI_FallbackToMinimalModels(t *testing.T) {
	// Server that returns empty language models but has minimal models
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/language-models" {
			// Return empty list - should trigger fallback to /models
			response := XAILanguageModelsResponse{Models: []XAILanguageModel{}}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)
			return
		}
		if r.URL.Path == "/models" {
			response := XAIModelsResponse{
				Object: "list",
				Data: []XAIModel{
					{ID: "minimal-model", Object: "model", OwnedBy: "xai"},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	svc := &DiscoveryService{httpClient: server.Client()}
	provider := &Provider{
		ID:      uuid.New(),
		BaseURL: server.URL,
	}

	ctx := context.Background()
	models, err := svc.discoverXAI(ctx, provider, "test-api-key")
	if err != nil {
		t.Fatalf("discoverXAI failed: %v", err)
	}
	// Empty /language-models -> minimal /models -> unioned with the catalog.
	if len(models) != len(GetXAICatalog())+1 {
		t.Errorf("expected catalog+1 merged models from minimal fallback, got %d", len(models))
	}
	var foundLive bool
	for _, m := range models {
		if m.ModelID == "minimal-model" {
			foundLive = true
		}
	}
	if !foundLive {
		t.Error("expected live 'minimal-model' present in merged results")
	}
}

// Test discoverXAI with 429 rate limit - should fallback to catalog
func TestDiscoverXAI_RateLimitFallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/language-models" || r.URL.Path == "/models" {
			http.Error(w, "Rate Limited", http.StatusTooManyRequests)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	svc := &DiscoveryService{httpClient: server.Client()}
	provider := &Provider{
		ID:      uuid.New(),
		BaseURL: server.URL,
	}

	ctx := context.Background()
	_, err := svc.discoverXAI(ctx, provider, "test-api-key")
	// 429 is not treated as a no-access error (only 403 is), so it returns an error
	if err == nil {
		t.Fatal("expected error for 429 status, got nil")
		return
	}
}

// Test discoverXAI with HTTP error (not 403/429) - should return error
func TestDiscoverXAI_HttpError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/language-models" || r.URL.Path == "/models" {
			http.Error(w, "Bad Gateway", http.StatusBadGateway)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	svc := &DiscoveryService{httpClient: server.Client()}
	provider := &Provider{
		ID:      uuid.New(),
		BaseURL: server.URL,
	}

	ctx := context.Background()
	_, err := svc.discoverXAI(ctx, provider, "test-api-key")
	if err == nil {
		t.Fatal("expected error for 502 status, got nil")
		return
	}
}

// Test discoverXAI with invalid JSON in language models
func TestDiscoverXAI_InvalidJSONLanguageModels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/language-models" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte("{ invalid json }"))
			return
		}
		if r.URL.Path == "/models" {
			http.Error(w, "Not Found", http.StatusNotFound)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	svc := &DiscoveryService{httpClient: server.Client()}
	provider := &Provider{
		ID:      uuid.New(),
		BaseURL: server.URL,
	}

	ctx := context.Background()
	_, err := svc.discoverXAI(ctx, provider, "test-api-key")
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
		return
	}
}

// Test discoverXAI with invalid JSON in minimal models
func TestDiscoverXAI_InvalidJSONMinimalModels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/language-models" {
			http.Error(w, "Not Found", http.StatusNotFound)
			return
		}
		if r.URL.Path == "/models" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte("{ invalid json }"))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	svc := &DiscoveryService{httpClient: server.Client()}
	provider := &Provider{
		ID:      uuid.New(),
		BaseURL: server.URL,
	}

	ctx := context.Background()
	_, err := svc.discoverXAI(ctx, provider, "test-api-key")
	if err == nil {
		t.Fatal("expected error for invalid JSON in minimal models, got nil")
		return
	}
}

// Test isNoAccessError helper function
func TestIsNoAccessError(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantResult bool
	}{
		{
			name:       "nil error",
			err:        nil,
			wantResult: false,
		},
		{
			name:       "403 forbidden",
			err:        &httpError{StatusCode: http.StatusForbidden},
			wantResult: true,
		},
		{
			name:       "429 too many requests",
			err:        &httpError{StatusCode: http.StatusTooManyRequests},
			wantResult: true,
		},
		{
			name:       "500 internal server error",
			err:        &httpError{StatusCode: http.StatusInternalServerError},
			wantResult: false,
		},
		{
			name:       "404 not found",
			err:        &httpError{StatusCode: http.StatusNotFound},
			wantResult: false,
		},
		{
			name:       "generic error",
			err:        fmt.Errorf("generic error"),
			wantResult: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isNoAccessError(tt.err)
			if got != tt.wantResult {
				t.Errorf("isNoAccessError() = %v, want %v", got, tt.wantResult)
			}
		})
	}
}

// Test httpError Error method
func TestHttpError_Error(t *testing.T) {
	err := &httpError{StatusCode: http.StatusForbidden, Body: "forbidden"}
	expected := "unexpected status 403"
	if err.Error() != expected {
		t.Errorf("httpError.Error() = %q, want %q", err.Error(), expected)
	}
}

func TestDiscoverXAIMinimalModels_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/models" {
			http.Error(w, "Bad Gateway", http.StatusBadGateway)
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
		Name:    "test-xai",
		BaseURL: server.URL,
	}

	models, err := service.discoverXAIMinimalModels(context.Background(), provider, "test-api-key", server.URL)
	if err == nil {
		t.Error("Expected error for 502 response, got nil")
	}
	if models != nil {
		t.Errorf("Expected nil models, got %d models", len(models))
	}
}

func TestDiscoverXAIMinimalModels_EmptyResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/models" {
			response := XAIModelsResponse{
				Object: "list",
				Data:   []XAIModel{},
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
		Name:    "test-xai",
		BaseURL: server.URL,
	}

	models, err := service.discoverXAIMinimalModels(context.Background(), provider, "test-api-key", server.URL)
	if err != nil {
		t.Fatalf("discoverXAIMinimalModels failed: %v", err)
	}
	if len(models) != 0 {
		t.Errorf("Expected 0 models for empty response, got %d", len(models))
	}
}

func TestDiscoverXAIMinimalModels_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/models" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte("{ invalid json "))
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
		Name:    "test-xai",
		BaseURL: server.URL,
	}

	_, err := service.discoverXAIMinimalModels(context.Background(), provider, "test-api-key", server.URL)
	if err == nil {
		t.Error("Expected error for invalid JSON, got nil")
	}
}

func TestDiscoverXAIMinimalModels_429ReturnsError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/models" {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error": "rate limited"}`))
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
		Name:    "test-xai",
		BaseURL: server.URL,
	}

	models, err := service.discoverXAIMinimalModels(context.Background(), provider, "test-api-key", server.URL)
	if models != nil {
		t.Errorf("Expected nil models, got %d models", len(models))
	}
	if err == nil {
		t.Fatal("Expected error for 429 response, got nil")
	}
	// 429 is NOT returned as httpError (only 403 is), so it should be a regular error
	if !strings.Contains(err.Error(), "429") {
		t.Errorf("Expected error to mention 429, got: %v", err)
	}
}

func TestDiscoverXAI_429DoesNotFallbackToCatalog(t *testing.T) {
	// Both endpoints return 429 (rate limit) which is NOT treated as httpError
	// Only 403 triggers httpError catalog fallback, so 429 returns an error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/language-models" || r.URL.Path == "/models" {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error": "rate limited"}`))
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
		Name:    "test-xai",
		BaseURL: server.URL,
	}

	_, err := service.discoverXAI(context.Background(), provider, "test-api-key")
	if err == nil {
		t.Error("Expected error for 429 (not a catalog fallback), got nil")
	}
}

func TestDiscoverXAILanguageModels_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Bad Gateway", http.StatusBadGateway)
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
		t.Error("Expected error for 502 response, got nil")
	}
}

func TestDiscoverXAILanguageModels_VisionModel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/language-models" {
			http.NotFound(w, r)
			return
		}
		response := XAILanguageModelsResponse{
			Models: []XAILanguageModel{
				{
					ID:                         "grok-vision",
					Object:                     "model",
					OwnedBy:                    "xai",
					Version:                    "1.0",
					InputModalities:            []string{"text", "image"},
					OutputModalities:           []string{"text"},
					PromptTextTokenPrice:       100,
					CachedPromptTextTokenPrice: 50,
					CompletionTextTokenPrice:   300,
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

	models, err := service.discoverXAILanguageModels(context.Background(), provider, "test-api-key", server.URL)
	if err != nil {
		t.Fatalf("discoverXAILanguageModels failed: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("Expected 1 model, got %d", len(models))
	}

	var caps model.Capability
	if err := json.Unmarshal([]byte(models[0].Capabilities), &caps); err != nil {
		t.Fatalf("Failed to unmarshal capabilities: %v", err)
	}
	if !caps.Vision {
		t.Error("Expected Vision capability to be true for multimodal model")
	}
}

func TestDiscoverXAIMinimalModels_UnknownModel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			http.NotFound(w, r)
			return
		}
		response := XAIModelsResponse{
			Object: "list",
			Data: []XAIModel{
				{
					ID:      "grok-unknown-future-model",
					Object:  "model",
					OwnedBy: "xai",
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

	models, err := service.discoverXAIMinimalModels(context.Background(), provider, "test-api-key", server.URL)
	if err != nil {
		t.Fatalf("discoverXAIMinimalModels failed: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("Expected 1 model, got %d", len(models))
	}
	// Unknown model should get minimal entry with streaming only
	if models[0].ModelID != "grok-unknown-future-model" {
		t.Errorf("Expected ModelID 'grok-unknown-future-model', got '%s'", models[0].ModelID)
	}
}

func TestDiscoverXAIMinimalModels_ConnectionError(t *testing.T) {
	// Use a closed server to simulate connection error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	server.Close()

	service := &DiscoveryService{
		httpClient: server.Client(),
	}

	provider := &Provider{
		ID:      uuid.New(),
		Name:    "test-xai",
		BaseURL: server.URL,
	}

	_, err := service.discoverXAIMinimalModels(context.Background(), provider, "test-api-key", server.URL)
	if err == nil {
		t.Error("Expected error for connection failure, got nil")
	}
}

func TestDiscoverXAIMinimalModels_CatalogModelLookup(t *testing.T) {
	// The minimal /models path returns clean live entries (id + owner only);
	// catalog backfill now happens in mergeLiveAndCatalog, not at this layer.
	catalog := GetXAICatalog()
	if len(catalog) == 0 {
		t.Skip("No models in xAI catalog")
	}
	// Use a model ID that exists in the catalog to prove this layer does NOT
	// apply catalog data on its own.
	catalogModelID := catalog[0].ModelID

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			http.NotFound(w, r)
			return
		}
		response := XAIModelsResponse{
			Object: "list",
			Data: []XAIModel{
				{ID: catalogModelID, Object: "model", OwnedBy: "xai"},
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

	models, err := service.discoverXAIMinimalModels(context.Background(), provider, "test-api-key", server.URL)
	if err != nil {
		t.Fatalf("discoverXAIMinimalModels failed: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("Expected 1 model, got %d", len(models))
	}

	// Minimal layer must NOT backfill from the catalog: context/max-output stay
	// nil here and are filled later by mergeLiveAndCatalog.
	m := models[0]
	if m.ModelID != catalogModelID {
		t.Errorf("ModelID = %q, want %q", m.ModelID, catalogModelID)
	}
	if m.ContextLength != nil {
		t.Errorf("ContextLength = %d, want nil at the minimal layer", *m.ContextLength)
	}
	if m.MaxOutputTokens != nil {
		t.Errorf("MaxOutputTokens = %d, want nil at the minimal layer", *m.MaxOutputTokens)
	}
}

func TestDiscoverXAI_MinimalModelsFallback(t *testing.T) {
	// Server where language models return error but minimal models succeed
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/language-models" || r.URL.Path == "/language-models" {
			http.Error(w, "Not Found", http.StatusNotFound)
			return
		}
		if r.URL.Path == "/v1/models" || r.URL.Path == "/models" {
			response := XAIModelsResponse{
				Object: "list",
				Data: []XAIModel{
					{ID: "test-minimal-model", Object: "model", OwnedBy: "xai"},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	svc := &DiscoveryService{httpClient: server.Client()}
	provider := &Provider{
		ID:      uuid.New(),
		Name:    "test-xai-minimal-fallback",
		BaseURL: server.URL,
	}

	models, err := svc.discoverXAI(context.Background(), provider, "test-api-key")
	if err != nil {
		t.Fatalf("discoverXAI failed: %v", err)
	}
	// The minimal live model is now unioned with the catalog, so the result is
	// the catalog plus the one live-only model.
	if len(models) != len(GetXAICatalog())+1 {
		t.Errorf("expected catalog+1 merged models, got %d", len(models))
	}
	var foundLive bool
	for _, m := range models {
		if m.ModelID == "test-minimal-model" {
			foundLive = true
		}
	}
	if !foundLive {
		t.Error("expected live-only 'test-minimal-model' present in merged results")
	}
}

func TestDiscoverXAI_LanguageModelsEmpty_MinimalModelsEmpty(t *testing.T) {
	// The rich endpoint returns an empty (but successful) list. The union with
	// the catalog means discovery still yields the catalog models rather than
	// failing.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/language-models" || r.URL.Path == "/language-models" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(XAILanguageModelsResponse{Models: []XAILanguageModel{}})
			return
		}
		if r.URL.Path == "/v1/models" || r.URL.Path == "/models" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(XAIModelsResponse{Object: "list", Data: []XAIModel{}})
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	svc := &DiscoveryService{httpClient: server.Client()}
	provider := &Provider{
		ID:      uuid.New(),
		Name:    "test-xai-empty",
		BaseURL: server.URL,
	}

	models, err := svc.discoverXAI(context.Background(), provider, "test-api-key")
	if err != nil {
		t.Fatalf("expected no error with empty live, got: %v", err)
	}
	// Empty-but-successful endpoints return empty (not the catalog) so
	// RecordMissingModels stays a no-op; the no-access 403/429 path still
	// returns the catalog (covered by TestDiscoverXAI_RateLimitFallback).
	if len(models) != 0 {
		t.Errorf("expected 0 models when live is empty, got %d", len(models))
	}
}

func TestDiscoverXAIImageModels(t *testing.T) {
	imageServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/image-generation-models" || r.Method != "GET" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer test-api-key" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		response := XAIImageGenerationModelsResponse{
			Models: []XAIImageGenerationModel{
				{
					ID:               "grok-imagine-image",
					Object:           "model",
					OwnedBy:          "xai",
					Version:          "1.0",
					InputModalities:  []string{"text", "image"},
					OutputModalities: []string{"image"},
					ImagePrice:       200000000,
					Aliases:          []string{"grok-imagine-image-2026-03-02"},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer imageServer.Close()

	service := &DiscoveryService{httpClient: imageServer.Client()}
	provider := &Provider{ID: uuid.New(), BaseURL: imageServer.URL}

	models, err := service.discoverXAIImageModels(context.Background(), provider, "test-api-key", imageServer.URL)
	if err != nil {
		t.Fatalf("discoverXAIImageModels failed: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("expected 1 image model, got %d", len(models))
	}
	m := models[0]
	if m.ModelID != "grok-imagine-image" {
		t.Errorf("model ID = %q, want grok-imagine-image", m.ModelID)
	}
	if !strings.Contains(m.OutputModalities, "image") {
		t.Errorf("output modalities = %q, want to contain image", m.OutputModalities)
	}
	if !strings.Contains(m.InputModalities, "image") {
		t.Errorf("input modalities = %q, want to contain image (grok image models take image input)", m.InputModalities)
	}
	if m.Modality != "image" {
		t.Errorf("modality = %q, want image", m.Modality)
	}
	if !strings.Contains(m.Params, `"image_generation":true`) {
		t.Errorf("params = %q, want image_generation true", m.Params)
	}
	if !strings.Contains(m.Params, `"image_price":200000000`) {
		t.Errorf("params = %q, want image_price 200000000", m.Params)
	}
	if m.InputPricePerMillion != nil || m.OutputPricePerMillion != nil {
		t.Error("image models must not carry token pricing")
	}
	if !m.Enabled {
		t.Error("discovered image model should be enabled")
	}
}

func TestDiscoverXAIImageModels_Non200IsError(t *testing.T) {
	imageServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer imageServer.Close()

	service := &DiscoveryService{httpClient: imageServer.Client()}
	provider := &Provider{ID: uuid.New(), BaseURL: imageServer.URL}

	if _, err := service.discoverXAIImageModels(context.Background(), provider, "test-api-key", imageServer.URL); err == nil {
		t.Fatal("expected error on non-200 image-models response")
	}
}

// TestDiscoverXAI_UnionsImageModels drives the full discoverXAI path with both
// the language and image endpoints populated, covering the best-effort image
// append/dedup wiring in the merged path.
func TestDiscoverXAI_UnionsImageModels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/language-models":
			_ = json.NewEncoder(w).Encode(XAILanguageModelsResponse{
				Models: []XAILanguageModel{{
					ID:               "grok-lang",
					Object:           "model",
					OwnedBy:          "xai",
					Version:          "1.0",
					InputModalities:  []string{"text"},
					OutputModalities: []string{"text"},
				}},
			})
		case "/image-generation-models":
			_ = json.NewEncoder(w).Encode(XAIImageGenerationModelsResponse{
				Models: []XAIImageGenerationModel{{
					ID:               "grok-imagine-image",
					Object:           "model",
					OwnedBy:          "xai",
					InputModalities:  []string{"text", "image"},
					OutputModalities: []string{"image"},
					ImagePrice:       200000000,
				}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	svc := &DiscoveryService{httpClient: server.Client()}
	provider := &Provider{ID: uuid.New(), BaseURL: server.URL}

	models, err := svc.discoverXAI(context.Background(), provider, "test-api-key")
	if err != nil {
		t.Fatalf("discoverXAI failed: %v", err)
	}
	var img *model.Model
	for _, m := range models {
		if m.ModelID == "grok-imagine-image" {
			img = m
		}
	}
	if img == nil {
		t.Fatal("expected image model to be unioned into discoverXAI results")
	}
	if !strings.Contains(img.OutputModalities, "image") {
		t.Errorf("image model output modalities = %q, want to contain image", img.OutputModalities)
	}
}

// TestDiscoverXAIImageModels_EmptyModalitiesFallback covers the input/output
// modality fallbacks and the "image" modality classification.
func TestDiscoverXAIImageModels_EmptyModalitiesFallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/image-generation-models" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(XAIImageGenerationModelsResponse{
			Models: []XAIImageGenerationModel{{ID: "bare-image", OwnedBy: "xai"}},
		})
	}))
	defer server.Close()

	svc := &DiscoveryService{httpClient: server.Client()}
	provider := &Provider{ID: uuid.New(), BaseURL: server.URL}

	models, err := svc.discoverXAIImageModels(context.Background(), provider, "test-api-key", server.URL)
	if err != nil {
		t.Fatalf("discoverXAIImageModels failed: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("expected 1 model, got %d", len(models))
	}
	m := models[0]
	if m.InputModalities != `["text"]` {
		t.Errorf("input modalities = %q, want [\"text\"] fallback", m.InputModalities)
	}
	if m.OutputModalities != `["image"]` {
		t.Errorf("output modalities = %q, want [\"image\"] fallback", m.OutputModalities)
	}
	if m.Modality != "image" {
		t.Errorf("modality = %q, want image", m.Modality)
	}
	if strings.Contains(m.Params, "image_price") {
		t.Errorf("params = %q, should omit image_price when zero", m.Params)
	}
}

// TestDiscoverXAIImageModels_InvalidJSONIsError covers the decode error path.
func TestDiscoverXAIImageModels_InvalidJSONIsError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{not json"))
	}))
	defer server.Close()

	svc := &DiscoveryService{httpClient: server.Client()}
	provider := &Provider{ID: uuid.New(), BaseURL: server.URL}

	if _, err := svc.discoverXAIImageModels(context.Background(), provider, "test-api-key", server.URL); err == nil {
		t.Fatal("expected error decoding invalid JSON")
	}
}

// TestDiscoverXAIImageModels_RequestBuildError covers the request-construction
// error path via a base URL containing a control character.
func TestDiscoverXAIImageModels_RequestBuildError(t *testing.T) {
	svc := &DiscoveryService{httpClient: http.DefaultClient}
	provider := &Provider{ID: uuid.New(), BaseURL: "http://\x7fbad"}

	if _, err := svc.discoverXAIImageModels(context.Background(), provider, "test-api-key", "http://\x7fbad"); err == nil {
		t.Fatal("expected error building request from an invalid base URL")
	}
}

// TestDiscoverXAIImageModels_TransportError covers the transport-failure path by
// pointing at a server that has already been closed.
func TestDiscoverXAIImageModels_TransportError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := server.URL
	server.Close()

	svc := &DiscoveryService{httpClient: server.Client()}
	provider := &Provider{ID: uuid.New(), BaseURL: url}

	if _, err := svc.discoverXAIImageModels(context.Background(), provider, "test-api-key", url); err == nil {
		t.Fatal("expected transport error against a closed server")
	}
}

// TestDiscoverXAIImageModels_BodyReadError covers the response-read error path by
// declaring a Content-Length larger than the body actually written, so the
// client's io.ReadAll fails with an unexpected EOF.
func TestDiscoverXAIImageModels_BodyReadError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "1000")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("short"))
	}))
	defer server.Close()

	svc := &DiscoveryService{httpClient: server.Client()}
	provider := &Provider{ID: uuid.New(), BaseURL: server.URL}

	if _, err := svc.discoverXAIImageModels(context.Background(), provider, "test-api-key", server.URL); err == nil {
		t.Fatal("expected read error when the body is shorter than Content-Length")
	}
}

// TestDiscoverXAI_ImageModelsOnCatalogFallback verifies image models are still
// unioned in when /language-models returns no-access (403) and discovery falls
// back to the catalog: image generation has its own endpoint access.
func TestDiscoverXAI_ImageModelsOnCatalogFallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/language-models", "/models":
			http.Error(w, "forbidden", http.StatusForbidden)
		case "/image-generation-models":
			_ = json.NewEncoder(w).Encode(XAIImageGenerationModelsResponse{
				Models: []XAIImageGenerationModel{{
					ID:               "grok-imagine-image",
					OwnedBy:          "xai",
					InputModalities:  []string{"text", "image"},
					OutputModalities: []string{"image"},
					ImagePrice:       200000000,
				}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	svc := &DiscoveryService{httpClient: server.Client()}
	provider := &Provider{ID: uuid.New(), BaseURL: server.URL}

	models, err := svc.discoverXAI(context.Background(), provider, "test-api-key")
	if err != nil {
		t.Fatalf("discoverXAI failed: %v", err)
	}
	var img *model.Model
	for _, m := range models {
		if m.ModelID == "grok-imagine-image" {
			img = m
		}
	}
	if img == nil {
		t.Fatal("expected image model to be unioned in on the catalog no-access fallback")
	}
	if img.Modality != "image" {
		t.Errorf("image model modality = %q, want image", img.Modality)
	}
}
