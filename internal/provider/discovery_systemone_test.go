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

// A vllm-srun 0.2.0 worker's GET /v1/models with vela3-lite loaded, as the
// live server answers it (the card's engine, device and plugin members cut).
const systemOneWorkerListingBody = `{"object":"list","api_version":"5.0.0","data":[{"id":"vela3-lite","object":"model","owned_by":"vllm-sr","family":"vela3_d3","repo":"vllm-sr/vela3-lite","surfaces":["decisions"],"question_types":["choice","noul","score","span"],"limits":{"max_input_tokens":262144,"min_options":2,"max_options":255},"licence":"apache-2.0","ready":true,"status":"ready","presets":["pii","halu"],"modalities":["text","image"],"image_placements":["state"]}],"limits":{"max_bundle_tasks":64,"max_request_bytes":33554432},"models":[{"name":"vela3-lite","description":"vela3_d3 decision model","release_date":"","version":"5.0","features":["choice","noul","score","span"]}]}`

// A Router frontend's GET /v1/systemone/models: one model whose deployment
// states what it answers, and one forwarded to another System One server,
// about which the frontend's configuration says nothing.
const systemOneFrontendListingBody = `{"object":"list","data":[{"id":"vllm-sr/vela3-fast","object":"model","api":"systemone","routing":false,"question_types":["choice","noul"],"modalities":["text"]},{"id":"upstream-judge","object":"model","api":"systemone","routing":true}]}`

// systemOneServer answers as a worker (native listing 404, /v1/models the
// worker listing) or, with frontend set, as a Router frontend (native listing
// served, /v1/models its chat models).
func systemOneServer(t *testing.T, frontend bool) (*httptest.Server, *[]string) {
	t.Helper()
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v1/systemone/models" && frontend:
			_, _ = w.Write([]byte(systemOneFrontendListingBody))
		case r.URL.Path == "/v1/models" && frontend:
			_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"qwen3-8b","object":"model"}]}`))
		case r.URL.Path == "/v1/models":
			_, _ = w.Write([]byte(systemOneWorkerListingBody))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":{"code":"invalid_request","message":"no route ` + r.URL.Path + `"}}`))
		}
	}))
	return srv, &paths
}

func TestDiscoverSystemOne_Worker(t *testing.T) {
	srv, paths := systemOneServer(t, false)
	defer srv.Close()

	svc := &DiscoveryService{httpClient: srv.Client()}
	provider := &Provider{ID: uuid.New(), ProviderType: "systemone", BaseURL: srv.URL + "/v1"}
	models, err := svc.discoverSystemOne(context.Background(), provider, "")
	if err != nil {
		t.Fatalf("discoverSystemOne: %v", err)
	}
	if !slices.Equal(*paths, []string{"/v1/systemone/models", "/v1/models"}) {
		t.Errorf("asked %v, want the native listing first and the worker's own after its 404", *paths)
	}
	if len(models) != 1 || models[0].ModelID != "vela3-lite" {
		t.Fatalf("got %+v, want the one loaded model", models)
	}
	m := models[0]
	if m.InputModalities != `["text","image"]` || m.OutputModalities != `["decisions"]` {
		t.Errorf("modalities = %s / %s, want text and image in, decisions out", m.InputModalities, m.OutputModalities)
	}
	if m.ContextLength == nil || *m.ContextLength != 262144 || !m.LiveMeta.ContextLength {
		t.Errorf("context = %v (live %v), want 262144 live from limits.max_input_tokens", m.ContextLength, m.LiveMeta.ContextLength)
	}
	if !strings.Contains(m.Description, "choice, noul, score, span") {
		t.Errorf("description = %q, want the question types the model answers", m.Description)
	}
	if m.Params != `{"question_types":["choice","noul","score","span"]}` {
		t.Errorf("params = %s, want the question types for the Test probe", m.Params)
	}
	if m.OwnedBy != "systemone" {
		t.Errorf("owned_by = %q, want the family", m.OwnedBy)
	}
	// The class is what the rest of the gateway routes on: a decision model
	// must stay out of the chat pickers and lose the chat-only capabilities.
	NormalizeModelClassification(m)
	if m.Modality != "decision" {
		t.Errorf("class = %q, want decision", m.Modality)
	}
	if c := decodeCaps(t, m); c.Streaming || c.ToolCalling || !c.Vision {
		t.Errorf("caps = %+v, want no chat capability and image input", c)
	}
}

// A Router lists its chat models on /v1/models; the native listing is what
// this provider type serves, and it is never asked for the other.
func TestDiscoverSystemOne_Frontend(t *testing.T) {
	srv, paths := systemOneServer(t, true)
	defer srv.Close()

	svc := &DiscoveryService{httpClient: srv.Client()}
	provider := &Provider{ID: uuid.New(), ProviderType: "systemone", BaseURL: srv.URL + "/v1"}
	models, err := svc.discoverSystemOne(context.Background(), provider, "listener-key")
	if err != nil {
		t.Fatalf("discoverSystemOne: %v", err)
	}
	if !slices.Equal(*paths, []string{"/v1/systemone/models"}) {
		t.Errorf("asked %v, want the native listing alone", *paths)
	}
	if len(models) != 2 {
		t.Fatalf("got %d models, want both published ones", len(models))
	}
	for _, m := range models {
		if m.OutputModalities != `["decisions"]` || m.InputModalities != `["text"]` {
			t.Errorf("%s modalities = %s / %s, want text in, decisions out", m.ModelID, m.InputModalities, m.OutputModalities)
		}
	}
	// The forwarded model's row says only that it is a System One model.
	if got := models[1]; got.Description != "System One decision model" || got.Params != "{}" {
		t.Errorf("forwarded model description = %q params = %s, want no question types claimed", got.Description, got.Params)
	}
}

