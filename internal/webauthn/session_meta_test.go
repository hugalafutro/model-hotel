package webauthn

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Behind Traefik the last X-Forwarded-For hop is the address the trusted proxy
// itself saw; earlier entries are client-supplied noise. RemoteAddr would be
// the proxy, which tells the operator nothing.
func TestMetaFromRequest_PrefersLastForwardedHop(t *testing.T) {
	r := httptest.NewRequest("POST", "/login", http.NoBody)
	r.Header.Set("User-Agent", "Mozilla/5.0 Firefox/141.0")
	r.Header.Set("X-Forwarded-For", "10.0.0.9, 203.0.113.7")
	r.RemoteAddr = "172.18.0.2:41234"

	meta := MetaFromRequest(r)
	if meta.IP != "203.0.113.7" {
		t.Errorf("IP = %q, want the last X-Forwarded-For hop", meta.IP)
	}
	if meta.UserAgent != "Mozilla/5.0 Firefox/141.0" {
		t.Errorf("UserAgent = %q", meta.UserAgent)
	}
}

// Without a proxy there is no X-Forwarded-For; the peer address minus the
// ephemeral port is the client.
func TestMetaFromRequest_FallsBackToRemoteAddr(t *testing.T) {
	r := httptest.NewRequest("POST", "/login", http.NoBody)
	r.Header.Del("X-Forwarded-For")
	r.RemoteAddr = "192.0.2.10:55555"

	if meta := MetaFromRequest(r); meta.IP != "192.0.2.10" {
		t.Errorf("IP = %q, want 192.0.2.10", meta.IP)
	}
}

// A RemoteAddr with no port (a unix socket, or a proxy that rewrites it) is
// stored as-is rather than dropped.
func TestMetaFromRequest_PortlessRemoteAddrKeptVerbatim(t *testing.T) {
	r := httptest.NewRequest("POST", "/login", http.NoBody)
	r.Header.Del("X-Forwarded-For")
	r.RemoteAddr = "192.0.2.10"

	if meta := MetaFromRequest(r); meta.IP != "192.0.2.10" {
		t.Errorf("IP = %q, want the raw RemoteAddr", meta.IP)
	}
}

// A hostile client can send arbitrarily large headers; what lands in the
// sessions table stays bounded.
func TestMetaFromRequest_BoundsHostileHeaders(t *testing.T) {
	r := httptest.NewRequest("POST", "/login", http.NoBody)
	r.Header.Set("User-Agent", strings.Repeat("A", 10_000))
	r.Header.Set("X-Forwarded-For", strings.Repeat("9", 10_000))
	r.RemoteAddr = "192.0.2.10:55555"

	meta := MetaFromRequest(r)
	if len(meta.UserAgent) > 256 {
		t.Errorf("UserAgent len = %d, want capped at 256", len(meta.UserAgent))
	}
	if len(meta.IP) > 64 {
		t.Errorf("IP len = %d, want capped at 64", len(meta.IP))
	}
}
