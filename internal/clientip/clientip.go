// Package clientip resolves the client address of an HTTP request with
// trusted-proxy awareness: forwarded headers are honored only when the TCP
// peer is a configured trusted proxy (TRUSTED_PROXIES), so a direct client
// can never spoof its own address. It is the single owner of that logic —
// the rate limiter, the access log, auth warnings, the audit trail, and
// session metadata all resolve through here so every surface reports the
// same address for the same request.
package clientip

import (
	"context"
	"net"
	"net/http"
	"slices"
	"strings"

	"github.com/hugalafutro/model-hotel/internal/config"
)

// ctxKey carries the per-request resolved client IP set by Middleware.
type ctxKey struct{}

// Middleware resolves the client IP once and stores it on the request
// context for From. Mount it before anything that logs an address (the
// access logger included), so the whole chain sees the same resolution.
func Middleware(trustedProxies []*net.IPNet) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := context.WithValue(r.Context(), ctxKey{}, Resolve(r, trustedProxies))
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// From returns the client IP resolved by Middleware. Off the middleware
// path (tests, auxiliary servers) it falls back to the bare peer address
// with the port stripped — never a forwarded header, which without the
// trusted-proxy check is attacker-controlled.
func From(r *http.Request) string {
	if ip, ok := r.Context().Value(ctxKey{}).(string); ok {
		return ip
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

// Resolve determines the client IP from the request.
// When trustedProxies is non-empty and contains the RemoteAddr, the XFF chain
// is walked right-to-left, skipping IPs that belong to trusted proxy CIDRs.
// The rightmost non-trusted IP is the real client. This prevents spoofing
// by clients behind a trusted proxy. X-Real-IP is used as a fallback when
// XFF is absent.
func Resolve(r *http.Request, trustedProxies []*net.IPNet) string {
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
	for _, part := range slices.Backward(parts) {
		ip := strings.TrimSpace(part)
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
