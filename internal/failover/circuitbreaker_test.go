package failover

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/events"
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

	for range 4 {
		cb.RecordFailure(pid, "test-provider")
	}
	cb.RecordSuccess(pid, "test-provider") // resets counter

	// Need 5 more failures to open
	for range 4 {
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

	if prev := cb.Reset(pid); prev != StateOpen {
		t.Errorf("Reset should report the pre-reset state open, got %v", prev)
	}
	if cb.IsOpen(pid, "test-provider") {
		t.Error("should be closed after reset")
	}
}

// TestCircuitBreaker_ResetReportsPreResetState pins the value an operator-facing
// caller reports back: only a circuit that was actually sidelining its provider
// may read as a change. A closed circuit and a provider the breaker has never
// routed are the same healthy state, so both report closed.
func TestCircuitBreaker_ResetReportsPreResetState(t *testing.T) {
	cb := newTestCB(2, 30*time.Second)

	untracked := uuid.New()
	if prev := cb.Reset(untracked); prev != StateClosed {
		t.Errorf("untracked provider: Reset = %v, want closed", prev)
	}

	belowThreshold := uuid.New()
	cb.RecordFailure(belowThreshold, "test-provider") // 1 of 2: still closed
	if prev := cb.Reset(belowThreshold); prev != StateClosed {
		t.Errorf("below-threshold circuit: Reset = %v, want closed", prev)
	}

	// A circuit whose cooldown has elapsed reads as half-open everywhere else
	// (Status, GetState); Reset must not disagree with them.
	readyToProbe := uuid.New()
	cbShort := newTestCB(1, time.Millisecond)
	cbShort.RecordFailure(readyToProbe, "test-provider")
	time.Sleep(5 * time.Millisecond)
	if prev := cbShort.Reset(readyToProbe); prev != StateHalfOpen {
		t.Errorf("cooldown-elapsed circuit: Reset = %v, want half-open", prev)
	}
}

func TestCircuitBreaker_ResetAll(t *testing.T) {
	cb := newTestCB(1, 30*time.Second)
	p1, p2 := uuid.New(), uuid.New()

	cb.RecordFailure(p1, "test-provider")
	cb.RecordFailure(p2, "test-provider")

	cleared, recovered := cb.ResetAll()
	if cleared != 2 || recovered != 2 {
		t.Errorf("ResetAll = (cleared %d, recovered %d), want (2, 2)", cleared, recovered)
	}

	if cb.IsOpen(p1, "test-provider") || cb.IsOpen(p2, "test-provider") {
		t.Error("all circuits should be cleared after ResetAll")
	}
}

