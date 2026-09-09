package proxy

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hugalafutro/model-hotel/internal/provider"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// OpenCode Go rejects a chat request that carries no x-opencode-session with a
// 400 MissingSessionID, so every upstream request the gateway builds for that
// provider type has to carry one, and no other provider may see it.
func TestBuildCandidateRequest_OpenCodeGoSessionHeader(t *testing.T) {
	tests := []struct {
		name         string
		providerType string
		session      string
		want         string
	}{
		{"opencode-go carries the session", "opencode-go", "mh-abc123", "mh-abc123"},
		{"a client id rides through unchanged", "opencode-go", "ses_client_1", "ses_client_1"},
		{"openai never sees it", "openai", "mh-abc123", ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := make(chan string, 1)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got <- r.Header.Get(util.OpenCodeGoSessionHeader)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"choices":[]}`))
			}))
			defer srv.Close()

			h := newIntegrationHandler()
			t.Cleanup(func() { stopUnitHandler(h) })

			st, cand := probeStateForServer(srv.URL)
			st.isStreaming = false
			st.opencodeSession = tc.session
			cand.provider.ProviderType = tc.providerType

			req, _, _, err := h.buildCandidateRequest(context.Background(), st, cand)
			if err != nil {
				t.Fatalf("buildCandidateRequest: %v", err)
			}
			resp, err := srv.Client().Do(req)
			if err != nil {
				t.Fatalf("upstream request: %v", err)
			}
			_ = resp.Body.Close()

			if seen := <-got; seen != tc.want {
				t.Errorf("upstream saw %s = %q, want %q", util.OpenCodeGoSessionHeader, seen, tc.want)
			}
		})
	}
}

// The retirement probe has no client and no virtual key, and a 400 refusal for
// a missing session is not evidence that a model is gone. It carries its own
// fixed session so it draws a real answer.
func TestNewProbeState_CarriesAProbeSession(t *testing.T) {
	t.Parallel()

	_, cand := probeStateForServer("https://opencode.invalid")
	st := newProbeState(cand, endpointTypeChat, "/chat/completions")
	if st.opencodeSession != util.OpenCodeGoProbeSession {
		t.Errorf("probe session = %q, want %q", st.opencodeSession, util.OpenCodeGoProbeSession)
	}
}

// ...and if such a refusal ever did reach the classifier, it must not read as a
// retirement: three of them would disable a model the provider still serves.
func TestClassifyUpstreamError_MissingSessionIDIsNotARetirement(t *testing.T) {
	t.Parallel()

	body := `{"type":"error","error":{"type":"MissingSessionID","message":"Error from provider (Console Go): Request is missing x-opencode-session and cannot be routed efficiently"}}`
	if kind, _ := classifyUpstreamError(http.StatusBadRequest, body, "glm-5.2"); kind == KindProviderModelGone {
		t.Errorf("a missing-session refusal must not classify as a retirement, got %q", kind)
	}
}

// A hedged attempt builds its upstream request through buildCandidateRequest
// like every other attempt, so the session id survives the race: a backup that
// wins must land on the same upstream session as the probe it overtook.
func TestProbeStreamingCandidate_CarriesTheSession(t *testing.T) {
	h := newIntegrationHandler()
	t.Cleanup(func() { stopUnitHandler(h) })

	got := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got <- r.Header.Get(util.OpenCodeGoSessionHeader)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"}}]}\n\ndata: [DONE]\n\n")
	}))
	defer srv.Close()

	st, cand := probeStateForServer(srv.URL)
	st.opencodeSession = "ses_hedged"
	cand.provider.ProviderType = "opencode-go"

	res := h.probeStreamingCandidate(context.Background(), st, cand, 0, 5*time.Second, 30*time.Second)
	if res.resp != nil {
		_ = res.resp.Body.Close()
	}
	if !res.won {
		t.Fatalf("expected a win, got reqErr=%+v", res.reqErr)
	}
	if seen := <-got; seen != "ses_hedged" {
		t.Errorf("hedged upstream saw %s = %q, want %q", util.OpenCodeGoSessionHeader, seen, "ses_hedged")
	}
}

// opencodeGoEnv builds the standard proxy fixtures around upstream and corrects
// the provider's type to opencode-go, the only type the session header is
// stamped for.
func opencodeGoEnv(t *testing.T, upstream *httptest.Server) *testProxyEnv {
	t.Helper()
	env := newTestProxyEnvWithUpstream(t, upstream)
	if _, err := testDB.Pool().Exec(context.Background(),
		`UPDATE providers SET provider_type = 'opencode-go' WHERE id = $1`, env.ProviderID); err != nil {
		t.Fatalf("set provider type: %v", err)
	}
	// The row was cached by the create above, and resolve reads the cache
	// before the table.
	provider.InvalidateProviderCache()
	return env
}

// sessionCapturingUpstream answers a chat completion and reports the session id
// it was sent.
func sessionCapturingUpstream(seen chan<- string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen <- r.Header.Get(util.OpenCodeGoSessionHeader)
		var reqBody map[string]any
		_ = json.NewDecoder(r.Body).Decode(&reqBody)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "chatcmpl-x", "object": "chat.completion", "created": time.Now().Unix(),
			"model": reqBody["model"],
			"choices": []map[string]any{
				{"index": 0, "message": map[string]any{"role": "assistant", "content": "hi"}, "finish_reason": "stop"},
			},
			"usage": map[string]any{"prompt_tokens": 5, "completion_tokens": 3, "total_tokens": 8},
		})
	}))
}

// End-to-end on the keyless surface: admin chat authenticates a dashboard
// session rather than a virtual key, so it has no key to derive a session from
// and every such request shares the one fixed id.
func TestChatCompletions_E2E_AdminChatCarriesTheAdminSession(t *testing.T) {
	seen := make(chan string, 1)
	upstream := sessionCapturingUpstream(seen)
	defer upstream.Close()

	env := opencodeGoEnv(t, upstream)
	body := `{"model":"` + env.ProviderName + `/` + env.ModelName + `","messages":[{"role":"user","content":"hello"}],"stream":false}`
	req := httptest.NewRequest("POST", "/api/chat/chat", strings.NewReader(body))
	// Mirrors RegisterAdminChat: a surface name, and no virtual key hash.
	req = req.WithContext(context.WithValue(req.Context(), virtualKeyNameKey, "chat"))
	w := httptest.NewRecorder()
	env.Handler.ChatCompletions(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, body = %s", w.Code, w.Body.String())
	}
	if got := <-seen; got != util.OpenCodeGoAdminSession {
		t.Errorf("upstream saw %s = %q, want %q", util.OpenCodeGoSessionHeader, got, util.OpenCodeGoAdminSession)
	}
}

// End-to-end on the native Anthropic surface: /v1/messages has its own handler
// but shares ingestRequest, so a keyed request there carries the same derived
// session id a /v1/chat/completions request with that key would.
func TestMessages_E2E_CarriesTheDerivedSession(t *testing.T) {
	seen := make(chan string, 1)
	upstream := sessionCapturingUpstream(seen)
	defer upstream.Close()

	env := opencodeGoEnv(t, upstream)
	body := `{"model":"` + env.ProviderName + `/` + env.ModelName + `","max_tokens":50,"messages":[{"role":"user","content":"hello"}]}`
	w := doMessagesRequest(env, body)

	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, body = %s", w.Code, w.Body.String())
	}
	want := util.OpenCodeGoSession("", env.KeyHash)
	if got := <-seen; got != want {
		t.Errorf("upstream saw %s = %q, want %q", util.OpenCodeGoSessionHeader, got, want)
	}
}
