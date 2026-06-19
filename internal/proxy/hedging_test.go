package proxy

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/provider"
)

// ---------------------------------------------------------------------------
// runHedgedStreaming orchestrator tests.
//
// These drive the race with an injected fake probeFn (the same seam the real
// path uses), so winner selection, eager/staggered launching, loser
// cancellation, exhaustion, and client-disconnect handling are all exercised
// without real upstreams. The real begin+probe+breaker wiring lives in
// probeStreamingCandidate and is covered by the streaming integration tests.
// ---------------------------------------------------------------------------

// fakeProbeSpec scripts one candidate's behavior in a hedged race.
type fakeProbeSpec struct {
	delay     time.Duration // time before the probe resolves
	won       bool          // true = streamable first token
	reqErr    reqError      // failover cause when !won
	ignoreCtx bool          // when true, the probe does not return early on ctx cancel
}

// hedgeHarness records how runHedgedStreaming drove the probes and hands back a
// streamable response for any winner.
type hedgeHarness struct {
	mu     sync.Mutex
	specs  []fakeProbeSpec
	probed []int
	ctxs   map[int]context.Context
}

func newHedgeHarness(specs []fakeProbeSpec) *hedgeHarness {
	return &hedgeHarness{specs: specs, ctxs: map[int]context.Context{}}
}

func (hh *hedgeHarness) probe(ctx context.Context, _ *requestState, candidate modelCandidate, attempt int, _, _ time.Duration) hedgeResult {
	hh.mu.Lock()
	hh.probed = append(hh.probed, attempt)
	hh.ctxs[attempt] = ctx
	spec := hh.specs[attempt]
	hh.mu.Unlock()

	if spec.ignoreCtx {
		time.Sleep(spec.delay)
	} else {
		select {
		case <-time.After(spec.delay):
		case <-ctx.Done():
			// Cancelled by the orchestrator because another candidate won.
			return hedgeResult{idx: attempt, reqErr: reqError{Kind: KindClientDisconnect, Attempt: attempt, Provider: candidate.provider.Name}}
		}
	}
	if spec.won {
		return hedgeResult{
			idx:        attempt,
			won:        true,
			resp:       newStreamableResp(),
			trueTtftMs: 1,
		}
	}
	return hedgeResult{idx: attempt, reqErr: spec.reqErr}
}

func (hh *hedgeHarness) probedOrder() []int {
	hh.mu.Lock()
	defer hh.mu.Unlock()
	out := make([]int, len(hh.probed))
	copy(out, hh.probed)
	return out
}

// newStreamableResp builds a minimal SSE 200 response the winner path can stream.
func newStreamableResp() *http.Response {
	body := "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"}}]}\n\ndata: [DONE]\n\n"
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}

func hedgeCandidates(names ...string) []modelCandidate {
	cands := make([]modelCandidate, len(names))
	for i, n := range names {
		cands[i] = modelCandidate{
			model:    &model.Model{ModelID: "m-" + n},
			provider: &provider.Provider{ID: uuid.New(), Name: n},
		}
	}
	return cands
}

func newHedgeState(hedgeDelay time.Duration) (*requestState, *requestLogData) {
	logData := &requestLogData{
		modelID:        "test-model",
		streaming:      true,
		state:          "streaming",
		virtualKeyName: "test-key",
		virtualKeyID:   "00000000-0000-0000-0000-000000000001",
	}
	// No insertWg.Add and an empty logData.id: updateRequestLog skips (no row),
	// WaitForInsert returns immediately, so the winner path needs no DB row.
	st := &requestState{
		startTime:             time.Now(),
		reqModel:              "test-model",
		isStreaming:           true,
		isFailover:            true,
		circuitBreakerEnabled: false,
		hedgingEnabled:        true,
		hedgeDelay:            hedgeDelay,
		failoverTimeout:       time.Minute,
		overallDeadline:       time.Now().Add(time.Hour),
		logData:               logData,
	}
	return st, logData
}

