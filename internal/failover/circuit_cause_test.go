package failover

import (
	"context"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/events"
)

// lastVerdict reads a circuit's remembered verdict straight off the ledger.
func lastVerdict(t *testing.T, cb *CircuitBreaker, id uuid.UUID, model string) (string, int, time.Time) {
	t.Helper()
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	c, ok := cb.circuits[id.String()][model]
	if !ok {
		t.Fatalf("no circuit tracked for %s/%s", id, model)
	}
	return c.lastCause, c.lastStatus, c.lastAt
}

func TestUpstreamStatus(t *testing.T) {
	if got := UpstreamStatus(503, ""); got.Status != 503 || got.Reason != "upstream status 503" {
		t.Errorf("UpstreamStatus(503) = %+v", got)
	}
	if got := UpstreamStatus(429, "saturated"); got.Status != 429 || got.Reason != "upstream status 429 (saturated)" {
		t.Errorf("UpstreamStatus(429, saturated) = %+v", got)
	}
}

// Every verdict site leaves its cause on the circuit: what the breaker saw,
// the status behind it, and when. This is the memory the detail endpoint and
// the open event read, so a site that forgot to stamp would show an operator
// the PREVIOUS verdict under the current state.
func TestCircuitRemembersItsLastVerdict(t *testing.T) {
	cb := newTestCB(3, time.Minute)
	id := uuid.New()
	before := time.Now()

	cb.RecordFailure(id, "p", "m", UpstreamStatus(503, ""))
	cause, status, at := lastVerdict(t, cb, id, "m")
	if cause != "upstream status 503" || status != 503 || at.Before(before) {
		t.Errorf("after a 503 charge: cause=%q status=%d at=%v", cause, status, at)
	}

	cb.RecordSuccess(id, "p", "m")
	if cause, status, _ := lastVerdict(t, cb, id, "m"); cause != causeSuccess || status != 0 {
		t.Errorf("after a success: cause=%q status=%d", cause, status)
	}

	cb.RecordAlive(id, "p", "m", 400)
	if cause, status, _ := lastVerdict(t, cb, id, "m"); cause != "upstream status 400 (alive)" || status != 400 {
		t.Errorf("after a 400: cause=%q status=%d", cause, status)
	}

	cb.RecordRateLimited(id, "p", "m", UpstreamStatus(429, "unrecognised"))
	if cause, status, _ := lastVerdict(t, cb, id, "m"); cause != "upstream status 429 (unrecognised)" || status != 429 {
		t.Errorf("after an unrecognised 429: cause=%q status=%d", cause, status)
	}

	cb.RecordFailure(id, "p", "m", Cause{Reason: "upstream request failed"})
	if cause, status, _ := lastVerdict(t, cb, id, "m"); cause != "upstream request failed" || status != 0 {
		t.Errorf("after a connection failure: cause=%q status=%d, want no status", cause, status)
	}

	cb.RecordExhausted(id, "p", "m2", 0)
	if cause, status, _ := lastVerdict(t, cb, id, "m2"); cause != "upstream status 429 (exhausted)" || status != 429 {
		t.Errorf("after an exhausted 429: cause=%q status=%d", cause, status)
	}
}

// A saturated 429 is remembered without being charged or credited: the
// circuit's counters and state are exactly what they were, only the verdict
// changes. That is what lets a status row say "busy" about a closed circuit.
func TestRecordSaturated_RemembersWithoutCharging(t *testing.T) {
	cb := newTestCB(3, time.Minute)
	id := uuid.New()
	cb.RecordFailure(id, "p", "m", UpstreamStatus(503, ""))
	cb.RecordFailure(id, "p", "m", UpstreamStatus(503, ""))

	cb.RecordSaturated(id, "m")

	cause, status, _ := lastVerdict(t, cb, id, "m")
	if cause != "upstream status 429 (saturated)" || status != 429 {
		t.Errorf("cause=%q status=%d", cause, status)
	}
	if got := cb.GetState(id, "m"); got != StateClosed {
		t.Errorf("state = %v, want closed: a saturated 429 must not charge", got)
	}
	cb.mu.RLock()
	fails := cb.circuits[id.String()]["m"].consecutiveFails
	cb.mu.RUnlock()
	if fails != 2 {
		t.Errorf("consecutiveFails = %d, want the 2 earlier charges untouched (neither charged nor reset)", fails)
	}
	// And a fresh pair gets a circuit, so the verdict has somewhere to live.
	other := uuid.New()
	cb.RecordSaturated(other, "m")
	if cause, _, _ := lastVerdict(t, cb, other, "m"); cause != "upstream status 429 (saturated)" {
		t.Errorf("untracked pair after RecordSaturated: cause=%q", cause)
	}
	// Nothing landed, so nothing moves the eviction clock: a circuit that
	// exists only for this stamp ranks first for eviction.
	cb.mu.RLock()
	stamped := cb.circuits[other.String()]["m"].lastCharged
	cb.mu.RUnlock()
	if !stamped.IsZero() {
		t.Errorf("lastCharged = %v after a saturated stamp, want untouched", stamped)
	}
}

