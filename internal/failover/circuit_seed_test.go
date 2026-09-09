package failover

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// Seeded quota pins: the circuit a measured exhaustion reading opens on its
// own, with no request behind it. The breaker is in-memory, so after a restart
// (or on a member that has never served the provider) every circuit is closed
// while the stored snapshot already says the window is spent. These tests fix
// the properties that make the badge and the breaker agree without spending a
// request to find out.

// seededCircuit returns the seeded circuit of one provider, or nil.
func seededCircuit(cb *CircuitBreaker, id uuid.UUID) *circuit {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.circuits[id.String()][seededModel]
}

// TestSeedQuotaPin_OpensClosedCircuitAndDarkensEveryModel is the whole point:
// a provider the breaker has never heard of is skipped for every model, not
// just for one the reading happens to name, and the wait is the advisor's
// measured reset rather than the configured cooldown.
func TestSeedQuotaPin_OpensClosedCircuitAndDarkensEveryModel(t *testing.T) {
	cb := newTestCB(3, time.Minute)
	id := uuid.New()
	reset := time.Now().Add(6 * time.Hour)

	cb.ApplyQuotaPins(map[uuid.UUID]time.Time{id: reset})

	// Never requested, never failed: the derived provider verdict is the only
	// thing that can be skipping it.
	if !cb.IsOpen(id, "test-provider", "a-model-nothing-ever-asked-for") {
		t.Error("a seeded pin must darken a model the breaker has never seen")
	}

	statuses := cb.StatusDetail()
	if len(statuses) != 1 {
		t.Fatalf("got %d statuses, want 1", len(statuses))
	}
	s := statuses[0]
	if !s.QuotaPinned || s.PinSource != pinSourceAdvisor {
		t.Errorf("got quota_pinned=%v pin_source=%q, want true/advisor", s.QuotaPinned, s.PinSource)
	}
	if !s.ProviderOpen {
		t.Error("a seeded pin speaks for the account, so the provider verdict must be open")
	}
	// Roughly 6h: computed from time.Until(reset), plus positive-only jitter
	// capped at 5%. A second of slack at the bottom for the elapsed test time.
	minMs := (6*time.Hour - time.Second).Milliseconds()
	maxMs := (6 * time.Hour).Milliseconds() * 21 / 20
	if s.CooldownMs < minMs || s.CooldownMs > maxMs {
		t.Errorf("got CooldownMs=%d, want within [%d,%d]", s.CooldownMs, minMs, maxMs)
	}
	if len(s.Circuits) != 1 || s.Circuits[0].Model != seededModel {
		t.Fatalf("got circuits %+v, want one seeded circuit", s.Circuits)
	}
	if s.Circuits[0].LastCause != causeSeeded {
		t.Errorf("got last_cause=%q, want %q", s.Circuits[0].LastCause, causeSeeded)
	}
}

// TestSeedQuotaPin_CeilingApplies proves the seeded pin is bounded by the same
// operator lever every other pin is: a weekly reset must not hold a provider
// dark past circuit_breaker_quota_pin_max.
func TestSeedQuotaPin_CeilingApplies(t *testing.T) {
	cb := NewCircuitBreaker(&stubSettings{cooldown: time.Minute, pinMax: 2 * time.Hour})
	id := uuid.New()

	cb.ApplyQuotaPins(map[uuid.UUID]time.Time{id: time.Now().Add(7 * 24 * time.Hour)})

	s := cb.Status()[0]
	// The ceiling is a pre-jitter cap, so up to 5% above it is expected.
	if maxMs := (2 * time.Hour).Milliseconds() * 21 / 20; s.CooldownMs > maxMs {
		t.Errorf("got CooldownMs=%d, want at most %d (pin ceiling)", s.CooldownMs, maxMs)
	}
	if s.CooldownMs < (2 * time.Hour).Milliseconds() {
		t.Errorf("got CooldownMs=%d, want the full ceiling", s.CooldownMs)
	}
}

