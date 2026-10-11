package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/model"
)

// sendCachedChat sends a chat request with a system prompt, two tools and three
// turns, the shape an agent loop produces, through the integration env.
func sendCachedChat(env *testProxyEnv, extra string) *httptest.ResponseRecorder {
	body := fmt.Sprintf(`{"model":"%s/%s","max_tokens":200%s,
		"messages":[
			{"role":"system","content":"You audit code."},
			{"role":"user","content":"first"},
			{"role":"assistant","content":"ack"},
			{"role":"user","content":"second"}],
		"tools":[{"type":"function","function":{"name":"a","parameters":{"type":"object"}}},{"type":"function","function":{"name":"b","parameters":{"type":"object"}}}]}`,
		env.ProviderName, env.ModelName, extra)
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	ctx := context.WithValue(req.Context(), virtualKeyNameKey, "test-key")
	ctx = context.WithValue(ctx, virtualKeyIDKey, uuid.New().String())
	ctx = context.WithValue(ctx, VirtualKeyHashKey, env.KeyHash)
	w := httptest.NewRecorder()
	env.Handler.ChatCompletions(w, req.WithContext(ctx))
	return w
}

// bodyHasCacheControl reports whether a captured upstream body carries any
// cache_control marker at any depth.
func bodyHasCacheControl(body map[string]any) bool {
	raw, _ := json.Marshal(body)
	return strings.Contains(string(raw), `"cache_control"`)
}

