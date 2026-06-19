package totp

import (
	"sync"
	"time"
)

// maxThrottleEntries bounds the in-memory key map so a key-rotating attacker
// cannot grow it without limit; once exceeded, expired entries are swept.
const maxThrottleEntries = 4096

// Throttle applies per-key exponential backoff to repeated failures, layered on
// top of the per-IP request-rate limiter. It is in-memory and single-instance
// (consistent with the existing rate limiter and circuit breaker, which are
// also in-process pre-HA).
//
// Backoff is always bounded by maxDelay and self-clears, and a successful
// attempt resets the key. This slows brute force of the TOTP second factor
// (which is only reachable by someone already holding the admin first factor)
// without ever permanently locking out the admin: during a sustained attack the
// admin's own attempts may be delayed up to maxDelay, but never refused outright
// once the delay elapses.
type Throttle struct {
	mu          sync.Mutex
	entries     map[string]*throttleEntry
	maxFailures int           // failures permitted before backoff begins
	baseDelay   time.Duration // first backoff step
	maxDelay    time.Duration // cap on backoff
}

type throttleEntry struct {
	failures    int
	lockedUntil time.Time
	lastSeen    time.Time
}

// NewThrottle constructs a Throttle. maxFailures is the number of failures
// allowed before backoff kicks in; baseDelay is the first lock duration and
// maxDelay caps the exponential growth.
func NewThrottle(maxFailures int, baseDelay, maxDelay time.Duration) *Throttle {
	return &Throttle{
		entries:     make(map[string]*throttleEntry),
		maxFailures: maxFailures,
		baseDelay:   baseDelay,
		maxDelay:    maxDelay,
	}
}

// Allowed reports whether a request for key may proceed now. When the key is in
// backoff it returns (false, retryAfter) where retryAfter is the remaining lock
// duration; otherwise (true, 0).
func (t *Throttle) Allowed(key string) (bool, time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()
	e := t.entries[key]
	if e == nil {
		return true, 0
	}
	if now := time.Now(); now.Before(e.lockedUntil) {
		return false, time.Until(e.lockedUntil)
	}
	return true, 0
}

// RecordFailure registers a failed attempt for key and, once the failure count
// exceeds maxFailures, extends the backoff window exponentially (capped).
func (t *Throttle) RecordFailure(key string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := time.Now()
	if len(t.entries) > maxThrottleEntries {
		t.sweepLocked(now)
	}
	e := t.entries[key]
	if e == nil {
		e = &throttleEntry{}
		t.entries[key] = e
	}
	e.failures++
	e.lastSeen = now
	if d := t.backoffFor(e.failures); d > 0 {
		e.lockedUntil = now.Add(d)
	}
}

// RecordSuccess clears any backoff state for key.
func (t *Throttle) RecordSuccess(key string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.entries, key)
}

// backoffFor returns the lock duration for a given failure count: 0 until
// maxFailures is exceeded, then baseDelay * 2^(n-maxFailures-1), capped at
// maxDelay (and guarded against shift overflow).
func (t *Throttle) backoffFor(failures int) time.Duration {
	shift := failures - t.maxFailures - 1
	if shift < 0 {
		return 0
	}
	if shift >= 63 {
		return t.maxDelay
	}
	d := t.baseDelay << uint(shift)
	if d <= 0 || d > t.maxDelay {
		return t.maxDelay
	}
	return d
}

// sweepLocked removes entries that are no longer locked and have not been seen
// for at least maxDelay. Caller must hold t.mu.
func (t *Throttle) sweepLocked(now time.Time) {
	for k, e := range t.entries {
		if now.After(e.lockedUntil) && now.Sub(e.lastSeen) > t.maxDelay {
			delete(t.entries, k)
		}
	}
}