// The quota poller's retarget and the two releases are transitions with no
// request behind them; each records itself as the verdict and keeps the
// status of the response that opened the circuit.
func TestQuotaPinTransitionsRecordTheirCause(t *testing.T) {
	cb := newTestCB(1, time.Minute)
	id := uuid.New()
	cb.RecordFailure(id, "p", "m", UpstreamStatus(429, "unrecognised"))
	if cb.GetState(id, "m") != StateOpen {
		t.Fatal("setup: circuit not open")
	}

	if n := cb.ApplyQuotaPins(map[uuid.UUID]time.Time{id: time.Now().Add(2 * time.Hour)}); n != 1 {
		t.Fatalf("retargeted %d, want 1", n)
	}
	if cause, status, _ := lastVerdict(t, cb, id, "m"); cause != causePinRetargeted || status != 429 {
		t.Errorf("after retarget: cause=%q status=%d", cause, status)
	}

	if n := cb.ReleaseQuotaPins(map[uuid.UUID]struct{}{id: {}}); n != 1 {
		t.Fatalf("released %d, want 1", n)
	}
	if cause, status, _ := lastVerdict(t, cb, id, "m"); cause != causePinReleasedQuota || status != 429 {
		t.Errorf("after release: cause=%q status=%d", cause, status)
	}

	cb.ApplyQuotaPins(map[uuid.UUID]time.Time{id: time.Now().Add(2 * time.Hour)})
	if n := cb.ReleaseAllQuotaPins(); n != 1 {
		t.Fatalf("released all %d, want 1", n)
	}
	if cause, _, _ := lastVerdict(t, cb, id, "m"); cause != causePinReleasedOff {
		t.Errorf("after release-all: cause=%q", cause)
	}
}

// The detail row lists every circuit it is built from, sorted, each with its
// state and verdict, and the open ones are exactly open_models: two readings
// of the same ledger must never disagree.
func TestStatus_CircuitsMatchOpenModels(t *testing.T) {
	cb := newTestCB(2, time.Minute)
	id := uuid.New()
	cb.RecordFailure(id, "p", "b-open", UpstreamStatus(503, ""))
	cb.RecordFailure(id, "p", "b-open", UpstreamStatus(503, ""))
	cb.RecordFailure(id, "p", "a-closed", UpstreamStatus(502, ""))
	cb.RecordSuccess(id, "p", "c-served")

	s := onlyDetail(t, cb)
	if len(s.Circuits) != 3 {
		t.Fatalf("circuits = %+v, want 3", s.Circuits)
	}
	for i, want := range []string{"a-closed", "b-open", "c-served"} {
		if s.Circuits[i].Model != want {
			t.Errorf("circuits[%d].Model = %q, want %q (sorted)", i, s.Circuits[i].Model, want)
		}
	}
	var open []string
	for _, c := range s.Circuits {
		if c.State == StateOpen.String() {
			open = append(open, c.Model)
		}
	}
	if len(open) != 1 || open[0] != "b-open" || len(s.OpenModels) != 1 || s.OpenModels[0] != "b-open" {
		t.Errorf("open circuits %v vs open_models %v", open, s.OpenModels)
	}

	byModel := map[string]CircuitStatus{}
	for _, c := range s.Circuits {
		byModel[c.Model] = c
	}
	o := byModel["b-open"]
	if o.LastCause != "upstream status 503" || o.LastStatus != 503 || o.LastAt == "" {
		t.Errorf("open circuit verdict = %+v", o)
	}
	if o.ConsecutiveFails != 2 || o.OpenedAt == "" || o.CooldownMs != time.Minute.Milliseconds() || o.NextRetryAt == "" {
		t.Errorf("open circuit wait = %+v", o)
	}
	c := byModel["a-closed"]
	if c.ConsecutiveFails != 1 || c.LastStatus != 502 || c.OpenedAt != "" || c.CooldownMs != 0 || c.NextRetryAt != "" {
		t.Errorf("closed circuit = %+v, want a charge and no wait", c)
	}
	if byModel["c-served"].LastCause != causeSuccess {
		t.Errorf("served circuit verdict = %+v", byModel["c-served"])
	}

	// A circuit owed a probe keeps its opened_at and its verdict, so the row
	// can still say how long it was dark and why.
	backdateOpenModel(t, cb, id, "b-open", 2*time.Minute)
	s = onlyDetail(t, cb)
	for _, c := range s.Circuits {
		if c.Model != "b-open" {
			continue
		}
		if c.State != StateHalfOpen.String() || c.OpenedAt == "" || c.NextRetryAt != "" || c.LastCause != "upstream status 503" {
			t.Errorf("half-open circuit = %+v", c)
		}
	}
	if len(s.OpenModels) != 0 {
		t.Errorf("open_models = %v after the cooldown elapsed", s.OpenModels)
	}
}

