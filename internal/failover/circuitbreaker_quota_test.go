package failover

import (
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/events"
)

// Quota pins: a circuit held open until the provider's quota window resets,
// and every way that pin is applied, retargeted and released.

func TestQuotaPin_ExtendsCooldownToResetDeadline(t *testing.T) {
	cb := NewCircuitBreaker(nil)
	id := uuid.New()
	reset := time.Now().Add(6 * time.Hour)
	cb.SetQuotaAdvisor(stubAdvisor{at: reset, ok: true})

	openBreaker(t, cb, id)

	statuses := cb.Status()
	if len(statuses) != 1 {
		t.Fatalf("got %d statuses, want 1", len(statuses))
	}
	s := statuses[0]
	if !s.QuotaPinned {
		t.Error("open circuit with quota advice must report quota_pinned")
	}
	// Roughly 6h, plus positive-only jitter capped at 5%. The lower bound has a
	// second of slack because the pin is computed from time.Until(reset), which
	// elapses slightly between the advisor call and this assertion.
	minMs := (6*time.Hour - time.Second).Milliseconds()
	maxMs := (6 * time.Hour).Milliseconds() * 21 / 20
	if s.CooldownMs < minMs || s.CooldownMs > maxMs {
		t.Errorf("got CooldownMs=%d, want within [%d,%d]", s.CooldownMs, minMs, maxMs)
	}
	// The circuit must still be closed to traffic until the pinned deadline.
	if !cb.IsOpen(id, "test-provider") {
		t.Error("pinned circuit must remain open")
	}
}

// TestQuotaPin_JitterVariesAcrossProviders verifies the anti-stampede property
// that motivates jitter in the first place: providers sharing one quota
// deadline (as every node in an HA fleet would, since the deadline is
// fleet-distributed) must not all probe at the same instant. A range check on
// a single circuit's CooldownMs (as the other tests here do) can't distinguish
// a jittering implementation from one that never jitters at all — deleting
// the jitter lines would still satisfy those bounds. This test instead opens
// many circuits against the identical deadline and requires at least two
// distinct CooldownMs values: jitter spans up to 18 minutes (5% of 6h) at
// millisecond granularity, so a non-jittering implementation collapses every
// provider onto the same value while a jittering one collides with
// negligible probability across this many samples.
func TestQuotaPin_JitterVariesAcrossProviders(t *testing.T) {
	cb := NewCircuitBreaker(nil)
	reset := time.Now().Add(6 * time.Hour)
	cb.SetQuotaAdvisor(stubAdvisor{at: reset, ok: true})

	const providerCount = 16
	for range providerCount {
		openBreaker(t, cb, uuid.New())
	}

	seen := make(map[int64]bool)
	for _, s := range cb.Status() {
		seen[s.CooldownMs] = true
	}
	if len(seen) <= 1 {
		t.Errorf("got %d distinct CooldownMs value(s) across %d providers sharing one deadline, want more than 1 (jitter should desync them)", len(seen), providerCount)
	}
}

func TestQuotaPin_FloorNeverShortensCooldown(t *testing.T) {
	cb := NewCircuitBreaker(nil)
	id := uuid.New()
	// Reset is sooner than the default 60s cooldown: the pin must be ignored.
	cb.SetQuotaAdvisor(stubAdvisor{at: time.Now().Add(5 * time.Second), ok: true})

	openBreaker(t, cb, id)

	s := cb.Status()[0]
	if s.QuotaPinned {
		t.Error("a reset sooner than the normal cooldown must not pin")
	}
	if s.CooldownMs != cb.Cooldown.Milliseconds() {
		t.Errorf("got CooldownMs=%d, want default %d", s.CooldownMs, cb.Cooldown.Milliseconds())
	}
}

func TestQuotaPin_CeilingCapsRunawayDeadline(t *testing.T) {
	cb := NewCircuitBreaker(nil)
	id := uuid.New()
	cb.SetQuotaAdvisor(stubAdvisor{at: time.Now().Add(500 * 24 * time.Hour), ok: true})

	openBreaker(t, cb, id)

	s := cb.Status()[0]
	ceiling := (24 * time.Hour).Milliseconds()
	if s.CooldownMs > ceiling+ceiling/20 {
		t.Errorf("got CooldownMs=%d, want capped near %d", s.CooldownMs, ceiling)
	}
}

