package proxy

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/hugalafutro/model-hotel/internal/ctxkeys"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// ipResolver is the interface for DNS resolution, allowing mocking in tests.
type ipResolver interface {
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
}

// SafeDialer wraps a net.Dialer with IP-range checking on every dial.
// Intended for use as http.Transport.DialContext to prevent DNS-rebinding
// attacks that redirect proxy requests to private or reserved IPs.
type SafeDialer struct {
	d            *net.Dialer
	hosts        map[string]bool
	resolver     ipResolver
	knownProxies []*net.IPNet
}

// NewSafeDialer creates a SafeDialer that blocks connections to private,
// loopback, link-local, and cloud-metadata IPs. Hosts in allowedHosts
// (lowercased for comparison) bypass all IP checks. IPs within knownProxies
// CIDRs bypass the private-IP restriction (for internal LLM servers).
func NewSafeDialer(allowedHosts []string, knownProxies []*net.IPNet) *SafeDialer {
	hosts := make(map[string]bool, len(allowedHosts))
	for _, h := range allowedHosts {
		hosts[strings.ToLower(h)] = true
	}
	return &SafeDialer{
		d:            &net.Dialer{Resolver: net.DefaultResolver},
		hosts:        hosts,
		resolver:     net.DefaultResolver,
		knownProxies: knownProxies,
	}
}

// newSafeDialerWithResolver creates a SafeDialer with a custom resolver for testing.
// This is exported for testing purposes only.
func newSafeDialerWithResolver(allowedHosts []string, resolver ipResolver, knownProxies []*net.IPNet) *SafeDialer {
	hosts := make(map[string]bool, len(allowedHosts))
	for _, h := range allowedHosts {
		hosts[strings.ToLower(h)] = true
	}
	return &SafeDialer{
		d:            &net.Dialer{Resolver: net.DefaultResolver},
		hosts:        hosts,
		resolver:     resolver,
		knownProxies: knownProxies,
	}
}

