package failover

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/events"
)

// The probe backoff is exercised with minute-scale cooldowns that never elapse
// during a test; backdateOpen moves the open instant instead of the test
// waiting. Every assertion is about what the breaker would DO (IsOpen) or what
// it TELLS an operator (Status, the event, the log line), never about the
// stored field.
//
// base sits well under the 1h default ceiling so every doubling is visible:
// 10m, 20m, 40m, then the ceiling.
const backoffTestBase = 10 * time.Minute

// failProbe hands the circuit's half-open probe to a request and fails it: the
// circuit must already be past its cooldown for IsOpen to give one out.
func failProbe(t *testing.T, cb *CircuitBreaker, id uuid.UUID) {
	t.Helper()
	if cb.IsOpen(id, "test-provider", "") {
		t.Fatal("setup: the circuit is still dark, no probe to fail")
	}
	cb.RecordFailure(id, "test-provider", "")
	if got := cb.GetState(id, ""); got != StateOpen {
		t.Fatalf("setup: after a failed probe got state %v, want open", got)
	}
}

func TestCircuitBreaker_EachFailedProbeDoublesTheCooldown(t *testing.T) {
	cb := newTestCB(1, backoffTestBase)
	id := uuid.New()
	openBreaker(t, cb, id)

	if s := onlyStatus(t, cb); s.CooldownMs != backoffTestBase.Milliseconds() || s.BackedOff || s.FailedProbes != 0 {
		t.Fatalf("a freshly opened circuit reports cooldown %dms backed_off=%v failed_probes=%d, want the base and no backoff",
			s.CooldownMs, s.BackedOff, s.FailedProbes)
	}

	backdateOpen(t, cb, id, backoffTestBase+time.Minute)
	failProbe(t, cb, id)

	s := onlyStatus(t, cb)
	if s.CooldownMs != (2 * backoffTestBase).Milliseconds() {
		t.Errorf("after one failed probe cooldown is %dms, want %v", s.CooldownMs, 2*backoffTestBase)
	}
	if !s.BackedOff || s.FailedProbes != 1 {
		t.Errorf("after one failed probe backed_off=%v failed_probes=%d, want true/1", s.BackedOff, s.FailedProbes)
	}

	// The number the status publishes has to be the one the breaker enforces:
	// 15 minutes into a doubled 20-minute cooldown the circuit is still dark,
	// where the base alone would have handed out a probe five minutes ago.
	backdateOpen(t, cb, id, 15*time.Minute)
	if !cb.IsOpen(id, "test-provider", "") {
		t.Fatal("a probe was handed out 15 minutes into a doubled 20-minute cooldown")
	}

	backdateOpen(t, cb, id, 6*time.Minute)
	failProbe(t, cb, id)
	if s := onlyStatus(t, cb); s.CooldownMs != (4*backoffTestBase).Milliseconds() || s.FailedProbes != 2 {
		t.Errorf("after two failed probes cooldown %dms failed_probes=%d, want %v/2", s.CooldownMs, s.FailedProbes, 4*backoffTestBase)
	}
}

func TestCircuitBreaker_BackoffStopsAtTheCeiling(t *testing.T) {
	// The default ceiling, reached on the third failed probe: 20m, 40m, 60m.
	cb := newTestCB(1, backoffTestBase)
	id := uuid.New()
	openBreaker(t, cb, id)
	for i, want := range []time.Duration{20 * time.Minute, 40 * time.Minute, time.Hour, time.Hour} {
		backdateOpen(t, cb, id, 24*time.Hour)
		failProbe(t, cb, id)
		if s := onlyStatus(t, cb); s.CooldownMs != want.Milliseconds() {
			t.Errorf("after %d failed probes cooldown is %dms, want %v", i+1, s.CooldownMs, want)
		}
	}

	// A configured ceiling is read and applied, not just the default.
	cb = NewCircuitBreaker(&stubSettings{threshold: 1, cooldown: time.Hour, backoffMax: 3 * time.Hour})
	id = uuid.New()
	openBreaker(t, cb, id)
	for i, want := range []time.Duration{2 * time.Hour, 3 * time.Hour, 3 * time.Hour} {
		backdateOpen(t, cb, id, 24*time.Hour)
		failProbe(t, cb, id)
		if s := onlyStatus(t, cb); s.CooldownMs != want.Milliseconds() {
			t.Errorf("configured 3h ceiling: after %d failed probes cooldown is %dms, want %v", i+1, s.CooldownMs, want)
		}
	}
}