// TestQuotaPin_SettingsMaxCapsCooldown verifies that a settings-supplied
// circuit_breaker_quota_pin_max is actually read and applied, not just the
// hardcoded 24h fallback. A 1h configured max against a 6h deadline caps near
// 1h; if the settings value were ignored (falling back to the 24h default),
// the 6h deadline would need no capping at all and the result would land near
// 6h instead.
func TestQuotaPin_SettingsMaxCapsCooldown(t *testing.T) {
	cb := NewCircuitBreaker(&stubSettings{threshold: 1, cooldown: time.Second, pinMax: time.Hour})
	id := uuid.New()
	cb.SetQuotaAdvisor(stubAdvisor{at: time.Now().Add(6 * time.Hour), ok: true})

	openBreaker(t, cb, id)

	s := cb.Status()[0]
	if !s.QuotaPinned {
		t.Fatal("setup: expected the pin to apply (6h deadline exceeds the 1s configured cooldown)")
	}
	ceiling := time.Hour.Milliseconds()
	if s.CooldownMs > ceiling+ceiling/20 {
		t.Errorf("got CooldownMs=%d, want capped near the settings max %d", s.CooldownMs, ceiling)
	}
	sixHourMs := (6 * time.Hour).Milliseconds()
	if s.CooldownMs >= sixHourMs/2 {
		t.Errorf("got CooldownMs=%d close to the uncapped 6h deadline (%d) — settings pin max was not applied", s.CooldownMs, sixHourMs)
	}
}

func TestQuotaPin_AbsentOrDecliningAdvisorUsesDefault(t *testing.T) {
	cases := []struct {
		name    string
		advisor QuotaAdvisor
	}{
		{"nil advisor", nil},
		{"advisor declines", stubAdvisor{ok: false}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cb := NewCircuitBreaker(nil)
			id := uuid.New()
			if tc.advisor != nil {
				cb.SetQuotaAdvisor(tc.advisor)
			}

			openBreaker(t, cb, id)

			s := cb.Status()[0]
			if s.QuotaPinned {
				t.Error("must not report pinned without usable advice")
			}
			if s.CooldownMs != cb.Cooldown.Milliseconds() {
				t.Errorf("got CooldownMs=%d, want default %d", s.CooldownMs, cb.Cooldown.Milliseconds())
			}
		})
	}
}

// TestQuotaPin_DisabledBySettingUsesDefault verifies the operator's kill
// switch: circuit_breaker_quota_pin_enabled=false must suppress pinning even
// when the advisor offers perfectly usable, floor-clearing advice.
func TestQuotaPin_DisabledBySettingUsesDefault(t *testing.T) {
	disabled := false
	settings := &stubSettings{threshold: 1, cooldown: 50 * time.Millisecond, pinEnabled: &disabled}
	cb := NewCircuitBreaker(settings)
	id := uuid.New()
	cb.SetQuotaAdvisor(stubAdvisor{at: time.Now().Add(6 * time.Hour), ok: true})

	openBreaker(t, cb, id)

	s := cb.Status()[0]
	if s.QuotaPinned {
		t.Error("circuit_breaker_quota_pin_enabled=false must disable pinning even with usable advice")
	}
	if s.CooldownMs != cb.effectiveCooldown().Milliseconds() {
		t.Errorf("got CooldownMs=%d, want configured cooldown %d", s.CooldownMs, cb.effectiveCooldown().Milliseconds())
	}
}

// TestQuotaPin_DisablingTheSettingReleasesAnAlreadyPinnedCircuit verifies the
// kill switch is retroactive, not merely prospective. An operator looking at
// "next retry in 22 hours" flips circuit_breaker_quota_pin_enabled to false to
// get the provider back; if the switch were only consulted at the moment a
// circuit opens, nothing would change on any surface until the pin expired.
// That matters because it is the only fleet-wide recovery lever: the
// alternative to clearing every pin at once is resetting circuits one provider
// at a time (Reset) or restarting the process.
//
// The assertion is the real mechanism, not just the reported number: after the
// flip the circuit must actually admit a probe once the *configured* cooldown
// has elapsed, which a pinned circuit would refuse for another six hours.
func TestQuotaPin_DisablingTheSettingReleasesAnAlreadyPinnedCircuit(t *testing.T) {
	enabled := true
	// 500ms, not the tens of milliseconds the other cooldown tests use: between
	// openBreaker and the "still open" assertion below sit two Status() calls,
	// and a GC pause or a loaded runner overrunning the configured cooldown there
	// would fail the test with a message about the kill switch. The assertions
	// are unchanged; only the budget they run inside is widened.
	settings := &stubSettings{threshold: 1, cooldown: 500 * time.Millisecond, pinEnabled: &enabled}
	cb := NewCircuitBreaker(settings)
	id := uuid.New()
	cb.SetQuotaAdvisor(stubAdvisor{at: time.Now().Add(6 * time.Hour), ok: true})

	openBreaker(t, cb, id)

	pinnedMs := cb.Status()[0].CooldownMs
	if !cb.Status()[0].QuotaPinned {
		t.Fatal("setup: a 6h deadline against a 500ms cooldown must pin the circuit")
	}
	if pinnedMs < (6*time.Hour - time.Minute).Milliseconds() {
		t.Fatalf("setup: got CooldownMs=%d, want the ~6h pin", pinnedMs)
	}

	// The operator flips the kill switch while the pin is already in force.
	enabled = false

	s := cb.Status()[0]
	if s.QuotaPinned {
		t.Error("a pin the kill switch has disabled must not still report quota_pinned")
	}
	if s.CooldownMs != cb.effectiveCooldown().Milliseconds() {
		t.Errorf("got CooldownMs=%d, want the configured cooldown %d back", s.CooldownMs, cb.effectiveCooldown().Milliseconds())
	}

	// The number and the behaviour must agree: the configured cooldown elapses
	// and the circuit admits a probe again.
	if !cb.IsOpen(id, "test-provider") {
		t.Fatal("the configured cooldown has not elapsed yet; the circuit must still be open")
	}
	time.Sleep(600 * time.Millisecond)
	if cb.IsOpen(id, "test-provider") {
		t.Error("with the pin released, the configured cooldown must let a probe through")
	}
	if got := cb.GetState(id); got == StateOpen {
		t.Errorf("got state %v, want the circuit off the open state once its configured cooldown elapsed", got)
	}
}

