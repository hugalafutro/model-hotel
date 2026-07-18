package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestDetectProviderType_VertexExpress(t *testing.T) {
	for _, u := range []string{
		"https://aiplatform.googleapis.com",
		"https://aiplatform.googleapis.com/v1",
		"https://us-central1-aiplatform.googleapis.com",
	} {
		if got := DetectProviderType(u); got != "vertex-express" {
			t.Errorf("DetectProviderType(%s) = %q, want vertex-express", u, got)
		}
	}
	// AI Studio stays on the google type (OpenAI-compat surface + own listing).
	if got := DetectProviderType("https://generativelanguage.googleapis.com/v1beta/openai"); got != "google" {
		t.Errorf("generativelanguage = %q, want google", got)
	}
	// Lookalike host on an unrelated domain must not match.
	if got := DetectProviderType("https://aiplatform.googleapis.com.evil.example"); got == "vertex-express" {
		t.Error("evil-suffix domain detected as vertex-express")
	}
}

// vertexProbeServer serves countTokens probes: eligible model IDs get 200,
// unknown ones 404, mirroring live Vertex express behavior (2026-07-18).
func vertexProbeServer(t *testing.T, eligible ...string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || !strings.HasPrefix(r.URL.Path, "/v1/publishers/google/models/") {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("x-goog-api-key") != "test-api-key" {
			http.Error(w, `{"error":{"code":401}}`, http.StatusUnauthorized)
			return
		}
		rest := strings.TrimPrefix(r.URL.Path, "/v1/publishers/google/models/")
		modelID, action, _ := strings.Cut(rest, ":")
		if action != "countTokens" {
			http.NotFound(w, r)
			return
		}
		if slices.Contains(eligible, modelID) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"totalTokens":1}`))
			return
		}
		http.NotFound(w, r)
	}))
}

func TestDiscoverVertexExpress_ProbesCatalog(t *testing.T) {
	server := vertexProbeServer(t, "gemini-2.5-flash", "gemini-2.5-pro")
	defer server.Close()

	service := &DiscoveryService{httpClient: server.Client()}
	provider := &Provider{ID: uuid.New(), BaseURL: server.URL}

	models, err := service.discoverVertexExpress(context.Background(), provider, "test-api-key")
	if err != nil {
		t.Fatalf("discoverVertexExpress failed: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("models = %d, want the 2 eligible catalog entries", len(models))
	}
	// Catalog order is preserved: pro sorts before flash in the shipped list.
	if models[0].ModelID != "gemini-2.5-pro" || models[1].ModelID != "gemini-2.5-flash" {
		t.Errorf("models = %s, %s; want catalog order pro, flash", models[0].ModelID, models[1].ModelID)
	}
	for _, m := range models {
		if m.OwnedBy != "google" || !m.Enabled {
			t.Errorf("model %s: OwnedBy=%q Enabled=%v", m.ModelID, m.OwnedBy, m.Enabled)
		}
	}
}

func TestDiscoverVertexExpress_NoneEligible(t *testing.T) {
	server := vertexProbeServer(t) // every probe 404s
	defer server.Close()

	service := &DiscoveryService{httpClient: server.Client()}
	provider := &Provider{ID: uuid.New(), BaseURL: server.URL}

	models, err := service.discoverVertexExpress(context.Background(), provider, "test-api-key")
	if err != nil {
		t.Fatalf("discoverVertexExpress failed: %v", err)
	}
	if len(models) != 0 {
		t.Errorf("models = %d, want 0 when nothing is eligible", len(models))
	}
}

func TestDiscoverVertexExpress_Unauthorized(t *testing.T) {
	server := vertexProbeServer(t, "gemini-2.5-flash")
	defer server.Close()

	service := &DiscoveryService{httpClient: server.Client()}
	provider := &Provider{ID: uuid.New(), BaseURL: server.URL}

	// A bad key must fail discovery loudly, not report zero eligible models.
	if _, err := service.discoverVertexExpress(context.Background(), provider, "wrong-key"); err == nil {
		t.Error("expected error for unauthorized key")
	}
}

func TestDiscoverVertexExpress_InvalidBaseURL(t *testing.T) {
	service := &DiscoveryService{httpClient: http.DefaultClient}
	provider := &Provider{ID: uuid.New(), BaseURL: "not a url"}

	if _, err := service.discoverVertexExpress(context.Background(), provider, "k"); err == nil {
		t.Error("expected error for invalid base URL")
	}
}
