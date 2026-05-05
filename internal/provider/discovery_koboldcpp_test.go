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
// koboldcppVersion
// ---------------------------------------------------------------------------

func TestKoboldCPPVersion_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/extra/version" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result":"KoboldCpp","version":"1.2.3"}`))
	}))
	defer srv.Close()

	svc := &DiscoveryService{httpClient: srv.Client()}
	version, err := svc.koboldcppVersion(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("koboldcppVersion failed: %v", err)
	}
	if version != "1.2.3" {
		t.Errorf("expected version 1.2.3, got %q", version)
	}
}

func TestKoboldCPPVersion_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	svc := &DiscoveryService{httpClient: srv.Client()}
	_, err := svc.koboldcppVersion(context.Background(), srv.URL)
	if err == nil {
		t.Fatal("expected error for non-200 status")
	}
}

func TestKoboldCPPVersion_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`not json`))
	}))
	defer srv.Close()

	svc := &DiscoveryService{httpClient: srv.Client()}
	_, err := svc.koboldcppVersion(context.Background(), srv.URL)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestKoboldCPPVersion_WrongResult(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result":"NotKoboldCpp","version":"1.0.0"}`))
	}))
	defer srv.Close()

	svc := &DiscoveryService{httpClient: srv.Client()}
	_, err := svc.koboldcppVersion(context.Background(), srv.URL)
	if err == nil {
		t.Fatal("expected error for wrong result field")
	}
}

// ---------------------------------------------------------------------------
// koboldcppLoadedModel
// ---------------------------------------------------------------------------

func TestKoboldCPPLoadedModel_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"llama-3-8b","object":"model","created":1700000000,"owned_by":"koboldcpp"}]}`))
	}))
	defer srv.Close()

	svc := &DiscoveryService{httpClient: srv.Client()}
	modelID, err := svc.koboldcppLoadedModel(context.Background(), srv.URL, "")
	if err != nil {
		t.Fatalf("koboldcppLoadedModel failed: %v", err)
	}
	if modelID != "llama-3-8b" {
		t.Errorf("expected model ID 'llama-3-8b', got %q", modelID)
	}
}

func TestKoboldCPPLoadedModel_EmptyModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[]}`))
	}))
	defer srv.Close()

	svc := &DiscoveryService{httpClient: srv.Client()}
	modelID, err := svc.koboldcppLoadedModel(context.Background(), srv.URL, "")
	if err != nil {
		t.Fatalf("koboldcppLoadedModel should not error on empty list: %v", err)
	}
	if modelID != "" {
		t.Errorf("expected empty model ID for empty list, got %q", modelID)
	}
}

func TestKoboldCPPLoadedModel_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`forbidden`))
	}))
	defer srv.Close()

	svc := &DiscoveryService{httpClient: srv.Client()}
	_, err := svc.koboldcppLoadedModel(context.Background(), srv.URL, "")
	if err == nil {
		t.Fatal("expected error for non-200 status")
	}
}

func TestKoboldCPPLoadedModel_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`bad json`))
	}))
	defer srv.Close()

	svc := &DiscoveryService{httpClient: srv.Client()}
	_, err := svc.koboldcppLoadedModel(context.Background(), srv.URL, "")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestKoboldCPPLoadedModel_SendsAPIKey(t *testing.T) {
	var authHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"test-model","object":"model","created":0,"owned_by":""}]}`))
	}))
	defer srv.Close()

	svc := &DiscoveryService{httpClient: srv.Client()}
	_, err := svc.koboldcppLoadedModel(context.Background(), srv.URL, "sk-test-key")
	if err != nil {
		t.Fatalf("koboldcppLoadedModel failed: %v", err)
	}
	if authHeader != "Bearer sk-test-key" {
		t.Errorf("expected Authorization header 'Bearer sk-test-key', got %q", authHeader)
	}
}

// ---------------------------------------------------------------------------
// koboldcppPerf
// ---------------------------------------------------------------------------

func TestKoboldCPPPerf_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/extra/perf" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"last_process":0.5,"last_gen":2.0,"queue":0,"maxcontextlen":4096,"model_loaded":true}`))
	}))
	defer srv.Close()

	svc := &DiscoveryService{httpClient: srv.Client()}
	perf, err := svc.koboldcppPerf(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("koboldcppPerf failed: %v", err)
	}
	if perf.MaxContextLength != 4096 {
		t.Errorf("expected MaxContextLength 4096, got %d", perf.MaxContextLength)
	}
	if !perf.ModelLoaded {
		t.Error("expected ModelLoaded to be true")
	}
	if perf.LastProcessTime != 0.5 {
		t.Errorf("expected LastProcessTime 0.5, got %f", perf.LastProcessTime)
	}
	if perf.LastGenerationTime != 2.0 {
		t.Errorf("expected LastGenerationTime 2.0, got %f", perf.LastGenerationTime)
	}
	if perf.Queue != 0 {
		t.Errorf("expected Queue 0, got %d", perf.Queue)
	}
}

