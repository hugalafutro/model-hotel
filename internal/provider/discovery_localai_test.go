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

// A LocalAI 4.10 listing as the live server answers it: every llama.cpp model
// carries "chat" beside its real usecase, modifiers appear once the model has
// loaded, and the bare entry is a model file with no config.
const localAICapabilitiesBody = `{"object":"list","data":[
	{"id":"qwen3-1.7b","object":"model","capabilities":["chat","tools","thinking"],"input_modalities":["text"],"output_modalities":["text"],"context_size":8192},
	{"id":"gemma-3-4b-it","object":"model","capabilities":["chat","vision"],"input_modalities":["text","image"],"output_modalities":["text"],"context_size":8192},
	{"id":"nomic-embed-text-v1.5","object":"model","capabilities":["chat","embeddings"],"input_modalities":["text"],"output_modalities":["text"],"context_size":2048},
	{"id":"bge-reranker-v2-m3","object":"model","capabilities":["chat","rerank"],"input_modalities":["text"],"output_modalities":["text"],"context_size":2048},
	{"id":"whisper-base.en","object":"model","capabilities":["transcript"],"input_modalities":["audio"],"output_modalities":["text"],"context_size":4096},
	{"id":"piper-lessac","object":"model","capabilities":["tts"],"input_modalities":["text"],"output_modalities":["audio"],"context_size":4096},
	{"id":"dreamshaper-8","object":"model","capabilities":["image"],"input_modalities":["text"],"output_modalities":["image"],"context_size":4096},
	{"id":"silero-vad","object":"model","capabilities":["vad"],"input_modalities":["audio"],"output_modalities":[],"context_size":4096},
	{"id":"loose-file.gguf","object":"model","capabilities":null,"input_modalities":null,"output_modalities":null}
]}`

func TestDiscoverLocalAI_ClassesCapsAndContext(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models/capabilities" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(localAICapabilitiesBody))
	}))
	defer srv.Close()

	svc := &DiscoveryService{httpClient: srv.Client()}
	provider := &Provider{ID: uuid.New(), ProviderType: "localai", BaseURL: srv.URL + "/v1"}
	models, err := svc.discoverLocalAI(context.Background(), provider, "sk-local")
	if err != nil {
		t.Fatalf("discoverLocalAI: %v", err)
	}
	if gotAuth != "Bearer sk-local" {
		t.Errorf("Authorization = %q, want the key", gotAuth)
	}
	byID := make(map[string]*model.Model, len(models))
	for _, m := range models {
		byID[m.ModelID] = m
	}
	if _, listed := byID["silero-vad"]; listed || len(models) != 8 {
		t.Fatalf("got %d models, want 8 with the vad model skipped", len(models))
	}

	for id, want := range map[string]string{
		"qwen3-1.7b": "chat", "gemma-3-4b-it": "chat",
		"nomic-embed-text-v1.5": "embedding", "bge-reranker-v2-m3": "rerank",
		"whisper-base.en": "stt", "piper-lessac": "tts", "dreamshaper-8": "image",
	} {
		if got := byID[id].Modality; got != want {
			t.Errorf("%s modality = %q, want %q", id, got, want)
		}
	}

	caps := func(id string) model.Capability {
		var c model.Capability
		if err := json.Unmarshal([]byte(byID[id].Capabilities), &c); err != nil {
			t.Fatalf("%s capabilities: %v", id, err)
		}
		return c
	}
	if c := caps("qwen3-1.7b"); !c.Streaming || !c.StructuredOutput || !c.ToolCalling || !c.Reasoning || c.Vision {
		t.Errorf("qwen3-1.7b caps = %+v, want streaming, structured, tools, reasoning and no vision", c)
	}
	if c := caps("gemma-3-4b-it"); !c.Vision || c.ToolCalling || c.Reasoning {
		t.Errorf("gemma-3-4b-it caps = %+v, want vision only", c)
	}
	if got := byID["gemma-3-4b-it"].InputModalities; got != `["text","image"]` {
		t.Errorf("gemma-3-4b-it input modalities = %s", got)
	}
	if c := caps("loose-file.gguf"); !c.Streaming || c.ToolCalling || c.Reasoning || c.Vision {
		t.Errorf("loose file caps = %+v, want streaming only", c)
	}
	// A config-less file states no class: the name heuristics may still read
	// an embedding model out of it, which an explicit chat would forbid.
	if got := byID["loose-file.gguf"].Modality; got != "" {
		t.Errorf("loose file modality = %q, want none", got)
	}
	// A side model states its class and nothing else: the arrays and chat
	// capabilities are the classifier's to fill from the class.
	if got := byID["bge-reranker-v2-m3"].Capabilities; got != "{}" {
		t.Errorf("reranker caps = %s, want none", got)
	}

	if got := byID["qwen3-1.7b"].ContextLength; got == nil || *got != 8192 {
		t.Errorf("qwen3-1.7b context = %v, want 8192", got)
	}
	if got := byID["loose-file.gguf"].ContextLength; got != nil {
		t.Errorf("loose file context = %v, want none", *got)
	}
	if !byID["qwen3-1.7b"].LiveMeta.ContextLength {
		t.Error("context length is not marked live")
	}
}

