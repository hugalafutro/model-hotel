package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/provider"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// newOpenCodeGoClaudeEnv turns the integration env into an OpenCode Go provider
// (type inferred from the /zen/go base path) serving claude-haiku-5-5, with
// upstream dials redirected to the mock server, as newAnthropicEgressEnv does.
func newOpenCodeGoClaudeEnv(t *testing.T, upstream *httptest.Server) *testProxyEnv {
	t.Helper()
	env := newTestProxyEnvWithUpstream(t, upstream)
	pool := testDB.Pool()
	if _, err := pool.Exec(context.Background(),
		`UPDATE providers SET base_url = 'http://opencode.ai/zen/go/v1' WHERE id = $1`, env.ProviderID); err != nil {
		t.Fatalf("failed to update provider base URL: %v", err)
	}
	if _, err := pool.Exec(context.Background(),
		`UPDATE models SET model_id = 'claude-haiku-5-5' WHERE id = $1`, env.ModelID); err != nil {
		t.Fatalf("failed to rename model: %v", err)
	}
	env.ModelName = "claude-haiku-5-5"
	provider.InvalidateProviderCache()
	model.InvalidateModelCache()
	target := upstream.Listener.Addr().String()
	env.Handler.upstreamTransport = &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, target)
		},
	}
	return env
}

// openCodeMessagesUpstream answers like OpenCode Go's /zen/go/v1/messages:
// Anthropic Messages shape, x-api-key auth. Any other path is the chat route,
// which refuses a Claude model with the real 400 body.
func openCodeMessagesUpstream(t *testing.T, log *upstreamCallLog, sessions *[]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		log.record(anthropicUpstreamCall{path: r.URL.Path, body: body})
		if r.URL.Path != "/zen/go/v1/messages" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"type":"error","error":{"type":"ModelProtocolUnsupported","message":"Model does not support this protocol."}}`))
			return
		}
		if r.Header.Get("x-api-key") != "test-api-key" {
			t.Errorf("x-api-key = %q, want the provider key", r.Header.Get("x-api-key"))
		}
		if r.Header.Get("anthropic-version") == "" {
			t.Error("anthropic-version header missing")
		}
		*sessions = append(*sessions, r.Header.Get(util.OpenCodeGoSessionHeader))
		if stream, _ := body["stream"].(bool); stream {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			for _, ev := range []string{
				`{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"claude-haiku-5-5","content":[],"usage":{"input_tokens":9,"output_tokens":1}}}`,
				`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
				`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"PLUM-7431"}}`,
				`{"type":"content_block_stop","index":0}`,
				`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":4}}`,
				`{"type":"message_stop"}`,
			} {
				fmt.Fprint(w, "data: "+ev+"\n\n")
			}
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","model":"claude-haiku-5-5","content":[{"type":"text","text":"PLUM-7431"}],"stop_reason":"end_turn","usage":{"input_tokens":9,"output_tokens":4}}`))
	}))
}

