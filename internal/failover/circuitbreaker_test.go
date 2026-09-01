package failover

import (
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/events"
)

func TestCircuitBreaker_StartsClosed(t *testing.T) {
	cb := newTestCB(3, 30*time.Second)
	pid := uuid.New()

	if cb.IsOpen(pid, "test-provider", "") {
		t.Error("new provider should start in closed state")
	}
	if s := cb.GetState(pid, ""); s != StateClosed {
		t.Errorf("expected StateClosed, got %v", s)
	}
}

func TestCircuitBreaker_OpensAfterThreshold(t *testing.T) {
	cb := newTestCB(3, 30*time.Second)
	pid := uuid.New()

	cb.RecordFailure(pid, "test-provider", "", Cause{})
	cb.RecordFailure(pid, "test-provider", "", Cause{})

	if cb.IsOpen(pid, "test-provider", "") {
		t.Error("should not be open after 2 failures (threshold=3)")
	}

	cb.RecordFailure(pid, "test-provider", "", Cause{}) // 3rd failure → opens

	if !cb.IsOpen(pid, "test-provider", "") {
		t.Error("should be open after 3 consecutive failures")
	}
	if s := cb.GetState(pid, ""); s != StateOpen {
		t.Errorf("expected StateOpen, got %v", s)
	}
}

func TestCircuitBreaker_SuccessResetsFailures(t *testing.T) {
	cb := newTestCB(5, 30*time.Second)
	pid := uuid.New()

	for range 4 {
		cb.RecordFailure(pid, "test-provider", "", Cause{})
	}
	cb.RecordSuccess(pid, "test-provider", "") // resets counter

	// Need 5 more failures to open
	for range 4 {
		cb.RecordFailure(pid, "test-provider", "", Cause{})
	}
	if cb.IsOpen(pid, "test-provider", "") {
		t.Error("should still be closed — only 4 failures after reset")
	}

	cb.RecordFailure(pid, "test-provider", "", Cause{}) // 5th → opens
	if !cb.IsOpen(pid, "test-provider", "") {
		t.Error("should be open after 5 consecutive failures post-reset")
	}
}

func TestCircuitBreaker_TransitionsToHalfOpen(t *testing.T) {
	cb := newTestCB(1, 50*time.Millisecond)
	pid := uuid.New()

	cb.RecordFailure(pid, "test-provider", "", Cause{}) // threshold=1 → opens

	if !cb.IsOpen(pid, "test-provider", "") {
		t.Fatal("should be open")
	}

	time.Sleep(60 * time.Millisecond) // wait for cooldown

	// IsOpen should transition to half-open and return false
	if cb.IsOpen(pid, "test-provider", "") {
		t.Error("should have transitioned to half-open after cooldown")
	}
}

func TestCircuitBreaker_HalfOpenProbeSuccess(t *testing.T) {
	cb := newTestCB(1, 50*time.Millisecond)
	pid := uuid.New()

	cb.RecordFailure(pid, "test-provider", "", Cause{}) // opens
	time.Sleep(60 * time.Millisecond)
	cb.IsOpen(pid, "test-provider", "") // triggers Open→HalfOpen

	cb.RecordSuccess(pid, "test-provider", "") // probe succeeds → closes

	if cb.IsOpen(pid, "test-provider", "") {
		t.Error("should be closed after successful probe")
	}
	if s := cb.GetState(pid, ""); s != StateClosed {
		t.Errorf("expected StateClosed, got %v", s)
	}
}

func TestCircuitBreaker_HalfOpenProbeFailure(t *testing.T) {
	cb := newTestCB(1, 50*time.Millisecond)
	pid := uuid.New()

	cb.RecordFailure(pid, "test-provider", "", Cause{}) // opens
	time.Sleep(60 * time.Millisecond)
	cb.IsOpen(pid, "test-provider", "") // triggers Open→HalfOpen

	cb.RecordFailure(pid, "test-provider", "", Cause{}) // probe fails → re-opens

	if !cb.IsOpen(pid, "test-provider", "") {
		t.Error("should be re-opened after failed probe")
	}

	// Should stay open (cooldown not elapsed)
	if !cb.IsOpen(pid, "test-provider", "") {
		t.Error("should still be open (fresh cooldown)")
	}
}

