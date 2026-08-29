package proxy

import (
	"bytes"
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

	"github.com/hugalafutro/model-hotel/internal/config"
	"github.com/hugalafutro/model-hotel/internal/failover"
	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/paramrewrite"
	"github.com/hugalafutro/model-hotel/internal/provider"
)

// newRetryTestHandler builds a minimal Handler wired only with the transport
// bits retryWithStrippedParams needs (upstream transport + safe dialer +
// deprecation cache). No DB is required — the helper never touches it.
func newRetryTestHandler() *Handler {
	return &Handler{
		cfg: &config.Config{MasterKey: "test-master-key-for-integration"},
		upstreamTransport: &http.Transport{
			DialContext:           NewSafeDialer(append(config.KnownProviderHosts(), "127.0.0.1"), nil).DialContext,
			ResponseHeaderTimeout: 30 * time.Second,
			IdleConnTimeout:       30 * time.Second,
		},
		safeDialer: NewSafeDialer(nil, nil),
	}
}

func newRetryTestState() *requestState {
	return &requestState{
		startTime:       time.Now(),
		reqModel:        "test-model",
		isStreaming:     false,
		bodyBytes:       []byte(`{"model":"test-model","messages":[{"role":"user","content":"hi"}],"top_p":0.9}`),
		failoverTimeout: 30 * time.Second,
	}
}

func newRetryTestCandidate(baseURL string) modelCandidate {
	return modelCandidate{
		model:    &model.Model{ID: uuid.New(), ModelID: "test-model"},
		provider: &provider.Provider{ID: uuid.New(), Name: "retry-prov", BaseURL: baseURL},
		apiKey:   "test-api-key",
	}
}

func resp400(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusBadRequest,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}

// breakerOutcome describes the observable effect of recordBreakerOutcome on a
// fresh circuit: failure creates a circuit with one consecutive fail, success
// creates a circuit with zero, and "untouched" means no circuit was created
// (no-op / disabled / a 200 whose verdict is deferred until the body is read).
type breakerOutcome int

const (
	breakerUntouched breakerOutcome = iota
	breakerFailureRecorded
	breakerSuccessRecorded
)

func TestRecordBreakerOutcome(t *testing.T) {
	cases := []struct {
		name        string
		cbEnabled   bool
		isStreaming bool
		statusCode  int
		eligible    bool
		want        breakerOutcome
	}{
		{"eligible 5xx -> failure", true, false, 500, true, breakerFailureRecorded},
		{"eligible 429 -> failure", true, false, 429, true, breakerFailureRecorded},
		{"eligible 401 -> failure", true, false, 401, true, breakerFailureRecorded},
		{"eligible 403 -> failure", true, false, 403, true, breakerFailureRecorded},
		{"eligible 404 -> no-op", true, false, 404, true, breakerUntouched},
		{"eligible 499 -> no-op", true, false, 499, true, breakerUntouched},
		{"eligible 200 -> success (exhaustive switch)", true, false, 200, true, breakerSuccessRecorded},
		{"eligible 502 -> failure", true, false, 502, true, breakerFailureRecorded},
		{"eligible 503 -> failure", true, false, 503, true, breakerFailureRecorded},
		// A 2xx is a status, not an answer, on BOTH paths: the verdict waits for
		// the body — judgeStreamForBreaker for a stream, recordAnswerOutcome for
		// a completion.
		//
		// EVERY 2xx, not just 200. These four used to credit a success here at
		// header time, which was harmless only while a non-200 success could
		// never reach the body readers. Now that it can, crediting here would
		// reset consecutiveFails and erase the charge the answer verdict is
		// about to make, so a relay answering 201 to every request could never
		// open its circuit above a threshold of one.
		{"non-eligible 200 non-streaming -> deferred (untouched)", true, false, 200, false, breakerUntouched},
		{"non-eligible 200 streaming -> deferred (untouched)", true, true, 200, false, breakerUntouched},
		{"non-eligible 201 non-streaming -> deferred (untouched)", true, false, 201, false, breakerUntouched},
		{"non-eligible 202 streaming -> deferred (untouched)", true, true, 202, false, breakerUntouched},
		{"non-eligible 204 streaming -> deferred (untouched)", true, true, 204, false, breakerUntouched},
		{"non-eligible 204 non-streaming -> deferred (untouched)", true, false, 204, false, breakerUntouched},
		// A real non-2xx that is not failover-eligible still credits here: the
		// provider is plainly alive and no body reader will run.
		{"non-eligible 400 non-streaming -> success", true, false, 400, false, breakerSuccessRecorded},
		{"breaker disabled -> untouched", false, false, 500, true, breakerUntouched},
		{"breaker disabled 200 streaming -> untouched", false, true, 200, false, breakerUntouched},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cb := failover.NewCircuitBreaker(nil)
			h := &Handler{circuitBreaker: cb}
			st := &requestState{circuitBreakerEnabled: tc.cbEnabled, isStreaming: tc.isStreaming}
			provID := uuid.New()
			cand := modelCandidate{provider: &provider.Provider{ID: provID, Name: "p"}}

			h.recordBreakerOutcome(st, cand, tc.statusCode, tc.eligible)

			fails, seen := cbConsecutiveFails(cb, provID)
			switch tc.want {
			case breakerUntouched:
				if seen {
					t.Errorf("expected breaker untouched, but circuit exists (fails=%d)", fails)
				}
			case breakerFailureRecorded:
				if !seen || fails != 1 {
					t.Errorf("expected one failure recorded, got seen=%v fails=%d", seen, fails)
				}
			case breakerSuccessRecorded:
				if !seen || fails != 0 {
					t.Errorf("expected success recorded (circuit at 0 fails), got seen=%v fails=%d", seen, fails)
				}
			}
		})
	}
}