// TestChatCompletions_AnthropicEgressCachesPrompt: a served Messages request
// carries the prompt-cache breakpoints (last tool, system block, last two
// turns) and the cache counts Anthropic answers with reach the client as
// prompt_tokens_details.cached_tokens, so a caller sees its cache hits.
func TestChatCompletions_AnthropicEgressCachesPrompt(t *testing.T) {
	var log upstreamCallLog
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		log.record(anthropicUpstreamCall{path: r.URL.Path, body: body})
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","model":"claude","content":[{"type":"text","text":"CACHED-9"}],"stop_reason":"end_turn","usage":{"input_tokens":0,"cache_creation_input_tokens":12,"cache_read_input_tokens":3000,"output_tokens":4}}`))
	}))
	defer upstream.Close()
	env := newAnthropicMessagesEnv(t, upstream)
	// Priced, so the row proves the cache split AND the write premium reach the
	// bill: 3000 hits at $0.1 + 12 writes at $1 plus 25% + 4 output at $4,
	// all per million.
	if _, err := testDB.Pool().Exec(context.Background(),
		`UPDATE models SET input_price_per_million = 1, input_price_per_million_cache_hit = 0.1, output_price_per_million = 4 WHERE id = $1`, env.ModelID); err != nil {
		t.Fatal(err)
	}
	model.InvalidateModelCache()

	w := sendCachedChat(env, "")
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d\n%s", w.Code, w.Body.String())
	}
	call := log.takeOne(t, "cached chat")
	raw, _ := json.Marshal(call.body)
	if n := strings.Count(string(raw), `"cache_control"`); n != 4 {
		t.Fatalf("cache_control markers on the wire = %d, want 4\n%s", n, raw)
	}
	system, ok := call.body["system"].([]any)
	if !ok || len(system) != 1 {
		t.Errorf("system must be one block carrying the breakpoint, got %v", call.body["system"])
	}
	tools := call.body["tools"].([]any)
	if _, has := tools[1].(map[string]any)["cache_control"]; !has {
		t.Errorf("last tool carries no breakpoint: %v", tools)
	}

	var resp struct {
		Usage struct {
			PromptTokens        int `json:"prompt_tokens"`
			PromptTokensDetails struct {
				CachedTokens int `json:"cached_tokens"`
			} `json:"prompt_tokens_details"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response: %v", err)
	}
	if resp.Usage.PromptTokens != 3012 || resp.Usage.PromptTokensDetails.CachedTokens != 3000 {
		t.Errorf("usage = %+v, want prompt 3012 with 3000 cached", resp.Usage)
	}
	if state, _, _, _ := waitForRequestLog(t, env.ProviderID); state != "completed" {
		t.Fatalf("row state = %q, want completed", state)
	}
	var hit, miss int
	var cost float64
	if err := testDB.Pool().QueryRow(context.Background(),
		`SELECT tokens_prompt_cache_hit, tokens_prompt_cache_miss, cost_usd FROM request_logs WHERE provider_id = $1 ORDER BY created_at DESC LIMIT 1`,
		env.ProviderID).Scan(&hit, &miss, &cost); err != nil {
		t.Fatal(err)
	}
	if hit != 3000 || miss != 12 {
		t.Errorf("row cache split = (%d,%d), want (3000,12)", hit, miss)
	}
	// 300 + 12 + 3 (the write premium on the 12 written tokens) + 16, per million.
	if want := 331.0 / 1e6; math.Abs(cost-want) > 1e-9 { // REAL column
		t.Errorf("cost_usd = %v, want %v (the write premium on the 12 created tokens included)", cost, want)
	}
}

// TestChatCompletions_MessagesRetryLearnsCacheRefusal: a Messages endpoint that
// rejects the cache_control field (not Anthropic's, which never does) gets the
// request re-issued without breakpoints, the caller sees the answer, and the
// next request to that model carries none from the start. The refusal is
// learned for every Messages speaker, not only the Messages-only types.
func TestChatCompletions_MessagesRetryLearnsCacheRefusal(t *testing.T) {
	var log upstreamCallLog
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		log.record(anthropicUpstreamCall{path: r.URL.Path, body: body})
		if bodyHasCacheControl(body) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"type":"error","error":{"type":"invalid_request_error","message":"system.0.cache_control: Extra inputs are not permitted"}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","model":"claude","content":[{"type":"text","text":"PLAIN-5"}],"stop_reason":"end_turn","usage":{"input_tokens":5,"output_tokens":4}}`))
	}))
	defer upstream.Close()
	env := newAnthropicMessagesEnv(t, upstream)

	w := sendCachedChat(env, "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "PLAIN-5") {
		t.Fatalf("first request: %d\n%s", w.Code, w.Body.String())
	}
	calls := log.takeAll()
	if len(calls) != 2 {
		t.Fatalf("upstream calls = %d, want the refusal and the retry", len(calls))
	}
	if !bodyHasCacheControl(calls[0].body) || bodyHasCacheControl(calls[1].body) {
		t.Errorf("want breakpoints on the first attempt only, got %v / %v", bodyHasCacheControl(calls[0].body), bodyHasCacheControl(calls[1].body))
	}
	if _, isString := calls[1].body["system"].(string); !isString {
		t.Errorf("the rebuild must put system back to a plain string, got %v", calls[1].body["system"])
	}

	if w = sendCachedChat(env, ""); w.Code != http.StatusOK {
		t.Fatalf("second request: %d\n%s", w.Code, w.Body.String())
	}
	calls = log.takeAll()
	if len(calls) != 1 || bodyHasCacheControl(calls[0].body) {
		t.Fatalf("second request: want one call without breakpoints, got %d calls, cache_control=%v", len(calls), len(calls) > 0 && bodyHasCacheControl(calls[0].body))
	}
}

