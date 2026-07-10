package frontdesk

import (
	"fmt"
	"net"
	"net/http"
	"syscall"
	"time"

	"github.com/hugalafutro/model-hotel/internal/netguard"
)

// newProbeClient builds the HTTP client the pollers use to reach members and
// Traefik. Member URLs are admin-supplied, so the client applies two defences
// against one being pointed somewhere it should not:
//
//   - a dial-time guard that refuses connections to link-local, unspecified, or
//     cloud-metadata addresses. It runs on the post-resolution IP (via the
//     dialer Control hook), so it also catches DNS rebinding, not just literal
//     IPs at member-creation time. Private and loopback ranges are deliberately
//     allowed: Front Desk members live on the internal network by design, unlike
//     the proxy SafeDialer (util.IsBlockedIP) which blocks them for outbound
//     provider calls.
//   - a redirect policy that refuses cross-host redirects, so a member endpoint
//     cannot bounce a probe (carrying the member's admin Bearer token) to a
//     different host.
func newProbeClient(timeout time.Duration) *http.Client {
	dialer := &net.Dialer{
		Timeout:   timeout,
		KeepAlive: 30 * time.Second,
		Control: func(_, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			if ip := net.ParseIP(host); ip != nil && isProbeBlockedIP(ip) {
				return fmt.Errorf("frontdesk: refusing to connect to blocked address %s", host)
			}
			return nil
		},
	}
	return &http.Client{
		Timeout:       timeout,
		Transport:     &http.Transport{DialContext: dialer.DialContext},
		CheckRedirect: checkProbeRedirect,
	}
}

// checkProbeRedirect is the redirect policy for the member probe client. It
// refuses two ways a redirect could leak the member's admin Bearer token:
//   - a cross-host redirect, which would replay the token to a different host;
//   - an https->http downgrade, which would replay the token over plaintext even
//     to the same host.
func checkProbeRedirect(req *http.Request, via []*http.Request) error {
	if len(via) == 0 {
		return nil
	}
	orig := via[0].URL
	if req.URL.Host != orig.Host {
		return fmt.Errorf("frontdesk: refusing cross-host redirect to %s", req.URL.Host)
	}
	if orig.Scheme == "https" && req.URL.Scheme != "https" {
		return fmt.Errorf("frontdesk: refusing https->%s redirect (token must not transit plaintext)", req.URL.Scheme)
	}
	return nil
}

// isProbeBlockedIP reports addresses that are never a legitimate member and are
// the classic SSRF targets: the unspecified address, and link-local unicast
// (which includes the 169.254.169.254 cloud-metadata endpoint) and multicast.
// Private and loopback ranges are intentionally NOT blocked here. It delegates
// to netguard.BlockedIP so Front Desk, OIDC, and alerting share one predicate.
func isProbeBlockedIP(ip net.IP) bool {
	return netguard.BlockedIP(ip)
}
