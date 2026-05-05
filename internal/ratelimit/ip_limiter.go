package ratelimit

import (
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"github.com/hugalafutro/model-hotel/internal/util"
)

// default IP-based rate limit values
const (
	defaultIPRPS   = 30.0
	defaultIPBurst = 60
)

// ipEntry tracks a single IP address's rate limiter.
type ipEntry struct {
	limiter  *rate.Limiter
	lastUsed time.Time
}

// IPLimiter provides per-IP rate limiting as a DoS safety net.
// Unlike the per-key Limiter, this uses fixed limits from constructor
// arguments (no DB settings) so it always enforces a hard ceiling
// regardless of per-key configuration.
//
// It should be mounted BEFORE the auth middleware so it catches
// unauthenticated floods (brute-force key guessing, etc.).
type IPLimiter struct {
	mu       sync.Mutex
	limiters map[string]*ipEntry
	rps      float64
	burst    int
	stopCh   chan struct{}
}

// NewIPLimiter creates an IP rate limiter. If rps <= 0, the limiter
// is effectively unlimited (extremely high rate). A background goroutine
// cleans up entries idle for 10 minutes.
func NewIPLimiter(rps float64, burst int) *IPLimiter {
	if rps <= 0 {
		rps = defaultIPRPS
	}
	if burst <= 0 {
		burst = defaultIPBurst
	}
	l := &IPLimiter{
		limiters: make(map[string]*ipEntry),
		rps:      rps,
		burst:    burst,
		stopCh:   make(chan struct{}),
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
		ip := extractClientIP(r)
		entry := l.getLimiter(ip)

		reservation := entry.limiter.Reserve()
		if !reservation.OK() {
			l.writeHeaders(w, entry.limiter, 0)
			log.Printf("[ratelimit-ip] warning: rate limit exceeded for IP %s", ip)
			util.WriteOpenAIError(w, "rate limit exceeded", http.StatusTooManyRequests)
			return
		}

		delay := reservation.Delay()
		if delay > 0 {
			reservation.Cancel()
			l.writeHeaders(w, entry.limiter, delay)
			log.Printf("[ratelimit-ip] warning: rate limit exceeded for IP %s", ip)
			util.WriteOpenAIError(w, "rate limit exceeded", http.StatusTooManyRequests)
			return
		}

		l.writeHeaders(w, entry.limiter, 0)
		next.ServeHTTP(w, r)
	})
}

func (l *IPLimiter) getLimiter(ip string) *ipEntry {
	l.mu.Lock()
	defer l.mu.Unlock()

	entry, ok := l.limiters[ip]
	if !ok {
		entry = &ipEntry{
			limiter:  rate.NewLimiter(rate.Limit(l.rps), l.burst),
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
			delete(l.limiters, ip)
		}
	}
}

// extractClientIP determines the client IP from the request.
// Priority: X-Forwarded-For (first entry) > X-Real-IP > RemoteAddr.
// The port is stripped from RemoteAddr if present.
func extractClientIP(r *http.Request) string {
	// X-Forwarded-For may contain a comma-separated chain; the first
	// entry is the original client.
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if ip := strings.TrimSpace(strings.SplitN(xff, ",", 2)[0]); ip != "" {
			return ip
		}
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}
	// RemoteAddr includes port for TCP connections — strip it.
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}
