package failover

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

func newTestCB(threshold int, cooldown time.Duration) *CircuitBreaker {
	cb := NewCircuitBreaker(nil)
	cb.Threshold = threshold
	cb.Cooldown = cooldown
	cb.HalfOpenMaxProbes = 1
	return cb
}

func TestCircuitBreaker_StartsClosed(t *testing.T) {
	cb := newTestCB(3, 30*time.Second)
	pid := uuid.New()

	if cb.IsOpen(pid) {
		t.Error("new provider should start in closed state")
	}
	if s := cb.GetState(pid); s != StateClosed {
		t.Errorf("expected StateClosed, got %v", s)
	}
}

func TestCircuitBreaker_OpensAfterThreshold(t *testing.T) {
	cb := newTestCB(3, 30*time.Second)
	pid := uuid.New()

	cb.RecordFailure(pid)
	cb.RecordFailure(pid)

	if cb.IsOpen(pid) {
		t.Error("should not be open after 2 failures (threshold=3)")
	}

	cb.RecordFailure(pid) // 3rd failure → opens

	if !cb.IsOpen(pid) {
		t.Error("should be open after 3 consecutive failures")
	}
	if s := cb.GetState(pid); s != StateOpen {
		t.Errorf("expected StateOpen, got %v", s)
	}
}

func TestCircuitBreaker_SuccessResetsFailures(t *testing.T) {
	cb := newTestCB(5, 30*time.Second)
	pid := uuid.New()

	for i := 0; i < 4; i++ {
		cb.RecordFailure(pid)
	}
	cb.RecordSuccess(pid) // resets counter

	// Need 5 more failures to open
	for i := 0; i < 4; i++ {
		cb.RecordFailure(pid)
	}
	if cb.IsOpen(pid) {
		t.Error("should still be closed — only 4 failures after reset")
	}

	cb.RecordFailure(pid) // 5th → opens
	if !cb.IsOpen(pid) {
		t.Error("should be open after 5 consecutive failures post-reset")
	}
}

func TestCircuitBreaker_TransitionsToHalfOpen(t *testing.T) {
	cb := newTestCB(1, 50*time.Millisecond)
	pid := uuid.New()

	cb.RecordFailure(pid) // threshold=1 → opens

	if !cb.IsOpen(pid) {
		t.Fatal("should be open")
	}

	time.Sleep(60 * time.Millisecond) // wait for cooldown

	// IsOpen should transition to half-open and return false
	if cb.IsOpen(pid) {
		t.Error("should have transitioned to half-open after cooldown")
	}
}

func TestCircuitBreaker_HalfOpenProbeSuccess(t *testing.T) {
	cb := newTestCB(1, 50*time.Millisecond)
	pid := uuid.New()

	cb.RecordFailure(pid) // opens
	time.Sleep(60 * time.Millisecond)
	cb.IsOpen(pid) // triggers Open→HalfOpen

	cb.RecordSuccess(pid) // probe succeeds → closes

	if cb.IsOpen(pid) {
		t.Error("should be closed after successful probe")
	}
	if s := cb.GetState(pid); s != StateClosed {
		t.Errorf("expected StateClosed, got %v", s)
	}
}

func TestCircuitBreaker_HalfOpenProbeFailure(t *testing.T) {
	cb := newTestCB(1, 50*time.Millisecond)
	pid := uuid.New()

	cb.RecordFailure(pid) // opens
	time.Sleep(60 * time.Millisecond)
	cb.IsOpen(pid) // triggers Open→HalfOpen

	cb.RecordFailure(pid) // probe fails → re-opens

	if !cb.IsOpen(pid) {
		t.Error("should be re-opened after failed probe")
	}

	// Should stay open (cooldown not elapsed)
	if !cb.IsOpen(pid) {
		t.Error("should still be open (fresh cooldown)")
	}
}

func TestCircuitBreaker_Reset(t *testing.T) {
	cb := newTestCB(1, 30*time.Second)
	pid := uuid.New()

	cb.RecordFailure(pid) // opens
	if !cb.IsOpen(pid) {
		t.Fatal("should be open")
	}

	cb.Reset(pid)
	if cb.IsOpen(pid) {
		t.Error("should be closed after reset")
	}
}

func TestCircuitBreaker_ResetAll(t *testing.T) {
	cb := newTestCB(1, 30*time.Second)
	p1, p2 := uuid.New(), uuid.New()

	cb.RecordFailure(p1)
	cb.RecordFailure(p2)

	cb.ResetAll()

	if cb.IsOpen(p1) || cb.IsOpen(p2) {
		t.Error("all circuits should be cleared after ResetAll")
	}
}

func TestCircuitBreaker_Status(t *testing.T) {
	cb := newTestCB(1, 30*time.Second)
	pid := uuid.New()

	cb.RecordFailure(pid) // opens

	statuses := cb.Status()
	if len(statuses) != 1 {
		t.Fatalf("expected 1 status, got %d", len(statuses))
	}
	if statuses[0].State != "open" {
		t.Errorf("expected state 'open', got %q", statuses[0].State)
	}
	if statuses[0].ProviderID != pid.String() {
		t.Errorf("expected provider_id %s, got %s", pid, statuses[0].ProviderID)
	}
}

func TestCircuitBreaker_Concurrent(t *testing.T) {
	cb := newTestCB(100, 30*time.Second)
	pid := uuid.New()

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			cb.RecordFailure(pid)
		}()
		go func() {
			defer wg.Done()
			_ = cb.IsOpen(pid)
		}()
	}
	wg.Wait()

	// Should not panic; state should be valid
	statuses := cb.Status()
	if len(statuses) != 1 {
		t.Fatalf("expected 1 status, got %d", len(statuses))
	}
	if statuses[0].ConsecutiveFails != 50 {
		t.Errorf("expected 50 consecutive failures, got %d", statuses[0].ConsecutiveFails)
	}
}