// The open event and the open log line carry the verdict that produced them,
// and the event's sentence names it, because the message is all an outbound
// alert renders. The close carries its own verdict too.
func TestOpenEventAndLogCarryTheCause(t *testing.T) {
	capt := captureLogs(t)
	sub := events.Subscribe()
	defer events.Unsubscribe(sub)

	cb := newTestCB(1, time.Minute)
	id := uuid.New()
	cb.RecordFailure(id, "zai", "glm-5.3", UpstreamStatus(429, "unrecognised"))

	ev := waitForOpenEvent(t, sub, id)
	if got, _ := ev.Metadata["cause"].(string); got != "upstream status 429 (unrecognised)" {
		t.Errorf("event cause = %q", got)
	}
	if got, _ := ev.Metadata["status"].(int); got != 429 {
		t.Errorf("event status = %v", ev.Metadata["status"])
	}
	want := "Provider zai circuit breaker: open for model glm-5.3: upstream status 429 (unrecognised)"
	if ev.Message != want {
		t.Errorf("event message = %q, want %q", ev.Message, want)
	}

	var opened *capturedLog
	for _, r := range capt.forProvider(id) {
		if r.msg == "circuit-breaker: model state=closed→open" {
			rec := r
			opened = &rec
		}
	}
	if opened == nil {
		t.Fatal("no open log line for the provider")
	}
	if got, _ := opened.attrs["cause"].(string); got != "upstream status 429 (unrecognised)" {
		t.Errorf("log cause = %q", got)
	}
	// slog widens an int attribute to int64; the rendered value is what matters.
	if got := fmt.Sprint(opened.attrs["status"]); got != "429" {
		t.Errorf("log status = %v", opened.attrs["status"])
	}

	// Close it: the cooldown elapses, IsOpen moves the circuit to half-open
	// and lets the probe through, and the probe succeeds.
	backdateOpenModel(t, cb, id, "glm-5.3", 2*time.Minute)
	if cb.IsOpen(id, "zai", "glm-5.3") {
		t.Fatal("setup: circuit still blocking after its cooldown elapsed")
	}
	cb.RecordSuccess(id, "zai", "glm-5.3")
	deadline := time.After(2 * time.Second)
	for {
		select {
		case ev := <-sub:
			if ev.Type != "circuit_breaker.closed" {
				continue
			}
			if got, _ := ev.Metadata["cause"].(string); got != causeSuccess {
				t.Errorf("closed event cause = %q", got)
			}
			if ev.Message != "Provider zai circuit breaker: closed for model glm-5.3" {
				t.Errorf("closed message = %q: a recovery names only what recovered", ev.Message)
			}
			return
		case <-deadline:
			t.Fatal("no circuit_breaker.closed event")
		}
	}
}

func onlyDetail(t *testing.T, cb *CircuitBreaker) ProviderStatus {
	t.Helper()
	statuses := cb.StatusDetail()
	if len(statuses) != 1 {
		t.Fatalf("got %d detail statuses, want 1", len(statuses))
	}
	return statuses[0]
}

// The row alone never carries the list: Status is what the scrape and the
// aggregate poll read, and neither looks past the row.
func TestStatus_OmitsCircuitsWithoutDetail(t *testing.T) {
	cb := newTestCB(1, time.Minute)
	id := uuid.New()
	cb.RecordFailure(id, "p", "m", UpstreamStatus(503, ""))
	if s := onlyStatus(t, cb); s.Circuits != nil {
		t.Errorf("Status() carried circuits: %+v", s.Circuits)
	}
	if s := onlyDetail(t, cb); len(s.Circuits) != 1 {
		t.Errorf("StatusDetail() circuits = %+v, want one", s.Circuits)
	}
}

