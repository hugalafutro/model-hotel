package webauthn

import (
	"net"
	"net/http"

	"github.com/hugalafutro/model-hotel/internal/util"
)

// metaUserAgentMax caps what a login request's User-Agent can put into the
// sessions table: header size limits are the server's, not the operator's,
// and a hostile client should not get to store kilobytes per login attempt.
const metaUserAgentMax = 256

// ClientIPSource resolves a request's client address with trusted-proxy
// awareness: forwarded headers are honored only when the peer is a configured
// trusted proxy. Satisfied by *ratelimit.IPLimiter, which delegates to
// internal/clientip — the single owner of that logic — so session metadata
// must not grow a second, weaker copy.
type ClientIPSource interface {
	ClientIP(r *http.Request) string
}

// MetaFromRequest extracts the device metadata a login request carries, for
// storage on the session it mints.
//
// The IP feeds the operator's active-sessions list — their "was this me?"
// signal when hunting a stolen session — so only an address the server can
// vouch for is stored. ips decides when a forwarded header is trustworthy;
// with a nil ips the peer address is used and forwarded headers are ignored
// outright, since an attacker-writable X-Forwarded-For that relabels the
// attacker's own row is exactly how a rogue session survives review. A value
// that does not parse as an IP is dropped rather than displayed.
func MetaFromRequest(r *http.Request, ips ClientIPSource) SessionMeta {
	return SessionMeta{
		UserAgent: util.TruncateBytes(r.UserAgent(), metaUserAgentMax),
		IP:        clientIP(r, ips),
	}
}

// clientIP resolves the address to store: the resolver's answer when one is
// wired, else the bare peer address, and "" for anything that is not an IP.
func clientIP(r *http.Request, ips ClientIPSource) string {
	var ip string
	if ips != nil {
		ip = ips.ClientIP(r)
	} else if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		ip = host
	} else {
		ip = r.RemoteAddr
	}
	if net.ParseIP(ip) == nil {
		return ""
	}
	return ip
}