// runHedge runs the orchestrator under the given client context and returns the
// recorder.
func runHedge(ctx context.Context, h *Handler, hh *hedgeHarness, st *requestState, candidates []modelCandidate) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody).WithContext(ctx)
	h.runHedgedStreaming(w, req, st, candidates, hh.probe)
	return w
}

// TestRunHedgedStreaming_FastWinnerSkipsBackup: when the first candidate produces
// a token before hedge_delay, the backup is never launched.
func TestRunHedgedStreaming_FastWinnerSkipsBackup(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandler(h)

	hh := newHedgeHarness([]fakeProbeSpec{
		{delay: 5 * time.Millisecond, won: true},
		{delay: 5 * time.Millisecond, won: true},
	})
	st, logData := newHedgeState(500 * time.Millisecond)
	w := runHedge(context.Background(), h, hh, st, hedgeCandidates("prov-A", "prov-B"))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if order := hh.probedOrder(); len(order) != 1 || order[0] != 0 {
		t.Errorf("backup should not launch when A wins fast; probed=%v", order)
	}
	if logData.providerName != "prov-A" {
		t.Errorf("winner identity should be prov-A, got %q", logData.providerName)
	}
}

// TestRunHedgedStreaming_SlowStartHedgesAndBackupWins: A is slow, so after
// hedge_delay B is launched in parallel, B wins, and A's context is cancelled.
func TestRunHedgedStreaming_SlowStartHedgesAndBackupWins(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandler(h)

	hh := newHedgeHarness([]fakeProbeSpec{
		{delay: 10 * time.Second}, // A hangs until cancelled
		{delay: 5 * time.Millisecond, won: true},
	})
	st, logData := newHedgeState(20 * time.Millisecond)
	w := runHedge(context.Background(), h, hh, st, hedgeCandidates("prov-A", "prov-B"))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	order := hh.probedOrder()
	if len(order) != 2 {
		t.Fatalf("expected both candidates probed, got %v", order)
	}
	if logData.providerName != "prov-B" {
		t.Errorf("winner should be prov-B, got %q", logData.providerName)
	}
	if hh.ctxs[0].Err() == nil {
		t.Error("losing candidate A's context should be cancelled after B wins")
	}
}

// TestRunHedgedStreaming_FailedAttemptLaunchesNextEagerly: a stalling A frees its
// slot, so B is launched immediately (before the hedge tick) and wins.
func TestRunHedgedStreaming_FailedAttemptLaunchesNextEagerly(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandler(h)

	hh := newHedgeHarness([]fakeProbeSpec{
		{delay: 5 * time.Millisecond, reqErr: reqError{Kind: KindProviderTimeout, Attempt: 0, Provider: "prov-A"}},
		{delay: 5 * time.Millisecond, won: true},
	})
	// Large hedge delay: only the eager-on-failure launch can bring B in.
	st, logData := newHedgeState(10 * time.Second)
	w := runHedge(context.Background(), h, hh, st, hedgeCandidates("prov-A", "prov-B"))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if order := hh.probedOrder(); len(order) != 2 {
		t.Fatalf("backup should launch eagerly after A fails; probed=%v", order)
	}
	if logData.providerName != "prov-B" {
		t.Errorf("winner should be prov-B, got %q", logData.providerName)
	}
}

// TestRunHedgedStreaming_AllStallExhausts: every candidate stalls, so the request
// fails with the terminal provider_timeout status (502).
func TestRunHedgedStreaming_AllStallExhausts(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandler(h)

	stallErr := reqError{Kind: KindProviderTimeout, Attempt: 0, Provider: "prov-A"}
	hh := newHedgeHarness([]fakeProbeSpec{
		{delay: 5 * time.Millisecond, reqErr: stallErr},
		{delay: 5 * time.Millisecond, reqErr: stallErr},
	})
	st, _ := newHedgeState(10 * time.Second)
	w := runHedge(context.Background(), h, hh, st, hedgeCandidates("prov-A", "prov-B"))

	if w.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 (provider stall exhausted), got %d", w.Code)
	}
	if st.lastReqErr.Kind != KindProviderTimeout {
		t.Errorf("terminal cause should be provider_timeout, got %s", st.lastReqErr.Kind)
	}
}