// TestChatCompletions_OpenCodeGoClaudeTakesMessages: a plain chat request to a
// Claude model on OpenCode Go never touches the chat route (which would refuse
// it with "Model does not support this protocol"). It goes to /zen/go/v1/messages
// translated, carrying x-api-key and the Go session header, and the Messages
// answer comes back as a chat completion, streaming included.
func TestChatCompletions_OpenCodeGoClaudeTakesMessages(t *testing.T) {
	var log upstreamCallLog
	var sessions []string
	upstream := openCodeMessagesUpstream(t, &log, &sessions)
	defer upstream.Close()
	env := newOpenCodeGoClaudeEnv(t, upstream)

	send := func(stream bool) *httptest.ResponseRecorder {
		// reasoning_effort must survive into thinking (the OpenCode chat rule
		// would strip it; this body never reaches that route) and top_p must
		// not go upstream (Anthropic rejects it), both the anthropic-messages
		// rules.
		body := fmt.Sprintf(`{"model":"%s/%s","stream":%v,"reasoning_effort":"high","top_p":0.9,"messages":[{"role":"user","content":"what is the code?"}]}`,
			env.ProviderName, env.ModelName, stream)
		req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
		ctx := context.WithValue(req.Context(), virtualKeyNameKey, "test-key")
		ctx = context.WithValue(ctx, virtualKeyIDKey, uuid.New().String())
		ctx = context.WithValue(ctx, VirtualKeyHashKey, env.KeyHash)
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()
		env.Handler.ChatCompletions(w, req)
		return w
	}

	w := send(false)
	if w.Code != http.StatusOK {
		t.Fatalf("non-streaming: %d\n%s", w.Code, w.Body.String())
	}
	call := log.takeOne(t, "non-streaming")
	if call.path != "/zen/go/v1/messages" {
		t.Fatalf("request went to %s, want /zen/go/v1/messages", call.path)
	}
	if call.body["model"] != "claude-haiku-5-5" {
		t.Errorf("upstream model = %v, want the resolved id", call.body["model"])
	}
	if _, hasMessages := call.body["messages"]; !hasMessages {
		t.Errorf("translated body has no messages: %v", call.body)
	}
	if _, thinking := call.body["thinking"]; !thinking {
		t.Errorf("reasoning_effort was not turned into thinking: %v", call.body)
	}
	if _, topP := call.body["top_p"]; topP {
		t.Errorf("top_p went upstream: %v", call.body)
	}
	if !strings.Contains(w.Body.String(), `"object":"chat.completion"`) || !strings.Contains(w.Body.String(), "PLUM-7431") {
		t.Errorf("answer not translated back to a chat completion: %s", w.Body.String())
	}

	w = send(true)
	if w.Code != http.StatusOK {
		t.Fatalf("streaming: %d\n%s", w.Code, w.Body.String())
	}
	call = log.takeOne(t, "streaming")
	if call.path != "/zen/go/v1/messages" || call.body["stream"] != true {
		t.Fatalf("streaming request: path %s stream %v, want /zen/go/v1/messages with stream true", call.path, call.body["stream"])
	}
	if !strings.Contains(w.Body.String(), "chat.completion.chunk") || !strings.Contains(w.Body.String(), "PLUM-7431") {
		t.Errorf("stream not translated back to chat chunks: %s", w.Body.String())
	}

	if len(sessions) != 2 || sessions[0] == "" || sessions[0] != sessions[1] {
		t.Errorf("Go session header: want the same non-empty value on both calls, got %q", sessions)
	}
}

// TestMessages_OpenCodeGoClaudeStaysNative: an Anthropic-in request to a Claude
// model on OpenCode Go is forwarded verbatim to /zen/go/v1/messages with the
// Messages auth headers, exactly as it is to Anthropic itself.
func TestMessages_OpenCodeGoClaudeStaysNative(t *testing.T) {
	var log upstreamCallLog
	var sessions []string
	upstream := openCodeMessagesUpstream(t, &log, &sessions)
	defer upstream.Close()
	env := newOpenCodeGoClaudeEnv(t, upstream)

	body := fmt.Sprintf(`{"model":"%s/%s","max_tokens":64,"messages":[{"role":"user","content":"what is the code?"}]}`, env.ProviderName, env.ModelName)
	w := doMessagesRequest(env, body)
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, body = %s", w.Code, w.Body.String())
	}
	call := log.takeOne(t, "anthropic-in")
	if call.path != "/zen/go/v1/messages" {
		t.Fatalf("anthropic-in request went to %s, want /zen/go/v1/messages", call.path)
	}
	if call.body["max_tokens"] != float64(64) || call.body["model"] != "claude-haiku-5-5" {
		t.Errorf("body not forwarded verbatim with the resolved model: %v", call.body)
	}
	if len(sessions) != 1 || sessions[0] == "" {
		t.Errorf("Go session header missing on the native route: %q", sessions)
	}
	if !strings.Contains(w.Body.String(), `"type":"message"`) {
		t.Errorf("native answer not passed through: %s", w.Body.String())
	}
}

