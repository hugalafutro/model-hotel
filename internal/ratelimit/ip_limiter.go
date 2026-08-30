// Package ratelimit provides token-bucket rate limiting middleware.
package ratelimit

import (
	"context"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"github.com/hugalafutro/model-hotel/internal/clientip"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// default IP-based rate limit values (used when no DB setting is present)
const (
	defaultIPRPS   = 30.0
	defaultIPBurst = 60
)

// ipEntry tracks a single IP address's rate limiter.
type ipEntry struct {
	limiter  *rate.Limiter
	rps      float64
	burst    int
	lastUsed time.Time
	throttle throttleState // edge-triggered throttle logging (see throttle.go)
}

// throttleCtx builds the per-IP logging context for the shared throttleState.
func (e *ipEntry) throttleCtx(ip string) throttleLogCtx {
	return throttleLogCtx{prefix: "ratelimit-ip", label: "ip", id: ip, rps: e.rps, burst: e.burst}
}

func (e *ipEntry) noteRejected(ip string) {
	e.throttle.noteRejected(e.throttleCtx(ip))
}

func (e *ipEntry) noteAllowed(ip string) {
	e.throttle.noteAllowed(e.throttleCtx(ip))
}

// settings keys for IP rate limiter (stored in DB)
const (
	settingsKeyIPEnabled   = "rate_limit_ip_enabled"
	settingsKeyIPRPS       = "rate_limit_ip_rps"
	settingsKeyIPBurst     = "rate_limit_ip_burst"
	settingsKeyIPMaxWaitMs = "rate_limit_max_wait_ms" // shared with per-key limiter
)

// IPLimiter provides per-IP rate limiting as a DoS safety net.
// RPS and burst are read from DB settings on every request so changes
// take effect at runtime without a restart. Constructor arguments serve
// as fallback defaults when no DB setting exists.
//
// It should be mounted BEFORE the auth middleware so it catches
// unauthenticated floods (brute-force key guessing, etc.).
type IPLimiter struct {
	mu             sync.Mutex
	limiters       map[string]*ipEntry
	defaultRPS     float64 // fallback when no DB setting
	defaultBurst   int     // fallback when no DB setting
	stopCh         chan struct{}
	trustedProxies []*net.IPNet
	settings       SettingsReader
}

// NewIPLimiter creates an IP rate limiter. The rps and burst parameters
// serve as default values when no DB setting is present. If rps <= 0 or
// burst <= 0, built-in defaults (30/60) are used instead. A background
// goroutine cleans up entries idle for 10 minutes.
func NewIPLimiter(rps float64, burst int, trustedProxies []*net.IPNet, settings SettingsReader) *IPLimiter {
	if rps <= 0 {
		rps = defaultIPRPS
	}
	if burst <= 0 {
		burst = defaultIPBurst
	}
	l := &IPLimiter{
		limiters:       make(map[string]*ipEntry),
		defaultRPS:     rps,
		defaultBurst:   burst,
		stopCh:         make(chan struct{}),
		trustedProxies: trustedProxies,
		settings:       settings,
	}
	go l.cleanupLoop()
	return l
}

// Stop terminates the background cleanup goroutine.
func (l *IPLimiter) Stop() {
	close(l.stopCh)
}

// ClientIP returns the request's client IP using the same trusted-proxy aware
// resolution as everything else (internal/clientip owns it), so failure-backoff
// keys line up with rate-limit keys and with every logged address.
func (l *IPLimiter) ClientIP(r *http.Request) string {
	return clientip.Resolve(r, l.trustedProxies)
}

// chargedMarker marks a request as already charged against one limiter.
//
// The same limiter is mounted both across a whole route tree and again on
// individual login ceremonies, so without this a single login request draws two
// tokens from its bucket. That is not merely wasteful: the second charge is
// billed when the bucket is already one token poorer, so below ~5 rps its delay
// exceeds max_wait and every login attempt is refused, locking an operator out
// of every way in. The marker is keyed by the limiter so two DIFFERENT limiters
// on one request still charge independently.
type chargedMarker struct{ limiter *IPLimiter }

// Middleware returns an HTTP middleware that rate-limits requests per
// client IP. On limit violation the middleware responds with HTTP 429
// and sets Retry-After and X-RateLimit-* headers. One request costs one token
// however many times this limiter is mounted on the route.
func (l *IPLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Runtime toggle from DB settings; default true for safety.
		if l.settings != nil {
			if !l.settings.GetBool(r.Context(), settingsKeyIPEnabled, true) {
				next.ServeHTTP(w, r)
				return
			}
		}

		if r.Context().Value(chargedMarker{l}) != nil {
			next.ServeHTTP(w, r)
			return
		}
		r = r.WithContext(context.WithValue(r.Context(), chargedMarker{l}, struct{}{}))

		ip := clientip.Resolve(r, l.trustedProxies)
		entry := l.getLimiter(r.Context(), ip)

		reservation := entry.limiter.Reserve()
		if !reservation.OK() {
			entry.noteRejected(ip)
			l.writeHeaders(w, entry.limiter, 0)
			util.WriteOpenAIError(w, "rate limit exceeded", http.StatusTooManyRequests)
			return
		}

		delay := reservation.Delay()
		if delay > 0 {
			// Graceful backpressure: if the wait is within the configured max_wait,
			// sleep and proceed instead of rejecting immediately. The IP is still
			// under pressure, so an open throttle episode stays open (only a
			// no-delay serve below closes it).
			maxWait := time.Duration(defaultMaxWaitMs) * time.Millisecond
			if l.settings != nil {
				maxWait = time.Duration(l.settings.GetInt(r.Context(), settingsKeyIPMaxWaitMs, defaultMaxWaitMs)) * time.Millisecond
			}
			if delay <= maxWait {
				time.Sleep(delay)
				l.writeHeaders(w, entry.limiter, 0)
				next.ServeHTTP(w, r)
				return
			}
			// Wait exceeds max_wait - cancel the reservation and reject.
			reservation.Cancel()
			entry.noteRejected(ip)
			l.writeHeaders(w, entry.limiter, delay)
			util.WriteOpenAIError(w, "rate limit exceeded", http.StatusTooManyRequests)
			return
		}

		// Served with no delay — the bucket has recovered, so close any open
		// throttle episode for this IP.
		entry.noteAllowed(ip)
		l.writeHeaders(w, entry.limiter, 0)
		next.ServeHTTP(w, r)
	})
}