func TestKoboldCPPPerf_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	svc := &DiscoveryService{httpClient: srv.Client()}
	_, err := svc.koboldcppPerf(context.Background(), srv.URL)
	if err == nil {
		t.Fatal("expected error for non-200 status")
	}
}

func TestKoboldCPPPerf_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`invalid`))
	}))
	defer srv.Close()

	svc := &DiscoveryService{httpClient: srv.Client()}
	_, err := svc.koboldcppPerf(context.Background(), srv.URL)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

// ---------------------------------------------------------------------------
// discoverKoboldCPP
// ---------------------------------------------------------------------------

func TestDiscoverKoboldCPP_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/extra/version":
			_, _ = w.Write([]byte(`{"result":"KoboldCpp","version":"1.2.3"}`))
		case "/models", "/v1/models":
			_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"llama-3-8b","object":"model","created":1700000000,"owned_by":"koboldcpp"}]}`))
		case "/api/extra/perf":
			_, _ = w.Write([]byte(`{"last_process":0.5,"last_gen":2.0,"queue":0,"maxcontextlen":4096,"model_loaded":true}`))
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	svc := &DiscoveryService{httpClient: srv.Client()}
	provider := &Provider{
		ID:      uuid.New(),
		BaseURL: srv.URL + "/v1",
	}

	models, err := svc.discoverKoboldCPP(context.Background(), provider, "")
	if err != nil {
		t.Fatalf("discoverKoboldCPP failed: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("expected 1 model, got %d", len(models))
	}
	if models[0].ModelID != "llama-3-8b" {
		t.Errorf("expected model ID 'llama-3-8b', got %q", models[0].ModelID)
	}
	if models[0].OwnedBy != "koboldcpp" {
		t.Errorf("expected owned_by 'koboldcpp', got %q", models[0].OwnedBy)
	}
	if models[0].ProviderID != provider.ID {
		t.Errorf("expected provider ID %v, got %v", provider.ID, models[0].ProviderID)
	}
	if models[0].ContextLength == nil || *models[0].ContextLength != 4096 {
		t.Errorf("expected ContextLength 4096, got %v", models[0].ContextLength)
	}

	// verify capabilities
	var caps model.Capability
	if err := json.Unmarshal([]byte(models[0].Capabilities), &caps); err != nil {
		t.Fatalf("failed to unmarshal capabilities: %v", err)
	}
	if !caps.Streaming {
		t.Error("expected Streaming=true")
	}
	if caps.ToolCalling {
		t.Error("expected ToolCalling=false (conservative default)")
	}
}

func TestDiscoverKoboldCPP_EmptyModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/extra/version":
			_, _ = w.Write([]byte(`{"result":"KoboldCpp","version":"1.0.0"}`))
		case "/models":
			_, _ = w.Write([]byte(`{"object":"list","data":[]}`))
		case "/api/extra/perf":
			w.WriteHeader(http.StatusNotFound)
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	svc := &DiscoveryService{httpClient: srv.Client()}
	provider := &Provider{
		ID:      uuid.New(),
		BaseURL: srv.URL,
	}

	models, err := svc.discoverKoboldCPP(context.Background(), provider, "")
	if err != nil {
		t.Fatalf("discoverKoboldCPP should not error on empty models: %v", err)
	}
	if len(models) != 0 {
		t.Errorf("expected 0 models, got %d", len(models))
	}
}

func TestDiscoverKoboldCPP_VersionCheckFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result":"NotKoboldCpp","version":""}`))
	}))
	defer srv.Close()

	svc := &DiscoveryService{httpClient: srv.Client()}
	provider := &Provider{
		ID:      uuid.New(),
		BaseURL: srv.URL,
	}

	_, err := svc.discoverKoboldCPP(context.Background(), provider, "")
	if err == nil {
		t.Fatal("expected error when version check fails")
	}
}