// TestSeedQuotaPin_PinningOffSeedsNothing: the ceiling is the kill switch, and
// an operator who turned pinning off must not get a circuit opened on a quota
// reading either.
func TestSeedQuotaPin_PinningOffSeedsNothing(t *testing.T) {
	cb := NewCircuitBreaker(&stubSettings{cooldown: time.Minute, pinOff: true})
	id := uuid.New()

	cb.ApplyQuotaPins(map[uuid.UUID]time.Time{id: time.Now().Add(6 * time.Hour)})
	if seededCircuit(cb, id) != nil {
		t.Fatal("pinning is off: no circuit may be seeded")
	}
	if cb.IsOpen(id, "test-provider", "some-model") {
		t.Error("pinning is off: nothing may be seeded")
	}
}

// TestSeedQuotaPin_UndatableAdviceSeedsNothing. buildQuotaAdvice only ever
// emits a datable future reset, but the breaker must not depend on that: a zero
// or already-elapsed deadline is no measurement and cannot clear the floor.
func TestSeedQuotaPin_UndatableAdviceSeedsNothing(t *testing.T) {
	for _, tc := range []struct {
		name  string
		reset time.Time
	}{
		{"no deadline at all", time.Time{}},
		{"a window that has already rolled over", time.Now().Add(-time.Hour)},
		{"a reset inside the configured cooldown", time.Now().Add(10 * time.Second)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cb := newTestCB(3, time.Minute)
			id := uuid.New()

			cb.ApplyQuotaPins(map[uuid.UUID]time.Time{id: tc.reset})
			if seededCircuit(cb, id) != nil {
				t.Error("no measurable window: nothing may be seeded")
			}
		})
	}
}

// TestSeedQuotaPin_AlreadyPinnedProviderIsRetargetedNotSeeded keeps the
// pre-existing path intact: a circuit a request opened is retargeted the way it
// always was, and no second circuit appears beside it.
func TestSeedQuotaPin_AlreadyPinnedProviderIsRetargetedNotSeeded(t *testing.T) {
	cb := newTestCB(3, time.Minute)
	id := uuid.New()

	// An exhausted 429 that spoke for the account: opens "gemma-4-31b" with a
	// pin that already darkens the provider.
	cb.RecordExhaustedAccount(id, "test-provider", "gemma-4-31b", 402, time.Hour)

	if n := cb.ApplyQuotaPins(map[uuid.UUID]time.Time{id: time.Now().Add(6 * time.Hour)}); n != 1 {
		t.Fatalf("got %d circuits changed, want 1 retarget", n)
	}
	if seededCircuit(cb, id) != nil {
		t.Error("a provider already pinned account-wide needs no seeded circuit")
	}
	s := cb.StatusDetail()[0]
	if len(s.Circuits) != 1 || s.Circuits[0].Model != "gemma-4-31b" {
		t.Errorf("got circuits %+v, want only the retargeted one", s.Circuits)
	}
	if s.Circuits[0].LastCause != causePinRetargeted {
		t.Errorf("got last_cause=%q, want %q", s.Circuits[0].LastCause, causePinRetargeted)
	}
}

// TestSeedQuotaPin_SecondPassDoesNotReseed: the poller runs every few minutes
// and hands the same advice back. A seeded circuit must satisfy the check that
// created it, or every pass would stack another change.
func TestSeedQuotaPin_SecondPassDoesNotReseed(t *testing.T) {
	cb := newTestCB(3, time.Minute)
	id := uuid.New()
	advice := map[uuid.UUID]time.Time{id: time.Now().Add(6 * time.Hour)}

	cb.ApplyQuotaPins(advice)
	cb.ApplyQuotaPins(advice)
	cb.mu.RLock()
	got := len(cb.circuits[id.String()])
	cb.mu.RUnlock()
	if got != 1 {
		t.Errorf("got %d circuits, want 1", got)
	}
}

