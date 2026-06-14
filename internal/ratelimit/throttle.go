package ratelimit

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

// throttleState is the shared edge-triggered throttle bookkeeping used by both
// the per-key and per-IP limiters. It logs one line when an identity starts
// being rate-limited and one when it recovers, rather than one per rejected
// request (a burst could otherwise spam thousands).
//
// `throttled` is atomic so the hot path — noteAllowed's common not-throttled
// serve — is lock-free. throttledAt/rejectedN are guarded by mu, taken only on
// the exceptional rejection/transition paths, so the per-episode count is exact
// under concurrency (concurrent rejections can't lose increments to a racing
// reset).
type throttleState struct {
	mu          sync.Mutex
	throttled   atomic.Bool
	throttledAt time.Time
	rejectedN   int64
}

// throttleLogCtx carries the bits that differ between the key and IP limiters
// for the throttle log lines (message prefix, identity label/value, limits).
type throttleLogCtx struct {
	prefix string // "ratelimit" or "ratelimit-ip"
	label  string // "key" or "ip"
	id     string // the key hash or client IP
	rps    float64
	burst  int
}

// noteRejected records a 429 and logs "throttling started" on the first
// rejection of an episode. Subsequent rejections only bump the (exact) counter,
// so a sustained burst stays quiet in the log.
func (s *throttleState) noteRejected(c throttleLogCtx) {
	s.mu.Lock()
	if !s.throttled.Load() {
		s.throttled.Store(true)
		s.throttledAt = time.Now()
		s.rejectedN = 1
		s.mu.Unlock()
		debuglog.Warn(c.prefix+": throttling started",
			c.label, c.id, "rps", c.rps, "burst", c.burst)
		return
	}
	s.rejectedN++
	s.mu.Unlock()
}

// noteAllowed logs "throttling ended" when a throttled identity is served again
// with no delay (its bucket has fully recovered). The common not-throttled case
// is a lock-free atomic read.
func (s *throttleState) noteAllowed(c throttleLogCtx) {
	if !s.throttled.Load() {
		return
	}
	s.mu.Lock()
	if !s.throttled.Load() {
		s.mu.Unlock()
		return
	}
	s.throttled.Store(false)
	dur := time.Since(s.throttledAt)
	n := s.rejectedN
	s.mu.Unlock()
	logThrottlingEnded(c, "recovered", dur, n)
}

// endIfThrottled closes a still-open episode at eviction time (traffic stopped
// while the identity was rate-limited, so no later serve closed it). end is the
// identity's last activity. No-op when not throttled.
func (s *throttleState) endIfThrottled(c throttleLogCtx, end time.Time, reason string) {
	if !s.throttled.Load() {
		return
	}
	s.mu.Lock()
	dur := end.Sub(s.throttledAt)
	n := s.rejectedN
	s.mu.Unlock()
	logThrottlingEnded(c, reason, dur, n)
}

// logThrottlingEnded emits the episode summary (duration + rejected count),
// computed by the caller under the mutex.
func logThrottlingEnded(c throttleLogCtx, reason string, dur time.Duration, rejected int64) {
	debuglog.Info(c.prefix+": throttling ended",
		c.label, c.id,
		"reason", reason,
		"duration", dur.Round(time.Millisecond).String(),
		"rejected_requests", rejected)
}
