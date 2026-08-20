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

	// A 404 listing falls back to the catalog without erroring. The catalog is
	// an override channel that is normally empty, so this is exactly its
	// current (possibly zero) row count — never an aborted scan.
	if len(models) != len(GetOpenCodeGoCatalog()) {
		t.Errorf("Expected the catalog rows from fallback after 404, got %d models", len(models))
	}

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

// Catalog-entry backfill onto a live model is covered by catalog_merge_test.go
// and the OpenCode Zen discovery tests; the Go catalog is an override channel
// that is normally empty, so there is no shipped row to assert against here.

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