// A ceiling at or below the base cooldown must not shorten it: the ceiling
// bounds what the backoff may add, it is not a second cooldown setting.
func TestCircuitBreaker_BackoffCeilingBelowTheBaseNeverShortensIt(t *testing.T) {
	cb := NewCircuitBreaker(&stubSettings{threshold: 1, cooldown: time.Hour, backoffMax: time.Minute})
	id := uuid.New()
	openBreaker(t, cb, id)
	backdateOpen(t, cb, id, 24*time.Hour)
	failProbe(t, cb, id)

	if s := onlyStatus(t, cb); s.CooldownMs != time.Hour.Milliseconds() || s.BackedOff {
		t.Errorf("cooldown %dms backed_off=%v, want the base hour with no backoff", s.CooldownMs, s.BackedOff)
	}
}

func TestCircuitBreaker_AProbeThatSucceedsResetsTheBackoff(t *testing.T) {
	cb := newTestCB(1, backoffTestBase)
	id := uuid.New()
	openBreaker(t, cb, id)
	backdateOpen(t, cb, id, 24*time.Hour)
	failProbe(t, cb, id)
	backdateOpen(t, cb, id, 24*time.Hour)
	failProbe(t, cb, id)

	backdateOpen(t, cb, id, 24*time.Hour)
	if cb.IsOpen(id, "test-provider", "") {
		t.Fatal("setup: no probe handed out past the cooldown")
	}
	cb.RecordSuccess(id, "test-provider", "")
	if got := cb.GetState(id, ""); got != StateClosed {
		t.Fatalf("after a successful probe got state %v, want closed", got)
	}

	// The next open is a fresh incident and starts from the base again.
	cb.RecordFailure(id, "test-provider", "")
	if s := onlyStatus(t, cb); s.CooldownMs != backoffTestBase.Milliseconds() || s.BackedOff || s.FailedProbes != 0 {
		t.Errorf("after recovery and a new open: cooldown %dms backed_off=%v failed_probes=%d, want base/false/0",
			s.CooldownMs, s.BackedOff, s.FailedProbes)
	}
}

// The kill switch is re-read on every check, like the quota pin's: an operator
// who switches backoff off to get a provider back does not wait out a backoff
// that was stamped on before the switch.
func TestCircuitBreaker_DisablingBackoffReleasesOneAlreadyInForce(t *testing.T) {
	enabled := true
	cb := NewCircuitBreaker(&stubSettings{threshold: 1, cooldown: backoffTestBase, backoffEnabled: &enabled})
	id := uuid.New()
	openBreaker(t, cb, id)
	backdateOpen(t, cb, id, 24*time.Hour)
	failProbe(t, cb, id)
	if s := onlyStatus(t, cb); !s.BackedOff {
		t.Fatal("setup: no backoff in force")
	}

	enabled = false

	s := onlyStatus(t, cb)
	if s.CooldownMs != backoffTestBase.Milliseconds() || s.BackedOff {
		t.Errorf("with backoff disabled cooldown %dms backed_off=%v, want the base and false", s.CooldownMs, s.BackedOff)
	}
	// The count is still reported: it is what happened, not what governs.
	if s.FailedProbes != 1 {
		t.Errorf("failed_probes %d, want 1 even with backoff disabled", s.FailedProbes)
	}
	// And the breaker enforces the base: 15 minutes in, the probe is due.
	backdateOpen(t, cb, id, 15*time.Minute)
	if cb.IsOpen(id, "test-provider", "") {
		t.Error("backoff disabled but the circuit still enforced the doubled cooldown")
	}

	// Switching it back on re-applies the backoff already stamped, in the same
	// way: the switch is a read-time gate, not a stamp-time one.
	enabled = true
	if s := onlyStatus(t, cb); !s.BackedOff {
		t.Error("re-enabling backoff did not restore the backoff already stamped")
	}
}

