package ratelimit

import (
	"context"
	"log"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/time/rate"

	"github.com/hugalafutro/model-hotel/internal/ctxkeys"
)

// SettingsReader defines the subset of the settings repository that the
// rate limiter needs. The concrete *settings.Repository satisfies this
// interface, and tests can provide a lightweight stub instead.
type SettingsReader interface {
	GetBool(ctx context.Context, key string, defaultValue bool) bool
	GetFloat(ctx context.Context, key string, defaultValue float64) float64
	GetInt(ctx context.Context, key string, defaultValue int) int
}

// settings keys stored in the database
const (
	settingsKeyEnabled = "rate_limit_enabled"
	settingsKeyRPS     = "rate_limit_rps"
	settingsKeyBurst   = "rate_limit_burst"
)

// default values when no DB setting is present
const (
	defaultRPS   = 10.0
	defaultBurst = 20
)

// Limiter manages per-key rate limiting using token buckets.
// Each virtual key gets its own rate.Limiter. Settings are read from
// the SettingsReader on every request so runtime changes take effect
// without a restart.
type Limiter struct {
	mu          sync.Mutex
	limiters    map[string]*keyEntry
	settings    SettingsReader
	stopCh      chan struct{}
	wasDisabled atomic.Bool // tracks whether rate limiting was off so we can reset buckets on re-enable
}

type keyEntry struct {
	limiter  *rate.Limiter
	rps      float64
	burst    int
	lastUsed time.Time
}

// NewLimiter creates a Limiter that reads configuration from the provided
// SettingsReader. A background goroutine is started to clean up entries
// that have not been used in the last 10 minutes.
func NewLimiter(settings SettingsReader) *Limiter {
	l := &Limiter{
		limiters: make(map[string]*keyEntry),
		settings: settings,
		stopCh:   make(chan struct{}),
	}
	go l.cleanupLoop()
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
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Hard kill-switch from env var
			if !enabled {
				log.Printf("[ratelimit] rate limiting disabled via env")
				next.ServeHTTP(w, r)
				return
			}

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
				l.limiters = make(map[string]*keyEntry)
				l.mu.Unlock()
				log.Printf("[ratelimit] rate limiting re-enabled, reset all buckets")
			}

			keyHash := extractKey(r)
			if keyHash == "" {
				next.ServeHTTP(w, r)
				return
			}

			entry := l.getLimiter(keyHash, r.Context())

			reservation := entry.limiter.Reserve()
			if !reservation.OK() {
				l.writeRateLimitHeaders(w, entry.limiter, 0)
				log.Printf("[ratelimit] warning: rate limit exceeded for key %s", keyHash)
				http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
				return
			}

			delay := reservation.Delay()
			if delay > 0 {
				// Bucket exhausted — cancel the reservation and reject.
				reservation.Cancel()
				l.writeRateLimitHeaders(w, entry.limiter, delay)
				log.Printf("[ratelimit] warning: rate limit exceeded for key %s", keyHash)
				http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
				return
			}

			l.writeRateLimitHeaders(w, entry.limiter, 0)
			next.ServeHTTP(w, r)
		})
	}
}

// getLimiter returns (or creates) the rate.Limiter for the given key.
// If the stored limiter's RPS or burst no longer matches the current
// settings, it is replaced so runtime changes take effect immediately.
func (l *Limiter) getLimiter(keyHash string, ctx context.Context) *keyEntry {
	l.mu.Lock()
	defer l.mu.Unlock()

	rps := l.settings.GetFloat(ctx, settingsKeyRPS, defaultRPS)
	burst := l.settings.GetInt(ctx, settingsKeyBurst, defaultBurst)

	// Unlimited (RPS=0) — use an extremely high rate that never blocks.
	if rps <= 0 {
		rps = 1e6
		burst = 1e6
	}

	entry, ok := l.limiters[keyHash]
	if !ok || entry.rps != rps || entry.burst != burst {
		entry = &keyEntry{
			limiter:  rate.NewLimiter(rate.Limit(rps), burst),
			rps:      rps,
			burst:    burst,
			lastUsed: time.Now(),
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
	return r.RemoteAddr
}

// writeRateLimitHeaders adds standard rate-limit response headers.
func (l *Limiter) writeRateLimitHeaders(w http.ResponseWriter, lim *rate.Limiter, retryAfter time.Duration) {
	w.Header().Set("X-RateLimit-Limit", strconv.FormatFloat(float64(lim.Limit()), 'f', -1, 64))
	w.Header().Set("X-RateLimit-Remaining", strconv.FormatInt(int64(lim.Tokens()), 10))
	w.Header().Set("X-RateLimit-Burst", strconv.Itoa(lim.Burst()))

	if retryAfter > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())+1))
	}
}

// cleanupLoop periodically removes limiter entries that haven't been
// used recently, preventing unbounded memory growth.
func (l *Limiter) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-l.stopCh:
			return
		case <-ticker.C:
			l.cleanup()
		}
	}
}

func (l *Limiter) cleanup() {
	l.mu.Lock()
	defer l.mu.Unlock()

	cutoff := time.Now().Add(-10 * time.Minute)
	for key, entry := range l.limiters {
		if entry.lastUsed.Before(cutoff) {
			delete(l.limiters, key)
		}
	}
}
