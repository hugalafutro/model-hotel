// Package ratelimit provides token-bucket rate limiting middleware.
package ratelimit

import (
	"context"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/time/rate"

	"github.com/hugalafutro/model-hotel/internal/config"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
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

	// Edge-triggered throttle state — see keyEntry for the rationale. Logs one
	// line when an IP starts being throttled and one when it recovers, instead
	// of one per rejected request.
	throttled   atomic.Bool
	throttledAt atomic.Int64 // unix-nano of the current episode's first rejection
	rejectedN   atomic.Int64 // requests rejected during the current episode
}

// noteRejected logs "throttling started" on the first rejection of an episode
// (false→true edge); later rejections only bump the counter.
func (e *ipEntry) noteRejected(ip string) {
	if e.throttled.CompareAndSwap(false, true) {
		e.throttledAt.Store(time.Now().UnixNano())
		e.rejectedN.Store(1)
		debuglog.Warn("ratelimit-ip: throttling started",
			"ip", ip, "rps", e.rps, "burst", e.burst)
	} else {
		e.rejectedN.Add(1)
	}
}

// noteAllowed logs "throttling ended" when a throttled IP is served again with
// no delay (true→false edge).
func (e *ipEntry) noteAllowed(ip string) {
	if e.throttled.CompareAndSwap(true, false) {
		e.logThrottlingEnded(ip, time.Now(), "recovered")
	}
}

// logThrottlingEnded emits the episode summary (duration + rejected count).
func (e *ipEntry) logThrottlingEnded(ip string, end time.Time, reason string) {
	since := time.Unix(0, e.throttledAt.Load())
	debuglog.Info("ratelimit-ip: throttling ended",
		"ip", ip,
		"reason", reason,
		"duration", end.Sub(since).Round(time.Millisecond).String(),
		"rejected_requests", e.rejectedN.Load())
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

// Middleware returns an HTTP middleware that rate-limits requests per
// client IP. On limit violation the middleware responds with HTTP 429
// and sets Retry-After and X-RateLimit-* headers.
func (l *IPLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Runtime toggle from DB settings; default true for safety.
		if l.settings != nil {
			if !l.settings.GetBool(r.Context(), settingsKeyIPEnabled, true) {
				next.ServeHTTP(w, r)
				return
			}
		}

		ip := extractClientIP(r, l.trustedProxies)
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
			// IP was rate-limited). Use the IP's last activity as the end.
			if entry.throttled.Load() {
				entry.logThrottlingEnded(ip, entry.lastUsed, "idle")
			}
			delete(l.limiters, ip)
		}
	}
}

// extractClientIP determines the client IP from the request.
// When trustedProxies is non-empty and contains the RemoteAddr, the XFF chain
// is walked right-to-left, skipping IPs that belong to trusted proxy CIDRs.
// The rightmost non-trusted IP is the real client. This prevents spoofing
// by clients behind a trusted proxy. X-Real-IP is used as a fallback when
// XFF is absent.
func extractClientIP(r *http.Request, trustedProxies []*net.IPNet) string {
	if len(trustedProxies) > 0 {
		if config.IsTrustedProxy(r.RemoteAddr, trustedProxies) {
			if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
				if ip := rightmostUntrustedIP(xff, trustedProxies); ip != "" {
					return ip
				}
			}
			if xri := r.Header.Get("X-Real-IP"); xri != "" {
				candidate := strings.TrimSpace(xri)
				if net.ParseIP(candidate) != nil {
					return candidate
				}
			}
		}
	}
	// RemoteAddr includes port for TCP connections — strip it.
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}

// rightmostUntrustedIP parses the X-Forwarded-For header and returns the
// rightmost IP that is NOT in any trusted proxy CIDR. This correctly handles
// multi-hop proxy chains (e.g. CDN → load balancer → app) by walking the
// chain from the proxy-adjacent end toward the client.
func rightmostUntrustedIP(xff string, trustedProxies []*net.IPNet) string {
	parts := strings.Split(xff, ",")
	for i := len(parts) - 1; i >= 0; i-- {
		ip := strings.TrimSpace(parts[i])
		if ip == "" {
			continue
		}
		// Skip unparseable entries (e.g. "unknown" from older proxies)
		// so they don't become rate-limiter bucket keys.
		if net.ParseIP(ip) == nil {
			continue
		}
		if !isIPInTrustedNets(ip, trustedProxies) {
			return ip
		}
	}
	// All entries are trusted (unusual); fall back to the leftmost entry,
	// but only if it parses as a valid IP to avoid non-IP strings (e.g.
	// "unknown" from older proxies) becoming rate-limiter bucket keys.
	if len(parts) > 0 {
		candidate := strings.TrimSpace(parts[0])
		if net.ParseIP(candidate) != nil {
			return candidate
		}
	}
	return ""
}

// isIPInTrustedNets checks whether a bare IP address string belongs to any
// trusted proxy CIDR. Uses net.ParseIP directly to avoid the host:port
// format required by IsTrustedProxy, which would break IPv6 addresses
// that use :: zero-compression (e.g. "2001:db8::1" → "2001:db8::1:0").
func isIPInTrustedNets(ipStr string, trustedNets []*net.IPNet) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}
	for _, n := range trustedNets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}