// TestSeedQuotaPin_ReleasedOnRecovery is the reverse the user's requirement
// asks for: a snapshot that reads healthy again lifts the seeded pin through
// the same path that lifts every other one, and retires the circuit with it,
// so the provider is routed to on the next request.
func TestSeedQuotaPin_ReleasedOnRecovery(t *testing.T) {
	cb := newTestCB(3, time.Minute)
	id := uuid.New()

	cb.ApplyQuotaPins(map[uuid.UUID]time.Time{id: time.Now().Add(6 * time.Hour)})
	if !cb.IsOpen(id, "test-provider", "any-model") {
		t.Fatal("setup: seeded provider must be open")
	}

	if n := cb.ReleaseQuotaPins(map[uuid.UUID]struct{}{id: {}}); n != 1 {
		t.Fatalf("got %d pins released, want 1", n)
	}
	if cb.IsOpen(id, "test-provider", "any-model") {
		t.Error("a recovered provider must be routed to again")
	}
	if seededCircuit(cb, id) != nil {
		t.Error("a seeded circuit exists only to carry its pin: releasing it must retire the circuit")
	}
}

// TestSeedQuotaPin_ReleasedWhenPollingSwitchedOff. Switching quota polling off
// means nothing will ever report a recovery again, so the blunt release must
// retire seeded circuits too rather than leave them dark to the ceiling.
func TestSeedQuotaPin_ReleasedWhenPollingSwitchedOff(t *testing.T) {
	cb := newTestCB(3, time.Minute)
	id := uuid.New()

	cb.ApplyQuotaPins(map[uuid.UUID]time.Time{id: time.Now().Add(6 * time.Hour)})
	if n := cb.ReleaseAllQuotaPins(); n != 1 {
		t.Fatalf("got %d pins released, want 1", n)
	}
	if seededCircuit(cb, id) != nil {
		t.Error("seeded circuit must be retired when polling is switched off")
	}
	if cb.IsOpen(id, "test-provider", "any-model") {
		t.Error("provider must be routed to again once its seeded pin is gone")
	}
}

// TestSeedQuotaPin_LeavesOtherProvidersAlone: the advice map is per provider,
// and a seeded circuit must not reach a provider nobody advised about.
func TestSeedQuotaPin_LeavesOtherProvidersAlone(t *testing.T) {
	cb := newTestCB(3, time.Minute)
	spent, healthy := uuid.New(), uuid.New()

	cb.ApplyQuotaPins(map[uuid.UUID]time.Time{spent: time.Now().Add(6 * time.Hour)})

	if cb.IsOpen(healthy, "other-provider", "some-model") {
		t.Error("an unadvised provider must not be darkened")
	}
	if seededCircuit(cb, healthy) != nil {
		t.Error("an unadvised provider must get no seeded circuit")
	}
}

// TestSeedQuotaPin_ExpiredSeedIsRetired covers natural expiry, the one exit a
// seeded circuit cannot take by itself. The provider's snapshot goes stale, or
// stops being assessable, without ever reading healthy: it leaves the advice
// map with the pin still stamped, and absence is not affirmative recovery, so
// ReleaseQuotaPins never sees it. Nothing routes to the seeded model either, so
// no probe closes it and eviction passes over it while it reads open. The next
// advice pass has to retire it, or the row sits half-open for the life of the
// process.
func TestSeedQuotaPin_ExpiredSeedIsRetired(t *testing.T) {
	cb := newTestCB(3, time.Minute)
	id := uuid.New()

	cb.ApplyQuotaPins(map[uuid.UUID]time.Time{id: time.Now().Add(6 * time.Hour)})
	c := seededCircuit(cb, id)
	if c == nil {
		t.Fatal("setup: the reading must seed a circuit")
	}

	// The clock moves past the reset the pin was measured to.
	cb.mu.Lock()
	c.openedAt = c.openedAt.Add(-7 * time.Hour)
	cb.mu.Unlock()

	// No advice for this provider at all, which is also the pass that would
	// otherwise return early: the sweep runs before that return or a provider
	// that dropped out of the advice is never revisited.
	cb.ApplyQuotaPins(nil)

	if seededCircuit(cb, id) != nil {
		t.Error("an elapsed seeded pin must retire its circuit: nothing else ever can")
	}
	if len(cb.Status()) != 0 {
		t.Errorf("got %d breaker rows, want none: the retired seed must leave no half-open row behind", len(cb.Status()))
	}
	if cb.IsOpen(id, "test-provider", "any-model") {
		t.Error("a provider whose seeded pin elapsed must be routed to again")
	}
}
