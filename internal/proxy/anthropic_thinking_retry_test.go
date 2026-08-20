package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/anthropicegress"
	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/provider"
)

// testModelNamed is a bare model carrying only the upstream id, which is all the
// dialect cache keys on.
func testModelNamed(id string) *model.Model {
	return &model.Model{ID: uuid.New(), ModelID: id}
}

// budgetOnlyUpstream is a model on the older thinking dialect: it refuses an
// adaptive request exactly as claude-haiku-4-5 does, and answers a budget one.
// Every call is recorded so a test can assert both the refusal and the retry.
func budgetOnlyUpstream(t *testing.T, log *upstreamCallLog) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		log.record(anthropicUpstreamCall{path: r.URL.Path, body: body})

		thinking, _ := body["thinking"].(map[string]any)
		if thinking != nil && thinking["type"] == "adaptive" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"type":"error","error":{"type":"invalid_request_error","message":"adaptive thinking is not supported on this model"}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","model":"claude","content":[{"type":"text","text":"THOUGHT-9021"}],"stop_reason":"end_turn","usage":{"input_tokens":11,"output_tokens":6}}`))
	}))
}

// sendThinkingChat drives one chat request carrying reasoning_effort through the
// full ChatCompletions pipeline.
func sendThinkingChat(env *testProxyEnv, extra string) *httptest.ResponseRecorder {
	body := fmt.Sprintf(`{"model":"%s/%s","max_tokens":20000,"reasoning_effort":"high"%s,"messages":[{"role":"user","content":"think about it"}]}`,
		env.ProviderName, env.ModelName, extra)
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	ctx := context.WithValue(req.Context(), virtualKeyNameKey, "test-key")
	ctx = context.WithValue(ctx, virtualKeyIDKey, uuid.New().String())
	ctx = context.WithValue(ctx, VirtualKeyHashKey, env.KeyHash)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	env.Handler.ChatCompletions(w, req)
	return w
}

// The whole point of the self-heal: a model that refuses the adaptive shape gets
// asked again in the budget shape, the caller sees an answer rather than the
// 400, and the second request to that model skips the refusal entirely because
// the dialect was learned.
func TestChatCompletions_ThinkingDialectRetryAndLearn(t *testing.T) {
	var log upstreamCallLog
	upstream := budgetOnlyUpstream(t, &log)
	defer upstream.Close()

	env := newAnthropicMessagesEnv(t, upstream)

	w := sendThinkingChat(env, "")
	if w.Code != http.StatusOK {
		t.Fatalf("first request: %d\n%s", w.Code, w.Body.String())
	}

	calls := log.takeAll()
	if len(calls) != 2 {
		t.Fatalf("upstream calls = %d, want 2 (the refusal and the retry)", len(calls))
	}
	firstThinking, _ := calls[0].body["thinking"].(map[string]any)
	if firstThinking == nil || firstThinking["type"] != "adaptive" {
		t.Errorf("first attempt thinking = %v, want the adaptive default", calls[0].body["thinking"])
	}
	retryThinking, _ := calls[1].body["thinking"].(map[string]any)
	if retryThinking == nil || retryThinking["type"] != "enabled" {
		t.Fatalf("retry thinking = %v, want the budget shape", calls[1].body["thinking"])
	}
	if retryThinking["budget_tokens"] == nil {
		t.Errorf("retry carries no budget_tokens: %v", retryThinking)
	}
	if calls[1].path != "/v1/messages" {
		t.Errorf("retry went to %s, want /v1/messages", calls[1].path)
	}

	// The client sees the answer, translated back to chat shape: the 400 never
	// reaches it.
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response not JSON: %v\n%s", err, w.Body.String())
	}
	if resp["object"] != "chat.completion" {
		t.Fatalf("object = %v, want chat.completion", resp["object"])
	}
	choice := resp["choices"].([]any)[0].(map[string]any)
	if choice["message"].(map[string]any)["content"] != "THOUGHT-9021" {
		t.Errorf("content = %v", choice["message"])
	}

	// Second request: the dialect is known, so there is no refusal to recover
	// from and only one upstream call.
	w = sendThinkingChat(env, "")
	if w.Code != http.StatusOK {
		t.Fatalf("second request: %d\n%s", w.Code, w.Body.String())
	}
	calls = log.takeAll()
	if len(calls) != 1 {
		t.Fatalf("second request made %d upstream calls, want 1: the dialect was already learned", len(calls))
	}
	learned, _ := calls[0].body["thinking"].(map[string]any)
	if learned == nil || learned["type"] != "enabled" {
		t.Errorf("second request thinking = %v, want the learned budget shape", calls[0].body["thinking"])
	}
}

