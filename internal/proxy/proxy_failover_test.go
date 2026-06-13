package proxy

import (
	"context"
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
// (no-op / disabled / deferred streaming-200).
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
		{"non-eligible 200 non-streaming -> success", true, false, 200, false, breakerSuccessRecorded},
		{"non-eligible 200 streaming -> deferred (untouched)", true, true, 200, false, breakerUntouched},
		{"non-eligible non-200 streaming -> success", true, true, 204, false, breakerSuccessRecorded},
		{"non-eligible 204 non-streaming -> success", true, false, 204, false, breakerSuccessRecorded},
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
	if _, ok := h.deprecationCache.Load("openai:" + cand.model.ModelID); !ok {
		t.Errorf("expected deprecation cache to contain learned rejection for openai:%s", cand.model.ModelID)
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