func TestQuotaPin_ClearedWhenCircuitCloses(t *testing.T) {
	cb := NewCircuitBreaker(nil)
	// A 1ms base cooldown lets a 50ms pin clear the floor while still being
	// short enough to wait out in a test. The pin overrides the cooldown the
	// instant RecordFailure opens the circuit, so openBreaker's own state
	// check races against the override (50ms), not this 1ms base — safe.
	cb.Cooldown = time.Millisecond
	id := uuid.New()
	cb.SetQuotaAdvisor(stubAdvisor{at: time.Now().Add(50 * time.Millisecond), ok: true})

	openBreaker(t, cb, id)
	if !cb.Status()[0].QuotaPinned {
		t.Fatal("setup: circuit should be pinned")
	}

	// Wait out the pin, take the probe, and succeed: the circuit closes.
	time.Sleep(80 * time.Millisecond)
	if cb.IsOpen(id, "test-provider") {
		t.Fatal("setup: pinned cooldown elapsed, probe should be allowed")
	}
	cb.RecordSuccess(id, "test-provider")
	if got := cb.GetState(id); got != StateClosed {
		t.Fatalf("setup: got state %v, want closed", got)
	}

	// Reopen with no advice: the stale override must not survive the close.
	// This reopen is unpinned, so effectiveCooldownFor falls back to
	// cb.Cooldown live at read time — restore a long value first so
	// openBreaker's state check isn't racing the leftover 1ms cooldown from
	// the pinned phase above (a real flake: on a slow CI runner a fresh Open
	// circuit could already read back as logically half-open before the
	// assertions below run).
	cb.Cooldown = time.Minute
	cb.SetQuotaAdvisor(stubAdvisor{ok: false})
	openBreaker(t, cb, id)

	s := cb.Status()[0]
	if s.QuotaPinned {
		t.Error("a reopened circuit must re-evaluate the pin, not inherit the old one")
	}
	if s.CooldownMs != cb.Cooldown.Milliseconds() {
		t.Errorf("got CooldownMs=%d, want default %d", s.CooldownMs, cb.Cooldown.Milliseconds())
	}
}

func TestQuotaPin_RepinsAfterFailedProbe(t *testing.T) {
	cb := NewCircuitBreaker(nil)
	// Open unpinned with a long cooldown first, so openBreaker's own state
	// check isn't racing a tiny cooldown (a real flake on a slow CI runner:
	// the circuit could already read back as logically half-open before the
	// check runs). Only shrink the cooldown afterwards, for the deliberate
	// wait-out below.
	cb.Cooldown = time.Minute
	id := uuid.New()
	cb.SetQuotaAdvisor(stubAdvisor{ok: false})

	openBreaker(t, cb, id)
	cb.Cooldown = time.Millisecond
	time.Sleep(5 * time.Millisecond)
	if cb.IsOpen(id, "test-provider") {
		t.Fatal("setup: cooldown elapsed, circuit should allow a probe")
	}

	// Probe fails and quota now reports a long window: half-open to open re-pins.
	cb.SetQuotaAdvisor(stubAdvisor{at: time.Now().Add(4 * time.Hour), ok: true})
	cb.RecordFailure(id, "test-provider")

	if !cb.Status()[0].QuotaPinned {
		t.Error("half-open to open must apply the pin")
	}
}

// ---------------------------------------------------------------------------
// Quota pin release (auto-unpin on recovery)
// ---------------------------------------------------------------------------

