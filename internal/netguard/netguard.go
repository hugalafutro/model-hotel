// Package netguard provides SSRF defenses for outbound HTTP to
// admin-configured endpoints that legitimately live on the internal network:
// OIDC identity providers (e.g. an internal Authelia), the apprise-api
// notification container, and Front Desk members.
//
// Unlike the proxy SafeDialer (internal/proxy), which blocks every private
// range because upstream LLM providers are external SaaS, these guards allow
// private and loopback addresses and refuse only the addresses that are never a
// legitimate destination and are the classic SSRF targets: the unspecified
// address, link-local unicast (which includes the 169.254.169.254 cloud-metadata
// endpoint), and link-local multicast.
package netguard

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"syscall"
	"time"
)

// parseHTTPURL parses rawURL and requires an http/https scheme and a non-empty
// host, the shared structural check behind ValidateURL and ValidatePublicURL.
func parseHTTPURL(rawURL string) (*url.URL, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("URL scheme must be http or https, got %q", u.Scheme)
	}
	if u.Hostname() == "" {
		return nil, fmt.Errorf("URL has no host")
	}
	return u, nil
}

// BlockedIP reports whether an IP is one of the SSRF targets that must never be
// dialled by an internal-facing outbound client: the unspecified address, or
// link-local unicast/multicast (169.254.0.0/16 and fe80::/10, which cover the
// cloud-metadata endpoint). Private and loopback ranges are intentionally
// allowed so internal IdPs, the apprise-api container, and Front Desk members
// keep working.
func BlockedIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	return ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast()
}

// dialControl is the net.Dialer.Control hook that rejects a connection whose
// resolved address is a blocked IP. It runs after DNS resolution on the actual
// dial target, so it also catches DNS rebinding (a hostname that resolves to a
// metadata address between validation and dial) and, because the follow-up dial
// of a redirect passes through it too, redirect-to-metadata.
func dialControl(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	if ip := net.ParseIP(host); ip != nil && BlockedIP(ip) {
		return fmt.Errorf("netguard: refusing to connect to blocked address %s", host)
	}
	return nil
}

// NewClient builds an http.Client whose dialer refuses blocked post-resolution
// IPs. The dial guard also covers redirect targets (each hop is dialled through
// the same Control hook), so a 3xx to a metadata address fails at connect time.
func NewClient(timeout time.Duration) *http.Client {
	dialer := &net.Dialer{
		Timeout:   timeout,
		KeepAlive: 30 * time.Second,
		Control:   dialControl,
	}
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			// Honor HTTP(S)_PROXY like http.DefaultTransport so a deployment that
			// reaches an external IdP through an egress proxy keeps working; the
			// dial guard still runs on the proxy connection.
			Proxy:       http.ProxyFromEnvironment,
			DialContext: dialer.DialContext,
		},
	}
}

// ValidateURL parses rawURL for use as an admin-configured outbound endpoint and
// rejects it when the scheme is not http/https, the host is missing, or the host
// is a literal blocked IP (unspecified, link-local, or cloud-metadata). An empty
// string is accepted so operators can clear a setting. DNS is not resolved here
// (that would block the caller on a network lookup); NewClient's dial guard is
// the runtime defense against a hostname that resolves to a blocked IP.
func ValidateURL(rawURL string) error {
	if rawURL == "" {
		return nil
	}
	u, err := parseHTTPURL(rawURL)
	if err != nil {
		return err
	}
	if ip := net.ParseIP(u.Hostname()); ip != nil && BlockedIP(ip) {
		return fmt.Errorf("host %q is a blocked address (link-local/metadata)", u.Hostname())
	}
	return nil
}

// ValidatePublicURL parses rawURL for use as a public base URL (an external
// origin reflected into a redirect URI, never fetched by the server). It only
// enforces structural validity: an http/https scheme and a non-empty host. An
// empty string is accepted so operators can clear the setting.
func ValidatePublicURL(rawURL string) error {
	if rawURL == "" {
		return nil
	}
	_, err := parseHTTPURL(rawURL)
	return err
}
