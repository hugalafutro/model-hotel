package failover

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// The breaker under concurrent readers and writers.

func TestCircuitBreaker_Concurrent(t *testing.T) {
	cb := newTestCB(100, 30*time.Second)
	pid := uuid.New()

	var wg sync.WaitGroup
	for range 50 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			cb.RecordFailure(pid, "test-provider", "", Cause{})
		}()
		go func() {
			defer wg.Done()
			_ = cb.IsOpen(pid, "test-provider", "")
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

// TestCircuitBreaker_IsOpen_HalfOpenAllowsProbesConcurrently verifies that
// when a circuit is in half-open state, concurrent IsOpen calls all return
// false (allowing probes through). This exercises the read-lock fast path
// at line 133.
func TestCircuitBreaker_IsOpen_HalfOpenAllowsProbesConcurrently(t *testing.T) {
	t.Parallel()
	cb := newTestCB(1, 50*time.Millisecond)
	pid := uuid.New()

	cb.RecordFailure(pid, "test-provider", "", Cause{}) // opens
	time.Sleep(60 * time.Millisecond)
	cb.IsOpen(pid, "test-provider", "") // triggers transition to half-open

	var wg sync.WaitGroup
	results := make(chan bool, 20)
	for range 20 {
		wg.Go(func() {
			results <- cb.IsOpen(pid, "test-provider", "")
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
		cb.RecordFailure(pid, "test-provider", "", Cause{})
	}

	var wg sync.WaitGroup
	isOpenResults := make(chan bool, 100)
	for range 50 {
		wg.Go(func() {
			isOpenResults <- cb.IsOpen(pid, "test-provider", "")
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

func TestCircuitBreaker_IsOpen_RaceWithRecordSuccess(t *testing.T) {
	t.Parallel()
	cb := newTestCB(1, 50*time.Millisecond)
	pid := uuid.New()

	// Open the circuit
	cb.RecordFailure(pid, "test-provider", "", Cause{})
	time.Sleep(60 * time.Millisecond)

	// Trigger transition to half-open
	cb.IsOpen(pid, "test-provider", "")

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
			_ = cb.IsOpen(pid, "test-provider", "")
		}()
		go func() {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					errCh <- fmt.Errorf("panic in RecordSuccess: %v", r)
				}
			}()
			cb.RecordSuccess(pid, "test-provider", "")
		}()
	}
	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Error(err)
	}

	// Circuit should be closed after successful probe
	if cb.IsOpen(pid, "test-provider", "") {
		t.Error("circuit should be closed after successful probe in half-open state")
	}
}

func TestGetState_ConcurrentReads(t *testing.T) {
	t.Parallel()
	cb := newTestCB(100, 30*time.Second)
	pid := uuid.New()

	// Pre-populate with some failures
	for range 50 {
		cb.RecordFailure(pid, "test-provider", "", Cause{})
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
			s := cb.GetState(pid, "")
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