// The other half of the Messages self-heal: a model that has retired a param
// says so in a 400, and the request is re-issued without it. This is the common
// case, not an exotic one — claude-sonnet-5 and claude-opus-5 answer
// "`temperature` is deprecated for this model", and OpenAI clients send
// temperature by default.
func TestChatCompletions_MessagesRetryLearnsRejectedParam(t *testing.T) {
	var log upstreamCallLog
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		log.record(anthropicUpstreamCall{path: r.URL.Path, body: body})

		if _, sent := body["temperature"]; sent {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte("{\"type\":\"error\",\"error\":{\"type\":\"invalid_request_error\",\"message\":\"`temperature` is deprecated for this model.\"}}"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","model":"claude","content":[{"type":"text","text":"TEMP-4417"}],"stop_reason":"end_turn","usage":{"input_tokens":5,"output_tokens":4}}`))
	}))
	defer upstream.Close()

	env := newAnthropicMessagesEnv(t, upstream)

	send := func() *httptest.ResponseRecorder {
		body := fmt.Sprintf(`{"model":"%s/%s","temperature":0.7,"max_tokens":100,"messages":[{"role":"user","content":"hi"}]}`,
			env.ProviderName, env.ModelName)
		req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
		ctx := context.WithValue(req.Context(), virtualKeyNameKey, "test-key")
		ctx = context.WithValue(ctx, virtualKeyIDKey, uuid.New().String())
		ctx = context.WithValue(ctx, VirtualKeyHashKey, env.KeyHash)
		w := httptest.NewRecorder()
		env.Handler.ChatCompletions(w, req.WithContext(ctx))
		return w
	}

	w := send()
	if w.Code != http.StatusOK {
		t.Fatalf("first request: %d\n%s", w.Code, w.Body.String())
	}
	calls := log.takeAll()
	if len(calls) != 2 {
		t.Fatalf("upstream calls = %d, want 2 (the refusal and the retry)", len(calls))
	}
	if _, sent := calls[0].body["temperature"]; !sent {
		t.Error("first attempt did not carry the caller's temperature")
	}
	if _, sent := calls[1].body["temperature"]; sent {
		t.Errorf("retry still carries temperature: %v", calls[1].body)
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response not JSON: %v", err)
	}
	choice := resp["choices"].([]any)[0].(map[string]any)
	if choice["message"].(map[string]any)["content"] != "TEMP-4417" {
		t.Errorf("content = %v", choice["message"])
	}

	// Learned: the next request omits the param without needing the refusal.
	if w = send(); w.Code != http.StatusOK {
		t.Fatalf("second request: %d\n%s", w.Code, w.Body.String())
	}
	calls = log.takeAll()
	if len(calls) != 1 {
		t.Fatalf("second request made %d upstream calls, want 1: the strip was already learned", len(calls))
	}
	if _, sent := calls[0].body["temperature"]; sent {
		t.Errorf("second request still carries temperature: %v", calls[0].body)
	}
}

