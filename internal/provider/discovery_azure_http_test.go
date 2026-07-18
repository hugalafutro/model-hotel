package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/model"
)

func TestDiscoverAzure_ProjectRoute(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/projects/myproject/deployments" || r.Method != "GET" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("api-version") != "v1" {
			http.Error(w, "missing api-version", http.StatusBadRequest)
			return
		}
		if r.Header.Get("Authorization") != "Bearer test-api-key" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"value":[` +
			`{"name":"Kimi-K2.6","type":"ModelDeployment","modelName":"Kimi-K2.6","modelVersion":"2026-04-20","modelPublisher":"MoonshotAI","capabilities":{"chat_completion":"true"},"sku":{"name":"GlobalStandard","capacity":20}},` +
			`{"name":"my-fast-gpt","type":"ModelDeployment","modelName":"gpt-4.1-mini","modelVersion":"2025-04-14","modelPublisher":"OpenAI","capabilities":{"chat_completion":"true"}},` +
			`{"name":"some-connection","type":"ConnectionDeployment","modelName":"","modelPublisher":""}]}`))
	}))
	defer server.Close()

	service := &DiscoveryService{httpClient: server.Client()}
	provider := &Provider{
		ID:      uuid.New(),
		BaseURL: server.URL + "/api/projects/myproject",
	}

	models, err := service.discoverAzure(context.Background(), provider, "test-api-key")
	if err != nil {
		t.Fatalf("discoverAzure failed: %v", err)
	}

	// Non-model deployment entries must be skipped.
	if len(models) != 2 {
		t.Fatalf("Expected 2 models, got %d", len(models))
	}

	// ModelID is the deployment name (the only invokable identifier); Name
	// carries the underlying base-model name so enrichment can match aliased
	// deployments against models.dev.
	if models[0].ModelID != "Kimi-K2.6" || models[0].Name != "Kimi-K2.6" {
		t.Errorf("model[0] = %q/%q, want Kimi-K2.6/Kimi-K2.6", models[0].ModelID, models[0].Name)
	}
	if models[0].OwnedBy != "MoonshotAI" {
		t.Errorf("model[0] OwnedBy = %q, want MoonshotAI", models[0].OwnedBy)
	}
	if models[1].ModelID != "my-fast-gpt" {
		t.Errorf("model[1] ModelID = %q, want my-fast-gpt", models[1].ModelID)
	}
	if models[1].Name != "gpt-4.1-mini" {
		t.Errorf("model[1] Name = %q, want gpt-4.1-mini", models[1].Name)
	}
	for _, m := range models {
		if !m.Enabled {
			t.Errorf("Expected model %s to be enabled", m.ModelID)
		}
	}
}

func TestDiscoverAzure_LegacyRoute(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/openai/deployments" || r.Method != "GET" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("api-version") != "2023-03-15-preview" {
			http.Error(w, "unsupported api-version", http.StatusNotFound)
			return
		}
		if r.Header.Get("Authorization") != "Bearer test-api-key" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[` +
			`{"id":"Kimi-K2.6","model":"Kimi-K2.6","status":"succeeded","object":"deployment"},` +
			`{"id":"my-fast-gpt","model":"gpt-4.1-mini","status":"succeeded","object":"deployment"},` +
			`{"id":"half-baked","model":"gpt-4o","status":"canceled","object":"deployment"}]}`))
	}))
	defer server.Close()

	service := &DiscoveryService{httpClient: server.Client()}

	// Both non-project base URL shapes must reach the same legacy listing:
	// the bare resource root and an /openai/v1 base.
	for _, base := range []string{server.URL, server.URL + "/openai/v1"} {
		provider := &Provider{ID: uuid.New(), BaseURL: base}

		models, err := service.discoverAzure(context.Background(), provider, "test-api-key")
		if err != nil {
			t.Fatalf("discoverAzure(%s) failed: %v", base, err)
		}
		// Non-succeeded deployments are not invokable and must be skipped.
		if len(models) != 2 {
			t.Fatalf("discoverAzure(%s): expected 2 models, got %d", base, len(models))
		}
		if models[0].ModelID != "Kimi-K2.6" {
			t.Errorf("model[0] ModelID = %q, want Kimi-K2.6", models[0].ModelID)
		}
		if models[1].ModelID != "my-fast-gpt" || models[1].Name != "gpt-4.1-mini" {
			t.Errorf("model[1] = %q/%q, want my-fast-gpt/gpt-4.1-mini", models[1].ModelID, models[1].Name)
		}
	}
}

func TestDiscoverAzure_Unauthorized(t *testing.T) {
	assertDiscoverHTTPError(t, "unauthorized request", errorStatusHandler(http.StatusUnauthorized),
		func(svc *DiscoveryService, p *Provider) ([]*model.Model, error) {
			return svc.discoverAzure(context.Background(), p, "wrong-api-key")
		})
}

func TestDiscoverAzure_InvalidResponse(t *testing.T) {
	assertDiscoverHTTPError(t, "invalid JSON", invalidJSONHandler(),
		func(svc *DiscoveryService, p *Provider) ([]*model.Model, error) {
			return svc.discoverAzure(context.Background(), p, "test-api-key")
		})
}

func TestDiscoverAzure_ProjectRouteErrors(t *testing.T) {
	// The shared error helpers hit the legacy route (bare-root base URL); the
	// project route has its own fetch/decode error branches.
	tests := []struct {
		name    string
		handler http.HandlerFunc
	}{
		{"unauthorized", errorStatusHandler(http.StatusUnauthorized)},
		{"invalid JSON", invalidJSONHandler()},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(tc.handler)
			defer server.Close()

			service := &DiscoveryService{httpClient: server.Client()}
			provider := &Provider{ID: uuid.New(), BaseURL: server.URL + "/api/projects/myproject"}

			if _, err := service.discoverAzure(context.Background(), provider, "test-api-key"); err == nil {
				t.Errorf("Expected error for %s, got nil", tc.name)
			}
		})
	}
}

func TestDiscoverAzure_InvalidBaseURL(t *testing.T) {
	service := &DiscoveryService{httpClient: http.DefaultClient}
	provider := &Provider{ID: uuid.New(), BaseURL: "not a url"}

	if _, err := service.discoverAzure(context.Background(), provider, "test-api-key"); err == nil {
		t.Error("Expected error for invalid base URL, got nil")
	}
}

func TestDiscoverAzure_EmptyResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"value":[]}`))
	}))
	defer server.Close()

	service := &DiscoveryService{httpClient: server.Client()}
	provider := &Provider{ID: uuid.New(), BaseURL: server.URL + "/api/projects/empty"}

	models, err := service.discoverAzure(context.Background(), provider, "test-api-key")
	if err != nil {
		t.Fatalf("discoverAzure failed: %v", err)
	}
	if len(models) != 0 {
		t.Errorf("Expected 0 models for empty listing, got %d", len(models))
	}
}