// TestRunHedgedStreaming_ClientDisconnect: the client connection drops mid-race
// with no prior provider stall, yielding a genuine 499.
func TestRunHedgedStreaming_ClientDisconnect(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandler(h)

	hh := newHedgeHarness([]fakeProbeSpec{
		{delay: 30 * time.Millisecond, ignoreCtx: true},
		{delay: 30 * time.Millisecond, ignoreCtx: true},
	})
	st, _ := newHedgeState(10 * time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // client gone before the first select

	w := runHedge(ctx, h, hh, st, hedgeCandidates("prov-A", "prov-B"))
	if w.Code != statusClientClosedRequest {
		t.Fatalf("expected 499 (client disconnect), got %d", w.Code)
	}
}

// TestRunHedgedStreaming_DisconnectPreservesProviderStall: a client/intermediary
// disconnect after a provider stall is recorded keeps the honest 502 rather than
// being relabeled a 499 (PR #258 classification, carried into the hedged path).
func TestRunHedgedStreaming_DisconnectPreservesProviderStall(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandler(h)

	hh := newHedgeHarness([]fakeProbeSpec{
		{delay: 30 * time.Millisecond, ignoreCtx: true},
	})
	st, _ := newHedgeState(10 * time.Second)
	// A provider stall was already recorded before the connection dropped.
	st.setReqErr(reqError{Kind: KindProviderTimeout, Attempt: 0, Provider: "prov-A"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	w := runHedge(ctx, h, hh, st, hedgeCandidates("prov-A"))
	if w.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 (provider stall preserved on disconnect), got %d", w.Code)
	}
}

// TestRunHedgedStreaming_OverallDeadline: the overall request deadline expiring
// mid-race ends the request with the failover-timeout status (504).
func TestRunHedgedStreaming_OverallDeadline(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandler(h)

	hh := newHedgeHarness([]fakeProbeSpec{
		{delay: 50 * time.Millisecond, ignoreCtx: true},
		{delay: 50 * time.Millisecond, ignoreCtx: true},
	})
	st, _ := newHedgeState(10 * time.Second)
	st.overallDeadline = time.Now().Add(-time.Second) // already past
	w := runHedge(context.Background(), h, hh, st, hedgeCandidates("prov-A", "prov-B"))

	if w.Code != http.StatusGatewayTimeout {
		t.Fatalf("expected 504 (failover timeout), got %d", w.Code)
	}
	if st.lastReqErr.Kind != KindFailoverTimeout {
		t.Errorf("terminal cause should be failover_timeout, got %s", st.lastReqErr.Kind)
	}
}

// TestRunHedgedStreaming_RealProbesConcurrent drives the orchestrator with the
// REAL probeStreamingCandidate against multiple live upstreams so several probes
// run in parallel while the orchestrator records failed results. Run under -race
// it locks in the per-attempt requestState snapshot (no shared-state data race).
func TestRunHedgedStreaming_RealProbesConcurrent(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandler(h)

	sse := "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"x\"}}]}\n\ndata: [DONE]\n\n"
	// c0 fails fast (records a result while c1/c2 are in flight).
	failFast := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(10 * time.Millisecond)
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer failFast.Close()
	mkStream := func(delay time.Duration) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			time.Sleep(delay)
			_, _ = io.WriteString(w, sse)
		}))
	}
	slow := mkStream(120 * time.Millisecond)
	defer slow.Close()
	fast := mkStream(40 * time.Millisecond)
	defer fast.Close()

	st, _ := newHedgeState(25 * time.Millisecond)
	st.reqModel = "orig-model"
	st.bodyBytes = []byte(`{"model":"orig-model","messages":[{"role":"user","content":"hi"}],"stream":true}`)
	cands := []modelCandidate{
		{model: &model.Model{ModelID: "m0"}, provider: &provider.Provider{ID: uuid.New(), Name: "fail", BaseURL: failFast.URL}},
		{model: &model.Model{ModelID: "m1"}, provider: &provider.Provider{ID: uuid.New(), Name: "slow", BaseURL: slow.URL}},
		{model: &model.Model{ModelID: "m2"}, provider: &provider.Provider{ID: uuid.New(), Name: "fast", BaseURL: fast.URL}},
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
	h.runHedgedStreaming(w, req, st, cands, h.probeStreamingCandidate)

	if w.Code != http.StatusOK {
		t.Fatalf("expected a winning 200 from a healthy member, got %d", w.Code)
	}
}

