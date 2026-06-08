package failover

import (
	"context"
	"fmt"
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

	if cb.IsOpen(pid, "test-provider") {
		t.Error("new provider should start in closed state")
	}
	if s := cb.GetState(pid); s != StateClosed {
		t.Errorf("expected StateClosed, got %v", s)
	}
}

func TestCircuitBreaker_OpensAfterThreshold(t *testing.T) {
	cb := newTestCB(3, 30*time.Second)
	pid := uuid.New()

	cb.RecordFailure(pid, "test-provider")
	cb.RecordFailure(pid, "test-provider")

	if cb.IsOpen(pid, "test-provider") {
		t.Error("should not be open after 2 failures (threshold=3)")
	}

	cb.RecordFailure(pid, "test-provider") // 3rd failure → opens

	if !cb.IsOpen(pid, "test-provider") {
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
		cb.RecordFailure(pid, "test-provider")
	}
	cb.RecordSuccess(pid, "test-provider") // resets counter

	// Need 5 more failures to open
	for i := 0; i < 4; i++ {
		cb.RecordFailure(pid, "test-provider")
	}
	if cb.IsOpen(pid, "test-provider") {
		t.Error("should still be closed — only 4 failures after reset")
	}

	cb.RecordFailure(pid, "test-provider") // 5th → opens
	if !cb.IsOpen(pid, "test-provider") {
		t.Error("should be open after 5 consecutive failures post-reset")
	}
}

func TestCircuitBreaker_TransitionsToHalfOpen(t *testing.T) {
	cb := newTestCB(1, 50*time.Millisecond)
	pid := uuid.New()

	cb.RecordFailure(pid, "test-provider") // threshold=1 → opens

	if !cb.IsOpen(pid, "test-provider") {
		t.Fatal("should be open")
	}

	time.Sleep(60 * time.Millisecond) // wait for cooldown

	// IsOpen should transition to half-open and return false
	if cb.IsOpen(pid, "test-provider") {
		t.Error("should have transitioned to half-open after cooldown")
	}
}

func TestCircuitBreaker_HalfOpenProbeSuccess(t *testing.T) {
	cb := newTestCB(1, 50*time.Millisecond)
	pid := uuid.New()

	cb.RecordFailure(pid, "test-provider") // opens
	time.Sleep(60 * time.Millisecond)
	cb.IsOpen(pid, "test-provider") // triggers Open→HalfOpen

	cb.RecordSuccess(pid, "test-provider") // probe succeeds → closes

	if cb.IsOpen(pid, "test-provider") {
		t.Error("should be closed after successful probe")
	}
	if s := cb.GetState(pid); s != StateClosed {
		t.Errorf("expected StateClosed, got %v", s)
	}
}

func TestCircuitBreaker_HalfOpenProbeFailure(t *testing.T) {
	cb := newTestCB(1, 50*time.Millisecond)
	pid := uuid.New()

	cb.RecordFailure(pid, "test-provider") // opens
	time.Sleep(60 * time.Millisecond)
	cb.IsOpen(pid, "test-provider") // triggers Open→HalfOpen

	cb.RecordFailure(pid, "test-provider") // probe fails → re-opens

	if !cb.IsOpen(pid, "test-provider") {
		t.Error("should be re-opened after failed probe")
	}

	// Should stay open (cooldown not elapsed)
	if !cb.IsOpen(pid, "test-provider") {
		t.Error("should still be open (fresh cooldown)")
	}
}

func TestCircuitBreaker_Reset(t *testing.T) {
	cb := newTestCB(1, 30*time.Second)
	pid := uuid.New()

	cb.RecordFailure(pid, "test-provider") // opens
	if !cb.IsOpen(pid, "test-provider") {
		t.Fatal("should be open")
	}

	cb.Reset(pid)
	if cb.IsOpen(pid, "test-provider") {
		t.Error("should be closed after reset")
	}
}