// TestRetryWithStrippedParams_ParamErrorRetries verifies that a recognizable
// param-rejection 400 is learned into the deprecation cache and re-issued, and
// that the helper reports a successful retry with the retry response.
func TestRetryWithStrippedParams_ParamErrorRetries(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"x","object":"chat.completion","choices":[]}`)
	}))
	defer upstream.Close()

	h := newRetryTestHandler()
	st := newRetryTestState()
	cand := newRetryTestCandidate(upstream.URL)

	_, cancel := context.WithCancel(context.Background())
	var failoverCancelled atomic.Bool
	failoverCancel := func() { failoverCancelled.Store(true); cancel() }

	req := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
	var dialMs float64

	res := h.retryWithStrippedParams(req, st, cand, "openai", upstream.URL,
		resp400(`{"error":{"message":"Unsupported parameter: `+"`top_p`"+` is not supported"}}`),
		0, &dialMs, failoverCancel, "failover_timeout")

	if !res.retried {
		t.Fatalf("expected retried=true, got false (cont=%v lastReqErr=%+v)", res.cont, res.lastReqErr)
	}
	if res.cont {
		t.Errorf("expected cont=false on successful retry")
	}
	if res.resp == nil || res.resp.StatusCode != http.StatusOK {
		t.Errorf("expected retry resp 200, got %+v", res.resp)
	}
	if res.streamCancelOrigin != "retry_timeout" {
		t.Errorf("expected streamCancelOrigin=retry_timeout, got %q", res.streamCancelOrigin)
	}
	if res.retryCancel == nil {
		t.Errorf("expected non-nil retryCancel on successful retry")
	} else {
		res.retryCancel()
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("expected exactly one upstream retry call, got %d", got)
	}
	if !failoverCancelled.Load() {
		t.Errorf("expected the original failoverCancel to have been invoked")
	}
	// The rejection must have been learned into the deprecation cache.
	learnKey := paramrewrite.LearnedCacheKey(cand.provider.ID.String(), cand.model.ModelID)
	if _, ok := h.deprecationCache.Load(learnKey); !ok {
		t.Errorf("expected deprecation cache to contain learned rejection under %s", learnKey)
	}
	if res.resp != nil && res.resp.Body != nil {
		_ = res.resp.Body.Close()
	}
}

// TestRetryWithStrippedParams_NonParamErrorFallsThrough verifies that a 400 the
// parser does not recognize as a param rejection is NOT retried: the helper
// returns the original response with its body restored for normal non-200
// handling, and reports no retry.
func TestRetryWithStrippedParams_NonParamErrorFallsThrough(t *testing.T) {
	h := newRetryTestHandler()
	st := newRetryTestState()
	cand := newRetryTestCandidate("http://127.0.0.1:0") // never dialed

	var failoverCancelled atomic.Bool
	failoverCancel := func() { failoverCancelled.Store(true) }

	req := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
	var dialMs float64

	const origBody = `{"error":{"message":"some unrelated validation failure"}}`
	res := h.retryWithStrippedParams(req, st, cand, "openai", "http://127.0.0.1:0",
		resp400(origBody), 0, &dialMs, failoverCancel, "failover_timeout")

	if res.retried {
		t.Errorf("expected retried=false for non-param 400")
	}
	if res.cont {
		t.Errorf("expected cont=false (fall through to normal non-200 handling)")
	}
	if res.streamCancelOrigin != "failover_timeout" {
		t.Errorf("expected streamCancelOrigin unchanged, got %q", res.streamCancelOrigin)
	}
	if res.retryCancel != nil {
		t.Errorf("expected nil retryCancel when no retry issued")
	}
	if !failoverCancelled.Load() {
		t.Errorf("expected failoverCancel invoked even on fall-through")
	}
	// The original body must be restored and readable for downstream handling.
	body, _ := io.ReadAll(res.resp.Body)
	if string(body) != origBody {
		t.Errorf("expected original body restored, got %q", string(body))
	}
}

// ---------------------------------------------------------------------------
// Multi-round param self-heal.
//
// Upstreams name one offending param per 400, so a request carrying two of them
// needs the retry's own 400 to be parsed and learned as well.
// ---------------------------------------------------------------------------

// learnedRejected reads the rejected-param map the deprecation cache holds for
// one provider+model, or nil when nothing was learned.
func learnedRejected(t *testing.T, h *Handler, key string) map[string]bool {
	t.Helper()
	v, ok := h.deprecationCache.Load(key)
	if !ok {
		return nil
	}
	m, ok := v.(*map[string]bool)
	if !ok {
		t.Fatalf("deprecation cache holds %T under %s, want *map[string]bool", v, key)
	}
	return *m
}

// learnedRenames reads the rename map the rename cache holds for one
// provider+model, or nil when nothing was learned.
func learnedRenames(t *testing.T, h *Handler, key string) map[string]string {
	t.Helper()
	v, ok := h.paramRenameCache.Load(key)
	if !ok {
		return nil
	}
	m, ok := v.(*map[string]string)
	if !ok {
		t.Fatalf("rename cache holds %T under %s, want *map[string]string", v, key)
	}
	return *m
}

const (
	// The rename directive OpenAI's gpt-5 family answers max_tokens with.
	maxTokensRename400 = `{"error":{"message":"Unsupported parameter: 'max_tokens' is not supported with this model. Use 'max_completion_tokens' instead."}}`
	// The value-validation 400 the same models answer temperature:0 with.
	temperature400 = `{"error":{"message":"Unsupported value: 'temperature' does not support 0 with this model. Only the default (1) value is supported."}}`
)

// TestRetryWithStrippedParams_LearnsFromRetry400 is the regression test for the
// two-param request: the first 400 names max_tokens (a rename), the retry's own
// 400 names temperature. Before the fix that second 400 went straight to the
// client with nothing learned from it, so the next identical request re-learned
// from scratch. Both params must now be learned and the caller must get the 200.
func TestRetryWithStrippedParams_LearnsFromRetry400(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, temperature400)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"x","object":"chat.completion","choices":[]}`)
	}))
	defer upstream.Close()

	h := newRetryTestHandler()
	st := newRetryTestState()
	st.bodyBytes = []byte(`{"model":"test-model","messages":[{"role":"user","content":"hi"}],"max_tokens":64,"temperature":0}`)
	cand := newRetryTestCandidate(upstream.URL)

	req := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
	var dialMs float64

	res := h.retryWithStrippedParams(req, st, cand, "openai", upstream.URL,
		resp400(maxTokensRename400), 0, &dialMs, func() {}, "failover_timeout")

	if res.cont {
		t.Fatalf("expected cont=false, got lastReqErr=%+v", res.lastReqErr)
	}
	if !res.retried {
		t.Fatalf("expected retried=true")
	}
	if res.resp == nil || res.resp.StatusCode != http.StatusOK {
		t.Fatalf("expected the caller to get the 200, got %+v", res.resp)
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("expected two upstream retries (one per named param), got %d", got)
	}
	if res.retryCancel == nil {
		t.Errorf("expected non-nil retryCancel with a live body")
	} else {
		res.retryCancel()
	}
	_ = res.resp.Body.Close()

	learnKey := paramrewrite.LearnedCacheKey(cand.provider.ID.String(), cand.model.ModelID)
	if renames := learnedRenames(t, h, learnKey); renames["max_tokens"] != "max_completion_tokens" {
		t.Errorf("first 400's rename not learned: %v", renames)
	}
	if rejected := learnedRejected(t, h, learnKey); !rejected["temperature"] {
		t.Errorf("the retry's own 400 was not learned: %v", rejected)
	}
}