func TestCircuitBreaker_Reset(t *testing.T) {
	cb := newTestCB(1, 30*time.Second)
	pid := uuid.New()

	cb.RecordFailure(pid, "test-provider", "", Cause{}) // opens
	if !cb.IsOpen(pid, "test-provider", "") {
		t.Fatal("should be open")
	}

	if prev := cb.Reset(pid); prev != StateOpen {
		t.Errorf("Reset should report the pre-reset state open, got %v", prev)
	}
	if cb.IsOpen(pid, "test-provider", "") {
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
	cb.RecordFailure(belowThreshold, "test-provider", "", Cause{}) // 1 of 2: still closed
	if prev := cb.Reset(belowThreshold); prev != StateClosed {
		t.Errorf("below-threshold circuit: Reset = %v, want closed", prev)
	}

	// A circuit whose cooldown has elapsed reads as half-open everywhere else
	// (Status, GetState); Reset must not disagree with them.
	readyToProbe := uuid.New()
	cbShort := newTestCB(1, time.Millisecond)
	cbShort.RecordFailure(readyToProbe, "test-provider", "", Cause{})
	time.Sleep(5 * time.Millisecond)
	if prev := cbShort.Reset(readyToProbe); prev != StateHalfOpen {
		t.Errorf("cooldown-elapsed circuit: Reset = %v, want half-open", prev)
	}
}

func TestCircuitBreaker_ResetAll(t *testing.T) {
	cb := newTestCB(1, 30*time.Second)
	p1, p2 := uuid.New(), uuid.New()

	cb.RecordFailure(p1, "test-provider", "", Cause{})
	cb.RecordFailure(p2, "test-provider", "", Cause{})

	cleared, recovered := cb.ResetAll()
	if cleared != 2 || recovered != 2 {
		t.Errorf("ResetAll = (cleared %d, recovered %d), want (2, 2)", cleared, recovered)
	}

	if cb.IsOpen(p1, "test-provider", "") || cb.IsOpen(p2, "test-provider", "") {
		t.Error("all circuits should be cleared after ResetAll")
	}
}

// TestCircuitBreaker_ResetAllCountsOnlyBlockingCircuitsAsRecovered separates the
// two counts: every tracked circuit is discarded, but a circuit that was not
// blocking anything must not be reported as a recovered provider.
func TestCircuitBreaker_ResetAllCountsOnlyBlockingCircuitsAsRecovered(t *testing.T) {
	cb := newTestCB(2, 30*time.Second)
	open, healthy := uuid.New(), uuid.New()

	cb.RecordFailure(open, "test-provider", "", Cause{})
	cb.RecordFailure(open, "test-provider", "", Cause{}) // reaches threshold: opens
	cb.RecordFailure(healthy, "test-provider", "", Cause{})

	if !cb.IsOpen(open, "test-provider", "") {
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

	cb.RecordFailure(pid, "test-provider", "", Cause{}) // opens

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

	cb.RecordFailure(pid, "test-provider", "", Cause{}) // opens

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

func TestCircuitBreaker_UnknownProviderIsClosed(t *testing.T) {
	cb := newTestCB(5, 30*time.Second)
	pid := uuid.New()

	if cb.IsOpen(pid, "test-provider", "") {
		t.Error("unknown provider should not be open")
	}
	if s := cb.GetState(pid, ""); s != StateClosed {
		t.Errorf("expected StateClosed for unknown provider, got %v", s)
	}
}

func TestCircuitBreaker_FailureCountAccuracy(t *testing.T) {
	cb := newTestCB(5, 30*time.Second)
	pid := uuid.New()

	for range 4 {
		cb.RecordFailure(pid, "test-provider", "", Cause{})
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

func TestCircuitBreaker_SettingsOverrideThreshold(t *testing.T) {
	cb := NewCircuitBreaker(&stubSettings{threshold: 2, cooldown: 10 * time.Second})
	cb.HalfOpenMaxProbes = 1
	pid := uuid.New()

	// Default struct threshold is 5, but settings override to 2.
	// After 2 failures, the circuit should open.
	cb.RecordFailure(pid, "test-provider", "", Cause{})
	if cb.IsOpen(pid, "test-provider", "") {
		t.Error("should still be closed after 1 failure (threshold=2)")
	}
	cb.RecordFailure(pid, "test-provider", "", Cause{})
	if !cb.IsOpen(pid, "test-provider", "") {
		t.Error("should be open after 2 failures (settings threshold=2)")
	}
}

func TestCircuitBreaker_SettingsOverrideCooldown(t *testing.T) {
	cb := NewCircuitBreaker(&stubSettings{threshold: 1, cooldown: 50 * time.Millisecond})
	cb.HalfOpenMaxProbes = 1
	pid := uuid.New()

	// Open the circuit.
	cb.RecordFailure(pid, "test-provider", "", Cause{})
	if !cb.IsOpen(pid, "test-provider", "") {
		t.Fatal("should be open after 1 failure")
	}

	// Wait for the short cooldown (50ms) to elapse.
	time.Sleep(80 * time.Millisecond)

	// IsOpen should transition to half-open and return false.
	if cb.IsOpen(pid, "test-provider", "") {
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

	cb.RecordFailure(pid, "test-provider", "", Cause{})
	cb.RecordFailure(pid, "test-provider", "", Cause{})

	if cb.IsOpen(pid, "test-provider", "") {
		t.Error("should not be open after 2 failures (threshold=3)")
	}

	cb.RecordFailure(pid, "test-provider", "", Cause{}) // 3rd failure → opens

	if !cb.IsOpen(pid, "test-provider", "") {
		t.Error("should be open after 3 consecutive failures")
	}

	// Reset and verify that skipping RecordFailure (as the proxy handler
	// does for context errors) means the circuit stays closed.
	cb.Reset(pid)

	cb.RecordFailure(pid, "test-provider", "", Cause{})
	cb.RecordFailure(pid, "test-provider", "", Cause{})
	// 3rd "failure" was a context cancellation → RecordFailure NOT called
	// So we're at 2 failures, not 3. Circuit should remain closed.

	if cb.IsOpen(pid, "test-provider", "") {
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
		cb.RecordFailure(pid, "test-provider", "", Cause{})
	}
	if cb.IsOpen(pid, "test-provider", "") {
		t.Error("should be closed after 2/3 failures")
	}
	cb.RecordFailure(pid, "test-provider", "", Cause{})
	if !cb.IsOpen(pid, "test-provider", "") {
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

	cb.RecordFailure(pid, "test-provider", "", Cause{}) // opens the circuit

	// Immediately after opening, cooldown has not elapsed.
	if !cb.IsOpen(pid, "test-provider", "") {
		t.Error("circuit should still be open (cooldown not elapsed)")
	}

	// Verify internal state is still StateOpen (not half-open)
	cb.mu.RLock()
	c := cb.circuits[pid.String()][""]
	cb.mu.RUnlock()
	if c.state != StateOpen {
		t.Errorf("expected StateOpen, got %v", c.state)
	}
}

func TestCircuitBreaker_IsOpen_HalfOpenState(t *testing.T) {
	t.Parallel()
	cb := newTestCB(1, 50*time.Millisecond)
	pid := uuid.New()

	// Open the circuit
	cb.RecordFailure(pid, "test-provider", "", Cause{})
	if !cb.IsOpen(pid, "test-provider", "") {
		t.Fatal("should be open after 1 failure")
	}

	// Wait for cooldown to elapse
	time.Sleep(60 * time.Millisecond)

	// First IsOpen call transitions to half-open and returns false
	if cb.IsOpen(pid, "test-provider", "") {
		t.Error("IsOpen should return false after transitioning to half-open")
	}

	// Verify state is half-open
	if s := cb.GetState(pid, ""); s != StateHalfOpen {
		t.Errorf("expected StateHalfOpen, got %v", s)
	}

	// Subsequent IsOpen calls while in half-open should also return false
	if cb.IsOpen(pid, "test-provider", "") {
		t.Error("IsOpen should return false for half-open circuit (allow probe)")
	}
}

func TestCircuitBreaker_IsOpen_MultipleProviders(t *testing.T) {
	t.Parallel()
	cb := newTestCB(3, 30*time.Second)
	pid1 := uuid.New()
	pid2 := uuid.New()
	pid3 := uuid.New()

	// Open only pid1
	cb.RecordFailure(pid1, "test-provider", "", Cause{})
	cb.RecordFailure(pid1, "test-provider", "", Cause{})
	cb.RecordFailure(pid1, "test-provider", "", Cause{})

	// pid2 and pid3 should remain closed
	if !cb.IsOpen(pid1, "test-provider", "") {
		t.Error("pid1 should be open after 3 failures")
	}

	if cb.IsOpen(pid2, "test-provider", "") {
		t.Error("pid2 should be closed (no failures recorded)")
	}

	if cb.IsOpen(pid3, "test-provider", "") {
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
	cb.RecordFailure(pid, "test-provider", "", Cause{})

	// Verify it's open
	if !cb.IsOpen(pid, "test-provider", "") {
		t.Error("should be open immediately after failure")
	}

	// Wait for exactly the cooldown period
	time.Sleep(110 * time.Millisecond)

	// IsOpen should now transition to half-open and return false
	isOpen := cb.IsOpen(pid, "test-provider", "")
	if isOpen {
		t.Error("IsOpen should return false after cooldown (half-open state)")
	}

	// Verify the state transitioned
	if s := cb.GetState(pid, ""); s != StateHalfOpen {
		t.Errorf("expected StateHalfOpen after cooldown, got %v", s)
	}
}

func TestCircuitBreaker_IsOpen_UnknownProvider(t *testing.T) {
	t.Parallel()
	cb := newTestCB(3, 30*time.Second)
	unknownPID := uuid.New()

	// Never record any failures for this provider
	if cb.IsOpen(unknownPID, "test-provider", "") {
		t.Error("IsOpen should return false for unknown provider")
	}

	// Verify state is closed
	if s := cb.GetState(unknownPID, ""); s != StateClosed {
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

	cb.RecordFailure(pid, "test-provider", "", Cause{}) // threshold=1 → opens

	// Immediately after opening (no cooldown elapsed), GetState should return StateOpen
	if s := cb.GetState(pid, ""); s != StateOpen {
		t.Errorf("expected StateOpen immediately after opening, got %v", s)
	}
}

func TestGetState_OpenCircuitAfterCooldown(t *testing.T) {
	t.Parallel()
	cb := newTestCB(1, 50*time.Millisecond)
	pid := uuid.New()

	cb.RecordFailure(pid, "test-provider", "", Cause{}) // opens
	time.Sleep(60 * time.Millisecond)                   // wait for cooldown

	// After cooldown, GetState should return StateHalfOpen (logical transition)
	if s := cb.GetState(pid, ""); s != StateHalfOpen {
		t.Errorf("expected StateHalfOpen after cooldown, got %v", s)
	}
}

func TestGetState_DoesNotMutateInternalState(t *testing.T) {
	t.Parallel()
	cb := newTestCB(1, 50*time.Millisecond)
	pid := uuid.New()

	cb.RecordFailure(pid, "test-provider", "", Cause{}) // opens

	// Wait for cooldown
	time.Sleep(60 * time.Millisecond)

	// GetState returns StateHalfOpen but should NOT mutate the internal state
	state := cb.GetState(pid, "")
	if state != StateHalfOpen {
		t.Errorf("expected StateHalfOpen, got %v", state)
	}

	// Internal state should still be StateOpen (GetState computes logical state
	// without mutation). Verify by checking GetState again returns the same.
	state2 := cb.GetState(pid, "")
	if state2 != StateHalfOpen {
		t.Errorf("expected StateHalfOpen on second call, got %v", state2)
	}
}

func TestGetState_ClosedCircuitAfterSuccess(t *testing.T) {
	t.Parallel()
	cb := newTestCB(3, 30*time.Second)
	pid := uuid.New()

	// Record some failures but not enough to open
	cb.RecordFailure(pid, "test-provider", "", Cause{})
	cb.RecordFailure(pid, "test-provider", "", Cause{})

	if s := cb.GetState(pid, ""); s != StateClosed {
		t.Errorf("expected StateClosed after 2/3 failures, got %v", s)
	}
}

func TestGetState_HalfOpenTransitionsToClosedOnSuccess(t *testing.T) {
	t.Parallel()
	cb := newTestCB(1, 50*time.Millisecond)
	pid := uuid.New()

	cb.RecordFailure(pid, "test-provider", "", Cause{}) // opens
	time.Sleep(60 * time.Millisecond)

	// Transition to half-open via IsOpen (which mutates internal state)
	cb.IsOpen(pid, "test-provider", "")

	// Record success → should close
	cb.RecordSuccess(pid, "test-provider", "")

	if s := cb.GetState(pid, ""); s != StateClosed {
		t.Errorf("expected StateClosed after successful probe, got %v", s)
	}
}

func TestGetState_HalfOpenTransitionsToOpenOnFailure(t *testing.T) {
	t.Parallel()
	cb := newTestCB(1, 50*time.Millisecond)
	pid := uuid.New()

	cb.RecordFailure(pid, "test-provider", "", Cause{}) // opens
	time.Sleep(60 * time.Millisecond)

	// Transition to half-open via IsOpen (which mutates internal state)
	cb.IsOpen(pid, "test-provider", "")

	// Record failure in half-open state → should re-open
	cb.RecordFailure(pid, "test-provider", "", Cause{})

	if s := cb.GetState(pid, ""); s != StateOpen {
		t.Errorf("expected StateOpen after failed probe in half-open, got %v", s)
	}
}

// ---------------------------------------------------------------------------
// Quota pin tests
// ---------------------------------------------------------------------------

// TestOpenTransitionLogsTheGoverningCooldown covers the operator's only trail
// for a circuit that can stay dark for a day. The open-transition warning
// previously carried the failure count and nothing about how long the provider
// would be sidelined, so "why has this provider been dark since midnight" had no
// answer outside the Failover page and the SSE stream. Both open transitions
// (closed→open and a failed half-open probe) must name the cooldown actually
// governing the circuit and whether a quota pin produced it.
func TestOpenTransitionLogsTheGoverningCooldown(t *testing.T) {
	capt := captureLogs(t)

	settings := &stubSettings{threshold: 1, cooldown: 50 * time.Millisecond}
	cb := NewCircuitBreaker(settings)
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
	// Seconds, not milliseconds: the re-open below stamps a doubled cooldown,
	// and the assertion reads it back through Status, which reports a circuit
	// past its cooldown as half-open with no cooldown at all. A 100ms window
	// between the two calls is a flake under load; 20s is not.
	settings.cooldown = 10 * time.Second
	cb.SetQuotaAdvisor(stubAdvisor{ok: false}) // advice withdrawn; plain cooldown now
	cb.circuits[id.String()][""].cooldownOverride = 0
	cb.circuits[id.String()][""].state = StateHalfOpen
	cb.RecordFailure(id, "test-provider", "", Cause{})

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
	// After a failed probe the governing cooldown is the doubled one, and the
	// line must say so: the configured 50ms is exactly the number an operator
	// would be misled by.
	if want := cb.Status()[0].CooldownMs; reMs != want || reMs != (20*time.Second).Milliseconds() {
		t.Errorf("logged cooldown_ms=%d, want the cooldown actually governing the circuit (%d, the doubled 10s)", reMs, want)
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

// TestOnOpen_FiresOnClosedToOpen verifies the callback the quota nudge hangs
// off: a circuit that opens on the ordinary failure-threshold path must report
// which provider went dark, so a fresh quota reading can be fetched while the
// pin still matters.
func TestOnOpen_FiresOnClosedToOpen(t *testing.T) {
	cb := newTestCB(2, 30*time.Second)
	id := uuid.New()

	got := make(chan uuid.UUID, 1)
	cb.SetOnOpen(func(providerID uuid.UUID) { got <- providerID })

	cb.RecordFailure(id, "test-provider", "", Cause{})
	cb.RecordFailure(id, "test-provider", "", Cause{}) // threshold reached → opens

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

	cb.RecordFailure(id, "test-provider", "", Cause{}) // closed→open, no callback installed yet

	got := make(chan uuid.UUID, 1)
	cb.SetOnOpen(func(providerID uuid.UUID) { got <- providerID })

	if cb.IsOpen(id, "test-provider", "") {
		t.Fatal("setup: elapsed cooldown should hand out a half-open probe")
	}
	if s := cb.GetState(id, ""); s != StateHalfOpen {
		t.Fatalf("setup: got state %v, want half-open", s)
	}

	cb.RecordFailure(id, "test-provider", "", Cause{}) // probe fails → half-open→open

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

	cb.RecordFailure(id, "test-provider", "", Cause{})

	if s := cb.GetState(id, ""); s != StateOpen {
		t.Errorf("got state %v, want open", s)
	}
}

// ---------------------------------------------------------------------------
// Quota re-pin tests
// ---------------------------------------------------------------------------
