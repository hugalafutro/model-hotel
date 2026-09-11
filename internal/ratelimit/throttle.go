package ratelimit

import (
	"math"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/time/rate"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/httpx"
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
	// budget names WHICH limiter throttled, for deployments running several of
	// them. Two IP limiters with the same rps and burst otherwise produce
	// byte-identical log lines, and "a prober was refused" and "Traefik may be
	// losing its config source" are not the same event. Empty for a lone
	// limiter, which is what the gateway has.
	budget string
	rps    float64
	burst  int
}

// logAttrs renders the identity and limits of one throttle episode.
func (c throttleLogCtx) logAttrs() []any {
	attrs := []any{c.label, c.id}
	if c.budget != "" {
		attrs = append(attrs, "budget", c.budget)
	}
	return append(attrs, "rps", c.rps, "burst", c.burst)
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
		debuglog.Warn(c.prefix+": throttling started", c.logAttrs()...)
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

// bucketEntry is one identity's token bucket plus the edge-triggered throttle
// state its log lines are driven from. The per-key and per-IP limiters share it;
// prefix, label and budget are what tell their log lines apart.
type bucketEntry struct {
	limiter  *rate.Limiter
	rps      float64
	burst    int
	lastUsed time.Time
	throttle throttleState
	prefix   string // message prefix, e.g. "ratelimit-ip"
	label    string // identity label, e.g. "ip"
	budget   string // named budget, set only where a deployment runs several limiters
}

func (e *bucketEntry) throttleCtx(id string) throttleLogCtx {
	return throttleLogCtx{prefix: e.prefix, label: e.label, id: id, budget: e.budget, rps: e.rps, burst: e.burst}
}

func (e *bucketEntry) noteRejected(id string) { e.throttle.noteRejected(e.throttleCtx(id)) }

func (e *bucketEntry) noteAllowed(id string) { e.throttle.noteAllowed(e.throttleCtx(id)) }

// bucketRate normalises a configured rate. rps <= 0 means "no cap", expressed
// as a rate high enough never to block so the request path needs no special
// case for it.
func bucketRate(rps float64, burst int) (float64, int) {
	if rps <= 0 {
		return 1e6, 1e6
	}
	return rps, burst
}

// peekWait reports how long a bucket needs before it can hand out one token,
// without taking anything. A reservation answers the same question, but it
// charges for the answer and gives the charge back only while no later
// reservation has moved the bucket's last event past it: under a flood every
// refusal reserves and cancels, almost none of the cancels refund, and the
// bucket sinks far below empty, throttling the identity long after the flood
// has stopped. A read leaves the bucket where it was.
//
// A bucket that can never hand out a token (burst below one) reports no wait,
// because no amount of waiting would help and the reservation path already
// refuses it, free of charge and without promising a retry time.
func peekWait(lim *rate.Limiter, now time.Time) time.Duration {
	limit := float64(lim.Limit())
	if lim.Burst() < 1 || limit <= 0 {
		return 0
	}
	deficit := 1 - lim.TokensAt(now)
	if deficit <= 0 {
		return 0
	}
	wait := deficit / limit * float64(time.Second)
	if wait > math.MaxInt64 {
		return rate.InfDuration
	}
	return time.Duration(wait)
}

// runCleanup drives a limiter's idle-entry sweep until its stop channel closes.
func runCleanup(stopCh <-chan struct{}, sweep func()) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-stopCh:
			return
		case <-ticker.C:
			sweep()
		}
	}
}

// writeRateLimitHeaders adds the standard rate-limit response headers. A
// non-empty scope names the stage that rejected the request. Retry-After is the
// wait rounded up rather than truncated-plus-one, so a compliant client retries
// at the bucket boundary instead of a second past it.
func writeRateLimitHeaders(w http.ResponseWriter, lim *rate.Limiter, retryAfter time.Duration, scope string) {
	w.Header().Set("X-RateLimit-Limit", strconv.FormatFloat(float64(lim.Limit()), 'f', -1, 64))
	w.Header().Set("X-RateLimit-Remaining", strconv.FormatInt(int64(lim.Tokens()), 10))
	w.Header().Set("X-RateLimit-Burst", strconv.Itoa(lim.Burst()))
	if scope != "" {
		w.Header().Set("X-RateLimit-Scope", scope)
	}
	if retryAfter > 0 {
		httpx.SetRetryAfter(w, retryAfter)
	}
}
