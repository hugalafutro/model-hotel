package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/model"
)

// A TabbyAPI listing and loaded-model card as the live server answers a plain
// API key: the listing names the loaded model with llama-server's meta
// (n_ctx is the loaded max_seq_len) and no parameters; /v1/model carries the
// parameters and the chat template, here Qwen3's cut to the parts discovery
// reads.
const tabbyAPIListingBody = `{"object":"list","data":[{"id":"Qwen3-4B-exl3-4bpw","object":"model","created":1790969349,"owned_by":"tabbyAPI","logging":null,"parameters":null,"meta":{"n_ctx_train":40960,"n_ctx":4096,"n_vocab":151936,"n_embd":2560,"size":2891216176}}]}`

const tabbyAPIModelCardBody = `{"id":"Qwen3-4B-exl3-4bpw","object":"model","created":1790969361,"owned_by":"tabbyAPI","logging":null,"parameters":{"max_seq_len":4096,"cache_size":4096,"cache_mode":"FP16","rope_scale":1.0,"rope_alpha":1.0,"max_batch_size":128,"chunk_size":2048,"prompt_template":"from_tokenizer_config","prompt_template_content":"{%- if tools %}\n    {{- '<|im_start|>system\\n' }}\n{%- endif %}\n{%- if add_generation_prompt %}{{ '<|im_start|>assistant\\n<think>\\n' }}{%- endif %}","use_vision":false,"draft":null},"meta":{"n_ctx_train":40960,"n_ctx":4096,"n_vocab":151936,"n_embd":2560,"size":2891216176}}`

const tabbyAPIEmbeddingCardBody = `{"id":"all-MiniLM-L6-v2","object":"model","created":1790969349,"owned_by":"tabbyAPI","logging":null,"parameters":null,"meta":{"n_ctx_train":512,"n_ctx":null,"n_vocab":30522,"n_embd":384,"size":90868376}}`

const tabbyAPIServiceInfoBody = `{"version":0.1,"software":{"name":"TabbyAPI","repository":"https://github.com/theroyallab/tabbyAPI","homepage":"https://github.com/theroyallab/tabbyAPI"},"api":{"openai":{"name":"OpenAI API","relative_url":"/v1","version":1},"koboldai":{"name":"KoboldAI API","relative_url":"/api","version":1}}}`