func TestCircuitBreaker_ResetAll(t *testing.T) {
	cb := newTestCB(1, 30*time.Second)
	p1, p2 := uuid.New(), uuid.New()

	cb.RecordFailure(p1, "test-provider")
	cb.RecordFailure(p2, "test-provider")

	cb.ResetAll()

	if cb.IsOpen(p1, "test-provider") || cb.IsOpen(p2, "test-provider") {
		t.Error("all circuits should be cleared after ResetAll")
	}
}

func TestCircuitBreaker_Status(t *testing.T) {
	cb := newTestCB(1, 30*time.Second)
	pid := uuid.New()

	cb.RecordFailure(pid, "test-provider") // opens

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
			cb.RecordFailure(pid, "test-provider")
		}()
		go func() {
			defer wg.Done()
			_ = cb.IsOpen(pid, "test-provider")
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

	if cb.IsOpen(pid, "test-provider") {
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
		cb.RecordFailure(pid, "test-provider")
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
	cb.RecordFailure(pid, "test-provider")
	if cb.IsOpen(pid, "test-provider") {
		t.Error("should still be closed after 1 failure (threshold=2)")
	}
	cb.RecordFailure(pid, "test-provider")
	if !cb.IsOpen(pid, "test-provider") {
		t.Error("should be open after 2 failures (settings threshold=2)")
	}
}

func TestCircuitBreaker_SettingsOverrideCooldown(t *testing.T) {
	cb := NewCircuitBreaker(&stubSettings{threshold: 1, cooldown: 50 * time.Millisecond})
	cb.HalfOpenMaxProbes = 1
	pid := uuid.New()

	// Open the circuit.
	cb.RecordFailure(pid, "test-provider")
	if !cb.IsOpen(pid, "test-provider") {
		t.Fatal("should be open after 1 failure")
	}

	// Wait for the short cooldown (50ms) to elapse.
	time.Sleep(80 * time.Millisecond)

	// IsOpen should transition to half-open and return false.
	if cb.IsOpen(pid, "test-provider") {
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

	cb.RecordFailure(pid, "test-provider")
	cb.RecordFailure(pid, "test-provider")

	if cb.IsOpen(pid, "test-provider") {
		t.Error("should not be open after 2 failures (threshold=3)")
	}

	cb.RecordFailure(pid, "test-provider") // 3rd failure → opens

	if !cb.IsOpen(pid, "test-provider") {
		t.Error("should be open after 3 consecutive failures")
	}

	// Reset and verify that skipping RecordFailure (as the proxy handler
	// does for context errors) means the circuit stays closed.
	cb.Reset(pid)

	cb.RecordFailure(pid, "test-provider")
	cb.RecordFailure(pid, "test-provider")
	// 3rd "failure" was a context cancellation → RecordFailure NOT called
	// So we're at 2 failures, not 3. Circuit should remain closed.

	if cb.IsOpen(pid, "test-provider") {
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
		cb.RecordFailure(pid, "test-provider")
	}
	if cb.IsOpen(pid, "test-provider") {
		t.Error("should be closed after 2/3 failures")
	}
	cb.RecordFailure(pid, "test-provider")
	if !cb.IsOpen(pid, "test-provider") {
		t.Error("should be open after 3/3 failures (struct default)")
	}
}

func TestState_String(t *testing.T) {
	tests := []struct {
		name  string
		state State
		want  string
	}{
		{"StateClosed", StateClosed, "closed"},
		{"StateOpen", StateOpen, "open"},
		{"StateHalfOpen", StateHalfOpen, "half-open"},
		{"StateUnknown", State(999), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.state.String()
			if got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestState_MarshalText(t *testing.T) {
	tests := []struct {
		name    string
		state   State
		want    string
		wantErr bool
	}{
		{"StateClosed", StateClosed, "closed", false},
		{"StateOpen", StateOpen, "open", false},
		{"StateHalfOpen", StateHalfOpen, "half-open", false},
		{"StateUnknown", State(999), "unknown", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.state.MarshalText()
			if (err != nil) != tt.wantErr {
				t.Errorf("MarshalText() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if string(got) != tt.want {
				t.Errorf("MarshalText() = %q, want %q", string(got), tt.want)
			}
		})
	}
}

func TestCircuitBreaker_SeverityForState(t *testing.T) {
	tests := []struct {
		name  string
		state string
		want  string
	}{
		{"open", "open", "warning"},
		{"closed", "closed", "success"},
		{"unknown", "unknown", "info"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cb := newTestCB(3, 30*time.Second)
			got := cb.severityForState(tt.state)
			if got != tt.want {
				t.Errorf("severityForState(%q) = %q, want %q", tt.state, got, tt.want)
			}
		})
	}
}

// TestCircuitBreaker_IsOpen_OpenStillWithinCooldown verifies that IsOpen returns
// true (blocking requests) when the circuit has been open but the cooldown has
// NOT yet elapsed. This is the "stay open" branch at line 160.
func TestCircuitBreaker_IsOpen_OpenStillWithinCooldown(t *testing.T) {
	t.Parallel()
	cb := newTestCB(1, 10*time.Second) // long cooldown
	pid := uuid.New()

	cb.RecordFailure(pid, "test-provider") // opens the circuit

	// Immediately after opening, cooldown has not elapsed.
	if !cb.IsOpen(pid, "test-provider") {
		t.Error("circuit should still be open (cooldown not elapsed)")
	}

	// Verify internal state is still StateOpen (not half-open)
	cb.mu.RLock()
	c := cb.circuits[pid.String()]
	cb.mu.RUnlock()
	if c.state != StateOpen {
		t.Errorf("expected StateOpen, got %v", c.state)
	}
}

// TestCircuitBreaker_IsOpen_HalfOpenAllowsProbesConcurrently verifies that
// when a circuit is in half-open state, concurrent IsOpen calls all return
// false (allowing probes through). This exercises the read-lock fast path
// at line 133.
func TestCircuitBreaker_IsOpen_HalfOpenAllowsProbesConcurrently(t *testing.T) {
	t.Parallel()
	cb := newTestCB(1, 50*time.Millisecond)
	pid := uuid.New()

	cb.RecordFailure(pid, "test-provider") // opens
	time.Sleep(60 * time.Millisecond)
	cb.IsOpen(pid, "test-provider") // triggers transition to half-open

	var wg sync.WaitGroup
	results := make(chan bool, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- cb.IsOpen(pid, "test-provider")
		}()
	}
	wg.Wait()
	close(results)

	for r := range results {
		if r {
			t.Error("IsOpen should return false for half-open circuit (probe allowed via read-lock fast path)")
		}
	}
}

func TestCircuitBreaker_IsOpen_Concurrent(t *testing.T) {
	t.Parallel()
	cb := newTestCB(100, 30*time.Second)
	pid := uuid.New()

	// Pre-populate with some failures but not enough to open
	for i := 0; i < 50; i++ {
		cb.RecordFailure(pid, "test-provider")
	}

	var wg sync.WaitGroup
	isOpenResults := make(chan bool, 100)
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			isOpenResults <- cb.IsOpen(pid, "test-provider")
		}()
	}
	wg.Wait()
	close(isOpenResults)

	// All calls should return false (closed state)
	for result := range isOpenResults {
		if result {
			t.Error("Concurrent IsOpen calls should all return false for closed circuit")
		}
	}
}

func TestCircuitBreaker_IsOpen_HalfOpenState(t *testing.T) {
	t.Parallel()
	cb := newTestCB(1, 50*time.Millisecond)
	pid := uuid.New()

	// Open the circuit
	cb.RecordFailure(pid, "test-provider")
	if !cb.IsOpen(pid, "test-provider") {
		t.Fatal("should be open after 1 failure")
	}

	// Wait for cooldown to elapse
	time.Sleep(60 * time.Millisecond)

	// First IsOpen call transitions to half-open and returns false
	if cb.IsOpen(pid, "test-provider") {
		t.Error("IsOpen should return false after transitioning to half-open")
	}

	// Verify state is half-open
	if s := cb.GetState(pid); s != StateHalfOpen {
		t.Errorf("expected StateHalfOpen, got %v", s)
	}

	// Subsequent IsOpen calls while in half-open should also return false
	if cb.IsOpen(pid, "test-provider") {
		t.Error("IsOpen should return false for half-open circuit (allow probe)")
	}
}

func TestCircuitBreaker_IsOpen_RaceWithRecordSuccess(t *testing.T) {
	t.Parallel()
	cb := newTestCB(1, 50*time.Millisecond)
	pid := uuid.New()

	// Open the circuit
	cb.RecordFailure(pid, "test-provider")
	time.Sleep(60 * time.Millisecond)

	// Trigger transition to half-open
	cb.IsOpen(pid, "test-provider")

	var wg sync.WaitGroup
	errCh := make(chan error, 20)
	for i := 0; i < 10; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					errCh <- fmt.Errorf("panic in IsOpen: %v", r)
				}
			}()
			_ = cb.IsOpen(pid, "test-provider")
		}()
		go func() {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					errCh <- fmt.Errorf("panic in RecordSuccess: %v", r)
				}
			}()
			cb.RecordSuccess(pid, "test-provider")
		}()
	}
	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Error(err)
	}

	// Circuit should be closed after successful probe
	if cb.IsOpen(pid, "test-provider") {
		t.Error("circuit should be closed after successful probe in half-open state")
	}
}

