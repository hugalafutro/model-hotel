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

func TestDiscoverBedrock(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" || r.Method != "GET" {
			http.NotFound(w, r)
			return
		}

		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-api-key" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		response := OpenAIModelsResponse{
			Object: "list",
			Data: []OpenAIModel{
				{ID: "openai.gpt-oss-120b", Object: "model", Created: 1234567890, OwnedBy: "system"},
				{ID: "anthropic.claude-sonnet-5", Object: "model", Created: 1234567890, OwnedBy: "system"},
				{ID: "qwen.qwen3-32b", Object: "model", Created: 1234567890, OwnedBy: "system"},
				{ID: "anthropic.claude-haiku-4-5", Object: "model", Created: 1234567890, OwnedBy: "system"},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	service := &DiscoveryService{
		httpClient: server.Client(),
	}

	provider := &Provider{
		ID:      uuid.New(),
		BaseURL: server.URL + "/v1",
	}

	models, err := service.discoverBedrock(context.Background(), provider, "test-api-key")
	if err != nil {
		t.Fatalf("discoverBedrock failed: %v", err)
	}

	// anthropic.* models speak only the Anthropic Messages dialect on Bedrock
	// (they reject /v1/chat/completions), which the proxy cannot serve yet, so
	// discovery must skip them instead of exposing models that 400 on use.
	if len(models) != 2 {
		t.Fatalf("Expected 2 models (anthropic.* skipped), got %d", len(models))
	}

	if models[0].ModelID != "openai.gpt-oss-120b" {
		t.Errorf("Expected model ID 'openai.gpt-oss-120b', got '%s'", models[0].ModelID)
	}
	if models[1].ModelID != "qwen.qwen3-32b" {
		t.Errorf("Expected model ID 'qwen.qwen3-32b', got '%s'", models[1].ModelID)
	}

	for _, m := range models {
		if !m.Enabled {
			t.Errorf("Expected model %s to be enabled", m.ModelID)
		}
		if m.OwnedBy != "system" {
			t.Errorf("Expected ownedBy 'system' for %s, got '%s'", m.ModelID, m.OwnedBy)
		}
	}
}

func TestDiscoverBedrock_Unauthorized(t *testing.T) {
	assertDiscoverHTTPError(t, "unauthorized request", errorStatusHandler(http.StatusUnauthorized),
		func(svc *DiscoveryService, p *Provider) ([]*model.Model, error) {
			return svc.discoverBedrock(context.Background(), p, "wrong-api-key")
		})
}

func TestDiscoverBedrock_InvalidResponse(t *testing.T) {
	assertDiscoverHTTPError(t, "invalid JSON", invalidJSONHandler(),
		func(svc *DiscoveryService, p *Provider) ([]*model.Model, error) {
			return svc.discoverBedrock(context.Background(), p, "test-api-key")
		})
}

func TestDiscoverBedrock_EmptyResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := OpenAIModelsResponse{
			Object: "list",
			Data:   []OpenAIModel{},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	service := &DiscoveryService{httpClient: server.Client()}
	provider := &Provider{ID: uuid.New(), BaseURL: server.URL + "/v1"}

	models, err := service.discoverBedrock(context.Background(), provider, "test-api-key")
	if err != nil {
		t.Fatalf("discoverBedrock failed: %v", err)
	}
	if len(models) != 0 {
		t.Errorf("Expected 0 models for empty listing, got %d", len(models))
	}
}
