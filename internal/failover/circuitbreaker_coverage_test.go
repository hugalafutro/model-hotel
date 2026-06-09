package failover

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// ---------------------------------------------------------------------------
// IsOpen additional edge case tests
// ---------------------------------------------------------------------------

// TestCircuitBreaker_IsOpen_DefaultStateBranch tests the default branch in
// IsOpen's switch statement, which returns false. This branch is hit when
// the circuit state is an unexpected value.
func TestCircuitBreaker_IsOpen_DefaultStateBranch(t *testing.T) {
	t.Parallel()
	cb := newTestCB(3, 30*time.Second)
	pid := uuid.New()

	// Manually set the circuit to an unknown state to exercise the default branch
	cb.mu.Lock()
	cb.circuits[pid.String()] = &circuit{
		state:    State(42), // invalid state
		openedAt: time.Now(),
	}
	cb.mu.Unlock()

	// IsOpen should return false for the default branch
	if cb.IsOpen(pid, "test-provider") {
		t.Error("IsOpen should return false for unknown/default state")
	}
}

// TestCircuitBreaker_IsOpen_ClosedStateViaWriteLock tests the StateClosed
// branch reached via the slow (write-lock) path.
func TestCircuitBreaker_IsOpen_ClosedStateViaWriteLock(t *testing.T) {
	t.Parallel()
	cb := newTestCB(1, 30*time.Second)
	pid := uuid.New()

	// Open the circuit
	cb.RecordFailure(pid, "test-provider")

	// Transition to half-open via cooldown
	cb.Cooldown = 1 * time.Millisecond
	time.Sleep(10 * time.Millisecond)
	cb.IsOpen(pid, "test-provider") // triggers Open→HalfOpen transition

	// Record success to close
	cb.RecordSuccess(pid, "test-provider")

	// Now verify IsOpen returns false (Closed via write-lock path)
	if cb.IsOpen(pid, "test-provider") {
		t.Error("IsOpen should return false after successful probe closes circuit")
	}
}

// TestCircuitBreaker_IsOpen_HalfOpenViaWriteLock tests the HalfOpen branch
// reached via the slow (write-lock) path when another goroutine transitioned
// the state between our RLock and Lock.
func TestCircuitBreaker_IsOpen_HalfOpenViaWriteLock(t *testing.T) {
	t.Parallel()
	cb := newTestCB(1, 1*time.Millisecond)
	pid := uuid.New()

	// Open the circuit
	cb.RecordFailure(pid, "test-provider")
	time.Sleep(10 * time.Millisecond)

	// Trigger transition to HalfOpen
	cb.IsOpen(pid, "test-provider")

	// Verify state is half-open and IsOpen returns false via write lock
	if cb.IsOpen(pid, "test-provider") {
		t.Error("IsOpen should return false for HalfOpen circuit via write-lock path")
	}
}