func (l *IPLimiter) getLimiter(ctx context.Context, ip string) *ipEntry {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Read RPS/burst from DB settings, falling back to constructor defaults.
	rps := l.defaultRPS
	burst := l.defaultBurst
	if l.settings != nil {
		rps = l.settings.GetFloat(ctx, settingsKeyIPRPS, l.defaultRPS)
		burst = l.settings.GetInt(ctx, settingsKeyIPBurst, l.defaultBurst)
	}

	// Unlimited (RPS=0) — use an extremely high rate that never blocks.
	if rps <= 0 {
		rps = 1e6
		burst = 1e6
	}

	entry, ok := l.limiters[ip]
	if !ok || entry.rps != rps || entry.burst != burst {
		entry = &ipEntry{
			limiter:  rate.NewLimiter(rate.Limit(rps), burst),
			rps:      rps,
			burst:    burst,
			lastUsed: time.Now(),
		}
		l.limiters[ip] = entry
	} else {
		entry.lastUsed = time.Now()
	}
	return entry
}

func (l *IPLimiter) writeHeaders(w http.ResponseWriter, lim *rate.Limiter, retryAfter time.Duration) {
	w.Header().Set("X-RateLimit-Limit", strconv.FormatFloat(float64(lim.Limit()), 'f', -1, 64))
	w.Header().Set("X-RateLimit-Remaining", strconv.FormatInt(int64(lim.Tokens()), 10))
	w.Header().Set("X-RateLimit-Burst", strconv.Itoa(lim.Burst()))
	w.Header().Set("X-RateLimit-Scope", "ip")

	if retryAfter > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())+1))
	}
}

func (l *IPLimiter) cleanupLoop() {
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

func (l *IPLimiter) cleanup() {
	l.mu.Lock()
	defer l.mu.Unlock()
	cutoff := time.Now().Add(-10 * time.Minute)
	for ip, entry := range l.limiters {
		if entry.lastUsed.Before(cutoff) {
			// Close any still-open throttle episode (traffic stopped while the
			// IP was rate-limited).
			entry.throttle.endIfThrottled(entry.throttleCtx(ip), entry.lastUsed, "idle")
			delete(l.limiters, ip)
		}
	}
}
