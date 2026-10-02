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

// An SGLang 0.5 listing and model info as the live server answers them:
// the one served name with max_model_len, and the launch configuration.
const sglangListingBody = `{"object":"list","data":[{"id":"qwen3-0.6b","object":"model","created":1790954138,"owned_by":"sglang","root":"qwen3-0.6b","parent":null,"max_model_len":8192}]}`

const sglangModelInfoBody = `{"model_path":"/models/Qwen3-0.6B","served_model_name":"qwen3-0.6b","tokenizer_path":"/models/Qwen3-0.6B","is_generation":true,"preferred_sampling_params":null,"weight_version":"default","load_format":"auto","reasoning_parser":"qwen3","tool_call_parser":"qwen","disaggregation_mode":"null","has_image_understanding":false,"has_audio_understanding":false,"model_type":"qwen3","architectures":["Qwen3ForCausalLM"],"embedding":{"family":"none","task":"none"}}`

func sglangServer(t *testing.T, info string) (*httptest.Server, *[]string) {
	t.Helper()
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/models":
			_, _ = w.Write([]byte(sglangListingBody))
		case "/get_model_info":
			if info == "" {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"detail":"Not Found"}`))
				return
			}
			_, _ = w.Write([]byte(info))
		case "/api/tags":
			// SGLang 0.5 serves Ollama's tag listing in Ollama's shape too.
			_, _ = w.Write([]byte(`{"models":[{"name":"qwen3-0.6b","model":"qwen3-0.6b","size":0,"digest":"sha256:sglang0"}]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"detail":"Not Found"}`))
		}
	}))
	return srv, &paths
}

func sglangCaps(t *testing.T, m *model.Model) model.Capability {
	t.Helper()
	var c model.Capability
	if err := json.Unmarshal([]byte(m.Capabilities), &c); err != nil {
		t.Fatalf("%s capabilities: %v", m.ModelID, err)
	}
	return c
}

func TestDiscoverSGLang_ClassCapsAndContext(t *testing.T) {
	srv, _ := sglangServer(t, sglangModelInfoBody)
	defer srv.Close()

	svc := &DiscoveryService{httpClient: srv.Client()}
	provider := &Provider{ID: uuid.New(), ProviderType: "sglang", BaseURL: srv.URL + "/v1"}
	models, err := svc.discoverSGLang(context.Background(), provider, "sk-local")
	if err != nil {
		t.Fatalf("discoverSGLang: %v", err)
	}
	if len(models) != 1 || models[0].ModelID != "qwen3-0.6b" {
		t.Fatalf("got %+v, want the one served model", models)
	}
	m := models[0]
	if m.Modality != "chat" {
		t.Errorf("modality = %q, want chat", m.Modality)
	}
	if c := sglangCaps(t, m); !c.Streaming || !c.StructuredOutput || !c.Reasoning || !c.ToolCalling || c.Vision || c.AudioInput {
		t.Errorf("caps = %+v, want streaming, structured, reasoning and tools from the parsers, no vision or audio", c)
	}
	if m.InputModalities != `["text"]` || m.OutputModalities != `["text"]` {
		t.Errorf("modalities = %s / %s, want text in and out", m.InputModalities, m.OutputModalities)
	}
	if m.ContextLength == nil || *m.ContextLength != 8192 || !m.LiveMeta.ContextLength {
		t.Errorf("context = %v (live %v), want 8192 live from max_model_len", m.ContextLength, m.LiveMeta.ContextLength)
	}
}

// The parsers and the understanding flags are what the info says, each on
// its own: a multimodal server without parsers is vision and audio in,
// nothing else; an embedding server states the embedding class and no chat
// capability.
func TestDiscoverSGLang_InfoVariants(t *testing.T) {
	for name, tc := range map[string]struct {
		info       string
		wantClass  string
		wantInput  string
		wantVision bool
		wantAudio  bool
		wantTools  bool
	}{
		"multimodal without parsers": {
			info:       `{"model_path":"/models/Qwen3-Omni","is_generation":true,"reasoning_parser":"","tool_call_parser":"","has_image_understanding":true,"has_audio_understanding":true}`,
			wantClass:  "chat",
			wantInput:  `["text","image","audio"]`,
			wantVision: true, wantAudio: true,
		},
		"embedding server": {
			info:      `{"model_path":"/models/bge-m3","is_generation":false,"reasoning_parser":"","tool_call_parser":""}`,
			wantClass: "embedding",
		},
	} {
		t.Run(name, func(t *testing.T) {
			srv, _ := sglangServer(t, tc.info)
			defer srv.Close()
			svc := &DiscoveryService{httpClient: srv.Client()}
			provider := &Provider{ID: uuid.New(), ProviderType: "sglang", BaseURL: srv.URL + "/v1"}
			models, err := svc.discoverSGLang(context.Background(), provider, "")
			if err != nil || len(models) != 1 {
				t.Fatalf("discoverSGLang: %v, %d models", err, len(models))
			}
			m := models[0]
			if m.Modality != tc.wantClass {
				t.Errorf("modality = %q, want %q", m.Modality, tc.wantClass)
			}
			if tc.wantClass == "embedding" {
				if m.Capabilities != "{}" {
					t.Errorf("embedding caps = %s, want none", m.Capabilities)
				}
				return
			}
			c := sglangCaps(t, m)
			if c.Vision != tc.wantVision || c.AudioInput != tc.wantAudio || c.ToolCalling != tc.wantTools || c.Reasoning {
				t.Errorf("caps = %+v", c)
			}
			if m.InputModalities != tc.wantInput {
				t.Errorf("input = %s, want %s", m.InputModalities, tc.wantInput)
			}
		})
	}
}

// Without the info route the listing stands on its own, as custom reads it:
// chat by the central classification, streaming, context from the listing.
func TestDiscoverSGLang_WithoutModelInfo(t *testing.T) {
	srv, paths := sglangServer(t, "")
	defer srv.Close()

	svc := &DiscoveryService{httpClient: srv.Client()}
	provider := &Provider{ID: uuid.New(), ProviderType: "sglang", BaseURL: srv.URL + "/v1"}
	models, err := svc.discoverSGLang(context.Background(), provider, "")
	if err != nil || len(models) != 1 {
		t.Fatalf("discoverSGLang: %v, %d models", err, len(models))
	}
	m := models[0]
	if m.Modality != "" {
		t.Errorf("modality = %q, want none stated", m.Modality)
	}
	if c := sglangCaps(t, m); !c.Streaming || c.ToolCalling || c.Reasoning || c.StructuredOutput {
		t.Errorf("caps = %+v, want streaming only", c)
	}
	if m.ContextLength == nil || *m.ContextLength != 8192 {
		t.Errorf("context = %v, want 8192 from the listing", m.ContextLength)
	}
	if len(*paths) != 2 || (*paths)[0] != "/v1/models" || (*paths)[1] != "/get_model_info" {
		t.Errorf("probed %v, want the listing then the info at the origin", *paths)
	}
}

func TestDiscoverSGLang_RejectsMalformedListing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"object":"list","data":"nope"}`))
	}))
	defer srv.Close()
	svc := &DiscoveryService{httpClient: srv.Client()}
	provider := &Provider{ID: uuid.New(), ProviderType: "sglang", BaseURL: srv.URL + "/v1"}
	if _, err := svc.discoverSGLang(context.Background(), provider, ""); err == nil {
		t.Fatal("malformed listing accepted")
	}
}

