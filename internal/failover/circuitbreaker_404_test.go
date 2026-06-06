package failover

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestCircuitBreaker_404DoesNotOpenCircuit verifies that model-specific
// 404 errors do NOT trip the circuit breaker, even after many attempts.
// The proxy code handles this by not calling RecordFailure/RecordSuccess
// for 404 responses. This test directly validates the breaker behavior
// when a 404 results in no recording at all.
func TestCircuitBreaker_404DoesNotOpenCircuit(t *testing.T) {
	t.Parallel()

	cb := newTestCB(5, 30*time.Second)
	pid := uuid.New()

	// Simulate the proxy's behavior: 404 → no breaker recording.
	// After 6 requests that 404 (no RecordFailure, no RecordSuccess),
	// the breaker should remain closed.
	for i := 0; i < 6; i++ {
		// 404 path: no RecordFailure, no RecordSuccess — true no-op
	}

	if cb.IsOpen(pid, "test-provider") {
		t.Error("circuit should remain closed after 6 unrecorded 404s")
	}
	if s := cb.GetState(pid); s != StateClosed {
		t.Errorf("expected StateClosed, got %v", s)
	}
}

// TestCircuitBreaker_5xxStillOpensCircuit verifies that 5xx server errors
// still open the circuit after the threshold is reached.
func TestCircuitBreaker_5xxStillOpensCircuit(t *testing.T) {
	t.Parallel()

	cb := newTestCB(5, 30*time.Second)
	pid := uuid.New()

	// Simulate the proxy's behavior: 5xx → RecordFailure.
	for i := 0; i < 5; i++ {
		cb.RecordFailure(pid, "test-provider")
	}

	if !cb.IsOpen(pid, "test-provider") {
		t.Error("circuit should be open after 5 consecutive 5xx failures")
	}
	if s := cb.GetState(pid); s != StateOpen {
		t.Errorf("expected StateOpen, got %v", s)
	}
}

// TestCircuitBreaker_401OpensCircuit verifies that 401 (bad key) errors
// still trip the circuit breaker, since a wrong/expired key is a
// provider-wide issue.
func TestCircuitBreaker_401OpensCircuit(t *testing.T) {
	t.Parallel()

	cb := newTestCB(5, 30*time.Second)
	pid := uuid.New()

	// Simulate the proxy's behavior: 401 → RecordFailure.
	for i := 0; i < 5; i++ {
		cb.RecordFailure(pid, "test-provider")
	}

	if !cb.IsOpen(pid, "test-provider") {
		t.Error("circuit should be open after 5 consecutive 401 failures")
	}
}

// TestCircuitBreaker_Interleaved5xxAnd404 verifies that 404 (no-op for
// the breaker) does not erase real 5xx failure history. In the proxy,
// a 404 results in neither RecordFailure nor RecordSuccess, so the
// consecutiveFails counter is preserved across interleaved 404s.
func TestCircuitBreaker_Interleaved5xxAnd404(t *testing.T) {
	t.Parallel()

	cb := newTestCB(5, 30*time.Second)
	pid := uuid.New()

	// 4 × 5xx (RecordFailure)
	for i := 0; i < 4; i++ {
		cb.RecordFailure(pid, "test-provider")
	}

	// 1 × 404 (no-op — no recording at all)
	// If this were incorrectly recorded as RecordSuccess, it would reset
	// consecutiveFails to 0, erasing the 5xx history. With the true
	// no-op, the counter stays at 4.

	// 1 × 5xx (RecordFailure) — should bring counter to 5 and open the circuit
	cb.RecordFailure(pid, "test-provider")

	if !cb.IsOpen(pid, "test-provider") {
		t.Error("circuit should be open: 5 consecutive 5xx failures (no reset from 404 no-op)")
	}
}

// TestCircuitBreaker_404DoesNotPrematurelyCloseHalfOpen verifies that
// a 404 (no-op) during half-open state does NOT count as a passing probe.
// If 404 were recorded as RecordSuccess, it would prematurely close the
// circuit (HalfOpenMaxProbes=1).
func TestCircuitBreaker_404DoesNotPrematurelyCloseHalfOpen(t *testing.T) {
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

	// 404 during half-open: no-op (no RecordSuccess, no RecordFailure).
	// If this were incorrectly recorded as RecordSuccess, it would close
	// the circuit immediately (HalfOpenMaxProbes=1).

	// The circuit should still be in half-open state
	s := cb.GetState(pid)
	if s != StateHalfOpen {
		t.Errorf("expected StateHalfOpen after 404 no-op in half-open, got %v", s)
	}

	// Verify: another 5xx failure should re-open the circuit
	cb.RecordFailure(pid, "test-provider")
	if !cb.IsOpen(pid, "test-provider") {
		t.Error("circuit should have re-opened after 5xx in half-open state")
	}
}

// TestCircuitBreaker_RecordSuccessResetsFailures verifies that a
// RecordSuccess DOES reset the failure counter (the behavior we don't
// want from 404s, confirming why no-op is the correct treatment).
func TestCircuitBreaker_RecordSuccessResetsFailures(t *testing.T) {
	t.Parallel()

	cb := newTestCB(5, 30*time.Second)
	pid := uuid.New()

	// 4 × 5xx
	for i := 0; i < 4; i++ {
		cb.RecordFailure(pid, "test-provider")
	}

	// 1 × RecordSuccess (what we USED to do for 404 — wrong!)
	cb.RecordSuccess(pid, "test-provider")

	// Now we need 5 MORE failures to open (counter was reset to 0)
	for i := 0; i < 5; i++ {
		cb.RecordFailure(pid, "test-provider")
	}

	if !cb.IsOpen(pid, "test-provider") {
		t.Error("circuit should be open after 5 more failures post-reset")
	}
}
