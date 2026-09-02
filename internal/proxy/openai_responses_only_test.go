package proxy

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
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

	// A hedged /v1/responses attempt's 400 is not chat-completions learning
	// (learnFromHedgedRefusal leaves it alone), but OpenAI names a rejected
	// sampling parameter there by the same quoted name, so the Responses
	// share of the hedged learning teaches the strip.
	dialectHandler := &Handler{}
	dialect := &requestState{bodyBytes: []byte(plainChatBody), responsesAttempt: true}
	dialectHandler.learnFromHedgedRefusal(dialect, cand, "openai", 400, []byte(`{"error":{"message":"Unsupported parameter: 'temperature' is not supported with this model."}}`))
	if _, ok := dialectHandler.deprecationCache.Load(paramKey); ok {
		t.Fatal("the chat-completions learner must not read a Responses-dialect 400")
	}
	dialectHandler.learnFromHedgedResponsesRefusal(dialect, cand, 400, []byte(`{"error":{"message":"Unsupported parameter: 'temperature' is not supported with this model."}}`))
	if _, ok := dialectHandler.deprecationCache.Load(paramKey); !ok {
		t.Fatal("a Responses-dialect 400 naming temperature must teach the strip")
	}
	dialectHandler.learnFromHedgedResponsesRefusal(&requestState{bodyBytes: []byte(plainChatBody), responsesAttempt: true}, cand, 404, []byte(`{"error":{"message":"'top_p' is not supported"}}`))
	if cached, _ := dialectHandler.deprecationCache.Load(paramKey); cached != nil && (*cached.(*map[string]bool))["top_p"] {
		t.Fatal("a Responses-dialect 404 must not teach a strip")
	}
	// Only the Responses dialect: a gemini or Messages attempt's 400 names
	// that dialect's fields and must teach the compat path nothing.
	for _, other := range []*requestState{
		{bodyBytes: []byte(plainChatBody), geminiAttempt: true},
		{bodyBytes: []byte(plainChatBody), anthropicEgressAttempt: true},
	} {
		otherHandler := &Handler{}
		otherHandler.learnFromHedgedResponsesRefusal(other, cand, 400, []byte(`{"error":{"message":"'top_p' is not supported"}}`))
		if _, ok := otherHandler.deprecationCache.Load(paramKey); ok {
			t.Fatal("a non-Responses dialect 400 taught a strip through the Responses reader")
		}
	}
	// "reasoning" is the one shared name that means something else on the
	// Responses body; it is never learned from that dialect.
	reasoningHandler := &Handler{}
	reasoningHandler.learnFromHedgedResponsesRefusal(dialect, cand, 400, []byte(`{"error":{"message":"Unsupported parameter: 'reasoning' is not supported with this model."}}`))
	if _, ok := reasoningHandler.deprecationCache.Load(paramKey); ok {
		t.Fatal("a Responses-dialect 400 naming reasoning taught a strip")
	}
}

// A hedged probe whose Responses attempt is refused for a sampling parameter
// learns the strip through the race itself, not only through the helper.
func TestProbeStreamingCandidate_LearnsResponsesParamRefusal(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandler(h)
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":{"message":"Unsupported parameter: 'temperature' is not supported with this model.","type":"invalid_request_error","param":"temperature","code":"unsupported_parameter"}}`)
	}))
	defer srv.Close()
	st, cand := probeStateForServer(srv.URL)
	st.bodyBytes = []byte(`{"model":"orig-model","temperature":0.2,"messages":[{"role":"user","content":"hi"}],"stream":true}`)
	h.responsesRequiredCache.Store(responsesCacheKey("openai", cand.model.ModelID), responsesAlways)
	res := h.probeStreamingCandidate(context.Background(), st, cand, 0, 5*time.Second, 30*time.Second)
	if res.won {
		t.Fatal("a 400 must not win the race")
	}
	if !strings.HasSuffix(gotPath, "/responses") {
		t.Fatalf("probe went to %q, want /v1/responses", gotPath)
	}
	key := paramrewrite.LearnedCacheKey(cand.provider.ID.String(), cand.model.ModelID)
	if cached, ok := h.deprecationCache.Load(key); !ok || !(*cached.(*map[string]bool))["temperature"] {
		t.Fatal("the hedged Responses 400 did not teach the temperature strip")
	}
}

