package webauthn

import (
	"net"
	"net/http"
	"strings"
	"unicode/utf8"
)

// metaUserAgentMax caps what a login request's User-Agent can put into the
// sessions table: header size limits are the server's, not the operator's,
// and a hostile client should not get to store kilobytes per login attempt.
const metaUserAgentMax = 256

// ClientIPSource resolves a request's client address with trusted-proxy
// awareness: forwarded headers are honored only when the peer is a configured
// trusted proxy. Satisfied by *ratelimit.IPLimiter, which owns that logic for
// the whole codebase — session metadata must not grow a second, weaker copy.
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
		UserAgent: truncateUTF8(r.UserAgent(), metaUserAgentMax),
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

// truncateUTF8 cuts s to at most max bytes without splitting a rune, dropping
// any invalid sequences the wire delivered. A byte-level cut could split a
// multi-byte rune and Postgres refuses invalid UTF-8, which would turn a
// valid login from a long-UA browser into a 500.
func truncateUTF8(s string, maxBytes int) string {
	s = strings.ToValidUTF8(s, "")
	if len(s) <= maxBytes {
		return s
	}
	cut := maxBytes
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}