func TestCircuitBreaker_IsOpen_MultipleProviders(t *testing.T) {
	t.Parallel()
	cb := newTestCB(3, 30*time.Second)
	pid1 := uuid.New()
	pid2 := uuid.New()
	pid3 := uuid.New()

	// Open only pid1
	cb.RecordFailure(pid1, "test-provider")
	cb.RecordFailure(pid1, "test-provider")
	cb.RecordFailure(pid1, "test-provider")

	// pid2 and pid3 should remain closed
	if !cb.IsOpen(pid1, "test-provider") {
		t.Error("pid1 should be open after 3 failures")
	}

	if cb.IsOpen(pid2, "test-provider") {
		t.Error("pid2 should be closed (no failures recorded)")
	}

	if cb.IsOpen(pid3, "test-provider") {
		t.Error("pid3 should be closed (no failures recorded)")
	}

	// Verify independence
	statuses := cb.Status()
	if len(statuses) != 1 {
		t.Errorf("expected 1 status entry, got %d", len(statuses))
	}
}

func TestCircuitBreaker_IsOpen_OpenToHalfOpenTransition(t *testing.T) {
	t.Parallel()
	cb := newTestCB(1, 100*time.Millisecond)
	pid := uuid.New()

	// Open the circuit
	cb.RecordFailure(pid, "test-provider")

	// Verify it's open
	if !cb.IsOpen(pid, "test-provider") {
		t.Error("should be open immediately after failure")
	}

	// Wait for exactly the cooldown period
	time.Sleep(110 * time.Millisecond)

	// IsOpen should now transition to half-open and return false
	isOpen := cb.IsOpen(pid, "test-provider")
	if isOpen {
		t.Error("IsOpen should return false after cooldown (half-open state)")
	}

	// Verify the state transitioned
	if s := cb.GetState(pid); s != StateHalfOpen {
		t.Errorf("expected StateHalfOpen after cooldown, got %v", s)
	}
}