// A Responses attempt's 400 naming "reasoning" teaches nothing and issues no
// retry: the Responses body regenerates that field on every request, and a
// strip learned from it would delete the caller's object on the compat path.
func TestRetryLearnable400_ResponsesReasoningRefusalTeachesNothing(t *testing.T) {
	var posts int
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		posts++
		http.Error(w, "unexpected", http.StatusInternalServerError)
	}))
	defer upstream.Close()
	h := &Handler{upstreamTransport: &http.Transport{}}
	st := &requestState{bodyBytes: []byte(`{"model":"gpt-5.5-pro-2026-04-23","reasoning_effort":"high","reasoning":{"effort":"high"},"messages":[{"role":"user","content":"hi"}]}`), failoverTimeout: 5 * time.Second, responsesAttempt: true}
	cand := responsesTestCandidate(upstream.URL + "/v1")
	cand.model.ModelID = "gpt-5.5-pro-2026-04-23"
	refusal := &http.Response{StatusCode: 400, Body: io.NopCloser(strings.NewReader(`{"error":{"message":"Unsupported parameter: 'reasoning' is not supported with this model.","type":"invalid_request_error","param":"reasoning","code":"unsupported_parameter"}}`))}
	r := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
	var dialMs float64
	res, handled := h.retryLearnable400(r, st, cand, "openai", upstream.URL+"/v1/responses", refusal, 0, &dialMs, func() {}, "")
	if !handled || res.retried || posts != 0 {
		t.Fatalf("handled=%v retried=%v posts=%d, want handled with no retry", handled, res.retried, posts)
	}
	if _, ok := h.deprecationCache.Load(paramrewrite.LearnedCacheKey(cand.provider.ID.String(), cand.model.ModelID)); ok {
		t.Fatal("a Responses 400 naming reasoning taught a strip on the sequential path")
	}
}

// The Responses retry body cannot be built from a chat body the translator
// rejects; that is this gateway's own failure, reported as such.
func TestIssueParamRetry_ResponsesRebuildFailureIsInternal(t *testing.T) {
	h := &Handler{upstreamTransport: &http.Transport{}}
	st := &requestState{bodyBytes: []byte(`{"model":"gpt-5.5-pro","messages":"not-an-array"}`), failoverTimeout: time.Second, responsesAttempt: true}
	cand := responsesTestCandidate("https://api.openai.com/v1")
	r := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
	var dialMs float64
	resp, rc, reqErr := h.issueParamRetry(r, st, cand, "openai", "https://api.openai.com/v1/responses", map[string]bool{"temperature": true}, 0, &dialMs)
	if resp != nil || rc != nil || reqErr == nil || reqErr.Kind != KindInternal {
		t.Fatalf("resp=%v rc=%v err=%+v, want an internal error and nothing issued", resp, rc, reqErr)
	}
}