func TestDiscoverKoboldCPP_ListModelsHTTPError(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/extra/version" {
			_, _ = w.Write([]byte(`{"result":"KoboldCpp","version":"1.0.0"}`))
		} else {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`server error`))
		}
	}))
	defer srv.Close()

	svc := &DiscoveryService{httpClient: srv.Client()}
	provider := &Provider{
		ID:      uuid.New(),
		BaseURL: srv.URL,
	}

	_, err := svc.discoverKoboldCPP(context.Background(), provider, "")
	if err == nil {
		t.Fatal("expected error when /models returns 500")
	}
}

func TestDiscoverKoboldCPP_SendsAPIKey(t *testing.T) {
	var authHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/extra/version":
			_, _ = w.Write([]byte(`{"result":"KoboldCpp","version":"1.0.0"}`))
		case "/models":
			authHeader = r.Header.Get("Authorization")
			_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"test","object":"model","created":0,"owned_by":""}]}`))
		case "/api/extra/perf":
			_, _ = w.Write([]byte(`{"last_process":0,"last_gen":0,"queue":0,"maxcontextlen":0,"model_loaded":false}`))
		}
	}))
	defer srv.Close()

	svc := &DiscoveryService{httpClient: srv.Client()}
	provider := &Provider{
		ID:      uuid.New(),
		BaseURL: srv.URL,
	}
	_, err := svc.discoverKoboldCPP(context.Background(), provider, "sk-my-key")
	if err != nil {
		t.Fatalf("discoverKoboldCPP failed: %v", err)
	}
	if authHeader != "Bearer sk-my-key" {
		t.Errorf("expected Authorization 'Bearer sk-my-key', got %q", authHeader)
	}
}

func TestDiscoverKoboldCPP_NoAPIKey(t *testing.T) {
	var authHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/extra/version":
			_, _ = w.Write([]byte(`{"result":"KoboldCpp","version":"1.0.0"}`))
		case "/models":
			authHeader = r.Header.Get("Authorization")
			_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"test","object":"model","created":0,"owned_by":""}]}`))
		case "/api/extra/perf":
			_, _ = w.Write([]byte(`{"last_process":0,"last_gen":0,"queue":0,"maxcontextlen":0,"model_loaded":false}`))
		}
	}))
	defer srv.Close()

	svc := &DiscoveryService{httpClient: srv.Client()}
	provider := &Provider{
		ID:      uuid.New(),
		BaseURL: srv.URL,
	}
	_, err := svc.discoverKoboldCPP(context.Background(), provider, "")
	if err != nil {
		t.Fatalf("discoverKoboldCPP failed: %v", err)
	}
	if authHeader != "" {
		t.Errorf("expected no Authorization header when apiKey is empty, got %q", authHeader)
	}
}

func TestDiscoverKoboldCPP_PerfFailsGracefully(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/extra/version":
			_, _ = w.Write([]byte(`{"result":"KoboldCpp","version":"1.0.0"}`))
		case "/models":
			_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"llama","object":"model","created":0,"owned_by":""}]}`))
		case "/api/extra/perf":
			w.WriteHeader(http.StatusInternalServerError)
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	svc := &DiscoveryService{httpClient: srv.Client()}
	provider := &Provider{
		ID:      uuid.New(),
		BaseURL: srv.URL,
	}

	models, err := svc.discoverKoboldCPP(context.Background(), provider, "")
	if err != nil {
		t.Fatalf("discoverKoboldCPP should not fail when perf endpoint errors: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("expected 1 model, got %d", len(models))
	}
	if models[0].ContextLength != nil {
		t.Error("expected ContextLength to be nil when perf endpoint fails")
	}
}
