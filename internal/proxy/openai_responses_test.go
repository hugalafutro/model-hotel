package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/provider"
)

func responsesTestCandidate(baseURL string) modelCandidate {
	return modelCandidate{
		model:    &model.Model{ID: uuid.New(), ModelID: "gpt-5.6-sol"},
		provider: &provider.Provider{ID: uuid.New(), Name: "OpenAI", BaseURL: baseURL},
		apiKey:   "sk-test",
	}
}

const toolsReasoningBody = `{"model":"hotel/gpt-5.6","stream":false,"reasoning_effort":"high",` +
	`"tools":[{"type":"function","function":{"name":"f","parameters":{"type":"object"}}}],` +
	`"messages":[{"role":"user","content":"hi"}]}`

// Preemptive routing triggers only for direct-OpenAI candidates whose model is
// in the learned cache AND whose request carries tools+reasoning.
func TestShouldUseResponsesAttempt(t *testing.T) {
	h := &Handler{}
	cand := responsesTestCandidate("https://api.openai.com/v1")
	st := &requestState{bodyBytes: []byte(toolsReasoningBody)}

	if h.shouldUseResponsesAttempt(st, cand, "openai") {
		t.Error("must not trigger before the requirement is learned")
	}
	h.responsesRequiredCache.Store("openai:gpt-5.6-sol", true)
	if !h.shouldUseResponsesAttempt(st, cand, "openai") {
		t.Error("cached model + tools + reasoning must trigger")
	}
	if h.shouldUseResponsesAttempt(st, cand, "openrouter") {
		t.Error("non-openai provider must not trigger")
	}
	if h.shouldUseResponsesAttempt(&requestState{bodyBytes: []byte(`{"model":"m","messages":[]}`)}, cand, "openai") {
		t.Error("tools-free request must keep chat-completions")
	}
	if h.shouldUseResponsesAttempt(&requestState{bodyBytes: []byte(toolsReasoningBody), endpointPath: "/embeddings"}, cand, "openai") {
		t.Error("multimodal passthrough endpoints must not trigger")
	}
}

// buildResponsesRequest targets /v1/responses with a translated body and
// standard bearer auth.
func TestBuildResponsesRequest(t *testing.T) {
	h := &Handler{}
	st := &requestState{bodyBytes: []byte(toolsReasoningBody), reqModel: "hotel/gpt-5.6"}
	cand := responsesTestCandidate("https://api.openai.com/v1")

	req, ptype, url, err := h.buildResponsesRequest(context.Background(), st, cand, "openai")
	if err != nil {
		t.Fatalf("buildResponsesRequest: %v", err)
	}
	if ptype != "openai" {
		t.Errorf("ptype = %q", ptype)
	}
	if !strings.HasSuffix(url, "/v1/responses") {
		t.Errorf("url = %q, want suffix /v1/responses", url)
	}
	if req.Header.Get("Authorization") != "Bearer sk-test" {
		t.Errorf("auth header = %q", req.Header.Get("Authorization"))
	}
	body, _ := io.ReadAll(req.Body)
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("body not JSON: %v", err)
	}
	if m["model"] != "gpt-5.6-sol" {
		t.Errorf("model = %v, want resolved id", m["model"])
	}
	if store, present := m["store"]; !present || store != false {
		t.Errorf("store = %v (present=%v), want explicit false", store, present)
	}
	if _, hasMessages := m["messages"]; hasMessages {
		t.Error("chat messages leaked untranslated")
	}
	if _, hasInput := m["input"]; !hasInput {
		t.Error("input items missing")
	}
}

// A 400 that is not the Responses rejection is left for the param-strip
// retry: handled=false and the body restored for re-reading.
func TestRetryWithResponses_NotResponsesError(t *testing.T) {
	h := &Handler{}
	st := &requestState{bodyBytes: []byte(toolsReasoningBody), failoverTimeout: time.Second}
	cand := responsesTestCandidate("https://api.openai.com/v1")
	errBody := `{"error":{"message":"Unsupported parameter: 'max_tokens'"}}`
	resp := &http.Response{StatusCode: 400, Body: io.NopCloser(strings.NewReader(errBody))}
	r := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)

	var dialMs float64
	res, handled := h.retryWithResponses(r, st, cand, "openai", resp, 0, &dialMs, func() {}, "")
	if handled {
		t.Fatal("param error must not be handled by the responses retry")
	}
	if st.responsesAttempt {
		t.Error("responsesAttempt must stay false")
	}
	restored, _ := io.ReadAll(res.resp.Body)
	if string(restored) != errBody {
		t.Errorf("400 body not restored: %q", restored)
	}
	if _, learned := h.responsesRequiredCache.Load("openai:gpt-5.6-sol"); learned {
		t.Error("nothing should be learned from a param error")
	}
}

