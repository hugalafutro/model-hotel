package webauthn

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"
)

// fixedIPSource stands in for the rate limiter's trusted-proxy-aware
// resolver, which owns the decision of when a forwarded header is honored.
type fixedIPSource struct{ ip string }

func (s fixedIPSource) ClientIP(*http.Request) string { return s.ip }

// The stored IP is whatever the trusted-proxy-aware resolver says — that is
// the single place in this codebase that decides when X-Forwarded-For is
// honored, and this feature must not grow a second, weaker one.
func TestMetaFromRequest_UsesTheResolversAnswer(t *testing.T) {
	r := httptest.NewRequest("POST", "/login", http.NoBody)
	r.Header.Set("User-Agent", "Mozilla/5.0 Firefox/141.0")
	r.Header.Set("X-Forwarded-For", "10.0.0.9, 203.0.113.7")
	r.RemoteAddr = "172.18.0.2:41234"

	meta := MetaFromRequest(r, fixedIPSource{ip: "203.0.113.7"})
	if meta.IP != "203.0.113.7" {
		t.Errorf("IP = %q, want the resolver's answer", meta.IP)
	}
	if meta.UserAgent != "Mozilla/5.0 Firefox/141.0" {
		t.Errorf("UserAgent = %q", meta.UserAgent)
	}
}

// Without a resolver there is no trusted-proxy knowledge, so a forwarded
// header is attacker-writable and must be ignored: the peer address is the
// only address we can vouch for. This is what the operator's "was this me?"
// column rests on.
func TestMetaFromRequest_NoResolverNeverTrustsForwardedHeaders(t *testing.T) {
	r := httptest.NewRequest("POST", "/login", http.NoBody)
	r.Header.Set("X-Forwarded-For", "192.168.1.50")
	r.Header.Set("X-Real-IP", "192.168.1.50")
	r.RemoteAddr = "198.51.100.66:41234"

	if meta := MetaFromRequest(r, nil); meta.IP != "198.51.100.66" {
		t.Errorf("IP = %q, want the peer address, never the forged header", meta.IP)
	}
}

// The field renders in the operator's sessions list, so only a real address
// is stored: a resolver answer that does not parse as an IP (or a portless
// non-IP RemoteAddr like a unix socket) is dropped, not displayed.
func TestMetaFromRequest_NonIPValuesAreDropped(t *testing.T) {
	r := httptest.NewRequest("POST", "/login", http.NoBody)
	if meta := MetaFromRequest(r, fixedIPSource{ip: "192.168.1.50 · this browser"}); meta.IP != "" {
		t.Errorf("IP = %q, want empty for a non-IP resolver answer", meta.IP)
	}

	r.RemoteAddr = "@"
	if meta := MetaFromRequest(r, nil); meta.IP != "" {
		t.Errorf("IP = %q, want empty for a non-IP peer address", meta.IP)
	}
}

// A portless RemoteAddr that IS a real address is kept.
func TestMetaFromRequest_PortlessRemoteAddrKept(t *testing.T) {
	r := httptest.NewRequest("POST", "/login", http.NoBody)
	r.RemoteAddr = "192.0.2.10"

	if meta := MetaFromRequest(r, nil); meta.IP != "192.0.2.10" {
		t.Errorf("IP = %q, want 192.0.2.10", meta.IP)
	}
}

// Truncation must not split a multi-byte rune: Postgres refuses invalid UTF-8,
// which would turn a valid login from a long-UA browser into a 500.
func TestMetaFromRequest_TruncatesOnRuneBoundary(t *testing.T) {
	ua := strings.Repeat("a", 255) + "é" + strings.Repeat("b", 50)
	r := httptest.NewRequest("POST", "/login", http.NoBody)
	r.Header.Set("User-Agent", ua)
	r.RemoteAddr = "192.0.2.10:1"

	meta := MetaFromRequest(r, nil)
	if !utf8.ValidString(meta.UserAgent) {
		t.Fatalf("UserAgent is not valid UTF-8 after truncation: %q", meta.UserAgent)
	}
	if len(meta.UserAgent) > 256 {
		t.Errorf("UserAgent len = %d, want <= 256", len(meta.UserAgent))
	}
	if !strings.HasPrefix(meta.UserAgent, strings.Repeat("a", 255)) {
		t.Errorf("truncation lost the prefix: %q", meta.UserAgent)
	}
}

// Invalid bytes the wire delivered are dropped rather than stored.
func TestMetaFromRequest_DropsInvalidUTF8(t *testing.T) {
	r := httptest.NewRequest("POST", "/login", http.NoBody)
	r.Header.Set("User-Agent", "Mozilla\xc3\x28 Firefox")
	r.RemoteAddr = "192.0.2.10:1"

	meta := MetaFromRequest(r, nil)
	if !utf8.ValidString(meta.UserAgent) {
		t.Errorf("UserAgent still carries invalid UTF-8: %q", meta.UserAgent)
	}
}

// A hostile client can send arbitrarily large headers; what lands in the
// sessions table stays bounded.
func TestMetaFromRequest_BoundsHostileHeaders(t *testing.T) {
	r := httptest.NewRequest("POST", "/login", http.NoBody)
	r.Header.Set("User-Agent", strings.Repeat("A", 10_000))
	r.RemoteAddr = "192.0.2.10:55555"

	meta := MetaFromRequest(r, nil)
	if len(meta.UserAgent) > 256 {
		t.Errorf("UserAgent len = %d, want capped at 256", len(meta.UserAgent))
	}
}