func TestDiscoverSystemOne_Failures(t *testing.T) {
	for name, tc := range map[string]struct {
		native     int
		nativeBody string
		wantErr    string
		wantPaths  []string
	}{
		// A refused key is not a missing route: falling through would list
		// a Router's chat models as if they were its decision models.
		"refused native listing fails the scan": {http.StatusUnauthorized, `{"error":"invalid_api_key"}`, "failed to fetch models", []string{"/v1/systemone/models"}},
		"undecodable listing fails the scan":    {http.StatusOK, `not json`, "failed to decode response", []string{"/v1/systemone/models"}},
	} {
		t.Run(name, func(t *testing.T) {
			var paths []string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				paths = append(paths, r.URL.Path)
				w.WriteHeader(tc.native)
				_, _ = w.Write([]byte(tc.nativeBody))
			}))
			defer srv.Close()

			svc := &DiscoveryService{httpClient: srv.Client()}
			provider := &Provider{ID: uuid.New(), Name: "s1", ProviderType: "systemone", BaseURL: srv.URL + "/v1"}
			models, err := svc.discoverSystemOne(context.Background(), provider, "k")
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) || models != nil {
				t.Fatalf("got %v, %v; want no models and an error containing %q", models, err, tc.wantErr)
			}
			if !slices.Equal(paths, tc.wantPaths) {
				t.Errorf("asked %v, want %v", paths, tc.wantPaths)
			}
		})
	}
}

// Each row is filed by what it says it serves, and a member in a shape of its
// own costs that member only.
func TestBuildSystemOneModel_Variants(t *testing.T) {
	provider := &Provider{ID: uuid.New()}
	for name, tc := range map[string]struct {
		entry       SystemOneModel
		wantClass   string
		wantOutput  string
		wantInput   string
		wantContext int
	}{
		"decisions surface without question types": {
			entry:      SystemOneModel{ID: "d3-edge", Surfaces: []byte(`["decisions"]`)},
			wantOutput: `["decisions"]`, wantInput: `["text"]`,
		},
		"embedding worker": {
			entry:     SystemOneModel{ID: "vela-embedding", Surfaces: []byte(`["embeddings"]`), Limits: []byte(`{"max_input_tokens":"8192"}`)},
			wantClass: "embedding", wantContext: 8192,
		},
		"reranking worker": {
			entry:     SystemOneModel{ID: "vela-rerank", Surfaces: []byte(`["rerank"]`)},
			wantClass: "rerank",
		},
		"a row that says nothing is left to the name": {
			entry: SystemOneModel{ID: "mystery", Surfaces: []byte(`"decisions"`), QuestionTypes: []byte(`{"choice":true}`), Limits: []byte(`[1]`)},
		},
		"non-string entries are dropped, the rest kept": {
			entry:      SystemOneModel{ID: "mixed", QuestionTypes: []byte(`["choice",7,null,"noul"]`), Modalities: []byte(`[1]`)},
			wantOutput: `["decisions"]`, wantInput: `["text"]`,
		},
	} {
		t.Run(name, func(t *testing.T) {
			m := buildSystemOneModel(provider, tc.entry)
			if m.Modality != tc.wantClass || m.OutputModalities != tc.wantOutput || m.InputModalities != tc.wantInput {
				t.Errorf("class %q, modalities %s / %s; want class %q, %s / %s", m.Modality, m.InputModalities, m.OutputModalities, tc.wantClass, tc.wantInput, tc.wantOutput)
			}
			got := 0
			if m.ContextLength != nil {
				got = *m.ContextLength
			}
			if got != tc.wantContext {
				t.Errorf("context = %d, want %d", got, tc.wantContext)
			}
		})
	}
	if m := buildSystemOneModel(provider, SystemOneModel{ID: "mixed", QuestionTypes: []byte(`["choice",7,null,"noul"]`)}); !strings.HasSuffix(m.Description, "(choice, noul)") {
		t.Errorf("description = %q, want only the string question types", m.Description)
	}
}