// A request that never asked for thinking cannot be fixed by asking in another
// dialect, so a dialect 400 on one must not be retried: the re-issued body would
// be byte-identical and earn the same 400.
func TestChatCompletions_NoThinkingRequestIsNotRetried(t *testing.T) {
	var log upstreamCallLog
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		log.record(anthropicUpstreamCall{path: r.URL.Path, body: body})
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"invalid_request_error","message":"adaptive thinking is not supported on this model"}}`))
	}))
	defer upstream.Close()

	env := newAnthropicMessagesEnv(t, upstream)

	body := fmt.Sprintf(`{"model":"%s/%s","max_tokens":100,"messages":[{"role":"user","content":"no thinking here"}]}`,
		env.ProviderName, env.ModelName)
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	ctx := context.WithValue(req.Context(), virtualKeyNameKey, "test-key")
	ctx = context.WithValue(ctx, virtualKeyIDKey, uuid.New().String())
	ctx = context.WithValue(ctx, VirtualKeyHashKey, env.KeyHash)
	w := httptest.NewRecorder()
	env.Handler.ChatCompletions(w, req.WithContext(ctx))

	if calls := log.takeAll(); len(calls) != 1 {
		t.Errorf("upstream calls = %d, want 1: a request with no thinking has nothing to retry", len(calls))
	}
}

// A 400 that is not about thinking must reach the client as the failure it is,
// with no second round-trip spent on it.
func TestChatCompletions_UnrelatedBadRequestIsNotRetried(t *testing.T) {
	var log upstreamCallLog
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		log.record(anthropicUpstreamCall{path: r.URL.Path, body: body})
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"invalid_request_error","message":"messages.0.user.content.str: Input should be a valid string"}}`))
	}))
	defer upstream.Close()

	env := newAnthropicMessagesEnv(t, upstream)
	w := sendThinkingChat(env, "")

	if w.Code == http.StatusOK {
		t.Errorf("code = 200, want the upstream failure surfaced")
	}
	if calls := log.takeAll(); len(calls) != 1 {
		t.Errorf("upstream calls = %d, want 1: a non-dialect 400 is not a retry", len(calls))
	}
}

// A hedged race cannot spend a second round-trip inside one slot, so a dialect
// 400 there is learned rather than retried: this race is lost, and the next
// request to that model asks in the right shape from the start.
func TestProbeStreamingCandidate_LearnsThinkingDialectWithoutRetrying(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandler(h)

	var log upstreamCallLog
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		log.record(anthropicUpstreamCall{path: r.URL.Path, body: body})
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"invalid_request_error","message":"adaptive thinking is not supported on this model"}}`))
	}))
	defer srv.Close()

	st, cand := probeStateForServer(srv.URL)
	st.bodyBytes = []byte(`{"model":"orig-model","stream":true,"reasoning_effort":"high","max_tokens":20000,"messages":[{"role":"user","content":"think"}]}`)
	cand.provider.ProviderType = "anthropic-messages"

	res := h.probeStreamingCandidate(context.Background(), st, cand, 0, 5*time.Second, 30*time.Second)
	if res.won {
		t.Fatal("a 400 must not win the race")
	}
	if calls := log.takeAll(); len(calls) != 1 {
		t.Errorf("upstream calls = %d, want 1: a hedged slot learns but does not retry", len(calls))
	}
	if got := h.thinkingDialectFor(cand); got != anthropicegress.ThinkingBudget {
		t.Errorf("dialect after the hedged 400 = %s, want budget learned", got)
	}
}

// The self-heal is per candidate, not per request. A failover group mixes
// providers, so the second anthropic-messages candidate must get its own
// self-heal even when the first already spent one: each is a different model
// behind a different endpoint, with its own facts to learn. Within a single
// attempt the flag cannot fire twice anyway — attemptCandidate consults it from
// one branch, not a loop — so carrying it forward would deny the next candidate
// a retry it never used.
func TestBuildCandidateRequest_ResetsTheMessagesSelfHealPerCandidate(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandler(h)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","content":[],"stop_reason":"end_turn"}`))
	}))
	defer srv.Close()

	st, cand := probeStateForServer(srv.URL)
	st.bodyBytes = []byte(`{"model":"orig-model","messages":[{"role":"user","content":"hi"}]}`)
	cand.provider.ProviderType = "anthropic-messages"

	// Stand in for a previous candidate that already used its one retry.
	st.messagesRetried = true
	st.lastMessagesBody = []byte(`{"stale":true}`)

	if _, _, _, err := h.buildCandidateRequest(context.Background(), st, cand); err != nil {
		t.Fatalf("buildCandidateRequest: %v", err)
	}
	if st.messagesRetried {
		t.Error("messagesRetried survived into the next candidate, which would deny it a self-heal it never used")
	}
	if string(st.lastMessagesBody) == `{"stale":true}` {
		t.Error("lastMessagesBody still holds the previous candidate's body")
	}
	if !st.anthropicEgressAttempt {
		t.Error("the candidate was not marked as an egress attempt")
	}
}

