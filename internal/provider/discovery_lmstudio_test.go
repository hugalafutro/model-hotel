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
// discoverLMStudio
// ---------------------------------------------------------------------------

func TestDiscoverLMStudio_SuccessMultiple(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"object":"list",
			"data":[
				{"id":"llama-3-8b","object":"model","created":1700000000,"owned_by":"meta"},
				{"id":"mistral-7b","object":"model","created":1700000001,"owned_by":"mistral"}
			]
		}`))
	}))
	defer srv.Close()

	svc := &DiscoveryService{httpClient: srv.Client()}
	provider := &Provider{
		ID:      uuid.New(),
		BaseURL: srv.URL,
	}

	models, err := svc.discoverLMStudio(context.Background(), provider, "")
	if err != nil {
		t.Fatalf("discoverLMStudio failed: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("expected 2 models, got %d", len(models))
	}
	if models[0].ModelID != "llama-3-8b" {
		t.Errorf("expected first model 'llama-3-8b', got %q", models[0].ModelID)
	}
	if models[0].OwnedBy != "meta" {
		t.Errorf("expected first model owned_by 'meta', got %q", models[0].OwnedBy)
	}
	if models[1].ModelID != "mistral-7b" {
		t.Errorf("expected second model 'mistral-7b', got %q", models[1].ModelID)
	}
	if models[1].OwnedBy != "mistral" {
		t.Errorf("expected second model owned_by 'mistral', got %q", models[1].OwnedBy)
	}
	if models[0].ProviderID != provider.ID {
		t.Errorf("expected provider ID %v, got %v", provider.ID, models[0].ProviderID)
	}

	// verify capabilities
	var caps model.Capability
	if err := json.Unmarshal([]byte(models[0].Capabilities), &caps); err != nil {
		t.Fatalf("failed to unmarshal capabilities: %v", err)
	}
	if !caps.Streaming {
		t.Error("expected Streaming=true")
	}
	if !caps.StructuredOutput {
		t.Error("expected StructuredOutput=true")
	}
}

func TestDiscoverLMStudio_EmptyModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[]}`))
	}))
	defer srv.Close()

	svc := &DiscoveryService{httpClient: srv.Client()}
	provider := &Provider{
		ID:      uuid.New(),
		BaseURL: srv.URL,
	}

	models, err := svc.discoverLMStudio(context.Background(), provider, "")
	if err != nil {
		t.Fatalf("discoverLMStudio should not error on empty list: %v", err)
	}
	if len(models) != 0 {
		t.Errorf("expected 0 models, got %d", len(models))
	}
}

func TestDiscoverLMStudio_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`bad gateway`))
	}))
	defer srv.Close()

	svc := &DiscoveryService{httpClient: srv.Client()}
	provider := &Provider{
		ID:      uuid.New(),
		BaseURL: srv.URL,
	}

	_, err := svc.discoverLMStudio(context.Background(), provider, "")
	if err == nil {
		t.Fatal("expected error for non-200 status")
	}
}

func TestDiscoverLMStudio_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`not json`))
	}))
	defer srv.Close()

	svc := &DiscoveryService{httpClient: srv.Client()}
	provider := &Provider{
		ID:      uuid.New(),
		BaseURL: srv.URL,
	}

	_, err := svc.discoverLMStudio(context.Background(), provider, "")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestDiscoverLMStudio_ModelWithEmptyOwnedBy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"object":"list",
			"data":[
				{"id":"custom-model","object":"model","created":1700000000,"owned_by":""}
			]
		}`))
	}))
	defer srv.Close()

	svc := &DiscoveryService{httpClient: srv.Client()}
	provider := &Provider{
		ID:      uuid.New(),
		BaseURL: srv.URL,
	}

	models, err := svc.discoverLMStudio(context.Background(), provider, "")
	if err != nil {
		t.Fatalf("discoverLMStudio failed: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("expected 1 model, got %d", len(models))
	}
	if models[0].OwnedBy != "lmstudio" {
		t.Errorf("expected owned_by to default to 'lmstudio', got %q", models[0].OwnedBy)
	}
}

func TestDiscoverLMStudio_SendsAPIKey(t *testing.T) {
	var authHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[]}`))
	}))
	defer srv.Close()

	svc := &DiscoveryService{httpClient: srv.Client()}
	provider := &Provider{
		ID:      uuid.New(),
		BaseURL: srv.URL,
	}

	_, err := svc.discoverLMStudio(context.Background(), provider, "sk-test-key")
	if err != nil {
		t.Fatalf("discoverLMStudio failed: %v", err)
	}
	if authHeader != "Bearer sk-test-key" {
		t.Errorf("expected Authorization header 'Bearer sk-test-key', got %q", authHeader)
	}
}

func TestDiscoverLMStudio_NoAPIKey(t *testing.T) {
	var authHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[]}`))
	}))
	defer srv.Close()

	svc := &DiscoveryService{httpClient: srv.Client()}
	provider := &Provider{
		ID:      uuid.New(),
		BaseURL: srv.URL,
	}

	_, err := svc.discoverLMStudio(context.Background(), provider, "")
	if err != nil {
		t.Fatalf("discoverLMStudio failed: %v", err)
	}
	if authHeader != "" {
		t.Errorf("expected no Authorization header when apiKey is empty, got %q", authHeader)
	}
}