// An older LocalAI without the capabilities route still lists /v1/models;
// the plain listing is taken and carries no class.
func TestDiscoverLocalAI_FallsBackToOpenAIListing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models/capabilities":
			w.WriteHeader(http.StatusNotFound)
		case "/v1/models":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"old-model","object":"model"}]}`))
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	svc := &DiscoveryService{httpClient: srv.Client()}
	provider := &Provider{ID: uuid.New(), ProviderType: "localai", BaseURL: srv.URL + "/v1"}
	models, err := svc.discoverLocalAI(context.Background(), provider, "")
	if err != nil {
		t.Fatalf("discoverLocalAI: %v", err)
	}
	if len(models) != 1 || models[0].ModelID != "old-model" {
		t.Fatalf("got %+v, want the one plain model", models)
	}
}

func TestDiscoverLocalAI_RejectsMalformedListing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"object":"list","data":"nope"}`))
	}))
	defer srv.Close()

	svc := &DiscoveryService{httpClient: srv.Client()}
	provider := &Provider{ID: uuid.New(), ProviderType: "localai", BaseURL: srv.URL + "/v1"}
	if _, err := svc.discoverLocalAI(context.Background(), provider, ""); err == nil {
		t.Fatal("malformed listing accepted")
	}
}

func TestIdentifyLocalServer_LocalAI(t *testing.T) {
	srv := localAIFingerprintServer(t)
	defer srv.Close()

	svc := &DiscoveryService{httpClient: srv.Client()}
	got, err := svc.IdentifyLocalServer(context.Background(), srv.URL+"/v1", "", "")
	if err != nil {
		t.Fatalf("IdentifyLocalServer: %v", err)
	}
	if got.Type != "localai" {
		t.Errorf("type = %q, want localai", got.Type)
	}
}

// LocalAI also serves Ollama's /api/tags in Ollama's shape. In the fixed
// order (no expected family, or one that does not match) its own fingerprint
// is asked before Ollama's, so it is not filed as Ollama; and a LocalAI added
// AS Ollama is told apart by asking its own fingerprint after Ollama's
// matched, so the add is refused naming localai.
func TestIdentifyLocalServer_LocalAIAgainstOllamaFingerprint(t *testing.T) {
	srv := localAIFingerprintServer(t)
	defer srv.Close()

	svc := &DiscoveryService{httpClient: srv.Client()}
	for expected, want := range map[string]string{"": "localai", "koboldcpp": "localai", "ollama": "localai"} {
		got, err := svc.IdentifyLocalServer(context.Background(), srv.URL, "", expected)
		if err != nil {
			t.Fatalf("IdentifyLocalServer(expected %q): %v", expected, err)
		}
		if got.Type != want {
			t.Errorf("IdentifyLocalServer(expected %q) = %q, want %q", expected, got.Type, want)
		}
	}
}

// localAIFingerprintServer answers like a LocalAI: its capabilities listing,
// the Ollama-shaped tag listing it emulates, and 404 for everything else.
func localAIFingerprintServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/models/capabilities":
			_, _ = w.Write([]byte(localAICapabilitiesBody))
		case "/api/tags":
			_, _ = w.Write([]byte(`{"models":[{"name":"piper-lessac:latest","model":"piper-lessac:latest","size":0}]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":{"code":404,"message":"Resource not found","type":""}}`))
		}
	}))
}

func TestLocalAIFingerprintFailsClosed(t *testing.T) {
	for body, want := range map[string]bool{
		`{"object":"list","data":[]}`:                                          true,
		`{"object":"list","data":[{"id":"m","capabilities":["chat"]}]}`:        true,
		`{"object":"list","data":[{"id":"m","capabilities":null}]}`:            true,
		`{"object":"list","data":[{"id":"m","object":"model"}]}`:               false,
		`{"object":"list","data":[{"id":"m","capabilities":"chat"}]}`:          false,
		`{"object":"list","data":[{"id":"m","capabilities":{"a":1}}]}`:         false,
		`{"error":{"code":401,"message":"An authentication key is required"}}`: false,
		`{"object":"list"}`: false,
		`not json`:          false,
	} {
		if got := isLocalAICapabilitiesListing([]byte(body)); got != want {
			t.Errorf("isLocalAICapabilitiesListing(%s) = %v, want %v", body, got, want)
		}
	}
}
