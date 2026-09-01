package proxy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/failover"
	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/provider"
)

// End-to-end behaviour of the 429 saturation-vs-exhaustion classification on
// the ChatCompletions path: the wait-and-retry for a saturated last candidate,
// the one-charge open for an exhausted window, the honest terminal responses,
// and the kill switches that restore today's behaviour.

const neuralwattSaturatedBody = `{"error":{"type":"rate_limit_error","code":"concurrent_budget_exceeded","message":"Concurrency budget exceeded"}}`
const ollamaExhaustedBody = `{"error":"you have reached your session usage limit, upgrade for higher limits"}`

// chatRequest posts one non-streaming completion for the env's single
// provider/model and returns the recorder.
func chatRequest(t *testing.T, env *testProxyEnv) *httptest.ResponseRecorder {
	t.Helper()
	body := `{"model": "` + env.ProviderName + `/` + env.ModelName + `", "messages": [{"role": "user", "content": "hello"}], "stream": false}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	ctx := context.WithValue(req.Context(), virtualKeyNameKey, "test-key")
	ctx = context.WithValue(ctx, VirtualKeyHashKey, env.KeyHash)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	env.Handler.ChatCompletions(w, req)
	return w
}

func chatCompletionJSON(model string) string {
	return `{"id":"c","object":"chat.completion","model":"` + model + `","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`
}

// A saturated 429 on the last (only) candidate earns one short wait and a
// retry of the same candidate, so the two-second slot gaps that became 502s on
// 2026-08-31 become served requests. The breaker is never charged for it.
func TestChatCompletions_SaturatedLastCandidate_WaitsAndRetriesOnce(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(neuralwattSaturatedBody))
			return
		}
		var reqBody map[string]any
		_ = json.NewDecoder(r.Body).Decode(&reqBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(chatCompletionJSON(reqBody["model"].(string))))
	}))
	defer upstream.Close()
	env := newTestProxyEnvWithUpstream(t, upstream)

	w := chatRequest(t, env)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 after the saturation retry; body: %s", w.Code, w.Body.String())
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("upstream calls = %d, want 2 (the saturated attempt and its one retry)", got)
	}
	if fails, seen := cbConsecutiveFails(env.Handler.circuitBreaker, env.ProviderID); seen && fails != 0 {
		t.Errorf("breaker charged %d for a saturated 429; saturation must be a no-op", fails)
	}
}

// A second saturated 429 is terminal — one retry, never a loop — and the
// client gets the honest answer: the 429 itself with a Retry-After, classified
// provider_saturated, with the breaker still untouched.
func TestChatCompletions_SaturatedTwice_Forwards429WithRetryAfter(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(neuralwattSaturatedBody))
	}))
	defer upstream.Close()
	env := newTestProxyEnvWithUpstream(t, upstream)

	w := chatRequest(t, env)

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429; body: %s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Retry-After"); got != "1" {
		t.Errorf("Retry-After = %q, want the provider's own %q", got, "1")
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("upstream calls = %d, want exactly 2 (one retry, not a loop)", got)
	}
	if state := env.Handler.circuitBreaker.GetState(env.ProviderID, env.ModelName); state != failover.StateClosed {
		t.Errorf("breaker state = %v, want closed: two saturated 429s are zero charges", state)
	}
}

// One exhausted 429 opens the model circuit outright and pins it from the
// response's own claim, so no second request is spent confirming a spent
// window.
func TestChatCompletions_Exhausted429_OpensCircuitOnOneResponse(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(ollamaExhaustedBody))
	}))
	defer upstream.Close()
	env := newTestProxyEnvWithUpstream(t, upstream)

	w := chatRequest(t, env)

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want the 429 forwarded; body: %s", w.Code, w.Body.String())
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("upstream calls = %d, want 1: an exhausted window earns no retry", got)
	}
	if state := env.Handler.circuitBreaker.GetState(env.ProviderID, env.ModelName); state != failover.StateOpen {
		t.Fatalf("breaker state = %v, want open after ONE exhausted 429", state)
	}
	var pinned bool
	var pinSource string
	for _, s := range env.Handler.circuitBreaker.Status() {
		if s.ProviderID == env.ProviderID.String() {
			pinned, pinSource = s.QuotaPinned, s.PinSource
		}
	}
	if !pinned || pinSource != "response" {
		t.Errorf("quota_pinned=%v pin_source=%q, want a response-derived pin holding the circuit", pinned, pinSource)
	}
}

