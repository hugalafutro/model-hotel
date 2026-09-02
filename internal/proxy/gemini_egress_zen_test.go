package proxy

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/provider"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// OpenCode Zen routes each model family to a different upstream dialect. Its
// Gemini models are a passthrough to Google, so an OpenAI-shaped body posted to
// Zen's /chat/completions is rejected by Google itself with
// `Invalid JSON request body: Missing key at ["contents"]`. These tests pin the
// three things that had to be true for those models to work at all, each
// verified against the live service on 2026-07-30.

func TestIsGeminiEgressAttempt_ZenGeminiOnly(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		providerType string
		modelID      string
		st           *requestState
		want         bool
	}{
		{"zen gemini uses native dialect", "opencode-zen", "gemini-3.6-flash", &requestState{}, true},
		{"zen gemini pro", "opencode-zen", "gemini-3.1-pro", &requestState{}, true},
		{"zen gemini flash lite", "opencode-zen", "gemini-3.5-flash-lite", &requestState{}, true},
		{"zen claude stays on chat completions", "opencode-zen", "claude-opus-5", &requestState{}, false},
		{"zen gpt stays on chat completions", "opencode-zen", "gpt-5.6-sol", &requestState{}, false},
		{"zen glm stays on chat completions", "opencode-zen", "glm-5.2", &requestState{}, false},
		{"vertex express always native", "vertex-express", "gemini-2.5-pro", &requestState{}, true},
		{"other providers never", "opencode-go", "gemini-3.6-flash", &requestState{}, false},
		{"google direct is not egress", "google", "gemini-3.6-flash", &requestState{}, false},
		// An explicit endpoint override or a multipart body means the caller
		// already decided the shape; never re-route those.
		{"endpoint override opts out", "opencode-zen", "gemini-3.6-flash", &requestState{endpointPath: "/embeddings"}, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := isGeminiEgressAttempt(tc.st, tc.providerType, tc.modelID, ""); got != tc.want {
				t.Errorf("isGeminiEgressAttempt(%q, %q) = %v, want %v", tc.providerType, tc.modelID, got, tc.want)
			}
		})
	}
}

func TestGeminiEgressEndpoint_PerProvider(t *testing.T) {
	t.Parallel()

	tests := []struct {
		providerType string
		stream       bool
		want         string
	}{
		// Vertex express keys only work on the publisher routes.
		{"vertex-express", false, "/publishers/google/models/gemini-3.6-flash:generateContent"},
		{"vertex-express", true, "/publishers/google/models/gemini-3.6-flash:streamGenerateContent?alt=sse"},
		// Zen exposes the same verbs directly under its own /models.
		{"opencode-zen", false, "/models/gemini-3.6-flash:generateContent"},
		{"opencode-zen", true, "/models/gemini-3.6-flash:streamGenerateContent?alt=sse"},
	}

	for _, tc := range tests {
		got := geminiEgressEndpoint(tc.providerType, "gemini-3.6-flash", tc.stream)
		if got != tc.want {
			t.Errorf("geminiEgressEndpoint(%q, stream=%v) = %q, want %q", tc.providerType, tc.stream, got, tc.want)
		}
	}
}

// TestSetGeminiEgressAuth_ZenUsesGoogleHeader pins the auth scheme. Zen's Gemini
// passthrough answers a Bearer token with
// {"type":"AuthError","message":"Missing API key."} — only x-goog-api-key is
// accepted — even though every other Zen family needs Bearer. That is why this
// cannot be folded into util.SetProviderAuthHeaders, which keys off provider
// type alone.
func TestSetGeminiEgressAuth_ZenUsesGoogleHeader(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodPost, "https://opencode.ai/zen/v1/models/gemini-3.6-flash:generateContent", http.NoBody)
	setGeminiEgressAuth(req, "opencode-zen", "sk-test-key")

	if got := req.Header.Get("x-goog-api-key"); got != "sk-test-key" {
		t.Errorf("x-goog-api-key = %q, want the api key", got)
	}
	if got := req.Header.Get("Authorization"); got != "" {
		t.Errorf("Authorization must not be set for Zen's Gemini route, got %q", got)
	}

	// Vertex express keeps going through the shared helper.
	vreq := httptest.NewRequest(http.MethodPost, "https://example.com/v1/publishers/google/models/x:generateContent", http.NoBody)
	setGeminiEgressAuth(vreq, "vertex-express", "vertex-key")
	if got := vreq.Header.Get("x-goog-api-key"); got != "vertex-key" {
		t.Errorf("vertex-express x-goog-api-key = %q", got)
	}

	// A keyless provider must not gain an empty header.
	kreq := httptest.NewRequest(http.MethodPost, "https://opencode.ai/zen/v1/models/x:generateContent", http.NoBody)
	setGeminiEgressAuth(kreq, "opencode-zen", "")
	if _, present := kreq.Header["X-Goog-Api-Key"]; present {
		t.Error("keyless provider must not set an empty x-goog-api-key")
	}
}

