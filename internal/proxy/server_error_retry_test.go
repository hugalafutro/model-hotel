package proxy

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

// directChatRequest sends a chat completion straight at the env's provider
// (no hotel/ group, so the provider is the only candidate), streaming or not.
func directChatRequest(t *testing.T, env *testProxyEnv, stream bool) *httptest.ResponseRecorder {
	t.Helper()
	body := `{"model": "` + env.ProviderName + `/` + env.ModelName + `", "messages": [{"role": "user", "content": "hello"}], "stream": ` + map[bool]string{false: "false", true: "true"}[stream] + `}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	ctx := context.WithValue(req.Context(), virtualKeyNameKey, "test-key")
	ctx = context.WithValue(ctx, VirtualKeyHashKey, env.KeyHash)
	w := httptest.NewRecorder()
	env.Handler.ChatCompletions(w, req.WithContext(ctx))
	return w
}

const zaiNetworkFailureBody = `{"error":{"code":"1234","message":"Internal network failure, error id: 2026, please try again later."}}`

func TestRetryableServerError(t *testing.T) {
	for status, want := range map[int]bool{
		http.StatusInternalServerError:     true,
		http.StatusBadGateway:              true,
		http.StatusServiceUnavailable:      true,
		http.StatusGatewayTimeout:          true,
		http.StatusNotImplemented:          false,
		http.StatusHTTPVersionNotSupported: false,
		529:                                false,
		http.StatusTooManyRequests:         false,
		http.StatusBadRequest:              false,
		http.StatusOK:                      false,
	} {
		if got := retryableServerError(status); got != want {
			t.Errorf("retryableServerError(%d) = %v, want %v", status, got, want)
		}
	}
}

// A retryable 5xx on the only candidate earns one backoff and one retry of
// the same provider, so a provider's one-shot "internal network failure" is
// not the client's answer. The trail carries both attempts: the 500 with the
// provider's sentence, then the served retry as a further attempt.
func TestChatCompletions_ServerErrorLastCandidate_RetriesOnce(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(map[bool]string{false: "non-streaming", true: "streaming"}[stream], func(t *testing.T) {
			var calls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if calls.Add(1) == 1 {
					w.WriteHeader(http.StatusInternalServerError)
					_, _ = io.WriteString(w, zaiNetworkFailureBody)
					return
				}
				var reqBody map[string]any
				_ = json.NewDecoder(r.Body).Decode(&reqBody)
				if stream {
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"}}]}\n\ndata: {\"choices\":[],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1}}\n\ndata: [DONE]\n\n")
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, chatCompletionJSON(reqBody["model"].(string)))
			}))
			defer upstream.Close()
			env := newTestProxyEnvWithUpstream(t, upstream)

			start := time.Now()
			w := directChatRequest(t, env, stream)
			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 after the server-error retry; body: %s", w.Code, w.Body.String())
			}
			if got := calls.Load(); got != 2 {
				t.Errorf("upstream calls = %d, want 2 (the 500 and its one retry)", got)
			}
			if elapsed := time.Since(start); elapsed < serverErrorRetryBackoff {
				t.Errorf("retried after %v, want at least the %v backoff", elapsed, serverErrorRetryBackoff)
			}
			// The 500 was a real failure: charged once, and the served retry
			// then reset the count.
			if fails, seen := cbConsecutiveFails(env.Handler.circuitBreaker, env.ProviderID); seen && fails != 0 {
				t.Errorf("breaker consecutive fails = %d after a served retry, want 0", fails)
			}

			got := waitForTrailByProvider(t, env.ProviderID, 2)
			if len(got) != 2 {
				t.Fatalf("attempts = %+v, want the 500 and the served retry", got)
			}
			if got[0].Attempt != 0 || got[0].Status != 500 || got[0].ErrorKind != string(KindProviderError) || got[0].Breaker != breakerCharge || !strings.Contains(got[0].Detail, "Internal network failure") {
				t.Errorf("attempt 0 = %+v, want the charged 500 with the provider's sentence", got[0])
			}
			if got[1].Attempt != 1 || got[1].Status != 200 || got[1].Breaker != breakerSuccess || got[1].Hedged {
				t.Errorf("attempt 1 = %+v, want the served retry as a further attempt", got[1])
			}
		})
	}
}

// A second retryable 5xx is terminal: one retry, never a loop, and the
// client gets the provider's status. The terminal attempt's trail record
// carries no detail, since the row's error_message is that text already;
// the first attempt's does, since nothing else records it.
func TestChatCompletions_ServerErrorTwice_ForwardsUpstreamError(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(w, zaiNetworkFailureBody)
	}))
	defer upstream.Close()
	env := newTestProxyEnvWithUpstream(t, upstream)

	w := directChatRequest(t, env, false)
	if w.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want the provider's 502 forwarded; body: %s", w.Code, w.Body.String())
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("upstream calls = %d, want exactly 2 (one retry, not a loop)", got)
	}
	if fails, _ := cbConsecutiveFails(env.Handler.circuitBreaker, env.ProviderID); fails != 2 {
		t.Errorf("breaker consecutive fails = %d, want 2: each 502 was a real failure", fails)
	}

	got := waitForTrailByProvider(t, env.ProviderID, 2)
	if len(got) != 2 {
		t.Fatalf("attempts = %+v, want the 502 and its failed retry", got)
	}
	if got[0].Attempt != 0 || got[0].Status != 502 || got[0].Breaker != breakerCharge || !strings.Contains(got[0].Detail, "Internal network failure") {
		t.Errorf("attempt 0 = %+v, want the charged 502 with the provider's sentence", got[0])
	}
	if got[1].Attempt != 1 || got[1].Status != 502 || got[1].Breaker != breakerCharge || got[1].Detail != "" {
		t.Errorf("attempt 1 = %+v, want the terminal 502 with no detail (error_message carries it)", got[1])
	}
	var errMsg string
	if err := testDB.Pool().QueryRow(context.Background(), `SELECT error_message FROM request_logs WHERE provider_id = $1 ORDER BY created_at DESC LIMIT 1`, env.ProviderID).Scan(&errMsg); err != nil {
		t.Fatalf("error_message: %v", err)
	}
	if !strings.Contains(errMsg, "Internal network failure") {
		t.Errorf("error_message = %q, want the provider's sentence on the row itself", errMsg)
	}
}

// A 501 is a refusal that a retry cannot change: forwarded on the first
// answer, one upstream call.
func TestChatCompletions_NotImplemented_NotRetried(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusNotImplemented)
		_, _ = io.WriteString(w, `{"error":{"message":"not implemented"}}`)
	}))
	defer upstream.Close()
	env := newTestProxyEnvWithUpstream(t, upstream)

	w := directChatRequest(t, env, false)
	if w.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want 501 forwarded; body: %s", w.Code, w.Body.String())
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("upstream calls = %d, want 1: a 501 is not retried", got)
	}
}

// The server-error retry never waits past the request's own budget or a
// client that already left, the same guards as the saturation retry.
func TestRetryServerErrorCandidate_BudgetGuards(t *testing.T) {
	env := newTestProxyHandler(t)
	defer env.Upstream.Close()
	h := env.Handler
	attempted := false
	fn := func(http.ResponseWriter, *http.Request, *requestState, modelCandidate, int, int) candidateOutcome {
		attempted = true
		return outcomeServed
	}

	st := &requestState{overallDeadline: time.Now().Add(-time.Second), logData: &requestLogData{}}
	if h.retryServerErrorCandidate(httptest.NewRecorder(), httptest.NewRequest("POST", "/", http.NoBody), st, modelCandidateForBreaker(uuid.New()), 1, fn) {
		t.Error("an exhausted budget reported the request as answered")
	}
	if attempted {
		t.Error("the retry ran with no time budget left")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest("POST", "/", http.NoBody).WithContext(ctx)
	logData, _ := h.newPendingRequestLog(req, endpointTypeChat, "m", false)
	st = &requestState{startTime: time.Now(), overallDeadline: time.Now().Add(time.Hour), logData: logData}
	w := httptest.NewRecorder()
	if !h.retryServerErrorCandidate(w, req, st, modelCandidateForBreaker(uuid.New()), 1, fn) {
		t.Error("a disconnect during the backoff must be terminal, not fall through to the exhaustion path")
	}
	if attempted {
		t.Error("the retry ran after the client left")
	}
	if w.Code != statusClientClosedRequest || st.lastReqErr.Kind != KindClientDisconnect {
		t.Errorf("status = %d kind = %v, want 499 client_disconnect", w.Code, st.lastReqErr.Kind)
	}
}

// The two one-shot retries chain, and each fires once: a saturated 429 after
// the server-error backoff still earns its own wait-and-retry, and a third
// failure is terminal.
func TestSettleLastCandidateRetry_ChainsOnce(t *testing.T) {
	env := newTestProxyHandler(t)
	defer env.Upstream.Close()
	h := env.Handler
	var indices []int
	fn := func(_ http.ResponseWriter, _ *http.Request, st *requestState, _ modelCandidate, attempt, _ int) candidateOutcome {
		indices = append(indices, attempt)
		if !st.saturationRetried {
			st.saturationRetried = true
			return outcomeRetrySaturated
		}
		return outcomeFailover
	}
	st := &requestState{startTime: time.Now(), overallDeadline: time.Now().Add(time.Hour), serverErrorRetried: true, rateLimit: rateLimitVerdict{retryAfter: time.Millisecond}, logData: &requestLogData{}}
	req := httptest.NewRequest("POST", "/", http.NoBody)
	if h.settleLastCandidateRetry(httptest.NewRecorder(), req, st, modelCandidateForBreaker(uuid.New()), 1, fn, outcomeRetryServerError) {
		t.Error("a failed final retry must fall through to the exhaustion path")
	}
	// The server-error retry ran as attempt 1, answered saturated, and the
	// saturation retry ran as attempt 2, whose plain failure ended the chain.
	if len(indices) != 2 || indices[0] != 1 || indices[1] != 2 {
		t.Errorf("retry attempt indices = %v, want [1 2]", indices)
	}
}

// A retry that lost its admission slot wrote nothing, so it is not "answered":
// the loop must fall through to the exhaustion path rather than return with
// an empty response.
func TestSettleLastCandidateRetry_BusyRetryIsNotAnswered(t *testing.T) {
	env := newTestProxyHandler(t)
	defer env.Upstream.Close()
	fn := func(http.ResponseWriter, *http.Request, *requestState, modelCandidate, int, int) candidateOutcome {
		return outcomeBusy
	}
	st := &requestState{startTime: time.Now(), overallDeadline: time.Now().Add(time.Hour), serverErrorRetried: true, logData: &requestLogData{}}
	if env.Handler.settleLastCandidateRetry(httptest.NewRecorder(), httptest.NewRequest("POST", "/", http.NoBody), st, modelCandidateForBreaker(uuid.New()), 1, fn, outcomeRetryServerError) {
		t.Error("a busy retry was reported as answered with nothing written")
	}
}

// The server-error backoff yields to the deadline: with less room than the
// backoff, the retry still runs, after the room that is left.
func TestRetryServerErrorCandidate_BackoffYieldsToDeadline(t *testing.T) {
	env := newTestProxyHandler(t)
	defer env.Upstream.Close()
	attempted := false
	fn := func(http.ResponseWriter, *http.Request, *requestState, modelCandidate, int, int) candidateOutcome {
		attempted = true
		return outcomeServed
	}
	st := &requestState{startTime: time.Now(), overallDeadline: time.Now().Add(300 * time.Millisecond), logData: &requestLogData{}}
	start := time.Now()
	if !env.Handler.retryServerErrorCandidate(httptest.NewRecorder(), httptest.NewRequest("POST", "/", http.NoBody), st, modelCandidateForBreaker(uuid.New()), 1, fn) {
		t.Error("a served retry was not reported as answered")
	}
	if !attempted {
		t.Fatal("the retry did not run inside the room that was left")
	}
	if elapsed := time.Since(start); elapsed >= 300*time.Millisecond {
		t.Errorf("the retry waited %v, past the deadline it had to yield to", elapsed)
	}
}

// The 5xx retry is offered only while the deadline holds the longest backoff
// plus a round, judged before the answer in hand is drained: a request too
// close to its deadline forwards the provider's answer instead of trading it
// for a retry that would find no time.
func TestDeferLastCandidateRetry_ServerErrorNeedsRoom(t *testing.T) {
	env := newTestProxyHandler(t)
	defer env.Upstream.Close()
	for _, tc := range []struct {
		name     string
		deadline time.Time
		want     bool
	}{
		{"no deadline set", time.Time{}, true},
		{"room for the backoff and a round", time.Now().Add(5 * time.Second), true},
		{"budget left but not the backoff", time.Now().Add(300 * time.Millisecond), false},
		{"spent", time.Now().Add(-time.Second), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logData, _ := env.Handler.newPendingRequestLog(httptest.NewRequest("POST", "/", http.NoBody), endpointTypeChat, "m", false)
			cand := modelCandidateForBreaker(uuid.New())
			logData.openAttemptRecord(0, cand, false, time.Now(), true)
			st := &requestState{overallDeadline: tc.deadline, logData: logData}
			if got := st.serverErrorRetryFits(); got != tc.want {
				t.Fatalf("serverErrorRetryFits() = %v, want %v", got, tc.want)
			}
			body := io.NopCloser(strings.NewReader(zaiNetworkFailureBody))
			resp := &http.Response{StatusCode: http.StatusInternalServerError, Body: body}
			outcome, ok := env.Handler.deferLastCandidateRetry(st, cand, resp, 0, rateLimitVerdict{})
			if ok != tc.want {
				t.Fatalf("retry offered = %v, want %v", ok, tc.want)
			}
			if ok {
				if outcome != outcomeRetryServerError || !st.serverErrorRetried || st.lastReqErr.Kind != KindProviderError {
					t.Errorf("offered retry = (%v, retried=%v, kind=%v), want the server-error outcome with the flag set", outcome, st.serverErrorRetried, st.lastReqErr.Kind)
				}
				// The failed attempt is closed on the trail with the provider's
				// sentence before the retry opens its own record.
				if got := logData.attempts; len(got) != 1 || got[0].Status != 500 || !strings.Contains(got[0].Detail, "Internal network failure") || logData.openAttempt != nil {
					t.Errorf("trail after the defer = %+v (open=%v), want the closed 500 with its detail", got, logData.openAttempt != nil)
				}
			}
			if !ok {
				if outcome != outcomeFailover || st.serverErrorRetried {
					t.Errorf("declined retry = (%v, retried=%v), want the failover zero value with the flag untouched", outcome, st.serverErrorRetried)
				}
				// The answer is still whole for the caller to forward.
				if rest, _ := io.ReadAll(body); string(rest) != zaiNetworkFailureBody {
					t.Errorf("body after a declined retry = %q, want untouched", rest)
				}
			}
		})
	}
}
