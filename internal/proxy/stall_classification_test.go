package proxy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/provider"
)

// TestClassifyProbeFailure covers the zero-token TTFT probe-failure decision:
// when it is a provider stall (recorded against the breaker, failover-eligible)
// versus a genuinely fast client cancel (not charged to the provider), and when
// the reverse-proxy hint is attached.
func TestClassifyProbeFailure(t *testing.T) {
	const (
		stall = 30 * time.Second
		ttft  = 120 * time.Second
	)
	cases := []struct {
		name       string
		clientGone bool
		elapsed    time.Duration
		wantKind   ErrorKind
		wantRecord bool
		wantHint   bool
	}{
		// Our own TTFT timer fired (parent context still alive): always a
		// provider fault, regardless of elapsed.
		{"own TTFT fired", false, ttft, KindProviderTimeout, true, false},
		{"own timer, short elapsed still provider fault", false, time.Millisecond, KindProviderTimeout, true, false},
		// Downstream closed the connection, but only after the provider stayed
		// silent past the stall floor: provider stall + reverse-proxy hint.
		{"downstream close after stall floor", true, 90 * time.Second, KindProviderTimeout, true, true},
		{"downstream close exactly at stall floor", true, stall, KindProviderTimeout, true, true},
		// Fast downstream close with zero tokens: genuine client cancel.
		{"fast client cancel before stall floor", true, 3 * time.Second, KindClientDisconnect, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			re, rec := classifyProbeFailure("prov-x", "TTFT probe read error: boom", tc.clientGone, tc.elapsed, stall, ttft, 2)
			if re.Kind != tc.wantKind {
				t.Errorf("kind: got %s want %s", re.Kind, tc.wantKind)
			}
			if rec != tc.wantRecord {
				t.Errorf("recordFailure: got %v want %v", rec, tc.wantRecord)
			}
			if (re.Hint != "") != tc.wantHint {
				t.Errorf("hint presence: got %q want hint=%v", re.Hint, tc.wantHint)
			}
			if re.Attempt != 2 || re.Provider != "prov-x" || re.Underlying != "TTFT probe read error: boom" {
				t.Errorf("metadata not propagated: %+v", re)
			}
		})
	}
}

// TestClassifyProbeFailure_HintContent asserts the reverse-proxy hint names the
// provider, the cause, the elapsed time, the TTFT timeout, and the ttft_timeout
// knob so the dashboard message is self-diagnosing.
func TestClassifyProbeFailure_HintContent(t *testing.T) {
	re, _ := classifyProbeFailure("zai", "x", true, 90*time.Second, 30*time.Second, 120*time.Second, 0)
	for _, want := range []string{"zai", "reverse proxy", "90s", "2m0s", "ttft_timeout"} {
		if !strings.Contains(re.Hint, want) {
			t.Errorf("hint missing %q: %s", want, re.Hint)
		}
	}
	// The hint must never read as a client disconnect.
	if strings.Contains(strings.ToLower(re.Hint), "client disconnect") {
		t.Errorf("hint should not blame the client: %s", re.Hint)
	}
}

// TestReqErrorRender_ProviderTimeoutHint verifies the Hint is appended to the
// rendered provider_timeout message and that the terminal status stays 502.
func TestReqErrorRender_ProviderTimeoutHint(t *testing.T) {
	e := reqError{Kind: KindProviderTimeout, Attempt: 0, Provider: "zai", Hint: "an upstream reverse proxy likely closed the idle connection"}
	got := e.render()
	if !strings.Contains(got, "did not return a response in time") {
		t.Errorf("missing base message: %s", got)
	}
	if !strings.Contains(got, "reverse proxy") {
		t.Errorf("hint not rendered: %s", got)
	}
	if e.terminalStatus() != http.StatusBadGateway {
		t.Errorf("expected 502, got %d", e.terminalStatus())
	}
	// Without a hint the base message renders unchanged.
	plain := reqError{Kind: KindProviderTimeout, Attempt: 0, Provider: "zai"}.render()
	if strings.Contains(plain, "reverse proxy") {
		t.Errorf("plain provider_timeout should carry no hint: %s", plain)
	}
}

// runBackoffLoopWithPriorError drives runFailoverLoop with a pre-cancelled
// request context so the second candidate hits the backoff disconnect branch,
// after a fake first attempt recorded priorKind. It returns the HTTP status the
// loop wrote and the terminal reqError kind.
func runBackoffLoopWithPriorError(t *testing.T, priorKind ErrorKind) (int, ErrorKind) {
	t.Helper()
	h := newIntegrationHandler()
	defer stopUnitHandler(h)

	logData := &requestLogData{
		id:             uuid.New().String(),
		modelID:        "test-model",
		providerName:   "prov-A",
		streaming:      true,
		virtualKeyName: "test-key",
		virtualKeyID:   "00000000-0000-0000-0000-000000000001",
		state:          "streaming",
	}
	h.insertRequestLogAsync(logData)
	time.Sleep(100 * time.Millisecond)

	st := &requestState{
		startTime:             time.Now(),
		reqModel:              "test-model",
		isStreaming:           true,
		isFailover:            true,
		circuitBreakerEnabled: true,
		overallDeadline:       time.Now().Add(time.Hour),
		logData:               logData,
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // dead before the attempt-1 backoff select
	req := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody).WithContext(ctx)
	w := httptest.NewRecorder()

	candidates := []modelCandidate{
		{provider: &provider.Provider{ID: uuid.New(), Name: "prov-A"}},
		{provider: &provider.Provider{ID: uuid.New(), Name: "prov-B"}},
	}

	fakeAttempt := func(_ http.ResponseWriter, _ *http.Request, st *requestState, c modelCandidate, attempt, _ int) candidateOutcome {
		st.setReqErr(reqError{Kind: priorKind, Attempt: attempt, Provider: c.provider.Name, Underlying: "prior"})
		return outcomeFailover
	}

	h.runFailoverLoop(w, req, st, candidates, fakeAttempt)
	return w.Code, st.lastReqErr.Kind
}

// TestRunFailoverLoop_BackoffPreservesProviderStall verifies that when the prior
// attempt was a zero-token provider stall, a context cancellation during the
// backoff is reported as provider_timeout/502, NOT mislabeled client_disconnect.
// This is the exact site where the user's 499 was previously stamped.
func TestRunFailoverLoop_BackoffPreservesProviderStall(t *testing.T) {
	status, kind := runBackoffLoopWithPriorError(t, KindProviderTimeout)
	if status != http.StatusBadGateway {
		t.Errorf("expected 502 (provider stall preserved), got %d", status)
	}
	if kind != KindProviderTimeout {
		t.Errorf("expected lastReqErr to remain provider_timeout, got %s", kind)
	}
}

// TestRunFailoverLoop_BackoffClientDisconnectForNonStall verifies the existing
// behavior is unchanged when the prior cause was NOT a provider stall: a context
// cancellation during backoff is still a genuine client disconnect (499).
func TestRunFailoverLoop_BackoffClientDisconnectForNonStall(t *testing.T) {
	status, kind := runBackoffLoopWithPriorError(t, KindProviderError)
	if status != statusClientClosedRequest {
		t.Errorf("expected 499 (genuine client disconnect), got %d", status)
	}
	if kind != KindClientDisconnect {
		t.Errorf("expected lastReqErr client_disconnect, got %s", kind)
	}
}
