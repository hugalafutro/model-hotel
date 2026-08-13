package webauthn

import (
	"net"
	"net/http"
	"strings"
)

// Caps on what a login request's headers can put into the sessions table:
// header size limits are the server's, not the operator's, and a hostile
// client should not get to store kilobytes per login attempt.
const (
	metaUserAgentMax = 256
	metaIPMax        = 64
)

// MetaFromRequest extracts the device metadata a login request carries, for
// storage on the session it mints. The IP prefers the last X-Forwarded-For
// hop — the address the trusted reverse proxy itself saw — over RemoteAddr,
// which behind a proxy is the proxy. Earlier hops are client-supplied and
// spoofable, but this metadata is display-only for the operator's
// active-sessions list, never an authorization input, so a forged header
// mislabels only the attacker's own row.
func MetaFromRequest(r *http.Request) SessionMeta {
	ua := r.UserAgent()
	if len(ua) > metaUserAgentMax {
		ua = ua[:metaUserAgentMax]
	}
	ip := clientIP(r)
	if len(ip) > metaIPMax {
		ip = ip[:metaIPMax]
	}
	return SessionMeta{UserAgent: ua, IP: ip}
}

// clientIP resolves the peer address of a request: the last X-Forwarded-For
// hop when a proxy appended one, else RemoteAddr minus the ephemeral port.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		hops := strings.Split(xff, ",")
		if ip := strings.TrimSpace(hops[len(hops)-1]); ip != "" {
			return ip
		}
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}
