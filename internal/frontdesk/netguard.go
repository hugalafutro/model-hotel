package frontdesk

import (
	"fmt"
	"net"
	"net/http"
	"syscall"
	"time"
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
		Timeout:   timeout,
		Transport: &http.Transport{DialContext: dialer.DialContext},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > 0 && req.URL.Host != via[0].URL.Host {
				return fmt.Errorf("frontdesk: refusing cross-host redirect to %s", req.URL.Host)
			}
			return nil
		},
	}
}

// isProbeBlockedIP reports addresses that are never a legitimate member and are
// the classic SSRF targets: the unspecified address, and link-local unicast
// (which includes the 169.254.169.254 cloud-metadata endpoint) and multicast.
// Private and loopback ranges are intentionally NOT blocked here.
func isProbeBlockedIP(ip net.IP) bool {
	return ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast()
}
