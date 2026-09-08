package frontdesk

import (
	"fmt"
	"net"
	"net/http"
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
//   - a redirect policy that refuses cross-host redirects, https->http
//     downgrades, a chain longer than netguard's 10-hop cap, and a hop whose
//     literal host is a blocked address, so a member endpoint cannot bounce a
//     probe (carrying the member's admin Bearer token) somewhere else.
func newProbeClient(timeout time.Duration) *http.Client {
	dialer := &net.Dialer{
		Timeout:   timeout,
		KeepAlive: 30 * time.Second,
		Control:   netguard.DialControl,
	}
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			DialContext: dialer.DialContext,
			// Matches http.DefaultTransport. At zero an idle connection is pooled
			// indefinitely, so a sync or backup hours later reuses a long-dead
			// connection and pays a wasted round trip to discover it. The member
			// listener closes an idle connection at 180s (httpx.IdleTimeout), so
			// the pool has to give up first.
			IdleConnTimeout: 90 * time.Second,
		},
		CheckRedirect: checkProbeRedirect,
	}
}

// checkProbeRedirect is the redirect policy for the member probe client. On top
// of netguard's shared chain cap and blocked-address check it refuses two ways a
// redirect could leak the member's admin Bearer token:
//   - a cross-host redirect, which would replay the token to a different host;
//   - an https->http downgrade, which would replay the token over plaintext even
//     to the same host.
func checkProbeRedirect(req *http.Request, via []*http.Request) error {
	if err := netguard.CheckRedirect(req, via); err != nil {
		return err
	}
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