// TestRetryWithStrippedParams_TwoRetryCap holds the cap: an upstream that 400s
// forever, naming a fresh param every time, must be re-issued exactly twice.
// The last 400 is still learned and is handed back with its body restored.
func TestRetryWithStrippedParams_TwoRetryCap(t *testing.T) {
	named := []string{"top_p", "top_k", "frequency_penalty"}
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := int(calls.Add(1)) - 1
		param := "presence_penalty"
		if n < len(named) {
			param = named[n]
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = fmt.Fprintf(w, `{"error":{"message":"Unsupported parameter: '%s' is not supported with this model."}}`, param)
	}))
	defer upstream.Close()

	h := newRetryTestHandler()
	st := newRetryTestState()
	cand := newRetryTestCandidate(upstream.URL)

	req := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
	var dialMs float64

	res := h.retryWithStrippedParams(req, st, cand, "openai", upstream.URL,
		resp400(temperature400), 0, &dialMs, func() {}, "failover_timeout")

	if res.cont {
		t.Fatalf("expected cont=false, got lastReqErr=%+v", res.lastReqErr)
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("expected the two-retry cap to hold, got %d upstream retries", got)
	}
	if res.resp == nil || res.resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected the last 400 handed back, got %+v", res.resp)
	}
	if res.retryCancel != nil {
		t.Errorf("expected nil retryCancel: the body was buffered and the context released")
	}
	// The body must be readable by the caller's normal non-200 handling.
	body, err := io.ReadAll(res.resp.Body)
	if err != nil {
		t.Fatalf("read restored body: %v", err)
	}
	if !strings.Contains(string(body), "top_k") {
		t.Errorf("expected the last 400's body restored, got %q", string(body))
	}

	learnKey := paramrewrite.LearnedCacheKey(cand.provider.ID.String(), cand.model.ModelID)
	rejected := learnedRejected(t, h, learnKey)
	for _, p := range []string{"temperature", "top_p", "top_k"} {
		if !rejected[p] {
			t.Errorf("expected %q learned from its 400, got %v", p, rejected)
		}
	}
	if rejected["frequency_penalty"] {
		t.Errorf("frequency_penalty was learned, so a third retry was issued: %v", rejected)
	}
}

// TestRetryWithStrippedParams_StopsWhenNoNewParamNamed covers the other half of
// the termination guard, the one the round cap cannot stand in for: an upstream
// that keeps naming a param the retry ALREADY applied must not be re-issued at
// all, because the second request would be byte-identical to the first.
//
// Exactly one upstream call is the assertion that carries this. Delete the
// strict-progress test in retryWithStrippedParams (make it unconditionally true)
// and the cap alone still permits a second round, so this test — and only this
// test — turns red.
func TestRetryWithStrippedParams_StopsWhenNoNewParamNamed(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, temperature400)
	}))
	defer upstream.Close()

	h := newRetryTestHandler()
	st := newRetryTestState()
	cand := newRetryTestCandidate(upstream.URL)

	req := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
	var dialMs float64

	// Seed and retry both name temperature: the retry already stripped it, so a
	// second round has nothing new to apply.
	res := h.retryWithStrippedParams(req, st, cand, "openai", upstream.URL,
		resp400(temperature400), 0, &dialMs, func() {}, "failover_timeout")

	if res.cont {
		t.Fatalf("expected cont=false, got lastReqErr=%+v", res.lastReqErr)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("expected the repeated param to stop the loop after one retry, got %d", got)
	}
	if res.resp == nil || res.resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected the retry's 400 handed back, got %+v", res.resp)
	}
	if res.retryCancel != nil {
		t.Errorf("expected nil retryCancel: the body was buffered and the context released")
	}
	body, err := io.ReadAll(res.resp.Body)
	if err != nil {
		t.Fatalf("read restored body: %v", err)
	}
	if string(body) != temperature400 {
		t.Errorf("expected the retry's body restored, got %q", string(body))
	}
}

