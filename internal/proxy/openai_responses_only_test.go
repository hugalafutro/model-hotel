package proxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hugalafutro/model-hotel/internal/paramrewrite"
)

const plainChatBody = `{"model":"gpt-5.5-pro-2026-04-23","messages":[{"role":"user","content":"hi"}]}`

// A pro-tier model routes to /v1/responses from its first request, tools or
// not, by name; nothing has to be learned.
func TestShouldUseResponsesAttempt_ProTierByName(t *testing.T) {
	h := &Handler{}
	st := &requestState{bodyBytes: []byte(plainChatBody)}
	cand := responsesTestCandidate("https://api.openai.com/v1")
	cand.model.ModelID = "gpt-5.5-pro-2026-04-23"
	if !h.shouldUseResponsesAttempt(st, cand, "openai") {
		t.Fatal("a pro-tier model with a plain body must route to /v1/responses preemptively")
	}
	cand.model.ModelID = "gpt-5.5"
	if h.shouldUseResponsesAttempt(st, cand, "openai") {
		t.Fatal("an unflagged non-pro model must keep chat-completions")
	}
	// The name rule is OpenAI's own host only: a relay typed "openai" (the
	// type of every unrecognised host) re-exposing the name may have no
	// /v1/responses, and a wrong reroute there has no way back.
	relay := responsesTestCandidate("https://llm.corp.internal/v1")
	relay.model.ModelID = "gpt-5.5-pro-2026-04-23"
	if h.shouldUseResponsesAttempt(st, relay, "openai") {
		t.Fatal("the pro-tier name must not route a relay to /v1/responses")
	}
	// A relay that was learned from its own refusal still routes.
	h.responsesRequiredCache.Store("openai:gpt-5.5-pro-2026-04-23", responsesAlways)
	if !h.shouldUseResponsesAttempt(st, relay, "openai") {
		t.Fatal("a learned always requirement must route on any host")
	}
	// A tools-learned model still keeps plain requests on chat-completions.
	h.responsesRequiredCache.Store("openai:gpt-5.5", responsesForTools)
	if h.shouldUseResponsesAttempt(st, cand, "openai") {
		t.Fatal("a tools-learned model must not route a tools-free request")
	}
	h.responsesRequiredCache.Store("openai:gpt-5.5", responsesAlways)
	if !h.shouldUseResponsesAttempt(st, cand, "openai") {
		t.Fatal("an always-learned model must route every request")
	}
}