// learnAndRebuildMessages400 decides whether a 400 is worth acting on at all.
// Every "no" here is a 400 that must reach the client (or the next candidate)
// untouched, so each one is worth pinning: a false yes spends a round-trip
// re-sending bytes that will fail identically, and can poison a learned cache.
func TestLearnAndRebuildMessages400_DecidesWhatIsWorthRetrying(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandler(h)

	const thinkingBody = `{"model":"m","reasoning_effort":"high","messages":[{"role":"user","content":"hi"}]}`
	const plainBody = `{"model":"m","temperature":0.7,"messages":[{"role":"user","content":"hi"}]}`

	tests := []struct {
		name         string
		providerType string
		chatBody     string
		errBody      string
		wantOK       bool
	}{
		{
			name:         "dialect complaint on a thinking request is retried",
			providerType: "anthropic-messages",
			chatBody:     thinkingBody,
			errBody:      `{"type":"error","error":{"type":"invalid_request_error","message":"adaptive thinking is not supported on this model"}}`,
			wantOK:       true,
		},
		{
			name:         "rejected param is retried",
			providerType: "anthropic-messages",
			chatBody:     plainBody,
			errBody:      "{\"type\":\"error\",\"error\":{\"type\":\"invalid_request_error\",\"message\":\"`temperature` is deprecated for this model.\"}}",
			wantOK:       true,
		},
		{
			// The whole scoping rule: an `anthropic` provider's default route is
			// the compat endpoint, and the learned strip is keyed by
			// provider+model, so a name read out of a Messages 400 here would
			// remove a param from compat traffic that accepts it.
			name:         "a rejected param on an anthropic provider is NOT learned",
			providerType: "anthropic",
			chatBody:     plainBody,
			errBody:      "{\"type\":\"error\",\"error\":{\"type\":\"invalid_request_error\",\"message\":\"`temperature` is deprecated for this model.\"}}",
			wantOK:       false,
		},
		{
			name:         "a 400 naming nothing learnable is left alone",
			providerType: "anthropic-messages",
			chatBody:     plainBody,
			errBody:      `{"type":"error","error":{"type":"invalid_request_error","message":"messages.0.user.content.str: Input should be a valid string"}}`,
			wantOK:       false,
		},
		{
			name:         "an unreadable error body teaches nothing",
			providerType: "anthropic-messages",
			chatBody:     plainBody,
			errBody:      `<html>502</html>`,
			wantOK:       false,
		},
		{
			// A dialect complaint is only actionable on a request that asked for
			// thinking; anything else re-sends identical bytes.
			name:         "dialect complaint on a request without thinking is not retried",
			providerType: "anthropic-messages",
			chatBody:     plainBody,
			errBody:      `{"type":"error","error":{"type":"invalid_request_error","message":"adaptive thinking is not supported on this model"}}`,
			wantOK:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st, cand := probeStateForServer("http://unused.example")
			st.bodyBytes = []byte(tt.chatBody)
			st.lastMessagesBody = []byte(`{"previous":true}`)
			// A fresh provider per case, so one case's learned facts cannot leak
			// into the next through the shared handler caches.
			cand.provider.ID = uuid.New()

			rebuilt, _, _, ok := h.learnAndRebuildMessages400(st, cand, tt.providerType, []byte(tt.errBody))
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if ok && len(rebuilt) == 0 {
				t.Error("retry accepted but produced an empty body")
			}
			if !ok && rebuilt != nil {
				t.Errorf("no retry, but a body was returned: %s", rebuilt)
			}
		})
	}
}

