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
// discoverLMStudio — native /api/v0/models path
// ---------------------------------------------------------------------------

// nativeBody is a representative LM Studio /api/v0/models payload with one model
// of each type. Text-embedding-nomic is an embeddings model, qwen2-vl a vlm.
const lmStudioNativeBody = `{
	"object":"list",
	"data":[
		{"id":"llama-3-8b","object":"model","type":"llm","publisher":"meta","arch":"llama","max_context_length":8192},
		{"id":"qwen2-vl-7b","object":"model","type":"vlm","publisher":"qwen","arch":"qwen2_vl","max_context_length":32768},
		{"id":"text-embedding-nomic-embed-text-v1.5","object":"model","type":"embeddings","publisher":"nomic-ai","max_context_length":2048}
	]
}`

func TestDiscoverLMStudio_Native_TypesAndContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v0/models" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(lmStudioNativeBody))
	}))
	defer srv.Close()

	svc := &DiscoveryService{httpClient: srv.Client()}
	provider := &Provider{ID: uuid.New(), BaseURL: srv.URL + "/v1"}

	models, err := svc.discoverLMStudio(context.Background(), provider, "")
	if err != nil {
		t.Fatalf("discoverLMStudio failed: %v", err)
	}
	if len(models) != 3 {
		t.Fatalf("expected 3 models, got %d", len(models))
	}

	byID := make(map[string]*model.Model, len(models))
	for _, m := range models {
		byID[m.ModelID] = m
	}

	llm := byID["llama-3-8b"]
	if llm.Modality != "text" {
		t.Errorf("llm modality: got %q, want text", llm.Modality)
	}
	if llm.ContextLength == nil || *llm.ContextLength != 8192 {
		t.Errorf("llm context length: got %v, want 8192", llm.ContextLength)
	}
	if llm.OwnedBy != "meta" {
		t.Errorf("llm owned_by: got %q, want meta", llm.OwnedBy)
	}

	vlm := byID["qwen2-vl-7b"]
	if vlm.Modality != "vision" {
		t.Errorf("vlm modality: got %q, want vision", vlm.Modality)
	}
	var vcaps model.Capability
	_ = json.Unmarshal([]byte(vlm.Capabilities), &vcaps)
	if !vcaps.Vision {
		t.Error("vlm should have Vision capability")
	}

	emb := byID["text-embedding-nomic-embed-text-v1.5"]
	if emb.Modality != "embedding" {
		t.Errorf("embedding modality: got %q, want embedding", emb.Modality)
	}
	if emb.OutputModalities != `["embedding"]` {
		t.Errorf("embedding output modalities: got %q", emb.OutputModalities)
	}
}

func TestDiscoverLMStudio_Native_SendsAPIKey(t *testing.T) {
	var authHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(lmStudioNativeBody))
	}))
	defer srv.Close()

	svc := &DiscoveryService{httpClient: srv.Client()}
	provider := &Provider{ID: uuid.New(), BaseURL: srv.URL + "/v1"}

	if _, err := svc.discoverLMStudio(context.Background(), provider, "sk-test-key"); err != nil {
		t.Fatalf("discoverLMStudio failed: %v", err)
	}
	if authHeader != "Bearer sk-test-key" {
		t.Errorf("expected Authorization header 'Bearer sk-test-key', got %q", authHeader)
	}
}

// ---------------------------------------------------------------------------
// discoverLMStudio — fallback to OpenAI-compatible /v1/models
// ---------------------------------------------------------------------------

// lmStudioFallbackHandler serves 404 on the native endpoint and the given
// OpenAI-compatible body on /models, forcing the fallback path.
func lmStudioFallbackHandler(t *testing.T, openaiBody string) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v0/models":
			w.WriteHeader(http.StatusNotFound)
		case "/v1/models":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(openaiBody))
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}
}

