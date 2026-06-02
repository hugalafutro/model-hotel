package provider

import (
	"sync"
	"testing"
)

// ---------------------------------------------------------------------------
// getOrCreateCircuit
// ---------------------------------------------------------------------------

func TestGetOrCreateCircuit_NewProvider(t *testing.T) {
	t.Helper()

	d := NewDiscoveryService(nil, nil)
	circuit := d.getOrCreateCircuit("new-provider-123")

	if circuit == nil {
		t.Fatal("getOrCreateCircuit should return non-nil circuit for new provider")
		return
	}
	if circuit.consecFailures != 0 {
		t.Errorf("new circuit should have consecFailures=0, got %d", circuit.consecFailures)
	}
	if !circuit.openUntil.IsZero() {
		t.Errorf("new circuit should have zero openUntil, got %v", circuit.openUntil)
	}
	// Verify the circuit is usable
	if circuit.isCircuitOpen() {
		t.Error("new circuit should be closed")
	}
}

func TestGetOrCreateCircuit_SameProviderReturnsSame(t *testing.T) {
	t.Helper()

	d := NewDiscoveryService(nil, nil)

	circuit1 := d.getOrCreateCircuit("same-provider-456")
	circuit2 := d.getOrCreateCircuit("same-provider-456")

	if circuit1 != circuit2 {
		t.Error("getOrCreateCircuit should return the same pointer for the same providerID")
	}
	if circuit1 == nil {
		t.Fatal("first call should return non-nil circuit")
		return
	}
}

func TestGetOrCreateCircuit_MalformedValueFallback(t *testing.T) {
	t.Helper()

	d := NewDiscoveryService(nil, nil)

	// Inject a wrong type into the sync.Map
	d.quotaBreaker.Store("malformed-provider", "not-a-circuit")

	circuit := d.getOrCreateCircuit("malformed-provider")

	if circuit == nil {
		t.Fatal("getOrCreateCircuit should return non-nil circuit even for malformed value")
		return
	}
	// Verify the returned circuit is usable (can call isCircuitOpen without panic)
	if circuit.isCircuitOpen() {
		t.Error("fresh circuit from malformed fallback should be closed")
	}
	// Verify it starts with zero failures
	if circuit.consecFailures != 0 {
		t.Errorf("fresh circuit should have consecFailures=0, got %d", circuit.consecFailures)
	}
}

func TestGetOrCreateCircuit_ConcurrentAccess(t *testing.T) {
	t.Helper()

	d := NewDiscoveryService(nil, nil)
	providerID := "concurrent-provider-789"
	const goroutines = 20

	results := make([]*quotaCircuitState, goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := range goroutines {
		go func(idx int) {
			defer wg.Done()
			results[idx] = d.getOrCreateCircuit(providerID)
		}(i)
	}

	wg.Wait()

	// All results should be non-nil
	for i, r := range results {
		if r == nil {
			t.Errorf("goroutine %d: getOrCreateCircuit returned nil", i)
		}
	}

	// At least 2 should return the same pointer (proving LoadOrStore deduplication)
	uniquePointers := make(map[*quotaCircuitState]bool)
	for _, r := range results {
		uniquePointers[r] = true
	}

	if len(uniquePointers) > 1 {
		t.Errorf("expected all goroutines to get the same pointer, but got %d unique pointers", len(uniquePointers))
	}
	if len(uniquePointers) < 1 {
		t.Error("expected at least 1 unique pointer")
	}
}
