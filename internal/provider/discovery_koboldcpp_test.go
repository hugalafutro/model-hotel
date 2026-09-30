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
	versionInfo, err := svc.koboldcppVersion(context.Background(), srv.URL, "")
	if err != nil {
		t.Fatalf("koboldcppVersion failed: %v", err)
	}
	if versionInfo.Version != "1.2.3" {
		t.Errorf("expected version 1.2.3, got %q", versionInfo.Version)
	}
}

func TestKoboldCPPVersion_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	svc := &DiscoveryService{httpClient: srv.Client()}
	_, err := svc.koboldcppVersion(context.Background(), srv.URL, "")
	if err == nil {
		t.Fatal("expected error for non-200 status")
		return
	}
}

func TestKoboldCPPVersion_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`not json`))
	}))
	defer srv.Close()

	svc := &DiscoveryService{httpClient: srv.Client()}
	_, err := svc.koboldcppVersion(context.Background(), srv.URL, "")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
		return
	}
}

func TestKoboldCPPVersion_WrongResult(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result":"NotKoboldCpp","version":"1.0.0"}`))
	}))
	defer srv.Close()

	svc := &DiscoveryService{httpClient: srv.Client()}
	_, err := svc.koboldcppVersion(context.Background(), srv.URL, "")
	if err == nil {
		t.Fatal("expected error for wrong result field")
		return
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
		return
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
		return
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
// koboldcppContextLength
// ---------------------------------------------------------------------------

func TestKoboldCPPContextLength_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/extra/true_max_context_length" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"value":12288}`))
	}))
	defer srv.Close()

	svc := &DiscoveryService{httpClient: srv.Client()}
	got := svc.koboldcppContextLength(context.Background(), srv.URL, "")
	if got == nil {
		t.Fatal("expected a context length")
	}
	if *got != 12288 {
		t.Errorf("expected 12288, got %d", *got)
	}
}

// A build too old to serve the endpoint (or any other failure) leaves the
// context length unset rather than guessing one.
func TestKoboldCPPContextLength_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	svc := &DiscoveryService{httpClient: srv.Client()}
	if got := svc.koboldcppContextLength(context.Background(), srv.URL, ""); got != nil {
		t.Errorf("expected nil on a 404, got %d", *got)
	}
}

func TestKoboldCPPContextLength_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`invalid`))
	}))
	defer srv.Close()

	svc := &DiscoveryService{httpClient: srv.Client()}
	if got := svc.koboldcppContextLength(context.Background(), srv.URL, ""); got != nil {
		t.Errorf("expected nil on an undecodable body, got %d", *got)
	}
}

func TestKoboldCPPContextLength_ZeroValue(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"value":0}`))
	}))
	defer srv.Close()

	svc := &DiscoveryService{httpClient: srv.Client()}
	if got := svc.koboldcppContextLength(context.Background(), srv.URL, ""); got != nil {
		t.Errorf("expected nil for a zero value, got %d", *got)
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
		case "/api/extra/true_max_context_length":
			_, _ = w.Write([]byte(`{"value":4096}`))
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
	// Context length is a live probe, so it must be marked live.
	if !models[0].LiveMeta.ContextLength {
		t.Error("expected LiveMeta.ContextLength=true for the live context-length probe")
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
		case "/api/extra/true_max_context_length":
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
		return
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
		return
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
		case "/api/extra/true_max_context_length":
			_, _ = w.Write([]byte(`{"value":0}`))
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
		case "/api/extra/true_max_context_length":
			_, _ = w.Write([]byte(`{"value":0}`))
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

func TestDiscoverKoboldCPP_ContextLengthFailsGracefully(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/extra/version":
			_, _ = w.Write([]byte(`{"result":"KoboldCpp","version":"1.0.0"}`))
		case "/models":
			_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"llama","object":"model","created":0,"owned_by":""}]}`))
		case "/api/extra/true_max_context_length":
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
		t.Fatalf("discoverKoboldCPP should not fail when the context endpoint errors: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("expected 1 model, got %d", len(models))
	}
	if models[0].ContextLength != nil {
		t.Error("expected ContextLength to be nil when the context endpoint fails")
	}
}