// TestRetryWithStrippedParams_LargeRetry400StaysParseable pins the cap chosen
// for the retry's own 400 body. It is read through a LimitReader, and the limit
// has to be the parse-sized one: learning json.Unmarshals the document, so a
// body clipped at the 16 KiB classify cap would parse to nothing and teach
// nothing. A 32 KiB error body must still be learned from and still reach the
// caller whole.
func TestRetryWithStrippedParams_LargeRetry400StaysParseable(t *testing.T) {
	large := `{"error":{"message":"Unsupported parameter: 'top_p' is not supported with this model.","detail":"` +
		strings.Repeat("x", 32<<10) + `"}}`
	if len(large) <= failoverErrorClassifyCap {
		t.Fatalf("test body must exceed the classify cap to be meaningful, got %d", len(large))
	}
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, large)
	}))
	defer upstream.Close()

	h := newRetryTestHandler()
	st := newRetryTestState()
	cand := newRetryTestCandidate(upstream.URL)

	req := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
	var dialMs float64

	res := h.retryWithStrippedParams(req, st, cand, "openai", upstream.URL,
		resp400(temperature400), 0, &dialMs, func() {}, "failover_timeout")

	if res.cont {
		t.Fatalf("expected cont=false, got lastReqErr=%+v", res.lastReqErr)
	}
	if res.resp == nil || res.resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected a 400 handed back, got %+v", res.resp)
	}
	body, err := io.ReadAll(res.resp.Body)
	if err != nil {
		t.Fatalf("read restored body: %v", err)
	}
	if string(body) != large {
		t.Errorf("expected the oversized body restored whole (%d bytes), got %d", len(large), len(body))
	}
	learnKey := paramrewrite.LearnedCacheKey(cand.provider.ID.String(), cand.model.ModelID)
	if rejected := learnedRejected(t, h, learnKey); !rejected["top_p"] {
		t.Errorf("a 32 KiB 400 was not parsed for learning: %v", rejected)
	}
}