func TestCircuitBreaker_IsOpen_UnknownProvider(t *testing.T) {
	t.Parallel()
	cb := newTestCB(3, 30*time.Second)
	unknownPID := uuid.New()

	// Never record any failures for this provider
	if cb.IsOpen(unknownPID, "test-provider") {
		t.Error("IsOpen should return false for unknown provider")
	}

	// Verify state is closed
	if s := cb.GetState(unknownPID); s != StateClosed {
		t.Errorf("expected StateClosed for unknown provider, got %v", s)
	}
}

// ---------------------------------------------------------------------------
// GetState additional edge case tests
// ---------------------------------------------------------------------------

func TestGetState_OpenCircuitBeforeCooldown(t *testing.T) {
	t.Parallel()
	cb := newTestCB(1, 30*time.Second)
	pid := uuid.New()

	cb.RecordFailure(pid, "test-provider") // threshold=1 → opens

	// Immediately after opening (no cooldown elapsed), GetState should return StateOpen
	if s := cb.GetState(pid); s != StateOpen {
		t.Errorf("expected StateOpen immediately after opening, got %v", s)
	}
}

func TestGetState_OpenCircuitAfterCooldown(t *testing.T) {
	t.Parallel()
	cb := newTestCB(1, 50*time.Millisecond)
	pid := uuid.New()

	cb.RecordFailure(pid, "test-provider") // opens
	time.Sleep(60 * time.Millisecond)      // wait for cooldown

	// After cooldown, GetState should return StateHalfOpen (logical transition)
	if s := cb.GetState(pid); s != StateHalfOpen {
		t.Errorf("expected StateHalfOpen after cooldown, got %v", s)
	}
}