// TestReleaseQuotaPins_LiftsThePinWhenTheProviderRecovers is the whole point of
// the feature: an operator who tops up a spent plan must not watch a healthy
// provider stay benched for the rest of a pin that can run to 24 hours. The
// assertion is the behaviour, not just the reported number — once the pin is
// lifted the *configured* cooldown governs, and the circuit admits a probe as
// soon as that has elapsed.
func TestReleaseQuotaPins_LiftsThePinWhenTheProviderRecovers(t *testing.T) {
	cb := NewCircuitBreaker(&stubSettings{threshold: 1, cooldown: 300 * time.Millisecond})
	id := uuid.New()
	cb.SetQuotaAdvisor(stubAdvisor{at: time.Now().Add(6 * time.Hour), ok: true})

	openBreaker(t, cb, id)
	if !cb.Status()[0].QuotaPinned {
		t.Fatal("setup: a 6h deadline against a 300ms cooldown must pin the circuit")
	}

	// A successful advice refresh assessed this provider fresh and found its
	// window no longer spent: affirmative recovery evidence.
	if released := cb.ReleaseQuotaPins(map[uuid.UUID]struct{}{id: {}}); released != 1 {
		t.Errorf("got %d pin(s) released, want 1", released)
	}

	s := cb.Status()[0]
	if s.QuotaPinned {
		t.Error("a recovered provider must not still report quota_pinned")
	}
	if s.CooldownMs != cb.effectiveCooldown().Milliseconds() {
		t.Errorf("got CooldownMs=%d, want the configured cooldown %d back", s.CooldownMs, cb.effectiveCooldown().Milliseconds())
	}

	time.Sleep(400 * time.Millisecond)
	if cb.IsOpen(id, "test-provider") {
		t.Error("with the pin lifted, the configured cooldown must let a probe through")
	}
}

// TestReleaseQuotaPins_KeepsThePinForAStillExhaustedProvider guards the other
// direction: a provider the same refresh still assessed as exhausted keeps the
// long cooldown it was given, and an unrelated provider recovering does not drag
// it back into rotation.
func TestReleaseQuotaPins_KeepsThePinForAStillExhaustedProvider(t *testing.T) {
	cb := NewCircuitBreaker(&stubSettings{threshold: 1, cooldown: time.Second})
	stillSpent, recovered := uuid.New(), uuid.New()
	cb.SetQuotaAdvisor(stubAdvisor{at: time.Now().Add(6 * time.Hour), ok: true})

	openBreaker(t, cb, stillSpent)
	openBreaker(t, cb, recovered)

	if released := cb.ReleaseQuotaPins(map[uuid.UUID]struct{}{recovered: {}}); released != 1 {
		t.Errorf("got %d pin(s) released, want only the recovered provider's", released)
	}

	byID := make(map[string]ProviderStatus)
	for _, s := range cb.Status() {
		byID[s.ProviderID] = s
	}
	if !byID[stillSpent.String()].QuotaPinned {
		t.Error("a provider absent from the recovered set must keep its pin")
	}
	if byID[recovered.String()].QuotaPinned {
		t.Error("a provider in the recovered set must lose its pin")
	}
	if got := byID[stillSpent.String()].CooldownMs; got < (6*time.Hour - time.Minute).Milliseconds() {
		t.Errorf("got CooldownMs=%d for the still-exhausted provider, want the ~6h pin intact", got)
	}
}

// TestReleaseQuotaPins_DoesNotCloseTheCircuit is the contract boundary: quota
// never opens a circuit, never closes one, and never blocks a request — it only
// chooses the cooldown of an already-open circuit. Lifting a pin must therefore
// leave the circuit open and let HTTP decide recovery through the ordinary
// half-open probe. Asserting only on the cooldown would not catch an
// implementation that "helpfully" closed the circuit outright.
func TestReleaseQuotaPins_DoesNotCloseTheCircuit(t *testing.T) {
	cb := NewCircuitBreaker(&stubSettings{threshold: 1, cooldown: time.Hour})
	id := uuid.New()
	cb.SetQuotaAdvisor(stubAdvisor{at: time.Now().Add(6 * time.Hour), ok: true})

	openBreaker(t, cb, id)

	cb.ReleaseQuotaPins(map[uuid.UUID]struct{}{id: {}})

	if got := cb.GetState(id); got != StateOpen {
		t.Errorf("got state %v after lifting the pin, want open — quota must never close a circuit", got)
	}
	if !cb.IsOpen(id, "test-provider") {
		t.Error("the configured cooldown has not elapsed; the circuit must still refuse traffic")
	}
	if s := cb.Status()[0]; s.ConsecutiveFails == 0 {
		t.Error("lifting a pin must not reset the failure count that opened the circuit")
	}
}