// A /v1/responses attempt whose 400 names a sampling parameter learns the
// strip and re-issues the request in the Responses dialect without it, the
// way a chat-completions attempt would; the pro tier has no other route.
func TestRetryLearnable400_ResponsesAttemptStripsParam(t *testing.T) {
	var bodies []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		bodies = append(bodies, r.URL.Path+" "+string(raw))
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(string(raw), `"temperature"`) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":{"message":"Unsupported parameter: 'temperature' is not supported with this model.","type":"invalid_request_error","param":"temperature","code":"unsupported_parameter"}}`)
			return
		}
		_, _ = io.WriteString(w, `{"id":"resp_2","object":"response","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Paris"}]}],"usage":{"input_tokens":1,"output_tokens":1}}`)
	}))
	defer upstream.Close()
	h := &Handler{upstreamTransport: &http.Transport{}}
	body := `{"model":"gpt-5.5-pro-2026-04-23","temperature":0.2,"messages":[{"role":"user","content":"capital of France?"}]}`
	st := &requestState{bodyBytes: []byte(body), failoverTimeout: 5 * time.Second, responsesAttempt: true}
	cand := responsesTestCandidate(upstream.URL + "/v1")
	cand.model.ModelID = "gpt-5.5-pro-2026-04-23"
	targetURL := upstream.URL + "/v1/responses"
	first := &http.Response{StatusCode: 400, Body: io.NopCloser(strings.NewReader(`{"error":{"message":"Unsupported parameter: 'temperature' is not supported with this model.","type":"invalid_request_error","param":"temperature","code":"unsupported_parameter"}}`))}
	r := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
	var dialMs float64
	res, handled := h.retryLearnable400(r, st, cand, "openai", targetURL, first, 0, &dialMs, func() {}, "")
	if !handled || !res.retried {
		t.Fatalf("handled=%v retried=%v, want the request re-issued", handled, res.retried)
	}
	if len(bodies) != 1 || !strings.HasPrefix(bodies[0], "/v1/responses ") {
		t.Fatalf("retry bodies = %v, want one POST to /v1/responses", bodies)
	}
	if strings.Contains(bodies[0], `"temperature"`) || !strings.Contains(bodies[0], `"input"`) || strings.Contains(bodies[0], `"messages"`) {
		t.Fatalf("retry was not the Responses dialect without temperature: %s", bodies[0])
	}
	if res.resp == nil || res.resp.StatusCode != 200 {
		t.Fatalf("retry result = %+v, want the 200", res.resp)
	}
	key := paramrewrite.LearnedCacheKey(cand.provider.ID.String(), cand.model.ModelID)
	if cached, ok := h.deprecationCache.Load(key); !ok || !(*cached.(*map[string]bool))["temperature"] {
		t.Fatalf("temperature was not learned under %s", key)
	}
	_ = res.resp.Body.Close()
	if res.retryCancel != nil {
		res.retryCancel()
	}
}

// The Responses-only 404 is learned and the request re-issued on
// /v1/responses; when that rebuilt request is refused in turn for a
// parameter the pro tier does not take, the param self-heal runs on the
// reroute's own 400 rather than leaving it to the client. A first request
// carrying temperature therefore reaches the model in one attempt:
// chat 404, Responses 400, Responses 200.
func TestRetryLearnable400_RerouteRefusalStripsParam(t *testing.T) {
	upstream, recorded := rerouteFixture(t, "temperature")
	h := &Handler{upstreamTransport: &http.Transport{}}
	body := `{"model":"gpt-5.5-pro-2026-04-23","temperature":0.2,"messages":[{"role":"user","content":"capital of France?"}]}`
	st := &requestState{bodyBytes: []byte(body), failoverTimeout: 5 * time.Second}
	cand := responsesTestCandidate(upstream.URL + "/v1")
	cand.model.ModelID = "gpt-5.5-pro-2026-04-23"
	r := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
	var dialMs float64
	res, handled := h.retryLearnable400(r, st, cand, "openai", upstream.URL+"/v1/chat/completions", rerouteRefusal(), 0, &dialMs, func() {}, "")
	bodies := *recorded
	if !handled || !res.retried || res.cont {
		t.Fatalf("handled=%v retried=%v cont=%v err=%+v, want the request re-issued", handled, res.retried, res.cont, res.lastReqErr)
	}
	t.Cleanup(func() {
		_ = res.resp.Body.Close()
		if res.retryCancel != nil {
			res.retryCancel()
		}
	})
	if len(bodies) != 2 {
		t.Fatalf("upstream saw %d requests, want the reroute and its param retry: %v", len(bodies), bodies)
	}
	if !strings.HasPrefix(bodies[0], "/v1/responses ") || !strings.Contains(bodies[0], `"temperature"`) {
		t.Fatalf("first re-issue = %s, want the Responses dialect still carrying temperature", bodies[0])
	}
	if !strings.HasPrefix(bodies[1], "/v1/responses ") || strings.Contains(bodies[1], `"temperature"`) || !strings.Contains(bodies[1], `"input"`) {
		t.Fatalf("param retry = %s, want the Responses dialect without temperature", bodies[1])
	}
	if res.resp.StatusCode != http.StatusOK {
		t.Fatalf("result status = %d, want the 200 the param retry earned", res.resp.StatusCode)
	}
	if !st.responsesAttempt {
		t.Fatal("responsesAttempt not set: the dispatch would not translate the answer back")
	}
	key := paramrewrite.LearnedCacheKey(cand.provider.ID.String(), cand.model.ModelID)
	if cached, ok := h.deprecationCache.Load(key); !ok || !(*cached.(*map[string]bool))["temperature"] {
		t.Fatalf("temperature was not learned under %s", key)
	}
	if v, ok := h.responsesRequiredCache.Load("openai:gpt-5.5-pro-2026-04-23"); !ok || v != responsesAlways {
		t.Fatalf("learned %v, want the always requirement", v)
	}
}