func TestDiscoverLMStudio_Fallback_SuccessMultiple(t *testing.T) {
	body := `{
		"object":"list",
		"data":[
			{"id":"llama-3-8b","object":"model","created":1700000000,"owned_by":"meta"},
			{"id":"mistral-7b","object":"model","created":1700000001,"owned_by":"mistral"}
		]
	}`
	srv := httptest.NewServer(lmStudioFallbackHandler(t, body))
	defer srv.Close()

	svc := &DiscoveryService{httpClient: srv.Client()}
	provider := &Provider{ID: uuid.New(), BaseURL: srv.URL + "/v1"}

	models, err := svc.discoverLMStudio(context.Background(), provider, "")
	if err != nil {
		t.Fatalf("discoverLMStudio failed: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("expected 2 models, got %d", len(models))
	}
	if models[0].ModelID != "llama-3-8b" || models[0].OwnedBy != "meta" {
		t.Errorf("unexpected first model: %+v", models[0])
	}
	if models[1].ModelID != "mistral-7b" || models[1].OwnedBy != "mistral" {
		t.Errorf("unexpected second model: %+v", models[1])
	}
	if models[0].Modality != "text" {
		t.Errorf("chat model modality: got %q, want text", models[0].Modality)
	}

	var caps model.Capability
	if err := json.Unmarshal([]byte(models[0].Capabilities), &caps); err != nil {
		t.Fatalf("failed to unmarshal capabilities: %v", err)
	}
	if !caps.Streaming || !caps.StructuredOutput {
		t.Errorf("expected Streaming and StructuredOutput, got %+v", caps)
	}
}

func TestDiscoverLMStudio_Fallback_EmbeddingByName(t *testing.T) {
	body := `{
		"object":"list",
		"data":[
			{"id":"llama-3-8b","object":"model","owned_by":"meta"},
			{"id":"nomic-embed-text-v1.5","object":"model","owned_by":"nomic"},
			{"id":"bge-reranker-v2-m3","object":"model","owned_by":"baai"}
		]
	}`
	srv := httptest.NewServer(lmStudioFallbackHandler(t, body))
	defer srv.Close()

	svc := &DiscoveryService{httpClient: srv.Client()}
	provider := &Provider{ID: uuid.New(), BaseURL: srv.URL + "/v1"}

	models, err := svc.discoverLMStudio(context.Background(), provider, "")
	if err != nil {
		t.Fatalf("discoverLMStudio failed: %v", err)
	}

	byID := make(map[string]*model.Model, len(models))
	for _, m := range models {
		byID[m.ModelID] = m
	}
	if got := byID["llama-3-8b"].Modality; got != "text" {
		t.Errorf("chat model modality: got %q, want text", got)
	}
	emb := byID["nomic-embed-text-v1.5"]
	if emb.Modality != "embedding" {
		t.Errorf("embedding modality: got %q, want embedding", emb.Modality)
	}
	if emb.OutputModalities != `["embedding"]` {
		t.Errorf("embedding output modalities: got %q, want [\"embedding\"]", emb.OutputModalities)
	}
	rer := byID["bge-reranker-v2-m3"]
	if rer.Modality != "rerank" {
		t.Errorf("reranker modality: got %q, want rerank", rer.Modality)
	}
	if rer.OutputModalities != `["rerank"]` {
		t.Errorf("reranker output modalities: got %q, want [\"rerank\"]", rer.OutputModalities)
	}
}

func TestDiscoverLMStudio_Fallback_ModelWithEmptyOwnedBy(t *testing.T) {
	body := `{"object":"list","data":[{"id":"custom-model","object":"model","owned_by":""}]}`
	srv := httptest.NewServer(lmStudioFallbackHandler(t, body))
	defer srv.Close()

	svc := &DiscoveryService{httpClient: srv.Client()}
	provider := &Provider{ID: uuid.New(), BaseURL: srv.URL + "/v1"}

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

// ---------------------------------------------------------------------------
// error handling — both endpoints fail
// ---------------------------------------------------------------------------

func TestDiscoverLMStudio_EmptyModels(t *testing.T) {
	// Native returns an empty list (treated as "not really LM Studio") and the
	// OpenAI fallback is also empty: no models, no error.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[]}`))
	}))
	defer srv.Close()

	svc := &DiscoveryService{httpClient: srv.Client()}
	provider := &Provider{ID: uuid.New(), BaseURL: srv.URL + "/v1"}

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
	provider := &Provider{ID: uuid.New(), BaseURL: srv.URL + "/v1"}

	if _, err := svc.discoverLMStudio(context.Background(), provider, ""); err == nil {
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
	provider := &Provider{ID: uuid.New(), BaseURL: srv.URL + "/v1"}

	if _, err := svc.discoverLMStudio(context.Background(), provider, ""); err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestDiscoverLMStudio_NoAPIKey(t *testing.T) {
	var authHeader string
	var sawHeader bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h := r.Header.Get("Authorization"); h != "" {
			authHeader = h
			sawHeader = true
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[]}`))
	}))
	defer srv.Close()

	svc := &DiscoveryService{httpClient: srv.Client()}
	provider := &Provider{ID: uuid.New(), BaseURL: srv.URL + "/v1"}

	if _, err := svc.discoverLMStudio(context.Background(), provider, ""); err != nil {
		t.Fatalf("discoverLMStudio failed: %v", err)
	}
	if sawHeader {
		t.Errorf("expected no Authorization header when apiKey is empty, got %q", authHeader)
	}
}