// TestCircuitBreaker_ResetAllCountsOnlyBlockingCircuitsAsRecovered separates the
// two counts: every tracked circuit is discarded, but a circuit that was not
// blocking anything must not be reported as a recovered provider.
func TestCircuitBreaker_ResetAllCountsOnlyBlockingCircuitsAsRecovered(t *testing.T) {
	cb := newTestCB(2, 30*time.Second)
	open, healthy := uuid.New(), uuid.New()

	cb.RecordFailure(open, "test-provider")
	cb.RecordFailure(open, "test-provider") // reaches threshold: opens
	cb.RecordFailure(healthy, "test-provider")

	if !cb.IsOpen(open, "test-provider") {
		t.Fatal("setup: circuit should be open")
	}

	cleared, recovered := cb.ResetAll()
	if cleared != 2 {
		t.Errorf("cleared = %d, want 2 (both tracked circuits discarded)", cleared)
	}
	if recovered != 1 {
		t.Errorf("recovered = %d, want 1 (only the open circuit was blocking)", recovered)
	}

	if empty, _ := cb.ResetAll(); empty != 0 {
		t.Errorf("second ResetAll: cleared = %d, want 0", empty)
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

// TestCircuitBreaker_Status_ReportsLogicalHalfOpen verifies that Status mirrors
// GetState's logical cooldown transition: an open circuit whose cooldown has
// elapsed (and is therefore ready to be probed) is reported as "half-open"
// without an in-flight probe request. The internal state only flips to
// StateHalfOpen for the duration of a probe, so without this the half-open
// bucket would be unobservable from the status API.
func TestCircuitBreaker_Status_ReportsLogicalHalfOpen(t *testing.T) {
	cb := newTestCB(1, 50*time.Millisecond)
	pid := uuid.New()

	cb.RecordFailure(pid, "test-provider") // opens

	// Before cooldown elapses, it reports as open.
	if statuses := cb.Status(); statuses[0].State != "open" {
		t.Fatalf("expected 'open' before cooldown, got %q", statuses[0].State)
	}

	// After cooldown elapses, it should report as half-open even though no
	// probe request has flipped the internal state.
	time.Sleep(60 * time.Millisecond)
	statuses := cb.Status()
	if len(statuses) != 1 {
		t.Fatalf("expected 1 status, got %d", len(statuses))
	}
	if statuses[0].State != "half-open" {
		t.Errorf("expected 'half-open' after cooldown, got %q", statuses[0].State)
	}
	// A logically-half-open circuit is ready to probe now, so it must not
	// carry the open-only next-retry hint.
	if statuses[0].NextRetryAt != "" {
		t.Errorf("expected empty NextRetryAt for half-open, got %q", statuses[0].NextRetryAt)
	}
}

func TestCircuitBreaker_Concurrent(t *testing.T) {
	cb := newTestCB(100, 30*time.Second)
	pid := uuid.New()

	var wg sync.WaitGroup
	for range 50 {
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

	for range 4 {
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
	// pinEnabled overrides circuit_breaker_quota_pin_enabled when non-nil.
	pinEnabled *bool
	// pinMax overrides circuit_breaker_quota_pin_max when positive.
	pinMax time.Duration
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
	if key == "circuit_breaker_quota_pin_max" && s.pinMax > 0 {
		return s.pinMax
	}
	return def
}

func (s *stubSettings) GetBool(_ context.Context, key string, def bool) bool {
	if key == "circuit_breaker_quota_pin_enabled" && s.pinEnabled != nil {
		return *s.pinEnabled
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
	for range 2 {
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
	for range 20 {
		wg.Go(func() {
			results <- cb.IsOpen(pid, "test-provider")
		})
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
	for range 50 {
		cb.RecordFailure(pid, "test-provider")
	}

	var wg sync.WaitGroup
	isOpenResults := make(chan bool, 100)
	for range 50 {
		wg.Go(func() {
			isOpenResults <- cb.IsOpen(pid, "test-provider")
		})
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
	for range 10 {
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

// ---------------------------------------------------------------------------
// Quota pin tests
// ---------------------------------------------------------------------------

type stubAdvisor struct {
	at time.Time
	ok bool
}

func (s stubAdvisor) ResetsAt(uuid.UUID) (time.Time, bool) { return s.at, s.ok }

// openBreaker drives a fresh breaker to the Open state using real failures.
func openBreaker(t *testing.T, cb *CircuitBreaker, id uuid.UUID) {
	t.Helper()
	for i := 0; i < cb.effectiveThreshold(); i++ {
		cb.RecordFailure(id, "test-provider")
	}
	if got := cb.GetState(id); got != StateOpen {
		t.Fatalf("setup: got state %v, want open", got)
	}
}

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

func TestGetState_ConcurrentReads(t *testing.T) {
	t.Parallel()
	cb := newTestCB(100, 30*time.Second)
	pid := uuid.New()

	// Pre-populate with some failures
	for range 50 {
		cb.RecordFailure(pid, "test-provider")
	}

	var wg sync.WaitGroup
	errCh := make(chan error, 100)
	for range 100 {
		wg.Go(func() {
			defer func() {
				if r := recover(); r != nil {
					errCh <- fmt.Errorf("panic in GetState: %v", r)
				}
			}()
			s := cb.GetState(pid)
			if s != StateClosed && s != StateOpen {
				errCh <- fmt.Errorf("unexpected state: %v", s)
			}
		})
	}
	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Error(err)
	}
}

// ---------------------------------------------------------------------------
// SSE event quota-pin metadata
// ---------------------------------------------------------------------------

// waitForOpenEvent reads from sub until it sees a "circuit_breaker.open" event
// whose provider_id metadata matches id, or the deadline elapses. Filtering by
// type and provider makes the assertion deterministic even though the shared
// DefaultBus channel could otherwise carry events from a different provider
// or a different transition first.
func waitForOpenEvent(t *testing.T, sub chan events.Event, id uuid.UUID) events.Event {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case ev := <-sub:
			if ev.Type != "circuit_breaker.open" {
				continue
			}
			if pid, _ := ev.Metadata["provider_id"].(string); pid != id.String() {
				continue
			}
			return ev
		case <-deadline:
			t.Fatalf("no circuit_breaker.open event for provider %s published within timeout", id)
			return events.Event{}
		}
	}
}

// ---------------------------------------------------------------------------
// Open-transition log trail
// ---------------------------------------------------------------------------

// capturedLog is one slog record reduced to what these assertions care about.
type capturedLog struct {
	level slog.Level
	msg   string
	attrs map[string]any
}

// logCaptureHandler records every slog record emitted while it is installed.
type logCaptureHandler struct {
	mu      sync.Mutex
	records []capturedLog
}

func (h *logCaptureHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *logCaptureHandler) Handle(_ context.Context, r slog.Record) error {
	rec := capturedLog{level: r.Level, msg: r.Message, attrs: make(map[string]any, r.NumAttrs())}
	r.Attrs(func(a slog.Attr) bool {
		rec.attrs[a.Key] = a.Value.Any()
		return true
	})
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, rec)
	return nil
}

func (h *logCaptureHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *logCaptureHandler) WithGroup(string) slog.Handler      { return h }

// forProvider returns the records whose provider_id attribute matches id, so an
// assertion can never be satisfied by a line another test or another provider
// emitted onto the process-wide logger.
func (h *logCaptureHandler) forProvider(id uuid.UUID) []capturedLog {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []capturedLog
	for _, r := range h.records {
		if pid, ok := r.attrs["provider_id"].(uuid.UUID); ok && pid == id {
			out = append(out, r)
		}
	}
	return out
}

// last returns the most recent record with the given message. Used where the
// line's own provider_id is a plain string (the circuits map is keyed by the
// provider's UUID string), which forProvider's uuid.UUID assertion cannot match.
func (h *logCaptureHandler) last(msg string) (slog.Level, map[string]any, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for i := len(h.records) - 1; i >= 0; i-- {
		if h.records[i].msg == msg {
			return h.records[i].level, h.records[i].attrs, true
		}
	}
	return 0, nil, false
}

// captureLogs installs a capturing slog handler for the duration of the test.
// SetHandler swaps the process-wide default, so the previous one is restored.
func captureLogs(t *testing.T) *logCaptureHandler {
	t.Helper()
	prev := slog.Default().Handler()
	t.Cleanup(func() { debuglog.SetHandler(prev) })
	capt := &logCaptureHandler{}
	debuglog.SetHandler(capt)
	return capt
}

// TestOpenTransitionLogsTheGoverningCooldown covers the operator's only trail
// for a circuit that can stay dark for a day. The open-transition warning
// previously carried the failure count and nothing about how long the provider
// would be sidelined, so "why has this provider been dark since midnight" had no
// answer outside the Failover page and the SSE stream. Both open transitions
// (closed→open and a failed half-open probe) must name the cooldown actually
// governing the circuit and whether a quota pin produced it.
func TestOpenTransitionLogsTheGoverningCooldown(t *testing.T) {
	capt := captureLogs(t)

	cb := NewCircuitBreaker(&stubSettings{threshold: 1, cooldown: 50 * time.Millisecond})
	id := uuid.New()
	cb.SetQuotaAdvisor(stubAdvisor{at: time.Now().Add(6 * time.Hour), ok: true})

	openBreaker(t, cb, id)

	recs := capt.forProvider(id)
	if len(recs) != 1 {
		t.Fatalf("got %d log record(s) for the open transition, want 1: %+v", len(recs), recs)
	}
	opened := recs[0]
	if opened.level != slog.LevelWarn {
		t.Errorf("got level %v, want warn for an opening circuit", opened.level)
	}
	pinned, _ := opened.attrs["quota_pinned"].(bool)
	if !pinned {
		t.Errorf("a pinned circuit must log quota_pinned=true, got attrs %#v", opened.attrs)
	}
	cooldownMs, ok := opened.attrs["cooldown_ms"].(int64)
	if !ok {
		t.Fatalf("the open transition must log cooldown_ms, got attrs %#v", opened.attrs)
	}
	if want := cb.Status()[0].CooldownMs; cooldownMs != want {
		t.Errorf("logged cooldown_ms=%d, want the cooldown actually governing the circuit (%d)", cooldownMs, want)
	}

	// A failed half-open probe re-opens the circuit and must log the same way:
	// this is the transition an operator sees when a provider keeps failing.
	time.Sleep(60 * time.Millisecond)
	cb.Cooldown = 50 * time.Millisecond
	cb.SetQuotaAdvisor(stubAdvisor{ok: false}) // advice withdrawn; plain cooldown now
	cb.circuits[id.String()].cooldownOverride = 0
	cb.circuits[id.String()].state = StateHalfOpen
	cb.RecordFailure(id, "test-provider")

	recs = capt.forProvider(id)
	if len(recs) != 2 {
		t.Fatalf("got %d log record(s), want the half-open→open transition logged too: %+v", len(recs), recs)
	}
	reopened := recs[1]
	if reopened.level != slog.LevelWarn {
		t.Errorf("got level %v, want warn for a failed probe re-opening the circuit", reopened.level)
	}
	if pinned, _ := reopened.attrs["quota_pinned"].(bool); pinned {
		t.Errorf("an unpinned re-open must log quota_pinned=false, got attrs %#v", reopened.attrs)
	}
	reMs, ok := reopened.attrs["cooldown_ms"].(int64)
	if !ok {
		t.Fatalf("the half-open→open transition must log cooldown_ms, got attrs %#v", reopened.attrs)
	}
	if want := cb.effectiveCooldown().Milliseconds(); reMs != want {
		t.Errorf("logged cooldown_ms=%d, want the configured cooldown %d", reMs, want)
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

// TestPublishEvent_UnpinnedOmitsNextRetryAt verifies that when a circuit opens
// with no quota pin in effect (no advisor installed, so cooldownOverride
// stays zero), the published event reports quota_pinned=false and omits
// next_retry_at entirely rather than emitting an empty or zero-valued string.
func TestPublishEvent_UnpinnedOmitsNextRetryAt(t *testing.T) {
	sub := events.Subscribe()
	defer events.Unsubscribe(sub)

	cb := NewCircuitBreaker(nil) // no quota advisor installed
	id := uuid.New()

	openBreaker(t, cb, id)

	ev := waitForOpenEvent(t, sub, id)

	pinned, _ := ev.Metadata["quota_pinned"].(bool)
	if pinned {
		t.Fatalf("unpinned circuit must publish quota_pinned=false, got metadata %#v", ev.Metadata)
	}
	if v, ok := ev.Metadata["next_retry_at"]; ok {
		t.Fatalf("unpinned circuit must omit next_retry_at entirely, got %#v", v)
	}
	if v, ok := ev.Metadata["resets_at"]; ok {
		t.Fatalf("the old resets_at name must not linger alongside it, got %#v", v)
	}
}

// ---------------------------------------------------------------------------
// Open-transition callback tests
// ---------------------------------------------------------------------------

// waitForOnOpen returns the provider the open callback reported, or fails the
// test if nothing arrives. The callback runs on its own goroutine, so a channel
// with a deadline is the only sound way to observe it.
func waitForOnOpen(t *testing.T, got <-chan uuid.UUID) uuid.UUID {
	t.Helper()
	select {
	case id := <-got:
		return id
	case <-time.After(2 * time.Second):
		t.Fatal("open transition did not invoke the open callback")
		return uuid.Nil
	}
}

// TestOnOpen_FiresOnClosedToOpen verifies the callback the quota nudge hangs
// off: a circuit that opens on the ordinary failure-threshold path must report
// which provider went dark, so a fresh quota reading can be fetched while the
// pin still matters.
func TestOnOpen_FiresOnClosedToOpen(t *testing.T) {
	cb := newTestCB(2, 30*time.Second)
	id := uuid.New()

	got := make(chan uuid.UUID, 1)
	cb.SetOnOpen(func(providerID uuid.UUID) { got <- providerID })

	cb.RecordFailure(id, "test-provider")
	cb.RecordFailure(id, "test-provider") // threshold reached → opens

	if reported := waitForOnOpen(t, got); reported != id {
		t.Errorf("callback got provider %s, want %s", reported, id)
	}
}

// TestOnOpen_FiresOnHalfOpenToOpen covers the second open transition: a failed
// half-open probe re-opens the circuit with a fresh cooldown, which is exactly
// the cooldown a quota pin would retarget, so it must nudge too. The callback is
// installed after the first open so only the re-open can feed the channel.
func TestOnOpen_FiresOnHalfOpenToOpen(t *testing.T) {
	cb := newTestCB(1, 0) // zero cooldown: the first IsOpen call hands out a probe
	id := uuid.New()

	cb.RecordFailure(id, "test-provider") // closed→open, no callback installed yet

	got := make(chan uuid.UUID, 1)
	cb.SetOnOpen(func(providerID uuid.UUID) { got <- providerID })

	if cb.IsOpen(id, "test-provider") {
		t.Fatal("setup: elapsed cooldown should hand out a half-open probe")
	}
	if s := cb.GetState(id); s != StateHalfOpen {
		t.Fatalf("setup: got state %v, want half-open", s)
	}

	cb.RecordFailure(id, "test-provider") // probe fails → half-open→open

	if reported := waitForOnOpen(t, got); reported != id {
		t.Errorf("callback got provider %s, want %s", reported, id)
	}
}

// TestOnOpen_NoCallbackIsSafe verifies a breaker with nothing wired still opens
// normally. Every deployment that does not run the quota nudge is this case, and
// an unguarded invocation would panic on the request path.
func TestOnOpen_NoCallbackIsSafe(t *testing.T) {
	cb := newTestCB(1, 30*time.Second)
	id := uuid.New()

	cb.RecordFailure(id, "test-provider")

	if s := cb.GetState(id); s != StateOpen {
		t.Errorf("got state %v, want open", s)
	}
}

// ---------------------------------------------------------------------------
// Quota re-pin tests
// ---------------------------------------------------------------------------

// backdateOpen moves a circuit's open instant into the past, so a cooldown
// computed from openedAt is distinguishable from one computed from now without
// the test waiting out real time.
func backdateOpen(t *testing.T, cb *CircuitBreaker, id uuid.UUID, by time.Duration) {
	t.Helper()
	cb.mu.Lock()
	defer cb.mu.Unlock()
	c, ok := cb.circuits[id.String()]
	if !ok {
		t.Fatalf("setup: no circuit tracked for %s", id)
	}
	c.openedAt = c.openedAt.Add(-by)
}

// overrideFor reads a circuit's stored quota override. Status reports the
// cooldown actually governing the circuit, which hides the override whenever
// pinning is switched off, so assertions about the override itself read it here.
func overrideFor(t *testing.T, cb *CircuitBreaker, id uuid.UUID) time.Duration {
	t.Helper()
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	c, ok := cb.circuits[id.String()]
	if !ok {
		t.Fatalf("no circuit tracked for %s", id)
	}
	return c.cooldownOverride
}

func onlyStatus(t *testing.T, cb *CircuitBreaker) ProviderStatus {
	t.Helper()
	statuses := cb.Status()
	if len(statuses) != 1 {
		t.Fatalf("got %d statuses, want 1", len(statuses))
	}
	return statuses[0]
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
	cb := NewCircuitBreaker(&stubSettings{threshold: 1, cooldown: time.Second, pinMax: time.Hour})
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