func TestCircuitBreaker_UnknownProviderIsClosed(t *testing.T) {
	cb := newTestCB(5, 30*time.Second)
	pid := uuid.New()

	if cb.IsOpen(pid) {
		t.Error("unknown provider should not be open")
	}
	if s := cb.GetState(pid); s != StateClosed {
		t.Errorf("expected StateClosed for unknown provider, got %v", s)
	}
}

func TestCircuitBreaker_FailureCountAccuracy(t *testing.T) {
	cb := newTestCB(5, 30*time.Second)
	pid := uuid.New()

	for i := 0; i < 4; i++ {
		cb.RecordFailure(pid)
	}
	statuses := cb.Status()
	if len(statuses) != 1 {
		t.Fatal("expected 1 status entry")
	}
	if statuses[0].ConsecutiveFails != 4 {
		t.Errorf("expected 4 consecutive failures, got %d", statuses[0].ConsecutiveFails)
	}
	if statuses[0].State != "closed" {
		t.Errorf("expected 'closed' state after 4/5 failures, got %q", statuses[0].State)
	}
}

// stubSettings implements SettingsReader for tests.
type stubSettings struct {
	threshold int
	cooldown  time.Duration
}

func (s *stubSettings) GetInt(_ context.Context, key string, def int) int {
	if key == "circuit_breaker_threshold" && s.threshold > 0 {
		return s.threshold
	}
	return def
}

func (s *stubSettings) GetDuration(_ context.Context, key string, def time.Duration) time.Duration {
	if key == "circuit_breaker_cooldown" && s.cooldown > 0 {
		return s.cooldown
	}
	return def
}

func TestCircuitBreaker_SettingsOverrideThreshold(t *testing.T) {
	cb := NewCircuitBreaker(&stubSettings{threshold: 2, cooldown: 10 * time.Second})
	cb.HalfOpenMaxProbes = 1
	pid := uuid.New()

	// Default struct threshold is 5, but settings override to 2.
	// After 2 failures, the circuit should open.
	cb.RecordFailure(pid)
	if cb.IsOpen(pid) {
		t.Error("should still be closed after 1 failure (threshold=2)")
	}
	cb.RecordFailure(pid)
	if !cb.IsOpen(pid) {
		t.Error("should be open after 2 failures (settings threshold=2)")
	}
}

func TestCircuitBreaker_SettingsOverrideCooldown(t *testing.T) {
	cb := NewCircuitBreaker(&stubSettings{threshold: 1, cooldown: 50 * time.Millisecond})
	cb.HalfOpenMaxProbes = 1
	pid := uuid.New()

	// Open the circuit.
	cb.RecordFailure(pid)
	if !cb.IsOpen(pid) {
		t.Fatal("should be open after 1 failure")
	}

	// Wait for the short cooldown (50ms) to elapse.
	time.Sleep(80 * time.Millisecond)

	// IsOpen should transition to half-open and return false.
	if cb.IsOpen(pid) {
		t.Error("should have transitioned to half-open after 50ms cooldown")
	}
}

// TestCircuitBreaker_ContextCancellationSkipContract documents the expected
// behavior that context cancellation and deadline exceeded errors should NOT
// count as provider failures. The skip logic lives in the proxy handler
// (proxy.go:446-460), which checks errors.Is(err, context.Canceled) and
// errors.Is(err, context.DeadlineExceeded) before calling RecordFailure.
//
// This test verifies the RecordFailure contract: if RecordFailure is called
// the expected number of times, the circuit opens. The proxy handler is
// responsible for NOT calling RecordFailure for context errors.
func TestCircuitBreaker_ContextCancellationSkipContract(t *testing.T) {
	// If RecordFailure is called 3 times (threshold), the circuit opens.
	cb := newTestCB(3, 30*time.Second)
	pid := uuid.New()

	cb.RecordFailure(pid)
	cb.RecordFailure(pid)

	if cb.IsOpen(pid) {
		t.Error("should not be open after 2 failures (threshold=3)")
	}

	cb.RecordFailure(pid) // 3rd failure → opens

	if !cb.IsOpen(pid) {
		t.Error("should be open after 3 consecutive failures")
	}

	// Reset and verify that skipping RecordFailure (as the proxy handler
	// does for context errors) means the circuit stays closed.
	cb.Reset(pid)

	cb.RecordFailure(pid)
	cb.RecordFailure(pid)
	// 3rd "failure" was a context cancellation → RecordFailure NOT called
	// So we're at 2 failures, not 3. Circuit should remain closed.

	if cb.IsOpen(pid) {
		t.Error("should remain closed: only 2 failures recorded (3rd was a context cancellation, skipped)")
	}
}


func TestCircuitBreaker_NilSettingsUsesDefaults(t *testing.T) {
	cb := NewCircuitBreaker(nil)
	cb.Threshold = 3
	cb.Cooldown = 10 * time.Second
	cb.HalfOpenMaxProbes = 1
	pid := uuid.New()

	// With nil settings, effective methods should return struct defaults.
	for i := 0; i < 2; i++ {
		cb.RecordFailure(pid)
	}
	if cb.IsOpen(pid) {
		t.Error("should be closed after 2/3 failures")
	}
	cb.RecordFailure(pid)
	if !cb.IsOpen(pid) {
		t.Error("should be open after 3/3 failures (struct default)")
	}
}