// Rows without an id name nothing that could be routed to.
func TestDiscoverSystemOne_SkipsUnnamedRows(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"object":"list","data":[{"api":"systemone"},{"id":"kept","api":"systemone"}]}`))
	}))
	defer srv.Close()

	svc := &DiscoveryService{httpClient: srv.Client()}
	models, err := svc.discoverSystemOne(context.Background(), &Provider{ID: uuid.New(), BaseURL: srv.URL + "/v1"}, "")
	if err != nil || len(models) != 1 || models[0].ModelID != "kept" {
		t.Fatalf("got %+v, %v; want the one named row", models, err)
	}
}

// Both forms are confirmed as systemone, whatever the operator expected, and a
// worker added as systemone is asked nothing but its own listing.
func TestIdentifyLocalServer_SystemOne(t *testing.T) {
	for _, frontend := range []bool{false, true} {
		srv, paths := systemOneServer(t, frontend)
		svc := &DiscoveryService{httpClient: srv.Client()}
		for _, expected := range []string{"", "systemone", "ollama"} {
			*paths = nil
			got, err := svc.IdentifyLocalServer(context.Background(), srv.URL+"/v1", "", expected)
			if err != nil {
				t.Fatalf("IdentifyLocalServer(frontend %v, expected %q): %v", frontend, expected, err)
			}
			if got.Type != "systemone" {
				t.Errorf("IdentifyLocalServer(frontend %v, expected %q) = %q, want systemone", frontend, expected, got.Type)
			}
			if !frontend && got.Version != "5.0.0" {
				t.Errorf("worker version = %q, want its API contract version", got.Version)
			}
			if !frontend && expected == "systemone" && !slices.Equal(*paths, []string{"/v1/models"}) {
				t.Errorf("a worker added as systemone was asked %v, want its own listing alone", *paths)
			}
		}
		srv.Close()
	}
}

// An OpenAI-compatible server's /v1/models is the same envelope without the
// contract version, and must not pass for a worker.
func TestSystemOneFingerprintsFailClosed(t *testing.T) {
	for body, want := range map[string]bool{
		systemOneWorkerListingBody:                              true,
		`{"object":"list","api_version":"5.0.0","data":[]}`:     true,
		sglangListingBody:                                       false,
		`{"object":"list","api_version":5,"data":[]}`:           false,
		`{"api_version":"5.0.0","data":[]}`:                     false,
		`{"error":{"code":"invalid_request","message":"nope"}}`: false,
		`not json`: false,
		`{"object":"list","api_version":"","data":[{"id":"a"}]}`: false,
	} {
		if _, got := isSystemOneWorkerListing([]byte(body)); got != want {
			t.Errorf("isSystemOneWorkerListing(%s) = %v, want %v", body, got, want)
		}
	}
	for body, want := range map[string]bool{
		systemOneFrontendListingBody:                             true,
		systemOneWorkerListingBody:                               false,
		`{"object":"list","data":[]}`:                            false,
		`{"object":"list","data":[{"id":"gpt","api":"openai"}]}`: false,
		`{"data":[{"id":"a","api":"systemone"}]}`:                false,
		`{"error":"Unexpected endpoint or method."}`:             false,
		`not json`: false,
	} {
		if got := isSystemOneFrontendListing([]byte(body)); got != want {
			t.Errorf("isSystemOneFrontendListing(%s) = %v, want %v", body, got, want)
		}
	}
}

// A saved address that now answers as an OpenAI-compatible server (repointed,
// or a proxy in front) must fail the scan: its chat models are not System One
// models, and importing them would retire the real ones as missing.
func TestDiscoverSystemOne_RefusesAListingThatIsNotSystemOnes(t *testing.T) {
	for name, tc := range map[string]struct {
		native     string // "" answers the native route with 404
		models     string
		wantSource string
	}{
		"openai server behind a worker address": {
			models:     `{"object":"list","data":[{"id":"gpt-4o","object":"model","owned_by":"openai"}]}`,
			wantSource: "worker",
		},
		"catch-all proxy answering the native route with chat models": {
			native:     `{"object":"list","data":[{"id":"gpt-4o","object":"model"}]}`,
			wantSource: "frontend",
		},
		"native route answering something that is not a list": {
			native:     `{"data":[{"id":"a","api":"systemone"}]}`,
			wantSource: "frontend",
		},
	} {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path == "/v1/systemone/models" && tc.native != "":
					_, _ = w.Write([]byte(tc.native))
				case r.URL.Path == "/v1/models":
					_, _ = w.Write([]byte(tc.models))
				default:
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer srv.Close()

			svc := &DiscoveryService{httpClient: srv.Client()}
			models, err := svc.discoverSystemOne(context.Background(), &Provider{ID: uuid.New(), Name: "s1", BaseURL: srv.URL + "/v1"}, "")
			if err == nil || models != nil || !strings.Contains(err.Error(), "the "+tc.wantSource+" listing") {
				t.Fatalf("got %v, %v; want no models and the %s listing refused", models, err, tc.wantSource)
			}
		})
	}
}

// A frontend that publishes nothing yet lists nothing, and that is an answer.
func TestDiscoverSystemOne_EmptyFrontendListing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"object":"list","data":[]}`))
	}))
	defer srv.Close()

	svc := &DiscoveryService{httpClient: srv.Client()}
	models, err := svc.discoverSystemOne(context.Background(), &Provider{ID: uuid.New(), BaseURL: srv.URL + "/v1"}, "")
	if err != nil || len(models) != 0 {
		t.Fatalf("got %+v, %v; want an empty listing accepted", models, err)
	}
}