// TestReleaseQuotaPins_LeavesUnpinnedCircuitsAlone verifies the release is
// confined to circuits actually carrying an override: an ordinary open circuit
// (no quota advice at all) must be untouched, so a recovering provider
// elsewhere in the fleet cannot perturb it.
func TestReleaseQuotaPins_LeavesUnpinnedCircuitsAlone(t *testing.T) {
	cb := NewCircuitBreaker(&stubSettings{threshold: 1, cooldown: time.Hour})
	id := uuid.New()
	cb.SetQuotaAdvisor(stubAdvisor{ok: false})

	openBreaker(t, cb, id)

	if released := cb.ReleaseQuotaPins(map[uuid.UUID]struct{}{id: {}}); released != 0 {
		t.Errorf("got %d pin(s) released, want 0 — nothing was pinned", released)
	}
	s := cb.Status()[0]
	if s.State != StateOpen.String() {
		t.Errorf("got state %q, want open", s.State)
	}
	if s.CooldownMs != time.Hour.Milliseconds() {
		t.Errorf("got CooldownMs=%d, want the configured hour untouched", s.CooldownMs)
	}
}

// TestReleaseQuotaPins_KeepsThePinWithoutRecoveryEvidence is the asymmetry the
// whole release rule turns on. A provider is absent from the recovered set for
// three different reasons — its snapshot is stale, its payload could not be
// assessed, or it has no snapshot at all — and none of them is evidence of
// recovery. Releasing on absence would unpin exactly the provider whose quota
// fetches are broken, i.e. the one whose window is most likely still spent, so
// absence must leave the pin exactly as it was.
func TestReleaseQuotaPins_KeepsThePinWithoutRecoveryEvidence(t *testing.T) {
	cb := NewCircuitBreaker(&stubSettings{threshold: 1, cooldown: time.Second})
	id := uuid.New()
	cb.SetQuotaAdvisor(stubAdvisor{at: time.Now().Add(6 * time.Hour), ok: true})

	openBreaker(t, cb, id)
	if !cb.Status()[0].QuotaPinned {
		t.Fatal("setup: a 6h deadline against a 1s cooldown must pin the circuit")
	}

	// A refresh that recovered somebody else entirely (or nobody at all) says
	// nothing about this provider.
	if released := cb.ReleaseQuotaPins(map[uuid.UUID]struct{}{uuid.New(): {}}); released != 0 {
		t.Errorf("got %d pin(s) released, want 0 — no fresh evidence for this provider", released)
	}

	s := cb.Status()[0]
	if !s.QuotaPinned {
		t.Error("a provider with no fresh recovery evidence must keep its pin")
	}
	if got := s.CooldownMs; got < (6*time.Hour - time.Minute).Milliseconds() {
		t.Errorf("got CooldownMs=%d, want the ~6h pin intact", got)
	}
}

// TestReleaseQuotaPins_EmptyBreakerIsANoOp covers the common case: the poll runs
// every few minutes against a fleet with no open circuits at all.
func TestReleaseQuotaPins_EmptyBreakerIsANoOp(t *testing.T) {
	cb := NewCircuitBreaker(nil)
	if released := cb.ReleaseQuotaPins(map[uuid.UUID]struct{}{uuid.New(): {}}); released != 0 {
		t.Errorf("got %d pin(s) released on an empty breaker, want 0", released)
	}
	if len(cb.Status()) != 0 {
		t.Error("releasing pins must not create circuits")
	}
}

// TestReleaseQuotaPins_LogsTheLiftedPin covers the operator's log trail. A pin
// being applied is logged when the circuit opens; a pin ending early is just as
// operator-relevant, and without a line for it the provider silently returns to
// rotation hours before the "cooldown_ms" already in the log said it would.
func TestReleaseQuotaPins_LogsTheLiftedPin(t *testing.T) {
	cb := NewCircuitBreaker(&stubSettings{threshold: 1, cooldown: time.Hour})
	id := uuid.New()
	cb.SetQuotaAdvisor(stubAdvisor{at: time.Now().Add(6 * time.Hour), ok: true})

	openBreaker(t, cb, id)

	capt := captureLogs(t)

	cb.ReleaseQuotaPins(map[uuid.UUID]struct{}{id: {}})

	level, attrs, found := capt.last("circuit-breaker: quota pin released (provider no longer exhausted)")
	if !found {
		t.Fatal("lifting a pin must leave a log trail")
	}
	if level != slog.LevelInfo {
		t.Errorf("got level %v, want info", level)
	}
	if got, _ := attrs["provider_id"].(string); got != id.String() {
		t.Errorf("got provider_id=%v, want %s", attrs["provider_id"], id)
	}
	if got, _ := attrs["cooldown_ms"].(int64); got != time.Hour.Milliseconds() {
		t.Errorf("got cooldown_ms=%v, want the configured cooldown %d", attrs["cooldown_ms"], time.Hour.Milliseconds())
	}
}

