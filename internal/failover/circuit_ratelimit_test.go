package failover

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// The 429 saturation-vs-exhaustion contract on the breaker side:
// RecordExhausted opens on ONE charge and pins from the response's own hint
// (advisor always beating it), RecordRateLimited feeds the 429-only open
// streak whose third open lifts the backoff ceiling to the quota-pin ceiling,
// and LastSuccessWithin backs the proxy's behavioural fallback. The saturated
// no-op is deliberately NOT here: saturated means the breaker is never called,
// which the proxy-side recordBreakerOutcome tests pin.

// exhaustedCircuit reads the one circuit these tests drive.
func exhaustedCircuit(t *testing.T, cb *CircuitBreaker, id uuid.UUID, model string) *circuit {
	t.Helper()
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	c, ok := cb.circuits[id.String()][model]
	if !ok {
		t.Fatalf("no circuit tracked for %s/%q", id, model)
	}
	return c
}

func TestRecordExhausted_OpensOnOneCharge(t *testing.T) {
	cb := NewCircuitBreaker(&stubSettings{threshold: 5, cooldown: time.Minute})
	id := uuid.New()

	cb.RecordExhausted(id, "p", "m", 429, 0)

	if got := cb.GetState(id, "m"); got != StateOpen {
		t.Fatalf("state after one exhausted 429 = %v, want open", got)
	}
	if fails := exhaustedCircuit(t, cb, id, "m").consecutiveFails; fails != 5 {
		t.Errorf("consecutiveFails = %d, want the threshold (5), the same jump the half-open path makes", fails)
	}
}

// The rate-limit-open streak is documented as 429-only, and escalating it
// raises the probe-backoff ceiling into a BACKOFF — which no quota lever
// clears, since ReleaseQuotaPins and circuit_breaker_quota_pin_enabled=false
// both only ever clear a pin. A 402 is an exhaustion but not a rate limit, so
// counting it would strand a provider at the ceiling with no operator lever
// short of a manual reset.
func TestRecordExhausted_Only429FeedsTheOpenStreak(t *testing.T) {
	cb := NewCircuitBreaker(&stubSettings{threshold: 5, cooldown: time.Minute})
	id := uuid.New()

	cb.RecordExhausted(id, "p", "rate-limited", 429, 0)
	if got := exhaustedCircuit(t, cb, id, "rate-limited").opens429Streak; got != 1 {
		t.Errorf("streak after an exhausted 429 = %d, want 1", got)
	}

	cb.RecordExhausted(id, "p", "unpaid", 402, 0)
	if got := exhaustedCircuit(t, cb, id, "unpaid").opens429Streak; got != 0 {
		t.Errorf("streak after a payment-required 402 = %d, want 0: it is not a rate limit", got)
	}
}

func TestRecordExhausted_HintPinStampedWithJitterBounds(t *testing.T) {
	cb := NewCircuitBreaker(&stubSettings{threshold: 1, cooldown: time.Minute, pinMax: 24 * time.Hour})
	id := uuid.New()

	hint := 30 * time.Minute
	cb.RecordExhausted(id, "p", "m", 429, hint)

	got := overrideForModel(t, cb, id, "m")
	if got < hint || got > hint+hint/20 {
		t.Errorf("hint pin = %v, want within [%v, %v] (positive jitter of d/20)", got, hint, hint+hint/20)
	}
	s := onlyStatus(t, cb)
	if !s.QuotaPinned {
		t.Error("status does not report the hint pin as quota_pinned")
	}
	if s.PinSource != "response" {
		t.Errorf("pin_source = %q, want %q for a pin inferred from the response", s.PinSource, "response")
	}
}