// TestChatCompletions_OpenCodeGoClaudeLearnsRejectedParam: a Messages 400
// naming a parameter is learned for a Claude model on OpenCode exactly as for
// an anthropic-messages provider, since Messages is its only chat route too.
// The retry carries the Messages headers (x-api-key) like the first attempt,
// and the next request omits the parameter without a refusal.
func TestChatCompletions_OpenCodeGoClaudeLearnsRejectedParam(t *testing.T) {
	var log upstreamCallLog
	var keys []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		log.record(anthropicUpstreamCall{path: r.URL.Path, body: body})
		keys = append(keys, r.Header.Get("x-api-key"))
		if _, sent := body["temperature"]; sent {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte("{\"type\":\"error\",\"error\":{\"type\":\"invalid_request_error\",\"message\":\"`temperature` is deprecated for this model.\"}}"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","model":"claude-haiku-5-5","content":[{"type":"text","text":"TEMP-4417"}],"stop_reason":"end_turn","usage":{"input_tokens":5,"output_tokens":4}}`))
	}))
	defer upstream.Close()
	env := newOpenCodeGoClaudeEnv(t, upstream)

	send := func() *httptest.ResponseRecorder {
		body := fmt.Sprintf(`{"model":"%s/%s","temperature":0.7,"max_tokens":100,"messages":[{"role":"user","content":"hi"}]}`, env.ProviderName, env.ModelName)
		req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
		ctx := context.WithValue(req.Context(), virtualKeyNameKey, "test-key")
		ctx = context.WithValue(ctx, virtualKeyIDKey, uuid.New().String())
		ctx = context.WithValue(ctx, VirtualKeyHashKey, env.KeyHash)
		w := httptest.NewRecorder()
		env.Handler.ChatCompletions(w, req.WithContext(ctx))
		return w
	}

	if w := send(); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "TEMP-4417") {
		t.Fatalf("first request: %d\n%s", w.Code, w.Body.String())
	}
	calls := log.takeAll()
	if len(calls) != 2 || calls[0].path != "/zen/go/v1/messages" || calls[1].path != "/zen/go/v1/messages" {
		t.Fatalf("want the refusal and the retry on /zen/go/v1/messages, got %+v", calls)
	}
	if _, sent := calls[1].body["temperature"]; sent {
		t.Errorf("retry still carries temperature: %v", calls[1].body)
	}
	if len(keys) != 2 || keys[0] != "test-api-key" || keys[1] != "test-api-key" {
		t.Errorf("x-api-key on first attempt and retry: want the provider key on both, got %q", keys)
	}

	if w := send(); w.Code != http.StatusOK {
		t.Fatalf("second request: %d\n%s", w.Code, w.Body.String())
	}
	if calls = log.takeAll(); len(calls) != 1 {
		t.Fatalf("second request made %d upstream calls, want 1: the strip was learned", len(calls))
	}
	if _, sent := calls[0].body["temperature"]; sent {
		t.Errorf("second request still carries temperature: %v", calls[0].body)
	}
}