// closeTrackingBody is an io.ReadCloser that records whether Close was called.
type closeTrackingBody struct {
	closed bool
}

func (b *closeTrackingBody) Read(p []byte) (int, error) { return 0, io.EOF }
func (b *closeTrackingBody) Close() error               { b.closed = true; return nil }

// TestCommitHedgeWin_CancelledDowngrades verifies that a probe which produced a
// first token but whose context was already cancelled (it lost the race) closes
// its body and is downgraded to a non-win, so no runner-up connection leaks.
func TestCommitHedgeWin_CancelledDowngrades(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	body := &closeTrackingBody{}
	resp := &http.Response{StatusCode: http.StatusOK, Body: body, Header: make(http.Header)}
	cand := modelCandidate{provider: &provider.Provider{ID: uuid.New(), Name: "prov-A"}}

	res := commitHedgeWin(ctx, hedgeResult{idx: 1}, resp, nil, 0, cand)
	if res.won {
		t.Error("a cancelled attempt must not win")
	}
	if !body.closed {
		t.Error("the runner-up body must be closed on downgrade")
	}
	if res.reqErr.Kind != KindClientDisconnect {
		t.Errorf("expected client_disconnect downgrade, got %s", res.reqErr.Kind)
	}

	// Sanity: an uncancelled context keeps the win and the open body.
	body2 := &closeTrackingBody{}
	resp2 := &http.Response{StatusCode: http.StatusOK, Body: body2, Header: make(http.Header)}
	won := commitHedgeWin(context.Background(), hedgeResult{idx: 0}, resp2, nil, 0, cand)
	if !won.won || won.resp == nil {
		t.Error("an uncancelled attempt should win and keep its body")
	}
	if body2.closed {
		t.Error("the winner body must stay open for streaming")
	}
}

// TestDrainHedgeResults_ClosesRunnerUpBodies verifies the background drain closes
// the live body a runner-up win carries while skipping failover results (nil body).
func TestDrainHedgeResults_ClosesRunnerUpBodies(t *testing.T) {
	ch := make(chan hedgeResult, 2)
	runnerUp := &closeTrackingBody{}
	ch <- hedgeResult{idx: 1, won: true, resp: &http.Response{Body: runnerUp, Header: make(http.Header)}}
	ch <- hedgeResult{idx: 2, reqErr: reqError{Kind: KindProviderError}} // no body

	drainHedgeResults(ch, 2)

	if !runnerUp.closed {
		t.Error("a runner-up win's body must be closed by the drain")
	}
}

// TestFailHedgeDisconnect_PreservesStallOverLaterError covers I3: when an earlier
// candidate stalled but a later candidate failed with a non-stall error, a client
// disconnect must still be classified as the provider stall (502), not 499.
func TestFailHedgeDisconnect_PreservesStallOverLaterError(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandler(h)
	st, _ := newHedgeState(time.Second)
	// Most recent recorded result is a non-stall provider error...
	st.setReqErr(reqError{Kind: KindProviderError, Attempt: 1, Provider: "prov-B"})
	// ...but an earlier candidate stalled (preserved separately by the loop).
	stall := reqError{Kind: KindProviderTimeout, Attempt: 0, Provider: "prov-A"}

	w := httptest.NewRecorder()
	h.failHedgeDisconnect(w, st, 2, stall)
	if w.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 (stall preserved over later error), got %d", w.Code)
	}
}

