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

// ---------------------------------------------------------------------------
// discoverOpenCodeGo — additional paths not in discovery_http_test.go
// ---------------------------------------------------------------------------

func TestDiscoverOpenCodeGo_404FallsBackToCatalog(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" || r.URL.Path == "/models" {
			http.Error(w, "Not Found", http.StatusNotFound)
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
		Name:    "test-opencode-go",
		BaseURL: server.URL + "/v1",
	}

	models, err := service.discoverOpenCodeGo(context.Background(), provider, "test-api-key")
	if err != nil {
		t.Fatalf("discoverOpenCodeGo failed: %v", err)
	}

	// Should get catalog models via fallback
	if len(models) == 0 {
		t.Error("Expected catalog models from fallback after 404")
	}

	// Verify models have catalog data
	for _, m := range models {
		if m.ProviderID != provider.ID {
			t.Errorf("ProviderID = %v, want %v", m.ProviderID, provider.ID)
		}
		if !m.Enabled {
			t.Error("Expected model to be enabled")
		}
	}
}

func TestDiscoverOpenCodeGo_ConnectionError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	server.Close()

	service := &DiscoveryService{
		httpClient: server.Client(),
	}

	provider := &Provider{
		ID:      uuid.New(),
		Name:    "test-opencode-go",
		BaseURL: server.URL,
	}

	_, err := service.discoverOpenCodeGo(context.Background(), provider, "test-api-key")
	if err == nil {
		t.Error("Expected error for connection failure, got nil")
	}
}

func TestDiscoverOpenCodeGo_UnknownModel_MinimalEntry(t *testing.T) {
	// Test that a model not in the catalog gets a minimal entry
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := OpenAIModelsResponse{
			Object: "list",
			Data: []OpenAIModel{
				{ID: "future-unknown-model-xyz", Object: "model", OwnedBy: "opencode"},
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
		Name:    "test-opencode-go",
		BaseURL: server.URL,
	}

	models, err := service.discoverOpenCodeGo(context.Background(), provider, "test-api-key")
	if err != nil {
		t.Fatalf("discoverOpenCodeGo failed: %v", err)
	}
	// The unknown live model is unioned with the catalog.
	if len(models) != len(GetOpenCodeGoCatalog())+1 {
		t.Fatalf("Expected catalog+1 merged models, got %d", len(models))
	}

	var m *model.Model
	for _, mm := range models {
		if mm.ModelID == "future-unknown-model-xyz" {
			m = mm
		}
	}
	if m == nil {
		t.Fatal("expected unknown live model present in merged results")
	}
	if m.OwnedBy != "opencode" {
		t.Errorf("Expected OwnedBy 'opencode', got %q", m.OwnedBy)
	}

	// Unknown model should have streaming capability only
	var caps model.Capability
	if err := json.Unmarshal([]byte(m.Capabilities), &caps); err != nil {
		t.Fatalf("Failed to unmarshal capabilities: %v", err)
	}
	if !caps.Streaming {
		t.Error("Expected Streaming capability to be true")
	}
}

func TestDiscoverOpenCodeGo_CatalogModelPopulated(t *testing.T) {
	// Verify that a model in the catalog gets the full catalog treatment
	catalog := GetOpenCodeGoCatalog()
	if len(catalog) == 0 {
		t.Fatal("No models in OpenCode Go catalog")
	}
	firstCatalogModel := catalog[0].ModelID

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := OpenAIModelsResponse{
			Object: "list",
			Data: []OpenAIModel{
				{ID: firstCatalogModel, Object: "model", OwnedBy: "opencode"},
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
		Name:    "test-opencode-go",
		BaseURL: server.URL,
	}

	models, err := service.discoverOpenCodeGo(context.Background(), provider, "test-api-key")
	if err != nil {
		t.Fatalf("discoverOpenCodeGo failed: %v", err)
	}
	// The live catalog model merges with its catalog entry (no new union member).
	if len(models) != len(GetOpenCodeGoCatalog()) {
		t.Fatalf("Expected catalog-count merged models, got %d", len(models))
	}

	var m *model.Model
	for _, mm := range models {
		if mm.ModelID == firstCatalogModel {
			m = mm
		}
	}
	if m == nil {
		t.Fatalf("expected %q present in merged results", firstCatalogModel)
	}
	// Catalog model should have ContextLength + MaxOutputTokens backfilled.
	if m.ContextLength == nil {
		t.Error("Expected ContextLength to be set from catalog")
	}
	if m.MaxOutputTokens == nil {
		t.Error("Expected MaxOutputTokens to be set from catalog")
	}
}

func TestDiscoverOpenCodeGo_Unauthorized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
	}))
	defer server.Close()

	service := &DiscoveryService{
		httpClient: server.Client(),
	}

	provider := &Provider{
		ID:      uuid.New(),
		Name:    "test-opencode-go",
		BaseURL: server.URL,
	}

	_, err := service.discoverOpenCodeGo(context.Background(), provider, "wrong-key")
	if err == nil {
		t.Error("Expected error for unauthorized request, got nil")
	}
}