// TestProbeStreamingCandidate_OpenCodeClaudeLearnsRejectedParam: a hedged slot
// cannot re-issue, but a Messages 400 naming a parameter is learned there under
// the same gate as the sequential retry, for a Claude model on OpenCode as for
// an anthropic-messages provider. The next build for that model omits it.
func TestProbeStreamingCandidate_OpenCodeClaudeLearnsRejectedParam(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandler(h)

	var log upstreamCallLog
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		log.record(anthropicUpstreamCall{path: r.URL.Path, body: body})
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("{\"type\":\"error\",\"error\":{\"type\":\"invalid_request_error\",\"message\":\"`temperature` is deprecated for this model.\"}}"))
	}))
	defer srv.Close()

	st, cand := probeStateForServer(srv.URL)
	st.bodyBytes = []byte(`{"model":"orig-model","stream":true,"temperature":0.7,"max_tokens":200,"messages":[{"role":"user","content":"hi"}]}`)
	cand.provider.ProviderType = "opencode-go"
	cand.model.ModelID = "claude-haiku-5-5"

	res := h.probeStreamingCandidate(context.Background(), st, cand, 0, 5*time.Second, 30*time.Second)
	if res.won {
		t.Fatal("a 400 must not win the race")
	}
	calls := log.takeAll()
	if len(calls) != 1 || !strings.HasSuffix(calls[0].path, "/messages") {
		t.Fatalf("want one Messages call, got %+v", calls)
	}
	if _, sent := calls[0].body["temperature"]; !sent {
		t.Fatal("the probe did not carry the caller's temperature")
	}
	if !h.learnedStrip(learnedScopeFor(cand), cand.model.ModelID, "temperature") {
		t.Fatal("temperature was not learned from the hedged 400")
	}
	rebuilt, _, _, err := h.anthropicEgressBody(st, cand, "opencode-go", h.thinkingDialectFor(cand))
	if err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	var next map[string]any
	_ = json.Unmarshal(rebuilt, &next)
	if _, sent := next["temperature"]; sent {
		t.Errorf("the next build still carries temperature: %s", rebuilt)
	}
}

// A body the Messages translation refuses is skipped, not failed over: no
// provider was contacted, so the loop pays no backoff before the next
// candidate, and the refusal is the recorded verdict.
func TestAttemptCandidate_RefusedBodyIsSkipped(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandler(h)

	st, cand := probeStateForServer("http://unused.example")
	st.isStreaming = false
	st.logData.streaming = false
	st.bodyBytes = []byte(`{"model":"orig-model","messages":[{"role":"user","content":[{"type":"text","text":"summarise"},{"type":"file","file":{"file_id":"file-abc"}}]}]}`)
	cand.provider.ProviderType = "anthropic"
	cand.model.ModelID = "claude-haiku-5-5"

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", http.NoBody)
	if got := h.attemptCandidate(w, r, st, cand, 0, 2); got != outcomeSkipped {
		t.Fatalf("outcome = %v, want outcomeSkipped", got)
	}
	if st.attemptRefused {
		t.Error("attemptRefused was not cleared for the next attempt")
	}
	if st.lastReqErr.Kind != KindValidation || st.lastReqErr.Model != "claude-haiku-5-5" {
		t.Errorf("lastReqErr = %+v, want a validation error naming the candidate", st.lastReqErr)
	}
	if w.Code != http.StatusOK || w.Body.Len() != 0 {
		t.Errorf("a skipped attempt wrote to the client: %d %q", w.Code, w.Body.String())
	}
}