// The real rejection: requirement learned into the cache, request re-issued
// against /v1/responses with a translated body, attempt flagged for response
// translation.
func TestRetryWithResponses_LearnsAndRetries(t *testing.T) {
	var gotPath string
	var gotBody []byte
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_1","status":"completed","output":[]}`))
	}))
	defer upstream.Close()

	h := &Handler{upstreamTransport: &http.Transport{}}
	st := &requestState{bodyBytes: []byte(toolsReasoningBody), reqModel: "hotel/gpt-5.6", failoverTimeout: 5 * time.Second}
	cand := responsesTestCandidate(upstream.URL + "/v1")
	rejection := `{"error":{"message":"Function tools with reasoning_effort are not supported in the Chat Completions API for this model. Please use the /v1/responses endpoint, or set reasoning_effort to 'none'."}}`
	resp := &http.Response{StatusCode: 400, Body: io.NopCloser(strings.NewReader(rejection))}
	r := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)

	var dialMs float64
	res, handled := h.retryWithResponses(r, st, cand, "openai", resp, 0, &dialMs, func() {}, "")
	if !handled || !res.retried {
		t.Fatalf("rejection not handled (handled=%v retried=%v cont=%v err=%v)", handled, res.retried, res.cont, res.lastReqErr)
	}
	t.Cleanup(func() {
		_ = res.resp.Body.Close()
		if res.retryCancel != nil {
			res.retryCancel()
		}
	})
	if !st.responsesAttempt {
		t.Error("responsesAttempt not set — dispatch would not translate the answer")
	}
	if _, learned := h.responsesRequiredCache.Load("openai:gpt-5.6-sol"); !learned {
		t.Error("requirement not learned into the cache")
	}
	if gotPath != "/v1/responses" {
		t.Errorf("retry path = %q, want /v1/responses", gotPath)
	}
	var m map[string]any
	if err := json.Unmarshal(gotBody, &m); err != nil {
		t.Fatalf("retry body not JSON: %v", err)
	}
	if m["model"] != "gpt-5.6-sol" {
		t.Errorf("retry body model = %v", m["model"])
	}
	if _, hasInput := m["input"]; !hasInput {
		t.Errorf("retry body untranslated: %s", gotBody)
	}
	if res.resp.StatusCode != http.StatusOK {
		t.Errorf("retry status = %d", res.resp.StatusCode)
	}
}

// A transport failure on the /v1/responses retry reports a provider error
// and asks the loop to continue to the next candidate (cont), like the
// param-strip retry does.
func TestRetryWithResponses_RetryTransportFailure(t *testing.T) {
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	deadURL := dead.URL
	dead.Close() // connection refused on retry

	h := &Handler{upstreamTransport: &http.Transport{}}
	st := &requestState{bodyBytes: []byte(toolsReasoningBody), reqModel: "hotel/gpt-5.6", failoverTimeout: time.Second}
	cand := responsesTestCandidate(deadURL + "/v1")
	rejection := `{"error":{"message":"Function tools with reasoning_effort are not supported. Please use the /v1/responses endpoint."}}`
	resp := &http.Response{StatusCode: 400, Body: io.NopCloser(strings.NewReader(rejection))}
	r := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)

	var dialMs float64
	res, handled := h.retryWithResponses(r, st, cand, "openai", resp, 0, &dialMs, func() {}, "")
	if !handled {
		t.Fatal("rejection must be handled even when the retry fails")
	}
	if !res.cont {
		t.Error("transport failure must continue to the next candidate")
	}
	if res.lastReqErr.Kind != KindProviderError {
		t.Errorf("error kind = %v, want KindProviderError", res.lastReqErr.Kind)
	}
	if _, learned := h.responsesRequiredCache.Load("openai:gpt-5.6-sol"); !learned {
		t.Error("requirement should be learned even when the retry fails")
	}
}