// A rerouted request whose Responses 400 names nothing the learner reads is
// handed back as it arrived, the reroute's 400 and not the original 404, so
// the client sees the refusal the model actually gave.
func TestRetryLearnable400_RerouteRefusalUnlearnableIsForwarded(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":{"message":"Your input exceeds the context window of this model.","type":"invalid_request_error"}}`)
	}))
	defer upstream.Close()
	h := &Handler{upstreamTransport: &http.Transport{}}
	st := &requestState{bodyBytes: []byte(plainChatBody), failoverTimeout: 5 * time.Second}
	cand := responsesTestCandidate(upstream.URL + "/v1")
	cand.model.ModelID = "gpt-5.5-pro-2026-04-23"
	r := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
	var dialMs float64
	res, handled := h.retryLearnable400(r, st, cand, "openai", upstream.URL+"/v1/chat/completions", rerouteRefusal(), 0, &dialMs, func() {}, "")
	if !handled || res.cont || !res.retried {
		t.Fatalf("handled=%v cont=%v retried=%v, want the reroute's answer handed back as a retry's", handled, res.cont, res.retried)
	}
	if res.resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want the reroute's 400", res.resp.StatusCode)
	}
	got, _ := io.ReadAll(res.resp.Body)
	_ = res.resp.Body.Close()
	if !strings.Contains(string(got), "context window") {
		t.Fatalf("body = %s, want the reroute's own refusal readable for the client", got)
	}
	if res.retryCancel != nil {
		res.retryCancel()
	}
}

// rerouteFixture is the upstream for the chained self-heal: chat 404 sends
// the caller to /v1/responses, where each request is refused for the first
// of the named params it still carries and answered 200 once it carries
// none. Requests are recorded in order.
func rerouteFixture(t *testing.T, refuse ...string) (*httptest.Server, *[]string) {
	t.Helper()
	var bodies []string
	var mu sync.Mutex
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, r.URL.Path+" "+string(raw))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		for _, p := range refuse {
			if strings.Contains(string(raw), `"`+p+`"`) {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = io.WriteString(w, `{"error":{"message":"Unsupported parameter: '`+p+`' is not supported with this model.","type":"invalid_request_error","param":"`+p+`","code":"unsupported_parameter"}}`)
				return
			}
		}
		_, _ = io.WriteString(w, `{"id":"resp_4","object":"response","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Paris"}]}],"usage":{"input_tokens":1,"output_tokens":1}}`)
	}))
	t.Cleanup(upstream.Close)
	return upstream, &bodies
}

func rerouteRefusal() *http.Response {
	return &http.Response{StatusCode: 404, Body: io.NopCloser(strings.NewReader(`{"error":{"message":"This is not a chat model and thus not supported in the v1/chat/completions endpoint. Did you mean to use v1/completions?","type":"invalid_request_error","param":"model","code":null}}`))}
}

