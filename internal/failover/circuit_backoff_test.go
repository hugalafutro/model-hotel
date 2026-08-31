package failover

import (
	"fmt"
	"strings"
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
// base sits well under the 15m default ceiling so every doubling is visible:
// 2m, 4m, 8m, then the ceiling.
const backoffTestBase = 2 * time.Minute

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

// backOffOnce drives a fresh circuit to one failed probe: open, past the
// cooldown, probe failed, so the backoff is 2 x base.
func backOffOnce(t *testing.T, cb *CircuitBreaker, id uuid.UUID) {
	t.Helper()
	openBreaker(t, cb, id)
	backdateOpen(t, cb, id, 24*time.Hour)
	failProbe(t, cb, id)
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
	// three minutes into a doubled four-minute cooldown the circuit is still
	// dark, where the base alone would have handed out a probe a minute ago.
	backdateOpen(t, cb, id, 3*time.Minute)
	if !cb.IsOpen(id, "test-provider", "") {
		t.Fatal("a probe was handed out 3 minutes into a doubled 4-minute cooldown")
	}

	backdateOpen(t, cb, id, 2*time.Minute)
	failProbe(t, cb, id)
	if s := onlyStatus(t, cb); s.CooldownMs != (4*backoffTestBase).Milliseconds() || s.FailedProbes != 2 {
		t.Errorf("after two failed probes cooldown %dms failed_probes=%d, want %v/2", s.CooldownMs, s.FailedProbes, 4*backoffTestBase)
	}
}

func TestCircuitBreaker_BackoffStopsAtTheCeiling(t *testing.T) {
	// The default ceiling, reached on the third failed probe: 4m, 8m, 15m.
	cb := newTestCB(1, backoffTestBase)
	id := uuid.New()
	openBreaker(t, cb, id)
	for i, want := range []time.Duration{4 * time.Minute, 8 * time.Minute, defaultBackoffMax, defaultBackoffMax} {
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
	backOffOnce(t, cb, id)

	if s := onlyStatus(t, cb); s.CooldownMs != time.Hour.Milliseconds() || s.BackedOff {
		t.Errorf("cooldown %dms backed_off=%v, want the base hour with no backoff", s.CooldownMs, s.BackedOff)
	}
}

// The stamped backoff cannot know about a cooldown raised after it was stamped.
// The feature exists to lengthen a wait, so it must never serve less than the
// setting, and a row must not claim a backoff for a cooldown identical to it.
func TestCircuitBreaker_RaisingTheCooldownAboveABackoffGovernsAndDropsTheFlag(t *testing.T) {
	settings := &stubSettings{threshold: 1, cooldown: backoffTestBase}
	cb := NewCircuitBreaker(settings)
	id := uuid.New()
	backOffOnce(t, cb, id)
	if s := onlyStatus(t, cb); !s.BackedOff || s.CooldownMs != (2*backoffTestBase).Milliseconds() {
		t.Fatalf("setup: cooldown %dms backed_off=%v, want a 4m backoff", s.CooldownMs, s.BackedOff)
	}

	// Raised to exactly the backoff: the same cooldown, and no claim of a backoff.
	settings.cooldown = 2 * backoffTestBase
	if s := onlyStatus(t, cb); s.CooldownMs != (2*backoffTestBase).Milliseconds() || s.BackedOff {
		t.Errorf("base raised to the backoff's 4m: cooldown %dms backed_off=%v, want 4m/false", s.CooldownMs, s.BackedOff)
	}

	// The operator raises the cooldown past the backoff. The setting governs.
	settings.cooldown = 10 * time.Minute
	if s := onlyStatus(t, cb); s.CooldownMs != (10*time.Minute).Milliseconds() || s.BackedOff || s.FailedProbes != 1 {
		t.Errorf("base raised to 10m: cooldown %dms backed_off=%v failed_probes=%d, want 10m/false/1", s.CooldownMs, s.BackedOff, s.FailedProbes)
	}
	backdateOpen(t, cb, id, 5*time.Minute)
	if !cb.IsOpen(id, "test-provider", "") {
		t.Error("base raised to 10m but the circuit handed out a probe 5 minutes in, on the stale 4m backoff")
	}
}

func TestCircuitBreaker_AProbeThatSucceedsResetsTheBackoff(t *testing.T) {
	cb := newTestCB(1, backoffTestBase)
	id := uuid.New()
	backOffOnce(t, cb, id)
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
	backOffOnce(t, cb, id)
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
	// And the breaker enforces the base: 3 minutes in, the probe is due.
	backdateOpen(t, cb, id, 3*time.Minute)
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
// cooldown actually in force. A quota reading that says the window resets in
// three minutes must not pull a four-minute backoff in.
func TestCircuitBreaker_QuotaPinIsFlooredAtTheBackoff(t *testing.T) {
	cb := newTestCB(1, backoffTestBase)
	id := uuid.New()
	openBreaker(t, cb, id)
	backdateOpen(t, cb, id, 24*time.Hour)
	cb.SetQuotaAdvisor(stubAdvisor{at: time.Now().Add(3 * time.Minute), ok: true})
	failProbe(t, cb, id)

	if s := onlyStatus(t, cb); s.QuotaPinned || s.CooldownMs != (2*backoffTestBase).Milliseconds() {
		t.Errorf("a 3-minute pin against a 4m backoff: quota_pinned=%v cooldown %dms, want unpinned/4m", s.QuotaPinned, s.CooldownMs)
	}

	// A reading further out than the backoff still pins.
	backdateOpen(t, cb, id, 24*time.Hour)
	cb.SetQuotaAdvisor(stubAdvisor{at: time.Now().Add(10 * time.Hour), ok: true})
	failProbe(t, cb, id)
	if s := onlyStatus(t, cb); !s.QuotaPinned || s.CooldownMs < (9*time.Hour).Milliseconds() {
		t.Errorf("a 10h pin against an 8m backoff: quota_pinned=%v cooldown %dms, want pinned/~10h", s.QuotaPinned, s.CooldownMs)
	}
}

// The pin ceiling is applied before the floor is checked, or a ceiling below
// the cooldown in force would stamp a pin that shortens the wait the floor
// exists to protect.
func TestCircuitBreaker_PinCeilingBelowTheBackoffStampsNoPin(t *testing.T) {
	cb := NewCircuitBreaker(&stubSettings{threshold: 1, cooldown: backoffTestBase, pinMax: 3 * time.Minute})
	id := uuid.New()
	openBreaker(t, cb, id)
	backdateOpen(t, cb, id, 24*time.Hour)
	cb.SetQuotaAdvisor(stubAdvisor{at: time.Now().Add(10 * time.Hour), ok: true})
	failProbe(t, cb, id)

	if s := onlyStatus(t, cb); s.QuotaPinned || s.CooldownMs != (2*backoffTestBase).Milliseconds() {
		t.Errorf("10h advice under a 3m pin ceiling against a 4m backoff: quota_pinned=%v cooldown %dms, want unpinned/4m", s.QuotaPinned, s.CooldownMs)
	}
}

// A pin is floored at the cooldown in force when it is stamped, and with
// backoff switched off that floor is the base. Switch backoff back on and the
// pin in force is shorter than the backoff. The longest governs, so the circuit
// serves the backoff, and both flags say what is in force.
func TestCircuitBreaker_PinStampedWithBackoffOffNeverOutranksTheBackoffOnceOn(t *testing.T) {
	enabled := false
	cb := NewCircuitBreaker(&stubSettings{threshold: 1, cooldown: backoffTestBase, backoffEnabled: &enabled})
	id := uuid.New()
	openBreaker(t, cb, id)
	backdateOpen(t, cb, id, 24*time.Hour)
	cb.SetQuotaAdvisor(stubAdvisor{at: time.Now().Add(3 * time.Minute), ok: true})
	failProbe(t, cb, id)
	if s := onlyStatus(t, cb); !s.QuotaPinned || s.BackedOff {
		t.Fatalf("setup: quota_pinned=%v backed_off=%v, want a 3m pin over a disabled backoff", s.QuotaPinned, s.BackedOff)
	}

	enabled = true
	s := onlyStatus(t, cb)
	if s.CooldownMs != (2 * backoffTestBase).Milliseconds() {
		t.Errorf("backoff re-enabled under a shorter pin: cooldown %dms, want the 4m backoff", s.CooldownMs)
	}
	if !s.BackedOff || !s.QuotaPinned {
		t.Errorf("backed_off=%v quota_pinned=%v, want both: both are in force, the longer governs", s.BackedOff, s.QuotaPinned)
	}
	backdateOpen(t, cb, id, 3*time.Minute+30*time.Second)
	if !cb.IsOpen(id, "test-provider", "") {
		t.Error("the shorter pin handed out a probe inside the 4m backoff")
	}
}

func TestCircuitBreaker_RetargetedPinIsFlooredAtTheBackoff(t *testing.T) {
	cb := newTestCB(1, backoffTestBase)
	id := uuid.New()
	backOffOnce(t, cb, id)

	if n := cb.ApplyQuotaPins(map[uuid.UUID]time.Time{id: time.Now().Add(3 * time.Minute)}); n != 0 {
		t.Errorf("ApplyQuotaPins retargeted %d circuits with advice shorter than the backoff, want 0", n)
	}
	if s := onlyStatus(t, cb); s.QuotaPinned || s.CooldownMs != (2*backoffTestBase).Milliseconds() {
		t.Errorf("quota_pinned=%v cooldown %dms after a too-short retarget, want unpinned/4m", s.QuotaPinned, s.CooldownMs)
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
		t.Errorf("after release quota_pinned=%v backed_off=%v cooldown %dms, want unpinned, backed off, 4m",
			s.QuotaPinned, s.BackedOff, s.CooldownMs)
	}
	// The release line promises a cooldown; it must be the one that governs.
	_, attrs, ok := capt.last("circuit-breaker: quota pin released (provider no longer exhausted)")
	if !ok {
		t.Fatal("no release log line captured")
	}
	if got, _ := attrs["cooldown_ms"].(int64); got != (2 * backoffTestBase).Milliseconds() {
		t.Errorf("release log line cooldown_ms=%v, want the 4m backoff the circuit falls back to", attrs["cooldown_ms"])
	}
}

// Status is the Prometheus scrape path and it holds the lock the request path
// takes. The switches that gate a backoff and a pin are settings reads, and a
// deployment that never set them has no row to cache, so each is a DB round
// trip: the number of them one Status call takes must not grow with the number
// of backed-off circuits it walks, and the same holds for a request checking a
// sibling model against the provider verdict.
func TestCircuitBreaker_SwitchReadsDoNotScaleWithBackedOffCircuits(t *testing.T) {
	readsFor := func(circuits int) (status, isOpen int) {
		settings := newCountingSettings()
		cb := NewCircuitBreaker(settings)
		cb.Threshold, cb.Cooldown, cb.HalfOpenMaxProbes = 1, backoffTestBase, 1
		// The provider verdict would otherwise skip every sibling once two
		// circuits are open, and this test is about the per-circuit reads.
		cb.SpanModels = 1000
		id := uuid.New()
		for i := range circuits {
			model := fmt.Sprintf("model-%02d", i)
			cb.RecordFailure(id, "test-provider", model)
			cb.mu.Lock()
			cb.circuits[id.String()][model].openedAt = time.Now().Add(-24 * time.Hour)
			cb.mu.Unlock()
			if cb.IsOpen(id, "test-provider", model) {
				t.Fatalf("setup: %s still dark past its cooldown", model)
			}
			cb.RecordFailure(id, "test-provider", model)
		}
		settings.reset()
		s := onlyStatus(t, cb)
		if !s.BackedOff || len(s.OpenModels) != circuits {
			t.Fatalf("setup: backed_off=%v with %d blocking circuits, want true and %d", s.BackedOff, len(s.OpenModels), circuits)
		}
		status = settings.bools("circuit_breaker_backoff_enabled")
		settings.reset()
		cb.IsOpen(id, "test-provider", "a-sibling-nothing-charged")
		isOpen = settings.bools("circuit_breaker_backoff_enabled")
		return status, isOpen
	}

	fewStatus, fewOpen := readsFor(2)
	manyStatus, manyOpen := readsFor(16)
	if fewStatus != manyStatus || fewStatus > 1 {
		t.Errorf("Status() read the backoff switch %d times over 2 backed-off circuits and %d over 16, want once per call: a scrape must not cost one DB round trip per circuit under the breaker lock", fewStatus, manyStatus)
	}
	if fewOpen != manyOpen || fewOpen > 1 {
		t.Errorf("IsOpen() on a sibling read the backoff switch %d times over 2 backed-off circuits and %d over 16, want at most once per request", fewOpen, manyOpen)
	}
}

// The open event and its log line are the operator's only trail for why a
// cooldown is longer than the setting says, so they carry the count, the
// verdict, the cooldown enforced and the deadline it implies. model_id is the
// identity the alert dispatcher debounces on: without it, two models failing on
// one provider inside the dispatcher's window collapse to one alert and the
// second is dropped, which is the bug #829 fixed for the unstable event and
// which these two events still had.
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
	if got, _ := ev.Metadata["cooldown_ms"].(int64); got != backoffTestBase.Milliseconds() {
		t.Errorf("the first open reports cooldown_ms=%v, want the base %v", ev.Metadata["cooldown_ms"], backoffTestBase)
	}
	if v, ok := ev.Metadata["next_retry_at"]; ok {
		t.Errorf("the first open, on the plain cooldown, carries next_retry_at=%v; only a pin or a backoff earns a deadline", v)
	}
	if strings.Contains(ev.Message, "backing off") {
		t.Errorf("the first open's message mentions a backoff that is not in force: %q", ev.Message)
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
	if got, _ := ev.Metadata["cooldown_ms"].(int64); got != (2 * backoffTestBase).Milliseconds() {
		t.Errorf("the re-open reports cooldown_ms=%v, want the doubled %v", ev.Metadata["cooldown_ms"], 2*backoffTestBase)
	}
	deadline, err := time.Parse(time.RFC3339, fmt.Sprint(ev.Metadata["next_retry_at"]))
	if err != nil {
		t.Fatalf("the re-open carries no parseable next_retry_at (%v): an alert about a backed-off circuit has to say when it retries", ev.Metadata["next_retry_at"])
	}
	if want := time.Now().Add(2 * backoffTestBase); deadline.Before(want.Add(-5*time.Second)) || deadline.After(want.Add(5*time.Second)) {
		t.Errorf("next_retry_at=%v, want about %v (openedAt plus the 4m backoff)", deadline, want)
	}
	// The message is all an outbound alert renders, so it has to say this is
	// not a first open, or the operator cannot tell a blip from a model that
	// has failed its retries all afternoon.
	if want := "backing off after 1 failed retry, next retry in 4m"; !strings.Contains(ev.Message, want) {
		t.Errorf("re-open message %q does not say %q", ev.Message, want)
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
		t.Errorf("log line cooldown_ms=%v, want 4m: the line must show the cooldown actually enforced", attrs["cooldown_ms"])
	}
}

// Once the verdict says the provider itself is skipped, the event is about the
// provider and must debounce with it: the verdict lapses every time a blocking
// circuit's cooldown elapses, which lets one more sibling through to open, and
// keyed per model a provider outage would notify once per model inside one
// alert window. Dropping model_id makes the dispatcher fall back to the
// provider.
func TestCircuitBreaker_OpenEventDropsModelIDOnceTheProviderIsSkipped(t *testing.T) {
	sub := events.Subscribe()
	defer events.Unsubscribe(sub)

	cb := newTestCB(1, backoffTestBase)
	cb.SpanModels = 1 // the first open indicts the provider
	id := uuid.New()
	cb.RecordFailure(id, "test-provider", modelA)

	ev := waitForOpenEvent(t, sub, id)
	if open, _ := ev.Metadata["provider_open"].(bool); !open {
		t.Fatalf("setup: provider_open=%v at span 1, want true", open)
	}
	if v, ok := ev.Metadata["model_id"]; ok {
		t.Errorf("a provider-skipped open carries model_id=%v; it must debounce with the provider", v)
	}
	if got, _ := ev.Metadata["model"].(string); got != modelA {
		t.Errorf("model %q dropped along with model_id, want %q: the sentence still names the model", got, modelA)
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