// TestRetryWithStrippedParams_LargeFirst400 pins the same bound on the FIRST
// 400, which is read through readLearnable400 alongside the retry's. Both
// halves of that read matter and are asserted separately:
//
//   - a body the parser recognises must still be learned from at 32 KiB, i.e.
//     the cap must not be the 16 KiB classify one;
//   - a body it does not recognise must still reach the caller byte-identical,
//     which is the fall-through behaviour the brief pins as unchanged.
func TestRetryWithStrippedParams_LargeFirst400(t *testing.T) {
	pad := strings.Repeat("x", 32<<10)

	t.Run("unrecognised body restores intact", func(t *testing.T) {
		large := `{"error":{"message":"some unrelated validation failure","detail":"` + pad + `"}}`
		if len(large) <= failoverErrorClassifyCap {
			t.Fatalf("test body must exceed the classify cap to be meaningful, got %d", len(large))
		}
		h := newRetryTestHandler()
		st := newRetryTestState()
		cand := newRetryTestCandidate("http://127.0.0.1:0") // never dialled
		req := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
		var dialMs float64

		res := h.retryWithStrippedParams(req, st, cand, "openai", "http://127.0.0.1:0",
			resp400(large), 0, &dialMs, func() {}, "failover_timeout")

		if res.retried || res.cont {
			t.Fatalf("expected no retry for an unrecognised 400, got retried=%v cont=%v", res.retried, res.cont)
		}
		body, err := io.ReadAll(res.resp.Body)
		if err != nil {
			t.Fatalf("read restored body: %v", err)
		}
		if string(body) != large {
			t.Errorf("expected the oversized body restored whole (%d bytes), got %d", len(large), len(body))
		}
	})

	t.Run("named param still learned", func(t *testing.T) {
		large := `{"error":{"message":"Unsupported parameter: 'top_k' is not supported with this model.","detail":"` + pad + `"}}`
		var calls atomic.Int32
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			calls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"id":"x","object":"chat.completion","choices":[]}`)
		}))
		defer upstream.Close()

		h := newRetryTestHandler()
		st := newRetryTestState()
		cand := newRetryTestCandidate(upstream.URL)
		req := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
		var dialMs float64

		res := h.retryWithStrippedParams(req, st, cand, "openai", upstream.URL,
			resp400(large), 0, &dialMs, func() {}, "failover_timeout")

		if !res.retried {
			t.Fatalf("a 32 KiB first 400 was not parsed, so no retry was issued (cont=%v)", res.cont)
		}
		if res.resp == nil || res.resp.StatusCode != http.StatusOK {
			t.Fatalf("expected the caller to get the 200, got %+v", res.resp)
		}
		if res.retryCancel != nil {
			res.retryCancel()
		}
		_ = res.resp.Body.Close()
		if got := calls.Load(); got != 1 {
			t.Errorf("expected one upstream retry, got %d", got)
		}
		learnKey := paramrewrite.LearnedCacheKey(cand.provider.ID.String(), cand.model.ModelID)
		if rejected := learnedRejected(t, h, learnKey); !rejected["top_k"] {
			t.Errorf("a 32 KiB first 400 was not parsed for learning: %v", rejected)
		}
	})
}

// TestDoUpstream_ProviderErrorCapturesUnderlying verifies that a terminal
// transport error (here: connection refused, retried then exhausted) is
// captured into the structured error's Underlying field, classified as a
// provider error.
func TestDoUpstream_ProviderErrorCapturesUnderlying(t *testing.T) {
	h := newRetryTestHandler()
	st := newRetryTestState()
	st.logData = &requestLogData{}
	cand := newRetryTestCandidate("http://127.0.0.1:1")

	req, err := http.NewRequestWithContext(context.Background(), "POST",
		"http://127.0.0.1:1/chat/completions", strings.NewReader(string(st.bodyBytes)))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	var dialMs float64
	resp, ok := h.doUpstream(context.Background(), req, st, cand, 0, &dialMs)
	if ok || resp != nil {
		t.Fatalf("expected failure, got ok=%v resp=%v", ok, resp)
	}
	if st.lastReqErr.Kind != KindProviderError {
		t.Errorf("expected Kind=%q, got %q", KindProviderError, st.lastReqErr.Kind)
	}
	if !strings.Contains(st.lastReqErr.Underlying, "connection refused") {
		t.Errorf("Underlying did not capture transport error: %q", st.lastReqErr.Underlying)
	}
	if st.lastReqErr.Provider != "retry-prov" {
		t.Errorf("expected Provider=retry-prov, got %q", st.lastReqErr.Provider)
	}
}

// TestDoUpstream_ClientDisconnectPreservesProviderError is the regression test
// for the motivating bug: when the client disconnects while we are retrying a
// flaky provider, the real provider error must NOT be silently dropped — it is
// preserved as Underlying even though the terminal cause is the disconnect.
// The first try (connection refused) is retryable; the context is cancelled
// during the (>=100ms) backoff, well after the ~40ms cancel timer.
func TestDoUpstream_ClientDisconnectPreservesProviderError(t *testing.T) {
	h := newRetryTestHandler()
	st := newRetryTestState()
	st.logData = &requestLogData{}
	cand := newRetryTestCandidate("http://127.0.0.1:1")

	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, "POST",
		"http://127.0.0.1:1/chat/completions", strings.NewReader(string(st.bodyBytes)))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	go func() {
		time.Sleep(40 * time.Millisecond)
		cancel()
	}()
	var dialMs float64
	resp, ok := h.doUpstream(ctx, req, st, cand, 0, &dialMs)
	if ok || resp != nil {
		t.Fatalf("expected failure, got ok=%v resp=%v", ok, resp)
	}
	if st.lastReqErr.Kind != KindClientDisconnect {
		t.Errorf("expected Kind=%q, got %q", KindClientDisconnect, st.lastReqErr.Kind)
	}
	if !strings.Contains(st.lastReqErr.Underlying, "connection refused") {
		t.Errorf("client disconnect DROPPED the real provider error (the bug this fixes): Underlying=%q", st.lastReqErr.Underlying)
	}
}

// ---------------------------------------------------------------------------
// forwardUpstreamError response shape.
//
// This is the reachable half of "a non-2xx must never reach the client as a
// success-shaped body": the chat path sends every non-200 here before
// handleNonStreamingResponse can see it. A non-failover-eligible status (a
// payload-class refusal) answers straight from the provider's body whether or
// not candidates remain; a failover-eligible one only arrives here exhausted
// and answers with the classified envelope, never the body.
// ---------------------------------------------------------------------------

// runForwardUpstreamError drives one upstream answer through the function and
// returns what the client got plus the log row it left behind.
func runForwardUpstreamError(t *testing.T, h *Handler, status int, body string, isFailoverEligible bool) (*httptest.ResponseRecorder, *requestLogData) {
	t.Helper()
	return runForwardUpstreamErrorWith(t, h, status, body, isFailoverEligible, "")
}

// runForwardUpstreamErrorWithKey is runForwardUpstreamError with the candidate
// carrying a decrypted provider key, for the exact-value masking tests.
func runForwardUpstreamErrorWithKey(t *testing.T, h *Handler, status int, body, apiKey string) (*httptest.ResponseRecorder, *requestLogData) {
	t.Helper()
	return runForwardUpstreamErrorWith(t, h, status, body, false, apiKey)
}

func runForwardUpstreamErrorWith(t *testing.T, h *Handler, status int, body string, isFailoverEligible bool, apiKey string) (*httptest.ResponseRecorder, *requestLogData) {
	t.Helper()

	logData := &requestLogData{
		modelID:        "gpt-5.1-codex",
		providerName:   "opencode-zen",
		virtualKeyName: "test-key",
		virtualKeyID:   "00000000-0000-0000-0000-000000000001",
		state:          "pending",
	}
	st := &requestState{startTime: time.Now(), logData: logData}
	candidate := modelCandidate{
		model:    &model.Model{ModelID: "gpt-5.1-codex"},
		provider: &provider.Provider{ID: uuid.New(), Name: "opencode-zen"},
		apiKey:   apiKey,
	}
	// beginAttempt stamps this in production; the helper bypasses it.
	logData.masker = newCredentialMasker(apiKey)
	resp := &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}

	w := httptest.NewRecorder()
	if outcome := h.forwardUpstreamError(w, st, candidate, resp, 0, isFailoverEligible, 1.5); outcome != outcomeFatal {
		t.Fatalf("expected outcomeFatal, got %v", outcome)
	}
	return w, logData
}

// A non-2xx whose body carries no error object leaves as a synthesised envelope,
// whatever the eligibility verdict. zenChatShapedBody is the production shape:
// OpenCode Zen and OpenCode Go answer some failed requests with a complete
// chat.completion under an HTTP 400, which is valid JSON with nothing for a
// client to read `.error.message` off.
func TestForwardUpstreamError_Non2xxWithoutErrorObjectGetsEnvelope(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandler(h)

	// Every shape whose "error" member leaves a client with nothing to read is
	// the same case as no error member at all.
	bodies := map[string]string{
		"chat completion shape": zenChatShapedBody,
		"explicit null error":   `{"id":"chatcmpl_u5tt67g6rmf","error":null}`,
		"empty error object":    `{"id":"chatcmpl_u5tt67g6rmf","error":{}}`,
		"empty error string":    `{"id":"chatcmpl_u5tt67g6rmf","error":""}`,
		"blank error string":    `{"id":"chatcmpl_u5tt67g6rmf","error":"   "}`,
		"empty error list":      `{"id":"chatcmpl_u5tt67g6rmf","error":[]}`,
		"not an object":         `["upstream said no"]`,
		"not json at all":       `<html><body>400 Bad Request</body></html>`,
		// The C convention: an "error" member reporting there wasn't one. The
		// body may still carry detail, but not where a client reads `.error`,
		// which is the same position as any other body without an error member.
		"false error": `{"id":"chatcmpl_u5tt67g6rmf","error":false,"message":"context_length_exceeded"}`,
		"zero error":  `{"id":"chatcmpl_u5tt67g6rmf","error":0}`,
		// The same convention one level down: a relay's no-error struct.
		"zeroed error struct": `{"error":{"code":0,"message":"","type":""}}`,
		"all-null error":      `{"error":{"code":null,"message":null,"type":null}}`,
	}

	for name, upstreamBody := range bodies {
		for _, eligible := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/isFailoverEligible=%v", name, eligible), func(t *testing.T) {
				w, logData := runForwardUpstreamError(t, h, http.StatusBadRequest, upstreamBody, eligible)

				if w.Code != http.StatusBadRequest {
					t.Errorf("expected upstream status 400 to reach the client, got %d", w.Code)
				}
				var got map[string]json.RawMessage
				if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
					t.Fatalf("client body is not JSON: %v (%s)", err, w.Body.String())
				}
				rawErr, present := got["error"]
				if !present {
					t.Fatalf("client body has no error object: %s", w.Body.String())
				}
				var errObj struct {
					Message string `json:"message"`
				}
				if err := json.Unmarshal(rawErr, &errObj); err != nil {
					t.Fatalf("re-parse error object: %v", err)
				}
				if errObj.Message == "" {
					t.Errorf("error envelope carries no message: %s", w.Body.String())
				}
				if _, present := got["choices"]; present {
					t.Errorf("success-shaped choices reached the client on a 400: %s", w.Body.String())
				}
				if logData.state != "failed" {
					t.Errorf("expected state=%q, got %q", "failed", logData.state)
				}
				if logData.errorMessage == "" {
					t.Error("upstream body not recoverable from error_message")
				}
			})
		}
	}
}

// The upstream's own error object is detail this gateway cannot reconstruct
// (code, type, param, provider-specific fields), so a payload-class error
// forwards it byte for byte. The eligibility verdict is what decides, not how
// many candidates remain: a direct single-provider request gets the same body
// a sequentially failing-over hotel/ group request does. (A hedged race is the
// exception - its orchestrator writes the terminal error without this
// function.)
func TestForwardUpstreamError_ErrorObjectForwardedVerbatim(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandler(h)

	const upstreamBody = `{"error":{"message":"custom_validation_error","type":"invalid_request_error","param":"messages[0].content","code":"context_length_exceeded"},"request_id":"req_abc123"}`

	w, logData := runForwardUpstreamError(t, h, http.StatusBadRequest, upstreamBody, false)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
	if got := w.Body.String(); got != upstreamBody {
		t.Errorf("upstream error body not forwarded byte for byte:\n got: %s\nwant: %s", got, upstreamBody)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected application/json, got %q", ct)
	}
	if logData.state != "failed" {
		t.Errorf("expected state=%q, got %q", "failed", logData.state)
	}
}

// An error member that carries something is the provider's to describe, whatever
// shape it chose. Ollama answers with a bare string rather than an object, which
// is real detail and must survive.
func TestForwardUpstreamError_NonObjectErrorForwardedVerbatim(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandler(h)

	bodies := map[string]string{
		"ollama string error": `{"error":"unknown parameter, check the request body"}`,
		"list of errors":      `{"error":[{"message":"first"},{"message":"second"}]}`,
	}

	for name, upstreamBody := range bodies {
		t.Run(name, func(t *testing.T) {
			w, _ := runForwardUpstreamError(t, h, http.StatusBadRequest, upstreamBody, false)

			if w.Code != http.StatusBadRequest {
				t.Errorf("expected status 400, got %d", w.Code)
			}
			if got := w.Body.String(); got != upstreamBody {
				t.Errorf("upstream error detail not forwarded byte for byte:\n got: %s\nwant: %s", got, upstreamBody)
			}
		})
	}
}

// A failover-eligible status only reaches forwardUpstreamError with no
// candidates left, and its body is the kind that can quote the operator's
// provider credentials or account details (auth failures, billing, quota,
// server faults). The client gets the classified envelope, never the body, no
// matter how juicy the error object inside it is.
func TestForwardUpstreamError_EligibleClassGetsEnvelopeNotBody(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandler(h)

	const secret = "sk-live-0123456789abcdef0123456789abcdef"
	upstreamBody := `{"error":{"message":"Incorrect API key provided: ` + secret + `. You can find your API key at https://platform.example.com.","type":"invalid_request_error","code":"invalid_api_key"}}`

	for _, status := range []int{
		http.StatusUnauthorized,
		http.StatusPaymentRequired,
		http.StatusForbidden,
		http.StatusNotFound,
		http.StatusTooManyRequests,
		http.StatusInternalServerError,
	} {
		t.Run(fmt.Sprintf("status=%d", status), func(t *testing.T) {
			w, logData := runForwardUpstreamError(t, h, status, upstreamBody, true)

			if w.Code != status {
				t.Errorf("expected status %d to reach the client, got %d", status, w.Code)
			}
			body := w.Body.String()
			if strings.Contains(body, secret) {
				t.Errorf("provider credential reached the client: %s", body)
			}
			if strings.Contains(body, "Incorrect API key") {
				t.Errorf("upstream error body reached the client on an eligible status: %s", body)
			}
			var got struct {
				Error struct {
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
				t.Fatalf("client body is not an error envelope: %v (%s)", err, body)
			}
			if got.Error.Message == "" {
				t.Errorf("envelope carries no classified reason: %s", body)
			}
			// The operator still gets the full detail in the request log.
			if !strings.Contains(logData.errorMessage, "Incorrect API key") {
				t.Errorf("upstream body missing from the request log: %s", logData.errorMessage)
			}
		})
	}
}

// The forward-deny status class is static. shouldFailover's 429 verdict
// follows the failover_on_rate_limit setting, so with that toggle off a 429
// arrives here as NOT failover-eligible - and its body is still the operator's
// account state (OpenAI's insufficient_quota text, MiniMax's "insufficient
// balance" remapped onto 429 by remapMiniMaxBusinessError). What a client may
// see must not move with a routing knob, so these stay enveloped even when
// ineligible.
func TestForwardUpstreamError_StaticDenyClassIgnoresEligibility(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandler(h)

	upstreamBody := `{"error":{"message":"You exceeded your current quota, please check your plan and billing details.","type":"insufficient_quota"}}`

	for _, status := range []int{
		http.StatusTooManyRequests,
		http.StatusUnauthorized,
		http.StatusPaymentRequired,
		http.StatusNotFound,
		http.StatusBadGateway,
	} {
		t.Run(fmt.Sprintf("status=%d", status), func(t *testing.T) {
			w, _ := runForwardUpstreamError(t, h, status, upstreamBody, false)

			if w.Code != status {
				t.Errorf("expected status %d to reach the client, got %d", status, w.Code)
			}
			body := w.Body.String()
			if strings.Contains(body, "insufficient_quota") || strings.Contains(body, "billing details") {
				t.Errorf("operator account detail reached the client on a denied status: %s", body)
			}
			var got struct {
				Error struct {
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil || got.Error.Message == "" {
				t.Errorf("expected a classified envelope, got: %s", body)
			}
		})
	}
}

// A forwardable body that overflows its cap is demoted to the envelope rather
// than forwarded truncated: a client must never receive invalid JSON where the
// provider sent something complete.
func TestForwardUpstreamError_OversizedPayloadBodyGetsEnvelope(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandler(h)

	upstreamBody := `{"error":{"message":"` + strings.Repeat("a", forwardableErrorBodyCap) + `"}}`

	w, _ := runForwardUpstreamError(t, h, http.StatusBadRequest, upstreamBody, false)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
	if !json.Valid(w.Body.Bytes()) {
		t.Fatalf("client received invalid JSON (%d bytes)", w.Body.Len())
	}
	if w.Body.Len() > 32<<10 {
		t.Errorf("oversized upstream body reached the client: %d bytes", w.Body.Len())
	}
}

// The non-JSON default branch echoes the sanitized body inside an envelope, so
// it needs the same credential scrub as the verbatim branch.
func TestForwardUpstreamError_NonJSONBodyMasksKeyShapedTokens(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandler(h)

	const secret = "sk-live-0123456789abcdef0123456789abcdef"
	upstreamBody := `upstream proxy rejected key ` + secret + ` (not json)`

	w, _ := runForwardUpstreamError(t, h, http.StatusBadRequest, upstreamBody, false)

	body := w.Body.String()
	if strings.Contains(body, secret) {
		t.Errorf("key-shaped token reached the client via the non-JSON branch: %s", body)
	}
	if !strings.Contains(body, "[redacted]") {
		t.Errorf("non-JSON branch did not mask: %s", body)
	}
}

// A forwarded payload-class body is the provider's text, and providers have
// been known to quote credentials in it. Key-shaped tokens are masked on the
// way to the client; everything else is untouched.
func TestForwardUpstreamError_ForwardedBodyMasksKeyShapedTokens(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandler(h)

	const secret = "sk-proj-0123456789abcdef0123456789abcdef"
	upstreamBody := `{"error":{"message":"invalid key ` + secret + ` for this endpoint","type":"invalid_request_error","param":"api_key"}}`

	w, logData := runForwardUpstreamError(t, h, http.StatusBadRequest, upstreamBody, false)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
	body := w.Body.String()
	if strings.Contains(body, secret) {
		t.Errorf("key-shaped token reached the client: %s", body)
	}
	if !strings.Contains(body, "[redacted]") {
		t.Errorf("masked token not marked as redacted: %s", body)
	}
	if !strings.Contains(body, "invalid_request_error") || !strings.Contains(body, `"param":"api_key"`) {
		t.Errorf("masking damaged the rest of the body: %s", body)
	}
	if !json.Valid(w.Body.Bytes()) {
		t.Errorf("masked body is no longer valid JSON: %s", body)
	}
	// The request log is readable by non-admin users with the logs grant, so
	// it gets the same scrub.
	if strings.Contains(logData.errorMessage, secret) {
		t.Errorf("key-shaped token reached the request log: %s", logData.errorMessage)
	}
}

// maskKeyShapedTokens is a text scrub, so the contract worth pinning is which
// shapes it eats and, just as much, which prose it leaves alone.
func TestMaskKeyShapedTokens(t *testing.T) {
	masked := map[string]string{
		"openai key":      `key sk-0123456789abcdef0123 rejected`,
		"openai project":  `key sk-proj-0123456789abcdef0123 rejected`,
		"anthropic key":   `key sk-ant-api03-0123456789abcdef rejected`,
		"groq key":        `key gsk_0123456789abcdef0123 rejected`,
		"xai key":         `key xai-0123456789abcdef0123 rejected`,
		"google key":      `key AIzaSyA0123456789abcdef0123456789abcde rejected`,
		"huggingface key": `key hf_0123456789abcdefABCDEF rejected`,
		"aws access key":  `credential AKIAIOSFODNN7EXAMPLE not authorized`,
		"bare jwt":        `key eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N in body`,
		"bearer token":    `header "Bearer eyJhbGciOiJIUzI1NiJ9.payload.sig" invalid`,
	}
	for name, in := range masked {
		t.Run("masks/"+name, func(t *testing.T) {
			got := string(maskKeyShapedTokens([]byte(in)))
			if !strings.Contains(got, "[redacted]") {
				t.Errorf("nothing masked in %q -> %q", in, got)
			}
			if got == in {
				t.Errorf("input unchanged: %q", in)
			}
		})
	}

	untouched := map[string]string{
		"short sk prefix":       `the sk-abc field is required`,
		"prose with sk":         `risk-based checks and task-level errors`,
		"request id":            `request req_abc123 failed`,
		"embedded in word":      `whisk-0123456789abcdef0123`,
		"plain json error":      `{"error":{"message":"custom_validation_error","param":"messages[0].content"}}`,
		"bare word bearer":      `the bearer of this token`,
		"snake_case identifier": `param 'sk_business_unit_identifier' unknown`,
		"digitless bearer tail": `Bearer authentication-required for this endpoint`,
	}
	for name, in := range untouched {
		t.Run("leaves/"+name, func(t *testing.T) {
			if got := string(maskKeyShapedTokens([]byte(in))); got != in {
				t.Errorf("false positive:\n in:  %s\n got: %s", in, got)
			}
		})
	}
}

// forwardUpstreamError is never handed a success any more, so the old
// "a 2xx body is forwarded untouched" contract moved to the caller: see
// TestAttemptCandidate_ASuccessStatusOtherThan200IsMetered and its siblings in
// success_status_test.go, which prove a 2xx is served, metered and logged
// completed instead of reaching this function at all.

// A custom or self-hosted gateway key has no prefix the shape regex knows, so
// the exact decrypted credential is the control that covers it. Both forwarded
// branches must scrub it; the request log keeps the original.
func TestForwardUpstreamError_MasksExactProviderKey(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandler(h)

	const secret = "myCustomGatewayKey2024x9z8"
	for name, body := range map[string]string{
		"json":    `{"error":{"message":"invalid request for account key ` + secret + `: context too long | your key: ` + secret + `","type":"invalid_request_error","code":"context_length_exceeded"}}`,
		"nonjson": `upstream rejected key ` + secret + ` (not json)`,
	} {
		t.Run(name, func(t *testing.T) {
			w, logData := runForwardUpstreamErrorWithKey(t, h, http.StatusBadRequest, body, secret)
			got := w.Body.String()
			if strings.Contains(got, secret) {
				t.Fatalf("exact provider key reached the client: %s", got)
			}
			if strings.Count(got, "[redacted]") != strings.Count(body, secret) {
				t.Errorf("every occurrence must be masked, got: %s", got)
			}
			if strings.Contains(logData.errorMessage, secret) {
				t.Errorf("exact key must not reach the request log either: %s", logData.errorMessage)
			}
			if !strings.Contains(logData.errorMessage, "[redacted]") {
				t.Errorf("request log should keep the body with the key redacted: %s", logData.errorMessage)
			}
		})
	}
}

// exactMaskWriter must see a key split across writes, release everything on
// Flush, and pass bytes straight through when there is no key to hold for.
func TestExactMaskWriter(t *testing.T) {
	const secret = "myCustomGatewayKey2024x9z8"
	var out bytes.Buffer
	e := newExactMaskWriter(&out, newCredentialMasker(secret))
	for _, w := range []string{"prefix " + secret[:9], secret[9:20], secret[20:] + " mid " + secret[:3], secret[3:] + " end"} {
		if n, err := e.Write([]byte(w)); err != nil || n != len(w) {
			t.Fatalf("Write = %d, %v; want %d, nil", n, err, len(w))
		}
	}
	if err := e.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if got := out.String(); got != "prefix [redacted] mid [redacted] end" {
		t.Errorf("got %q", got)
	}

	out.Reset()
	e = newExactMaskWriter(&out, credentialMasker{})
	if _, err := e.Write([]byte("raw " + secret)); err != nil {
		t.Fatal(err)
	}
	if out.String() != "raw "+secret {
		t.Errorf("no key: bytes must pass through unchanged, got %q", out.String())
	}
}

// A keyless or placeholder credential below credentialMinLen must not turn
// into a body-wide rewrite.
func TestCredentialMasker(t *testing.T) {
	body := []byte(`{"error":{"message":"ab: key abcdefgh12 and sk-proj-0123456789abcdef0123456789abcdef"}}`)
	if got := newCredentialMasker("").mask(body); !bytes.Equal(got, maskKeyShapedTokens(body)) {
		t.Errorf("empty key must mask by shape only, got %s", got)
	}
	if got := newCredentialMasker("ab").mask(body); !bytes.Equal(got, maskKeyShapedTokens(body)) {
		t.Errorf("short key must mask by shape only, got %s", got)
	}
	got := string(newCredentialMasker("abcdefgh12").mask(body))
	if strings.Contains(got, "abcdefgh12") || strings.Contains(got, "sk-proj-") {
		t.Errorf("exact and shape layers must both apply, got %s", got)
	}
	if !strings.HasPrefix(got, `{"error":{"message":"ab: key [redacted] and [redacted]"}}`) {
		t.Errorf("unexpected rewrite: %s", got)
	}
}
