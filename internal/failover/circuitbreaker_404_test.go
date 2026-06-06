package failover

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestCircuitBreaker_NoOpPreservesFailureHistory verifies that skipping
// breaker recording (the 404/499 code path in the proxy) does not reset
// consecutiveFails or close a half-open circuit. This is the breaker-level
// guarantee that makes the noop action safe.
func TestCircuitBreaker_NoOpPreservesFailureHistory(t *testing.T) {
	t.Parallel()

	cb := newTestCB(5, 30*time.Second)
	pid := uuid.New()

	// 4 × RecordFailure (simulating 5xx responses)
	for i := 0; i < 4; i++ {
		cb.RecordFailure(pid, "test-provider")
	}

	// 404/499: the proxy calls neither RecordFailure nor RecordSuccess.
	// No call made here — that IS the behavior under test.
	// Previously, the proxy called RecordSuccess for 404, which would reset
	// consecutiveFails to 0 here.

	// 1 more RecordFailure should open the circuit (counter at 5).
	cb.RecordFailure(pid, "test-provider")

	if !cb.IsOpen(pid, "test-provider") {
		t.Error("no-op should preserve failure count: expected circuit open at 5 failures")
	}
}

// TestCircuitBreaker_NoOpInHalfOpenDoesNotCloseCircuit verifies that
// skipping breaker recording during half-open state does NOT count as a
// passing probe. If 404 were recorded as RecordSuccess, it would
// prematurely close the circuit (HalfOpenMaxProbes=1).
func TestCircuitBreaker_NoOpInHalfOpenDoesNotCloseCircuit(t *testing.T) {
	t.Parallel()

	cb := newTestCB(1, 50*time.Millisecond)
	pid := uuid.New()

	// Open the circuit with a single failure
	cb.RecordFailure(pid, "test-provider")
	if !cb.IsOpen(pid, "test-provider") {
		t.Fatal("circuit should be open")
	}

	// Wait for cooldown → half-open
	time.Sleep(80 * time.Millisecond)
	if cb.IsOpen(pid, "test-provider") {
		t.Fatal("circuit should have transitioned to half-open after cooldown")
	}
	if cb.GetState(pid) != StateHalfOpen {
		t.Fatal("expected StateHalfOpen")
	}

	// 404/499: the proxy calls neither RecordFailure nor RecordSuccess.
	// No call made here — that IS the behavior under test.
	// If this were RecordSuccess, the circuit would close immediately.

	// Circuit must still be in half-open state after the no-op.
	if cb.GetState(pid) != StateHalfOpen {
		t.Error("no-op should not close half-open circuit")
	}

	// Verify: a subsequent 5xx failure should re-open the circuit.
	cb.RecordFailure(pid, "test-provider")
	if !cb.IsOpen(pid, "test-provider") {
		t.Error("circuit should have re-opened after 5xx in half-open state")
	}
}

// TestCircuitBreaker_RecordSuccessResetsFailures documents why 404 must
// be a no-op rather than RecordSuccess: RecordSuccess resets consecutiveFails
// to 0 in the Closed state, erasing real 5xx failure history. If the proxy
// were to call RecordSuccess for a 404, interleaved 404s would prevent the
// circuit from ever opening during a provider outage.
func TestCircuitBreaker_RecordSuccessResetsFailures(t *testing.T) {
	t.Parallel()

	cb := newTestCB(5, 30*time.Second)
	pid := uuid.New()

	// 4 × RecordFailure (5xx)
	for i := 0; i < 4; i++ {
		cb.RecordFailure(pid, "test-provider")
	}

	// RecordSuccess (what we USED to do for 404 — the wrong behavior)
	cb.RecordSuccess(pid, "test-provider")

	// Counter was reset to 0 — need 5 MORE failures to open
	for i := 0; i < 4; i++ {
		cb.RecordFailure(pid, "test-provider")
	}
	if cb.IsOpen(pid, "test-provider") {
		t.Error("circuit should still be closed — only 4 failures after reset")
	}

	cb.RecordFailure(pid, "test-provider") // 5th after reset → opens

	if !cb.IsOpen(pid, "test-provider") {
		t.Error("circuit should be open after 5 failures post-reset")
	}
}