// The 404 "not a chat model" refusal on a tools-free request learns the model
// as Responses-only and re-issues the request against /v1/responses.
func TestRetryWithResponses_ResponsesOnly404(t *testing.T) {
	var gotPath string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"resp_1","object":"response","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hi"}]}],"usage":{"input_tokens":1,"output_tokens":1}}`)
	}))
	defer upstream.Close()
	h := &Handler{upstreamTransport: &http.Transport{}}
	st := &requestState{bodyBytes: []byte(plainChatBody), failoverTimeout: 5 * time.Second}
	cand := responsesTestCandidate(upstream.URL + "/v1")
	cand.model.ModelID = "gpt-5.5-pro-2026-04-23"
	refusal := `{"error":{"message":"This is not a chat model and thus not supported in the v1/chat/completions endpoint. Did you mean to use v1/completions?","type":"invalid_request_error","param":"model","code":null}}`
	resp := &http.Response{StatusCode: 404, Body: io.NopCloser(strings.NewReader(refusal))}
	r := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
	var dialMs float64
	res, handled := h.retryWithResponses(r, st, cand, "openai", resp, 0, &dialMs, func() {}, "")
	if !handled || !res.retried {
		t.Fatalf("handled=%v retried=%v, want the request re-issued", handled, res.retried)
	}
	if !strings.HasSuffix(gotPath, "/responses") {
		t.Fatalf("retry went to %q, want /v1/responses", gotPath)
	}
	if v, ok := h.responsesRequiredCache.Load("openai:gpt-5.5-pro-2026-04-23"); !ok || v != responsesAlways {
		t.Fatalf("learned %v, want the always requirement", v)
	}
	if res.resp != nil && res.resp.Body != nil {
		_ = res.resp.Body.Close()
	}
	if res.retryCancel != nil {
		res.retryCancel()
	}
}

// The attempt loop hands a chat-completions 404 from OpenAI's own host to the
// learnable-refusal path, and nothing else that is not a 400.
func TestIsLearnableRefusal(t *testing.T) {
	chat := &requestState{bodyBytes: []byte(plainChatBody)}
	const openai, relay = "https://api.openai.com/v1", "https://llm.corp.internal/v1"
	for _, tc := range []struct {
		status   int
		provider string
		baseURL  string
		st       *requestState
		want     bool
	}{
		{400, "anthropic", relay, chat, true},
		{404, "openai", openai, chat, true},
		{404, "openai", "HTTPS://API.OPENAI.COM/v1", chat, true},
		{404, "openai", relay, chat, false},
		{404, "custom", openai, chat, false},
		{404, "openai", openai, &requestState{endpointPath: "/embeddings"}, false},
		{500, "openai", openai, chat, false},
	} {
		if got := isLearnableRefusal(tc.status, tc.provider, tc.baseURL, tc.st); got != tc.want {
			t.Errorf("isLearnableRefusal(%d, %q, %q) = %v, want %v", tc.status, tc.provider, tc.baseURL, got, tc.want)
		}
	}
}

// A learnable 404 is read at the classifier's cap; a 400 keeps the parse-sized
// cap the Responses requirement needs.
func TestReadLearnable400_CapFollowsStatus(t *testing.T) {
	big := strings.Repeat("x", failoverErrorClassifyCap+100)
	got404, _ := readLearnable400(&http.Response{StatusCode: 404, Body: io.NopCloser(strings.NewReader(big))})
	if len(got404) != failoverErrorClassifyCap {
		t.Errorf("404 read %d bytes, want the classifier cap %d", len(got404), failoverErrorClassifyCap)
	}
	got400, _ := readLearnable400(&http.Response{StatusCode: 400, Body: io.NopCloser(strings.NewReader(big))})
	if len(got400) != len(big) {
		t.Errorf("400 read %d bytes, want all %d", len(got400), len(big))
	}
}

// The hedged race learns the pro tier's 404 refusal the way the sequential
// path does, on OpenAI's own host only, and reads params from a 400 alone.
func TestLearnFromHedgedRefusal(t *testing.T) {
	refusal := []byte(`{"error":{"message":"This is not a chat model and thus not supported in the v1/chat/completions endpoint. Did you mean to use v1/completions?","type":"invalid_request_error","param":"model","code":null}}`)
	newState := func() *requestState { return &requestState{bodyBytes: []byte(plainChatBody)} }

	h := &Handler{}
	cand := responsesTestCandidate("https://api.openai.com/v1")
	cand.model.ModelID = "gpt-5.5-pro-2026-04-23"
	h.learnFromHedgedRefusal(newState(), cand, "openai", 404, refusal)
	if v, ok := h.responsesRequiredCache.Load("openai:gpt-5.5-pro-2026-04-23"); !ok || v != responsesAlways {
		t.Fatalf("hedged 404 on api.openai.com learned %v, want the always requirement", v)
	}

	relayHandler := &Handler{}
	relay := responsesTestCandidate("https://llm.corp.internal/v1")
	relay.model.ModelID = "gpt-5.5-pro-2026-04-23"
	relayHandler.learnFromHedgedRefusal(newState(), relay, "openai", 404, refusal)
	if _, ok := relayHandler.responsesRequiredCache.Load("openai:gpt-5.5-pro-2026-04-23"); ok {
		t.Fatal("a relay's 404 must not teach the Responses requirement")
	}

	paramHandler := &Handler{}
	paramKey := paramrewrite.LearnedCacheKey(cand.provider.ID.String(), cand.model.ModelID)
	paramHandler.learnFromHedgedRefusal(newState(), cand, "openai", 404, []byte(`{"error":{"message":"`+"`top_p`"+` is not supported"}}`))
	if _, ok := paramHandler.deprecationCache.Load(paramKey); ok {
		t.Fatal("a 404 must not teach a param strip")
	}
	paramHandler.learnFromHedgedRefusal(newState(), cand, "openai", 400, []byte(`{"error":{"message":"`+"`top_p`"+` is not supported"}}`))
	if _, ok := paramHandler.deprecationCache.Load(paramKey); !ok {
		t.Fatal("a 400 naming a param must still teach the strip")
	}
}
