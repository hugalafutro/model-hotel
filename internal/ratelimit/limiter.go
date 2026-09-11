package ratelimit

import (
	"context"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/time/rate"

	"github.com/hugalafutro/model-hotel/internal/clientip"
	"github.com/hugalafutro/model-hotel/internal/ctxkeys"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// SettingsReader defines the subset of the settings repository that the
// rate limiter needs. The concrete *settings.Repository satisfies this
// interface, and tests can provide a lightweight stub instead.
type SettingsReader interface {
	GetBool(ctx context.Context, key string, defaultValue bool) bool
	GetDuration(ctx context.Context, key string, defaultValue time.Duration) time.Duration
	GetFloat(ctx context.Context, key string, defaultValue float64) float64
	GetInt(ctx context.Context, key string, defaultValue int) int
}

// settings keys stored in the database
const (
	settingsKeyEnabled   = "rate_limit_enabled"
	settingsKeyRPS       = "rate_limit_rps"
	settingsKeyBurst     = "rate_limit_burst"
	settingsKeyMaxWaitMs = "rate_limit_max_wait_ms"
)

// default values when no DB setting is present
const (
	defaultRPS       = 10.0
	defaultBurst     = 20
	defaultMaxWaitMs = 200
)

// Limiter manages per-key rate limiting using token buckets.
// Each virtual key gets its own rate.Limiter. Settings are read from
// the SettingsReader on every request so runtime changes take effect
// without a restart.
type Limiter struct {
	mu          sync.Mutex
	limiters    map[string]*bucketEntry
	settings    SettingsReader
	stopCh      chan struct{}
	wasDisabled atomic.Bool // tracks whether rate limiting was off so we can reset buckets on re-enable
}

// The prefix and label every per-key bucket logs under (see bucketEntry).
const (
	keyLogPrefix = "ratelimit"
	keyLogLabel  = "key"
)

// NewLimiter creates a Limiter that reads configuration from the provided
// SettingsReader. A background goroutine is started to clean up entries
// that have not been used in the last 10 minutes.
func NewLimiter(settings SettingsReader) *Limiter {
	l := &Limiter{
		limiters: make(map[string]*bucketEntry),
		settings: settings,
		stopCh:   make(chan struct{}),
	}
	go runCleanup(l.stopCh, l.cleanup)
	return l
}

// Stop terminates the background cleanup goroutine. Call this when the
// server is shutting down (e.g. via defer).
func (l *Limiter) Stop() {
	close(l.stopCh)
}

// Middleware returns an HTTP middleware that rate-limits requests per
// virtual key. The key identity is read from the "virtual_key_hash"
// context value (set by the proxy key middleware).
//
// The enabled parameter acts as a hard kill-switch (driven by the
// RATE_LIMIT_ENABLED env var at startup). When false, the middleware
// is a complete no-op. When true, the DB setting "rate_limit_enabled"
// controls whether limiting is active at runtime.
//
// On limit violation the middleware responds with HTTP 429 and sets
// Retry-After and X-RateLimit-* headers.
func (l *Limiter) Middleware(enabled bool) func(http.Handler) http.Handler {
	// Log the env kill-switch once at wiring time instead of on every request.
	if !enabled {
		debuglog.Info("ratelimit: per-key rate limiting disabled via env (RATE_LIMIT_ENABLED=false)")
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Hard kill-switch from env var
			if !enabled {
				next.ServeHTTP(w, r)
				return
			}

			settingsStart := time.Now()

			// Runtime toggle from DB settings
			if !l.settings.GetBool(r.Context(), settingsKeyEnabled, true) {
				l.wasDisabled.Store(true)
				next.ServeHTTP(w, r)
				return
			}

			// If rate limiting was previously disabled, evict all existing
			// limiters so every key gets a fresh bucket on re-enable.
			if l.wasDisabled.CompareAndSwap(true, false) {
				l.mu.Lock()
				l.limiters = make(map[string]*bucketEntry)
				l.mu.Unlock()
				debuglog.Info("ratelimit: rate limiting re-enabled, reset all buckets")
			}

			keyHash := extractKey(r)
			if keyHash == "" {
				next.ServeHTTP(w, r)
				return
			}

			// Read per-key rate limit overrides from context (set by ProxyKeyMiddleware).
			var perKeyRPS *float64
			var perKeyBurst *int
			if v := r.Context().Value(ctxkeys.VirtualKeyRateLimitRPSKey); v != nil {
				if p, ok := v.(*float64); ok {
					perKeyRPS = p
				}
			}
			if v := r.Context().Value(ctxkeys.VirtualKeyRateLimitBurstKey); v != nil {
				if p, ok := v.(*int); ok {
					perKeyBurst = p
				}
			}

			entry := l.getLimiter(r.Context(), keyHash, perKeyRPS, perKeyBurst)

			// User-level aggregate stage: when the key belongs to a user with
			// an RPS cap, all their keys share one "user:<uuid>" bucket. The
			// user reservation is taken first and cancelled if the per-key
			// stage rejects, so a 429 never burns aggregate budget.
			var userEntry *bucketEntry
			userKey := ""
			if uid, ok := r.Context().Value(ctxkeys.VirtualKeyOwnerIDKey).(string); ok && uid != "" {
				if uRPS, ok := r.Context().Value(ctxkeys.UserRateLimitRPSKey).(*float64); ok && uRPS != nil {
					userKey = "user:" + uid
					var uBurst *int
					if b, ok := r.Context().Value(ctxkeys.UserRateLimitBurstKey).(*int); ok {
						uBurst = b
					}
					userEntry = l.getLimiter(r.Context(), userKey, uRPS, uBurst)
				}
			}

			// Capture total settings read time (GetBool above + GetFloat/GetInt inside getLimiter).
			// Use a pointer so downstream handlers (resolve, proxy) can accumulate
			// additional settings reads via ctxkeys.AddSettingsReadMs.
			settingsReadMs := util.MillisSince(settingsStart)
			var settingsReadMsVal = settingsReadMs
			ctx := context.WithValue(r.Context(), ctxkeys.SettingsReadMsKey, &settingsReadMsVal)

			maxWait := time.Duration(l.settings.GetInt(r.Context(), settingsKeyMaxWaitMs, defaultMaxWaitMs)) * time.Millisecond

			// The reservations, the delay reads and the cancellations on the
			// two reject paths share this one instant. The abandoned path
			// takes a fresh one, for the reason given there. A refund is honoured only while the
			// reservation's activation time has not passed, and a reservation
			// taken for immediate use activates at the instant it was taken, so
			// reading the clock again at cancel time turns the zero-delay
			// hand-backs into silent no-ops: the owner token next to a per-key
			// rejection, and whichever stage did not force the wait on the
			// over-max_wait path. Both reject without waiting, so cancelling at
			// this instant rewinds the bucket clock by nothing measurable.
			// admitUserTPM pins for the same reason.
			now := time.Now()

			// reject answers one 429, naming the stage that refused so an
			// owner-wide refusal reads differently from a per-key one.
			reject := func(by *bucketEntry, id string, retryAfter time.Duration) {
				by.noteRejected(id)
				writeRateLimitHeaders(w, by.limiter, retryAfter, "")
				util.WriteOpenAIError(w, rejectedBy(by, userEntry), http.StatusTooManyRequests)
			}

			// Refuse before reserving when the buckets already say the wait is
			// past the ceiling, so a refusal costs the identity nothing: see
			// peekWait for what the reserve-then-cancel route costs instead.
			// The reading can be stale by the time the reservations below are
			// taken, which is the case the cancels on the over-max_wait path
			// still cover.
			if peeked, by, id := peekAdmission(entry, keyHash, userEntry, userKey, now); peeked > maxWait {
				reject(by, id, peeked)
				return
			}

			var userRes *rate.Reservation
			if userEntry != nil {
				userRes = userEntry.limiter.ReserveN(now, 1)
				if !userRes.OK() {
					reject(userEntry, userKey, 0)
					return
				}
			}

			reservation := entry.limiter.ReserveN(now, 1)
			if !reservation.OK() {
				if userRes != nil {
					userRes.CancelAt(now)
				}
				reject(entry, keyHash, 0)
				return
			}

			delay := reservation.DelayFrom(now)
			limitedBy := entry
			limitedKey := keyHash
			if userRes != nil {
				if ud := userRes.DelayFrom(now); ud > delay {
					delay = ud
					limitedBy = userEntry
					limitedKey = userKey
				}
			}
			if delay > 0 {
				// Graceful backpressure: if the wait is within the configured max_wait,
				// sleep and proceed instead of rejecting immediately. The key is still
				// under pressure, so an open throttle episode is left open (only a
				// no-delay serve below closes it).
				if delay <= maxWait {
					if !waitOrCancel(ctx, delay) {
						// Client left during the wait: give the budget back,
						// at a fresh instant rather than the pinned one. A
						// refund rewinds the bucket's clock to the instant it
						// is made, so refunding at the pinned instant would
						// re-credit the whole elapsed wait to every later
						// request, and a client that abandons in a loop could
						// inflate the bucket. The stage that forced the wait
						// still gets its token back, since its reservation
						// activates around now, though a wait that elapsed
						// before the client's departure was noticed can leave
						// even that one behind. A stage gets its token back
						// only while its own reservation is still ahead of this
						// instant, so a stage that was ready to serve keeps
						// one: a bounded over-charge, taken deliberately over
						// an unbounded under-charge.
						left := time.Now()
						reservation.CancelAt(left)
						if userRes != nil {
							userRes.CancelAt(left)
						}
						return
					}
					writeRateLimitHeaders(w, entry.limiter, 0, "")
					next.ServeHTTP(w, r.WithContext(ctx))
					return
				}
				// Wait exceeds max_wait — cancel the reservations and reject,
				// reporting whichever stage forced the longer wait.
				reservation.CancelAt(now)
				if userRes != nil {
					userRes.CancelAt(now)
				}
				reject(limitedBy, limitedKey, delay)
				return
			}

			// Served with no delay — the bucket has recovered, so close any open
			// throttle episode for this key (and the owner's aggregate bucket).
			entry.noteAllowed(keyHash)
			if userEntry != nil {
				userEntry.noteAllowed(userKey)
			}
			writeRateLimitHeaders(w, entry.limiter, 0, "")
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// peekAdmission reports the longest wait either admission stage still needs
// before it can hand this request a token, and which stage that is, without
// taking anything from either bucket.
func peekAdmission(entry *bucketEntry, keyID string, userEntry *bucketEntry, userID string, now time.Time) (time.Duration, *bucketEntry, string) {
	wait := peekWait(entry.limiter, now)
	if userEntry != nil {
		if uw := peekWait(userEntry.limiter, now); uw > wait {
			return uw, userEntry, userID
		}
	}
	return wait, entry, keyID
}

// rejectedBy names the stage a 429 came from, so an owner whose whole account
// is saturated is not told one of their keys is.
func rejectedBy(by, userEntry *bucketEntry) string {
	if by == userEntry {
		return "user rate limit exceeded"
	}
	return "rate limit exceeded"
}

// getLimiter returns (or creates) the rate.Limiter for the given key.
// If per-key overrides are provided (non-nil), they take precedence over
// global settings. If the stored limiter's RPS or burst no longer matches,
// it is replaced so runtime changes take effect immediately.
func (l *Limiter) getLimiter(ctx context.Context, keyHash string, perKeyRPS *float64, perKeyBurst *int) *bucketEntry {
	l.mu.Lock()
	defer l.mu.Unlock()

	rps := l.settings.GetFloat(ctx, settingsKeyRPS, defaultRPS)
	burst := l.settings.GetInt(ctx, settingsKeyBurst, defaultBurst)

	// Per-key overrides take precedence over global settings.
	if perKeyRPS != nil {
		rps = *perKeyRPS
	}
	if perKeyBurst != nil {
		burst = *perKeyBurst
	}

	// Fleet fair-share: N active members behind Traefik's round-robin /v1 pool each
	// enforce 1/N of the configured cap, so the N local shares sum to the global
	// limit. Applied only to finite caps (rps>0); "unlimited" (rps<=0) is handled
	// by the sentinel branch below and must not be divided. Burst floors to >=1 so
	// a small cap on a large fleet never rounds to a zero-burst (block-everything)
	// limiter. The sustained rate stays exact (rps divides as a float), so only
	// the instantaneous burst can exceed the cap, and only when burst < N: the
	// aggregate initial burst reaches N instead of the configured value — a
	// bounded, one-time cold-start overshoot, accepted as the lesser-evil versus
	// a zero burst. See fleetShareTPM for the same tradeoff on the token budget.
	if n := fleetDivisor(ctx, l.settings); n > 1 && rps > 0 {
		rps /= float64(n)
		burst = max(1, burst/n)
	}

	rps, burst = bucketRate(rps, burst)

	entry, ok := l.limiters[keyHash]
	if !ok || entry.rps != rps || entry.burst != burst {
		entry = &bucketEntry{
			limiter:  rate.NewLimiter(rate.Limit(rps), burst),
			rps:      rps,
			burst:    burst,
			lastUsed: time.Now(),
			prefix:   keyLogPrefix,
			label:    keyLogLabel,
		}
		l.limiters[keyHash] = entry
	} else {
		entry.lastUsed = time.Now()
	}
	return entry
}

// extractKey reads the virtual key hash from the request context.
// It uses the shared ctxkeys.VirtualKeyHashKey constant so that
// context.Value lookups succeed (Go requires an exact type match
// on context keys).
// Falls back to the remote address if no key identity is available
// (shouldn't happen in the proxy path where ProxyKeyMiddleware runs first).
func extractKey(r *http.Request) string {
	if v := r.Context().Value(ctxkeys.VirtualKeyHashKey); v != nil {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	// The fallback is address-keyed, never connection-keyed. r.RemoteAddr
	// carries the TCP port, so on a surface that reaches this stage without a
	// virtual key — the admin chat routes, which mount the limiter without
	// ProxyKeyMiddleware — every new connection drew a fresh full-burst bucket,
	// so a client that does not reuse connections escaped this stage entirely.
	// Not unbounded: the per-IP limiter still stood in front at its own looser
	// budget, so what the bug cost was the tighter per-key stage, not the
	// surface. That is the shape the surface's own review described as
	// address-keyed; the port was the unreviewed part.
	//
	// clientip.From is the address the rest of the chain already reports:
	// trusted-proxy aware, port stripped, and it honours a forwarded header
	// only when the peer is a configured proxy, so a direct client cannot pick
	// its own bucket. /v1 is unaffected either way, since the hash is always
	// present there and this line never runs.
	return clientip.From(r)
}

func (l *Limiter) cleanup() {
	l.mu.Lock()
	defer l.mu.Unlock()

	cutoff := time.Now().Add(-10 * time.Minute)
	for key, entry := range l.limiters {
		if entry.lastUsed.Before(cutoff) {
			// Close any still-open throttle episode (traffic stopped while the
			// key was rate-limited, so no later request closed it).
			entry.throttle.endIfThrottled(entry.throttleCtx(key), entry.lastUsed, "idle")
			delete(l.limiters, key)
		}
	}
}

// waitOrCancel sleeps for delay unless ctx ends first. It reports false when
// the context ended, so callers can return the reserved budget instead of
// holding it for a client that has already gone.
func waitOrCancel(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}