// The master switch restores today's behaviour bit for bit: with
// rate_limit_classify_enabled off, a saturated-looking 429 charges the breaker
// exactly as an unclassified one always has.
func TestChatCompletions_ClassifyDisabled_429ChargesAsToday(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(neuralwattSaturatedBody))
	}))
	defer upstream.Close()
	env := newTestProxyEnvWithUpstream(t, upstream)
	ctx := context.Background()
	if err := env.Handler.settingsRepo.Set(ctx, "rate_limit_classify_enabled", "false"); err != nil {
		t.Fatalf("failed to disable classification: %v", err)
	}
	defer func() { _ = env.Handler.settingsRepo.Set(ctx, "rate_limit_classify_enabled", "true") }()

	w := chatRequest(t, env)

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429 forwarded as today", w.Code)
	}
	if fails, seen := cbConsecutiveFails(env.Handler.circuitBreaker, env.ProviderID); !seen || fails != 1 {
		t.Errorf("breaker fails = %d (seen=%v), want exactly the one charge a 429 always made", fails, seen)
	}
}

// The behavioural fallback: an unclassifiable 429 from a circuit that served a
// 2xx inside the window is saturated (no charge); outside the window it stays
// unknown and charges. This is the rule that keeps the mechanism working with
// the phrase table deleted.
func TestClassify429Attempt_RecentSuccessFallback(t *testing.T) {
	env := newTestProxyHandler(t)
	defer env.Upstream.Close()
	h := env.Handler
	ctx := context.Background()
	cand := candidateFor(t, env)
	h.circuitBreaker.RecordSuccess(cand.provider.ID, cand.provider.Name, cand.model.ModelID)

	unknown429 := func() *http.Response {
		return &http.Response{
			StatusCode: http.StatusTooManyRequests,
			Header:     make(http.Header),
			Body:       http.NoBody,
		}
	}
	st := &requestState{circuitBreakerEnabled: true, logData: &requestLogData{}}

	if v := h.classify429Attempt(ctx, st, cand, unknown429()); v.class != rateLimitSaturated {
		t.Errorf("class with a fresh success = %v, want saturated: it served a moment ago, a spent window does not come back in a minute", v.class)
	}

	// Shrink the window under the age of the success instead of waiting it out.
	if err := h.settingsRepo.Set(ctx, "rate_limit_recent_success_window", "1ms"); err != nil {
		t.Fatalf("failed to shrink window: %v", err)
	}
	defer func() { _ = h.settingsRepo.Set(ctx, "rate_limit_recent_success_window", "60s") }()
	time.Sleep(5 * time.Millisecond)
	if v := h.classify429Attempt(ctx, st, cand, unknown429()); v.class != rateLimitUnknown {
		t.Errorf("class with the success outside the window = %v, want unknown (charged as today)", v.class)
	}
}

// candidateFor resolves the env's single provider/model into a modelCandidate.
func candidateFor(t *testing.T, env *testProxyEnv) modelCandidate {
	t.Helper()
	cands, _, _, err := env.Handler.resolveSpecificProvider(context.Background(), env.ProviderName, env.ModelName)
	if err != nil || len(cands) != 1 {
		t.Fatalf("failed to resolve test candidate: %v (n=%d)", err, len(cands))
	}
	return cands[0]
}

// The exhaustion path's terminal response when the loop dies with a saturated
// last error: a 429 with Retry-After and "busy" wording, flipping back to the
// legacy 502 when failover_exhaustion_status_429 is off.
func TestFailAllExhausted_SaturatedLastError(t *testing.T) {
	env := newTestProxyHandler(t)
	defer env.Upstream.Close()
	h := env.Handler
	ctx := context.Background()

	newState := func() *requestState {
		req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader("{}"))
		logData, _ := h.newPendingRequestLog(req, endpointTypeChat, "hotel/g", false)
		st := &requestState{startTime: time.Now(), reqModel: "hotel/g", isFailover: true, logData: logData}
		st.rateLimit = rateLimitVerdict{class: rateLimitSaturated, retryAfter: 3 * time.Second}
		st.setReqErr(rateLimitReqErr(st.rateLimit, 1, "busy-provider"))
		return st
	}

	w := httptest.NewRecorder()
	h.failAllExhausted(w, newState(), 2)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429 for an all-busy exhaustion", w.Code)
	}
	if got := w.Header().Get("Retry-After"); got != "3" {
		t.Errorf("Retry-After = %q, want %q (the last provider's ask)", got, "3")
	}
	if body := w.Body.String(); !strings.Contains(body, "all providers busy for model hotel/g") {
		t.Errorf("body %q does not say the providers are busy rather than failed", body)
	}

	if err := h.settingsRepo.Set(ctx, "failover_exhaustion_status_429", "false"); err != nil {
		t.Fatalf("failed to disable: %v", err)
	}
	defer func() { _ = h.settingsRepo.Set(ctx, "failover_exhaustion_status_429", "true") }()
	w = httptest.NewRecorder()
	h.failAllExhausted(w, newState(), 2)
	if w.Code != http.StatusBadGateway {
		t.Errorf("status with the flag off = %d, want the legacy 502", w.Code)
	}
}

