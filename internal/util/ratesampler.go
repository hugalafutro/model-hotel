package util

import (
	"slices"
	"sync"
	"time"
)

// rateSampler turns cumulative counters into per-second rates by remembering
// the previous reading. The first call seeds the state and reports ok=false,
// because a single sample of a cumulative counter says nothing about a rate;
// so does a call that arrives with no time between it and the last one, or one
// that changes how many counters it is tracking.
//
// A counter that went backwards (a reset, an interface that disappeared) yields
// 0 for that slot rather than a negative rate.
type rateSampler struct {
	mu   sync.Mutex
	at   time.Time
	prev []int64
}

func (s *rateSampler) Rates(now time.Time, totals ...int64) (perSec []float64, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	prev, at := s.prev, s.at
	s.prev = slices.Clone(totals)
	s.at = now

	if at.IsZero() || len(prev) != len(totals) {
		return nil, false
	}
	deltaSec := now.Sub(at).Seconds()
	if deltaSec <= 0 {
		return nil, false
	}
	perSec = make([]float64, len(totals))
	for i, total := range totals {
		if delta := total - prev[i]; delta > 0 {
			perSec[i] = float64(delta) / deltaSec
		}
	}
	return perSec, true
}

// reset drops the stored sample so the next call seeds afresh. Tests use it to
// start from a known state.
func (s *rateSampler) reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.at = time.Time{}
	s.prev = nil
}