func TestRecordExhausted_HintClampedToPinCeiling(t *testing.T) {
	cb := NewCircuitBreaker(&stubSettings{threshold: 1, cooldown: time.Minute, pinMax: time.Hour})
	id := uuid.New()

	// The "until somebody pays" sentinel: far above any ceiling on purpose.
	cb.RecordExhausted(id, "p", "m", 429, 90*24*time.Hour)

	got := overrideForModel(t, cb, id, "m")
	ceiling := time.Hour
	if got < ceiling || got > ceiling+ceiling/20 {
		t.Errorf("clamped hint pin = %v, want within [%v, %v]", got, ceiling, ceiling+ceiling/20)
	}
}

func TestRecordExhausted_HintBelowCooldownIgnored(t *testing.T) {
	cb := NewCircuitBreaker(&stubSettings{threshold: 1, cooldown: time.Hour})
	id := uuid.New()

	cb.RecordExhausted(id, "p", "m", 429, 30*time.Minute)

	if got := overrideForModel(t, cb, id, "m"); got != 0 {
		t.Errorf("override = %v, want 0: a pin must never make the breaker more aggressive than its cooldown", got)
	}
	if s := onlyStatus(t, cb); s.PinSource != "" {
		t.Errorf("pin_source = %q, want empty when no pin was stamped", s.PinSource)
	}
}

func TestRecordExhausted_AdvisorBeatsHint(t *testing.T) {
	cb := NewCircuitBreaker(&stubSettings{threshold: 1, cooldown: time.Minute, pinMax: 24 * time.Hour})
	advisorReset := 2 * time.Hour
	cb.SetQuotaAdvisor(stubAdvisor{at: time.Now().Add(advisorReset), ok: true})
	id := uuid.New()

	cb.RecordExhausted(id, "p", "m", 429, 30*time.Minute)

	got := overrideForModel(t, cb, id, "m")
	// The advisor's window, not the 30m hint: allow the jitter and the
	// time.Until skew.
	if got < advisorReset-time.Minute || got > advisorReset+advisorReset/20+time.Minute {
		t.Errorf("override = %v, want the advisor's ~%v, not the response hint", got, advisorReset)
	}
	if s := onlyStatus(t, cb); s.PinSource != "advisor" {
		t.Errorf("pin_source = %q, want %q: a measured reading beats an inferred one", s.PinSource, "advisor")
	}
}

func TestRecordExhausted_ReleaseQuotaPinsLiftsHintPin(t *testing.T) {
	cb := NewCircuitBreaker(&stubSettings{threshold: 1, cooldown: time.Minute, pinMax: 24 * time.Hour})
	id := uuid.New()
	cb.RecordExhausted(id, "p", "m", 429, 30*time.Minute)
	if overrideForModel(t, cb, id, "m") == 0 {
		t.Fatal("setup: no hint pin stamped")
	}

	released := cb.ReleaseQuotaPins(map[uuid.UUID]struct{}{id: {}})

	if released != 1 {
		t.Errorf("released = %d, want 1", released)
	}
	if got := overrideForModel(t, cb, id, "m"); got != 0 {
		t.Errorf("override after release = %v, want 0: a hint pin lifts exactly as an advisor pin does", got)
	}
}

func TestRecordExhausted_HalfOpenProbeCountsAsFailedProbe(t *testing.T) {
	cb := NewCircuitBreaker(&stubSettings{threshold: 1, cooldown: time.Minute})
	id := uuid.New()
	cb.RecordExhausted(id, "p", "m", 429, 0)
	backdateOpenModel(t, cb, id, "m", 2*time.Minute)
	if cb.IsOpen(id, "p", "m") {
		t.Fatal("setup: circuit should be owed a probe after its cooldown")
	}

	cb.RecordExhausted(id, "p", "m", 429, 0)

	c := exhaustedCircuit(t, cb, id, "m")
	if c.state != StateOpen || c.failedProbes != 1 {
		t.Errorf("state=%v failedProbes=%d, want a re-open counting one failed probe", c.state, c.failedProbes)
	}
}

