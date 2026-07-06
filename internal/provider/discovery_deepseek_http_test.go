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

func TestDiscoverDeepSeek(t *testing.T) {
	// Create test server with mock DeepSeek response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

	// Verify results: the live models are first, then the catalog is unioned in.
	// deepseek-chat is now also in the catalog, so it dedupes; only deepseek-coder
	// is live-only, adding a single entry on top of the catalog.
	if len(models) != len(GetDeepSeekModels())+1 {
		t.Errorf("Expected catalog+1 merged models, got %d", len(models))
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
	assertDiscoverHTTPError(t, "unauthorized request", errorStatusHandler(http.StatusUnauthorized),
		func(svc *DiscoveryService, p *Provider) ([]*model.Model, error) {
			return svc.discoverDeepSeek(context.Background(), p, "wrong-api-key")
		})
}

func TestDiscoverDeepSeek_InvalidResponse(t *testing.T) {
	assertDiscoverHTTPError(t, "invalid JSON", invalidJSONHandler(),
		func(svc *DiscoveryService, p *Provider) ([]*model.Model, error) {
			return svc.discoverDeepSeek(context.Background(), p, "test-api-key")
		})
}

func TestDiscoverDeepSeek_EmptyResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := OpenAIModelsResponse{
			Object: "list",
			Data:   []OpenAIModel{},
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

	models, err := service.discoverDeepSeek(context.Background(), provider, "test-api-key")
	if err != nil {
		t.Fatalf("discoverDeepSeek failed: %v", err)
	}
	// Empty-but-successful listing returns empty (no catalog union), so the
	// discovered set stays empty and RecordMissingModels is a no-op.
	if len(models) != 0 {
		t.Errorf("Expected 0 models for empty live response, got %d", len(models))
	}
}

func TestDiscoverDeepSeek_CatalogOverride(t *testing.T) {
	// Test that a known catalog model (deepseek-v4-flash) gets catalog pricing/limits
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" || r.Method != "GET" {
			http.NotFound(w, r)
			return
		}
		response := OpenAIModelsResponse{
			Object: "list",
			Data: []OpenAIModel{
				{
					ID:      "deepseek-v4-flash",
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

	service := &DiscoveryService{
		httpClient: server.Client(),
	}

	provider := &Provider{
		ID:      uuid.New(),
		BaseURL: server.URL,
	}

	models, err := service.discoverDeepSeek(context.Background(), provider, "test-api-key")
	if err != nil {
		t.Fatalf("discoverDeepSeek failed: %v", err)
	}
	// Live v4-flash merges with its catalog entry; v4-pro unions in.
	if len(models) != len(GetDeepSeekModels()) {
		t.Fatalf("Expected catalog-count merged models, got %d", len(models))
	}
	var m *model.Model
	for _, mm := range models {
		if mm.ModelID == "deepseek-v4-flash" {
			m = mm
		}
	}
	if m == nil {
		t.Fatal("expected deepseek-v4-flash in merged results")
	}
	// deepseek-v4-flash should have catalog values for context length and pricing
	if m.ContextLength == nil {
		t.Error("Expected ContextLength to be set from catalog for deepseek-chat")
	}
	if m.MaxOutputTokens == nil {
		t.Error("Expected MaxOutputTokens to be set from catalog for deepseek-chat")
	}
	if m.InputPricePerMillion == nil {
		t.Error("Expected InputPricePerMillion to be set from catalog for deepseek-chat")
	}
	if m.OutputPricePerMillion == nil {
		t.Error("Expected OutputPricePerMillion to be set from catalog for deepseek-chat")
	}
	// Catalog should provide InputPricePerMillionCacheHit
	if m.InputPricePerMillionCacheHit == nil {
		t.Error("Expected InputPricePerMillionCacheHit to be set from catalog for deepseek-chat")
	}
}

func TestDiscoverDeepSeek_UnknownModel_NoHardcodedDefault(t *testing.T) {
	// A model NOT in the catalog no longer gets a hardcoded 128k/8k default; it
	// becomes a clean stub (context/max-output nil) that models.dev fills in
	// production. In this unit test (no models.dev) the fields stay nil.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := OpenAIModelsResponse{
			Object: "list",
			Data: []OpenAIModel{
				{
					ID:      "deepseek-unknown-future-model",
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

	service := &DiscoveryService{
		httpClient: server.Client(),
	}

	provider := &Provider{
		ID:      uuid.New(),
		BaseURL: server.URL,
	}

	models, err := service.discoverDeepSeek(context.Background(), provider, "test-api-key")
	if err != nil {
		t.Fatalf("discoverDeepSeek failed: %v", err)
	}
	// Unknown live model unioned with the catalog.
	if len(models) != len(GetDeepSeekModels())+1 {
		t.Fatalf("Expected catalog+1 merged models, got %d", len(models))
	}
	var m *model.Model
	for _, mm := range models {
		if mm.ModelID == "deepseek-unknown-future-model" {
			m = mm
		}
	}
	if m == nil {
		t.Fatal("expected unknown live model in merged results")
	}
	// No hardcoded default: stub leaves context/max-output nil for models.dev.
	if m.ContextLength != nil {
		t.Errorf("expected nil ContextLength (no hardcoded default), got %v", *m.ContextLength)
	}
	if m.MaxOutputTokens != nil {
		t.Errorf("expected nil MaxOutputTokens (no hardcoded default), got %v", *m.MaxOutputTokens)
	}
}

func TestDiscoverDeepSeek_ConnectionError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	server.Close()

	service := &DiscoveryService{
		httpClient: server.Client(),
	}

	provider := &Provider{
		ID:      uuid.New(),
		BaseURL: server.URL,
	}

	_, err := service.discoverDeepSeek(context.Background(), provider, "test-api-key")
	if err == nil {
		t.Error("Expected error for connection failure, got nil")
	}
}

func TestDiscoverDeepSeek_ServerError(t *testing.T) {
	assertDiscoverHTTPError(t, "500 response", errorStatusHandler(http.StatusInternalServerError),
		func(svc *DiscoveryService, p *Provider) ([]*model.Model, error) {
			return svc.discoverDeepSeek(context.Background(), p, "test-api-key")
		})
}

func TestDiscoverDeepSeek_CapabilitiesSet(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := OpenAIModelsResponse{
			Object: "list",
			Data: []OpenAIModel{
				{
					ID:      "deepseek-v4-flash",
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

	service := &DiscoveryService{
		httpClient: server.Client(),
	}

	provider := &Provider{
		ID:      uuid.New(),
		BaseURL: server.URL,
	}

	models, err := service.discoverDeepSeek(context.Background(), provider, "test-api-key")
	if err != nil {
		t.Fatalf("discoverDeepSeek failed: %v", err)
	}
	// Streaming + ToolCalling come from the catalog backfill (OR-merged into the
	// live stub), so use a catalog model id.
	var m *model.Model
	for _, mm := range models {
		if mm.ModelID == "deepseek-v4-flash" {
			m = mm
		}
	}
	if m == nil {
		t.Fatal("expected deepseek-v4-flash in merged results")
	}

	var caps model.Capability
	if err := json.Unmarshal([]byte(m.Capabilities), &caps); err != nil {
		t.Fatalf("Failed to unmarshal capabilities: %v", err)
	}
	if !caps.Streaming {
		t.Error("Expected Streaming capability to be true")
	}
	if !caps.ToolCalling {
		t.Error("Expected ToolCalling capability to be true")
	}
}

func TestDiscoverDeepSeek_CatalogReasoning(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := OpenAIModelsResponse{
			Object: "list",
			Data: []OpenAIModel{
				{
					ID:      "deepseek-v4-pro",
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

	service := &DiscoveryService{
		httpClient: server.Client(),
	}

	provider := &Provider{
		ID:      uuid.New(),
		BaseURL: server.URL,
	}

	models, err := service.discoverDeepSeek(context.Background(), provider, "test-api-key")
	if err != nil {
		t.Fatalf("discoverDeepSeek failed: %v", err)
	}
	// Live v4-pro (first) merges with its catalog entry; v4-flash unions in.
	if len(models) != len(GetDeepSeekModels()) {
		t.Fatalf("Expected catalog-count merged models, got %d", len(models))
	}
	var m *model.Model
	for _, mm := range models {
		if mm.ModelID == "deepseek-v4-pro" {
			m = mm
		}
	}
	if m == nil {
		t.Fatal("expected deepseek-v4-pro in merged results")
	}

	var caps model.Capability
	if err := json.Unmarshal([]byte(m.Capabilities), &caps); err != nil {
		t.Fatalf("Failed to unmarshal capabilities: %v", err)
	}
	if !caps.Reasoning {
		t.Error("Expected Reasoning capability to be true for deepseek-v4-pro from catalog")
	}
}