// Pinning never makes the breaker more aggressive, and that floor is the
// cooldown actually in force. A quota reading that says the window resets in 15
// minutes must not pull a 20-minute backoff in.
func TestCircuitBreaker_QuotaPinIsFlooredAtTheBackoff(t *testing.T) {
	cb := newTestCB(1, backoffTestBase)
	id := uuid.New()
	openBreaker(t, cb, id)
	backdateOpen(t, cb, id, 24*time.Hour)
	cb.SetQuotaAdvisor(stubAdvisor{at: time.Now().Add(15 * time.Minute), ok: true})
	failProbe(t, cb, id)

	if s := onlyStatus(t, cb); s.QuotaPinned || s.CooldownMs != (2*backoffTestBase).Milliseconds() {
		t.Errorf("a 15-minute pin against a 20m backoff: quota_pinned=%v cooldown %dms, want unpinned/20m", s.QuotaPinned, s.CooldownMs)
	}

	// A reading further out than the backoff still pins.
	backdateOpen(t, cb, id, 24*time.Hour)
	cb.SetQuotaAdvisor(stubAdvisor{at: time.Now().Add(10 * time.Hour), ok: true})
	failProbe(t, cb, id)
	if s := onlyStatus(t, cb); !s.QuotaPinned || s.CooldownMs < (10*time.Hour).Milliseconds() {
		t.Errorf("a 10h pin against a 40m backoff: quota_pinned=%v cooldown %dms, want pinned/>=10h", s.QuotaPinned, s.CooldownMs)
	}
}

func TestCircuitBreaker_RetargetedPinIsFlooredAtTheBackoff(t *testing.T) {
	cb := newTestCB(1, backoffTestBase)
	id := uuid.New()
	openBreaker(t, cb, id)
	backdateOpen(t, cb, id, 24*time.Hour)
	failProbe(t, cb, id)

	if n := cb.ApplyQuotaPins(map[uuid.UUID]time.Time{id: time.Now().Add(15 * time.Minute)}); n != 0 {
		t.Errorf("ApplyQuotaPins retargeted %d circuits with advice shorter than the backoff, want 0", n)
	}
	if s := onlyStatus(t, cb); s.QuotaPinned || s.CooldownMs != (2*backoffTestBase).Milliseconds() {
		t.Errorf("quota_pinned=%v cooldown %dms after a too-short retarget, want unpinned/20m", s.QuotaPinned, s.CooldownMs)
	}
	if n := cb.ApplyQuotaPins(map[uuid.UUID]time.Time{id: time.Now().Add(10 * time.Hour)}); n != 1 {
		t.Errorf("ApplyQuotaPins retargeted %d circuits with advice beyond the backoff, want 1", n)
	}
}

// Lifting a pin only ever shortens a wait, but the wait it falls back to is the
// one the circuit had earned: a provider that recovered its quota is still a
// provider whose probe failed.
func TestCircuitBreaker_ReleasingAPinFallsBackToTheBackoff(t *testing.T) {
	capt := captureLogs(t)
	cb := newTestCB(1, backoffTestBase)
	id := uuid.New()
	openBreaker(t, cb, id)
	backdateOpen(t, cb, id, 24*time.Hour)
	cb.SetQuotaAdvisor(stubAdvisor{at: time.Now().Add(10 * time.Hour), ok: true})
	failProbe(t, cb, id)
	if s := onlyStatus(t, cb); !s.QuotaPinned {
		t.Fatal("setup: not pinned")
	}

	if n := cb.ReleaseQuotaPins(map[uuid.UUID]struct{}{id: {}}); n != 1 {
		t.Fatalf("released %d pins, want 1", n)
	}
	if s := onlyStatus(t, cb); s.QuotaPinned || !s.BackedOff || s.CooldownMs != (2*backoffTestBase).Milliseconds() {
		t.Errorf("after release quota_pinned=%v backed_off=%v cooldown %dms, want unpinned, backed off, 20m",
			s.QuotaPinned, s.BackedOff, s.CooldownMs)
	}
	// The release line promises a cooldown; it must be the one that governs.
	_, attrs, ok := capt.last("circuit-breaker: quota pin released (provider no longer exhausted)")
	if !ok {
		t.Fatal("no release log line captured")
	}
	if got, _ := attrs["cooldown_ms"].(int64); got != (2 * backoffTestBase).Milliseconds() {
		t.Errorf("release log line cooldown_ms=%v, want the 20m backoff the circuit falls back to", attrs["cooldown_ms"])
	}
}