// The chained self-heal keeps the strip loop's rounds: a reroute refused for
// one param, then refused again for a second, is answered on the third
// Responses request with neither.
func TestRetryLearnable400_RerouteRefusalStripsTwoRounds(t *testing.T) {
	upstream, bodies := rerouteFixture(t, "temperature", "top_p")
	h := &Handler{upstreamTransport: &http.Transport{}}
	body := `{"model":"gpt-5.5-pro-2026-04-23","temperature":0.2,"top_p":0.9,"messages":[{"role":"user","content":"capital of France?"}]}`
	st := &requestState{bodyBytes: []byte(body), failoverTimeout: 5 * time.Second}
	cand := responsesTestCandidate(upstream.URL + "/v1")
	cand.model.ModelID = "gpt-5.5-pro-2026-04-23"
	r := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
	var dialMs float64
	res, handled := h.retryLearnable400(r, st, cand, "openai", upstream.URL+"/v1/chat/completions", rerouteRefusal(), 0, &dialMs, func() {}, "")
	if !handled || !res.retried || res.cont || res.resp.StatusCode != http.StatusOK {
		t.Fatalf("handled=%v retried=%v cont=%v status=%d, want the 200 after two strips", handled, res.retried, res.cont, res.resp.StatusCode)
	}
	_ = res.resp.Body.Close()
	if res.retryCancel != nil {
		res.retryCancel()
	}
	if len(*bodies) != 3 {
		t.Fatalf("upstream saw %d requests, want reroute + two strip rounds: %v", len(*bodies), *bodies)
	}
	last := (*bodies)[2]
	if !strings.HasPrefix(last, "/v1/responses ") || strings.Contains(last, `"temperature"`) || strings.Contains(last, `"top_p"`) {
		t.Fatalf("final request = %s, want the Responses dialect without either param", last)
	}
	key := paramrewrite.LearnedCacheKey(cand.provider.ID.String(), cand.model.ModelID)
	cached, ok := h.deprecationCache.Load(key)
	if !ok || !(*cached.(*map[string]bool))["temperature"] || !(*cached.(*map[string]bool))["top_p"] {
		t.Fatalf("learned strips = %v, want both params", cached)
	}
}

// A transport failure on the chained strip retry asks the loop to move on,
// as the plain strip retry does; the reroute's context was released once
// its 400 was read, so nothing is left open.
func TestRetryLearnable400_RerouteRefusalStripTransportFailureContinues(t *testing.T) {
	var n int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&n, 1) == 1 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":{"message":"Unsupported parameter: 'temperature' is not supported with this model.","type":"invalid_request_error","param":"temperature","code":"unsupported_parameter"}}`)
			return
		}
		// The strip retry: drop the connection without a response.
		conn, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			_ = conn.Close()
		}
	}))
	defer upstream.Close()
	h := &Handler{upstreamTransport: &http.Transport{}}
	body := `{"model":"gpt-5.5-pro-2026-04-23","temperature":0.2,"messages":[{"role":"user","content":"capital of France?"}]}`
	st := &requestState{bodyBytes: []byte(body), failoverTimeout: 5 * time.Second}
	cand := responsesTestCandidate(upstream.URL + "/v1")
	cand.model.ModelID = "gpt-5.5-pro-2026-04-23"
	r := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
	var dialMs float64
	res, handled := h.retryLearnable400(r, st, cand, "openai", upstream.URL+"/v1/chat/completions", rerouteRefusal(), 0, &dialMs, func() {}, "")
	if !handled || !res.cont || res.lastReqErr.Kind != KindProviderError {
		t.Fatalf("handled=%v cont=%v kind=%v, want the loop asked to continue on a provider error", handled, res.cont, res.lastReqErr.Kind)
	}
	if res.retryCancel != nil {
		t.Fatal("a failed retry must not hand back a cancel func: there is no body to consume")
	}
	if atomic.LoadInt32(&n) != 2 {
		t.Fatalf("upstream saw %d requests, want the reroute and the strip retry", n)
	}
}