// tabbyAPIServer answers like TabbyAPI 2026-10 with the given listing, loaded
// chat card and embedding card; an empty card is the route's own 503 for an
// empty container.
func tabbyAPIServer(t *testing.T, listing, card, embedding string) (*httptest.Server, *[]string) {
	t.Helper()
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		empty := func(what string) {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"detail":"` + what + ` model is not loaded."}`))
		}
		switch r.URL.Path {
		case "/v1/models":
			_, _ = w.Write([]byte(listing))
		case "/v1/model":
			if card == "" {
				empty("A")
				return
			}
			_, _ = w.Write([]byte(card))
		case "/v1/model/embedding":
			if embedding == "" {
				empty("An embedding")
				return
			}
			_, _ = w.Write([]byte(embedding))
		case "/.well-known/serviceinfo":
			_, _ = w.Write([]byte(tabbyAPIServiceInfoBody))
		case "/api/extra/version":
			// TabbyAPI impersonates KoboldCpp for Kobold clients.
			_, _ = w.Write([]byte(`{"result":"KoboldCpp","version":"1.74"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"detail":"Not Found"}`))
		}
	}))
	return srv, &paths
}

func TestDiscoverTabbyAPI_LoadedModelCapsAndContext(t *testing.T) {
	srv, paths := tabbyAPIServer(t, tabbyAPIListingBody, tabbyAPIModelCardBody, "")
	defer srv.Close()

	svc := &DiscoveryService{httpClient: srv.Client()}
	provider := &Provider{ID: uuid.New(), ProviderType: "tabbyapi", BaseURL: srv.URL + "/v1"}
	models, err := svc.discoverTabbyAPI(context.Background(), provider, "sk-local")
	if err != nil {
		t.Fatalf("discoverTabbyAPI: %v", err)
	}
	if len(models) != 1 || models[0].ModelID != "Qwen3-4B-exl3-4bpw" {
		t.Fatalf("got %+v, want the one loaded model", models)
	}
	m := models[0]
	if m.Modality != "chat" {
		t.Errorf("modality = %q, want chat", m.Modality)
	}
	if c := decodeCaps(t, m); !c.Streaming || !c.StructuredOutput || !c.Reasoning || !c.ToolCalling || c.Vision {
		t.Errorf("caps = %+v, want streaming, structured, reasoning and tools from the template, no vision", c)
	}
	if m.InputModalities != `["text"]` || m.OutputModalities != `["text"]` {
		t.Errorf("modalities = %s / %s, want text in and out", m.InputModalities, m.OutputModalities)
	}
	if m.ContextLength == nil || *m.ContextLength != 4096 || !m.LiveMeta.ContextLength {
		t.Errorf("context = %v (live %v), want 4096 live from meta.n_ctx", m.ContextLength, m.LiveMeta.ContextLength)
	}
	if want := []string{"/v1/models", "/v1/model", "/v1/model/embedding"}; !slices.Equal(*paths, want) {
		t.Errorf("asked %v, want %v", *paths, want)
	}
}

// Each capability follows its own field: a vision projector adds image
// input, a template without a tools block or a think tag leaves tools and
// reasoning off, and context falls back to max_seq_len when meta is absent.
func TestDiscoverTabbyAPI_ParameterVariants(t *testing.T) {
	listing := `{"object":"list","data":[{"id":"m","object":"model","owned_by":"tabbyAPI","parameters":null}]}`
	card := func(params string) string {
		return `{"id":"m","object":"model","owned_by":"tabbyAPI","parameters":` + params + `}`
	}
	for name, tc := range map[string]struct {
		params        string
		wantInput     string
		wantVision    bool
		wantTools     bool
		wantReasoning bool
		wantContext   int
	}{
		"vision model, plain template": {
			params:      `{"max_seq_len":8192,"use_vision":true,"prompt_template_content":"{{ messages }}"}`,
			wantInput:   `["text","image"]`,
			wantVision:  true,
			wantContext: 8192,
		},
		"tools without thinking": {
			params:    `{"max_seq_len":2048,"use_vision":false,"prompt_template_content":"{%- if tools %}{{ tools }}{%- endif %}"}`,
			wantInput: `["text"]`,
			wantTools: true, wantContext: 2048,
		},
		"a word containing tools is not a tools block": {
			params:      `{"max_seq_len":2048,"prompt_template_content":"{{ toolset }}"}`,
			wantInput:   `["text"]`,
			wantContext: 2048,
		},
		// Odd shapes cost their own field only: the template still reads.
		"off-shape fields are read as absent": {
			params:    `{"max_seq_len":"lots","use_vision":"yes","prompt_template_content":"{%- if tools %}{{ tools }}{%- endif %}"}`,
			wantInput: `["text"]`,
			wantTools: true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			srv, _ := tabbyAPIServer(t, listing, card(tc.params), "")
			defer srv.Close()
			svc := &DiscoveryService{httpClient: srv.Client()}
			provider := &Provider{ID: uuid.New(), ProviderType: "tabbyapi", BaseURL: srv.URL + "/v1"}
			models, err := svc.discoverTabbyAPI(context.Background(), provider, "")
			if err != nil || len(models) != 1 {
				t.Fatalf("discoverTabbyAPI = %+v, %v; want one model", models, err)
			}
			m := models[0]
			c := decodeCaps(t, m)
			if c.Vision != tc.wantVision || c.ToolCalling != tc.wantTools || c.Reasoning != tc.wantReasoning || !c.StructuredOutput || !c.Streaming {
				t.Errorf("caps = %+v, want vision=%v tools=%v reasoning=%v, structured and streaming", c, tc.wantVision, tc.wantTools, tc.wantReasoning)
			}
			if m.InputModalities != tc.wantInput {
				t.Errorf("input = %s, want %s", m.InputModalities, tc.wantInput)
			}
			switch {
			case tc.wantContext == 0 && m.ContextLength != nil:
				t.Errorf("context = %d, want none from an off-shape max_seq_len", *m.ContextLength)
			case tc.wantContext > 0 && (m.ContextLength == nil || *m.ContextLength != tc.wantContext || !m.LiveMeta.ContextLength):
				t.Errorf("context = %v, want %d live from max_seq_len", m.ContextLength, tc.wantContext)
			}
		})
	}
}

// The listing's meta.n_ctx wins over the card's max_seq_len, as the doc says;
// a card answered with a body that is no model card leaves the listing read
// as custom would (logged); a card route that faults or answers a status that
// is neither an answer nor an empty container fails the scan, so the stored
// capabilities stay.
func TestDiscoverTabbyAPI_CardPrecedenceAndFaults(t *testing.T) {
	t.Run("listing n_ctx over card max_seq_len", func(t *testing.T) {
		card := `{"id":"Qwen3-4B-exl3-4bpw","parameters":{"max_seq_len":2048,"prompt_template_content":""}}`
		srv, _ := tabbyAPIServer(t, tabbyAPIListingBody, card, "")
		defer srv.Close()
		svc := &DiscoveryService{httpClient: srv.Client()}
		models, err := svc.discoverTabbyAPI(context.Background(), &Provider{ID: uuid.New(), ProviderType: "tabbyapi", BaseURL: srv.URL + "/v1"}, "")
		if err != nil || len(models) != 1 {
			t.Fatalf("discoverTabbyAPI = %+v, %v", models, err)
		}
		if models[0].ContextLength == nil || *models[0].ContextLength != 4096 {
			t.Errorf("context = %v, want the listing's 4096", models[0].ContextLength)
		}
		if c := decodeCaps(t, models[0]); !c.StructuredOutput || c.ToolCalling {
			t.Errorf("caps = %+v, want structured from the card, no tools from an empty template", c)
		}
	})
	t.Run("card route answers another server's body", func(t *testing.T) {
		srv, _ := tabbyAPIServer(t, tabbyAPIListingBody, `<html>proxy</html>`, `{"models":[]}`)
		defer srv.Close()
		svc := &DiscoveryService{httpClient: srv.Client()}
		models, err := svc.discoverTabbyAPI(context.Background(), &Provider{ID: uuid.New(), ProviderType: "tabbyapi", BaseURL: srv.URL + "/v1"}, "")
		if err != nil || len(models) != 1 {
			t.Fatalf("discoverTabbyAPI = %+v, %v; want the listing alone", models, err)
		}
		if c := decodeCaps(t, models[0]); !c.Streaming || c.StructuredOutput {
			t.Errorf("caps = %+v, want streaming only without a card", c)
		}
	})
	for name, cardStatus := range map[string]int{"card route answers 500": http.StatusInternalServerError, "card route answers 401": http.StatusUnauthorized, "card route drops the connection": 0} {
		t.Run(name, func(t *testing.T) {
			var srv *httptest.Server
			srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/v1/models" {
					_, _ = w.Write([]byte(tabbyAPIListingBody))
					return
				}
				if cardStatus == 0 {
					srv.CloseClientConnections()
					return
				}
				w.WriteHeader(cardStatus)
				_, _ = w.Write([]byte(`{"detail":"nope"}`))
			}))
			defer srv.Close()
			svc := &DiscoveryService{httpClient: srv.Client()}
			models, err := svc.discoverTabbyAPI(context.Background(), &Provider{ID: uuid.New(), ProviderType: "tabbyapi", BaseURL: srv.URL + "/v1"}, "")
			if err == nil || models != nil {
				t.Fatalf("discoverTabbyAPI = %+v, %v; want the scan to fail", models, err)
			}
		})
	}
	t.Run("card route 404 is no card", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if r.URL.Path == "/v1/models" {
				_, _ = w.Write([]byte(tabbyAPIListingBody))
				return
			}
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"detail":"Not Found"}`))
		}))
		defer srv.Close()
		svc := &DiscoveryService{httpClient: srv.Client()}
		models, err := svc.discoverTabbyAPI(context.Background(), &Provider{ID: uuid.New(), ProviderType: "tabbyapi", BaseURL: srv.URL + "/v1"}, "")
		if err != nil || len(models) != 1 {
			t.Fatalf("discoverTabbyAPI = %+v, %v; want the listing alone", models, err)
		}
	})
}

