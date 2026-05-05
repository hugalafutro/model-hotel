package proxy

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"strings"
)

// SafeDialer wraps a net.Dialer with IP-range checking on every dial.
// Intended for use as http.Transport.DialContext to prevent DNS-rebinding
// attacks that redirect proxy requests to private or reserved IPs.
type SafeDialer struct {
	d      *net.Dialer
	hosts  map[string]bool
}

// NewSafeDialer creates a SafeDialer that blocks connections to private,
// loopback, link-local, and cloud-metadata IPs. Hosts in allowedHosts
// (lowercased for comparison) bypass all IP checks.
func NewSafeDialer(allowedHosts []string) *SafeDialer {
	hosts := make(map[string]bool, len(allowedHosts))
	for _, h := range allowedHosts {
		hosts[strings.ToLower(h)] = true
	}
	return &SafeDialer{
		d:     &net.Dialer{Resolver: net.DefaultResolver},
		hosts: hosts,
	}
}

// DialContext implements the dial function signature expected by
// http.Transport.DialContext. It resolves the target host, checks
// every resolved IP against blocked ranges, and refuses the
// connection if all IPs are private/reserved.
func (s *SafeDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		// No port in addr — unusual but defensive.
		host = addr
	}

	// Allowlisted hosts skip all IP checks.
	if s.hosts[strings.ToLower(host)] {
		return s.d.DialContext(ctx, network, addr)
	}

	// Resolve the host to IP addresses.
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		// Resolution failure: let the underlying dial proceed so the
		// caller sees a normal connection error instead of a confusing
		// "private IP" error for a non-existent host.
		slog.Warn("proxy: SafeDialer DNS resolution failed, falling through to dial", "host", host, "error", err)
		return s.d.DialContext(ctx, network, addr)
	}

	// If every resolved IP is blocked, reject the connection.
	blocked := true
	for _, ip := range ips {
		if !isBlockedIP(ip.IP) {
			blocked = false
			break
		}
	}
	if blocked && len(ips) > 0 {
		return nil, fmt.Errorf("proxy: refused connection to private/reserved IP %s for host %s", ips[0].IP, host)
	}

	return s.d.DialContext(ctx, network, addr)
}

// isBlockedIP checks whether an IP falls into a range that should never be
// dialled by the proxy: loopback, private, link-local, or cloud metadata.
func isBlockedIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if ip.IsUnspecified() {
		return true
	}
	if ip.IsLoopback() {
		return true
	}
	if ip.IsPrivate() {
		return true
	}
	if ip.IsLinkLocalUnicast() {
		return true
	}
	// 169.254.169.254 is link-local unicast (caught above), but explicitly
	// check the string form for defence-in-depth.
	if ip.String() == "169.254.169.254" {
		return true
	}
	return false
}