// The up-front variant: every candidate skipped by the breaker answers 429
// with the earliest retry instant, exhausted only when every skip is
// quota-pinned, and stays a 502 when nothing was breaker-skipped (a genuinely
// empty group) or the flag is off.
func TestFailNoAvailableProvider(t *testing.T) {
	env := newTestProxyHandler(t)
	defer env.Upstream.Close()
	h := env.Handler

	run := func(skips breakerSkipSummary) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader("{}"))
		logData, _ := h.newPendingRequestLog(req, endpointTypeChat, "hotel/g", false)
		st := &requestState{startTime: time.Now(), reqModel: "hotel/g", isFailover: true, logData: logData}
		w := httptest.NewRecorder()
		h.failNoAvailableProvider(w, req, st, "g", resolveTimings{}, resolveCacheHits{}, skips)
		return w
	}

	// All skips pinned: exhaustion, dated by the earliest retry.
	w := run(breakerSkipSummary{skips: 2, earliestRetry: time.Now().Add(30 * time.Second), allPinned: true})
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("all-pinned status = %d, want 429", w.Code)
	}
	if got := w.Header().Get("Retry-After"); got != "30" {
		t.Errorf("Retry-After = %q, want %q", got, "30")
	}
	if body := w.Body.String(); !strings.Contains(body, "earliest retry in 30s") {
		t.Errorf("body %q does not date the retry", body)
	}

	// A retry instant beyond the cap is clamped to a minute: the verdict can
	// lapse long before the last circuit expires.
	w = run(breakerSkipSummary{skips: 1, earliestRetry: time.Now().Add(2 * time.Hour), allPinned: false})
	if got := w.Header().Get("Retry-After"); got != "60" {
		t.Errorf("capped Retry-After = %q, want %q", got, "60")
	}

	// No breaker skips: a genuinely empty group keeps the legacy 502.
	w = run(breakerSkipSummary{})
	if w.Code != http.StatusBadGateway {
		t.Errorf("no-skips status = %d, want 502", w.Code)
	}
}

// A hedged race whose every loser was saturated must answer with the LAST
// provider's own Retry-After: the verdict is classified on a per-attempt
// snapshot, so it has to travel back to the shared state through hedgeResult
// or the terminal 429 falls back to the class-default two seconds.
func TestRunHedgedStreaming_AllSaturatedCarriesRetryAfter(t *testing.T) {
	h := newIntegrationHandler()
	t.Cleanup(func() { stopUnitHandler(h) })

	saturated := rateLimitVerdict{class: rateLimitSaturated, retryAfter: 45 * time.Second}
	hh := newHedgeHarness([]fakeProbeSpec{
		{delay: 5 * time.Millisecond, reqErr: rateLimitReqErr(saturated, 0, "a"), rateLimit: saturated},
		{delay: 5 * time.Millisecond, reqErr: rateLimitReqErr(saturated, 1, "b"), rateLimit: saturated},
	})
	st, _ := newHedgeState(10 * time.Millisecond)

	w := runHedge(context.Background(), h, hh, st, hedgeCandidates("a", "b"))

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429 for an all-busy hedged race; body: %s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Retry-After"); got != "45" {
		t.Errorf("Retry-After = %q, want the provider's own %q, not the class default", got, "45")
	}
}