// An emulator fingerprint that answers neither 200 nor 404 after the expected
// family matched (a 5xx, a 401 on that one route) is an unanswered question:
// the add is refused as unconfirmed rather than saved as the family the
// emulator imitates.
func TestIdentifyLocalServer_EmulatorProbeIndeterminateStatusIsAnError(t *testing.T) {
	for _, status := range []int{http.StatusInternalServerError, http.StatusUnauthorized} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			switch r.URL.Path {
			case "/api/extra/version":
				_, _ = w.Write([]byte(`{"result":"KoboldCpp","version":"1.74"}`))
			case "/.well-known/serviceinfo":
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"detail":"later"}`))
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		svc := &DiscoveryService{httpClient: srv.Client()}
		got, err := svc.IdentifyLocalServer(context.Background(), srv.URL, "", "koboldcpp")
		srv.Close()
		if err == nil || got.Type != "" {
			t.Errorf("status %d: IdentifyLocalServer = %+v, %v; want an error and no type", status, got, err)
		}
	}
}

// A listing that does not name the loaded model (dummy names only) still
// gets it from the card: it is what the server serves.
func TestDiscoverTabbyAPI_LoadedCardWithoutListedEntry(t *testing.T) {
	srv, _ := tabbyAPIServer(t, `{"object":"list","data":[{"id":"gpt-3.5-turbo","object":"model","owned_by":"tabbyAPI"}]}`, tabbyAPIModelCardBody, "")
	defer srv.Close()
	svc := &DiscoveryService{httpClient: srv.Client()}
	models, err := svc.discoverTabbyAPI(context.Background(), &Provider{ID: uuid.New(), ProviderType: "tabbyapi", BaseURL: srv.URL + "/v1"}, "")
	if err != nil || len(models) != 2 {
		t.Fatalf("discoverTabbyAPI = %+v, %v; want the dummy and the loaded model", models, err)
	}
	loaded := models[1]
	if loaded.ModelID != "Qwen3-4B-exl3-4bpw" {
		t.Fatalf("second model = %s, want the loaded one from the card", loaded.ModelID)
	}
	if c := decodeCaps(t, loaded); !c.ToolCalling || !c.Reasoning || !c.StructuredOutput {
		t.Errorf("caps = %+v, want tools, reasoning and structured from the card", c)
	}
	if loaded.ContextLength == nil || *loaded.ContextLength != 4096 {
		t.Errorf("context = %v, want 4096 from the card", loaded.ContextLength)
	}
}

// An admin key lists the whole model directory: the loaded chat model (read
// from its card), the embedding model's own folder (filed as the embedding
// model, not a second chat entry) and models that are not loaded, filed as
// plain chat models the way custom would, since nothing says what they can
// do until they are loaded. A configured dummy name is the same case.
func TestDiscoverTabbyAPI_DirectoryListingAndEmbeddingModel(t *testing.T) {
	listing := `{"object":"list","data":[{"id":"gpt-3.5-turbo","object":"model","owned_by":"tabbyAPI"},{"id":"Qwen3-4B-exl3-4bpw","object":"model","owned_by":"tabbyAPI","parameters":null,"meta":{"n_ctx_train":40960,"n_ctx":null,"n_vocab":151936,"n_embd":2560,"size":0}},{"id":"all-MiniLM-L6-v2","object":"model","owned_by":"tabbyAPI","parameters":null,"meta":{"n_ctx_train":512,"n_ctx":null,"n_vocab":30522,"n_embd":384,"size":0}},{"id":"Llama-3.2-3B-exl3","object":"model","owned_by":"tabbyAPI","meta":{"n_ctx_train":131072,"n_ctx":null,"n_vocab":128256,"n_embd":3072,"size":0}}]}`
	srv, _ := tabbyAPIServer(t, listing, tabbyAPIModelCardBody, tabbyAPIEmbeddingCardBody)
	defer srv.Close()

	svc := &DiscoveryService{httpClient: srv.Client()}
	provider := &Provider{ID: uuid.New(), ProviderType: "tabbyapi", BaseURL: srv.URL + "/v1"}
	models, err := svc.discoverTabbyAPI(context.Background(), provider, "admin-key")
	if err != nil {
		t.Fatalf("discoverTabbyAPI: %v", err)
	}
	byID := map[string]*model.Model{}
	for _, m := range models {
		byID[m.ModelID] = m
	}
	if len(models) != 4 || len(byID) != 4 {
		t.Fatalf("got %d models %v, want the four listed, the embedding one once", len(models), models)
	}
	for _, id := range []string{"gpt-3.5-turbo", "Llama-3.2-3B-exl3"} {
		m := byID[id]
		if m.Modality != "chat" {
			t.Errorf("%s modality = %q, want chat", id, m.Modality)
		}
		if c := decodeCaps(t, m); !c.Streaming || c.StructuredOutput || c.ToolCalling || c.Reasoning || c.Vision {
			t.Errorf("%s caps = %+v, want streaming only for an unloaded model", id, c)
		}
		if m.ContextLength != nil {
			t.Errorf("%s context = %d, want none for an unloaded model", id, *m.ContextLength)
		}
	}
	loaded := byID["Qwen3-4B-exl3-4bpw"]
	if c := decodeCaps(t, loaded); !c.ToolCalling || !c.Reasoning || !c.StructuredOutput {
		t.Errorf("loaded model caps = %+v, want tools, reasoning and structured from its card", c)
	}
	// The directory listing has no n_ctx for it; the card's max_seq_len stands in.
	if loaded.ContextLength == nil || *loaded.ContextLength != 4096 || !loaded.LiveMeta.ContextLength {
		t.Errorf("loaded context = %v, want 4096 live from the card", loaded.ContextLength)
	}
	emb := byID["all-MiniLM-L6-v2"]
	if emb.Modality != "embedding" || emb.Description != "TabbyAPI embedding model" {
		t.Errorf("embedding model = %+v, want the embedding modality", emb)
	}
}

// The plain key's listing never names the embedding model, so the card adds
// it; an empty chat container (nothing loaded yet) leaves the listing's
// entries as custom would read them.
func TestDiscoverTabbyAPI_EmbeddingAddedAndEmptyChatContainer(t *testing.T) {
	srv, _ := tabbyAPIServer(t, tabbyAPIListingBody, "", tabbyAPIEmbeddingCardBody)
	defer srv.Close()

	svc := &DiscoveryService{httpClient: srv.Client()}
	provider := &Provider{ID: uuid.New(), ProviderType: "tabbyapi", BaseURL: srv.URL + "/v1"}
	models, err := svc.discoverTabbyAPI(context.Background(), provider, "")
	if err != nil || len(models) != 2 {
		t.Fatalf("discoverTabbyAPI = %+v, %v; want the listed model plus the embedding one", models, err)
	}
	if c := decodeCaps(t, models[0]); !c.Streaming || c.StructuredOutput || c.ToolCalling {
		t.Errorf("caps without a card = %+v, want streaming only", c)
	}
	if models[0].ContextLength == nil || *models[0].ContextLength != 4096 {
		t.Errorf("context = %v, want 4096 from the listing's meta.n_ctx", models[0].ContextLength)
	}
	if models[1].ModelID != "all-MiniLM-L6-v2" || models[1].Modality != "embedding" {
		t.Errorf("second model = %+v, want the embedding model", models[1])
	}
}

func TestDiscoverTabbyAPI_RejectsMalformedListing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"object":"list","data":"nope"}`))
	}))
	defer srv.Close()
	svc := &DiscoveryService{httpClient: srv.Client()}
	provider := &Provider{ID: uuid.New(), ProviderType: "tabbyapi", BaseURL: srv.URL + "/v1"}
	if _, err := svc.discoverTabbyAPI(context.Background(), provider, ""); err == nil {
		t.Fatal("malformed listing accepted")
	}
}

