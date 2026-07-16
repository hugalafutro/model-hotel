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

// maxRedirects caps redirect chains, matching net/http's own default. A hostile
// endpoint that answers every hop with a 3xx cannot pin the server in an
// unbounded fetch loop.
const maxRedirects = 10

// checkRedirect is the http.Client.CheckRedirect hook. It rejects a redirect
// whose target host is a literal blocked IP before the connection is attempted,
// and caps the chain length. This is defense in depth: dialControl already
// refuses a blocked address at dial time (covering hostnames that resolve to
// metadata), but rejecting literal-IP metadata targets here gives an earlier,
// clearer failure and keeps the guard from depending on the dial hook alone.
// Hostname targets are intentionally NOT resolved here: netguard resolves at
// dial time on purpose (avoiding a check-then-dial TOCTOU window), and
// dialControl remains the resolver-based guard for them.
func checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= maxRedirects {
		return fmt.Errorf("netguard: stopped after %d redirects", maxRedirects)
	}
	if ip := net.ParseIP(req.URL.Hostname()); ip != nil && BlockedIP(ip) {
		return fmt.Errorf("netguard: refusing redirect to blocked address %s", req.URL.Hostname())
	}
	return nil
}

// NewClient builds an http.Client whose dialer refuses blocked post-resolution
// IPs. The dial guard also covers redirect targets (each hop is dialled through
// the same Control hook), so a 3xx to a metadata address fails at connect time;
// checkRedirect adds an earlier, explicit rejection of literal-IP metadata
// redirects and bounds the redirect chain.
func NewClient(timeout time.Duration) *http.Client {
	dialer := &net.Dialer{
		Timeout:   timeout,
		KeepAlive: 30 * time.Second,
		Control:   dialControl,
	}
	return &http.Client{
		Timeout:       timeout,
		CheckRedirect: checkRedirect,
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