func TestKoboldCPPVersion_RequestCreationError(t *testing.T) {
	svc := &DiscoveryService{httpClient: http.DefaultClient}
	// Use an invalid URL that will cause request creation to fail
	_, err := svc.koboldcppVersion(context.Background(), "http://invalid host with spaces", "")
	if err == nil {
		t.Fatal("expected error for invalid URL")
	}
}

func TestKoboldCPPContextLength_RequestCreationError(t *testing.T) {
	svc := &DiscoveryService{httpClient: http.DefaultClient}
	if got := svc.koboldcppContextLength(context.Background(), "http://invalid host with spaces", ""); got != nil {
		t.Fatalf("expected nil for an invalid URL, got %d", *got)
	}
}

func TestKoboldCPPLoadedModel_RequestCreationError(t *testing.T) {
	svc := &DiscoveryService{httpClient: http.DefaultClient}
	if _, err := svc.koboldcppLoadedModel(context.Background(), "http://invalid host with spaces", ""); err == nil {
		t.Fatal("expected an error for an unbuildable request")
	}
}

// A KoboldCPP started with --password rejects its native endpoints too, so
// discovery has to authenticate against them or a protected server reports no
// models at all.
func TestDiscoverKoboldCPP_SendsKeyToNativeEndpoints(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer sk-pw" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/extra/version":
			_, _ = w.Write([]byte(`{"result":"KoboldCpp","version":"1.119"}`))
		case "/models":
			_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"protected","object":"model","created":0,"owned_by":"koboldcpp"}]}`))
		case "/api/extra/true_max_context_length":
			_, _ = w.Write([]byte(`{"value":16384}`))
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	svc := &DiscoveryService{httpClient: srv.Client()}
	models, err := svc.discoverKoboldCPP(context.Background(), &Provider{ID: uuid.New(), BaseURL: srv.URL}, "sk-pw")
	if err != nil {
		t.Fatalf("discoverKoboldCPP against a protected server: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("expected 1 model, got %d", len(models))
	}
	if models[0].ContextLength == nil || *models[0].ContextLength != 16384 {
		t.Errorf("ContextLength = %v, want 16384 (the context probe must authenticate too)", models[0].ContextLength)
	}
}

// The version endpoint is the only place KoboldCPP reports what the loaded chat
// model accepts, so a vision or audio build must not be filed as text-only.
func TestDiscoverKoboldCPP_ModalitiesFromVersionFlags(t *testing.T) {
	tests := []struct {
		name     string
		version  string
		expected string
		count    int // the chat model plus any side models the flags load
	}{
		{"plain text build", `{"result":"KoboldCpp","version":"1.119"}`, `["text"]`, 1},
		{
			"vision build",
			`{"result":"KoboldCpp","version":"1.119","vision":true}`,
			`["text","image"]`,
			1,
		},
		{
			"audio build",
			`{"result":"KoboldCpp","version":"1.119","audio":true}`,
			`["text","audio"]`,
			1,
		},
		{
			"both adapters",
			`{"result":"KoboldCpp","version":"1.119","vision":true,"audio":true}`,
			`["text","image","audio"]`,
			1,
		},
		{
			// transcribe means a separate Whisper model is loaded for
			// /api/extra/transcribe. It says nothing about the chat model, so
			// it must not add audio input.
			"whisper loaded but the chat model takes no audio",
			`{"result":"KoboldCpp","version":"1.119","transcribe":true,"tts":true,"embeddings":true}`,
			`["text"]`,
			4,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/api/extra/version":
					_, _ = w.Write([]byte(tc.version))
				case "/models":
					_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"m","object":"model","created":0,"owned_by":""}]}`))
				case "/api/extra/true_max_context_length":
					_, _ = w.Write([]byte(`{"value":8192}`))
				default:
					t.Errorf("unexpected path: %s", r.URL.Path)
				}
			}))
			defer srv.Close()

			svc := &DiscoveryService{httpClient: srv.Client()}
			models, err := svc.discoverKoboldCPP(context.Background(), &Provider{ID: uuid.New(), BaseURL: srv.URL}, "")
			if err != nil {
				t.Fatalf("discoverKoboldCPP: %v", err)
			}
			// The chat model comes first; side models follow it.
			if len(models) != tc.count || models[0].ModelID != "m" {
				t.Fatalf("expected %d models with the chat model first, got %d", tc.count, len(models))
			}
			if models[0].InputModalities != tc.expected {
				t.Errorf("InputModalities = %s, want %s", models[0].InputModalities, tc.expected)
			}
		})
	}
}