// A hedged probe cannot retry in-race, but a 400 with the Responses rejection
// must still LEARN the requirement so subsequent requests (hedged or
// sequential) route preemptively — otherwise an all-OpenAI hedged group would
// 400 on that model forever. A generic 400 must learn nothing.
func TestProbeStreamingCandidate_LearnsResponsesRequirement(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandler(h)

	run := func(t *testing.T, modelID, errBody string) (hedgeResult, modelCandidate) {
		t.Helper()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(errBody))
		}))
		defer srv.Close()
		st, cand := probeStateForServer(srv.URL)
		cand.model.ModelID = modelID // distinct per case: the learned flag is keyed by model
		st.bodyBytes = []byte(toolsReasoningBody)
		return h.probeStreamingCandidate(context.Background(), st, cand, 0, 5*time.Second, 30*time.Second), cand
	}

	res, cand := run(t, "gpt-hedge-reject", `{"error":{"message":"Function tools with reasoning_effort are not supported in the Chat Completions API for this model. Please use the /v1/responses endpoint, or set reasoning_effort to 'none'."}}`)
	if res.won || res.reqErr.Kind != KindProviderError {
		t.Errorf("400 probe must fail the candidate: won=%v kind=%v", res.won, res.reqErr.Kind)
	}
	if _, learned := h.responsesRequiredCache.Load("openai:" + cand.model.ModelID); !learned {
		t.Error("hedged probe did not learn the responses requirement")
	}

	res, cand = run(t, "gpt-hedge-generic", `{"error":{"message":"Invalid request"}}`)
	if res.won {
		t.Error("generic 400 must fail the candidate")
	}
	if _, learned := h.responsesRequiredCache.Load("openai:" + cand.model.ModelID); learned {
		t.Error("generic 400 must not be learned as a responses requirement")
	}
}