// TestChatCompletions_UnrelatedMessages400DoesNotDisableCaching: a 400 about
// something else must not switch caching off for the model, or every later
// request would silently pay full price.
func TestChatCompletions_UnrelatedMessages400DoesNotDisableCaching(t *testing.T) {
	var log upstreamCallLog
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		log.record(anthropicUpstreamCall{path: r.URL.Path, body: body})
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"invalid_request_error","message":"messages.0.content.0: Input should be a valid string"}}`))
	}))
	defer upstream.Close()
	env := newAnthropicMessagesEnv(t, upstream)

	sendCachedChat(env, "")
	log.takeAll()
	sendCachedChat(env, "")
	calls := log.takeAll()
	if len(calls) != 1 || !bodyHasCacheControl(calls[0].body) {
		t.Fatalf("the second request must still carry breakpoints, got %d calls, cache_control=%v", len(calls), len(calls) > 0 && bodyHasCacheControl(calls[0].body))
	}
}

// TestProbeStreamingCandidate_LearnsCacheRefusal: a hedged slot cannot retry,
// but it learns the refusal so the next request to that model is built without
// breakpoints.
func TestProbeStreamingCandidate_LearnsCacheRefusal(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandler(h)

	var log upstreamCallLog
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		log.record(anthropicUpstreamCall{path: r.URL.Path, body: body})
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"invalid_request_error","message":"messages.0.content.0.cache_control: Extra inputs are not permitted"}}`))
	}))
	defer srv.Close()

	st, cand := probeStateForServer(srv.URL)
	st.bodyBytes = []byte(`{"model":"orig-model","stream":true,"max_tokens":200,"messages":[{"role":"user","content":"hi"}]}`)
	cand.provider.ProviderType = "anthropic-messages"
	if !h.cachingFor(cand) {
		t.Fatal("caching must be on before anything was learned")
	}

	if res := h.probeStreamingCandidate(context.Background(), st, cand, 0, 5*time.Second, 30*time.Second); res.won {
		t.Fatal("a 400 must not win the race")
	}
	calls := log.takeAll()
	if len(calls) != 1 || !bodyHasCacheControl(calls[0].body) {
		t.Fatalf("want one hedged call carrying breakpoints, got %+v", calls)
	}
	if h.cachingFor(cand) {
		t.Error("the hedged refusal was not learned")
	}
	rebuilt, _, _, err := h.anthropicEgressBody(st, cand, "anthropic-messages", h.thinkingDialectFor(cand))
	if err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if strings.Contains(string(rebuilt), "cache_control") {
		t.Errorf("the next build still carries breakpoints: %s", rebuilt)
	}
}

// TestLearnAndRebuildMessages400_CacheRefusalExits pins the refusal arm's two
// "do not retry" exits: a refusal arriving after the model was already learned
// (the rebuild carries no breakpoints either, so re-sending would earn the same
// 400) and a body the rebuild cannot translate at all. Both still record the
// refusal, so the next request is built without breakpoints.
func TestLearnAndRebuildMessages400_CacheRefusalExits(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandler(h)
	refusal := []byte(`{"type":"error","error":{"type":"invalid_request_error","message":"system.0.cache_control: Extra inputs are not permitted"}}`)

	st, cand := probeStateForServer("http://127.0.0.1:9")
	cand.provider.ProviderType = "anthropic-messages"
	st.bodyBytes = []byte(`{"model":"m","messages":[{"role":"user","content":"hi"}]}`)

	// Already learned: the refused body carried no breakpoints, so the rebuild is
	// byte-identical and must not be re-sent.
	h.learnCacheRefusal(cand)
	plain, _, _, err := h.anthropicEgressBody(st, cand, "anthropic-messages", h.thinkingDialectFor(cand))
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	st.lastMessagesBody = plain
	if _, _, _, ok := h.learnAndRebuildMessages400(st, cand, "anthropic-messages", refusal); ok {
		t.Error("a refusal of a body without breakpoints must not be retried")
	}

	// Untranslatable body: the refusal is learned, but nothing can be rebuilt.
	fresh := probeStateForServer
	st2, cand2 := fresh("http://127.0.0.1:9")
	cand2.provider.ProviderType = "anthropic-messages"
	cand2.model.ModelID = "claude-other"
	st2.bodyBytes = []byte(`{not json`)
	if _, _, _, ok := h.learnAndRebuildMessages400(st2, cand2, "anthropic-messages", refusal); ok {
		t.Error("an untranslatable body must not be retried")
	}
	if h.cachingFor(cand2) {
		t.Error("the refusal must be learned even when the rebuild fails")
	}
}