func TestDiscoverKoboldCPP_NoContextLength(t *testing.T) {
	// A zero value means the server reported no context size, so the model
	// carries none rather than a fabricated one.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/extra/version":
			_, _ = w.Write([]byte(`{"result":"KoboldCpp","version":"1.0.0"}`))
		case "/models":
			_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"test","object":"model","created":0,"owned_by":""}]}`))
		case "/api/extra/true_max_context_length":
			_, _ = w.Write([]byte(`{"value":0}`))
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
		t.Fatalf("discoverKoboldCPP failed: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("expected 1 model, got %d", len(models))
	}
	if models[0].ContextLength != nil {
		t.Error("expected ContextLength to be nil when the server reports zero")
	}
}

// sideModelServer answers a KoboldCPP whose version payload is given, with the
// chat model "chat" and the given image listing (a 404 when empty). An
// unexpected path fails the test.
func sideModelServer(t *testing.T, version, sdModels string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/extra/version":
			_, _ = w.Write([]byte(version))
		case "/models", "/v1/models":
			_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"chat","object":"model","created":0,"owned_by":"koboldcpp"}]}`))
		case "/api/extra/true_max_context_length":
			_, _ = w.Write([]byte(`{"value":8192}`))
		case "/sdapi/v1/sd-models":
			if sdModels == "" {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_, _ = w.Write([]byte(sdModels))
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// Every side endpoint the version flags report gets a model of its own class,
// so image generation, speech, transcription and embeddings on a KoboldCPP are
// pickable instead of reachable only by sending the chat model's name.
func TestDiscoverKoboldCPP_SideModels(t *testing.T) {
	srv := sideModelServer(t,
		`{"result":"KoboldCpp","version":"1.122.1","llm":true,"txt2img":true,"tts":true,"transcribe":true,"embeddings":true}`,
		`[{"title":"dreamshaper_8","model_name":"dreamshaper_8","filename":"/models/dreamshaper_8.safetensors"}]`)

	svc := &DiscoveryService{httpClient: srv.Client()}
	models, err := svc.discoverKoboldCPP(context.Background(), &Provider{ID: uuid.New(), BaseURL: srv.URL + "/v1"}, "")
	if err != nil {
		t.Fatalf("discoverKoboldCPP: %v", err)
	}

	want := map[string]string{
		"chat":                    "",
		"koboldcpp/dreamshaper_8": "image",
		"koboldcpp/tts":           "tts",
		"koboldcpp/whisper":       "stt",
		"koboldcpp/embeddings":    "embedding",
	}
	if len(models) != len(want) {
		t.Fatalf("expected %d models, got %d", len(want), len(models))
	}
	for _, m := range models {
		class, ok := want[m.ModelID]
		if !ok {
			t.Errorf("unexpected model %q", m.ModelID)
			continue
		}
		if m.Modality != class {
			t.Errorf("%s: Modality = %q, want %q", m.ModelID, m.Modality, class)
		}
		if m.OwnedBy != "koboldcpp" || !m.Enabled {
			t.Errorf("%s: owned_by=%q enabled=%v", m.ModelID, m.OwnedBy, m.Enabled)
		}
	}
}

// The explicit class is final through classification, which fills the arrays
// from it: a side model must never be re-derived into a chat model.
func TestDiscoverKoboldCPP_SideModelClassesSurviveNormalization(t *testing.T) {
	srv := sideModelServer(t,
		`{"result":"KoboldCpp","version":"1.122.1","llm":false,"txt2img":true,"tts":true,"transcribe":true,"embeddings":true}`,
		`[{"model_name":"dreamshaper_8"}]`)

	svc := &DiscoveryService{httpClient: srv.Client()}
	models, err := svc.discoverKoboldCPP(context.Background(), &Provider{ID: uuid.New(), BaseURL: srv.URL}, "")
	if err != nil {
		t.Fatalf("discoverKoboldCPP: %v", err)
	}
	want := map[string][2]string{
		"koboldcpp/dreamshaper_8": {"image", `["image"]`},
		"koboldcpp/tts":           {"tts", `["audio"]`},
		"koboldcpp/whisper":       {"stt", `["text"]`},
		"koboldcpp/embeddings":    {"embedding", `["embedding"]`},
	}
	if len(models) != len(want) {
		t.Fatalf("expected %d models, got %d", len(want), len(models))
	}
	for _, m := range models {
		NormalizeModelClassification(m)
		w := want[m.ModelID]
		if m.Modality != w[0] || m.OutputModalities != w[1] {
			t.Errorf("%s: class=%q output=%s, want %q %s", m.ModelID, m.Modality, m.OutputModalities, w[0], w[1])
		}
	}
}

// A KoboldCPP started without a chat model still answers /models with a
// placeholder named "inactive"; the llm flag keeps it out, and /models is
// never asked.
func TestDiscoverKoboldCPP_NoChatModelLoaded(t *testing.T) {
	var modelsCalled bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/extra/version":
			_, _ = w.Write([]byte(`{"result":"KoboldCpp","version":"1.122.1","llm":false,"tts":true}`))
		case "/models":
			modelsCalled = true
			_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"inactive","object":"model","created":0,"owned_by":"koboldcpp"}]}`))
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	svc := &DiscoveryService{httpClient: srv.Client()}
	models, err := svc.discoverKoboldCPP(context.Background(), &Provider{ID: uuid.New(), BaseURL: srv.URL}, "")
	if err != nil {
		t.Fatalf("discoverKoboldCPP: %v", err)
	}
	if modelsCalled {
		t.Error("/models was queried although the version endpoint reports no chat model")
	}
	if len(models) != 1 || models[0].ModelID != "koboldcpp/tts" {
		t.Fatalf("expected only koboldcpp/tts, got %d models", len(models))
	}
}

// A server with no image listing, or one that names nothing, still yields the
// image model under the fixed fallback ID: the txt2img flag already proved the
// endpoint is served, and the name is missing for good.
func TestDiscoverKoboldCPP_ImageModelNameFallback(t *testing.T) {
	for name, sd := range map[string]string{
		"no listing (404)": "",
		"empty listing":    `[]`,
		"blank name":       `[{"model_name":"  "}]`,
	} {
		t.Run(name, func(t *testing.T) {
			srv := sideModelServer(t, `{"result":"KoboldCpp","version":"1.122.1","llm":false,"txt2img":true}`, sd)
			svc := &DiscoveryService{httpClient: srv.Client()}
			models, err := svc.discoverKoboldCPP(context.Background(), &Provider{ID: uuid.New(), BaseURL: srv.URL}, "")
			if err != nil {
				t.Fatalf("discoverKoboldCPP: %v", err)
			}
			if len(models) != 1 || models[0].ModelID != "koboldcpp/image" {
				t.Fatalf("expected only koboldcpp/image, got %d models", len(models))
			}
		})
	}
}

// A failure that may be transient must not rename the image model for a scan:
// it sits the scan out instead, and the rest of discovery goes ahead.
func TestDiscoverKoboldCPP_ImageListingTransientFailure(t *testing.T) {
	for name, respond := range map[string]func(http.ResponseWriter){
		"server error": func(w http.ResponseWriter) { w.WriteHeader(http.StatusInternalServerError) },
		"undecodable":  func(w http.ResponseWriter) { _, _ = w.Write([]byte(`not json`)) },
	} {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/api/extra/version":
					_, _ = w.Write([]byte(`{"result":"KoboldCpp","version":"1.122.1","llm":false,"txt2img":true,"tts":true}`))
				case "/sdapi/v1/sd-models":
					respond(w)
				default:
					t.Errorf("unexpected path: %s", r.URL.Path)
				}
			}))
			defer srv.Close()

			svc := &DiscoveryService{httpClient: srv.Client()}
			models, err := svc.discoverKoboldCPP(context.Background(), &Provider{ID: uuid.New(), BaseURL: srv.URL}, "")
			if err != nil {
				t.Fatalf("discoverKoboldCPP: %v", err)
			}
			if len(models) != 1 || models[0].ModelID != "koboldcpp/tts" {
				t.Fatalf("expected only koboldcpp/tts, got %d models", len(models))
			}
		})
	}
}

// A build too old to report the llm flag still lists the "inactive"
// placeholder when started without a chat model; it is not a model.
func TestDiscoverKoboldCPP_InactivePlaceholderWithoutLLMFlag(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/extra/version":
			_, _ = w.Write([]byte(`{"result":"KoboldCpp","version":"1.80","tts":true}`))
		case "/models":
			_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"inactive","object":"model","created":0,"owned_by":"koboldcpp"}]}`))
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	svc := &DiscoveryService{httpClient: srv.Client()}
	models, err := svc.discoverKoboldCPP(context.Background(), &Provider{ID: uuid.New(), BaseURL: srv.URL}, "")
	if err != nil {
		t.Fatalf("discoverKoboldCPP: %v", err)
	}
	if len(models) != 1 || models[0].ModelID != "koboldcpp/tts" {
		t.Fatalf("expected only koboldcpp/tts, got %d models", len(models))
	}
}