// In the fixed order SGLang's own fingerprint comes before Ollama's, and a
// SGLang added AS Ollama is told apart by the emulator check after Ollama's
// tag listing matched.
func TestIdentifyLocalServer_SGLang(t *testing.T) {
	srv, _ := sglangServer(t, sglangModelInfoBody)
	defer srv.Close()

	svc := &DiscoveryService{httpClient: srv.Client()}
	for _, expected := range []string{"", "sglang", "ollama"} {
		got, err := svc.IdentifyLocalServer(context.Background(), srv.URL+"/v1", "", expected)
		if err != nil {
			t.Fatalf("IdentifyLocalServer(expected %q): %v", expected, err)
		}
		if got.Type != "sglang" {
			t.Errorf("IdentifyLocalServer(expected %q) = %q, want sglang", expected, got.Type)
		}
	}
}

func TestSGLangFingerprintFailsClosed(t *testing.T) {
	for body, want := range map[string]bool{
		sglangModelInfoBody:                         true,
		`{"model_path":"/m","is_generation":false}`: true,
		`{"model_path":"","is_generation":true}`:    false,
		`{"model_path":"/m"}`:                       false,
		`{"detail":"Not Found"}`:                    false,
		`{"models":[{"name":"llama3:8b"}]}`:         false,
		`{"model_path":"/m","is_generation":"yes"}`: false,
		`not json`: false,
	} {
		if got := isSGLangModelInfo([]byte(body)); got != want {
			t.Errorf("isSGLangModelInfo(%s) = %v, want %v", body, got, want)
		}
	}
}