// End-to-end through the real ChatCompletions pipeline (plan §11 in
// miniature): a tools+reasoning request 400s on /chat/completions with the
// Responses rejection, the proxy learns + retries against /v1/responses and
// the client receives a translated chat.completion with tool_calls and
// reasoning_content. A second request routes to /v1/responses preemptively —
// no repeated 400 round-trip.
func TestChatCompletions_ResponsesRerouteLearnAndPreempt(t *testing.T) {
	var chatHits, responsesHits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/chat/completions"):
			chatHits.Add(1)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"message":"Function tools with reasoning_effort are not supported in the Chat Completions API for this model. Please use the /v1/responses endpoint, or set reasoning_effort to 'none'."}}`))
		case strings.HasSuffix(r.URL.Path, "/responses"):
			responsesHits.Add(1)
			var reqBody map[string]any
			_ = json.NewDecoder(r.Body).Decode(&reqBody)
			if _, hasInput := reqBody["input"]; !hasInput {
				t.Errorf("upstream /responses got untranslated body: %v", reqBody)
			}
			if stream, _ := reqBody["stream"].(bool); stream {
				w.Header().Set("Content-Type", "text/event-stream")
				w.WriteHeader(http.StatusOK)
				fmt.Fprint(w, "event: response.reasoning_summary_text.delta\n")
				fmt.Fprint(w, `data: {"type":"response.reasoning_summary_text.delta","delta":"pondering"}`+"\n\n")
				fmt.Fprint(w, `data: {"type":"response.output_item.added","output_index":1,"item":{"type":"function_call","call_id":"call_1","name":"get_weather"}}`+"\n\n")
				fmt.Fprint(w, `data: {"type":"response.function_call_arguments.delta","output_index":1,"delta":"{\"city\":\"oslo\"}"}`+"\n\n")
				fmt.Fprint(w, `data: {"type":"response.completed","response":{"status":"completed","usage":{"input_tokens":11,"output_tokens":7,"output_tokens_details":{"reasoning_tokens":4}}}}`+"\n\n")
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"resp_1","status":"completed","output":[
				{"type":"reasoning","summary":[{"type":"summary_text","text":"pondering"}]},
				{"type":"function_call","call_id":"call_1","name":"get_weather","arguments":"{\"city\":\"oslo\"}"}
			],"usage":{"input_tokens":11,"output_tokens":7,"output_tokens_details":{"reasoning_tokens":4}}}`))
		default:
			t.Errorf("unexpected upstream path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	env := newTestProxyEnvWithUpstream(t, upstream)
	defer upstream.Close()

	send := func(stream bool) *httptest.ResponseRecorder {
		body := fmt.Sprintf(`{"model":"%s/%s","stream":%v,"reasoning_effort":"high",
			"tools":[{"type":"function","function":{"name":"get_weather","parameters":{"type":"object"}}}],
			"messages":[{"role":"user","content":"weather in oslo?"}]}`, env.ProviderName, env.ModelName, stream)
		req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
		ctx := context.WithValue(req.Context(), virtualKeyNameKey, "test-key")
		ctx = context.WithValue(ctx, virtualKeyIDKey, uuid.New().String())
		ctx = context.WithValue(ctx, VirtualKeyHashKey, env.KeyHash)
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()
		env.Handler.ChatCompletions(w, req)
		return w
	}

	// First request: learn from the 400, retry via /v1/responses.
	w := send(false)
	if w.Code != http.StatusOK {
		t.Fatalf("first request: %d\n%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response not JSON: %v\n%s", err, w.Body.String())
	}
	choice := resp["choices"].([]any)[0].(map[string]any)
	msg := choice["message"].(map[string]any)
	if choice["finish_reason"] != "tool_calls" {
		t.Errorf("finish_reason = %v", choice["finish_reason"])
	}
	if msg["reasoning_content"] != "pondering" {
		t.Errorf("reasoning_content = %v", msg["reasoning_content"])
	}
	tcs, _ := msg["tool_calls"].([]any)
	if len(tcs) != 1 || tcs[0].(map[string]any)["id"] != "call_1" {
		t.Errorf("tool_calls = %v", msg["tool_calls"])
	}
	if got := chatHits.Load(); got != 1 {
		t.Errorf("chat-completions hits after first request = %d, want 1", got)
	}

	// Second request (streaming): preemptive — chat-completions never hit again.
	w = send(true)
	if w.Code != http.StatusOK {
		t.Fatalf("second request: %d\n%s", w.Code, w.Body.String())
	}
	sse := w.Body.String()
	if !strings.Contains(sse, `"reasoning_content":"pondering"`) {
		t.Errorf("streamed reasoning missing:\n%s", sse)
	}
	if !strings.Contains(sse, `"get_weather"`) || !strings.Contains(sse, `\"oslo\"`) {
		t.Errorf("streamed tool call missing:\n%s", sse)
	}
	if !strings.Contains(sse, `"finish_reason":"tool_calls"`) || !strings.Contains(sse, "data: [DONE]") {
		t.Errorf("terminal chunks missing:\n%s", sse)
	}
	if got := chatHits.Load(); got != 1 {
		t.Errorf("chat-completions hits after second request = %d, want 1 (preemptive routing)", got)
	}
	if got := responsesHits.Load(); got != 2 {
		t.Errorf("responses hits = %d, want 2", got)
	}
}

// translateResponsesResponseBody swaps a Responses 200 body for its chat
// translation in place, and errors on a non-Responses body so the caller can
// fail over instead of forwarding garbage.
func TestTranslateResponsesResponseBody(t *testing.T) {
	resp := &http.Response{Body: io.NopCloser(strings.NewReader(
		`{"id":"resp_1","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hi"}]}],"usage":{"input_tokens":1,"output_tokens":2}}`,
	))}
	if err := translateResponsesResponseBody(resp, "hotel/gpt-5.6"); err != nil {
		t.Fatalf("translate: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("translated body not JSON: %v", err)
	}
	if m["object"] != "chat.completion" {
		t.Errorf("object = %v", m["object"])
	}

	bad := &http.Response{Body: io.NopCloser(strings.NewReader(`{"not":"responses"}`))}
	if err := translateResponsesResponseBody(bad, "m"); err == nil {
		t.Error("non-Responses 200 body must error (failover)")
	}
}