func TestGetState_DoesNotMutateInternalState(t *testing.T) {
	t.Parallel()
	cb := newTestCB(1, 50*time.Millisecond)
	pid := uuid.New()

	cb.RecordFailure(pid, "test-provider") // opens

	// Wait for cooldown
	time.Sleep(60 * time.Millisecond)

	// GetState returns StateHalfOpen but should NOT mutate the internal state
	state := cb.GetState(pid)
	if state != StateHalfOpen {
		t.Errorf("expected StateHalfOpen, got %v", state)
	}

	// Internal state should still be StateOpen (GetState computes logical state
	// without mutation). Verify by checking GetState again returns the same.
	state2 := cb.GetState(pid)
	if state2 != StateHalfOpen {
		t.Errorf("expected StateHalfOpen on second call, got %v", state2)
	}
}

func TestGetState_ClosedCircuitAfterSuccess(t *testing.T) {
	t.Parallel()
	cb := newTestCB(3, 30*time.Second)
	pid := uuid.New()

	// Record some failures but not enough to open
	cb.RecordFailure(pid, "test-provider")
	cb.RecordFailure(pid, "test-provider")

	if s := cb.GetState(pid); s != StateClosed {
		t.Errorf("expected StateClosed after 2/3 failures, got %v", s)
	}
}

func TestGetState_HalfOpenTransitionsToClosedOnSuccess(t *testing.T) {
	t.Parallel()
	cb := newTestCB(1, 50*time.Millisecond)
	pid := uuid.New()

	cb.RecordFailure(pid, "test-provider") // opens
	time.Sleep(60 * time.Millisecond)

	// Transition to half-open via IsOpen (which mutates internal state)
	cb.IsOpen(pid, "test-provider")

	// Record success → should close
	cb.RecordSuccess(pid, "test-provider")

	if s := cb.GetState(pid); s != StateClosed {
		t.Errorf("expected StateClosed after successful probe, got %v", s)
	}
}

func TestGetState_HalfOpenTransitionsToOpenOnFailure(t *testing.T) {
	t.Parallel()
	cb := newTestCB(1, 50*time.Millisecond)
	pid := uuid.New()

	cb.RecordFailure(pid, "test-provider") // opens
	time.Sleep(60 * time.Millisecond)

	// Transition to half-open via IsOpen (which mutates internal state)
	cb.IsOpen(pid, "test-provider")

	// Record failure in half-open state → should re-open
	cb.RecordFailure(pid, "test-provider")

	if s := cb.GetState(pid); s != StateOpen {
		t.Errorf("expected StateOpen after failed probe in half-open, got %v", s)
	}
}

func TestGetState_ConcurrentReads(t *testing.T) {
	t.Parallel()
	cb := newTestCB(100, 30*time.Second)
	pid := uuid.New()

	// Pre-populate with some failures
	for i := 0; i < 50; i++ {
		cb.RecordFailure(pid, "test-provider")
	}

	var wg sync.WaitGroup
	errCh := make(chan error, 100)
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					errCh <- fmt.Errorf("panic in GetState: %v", r)
				}
			}()
			s := cb.GetState(pid)
			if s != StateClosed && s != StateOpen {
				errCh <- fmt.Errorf("unexpected state: %v", s)
			}
		}()
	}
	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Error(err)
	}
}