// TestZenGeminiTargetURL checks the assembled URL against the one verified to
// return a 200 from the live service.
func TestZenGeminiTargetURL(t *testing.T) {
	t.Parallel()

	endpoint := geminiEgressEndpoint("opencode-zen", "gemini-3.6-flash", false)
	got := util.BuildProviderTargetURL("https://opencode.ai/zen/v1", "opencode-zen", endpoint)
	want := "https://opencode.ai/zen/v1/models/gemini-3.6-flash:generateContent"
	if got != want {
		t.Errorf("target URL = %q, want %q", got, want)
	}
	if strings.Contains(got, "/publishers/") {
		t.Error("Zen must not get the Vertex publisher prefix")
	}
}

// TestBuildGeminiRequest_ZenWireContract asserts the exact request MH puts on
// the wire for a Zen Gemini model, against the one verified by hand to return
// 200 from the live service on 2026-07-30:
//
//	POST https://opencode.ai/zen/v1/models/gemini-3.6-flash:generateContent
//	x-goog-api-key: <key>
//	{"contents":[...]}
//
// Posting the same request with Bearer auth returns AuthError "Missing API
// key."; posting an OpenAI-shaped body to /chat/completions returns Google's
// `Missing key at ["contents"]`. Both were the bug this replaces.
func TestBuildGeminiRequest_ZenWireContract(t *testing.T) {
	t.Parallel()

	h := &Handler{}
	st := &requestState{
		bodyBytes: []byte(`{"model":"gemini-3.6-flash","messages":[{"role":"user","content":"say ok"}]}`),
		reqModel:  "gemini-3.6-flash",
	}
	candidate := modelCandidate{
		model:    &model.Model{ModelID: "gemini-3.6-flash"},
		provider: &provider.Provider{Name: "OpenCode Zen", BaseURL: "https://opencode.ai/zen/v1"},
		apiKey:   "sk-zen-test",
	}

	req, _, targetURL, err := h.buildGeminiRequest(context.Background(), st, candidate, "opencode-zen")
	if err != nil {
		t.Fatalf("buildGeminiRequest: %v", err)
	}

	const want = "https://opencode.ai/zen/v1/models/gemini-3.6-flash:generateContent"
	if targetURL != want {
		t.Errorf("target URL = %q, want %q", targetURL, want)
	}
	if req.URL.String() != want {
		t.Errorf("request URL = %q, want %q", req.URL.String(), want)
	}
	if got := req.Header.Get("x-goog-api-key"); got != "sk-zen-test" {
		t.Errorf("x-goog-api-key = %q, want the key", got)
	}
	if got := req.Header.Get("Authorization"); got != "" {
		t.Errorf("Authorization must be absent (Zen answers Bearer with AuthError), got %q", got)
	}

	body, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	if _, ok := parsed["contents"]; !ok {
		t.Errorf(`translated body must carry "contents" (Google rejects anything else), got %s`, body)
	}
	if _, ok := parsed["messages"]; ok {
		t.Error(`translated body must not still carry OpenAI "messages"`)
	}
	if _, ok := parsed["chat_template_args"]; ok {
		t.Error("chat_template_args must never reach Zen")
	}
}