// TestReleaseAllQuotaPins_LiftsEveryPin covers the "we have stopped looking"
// lever. Quota polling being switched off means no refresh will ever report a
// recovery again, so every pin still in force would be served out to its ceiling
// on evidence the operator deliberately stopped collecting. Holding a healthy
// provider out is the expensive direction, so when the gateway stops advising it
// stops holding too.
func TestReleaseAllQuotaPins_LiftsEveryPin(t *testing.T) {
	cb := NewCircuitBreaker(&stubSettings{threshold: 1, cooldown: 300 * time.Millisecond})
	pinnedA, pinnedB, unpinned := uuid.New(), uuid.New(), uuid.New()

	cb.SetQuotaAdvisor(stubAdvisor{at: time.Now().Add(6 * time.Hour), ok: true})
	openBreaker(t, cb, pinnedA)
	openBreaker(t, cb, pinnedB)
	cb.SetQuotaAdvisor(stubAdvisor{ok: false})
	openBreaker(t, cb, unpinned)

	if released := cb.ReleaseAllQuotaPins(); released != 2 {
		t.Errorf("got %d pin(s) released, want 2 — every pin, and only the pins", released)
	}

	for _, s := range cb.Status() {
		if s.QuotaPinned {
			t.Errorf("provider %s still reports quota_pinned after a release-all", s.ProviderID)
		}
		if s.CooldownMs != cb.effectiveCooldown().Milliseconds() {
			t.Errorf("provider %s: got CooldownMs=%d, want the configured cooldown %d back",
				s.ProviderID, s.CooldownMs, cb.effectiveCooldown().Milliseconds())
		}
	}

	// A second call has nothing left to lift: the release is idempotent, which
	// is what lets the caller run it once per disabled span without bookkeeping.
	if released := cb.ReleaseAllQuotaPins(); released != 0 {
		t.Errorf("got %d pin(s) released on the second call, want 0", released)
	}

	time.Sleep(400 * time.Millisecond)
	if cb.IsOpen(pinnedA, "test-provider") {
		t.Error("with the pin lifted, the configured cooldown must let a probe through")
	}
}

// TestReleaseAllQuotaPins_DoesNotCloseCircuits holds the same contract boundary
// as the recovered-set release: quota chooses cooldowns, nothing else. A blunt
// release-all must not be a back door to closing circuits.
func TestReleaseAllQuotaPins_DoesNotCloseCircuits(t *testing.T) {
	cb := NewCircuitBreaker(&stubSettings{threshold: 1, cooldown: time.Hour})
	id := uuid.New()
	cb.SetQuotaAdvisor(stubAdvisor{at: time.Now().Add(6 * time.Hour), ok: true})

	openBreaker(t, cb, id)
	cb.ReleaseAllQuotaPins()

	if got := cb.GetState(id); got != StateOpen {
		t.Errorf("got state %v after releasing every pin, want open", got)
	}
	if !cb.IsOpen(id, "test-provider") {
		t.Error("the configured cooldown has not elapsed; the circuit must still refuse traffic")
	}
	if s := cb.Status()[0]; s.ConsecutiveFails == 0 {
		t.Error("releasing pins must not reset the failure count that opened the circuit")
	}
}

func TestReleaseAllQuotaPins_EmptyBreakerIsANoOp(t *testing.T) {
	cb := NewCircuitBreaker(nil)
	if released := cb.ReleaseAllQuotaPins(); released != 0 {
		t.Errorf("got %d pin(s) released on an empty breaker, want 0", released)
	}
	if len(cb.Status()) != 0 {
		t.Error("releasing pins must not create circuits")
	}
}

// TestReleaseAllQuotaPins_LogsTheReason keeps the two releases distinguishable
// in the log. An operator reading "pin released" needs to know whether the
// provider recovered or whether they switched the poller off, because only one
// of those means the window is actually back.
func TestReleaseAllQuotaPins_LogsTheReason(t *testing.T) {
	cb := NewCircuitBreaker(&stubSettings{threshold: 1, cooldown: time.Hour})
	id := uuid.New()
	cb.SetQuotaAdvisor(stubAdvisor{at: time.Now().Add(6 * time.Hour), ok: true})

	openBreaker(t, cb, id)

	capt := captureLogs(t)
	cb.ReleaseAllQuotaPins()

	level, attrs, found := capt.last("circuit-breaker: quota pin released (quota polling disabled)")
	if !found {
		t.Fatal("releasing every pin must leave a log trail of its own")
	}
	if level != slog.LevelInfo {
		t.Errorf("got level %v, want info", level)
	}
	if got, _ := attrs["provider_id"].(string); got != id.String() {
		t.Errorf("got provider_id=%v, want %s", attrs["provider_id"], id)
	}
	if got, _ := attrs["cooldown_ms"].(int64); got != time.Hour.Milliseconds() {
		t.Errorf("got cooldown_ms=%v, want the configured cooldown %d", attrs["cooldown_ms"], time.Hour.Milliseconds())
	}
}