// Three 429-caused opens inside the window mark the circuit exhausted without
// any phrase saying so, and its probe backoff may then climb past
// circuit_breaker_backoff_max toward circuit_breaker_quota_pin_max.
func TestRecordRateLimited_ThirdOpenLiftsBackoffCeiling(t *testing.T) {
	cb := NewCircuitBreaker(&stubSettings{threshold: 1, cooldown: time.Minute, backoffMax: 2 * time.Minute, pinMax: 5 * time.Hour})
	id := uuid.New()

	// Open 1 (closed → open): streak 1, no probes failed yet.
	cb.RecordRateLimited(id, "p", "m", Cause{})
	// Open 2 (failed probe): streak 2, backoff 2m — the ordinary ceiling.
	backdateOpenModel(t, cb, id, "m", 2*time.Minute)
	cb.IsOpen(id, "p", "m") // transitions to half-open
	cb.RecordRateLimited(id, "p", "m", Cause{})
	if got := exhaustedCircuit(t, cb, id, "m").cooldownBackoff; got != 2*time.Minute {
		t.Fatalf("backoff after second 429 open = %v, want the ordinary ceiling of 2m", got)
	}
	// Open 3: streak 3 = escalated, and the doubling may pass the old ceiling.
	backdateOpenModel(t, cb, id, "m", 3*time.Minute)
	cb.IsOpen(id, "p", "m")
	cb.RecordRateLimited(id, "p", "m", Cause{})

	if got := exhaustedCircuit(t, cb, id, "m").cooldownBackoff; got != 4*time.Minute {
		t.Errorf("backoff after third 429 open = %v, want 4m (1m doubled twice, past the 2m backoff_max, under the 5h pin ceiling)", got)
	}
}

func TestRecordRateLimited_NonRateLimitOpenResetsEscalation(t *testing.T) {
	cb := NewCircuitBreaker(&stubSettings{threshold: 1, cooldown: time.Minute, backoffMax: 2 * time.Minute, pinMax: 5 * time.Hour})
	id := uuid.New()

	cb.RecordRateLimited(id, "p", "m", Cause{})
	backdateOpenModel(t, cb, id, "m", 2*time.Minute)
	cb.IsOpen(id, "p", "m")
	// A 5xx-caused open in between: different failure, streak resets.
	cb.RecordFailure(id, "p", "m", Cause{})
	backdateOpenModel(t, cb, id, "m", 3*time.Minute)
	cb.IsOpen(id, "p", "m")
	cb.RecordRateLimited(id, "p", "m", Cause{})

	c := exhaustedCircuit(t, cb, id, "m")
	if c.opens429Streak != 1 {
		t.Errorf("streak = %d, want 1: the 5xx open reset the run and this 429 started a new one", c.opens429Streak)
	}
	if got := c.cooldownBackoff; got != 2*time.Minute {
		t.Errorf("backoff = %v, want capped at backoff_max (2m): no escalation without three 429 opens in a row", got)
	}
}

func TestRecordSuccess_ClosingProbeClearsEscalation(t *testing.T) {
	cb := NewCircuitBreaker(&stubSettings{threshold: 1, cooldown: time.Minute})
	id := uuid.New()
	cb.RecordRateLimited(id, "p", "m", Cause{})
	backdateOpenModel(t, cb, id, "m", 2*time.Minute)
	cb.IsOpen(id, "p", "m")

	cb.RecordSuccess(id, "p", "m")

	c := exhaustedCircuit(t, cb, id, "m")
	if c.state != StateClosed || c.opens429Streak != 0 {
		t.Errorf("state=%v streak=%d, want closed with the streak cleared: HTTP proved recovery", c.state, c.opens429Streak)
	}
}