// isKnownProxy checks if the given IP belongs to any of the known proxy CIDRs.
func (s *SafeDialer) isKnownProxy(ip net.IP) bool {
	for _, n := range s.knownProxies {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// DialContext implements the dial function signature expected by
// http.Transport.DialContext. It resolves the target host, checks
// every resolved IP against blocked ranges, and refuses the
// connection if all IPs are private/reserved. To close the TOCTOU
// gap between DNS resolution and dial, it dials by IP (picking the
// first allowed address) so the connection target is the same IP that
// was checked. The original hostname is preserved via TLS ServerName
// and HTTP Host header by the transport layer.
func (s *SafeDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		// No port in addr — unusual but defensive.
		host = addr
		port = ""
	}

	// Allowlisted hosts skip IP checks but are still timed.
	if s.hosts[strings.ToLower(host)] {
		dialStart := time.Now()
		conn, err := s.d.DialContext(ctx, network, addr)
		if v := ctx.Value(ctxkeys.DialMsKey); v != nil {
			if p, ok := v.(*float64); ok {
				*p = float64(time.Since(dialStart).Microseconds()) / 1000.0
			}
		}
		return conn, err
	}

	// Resolve the host to IP addresses (timed).
	dnsStart := time.Now()
	ips, err := s.resolver.LookupIPAddr(ctx, host)
	// Write per-request dial timing if the caller provided a pointer.
	// This captures DNS resolution only when the dial fails before TCP;
	// successful dials overwrite this with the full DNS+TCP time below.
	if v := ctx.Value(ctxkeys.DialMsKey); v != nil {
		if p, ok := v.(*float64); ok {
			*p = float64(time.Since(dnsStart).Microseconds()) / 1000.0
		}
	}
	if err != nil {
		// Resolution failure: return the DNS error directly rather than
		// falling through to an unchecked dial. A host that can't be
		// resolved can't be safely dialed — the fallback dial would bypass
		// the IP blocklist, violating the invariant that every dial is
		// IP-checked. The caller still sees a connection error, just a
		// more specific one.
		debuglog.Warn("proxy: SafeDialer DNS resolution failed, rejecting dial", "host", host, "error", err)
		return nil, fmt.Errorf("safeDialer: DNS resolution failed for %s: %w", host, err)
	}

	debuglog.Debug("proxy: SafeDialer DNS resolved", "host", host, "ip_count", len(ips), "dns_ms", float64(time.Since(dnsStart).Microseconds())/1000.0)

	// If every resolved IP is blocked (and not in knownProxies), reject.
	blocked := true
	for _, ip := range ips {
		if !isBlockedIP(ip.IP) || s.isKnownProxy(ip.IP) {
			blocked = false
			break
		}
	}
	if blocked && len(ips) > 0 {
		return nil, fmt.Errorf("proxy: refused connection to private/reserved IP %s for host %s", ips[0].IP, host)
	}

	// Dial by the first allowed IP to close the TOCTOU gap: the IP we
	// checked is the one we connect to, preventing DNS rebinding between
	// resolution and dial.
	for _, ip := range ips {
		if isBlockedIP(ip.IP) && !s.isKnownProxy(ip.IP) {
			debuglog.Debug("proxy: SafeDialer blocked IP skipped", "host", host, "ip", ip.IP)
			continue
		}
		dialAddr := net.JoinHostPort(ip.IP.String(), port)
		conn, dialErr := s.d.DialContext(ctx, network, dialAddr)
		if dialErr != nil {
			debuglog.Warn("proxy: SafeDialer dial failed", "host", host, "ip", ip.IP, "error", dialErr)
			continue
		}
		debuglog.Debug("proxy: SafeDialer connected", "host", host, "ip", ip.IP, "total_ms", float64(time.Since(dnsStart).Microseconds())/1000.0)
		// Overwrite the timing with full DNS+TCP duration.
		if v := ctx.Value(ctxkeys.DialMsKey); v != nil {
			if p, ok := v.(*float64); ok {
				*p = float64(time.Since(dnsStart).Microseconds()) / 1000.0
			}
		}
		return conn, nil
	}

	// Should not be reachable (fell through without a non-blocked IP),
	// but handle gracefully.
	return nil, fmt.Errorf("proxy: no allowed IP found for host %s", host)
}

// CheckRedirect validates redirect targets against SafeDialer rules.
// It implements the http.Client.CheckRedirect callback signature.
func (s *SafeDialer) CheckRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return fmt.Errorf("proxy: stopped after 10 redirects")
	}
	host := req.URL.Hostname()
	// A redirect that leaves the original upstream host must not carry the
	// provider's credentials with it. Go's http.Client strips the standard
	// Authorization header across hosts, but forwards custom auth headers
	// (x-api-key, x-goog-api-key) verbatim, which would leak the key to the
	// redirect target. Strip all provider auth headers on any cross-host hop,
	// before the allowlist/IP checks below, so it applies regardless of whether
	// the target is allowed.
	if len(via) > 0 && !strings.EqualFold(host, via[0].URL.Hostname()) {
		util.StripProviderAuthHeaders(req)
	}
	// Allowlisted hosts bypass all checks.
	if s.hosts[strings.ToLower(host)] {
		return nil
	}
	// Resolve host and check IPs. Derive the timeout from the request
	// context so that cancelled requests don't leave DNS goroutines running.
	resolveCtx, cancel := context.WithTimeout(req.Context(), 5*time.Second)
	defer cancel()
	ips, err := s.resolver.LookupIPAddr(resolveCtx, host)
	if err != nil {
		// Resolution failure on a redirect: reject the redirect since we
		// cannot validate its target. The original response is still available
		// to the caller via the last successful request.
		return fmt.Errorf("proxy: redirect to host %s rejected: DNS resolution failed: %w", host, err)
	}
	hasAllowedIP := false
	for _, ip := range ips {
		if !isBlockedIP(ip.IP) || s.isKnownProxy(ip.IP) {
			hasAllowedIP = true
			break
		}
	}
	if !hasAllowedIP {
		return fmt.Errorf("proxy: redirect to host %s rejected: all resolved IPs are private/reserved", host)
	}
	return nil
}

// isBlockedIP checks whether an IP falls into a range that should never be
// dialled by the proxy: loopback, private, link-local, carrier-grade NAT, or
// cloud metadata. It delegates to util.IsBlockedIP so provider-URL validation
// (config.ValidateProviderURL) enforces the exact same ranges.
func isBlockedIP(ip net.IP) bool {
	return util.IsBlockedIP(ip)
}