// A self-heal round is not issued past the request's overall deadline: the
// refusal is learned and handed on as the provider gave it (to fail over or
// reach the client), never turned into a timeout of the gateway's making.
// Inside the deadline the rounds run, each cut at it.
func TestRetryLearnable400_RoundsRespectTheOverallDeadline(t *testing.T) {
	upstream, bodies := rerouteFixture(t, "temperature")
	h := &Handler{upstreamTransport: &http.Transport{}}
	body := `{"model":"gpt-5.5-pro-2026-04-23","temperature":0.2,"messages":[{"role":"user","content":"capital of France?"}]}`
	cand := responsesTestCandidate(upstream.URL + "/v1")
	cand.model.ModelID = "gpt-5.5-pro-2026-04-23"
	r := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
	var dialMs float64

	// Expired before the round: the reroute is not issued, the 404 comes
	// back readable, and the requirement is learned for the next request.
	st := &requestState{bodyBytes: []byte(body), failoverTimeout: 5 * time.Second, overallDeadline: time.Now().Add(-time.Second)}
	res, handled := h.retryLearnable400(r, st, cand, "openai", upstream.URL+"/v1/chat/completions", rerouteRefusal(), 0, &dialMs, func() {}, "")
	if !handled || res.cont || res.retried || res.resp.StatusCode != http.StatusNotFound {
		t.Fatalf("handled=%v cont=%v retried=%v status=%d, want the refusal handed on as it came", handled, res.cont, res.retried, res.resp.StatusCode)
	}
	if got, _ := io.ReadAll(res.resp.Body); !strings.Contains(string(got), "not a chat model") {
		t.Fatalf("refusal body = %s, want it readable for the client", got)
	}
	if v, ok := h.responsesRequiredCache.Load("openai:gpt-5.5-pro-2026-04-23"); !ok || v != responsesAlways {
		t.Fatalf("learned %v, want the requirement learned even without a round", v)
	}
	if len(*bodies) != 0 {
		t.Fatalf("upstream saw %v past the overall deadline, want nothing", *bodies)
	}

	// Expired between a strip round and the next: the second 400 is learned
	// and handed on, not retried.
	stripped, recorded := rerouteFixture(t, "temperature", "top_p")
	cand2 := responsesTestCandidate(stripped.URL + "/v1")
	cand2.model.ModelID = "gpt-5.5-pro-2026-04-23"
	st = &requestState{bodyBytes: []byte(`{"model":"gpt-5.5-pro-2026-04-23","temperature":0.2,"top_p":0.9,"messages":[{"role":"user","content":"hi"}]}`), failoverTimeout: 5 * time.Second, responsesAttempt: true, overallDeadline: time.Now().Add(-time.Second)}
	first := &http.Response{StatusCode: 400, Body: io.NopCloser(strings.NewReader(`{"error":{"message":"Unsupported parameter: 'temperature' is not supported with this model.","type":"invalid_request_error","param":"temperature","code":"unsupported_parameter"}}`))}
	res, handled = h.retryLearnable400(r, st, cand2, "openai", stripped.URL+"/v1/responses", first, 0, &dialMs, func() {}, "")
	if !handled || res.cont || res.retried || res.resp.StatusCode != http.StatusBadRequest || len(*recorded) != 0 {
		t.Fatalf("handled=%v cont=%v retried=%v status=%d requests=%d, want the 400 handed on and no round", handled, res.cont, res.retried, res.resp.StatusCode, len(*recorded))
	}
	key := paramrewrite.LearnedCacheKey(cand2.provider.ID.String(), cand2.model.ModelID)
	if cached, ok := h.deprecationCache.Load(key); !ok || !(*cached.(*map[string]bool))["temperature"] {
		t.Fatalf("temperature was not learned without a round")
	}

	// Deadline ahead of the budget: the rounds run.
	st = &requestState{bodyBytes: []byte(body), failoverTimeout: 5 * time.Second, overallDeadline: time.Now().Add(time.Minute)}
	res, handled = h.retryLearnable400(r, st, cand, "openai", upstream.URL+"/v1/chat/completions", rerouteRefusal(), 0, &dialMs, func() {}, "")
	if !handled || !res.retried || res.cont || res.resp.StatusCode != http.StatusOK {
		t.Fatalf("handled=%v retried=%v cont=%v, want the healed 200 inside the deadline", handled, res.retried, res.cont)
	}
	_ = res.resp.Body.Close()
	if res.retryCancel != nil {
		res.retryCancel()
	}
	if len(*bodies) != 2 {
		t.Fatalf("upstream saw %d requests, want the reroute and its strip", len(*bodies))
	}
}
