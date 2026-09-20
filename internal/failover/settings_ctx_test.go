package failover

import (
	"context"
	"testing"
	"time"
)

// deadlineSettings records whether every read carried a deadline.
type deadlineSettings struct {
	reads, bounded int
}

func (s *deadlineSettings) note(ctx context.Context) {
	s.reads++
	if _, ok := ctx.Deadline(); ok {
		s.bounded++
	}
}

func (s *deadlineSettings) GetInt(ctx context.Context, _ string, def int) int {
	s.note(ctx)
	return def
}

func (s *deadlineSettings) GetDuration(ctx context.Context, _ string, def time.Duration) time.Duration {
	s.note(ctx)
	return def
}

func (s *deadlineSettings) GetBool(ctx context.Context, _ string, def bool) bool {
	s.note(ctx)
	return def
}

// Every settings read the breaker makes is bounded: several run under cb.mu,
// where a stalled store would otherwise hold every request's breaker verdict.
func TestCircuitBreaker_SettingsReadsAreBounded(t *testing.T) {
	s := &deadlineSettings{}
	cb := NewCircuitBreaker(s)
	cb.effectiveThreshold()
	cb.effectiveSpan()
	cb.effectiveCooldown()
	cb.pinProbeInterval()
	ceilingOrDefault(s, "circuit_breaker_backoff_max", time.Minute)
	if s.reads != 5 || s.bounded != s.reads {
		t.Fatalf("reads = %d, bounded = %d; want every read to carry a deadline", s.reads, s.bounded)
	}
}