// The guard is what stops a second round on the same candidate. Nothing inside
// one attempt loops, so this is belt and braces — but the flag is also what the
// per-candidate reset clears, so its effect is worth pinning.
func TestRetryLearnableMessages400_GuardsAndPreconditions(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandler(h)

	dialect400 := func() *http.Response {
		return &http.Response{
			StatusCode: http.StatusBadRequest,
			Body:       newBodyReader(`{"type":"error","error":{"type":"invalid_request_error","message":"adaptive thinking is not supported on this model"}}`),
		}
	}

	t.Run("an attempt that already retried is not retried again", func(t *testing.T) {
		st, cand := probeStateForServer("http://unused.example")
		st.bodyBytes = []byte(`{"model":"m","reasoning_effort":"high","messages":[{"role":"user","content":"hi"}]}`)
		st.messagesRetried = true
		resp := dialect400()
		defer func() { _ = resp.Body.Close() }()

		if _, handled := h.retryLearnableMessages400(httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody),
			st, cand, "anthropic-messages", resp, 0, new(float64), func() {}, ""); handled {
			t.Error("a second retry was issued on the same attempt")
		}
	})

	t.Run("a non-egress attempt is not this learner's business", func(t *testing.T) {
		st, cand := probeStateForServer("http://unused.example")
		st.anthropicEgressAttempt = false
		resp := dialect400()
		defer func() { _ = resp.Body.Close() }()

		if _, handled := h.retryLearnableMessages400(httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody),
			st, cand, "anthropic-messages", resp, 0, new(float64), func() {}, ""); handled {
			t.Error("a chat-completions attempt was handled by the Messages learner")
		}
	})
}

// A transport failure on the retry must surface as a failover-worthy error
// rather than a silent success or a leaked context.
func TestRetryLearnableMessages400_TransportFailureFailsOver(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandler(h)

	// A listener that accepts and immediately hangs up, so the retry's own
	// round-trip fails at the transport.
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			return
		}
		conn, _, err := hj.Hijack()
		if err == nil {
			_ = conn.Close()
		}
	}))
	defer dead.Close()

	st, cand := probeStateForServer(dead.URL)
	st.anthropicEgressAttempt = true
	st.bodyBytes = []byte(`{"model":"m","reasoning_effort":"high","messages":[{"role":"user","content":"hi"}]}`)
	resp := &http.Response{
		StatusCode: http.StatusBadRequest,
		Body:       newBodyReader(`{"type":"error","error":{"type":"invalid_request_error","message":"adaptive thinking is not supported on this model"}}`),
	}

	res, handled := h.retryLearnableMessages400(httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody),
		st, cand, "anthropic-messages", resp, 0, new(float64), func() {}, "")
	if !handled {
		t.Fatal("a dialect 400 was not handled")
	}
	if !res.cont {
		t.Error("a failed retry must ask the caller to fail over")
	}
	if res.lastReqErr.Kind == "" {
		t.Error("a failed retry must carry an error kind for the log row")
	}
	if res.retried {
		t.Error("a retry that never got a response must not be marked as retried")
	}
	// The dialect is still learned: this race was lost, but the next request to
	// this model should not repeat the refusal.
	if got := h.thinkingDialectFor(cand); got != anthropicegress.ThinkingBudget {
		t.Errorf("dialect = %s, want budget learned even though the retry failed", got)
	}
}

// thinkingDialectFor is what every attempt consults, so its default and its
// scoping are worth pinning directly.
func TestThinkingDialectFor(t *testing.T) {
	h := &Handler{}
	prov := &provider.Provider{ID: uuid.New(), Name: "p"}
	candidate := modelCandidate{provider: prov, model: testModelNamed("claude-x")}

	if got := h.thinkingDialectFor(candidate); got != anthropicegress.ThinkingAdaptive {
		t.Errorf("unlearned dialect = %s, want the adaptive default", got)
	}

	h.learnThinkingDialect(candidate, anthropicegress.ThinkingBudget)
	if got := h.thinkingDialectFor(candidate); got != anthropicegress.ThinkingBudget {
		t.Errorf("learned dialect = %s, want budget", got)
	}

	// A different model on the same provider is a separate fact.
	other := modelCandidate{provider: prov, model: testModelNamed("claude-y")}
	if got := h.thinkingDialectFor(other); got != anthropicegress.ThinkingAdaptive {
		t.Errorf("sibling model dialect = %s, want the default: the fact is per model", got)
	}

	// So is the same model id behind a different provider, which need not be the
	// same build at all.
	elsewhere := modelCandidate{provider: &provider.Provider{ID: uuid.New(), Name: "q"}, model: testModelNamed("claude-x")}
	if got := h.thinkingDialectFor(elsewhere); got != anthropicegress.ThinkingAdaptive {
		t.Errorf("same model at another provider = %s, want the default", got)
	}
}