// In the fixed order TabbyAPI's own fingerprint comes before KoboldCPP's, and
// a TabbyAPI added AS KoboldCPP is told apart by the emulator check after its
// KoboldCpp impersonation matched.
func TestIdentifyLocalServer_TabbyAPI(t *testing.T) {
	srv, paths := tabbyAPIServer(t, tabbyAPIListingBody, tabbyAPIModelCardBody, "")
	defer srv.Close()

	svc := &DiscoveryService{httpClient: srv.Client()}
	for _, expected := range []string{"", "tabbyapi", "koboldcpp"} {
		*paths = nil
		got, err := svc.IdentifyLocalServer(context.Background(), srv.URL+"/v1", "", expected)
		if err != nil {
			t.Fatalf("IdentifyLocalServer(expected %q): %v", expected, err)
		}
		if got.Type != "tabbyapi" {
			t.Errorf("IdentifyLocalServer(expected %q) = %q, want tabbyapi", expected, got.Type)
		}
		if expected != "koboldcpp" && slices.Contains(*paths, "/api/extra/version") {
			t.Errorf("IdentifyLocalServer(expected %q) asked KoboldCPP's route before TabbyAPI's own: %v", expected, *paths)
		}
	}
}

// A real KoboldCPP added as KoboldCPP gets the one extra serviceinfo GET,
// which it answers with its own serviceinfo naming KoboldCpp, and stays
// KoboldCPP.
func TestIdentifyLocalServer_RealKoboldCPPAsKoboldCPP(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/extra/version":
			_, _ = w.Write([]byte(`{"result":"KoboldCpp","version":"1.98"}`))
		case "/.well-known/serviceinfo":
			_, _ = w.Write([]byte(`{"version":0.2,"software":{"name":"KoboldCpp","version":"1.98","repository":"https://github.com/LostRuins/koboldcpp"},"api":{"koboldai":{"name":"KoboldAI API","relative_url":"/api","version":1}}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	svc := &DiscoveryService{httpClient: srv.Client()}
	got, err := svc.IdentifyLocalServer(context.Background(), srv.URL, "", "koboldcpp")
	if err != nil || got.Type != "koboldcpp" || got.Version != "1.98" {
		t.Fatalf("IdentifyLocalServer = %+v, %v; want koboldcpp 1.98", got, err)
	}
	if want := []string{"/api/extra/version", "/.well-known/serviceinfo"}; !slices.Equal(paths, want) {
		t.Errorf("probed %v, want %v", paths, want)
	}
}

func TestTabbyAPIFingerprintFailsClosed(t *testing.T) {
	for body, want := range map[string]bool{
		tabbyAPIServiceInfoBody:                   true,
		`{"software":{"name":"TabbyAPI"}}`:        true,
		`{"software":{"name":"KoboldCpp"}}`:       false,
		`{"software":{"name":""}}`:                false,
		`{"software":"TabbyAPI"}`:                 false,
		`{"detail":"Not Found"}`:                  false,
		`{"result":"KoboldCpp","version":"1.74"}`: false,
		`not json`: false,
	} {
		if got := isTabbyAPIServiceInfo([]byte(body)); got != want {
			t.Errorf("isTabbyAPIServiceInfo(%s) = %v, want %v", body, got, want)
		}
	}
}
