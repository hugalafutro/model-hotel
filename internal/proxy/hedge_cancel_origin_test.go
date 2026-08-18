package proxy

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hugalafutro/model-hotel/internal/ctxkeys"
)

// attrCapture records logged messages together with their attributes, which the
// message-only captureHandler in deprecation_cache_test.go cannot do.
type attrCapture struct {
	mu      sync.Mutex
	records []capturedRecord
}

type capturedRecord struct {
	level slog.Level
	msg   string
	attrs map[string]string
}

func (c *attrCapture) Enabled(context.Context, slog.Level) bool { return true }

func (c *attrCapture) Handle(_ context.Context, r slog.Record) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	rec := capturedRecord{level: r.Level, msg: r.Message, attrs: map[string]string{}}
	r.Attrs(func(a slog.Attr) bool {
		rec.attrs[a.Key] = a.Value.String()
		return true
	})
	c.records = append(c.records, rec)
	return nil
}

func (c *attrCapture) WithAttrs([]slog.Attr) slog.Handler { return c }
func (c *attrCapture) WithGroup(string) slog.Handler      { return c }

func (c *attrCapture) find(msg string) []capturedRecord {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []capturedRecord
	for _, r := range c.records {
		if strings.Contains(r.msg, msg) {
			out = append(out, r)
		}
	}
	return out
}

// captureProxyLogs redirects slog for the duration of a test.
func captureProxyLogs(t *testing.T) *attrCapture {
	t.Helper()
	capture := &attrCapture{}
	original := slog.Default()
	t.Cleanup(func() { slog.SetDefault(original) })
	slog.SetDefault(slog.New(capture))
	return capture
}