// A chat model whose file shares a side model's name keeps the ID: a second row
// with it would overwrite the chat model's class on every scan.
func TestDiscoverKoboldCPP_SideModelSharingChatIDIsSkipped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/extra/version":
			_, _ = w.Write([]byte(`{"result":"KoboldCpp","version":"1.122.1","llm":true,"tts":true,"transcribe":true}`))
		case "/models":
			_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"koboldcpp/tts","object":"model","created":0,"owned_by":"koboldcpp"}]}`))
		case "/api/extra/true_max_context_length":
			_, _ = w.Write([]byte(`{"value":4096}`))
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	svc := &DiscoveryService{httpClient: srv.Client()}
	models, err := svc.discoverKoboldCPP(context.Background(), &Provider{ID: uuid.New(), BaseURL: srv.URL}, "")
	if err != nil {
		t.Fatalf("discoverKoboldCPP: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("expected the chat model and koboldcpp/whisper, got %d models", len(models))
	}
	if models[0].ModelID != "koboldcpp/tts" || models[0].Modality != "" {
		t.Errorf("chat model = %q class %q, want koboldcpp/tts left for chat derivation", models[0].ModelID, models[0].Modality)
	}
	if models[1].ModelID != "koboldcpp/whisper" {
		t.Errorf("second model = %q, want koboldcpp/whisper", models[1].ModelID)
	}
}

// A --password server rejects the image listing too, so it carries the key.
func TestDiscoverKoboldCPP_ImageListingSendsKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer sk-pw" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/extra/version":
			_, _ = w.Write([]byte(`{"result":"KoboldCpp","version":"1.122.1","llm":false,"txt2img":true}`))
		case "/sdapi/v1/sd-models":
			_, _ = w.Write([]byte(`[{"model_name":"dreamshaper_8"}]`))
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	svc := &DiscoveryService{httpClient: srv.Client()}
	models, err := svc.discoverKoboldCPP(context.Background(), &Provider{ID: uuid.New(), BaseURL: srv.URL}, "sk-pw")
	if err != nil {
		t.Fatalf("discoverKoboldCPP: %v", err)
	}
	if len(models) != 1 || models[0].ModelID != "koboldcpp/dreamshaper_8" {
		t.Fatalf("expected koboldcpp/dreamshaper_8, got %d models", len(models))
	}
}
