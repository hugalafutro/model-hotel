package util

import (
	"testing"
	"time"
)

func TestRateSampler(t *testing.T) {
	var s rateSampler
	base := time.Now()

	if _, ok := s.Rates(base, 100, 200); ok {
		t.Error("the first sample reported a rate")
	}

	rates, ok := s.Rates(base.Add(2*time.Second), 300, 200)
	if !ok {
		t.Fatal("the second sample reported no rate")
	}
	if rates[0] != 100 {
		t.Errorf("rate[0] = %v, want 100", rates[0])
	}
	// A counter that did not move is 0, not a negative or a carried value.
	if rates[1] != 0 {
		t.Errorf("rate[1] = %v, want 0", rates[1])
	}

	// A counter that went backwards (a reset) reports 0 rather than negative.
	rates, ok = s.Rates(base.Add(4*time.Second), 10, 200)
	if !ok {
		t.Fatal("expected a rate after a counter reset")
	}
	if rates[0] != 0 {
		t.Errorf("rate after reset = %v, want 0", rates[0])
	}

	// No wall time between samples means no rate.
	if _, ok := s.Rates(base.Add(4*time.Second), 500, 200); ok {
		t.Error("a zero-duration interval reported a rate")
	}

	// Changing how many counters are tracked reseeds instead of misaligning.
	if _, ok := s.Rates(base.Add(6*time.Second), 1, 2, 3); ok {
		t.Error("a changed counter count reported a rate")
	}
	if _, ok := s.Rates(base.Add(8*time.Second), 1, 2, 3); !ok {
		t.Error("the sampler did not reseed after the counter count changed")
	}

	s.reset()
	if _, ok := s.Rates(base.Add(10*time.Second), 1, 2, 3); ok {
		t.Error("reset did not drop the previous sample")
	}
}