func TestLastSuccessWithin(t *testing.T) {
	cb := newTestCB(5, time.Minute)
	id := uuid.New()

	if cb.LastSuccessWithin(id, "m", time.Minute) {
		t.Error("untracked pair reports a recent success; an unfamiliar provider must never get the gentler treatment on a guess")
	}
	cb.RecordSuccess(id, "p", "m")
	if !cb.LastSuccessWithin(id, "m", time.Minute) {
		t.Error("a success recorded just now is not seen inside a 1m window")
	}
	// Age the stamp past the window without waiting out real time.
	cb.mu.Lock()
	cb.circuits[id.String()]["m"].lastSuccess = time.Now().Add(-61 * time.Second)
	cb.mu.Unlock()
	if cb.LastSuccessWithin(id, "m", time.Minute) {
		t.Error("a success 61s ago is inside a 60s window")
	}
	// A failure does not refresh the success stamp.
	cb.RecordFailure(id, "p", "m", Cause{})
	if cb.LastSuccessWithin(id, "m", time.Minute) {
		t.Error("RecordFailure refreshed the success stamp")
	}
	// Neither does an alive-but-served-nothing credit (a plain 400): a client's
	// own malformed payloads must not keep the fallback reading a spent window
	// as busy.
	cb.RecordAlive(id, "p", "m", 400)
	if cb.LastSuccessWithin(id, "m", time.Minute) {
		t.Error("RecordAlive stamped a serve the provider never made")
	}
	if got := exhaustedCircuit(t, cb, id, "m").consecutiveFails; got != 0 {
		t.Errorf("RecordAlive did not credit the circuit: fails=%d, want 0", got)
	}
}

func TestBlockedUntil(t *testing.T) {
	cb := NewCircuitBreaker(&stubSettings{threshold: 1, cooldown: time.Minute})
	id := uuid.New()

	if _, _, ok := cb.BlockedUntil(id, "m"); ok {
		t.Error("an untracked pair reports itself blocked")
	}

	// The model's own blocking circuit: retry at openedAt+cooldown, unpinned.
	cb.RecordFailure(id, "p", "m", Cause{})
	retryAt, pinned, ok := cb.BlockedUntil(id, "m")
	if !ok || pinned {
		t.Fatalf("own blocking circuit: ok=%v pinned=%v, want blocked and unpinned", ok, pinned)
	}
	if until := time.Until(retryAt); until <= 50*time.Second || until > time.Minute {
		t.Errorf("retryAt %v from now, want just under the 1m cooldown", until)
	}

	// A sibling model of an indicted provider: blocked by the verdict, and the
	// retry instant is the earliest among the blocking circuits. Pinning every
	// blocking circuit makes the verdict an exhaustion.
	cb.RecordExhausted(id, "p", "m2", 429, 30*time.Minute)
	if _, _, ok := cb.BlockedUntil(id, "other-model"); !ok {
		t.Fatal("provider indicted at span 2, but a sibling model reports unblocked")
	}
	pinSibling(t, cb, id, "m", 0, 30*time.Minute)
	_, pinned, ok = cb.BlockedUntil(id, "other-model")
	if !ok || !pinned {
		t.Errorf("all blocking circuits pinned: ok=%v pinned=%v, want blocked and pinned", ok, pinned)
	}
}

// overrideForModel is overrideFor for a named model circuit.
func overrideForModel(t *testing.T, cb *CircuitBreaker, id uuid.UUID, model string) time.Duration {
	t.Helper()
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	c, ok := cb.circuits[id.String()][model]
	if !ok {
		t.Fatalf("no circuit tracked for %s/%q", id, model)
	}
	return c.cooldownOverride
}

// backdateOpenModel is backdateOpen for a named model circuit.
func backdateOpenModel(t *testing.T, cb *CircuitBreaker, id uuid.UUID, model string, by time.Duration) {
	t.Helper()
	cb.mu.Lock()
	defer cb.mu.Unlock()
	c, ok := cb.circuits[id.String()][model]
	if !ok {
		t.Fatalf("setup: no circuit tracked for %s/%q", id, model)
	}
	c.openedAt = c.openedAt.Add(-by)
}