// retryAfterSlotFrees: a busy candidate whose freed slot refuses the body is
// dropped from the wait, the others still waiting are tried, and the verdict
// while they wait is one of their saturations, not the refusal. When the last
// one refuses, the wait ends with the refusal as the verdict. The standing skip
// belongs to the candidate admitted first, the ordering in which a refusal
// replaces it.
func TestRetryAfterSlotFrees_RefusingCandidateLeavesTheWait(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandler(h)

	newState := func() (*requestState, []modelCandidate) {
		st, a := probeStateForServer("http://unused.example")
		st.overallDeadline = time.Now().Add(5 * time.Second)
		_, b := probeStateForServer("http://unused.example")
		b.provider.Name = "prov-B"
		st.setReqErr(reqError{Kind: KindProviderSaturated, Attempt: 1, Provider: "prov-A", Model: a.model.ModelID, Detail: busySkipDetail})
		return st, []modelCandidate{a, b}
	}
	refuse := func(st *requestState, c modelCandidate, attempt int) candidateOutcome {
		st.setReqErr(reqError{Kind: KindValidation, Attempt: attempt, Provider: c.provider.Name, Model: c.model.ModelID, Underlying: "refused"})
		return outcomeSkipped
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", http.NoBody)

	// A refuses, B serves.
	st, busy := newState()
	var tried []string
	var verdictWhileWaiting reqError
	served := h.retryAfterSlotFrees(w, r, st, busy, 2, func(_ http.ResponseWriter, _ *http.Request, st *requestState, c modelCandidate, attempt, _ int) candidateOutcome {
		tried = append(tried, c.provider.Name)
		if c.provider.Name == "prov-A" {
			return refuse(st, c, attempt)
		}
		verdictWhileWaiting = st.lastReqErr
		return outcomeServed
	})
	if !served || len(tried) != 2 || tried[0] != "prov-A" || tried[1] != "prov-B" {
		t.Fatalf("served = %v, tried = %v, want A refused then B served", served, tried)
	}
	if verdictWhileWaiting.Kind != KindProviderSaturated || verdictWhileWaiting.Provider != "prov-B" {
		t.Errorf("verdict while B waited = %+v, want B's saturation, not A's refusal or A's skip", verdictWhileWaiting)
	}

	// Both refuse: the wait ends, the last refusal is the verdict.
	st, busy = newState()
	served = h.retryAfterSlotFrees(w, r, st, busy, 2, func(_ http.ResponseWriter, _ *http.Request, st *requestState, c modelCandidate, attempt, _ int) candidateOutcome {
		return refuse(st, c, attempt)
	})
	if served {
		t.Fatal("two refusals must not count as served")
	}
	if st.lastReqErr.Kind != KindValidation || st.lastReqErr.Provider != "prov-B" {
		t.Errorf("lastReqErr = %+v, want B's refusal once no candidate waits", st.lastReqErr)
	}

	// A third candidate with a route for the body failed provider-side and
	// holds the verdict: two refusals during the wait leave it standing.
	st, busy = newState()
	st.setReqErr(reqError{Kind: KindProviderError, Attempt: 2, Provider: "prov-C", Model: "gpt", Underlying: "HTTP 502"})
	served = h.retryAfterSlotFrees(w, r, st, busy, 3, func(_ http.ResponseWriter, _ *http.Request, st *requestState, c modelCandidate, attempt, _ int) candidateOutcome {
		return refuse(st, c, attempt)
	})
	if served {
		t.Fatal("two refusals must not count as served")
	}
	if st.lastReqErr.Kind != KindProviderError || st.lastReqErr.Provider != "prov-C" {
		t.Errorf("lastReqErr = %+v, want the third candidate's provider failure kept through both refusals", st.lastReqErr)
	}
}

// TestProbeStreamingCandidate_AnthropicLearnsParams: the hedged learner reads
// a rejected parameter for an "anthropic" provider like for every other
// Messages speaker. Messages is that type's only chat route, so the strip keyed
// by provider+model cannot reach traffic that accepts the parameter.
func TestProbeStreamingCandidate_AnthropicLearnsParams(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandler(h)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("{\"type\":\"error\",\"error\":{\"type\":\"invalid_request_error\",\"message\":\"`temperature` is deprecated for this model.\"}}"))
	}))
	defer srv.Close()

	st, cand := probeStateForServer(srv.URL)
	st.bodyBytes = []byte(`{"model":"orig-model","stream":true,"temperature":0.7,"max_tokens":200,"messages":[{"role":"user","content":"hi"}]}`)
	cand.provider.ProviderType = "anthropic"
	cand.model.ModelID = "claude-sonnet-5"

	if res := h.probeStreamingCandidate(context.Background(), st, cand, 0, 5*time.Second, 30*time.Second); res.won {
		t.Fatal("a 400 must not win the race")
	}
	if !st.anthropicEgressAttempt {
		t.Fatal("text to anthropic must take the Messages route")
	}
	if !h.learnedStrip(learnedScopeFor(cand), cand.model.ModelID, "temperature") {
		t.Error("temperature was not learned for an anthropic provider from the hedged 400")
	}
}