// The open event and its log line are the operator's only trail for why a
// cooldown is longer than the setting says, so they carry the count and the
// verdict. model_id is the identity the alert dispatcher debounces on: without
// it, two models failing on one provider inside the dispatcher's window collapse
// to one alert and the second is dropped, which is the bug #829 fixed for the
// unstable event and which these two events still had.
func TestCircuitBreaker_OpenEventCarriesModelIDAndTheBackoff(t *testing.T) {
	capt := captureLogs(t)
	sub := events.Subscribe()
	defer events.Unsubscribe(sub)

	cb := newTestCB(1, backoffTestBase)
	id := uuid.New()
	cb.RecordFailure(id, "test-provider", modelA)

	ev := waitForOpenEvent(t, sub, id)
	if got, _ := ev.Metadata["model_id"].(string); got != modelA {
		t.Errorf("open event model_id %q, want %q", got, modelA)
	}
	if got, _ := ev.Metadata["backed_off"].(bool); got {
		t.Error("the first open reports backed_off=true, nothing has been probed yet")
	}
	if got, _ := ev.Metadata["failed_probes"].(int); got != 0 {
		t.Errorf("the first open reports failed_probes=%d, want 0", got)
	}

	cb.mu.Lock()
	cb.circuits[id.String()][modelA].openedAt = time.Now().Add(-24 * time.Hour)
	cb.mu.Unlock()
	if cb.IsOpen(id, "test-provider", modelA) {
		t.Fatal("setup: no probe handed out past the cooldown")
	}
	cb.RecordFailure(id, "test-provider", modelA)

	ev = waitForOpenEvent(t, sub, id)
	if got, _ := ev.Metadata["backed_off"].(bool); !got {
		t.Error("the re-open after a failed probe reports backed_off=false")
	}
	if got, _ := ev.Metadata["failed_probes"].(int); got != 1 {
		t.Errorf("the re-open reports failed_probes=%d, want 1", got)
	}
	_, attrs, ok := capt.last("circuit-breaker: model state=half-open→open (probe failed)")
	if !ok {
		t.Fatal("no probe-failed log line captured")
	}
	if got, _ := attrs["failed_probes"].(int64); got != 1 {
		t.Errorf("log line failed_probes=%v, want 1", attrs["failed_probes"])
	}
	if got, _ := attrs["backed_off"].(bool); !got {
		t.Errorf("log line backed_off=%v, want true", attrs["backed_off"])
	}
	if got, _ := attrs["cooldown_ms"].(int64); got != (2 * backoffTestBase).Milliseconds() {
		t.Errorf("log line cooldown_ms=%v, want 20m: the line must show the cooldown actually enforced", attrs["cooldown_ms"])
	}
}

func TestCircuitBreaker_ClosedEventCarriesModelID(t *testing.T) {
	sub := events.Subscribe()
	defer events.Unsubscribe(sub)

	cb := newTestCB(1, 0)
	id := uuid.New()
	cb.RecordFailure(id, "test-provider", modelA)
	if cb.IsOpen(id, "test-provider", modelA) {
		t.Fatal("setup: no probe handed out with a zero cooldown")
	}
	cb.RecordSuccess(id, "test-provider", modelA)

	deadline := time.After(2 * time.Second)
	for {
		select {
		case ev := <-sub:
			if ev.Type != "circuit_breaker.closed" {
				continue
			}
			if pid, _ := ev.Metadata["provider_id"].(string); pid != id.String() {
				continue
			}
			if got, _ := ev.Metadata["model_id"].(string); got != modelA {
				t.Errorf("closed event model_id %q, want %q", got, modelA)
			}
			return
		case <-deadline:
			t.Fatal("no circuit_breaker.closed event published within timeout")
		}
	}
}