// TestPublishEvent_CarriesQuotaPinMetadata verifies that when a circuit opens
// with an active quota pin, the published event's Metadata carries
// quota_pinned=true and next_retry_at.
//
// The field is next_retry_at, not resets_at, and the assertion is that it equals
// the status API's NextRetryAt for the same circuit — because that is what it is.
// It carries openedAt plus the ceiling-clamped, jittered cooldown, so on a weekly
// plan pinned at the 24h ceiling it says "tomorrow" while the quota resets in
// five days. Calling one instant by two names across two surfaces of one feature
// is how a consumer ends up reporting a reset deadline that is not one.
func TestPublishEvent_CarriesQuotaPinMetadata(t *testing.T) {
	sub := events.Subscribe()
	defer events.Unsubscribe(sub)

	cb := NewCircuitBreaker(nil)
	id := uuid.New()
	reset := time.Now().Add(3 * time.Hour)
	cb.SetQuotaAdvisor(stubAdvisor{at: reset, ok: true})

	openBreaker(t, cb, id)

	ev := waitForOpenEvent(t, sub, id)

	pinned, _ := ev.Metadata["quota_pinned"].(bool)
	if !pinned {
		t.Fatalf("pinned circuit must publish quota_pinned=true, got metadata %#v", ev.Metadata)
	}
	if v, ok := ev.Metadata["resets_at"]; ok {
		t.Fatalf("resets_at named the retry time, not the quota reset, and is gone; got %#v", v)
	}
	nextRetryAt, ok := ev.Metadata["next_retry_at"].(string)
	if !ok {
		t.Fatalf("pinned circuit must publish next_retry_at as a string, got metadata %#v", ev.Metadata)
	}
	if _, err := time.Parse(time.RFC3339, nextRetryAt); err != nil {
		t.Fatalf("next_retry_at %q is not RFC3339: %v", nextRetryAt, err)
	}
	if want := cb.Status()[0].NextRetryAt; nextRetryAt != want {
		t.Errorf("event next_retry_at=%q, want the same instant the status API publishes under that name (%q)", nextRetryAt, want)
	}
}

// TestApplyQuotaPins_RetargetsOpenCircuit is the whole point of the re-pin: a
// circuit that opened before the exhaustion was known carries an ordinary
// cooldown, and the reading that arrives seconds later must move its retry
// instant out to the real reset rather than waiting for the circuit to open a
// second time.
//
// The circuit is backdated so the assertion can tell the two candidate
// computations apart. The breaker enforces a cooldown measured from openedAt,
// so a pin derived from "time until reset" expires early by however long the
// circuit has already been open and probes before the window rolls over. The
// backdate has to clear the 5% positive jitter to be decisive, hence two hours
// against a six-hour reset, and the configured cooldown has to outlast the
// backdate or the circuit would read as half-open and be skipped by design.
func TestApplyQuotaPins_RetargetsOpenCircuit(t *testing.T) {
	cb := newTestCB(1, 3*time.Hour)
	id := uuid.New()

	cb.RecordFailure(id, "test-provider") // opens unpinned: no advice existed yet
	if got := overrideFor(t, cb, id); got != 0 {
		t.Fatalf("setup: got override %v, want an unpinned circuit", got)
	}
	backdateOpen(t, cb, id, 2*time.Hour)

	resetsAt := time.Now().Add(6 * time.Hour).Truncate(time.Second)
	if got := cb.ApplyQuotaPins(map[uuid.UUID]time.Time{id: resetsAt}); got != 1 {
		t.Fatalf("got %d circuits retargeted, want 1", got)
	}

	s := onlyStatus(t, cb)
	if !s.QuotaPinned {
		t.Error("a retargeted circuit must report quota_pinned")
	}
	next, err := time.Parse(time.RFC3339, s.NextRetryAt)
	if err != nil {
		t.Fatalf("parse next_retry_at %q: %v", s.NextRetryAt, err)
	}
	if next.Before(resetsAt) {
		t.Errorf("next retry %s falls before the quota reset %s: the pin must be measured from openedAt, not from now", next, resetsAt)
	}
	// Positive-only jitter is capped at 5% of the pin, which spans openedAt to
	// the reset, so the retry cannot land arbitrarily far past it either.
	if latest := resetsAt.Add(8 * time.Hour / 20); next.After(latest) {
		t.Errorf("next retry %s lands past %s, further than jitter allows", next, latest)
	}
}