// TestResolveCancelOrigin covers the classification that decides whether a
// cancelled upstream attempt is reported as the client hanging up. A hedge
// loser the orchestrator abandoned must never be called a client disconnect:
// the client is still connected and is served by the winning attempt.
func TestResolveCancelOrigin(t *testing.T) {
	supersededCtx := func(v bool) context.Context {
		b := &atomic.Bool{}
		b.Store(v)
		return context.WithValue(context.Background(), ctxkeys.HedgeSupersededKey, b)
	}

	tests := []struct {
		name string
		ctx  context.Context
		err  error
		want string
	}{
		{
			name: "hedge loser cancelled by the orchestrator",
			ctx:  supersededCtx(true),
			err:  context.Canceled,
			want: "hedge_superseded",
		},
		{
			name: "hedged attempt still live when the client hangs up",
			ctx:  supersededCtx(false),
			err:  context.Canceled,
			want: "client_disconnect",
		},
		{
			name: "sequential path has no hedge flag at all",
			ctx:  context.Background(),
			err:  context.Canceled,
			want: "client_disconnect",
		},
		{
			name: "deadline on a hedged attempt is still the failover timeout",
			ctx:  context.WithValue(supersededCtx(true), ctxkeys.CancelOriginKey, "failover_timeout"),
			err:  context.DeadlineExceeded,
			want: "failover_timeout",
		},
		{
			name: "deadline reads the retry origin",
			ctx:  context.WithValue(context.Background(), ctxkeys.CancelOriginKey, "retry_timeout"),
			err:  context.DeadlineExceeded,
			want: "retry_timeout",
		},
		{
			name: "deadline with no recorded origin keeps the old default",
			ctx:  context.Background(),
			err:  context.DeadlineExceeded,
			want: "client_disconnect",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveCancelOrigin(tt.ctx, tt.err); got != tt.want {
				t.Errorf("resolveCancelOrigin() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestCancelOriginToKind_HedgeSuperseded pins the origin→kind mapping so the
// request log's error_kind carries the distinction too, not just the log line.
func TestCancelOriginToKind_HedgeSuperseded(t *testing.T) {
	if got := cancelOriginToKind("hedge_superseded"); got != KindHedgeSuperseded {
		t.Errorf("cancelOriginToKind(hedge_superseded) = %v, want %v", got, KindHedgeSuperseded)
	}
}

// TestHedgeAbandonKind covers the same distinction on the runner-up path, where
// a candidate wins its own probe after the orchestrator already picked a winner.
func TestHedgeAbandonKind(t *testing.T) {
	b := &atomic.Bool{}
	ctx := context.WithValue(context.Background(), ctxkeys.HedgeSupersededKey, b)
	if got := hedgeAbandonKind(ctx); got != KindClientDisconnect {
		t.Errorf("unflagged attempt should read as client disconnect, got %v", got)
	}
	b.Store(true)
	if got := hedgeAbandonKind(ctx); got != KindHedgeSuperseded {
		t.Errorf("flagged attempt should read as superseded, got %v", got)
	}
	if got := hedgeAbandonKind(context.Background()); got != KindClientDisconnect {
		t.Errorf("context without the flag should read as client disconnect, got %v", got)
	}
}

// TestHedgeSupersededRendersWithoutBlamingTheClient guards the operator-facing
// text: the whole point of the new kind is that it does not say "client
// disconnected" about a request the client received in full.
func TestHedgeSupersededRendersWithoutBlamingTheClient(t *testing.T) {
	e := reqError{Kind: KindHedgeSuperseded, Attempt: 2, Provider: "Ollama Cloud"}
	got := e.render()
	if strings.Contains(strings.ToLower(got), "client disconnected") {
		t.Errorf("superseded attempt must not be rendered as a client disconnect: %q", got)
	}
	if !strings.Contains(got, "Ollama Cloud") || !strings.Contains(got, "attempt 3") {
		t.Errorf("render() should name the provider and 1-based attempt, got %q", got)
	}
	if got := humanReadableCancelOrigin("hedge_superseded"); got == "hedge_superseded" {
		t.Error("humanReadableCancelOrigin should translate hedge_superseded into prose")
	}
}

// TestRunHedgedStreaming_LoserIsSupersededNotDisconnected runs the real
// orchestrator: A hangs, B wins, A is cancelled. The cancellation A observes
// must classify as our own abandonment, because the client is still connected
// and receives B's stream.
func TestRunHedgedStreaming_LoserIsSupersededNotDisconnected(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandler(h)

	hh := newHedgeHarness([]fakeProbeSpec{
		{delay: 10 * time.Second}, // A hangs until cancelled
		{delay: 5 * time.Millisecond, won: true},
	})
	st, _ := newHedgeState(20 * time.Millisecond)
	w := runHedge(context.Background(), h, hh, st, hedgeCandidates("prov-A", "prov-B"))

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for the client, got %d", w.Code)
	}
	loserCtx := hh.ctxs[0]
	if loserCtx.Err() == nil {
		t.Fatal("loser's context should be cancelled after B wins")
	}
	if got := resolveCancelOrigin(loserCtx, loserCtx.Err()); got != "hedge_superseded" {
		t.Errorf("loser cancellation classified as %q, want hedge_superseded "+
			"(the client never disconnected — it got a 200 from prov-B)", got)
	}
	if got := hedgeAbandonKind(loserCtx); got != KindHedgeSuperseded {
		t.Errorf("hedgeAbandonKind = %v, want KindHedgeSuperseded", got)
	}
}

// TestRunHedgedStreaming_WinnerLogsTheStoredAttemptNumber pins the hedge-winner
// log line to the same 0-based attempt the request log stores, so the two lines
// describing one request cannot disagree.
func TestRunHedgedStreaming_WinnerLogsTheStoredAttemptNumber(t *testing.T) {
	logs := captureProxyLogs(t)
	h := newIntegrationHandler()
	defer stopUnitHandler(h)

	hh := newHedgeHarness([]fakeProbeSpec{
		{delay: 10 * time.Second},
		{delay: 5 * time.Millisecond, won: true},
	})
	st, logData := newHedgeState(20 * time.Millisecond)
	if w := runHedge(context.Background(), h, hh, st, hedgeCandidates("prov-A", "prov-B")); w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	winner := logs.find("hedge winner")
	if len(winner) == 0 {
		t.Fatal("expected a hedge winner log line")
	}
	// prov-B is candidate index 1, and that is what lands in failover_attempt.
	if got := winner[0].attrs["attempt"]; got != "1" {
		t.Errorf("hedge winner logged attempt=%s, want 1 (the stored failover_attempt)", got)
	}
	if logData.failoverAttempt != 1 {
		t.Fatalf("test assumption broken: stored failoverAttempt = %d, want 1", logData.failoverAttempt)
	}
	for _, r := range logs.find("streaming finished") {
		if got := r.attrs["attempt"]; got != "1" {
			t.Errorf("streaming finished logged attempt=%s, want 1 — the two lines "+
				"describing this request disagree", got)
		}
	}
}

// TestProbeStreamingCandidate_LogsTheBreakerFailureItRecords covers the gap that
// made a live breaker trip unattributable: a hedged probe failure recorded a
// circuit-breaker failure with no log line anywhere, so the only evidence a
// provider had failed was the breaker's own state transition — which says
// nothing about the cause.
func TestProbeStreamingCandidate_LogsTheBreakerFailureItRecords(t *testing.T) {
	logs := captureProxyLogs(t)
	h := newIntegrationHandler()
	defer stopUnitHandler(h)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		time.Sleep(400 * time.Millisecond) // accepts the connection, never sends a token
	}))
	defer srv.Close()

	st, cand := probeStateForServer(srv.URL)
	st.circuitBreakerEnabled = true
	res := h.probeStreamingCandidate(context.Background(), st, cand, 0, 100*time.Millisecond, 30*time.Millisecond)
	if res.won {
		t.Fatal("a silent stream must not win")
	}

	recorded := logs.find("recording circuit breaker failure")
	if len(recorded) == 0 {
		t.Fatal("a breaker failure was recorded with no log line explaining why")
	}
	got := recorded[0]
	if got.level < slog.LevelWarn {
		t.Errorf("breaker-failure log is level %v; below Warn it is invisible at the "+
			"default production log level", got.level)
	}
	if got.attrs["provider"] != "prov-A" {
		t.Errorf("log should name the provider, got %q", got.attrs["provider"])
	}
	if got.attrs["reason"] == "" {
		t.Error("log should carry the reason the failure was recorded")
	}
}

// TestRunHedgedStreaming_DeadlineDoesNotLookLikeASupersededLoss guards the
// inverse of the bug this file exists for. The deferred safety-net cancellation
// runs on every return path, so flagging attempts there unconditionally would
// relabel the overall-deadline and client-disconnect causes as a hedge loss —
// reintroducing the same misattribution with the sign flipped.
func TestRunHedgedStreaming_DeadlineDoesNotLookLikeASupersededLoss(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandler(h)

	hh := newHedgeHarness([]fakeProbeSpec{
		{delay: 10 * time.Second}, // nothing ever wins
		{delay: 10 * time.Second},
	})
	st, _ := newHedgeState(20 * time.Millisecond)
	st.overallDeadline = time.Now().Add(80 * time.Millisecond)
	runHedge(context.Background(), h, hh, st, hedgeCandidates("prov-A", "prov-B"))

	ctx := hh.ctxs[0]
	if ctx.Err() == nil {
		t.Fatal("attempts should be cancelled once the overall deadline passes")
	}
	if got := resolveCancelOrigin(ctx, context.Canceled); got == "hedge_superseded" {
		t.Error("an attempt cancelled at the overall deadline was not superseded by " +
			"anything — reporting it as a hedge loss hides the real cause")
	}
}

// TestRunHedgedStreaming_ClientHangupIsNotRelabelled is the same guard for the
// disconnect path: the client really did go away, and the safety-net
// cancellation must not overwrite that with our own abandonment.
func TestRunHedgedStreaming_ClientHangupIsNotRelabelled(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandler(h)

	hh := newHedgeHarness([]fakeProbeSpec{
		{delay: 10 * time.Second},
		{delay: 10 * time.Second},
	})
	st, _ := newHedgeState(20 * time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(60 * time.Millisecond)
		cancel() // the client hangs up mid-race
	}()
	runHedge(ctx, h, hh, st, hedgeCandidates("prov-A", "prov-B"))
	cancel()

	attemptCtx := hh.ctxs[0]
	if got := resolveCancelOrigin(attemptCtx, context.Canceled); got != "client_disconnect" {
		t.Errorf("client hangup classified as %q, want client_disconnect", got)
	}
}