// breakerSkipSummary.note aggregates the earliest retry and the all-pinned
// verdict, and an undatable skip both counts and breaks the pin claim.
func TestBreakerSkipSummary_Note(t *testing.T) {
	var s breakerSkipSummary
	early := time.Now().Add(10 * time.Second)
	late := time.Now().Add(10 * time.Minute)

	s.note(late, true, true)
	s.note(early, true, true)
	if s.skips != 2 || !s.allPinned || !s.earliestRetry.Equal(early) {
		t.Errorf("after two pinned skips: %+v, want earliest=%v allPinned=true", s, early)
	}
	s.note(time.Time{}, false, false)
	if s.skips != 3 || s.allPinned {
		t.Errorf("an undatable skip must count and drop the all-pinned claim: %+v", s)
	}
}

// The classified-429 arms of recordBreakerOutcome: saturated touches nothing
// (a half-open circuit stays half-open with its probe budget intact), and
// exhausted opens — unless its own setting demotes it to an ordinary charge.
func TestRecordBreakerOutcome_ClassifiedRateLimits(t *testing.T) {
	env := newTestProxyHandler(t)
	defer env.Upstream.Close()
	h := env.Handler
	ctx := context.Background()

	t.Run("saturated is a no-op", func(t *testing.T) {
		cb := failover.NewCircuitBreaker(nil)
		h.circuitBreaker = cb
		st := &requestState{circuitBreakerEnabled: true}
		provID := uuid.New()
		cand := modelCandidateForBreaker(provID)

		h.recordBreakerOutcome(ctx, st, cand, 429, true, rateLimitVerdict{class: rateLimitSaturated, retryAfter: time.Second})

		if _, seen := cbConsecutiveFails(cb, provID); seen {
			t.Error("a saturated 429 touched the breaker; it must be a no-op")
		}
	})

	t.Run("saturated on a half-open probe keeps the circuit half-open", func(t *testing.T) {
		cb := failover.NewCircuitBreaker(nil)
		cb.Threshold = 1
		cb.Cooldown = time.Nanosecond
		h.circuitBreaker = cb
		st := &requestState{circuitBreakerEnabled: true}
		provID := uuid.New()
		cand := modelCandidateForBreaker(provID)
		cb.RecordFailure(provID, "p", cand.model.ModelID)
		time.Sleep(time.Millisecond)
		cb.IsOpen(provID, "p", cand.model.ModelID) // cooldown elapsed: half-open

		h.recordBreakerOutcome(ctx, st, cand, 429, true, rateLimitVerdict{class: rateLimitSaturated})

		if got := cb.GetState(provID, cand.model.ModelID); got != failover.StateHalfOpen {
			t.Errorf("state = %v, want half-open preserved: a busy signal is not a failed probe", got)
		}
	})

	t.Run("exhausted opens at once", func(t *testing.T) {
		cb := failover.NewCircuitBreaker(nil)
		h.circuitBreaker = cb
		st := &requestState{circuitBreakerEnabled: true}
		provID := uuid.New()
		cand := modelCandidateForBreaker(provID)

		h.recordBreakerOutcome(ctx, st, cand, 429, true, rateLimitVerdict{class: rateLimitExhausted, pinHint: 30 * time.Minute})

		if got := cb.GetState(provID, cand.model.ModelID); got != failover.StateOpen {
			t.Errorf("state = %v, want open on one exhausted charge", got)
		}
	})

	t.Run("open_on_exhaustion off demotes to an ordinary charge", func(t *testing.T) {
		if err := h.settingsRepo.Set(ctx, "circuit_breaker_open_on_exhaustion", "false"); err != nil {
			t.Fatalf("failed to disable: %v", err)
		}
		defer func() { _ = h.settingsRepo.Set(ctx, "circuit_breaker_open_on_exhaustion", "true") }()
		cb := failover.NewCircuitBreaker(nil)
		h.circuitBreaker = cb
		st := &requestState{circuitBreakerEnabled: true}
		provID := uuid.New()
		cand := modelCandidateForBreaker(provID)

		h.recordBreakerOutcome(ctx, st, cand, 429, true, rateLimitVerdict{class: rateLimitExhausted})

		if fails, seen := cbConsecutiveFails(cb, provID); !seen || fails != 1 {
			t.Errorf("fails = %d (seen=%v), want the single ordinary charge", fails, seen)
		}
		if got := cb.GetState(provID, cand.model.ModelID); got != failover.StateClosed {
			t.Errorf("state = %v, want closed at the default threshold", got)
		}
	})
}

func modelCandidateForBreaker(provID uuid.UUID) modelCandidate {
	return modelCandidate{
		model:    &model.Model{ModelID: "rl-model"},
		provider: &provider.Provider{ID: provID, Name: "p"},
	}
}