// TestFailHedgeDisconnect_NoStallIsClientDisconnect verifies the genuine-hangup
// path: no provider stall anywhere means a disconnect is a real 499.
func TestFailHedgeDisconnect_NoStallIsClientDisconnect(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandler(h)
	st, _ := newHedgeState(time.Second)
	st.setReqErr(reqError{Kind: KindProviderError, Attempt: 0, Provider: "prov-A"})

	w := httptest.NewRecorder()
	h.failHedgeDisconnect(w, st, 1, reqError{}) // no stall recorded
	if w.Code != statusClientClosedRequest {
		t.Fatalf("expected 499 (genuine client disconnect), got %d", w.Code)
	}
}

// probeStateForServer builds a requestState + candidate pointed at an httptest
// upstream for direct probeStreamingCandidate tests.
func probeStateForServer(serverURL string) (*requestState, modelCandidate) {
	st := &requestState{
		startTime:             time.Now(),
		reqModel:              "orig-model",
		isStreaming:           true,
		bodyBytes:             []byte(`{"model":"orig-model","messages":[{"role":"user","content":"hi"}],"stream":true}`),
		circuitBreakerEnabled: false,
		failoverTimeout:       30 * time.Second,
		logData:               &requestLogData{modelID: "orig-model", streaming: true},
	}
	cand := modelCandidate{
		model:    &model.Model{ModelID: "upstream-model"},
		provider: &provider.Provider{ID: uuid.New(), Name: "prov-A", BaseURL: serverURL},
		apiKey:   "sk-test",
	}
	return st, cand
}

// TestProbeStreamingCandidate covers the real begin+probe path (no client write):
// a 200 with a first token wins, a silent 200 past the probe window is a
// provider_timeout, and a non-200 drops as a provider error.
func TestProbeStreamingCandidate(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandler(h)

	t.Run("first token wins", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"}}]}\n\ndata: [DONE]\n\n")
		}))
		defer srv.Close()

		st, cand := probeStateForServer(srv.URL)
		res := h.probeStreamingCandidate(context.Background(), st, cand, 0, 5*time.Second, 30*time.Second)
		if !res.won {
			t.Fatalf("expected a win, got reqErr=%+v", res.reqErr)
		}
		if res.resp != nil {
			_ = res.resp.Body.Close()
		}
	})

	t.Run("silent 200 is provider_timeout", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			time.Sleep(400 * time.Millisecond) // never sends a token
		}))
		defer srv.Close()

		st, cand := probeStateForServer(srv.URL)
		res := h.probeStreamingCandidate(context.Background(), st, cand, 0, 100*time.Millisecond, 30*time.Millisecond)
		if res.won {
			t.Fatal("a silent stream must not win")
		}
		if res.reqErr.Kind != KindProviderTimeout {
			t.Errorf("expected provider_timeout, got %s", res.reqErr.Kind)
		}
	})

	t.Run("ttft disabled commits immediately", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, "data: {\"choices\":[]}\n\ndata: [DONE]\n\n")
		}))
		defer srv.Close()

		st, cand := probeStateForServer(srv.URL)
		// ttftTimeout == 0 disables the probe: a 200 is an immediate win.
		res := h.probeStreamingCandidate(context.Background(), st, cand, 0, 0, 30*time.Second)
		if !res.won {
			t.Fatalf("expected immediate win with probe disabled, got reqErr=%+v", res.reqErr)
		}
		if res.resp != nil {
			_ = res.resp.Body.Close()
		}
	})

	t.Run("non-200 drops as provider error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "boom", http.StatusInternalServerError)
		}))
		defer srv.Close()

		st, cand := probeStateForServer(srv.URL)
		res := h.probeStreamingCandidate(context.Background(), st, cand, 0, 5*time.Second, 30*time.Second)
		if res.won {
			t.Fatal("a 500 must not win")
		}
		if res.reqErr.Kind != KindProviderError {
			t.Errorf("expected provider_error, got %s", res.reqErr.Kind)
		}
	})
}