// ResetModel clears exactly one circuit: the sibling keeps its charges, an
// untracked pair is a no-op that says so, the provider entry disappears with
// its last circuit, and every manual reset logs one line per circuit with the
// cause vocabulary the rest of the breaker uses.
func TestResetModel_ClearsOneCircuitAndLogsEach(t *testing.T) {
	capt := captureLogs(t)
	cb := newTestCB(2, time.Minute)
	id := uuid.New()
	cb.RecordFailure(id, "p", "dark", UpstreamStatus(503, ""))
	cb.RecordFailure(id, "p", "dark", UpstreamStatus(503, ""))
	cb.RecordFailure(id, "p", "charged", UpstreamStatus(502, "")) // one charge, still closed

	if prev, existed := cb.ResetModel(id, "missing"); prev != StateClosed || existed {
		t.Errorf("untracked pair = (%v, %v), want (closed, false)", prev, existed)
	}
	prev, existed := cb.ResetModel(id, "dark")
	if prev != StateOpen || !existed {
		t.Fatalf("reset of the open circuit = (%v, %v), want (open, true)", prev, existed)
	}
	if cb.GetState(id, "dark") != StateClosed {
		t.Error("the reset circuit still reports open")
	}
	if !hasCircuit(t, cb, id, "charged") {
		t.Error("the sibling circuit was cleared by a scoped reset")
	}
	if prev, existed := cb.ResetModel(id, "charged"); prev != StateClosed || !existed {
		t.Errorf("reset of a closed tracked circuit = (%v, %v), want (closed, true)", prev, existed)
	}
	if countCircuits(t, cb, id) != 0 {
		t.Error("provider entry survived its last circuit")
	}

	lines := manualResetLines(capt, id.String())
	if len(lines) != 2 {
		t.Fatalf("manual reset lines = %d, want one per circuit that existed", len(lines))
	}
	for _, l := range lines {
		if l["cause"] != "manual reset" {
			t.Errorf("reset line cause = %v", l["cause"])
		}
	}

	// Reset (whole provider) and ResetAll log per circuit too.
	cb.RecordFailure(id, "p", "a", UpstreamStatus(503, ""))
	cb.RecordFailure(id, "p", "b", UpstreamStatus(503, ""))
	cb.Reset(id)
	other := uuid.New()
	cb.RecordFailure(other, "q", "c", UpstreamStatus(503, ""))
	cb.ResetAll()
	total := len(manualResetLines(capt, ""))
	if total != 5 {
		t.Errorf("manual reset lines overall = %d, want 5 (2 + 2 + 1)", total)
	}
}

// manualResetLines returns the attrs of every "manual reset" line captured,
// optionally only those for one provider id. The reset lines carry the id as
// the map's string key, which forProvider's uuid.UUID match cannot see.
func manualResetLines(capt *logCaptureHandler, providerID string) []map[string]any {
	capt.mu.Lock()
	defer capt.mu.Unlock()
	var out []map[string]any
	for _, r := range capt.records {
		if r.msg != "circuit-breaker: manual reset" {
			continue
		}
		if providerID != "" && r.attrs["provider_id"] != providerID {
			continue
		}
		out = append(out, r.attrs)
	}
	return out
}

// breakerReadingHandler reads the breaker while handling a record, standing in
// for the app-log handler that can block on its store for seconds per line.
type breakerReadingHandler struct {
	*logCaptureHandler
	cb *CircuitBreaker
	id uuid.UUID
}

func (h *breakerReadingHandler) Handle(ctx context.Context, r slog.Record) error {
	if r.Message == "circuit-breaker: manual reset" {
		// Takes cb.mu: deadlocks if the reset that logged still holds it.
		h.cb.GetState(h.id, "unrelated")
	}
	return h.logCaptureHandler.Handle(ctx, r)
}

// Every manual reset logs with the breaker's lock released. cb.mu is the lock
// each request's IsOpen takes, and the app-log sink can stall for seconds per
// line when its store is backed up: a fleet-wide reset that logged N circuits
// under the lock would stall the request path for N of those stalls.
func TestManualResetsLogWithTheLockReleased(t *testing.T) {
	capt := captureLogs(t)
	cb := newTestCB(2, time.Minute)
	id := uuid.New()
	debuglog.SetHandler(&breakerReadingHandler{logCaptureHandler: capt, cb: cb, id: id})
	for _, m := range []string{"a", "b", "c"} {
		cb.RecordFailure(id, "p", m, UpstreamStatus(503, ""))
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		cb.ResetModel(id, "a")
		cb.Reset(id)
		cb.RecordFailure(id, "p", "d", UpstreamStatus(503, ""))
		cb.ResetAll()
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("a manual reset logged while holding the breaker's lock")
	}
	if n := len(manualResetLines(capt, id.String())); n != 4 {
		t.Errorf("manual reset lines = %d, want 4 (1 scoped + 2 provider + 1 all)", n)
	}
}