// TestApplyQuotaPins_LeavesNonOpenCircuitsAlone verifies the re-pin only touches
// circuits that are actually holding traffic back. A closed circuit has no
// cooldown to retarget, and a half-open one has a probe out or due, so HTTP is
// mid-verdict: pushing it back into the dark would overturn a decision the
// breaker has already handed to the request path.
func TestApplyQuotaPins_LeavesNonOpenCircuitsAlone(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T, cb *CircuitBreaker, id uuid.UUID)
		why   string
	}{
		{
			name: "closed",
			setup: func(_ *testing.T, cb *CircuitBreaker, id uuid.UUID) {
				cb.RecordFailure(id, "test-provider") // one short of the threshold
			},
			why: "a closed circuit is serving traffic and has no cooldown to retarget",
		},
		{
			name: "half-open probe out",
			setup: func(t *testing.T, cb *CircuitBreaker, id uuid.UUID) {
				cb.RecordFailure(id, "test-provider")
				cb.RecordFailure(id, "test-provider") // opens
				backdateOpen(t, cb, id, time.Second)
				cb.IsOpen(id, "test-provider") // cooldown elapsed: hands out a probe
			},
			why: "a probe is in flight and HTTP is about to decide",
		},
		{
			name: "cooldown elapsed",
			setup: func(t *testing.T, cb *CircuitBreaker, id uuid.UUID) {
				cb.RecordFailure(id, "test-provider")
				cb.RecordFailure(id, "test-provider") // opens
				backdateOpen(t, cb, id, time.Second)  // probe due, reads as half-open
			},
			why: "the cooldown already elapsed, so the circuit is due a probe",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cb := newTestCB(2, 50*time.Millisecond)
			id := uuid.New()
			c.setup(t, cb, id)

			got := cb.ApplyQuotaPins(map[uuid.UUID]time.Time{id: time.Now().Add(6 * time.Hour)})

			if got != 0 {
				t.Errorf("got %d circuits retargeted, want 0: %s", got, c.why)
			}
			if o := overrideFor(t, cb, id); o != 0 {
				t.Errorf("got override %v, want none: %s", o, c.why)
			}
		})
	}
}

// TestApplyQuotaPins_NeverShortensExistingPin verifies the re-pin only ever
// lengthens a wait. Releasing a pin needs affirmative proof the provider
// recovered, which is ReleaseQuotaPins' job; a shorter deadline arriving here
// is not that proof.
func TestApplyQuotaPins_NeverShortensExistingPin(t *testing.T) {
	cb := newTestCB(1, 60*time.Second)
	id := uuid.New()
	cb.SetQuotaAdvisor(stubAdvisor{at: time.Now().Add(6 * time.Hour), ok: true})

	cb.RecordFailure(id, "test-provider") // opens pinned to ~6h
	before := overrideFor(t, cb, id)
	if before == 0 {
		t.Fatal("setup: expected the open transition to pin the circuit")
	}

	if got := cb.ApplyQuotaPins(map[uuid.UUID]time.Time{id: time.Now().Add(time.Hour)}); got != 0 {
		t.Errorf("got %d circuits retargeted, want 0 for a nearer deadline", got)
	}
	if after := overrideFor(t, cb, id); after != before {
		t.Errorf("got override %v, want the longer existing pin %v left alone", after, before)
	}
}

// TestApplyQuotaPins_SkipsWhenPinDisabled verifies the operator kill switch
// covers this path too. Status would hide the override anyway, so the stored
// value is checked directly: a circuit must not carry a pin an operator has
// switched off, or re-enabling would resurrect deadlines from a disabled span.
func TestApplyQuotaPins_SkipsWhenPinDisabled(t *testing.T) {
	disabled := false
	cb := NewCircuitBreaker(&stubSettings{threshold: 1, cooldown: 60 * time.Second, pinEnabled: &disabled})
	id := uuid.New()

	cb.RecordFailure(id, "test-provider")

	if got := cb.ApplyQuotaPins(map[uuid.UUID]time.Time{id: time.Now().Add(6 * time.Hour)}); got != 0 {
		t.Errorf("got %d circuits retargeted, want 0 while quota pinning is disabled", got)
	}
	if o := overrideFor(t, cb, id); o != 0 {
		t.Errorf("got override %v, want none while quota pinning is disabled", o)
	}
}

// TestApplyQuotaPins_CeilingCapsRunawayDeadline verifies the re-pin obeys the
// same ceiling the open-time pin does, so a nonsense deadline cannot bench a
// provider indefinitely.
func TestApplyQuotaPins_CeilingCapsRunawayDeadline(t *testing.T) {
	// A cooldown long enough that scheduling delay cannot age the circuit into
	// half-open before ApplyQuotaPins reads it; still far below the 1h ceiling
	// under test, so the pin is a genuine lengthening.
	cb := NewCircuitBreaker(&stubSettings{threshold: 1, cooldown: 60 * time.Second, pinMax: time.Hour})
	id := uuid.New()

	cb.RecordFailure(id, "test-provider")

	if got := cb.ApplyQuotaPins(map[uuid.UUID]time.Time{id: time.Now().Add(500 * 24 * time.Hour)}); got != 1 {
		t.Fatalf("got %d circuits retargeted, want 1", got)
	}
	ceiling := time.Hour
	if o := overrideFor(t, cb, id); o > ceiling+ceiling/20 {
		t.Errorf("got override %v, want capped near the settings max %v", o, ceiling)
	}
}
