package proxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
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

// The attempt loop hands a chat-completions 404 from an OpenAI provider to
// the learnable-refusal path, and nothing else that is not a 400.
func TestIsLearnableRefusal(t *testing.T) {
	chat := &requestState{bodyBytes: []byte(plainChatBody)}
	for _, tc := range []struct {
		status   int
		provider string
		st       *requestState
		want     bool
	}{
		{400, "anthropic", chat, true},
		{404, "openai", chat, true},
		{404, "custom", chat, false},
		{404, "openai", &requestState{endpointPath: "/embeddings"}, false},
		{500, "openai", chat, false},
	} {
		if got := isLearnableRefusal(tc.status, tc.provider, tc.st); got != tc.want {
			t.Errorf("isLearnableRefusal(%d, %q) = %v, want %v", tc.status, tc.provider, got, tc.want)
		}
	}
}
