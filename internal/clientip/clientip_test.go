package clientip

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

// ---------------------------------------------------------------------------
// Resolve tests
// ---------------------------------------------------------------------------

func TestResolve_RemoteAddr(t *testing.T) {
	r := httptest.NewRequest("POST", "/", http.NoBody)
	r.RemoteAddr = "192.168.1.1:54321"
	ip := Resolve(r, nil)
	if ip != "192.168.1.1" {
		t.Errorf("expected 192.168.1.1, got %q", ip)
	}
}

func TestResolve_RemoteAddrNoPort(t *testing.T) {
	r := httptest.NewRequest("POST", "/", http.NoBody)
	r.RemoteAddr = "192.168.1.1"
	ip := Resolve(r, nil)
	if ip != "192.168.1.1" {
		t.Errorf("expected 192.168.1.1, got %q", ip)
	}
}

func TestResolve_XFFIgnoredWhenUntrusted(t *testing.T) {
	r := httptest.NewRequest("POST", "/", http.NoBody)
	r.RemoteAddr = "10.0.0.1:1234"
	r.Header.Set("X-Forwarded-For", "1.1.1.1, 2.2.2.2, 3.3.3.3")
	// nil trustedProxies means header is ignored
	ip := Resolve(r, nil)
	if ip != "10.0.0.1" {
		t.Errorf("expected RemoteAddr when no trusted proxies, got %q", ip)
	}
}

func TestResolve_XRealIPIgnoredWhenUntrusted(t *testing.T) {
	r := httptest.NewRequest("POST", "/", http.NoBody)
	r.RemoteAddr = "10.0.0.1:1234"
	r.Header.Set("X-Real-IP", "8.8.8.8")
	ip := Resolve(r, nil)
	if ip != "10.0.0.1" {
		t.Errorf("expected RemoteAddr when no trusted proxies, got %q", ip)
	}
}

func TestResolve_XFFHonoredWhenTrusted(t *testing.T) {
	_, cidr, _ := net.ParseCIDR("10.0.0.0/8")
	trusted := []*net.IPNet{cidr}

	r := httptest.NewRequest("POST", "/", http.NoBody)
	r.RemoteAddr = "10.0.0.1:1234"
	r.Header.Set("X-Forwarded-For", "1.1.1.1, 2.2.2.2")
	ip := Resolve(r, trusted)
	// Rightmost non-trusted IP is returned (2.2.2.2 is not in 10.0.0.0/8)
	if ip != "2.2.2.2" {
		t.Errorf("expected rightmost non-trusted XFF IP, got %q", ip)
	}
}

func TestResolve_XRealIPHonoredWhenTrusted(t *testing.T) {
	_, cidr, _ := net.ParseCIDR("10.0.0.0/8")
	trusted := []*net.IPNet{cidr}

	r := httptest.NewRequest("POST", "/", http.NoBody)
	r.RemoteAddr = "10.0.0.1:1234"
	r.Header.Set("X-Real-IP", "8.8.8.8")
	ip := Resolve(r, trusted)
	if ip != "8.8.8.8" {
		t.Errorf("expected X-Real-IP when trusted, got %q", ip)
	}
}

func TestResolve_XFFPriorityWhenTrusted(t *testing.T) {
	_, cidr, _ := net.ParseCIDR("10.0.0.0/8")
	trusted := []*net.IPNet{cidr}

	r := httptest.NewRequest("POST", "/", http.NoBody)
	r.RemoteAddr = "10.0.0.1:1234"
	r.Header.Set("X-Forwarded-For", "4.4.4.4")
	r.Header.Set("X-Real-IP", "5.5.5.5")
	ip := Resolve(r, trusted)
	if ip != "4.4.4.4" {
		t.Errorf("X-Forwarded-For should take priority when trusted, got %q", ip)
	}
}

func TestResolve_HeadersIgnoredWhenRemoteNotTrusted(t *testing.T) {
	_, cidr, _ := net.ParseCIDR("10.0.0.0/8")
	trusted := []*net.IPNet{cidr}

	r := httptest.NewRequest("POST", "/", http.NoBody)
	r.RemoteAddr = "192.168.1.1:1234" // not in trusted CIDR
	r.Header.Set("X-Forwarded-For", "1.1.1.1")
	r.Header.Set("X-Real-IP", "2.2.2.2")
	ip := Resolve(r, trusted)
	if ip != "192.168.1.1" {
		t.Errorf("expected RemoteAddr when remote not trusted, got %q", ip)
	}
}

func TestResolve_EmptyXFFWhenTrusted(t *testing.T) {
	_, cidr, _ := net.ParseCIDR("10.0.0.0/8")
	trusted := []*net.IPNet{cidr}

	r := httptest.NewRequest("POST", "/", http.NoBody)
	r.RemoteAddr = "10.0.0.1:1234"
	r.Header.Set("X-Forwarded-For", "")
	r.Header.Set("X-Real-IP", "9.9.9.9")
	ip := Resolve(r, trusted)
	if ip != "9.9.9.9" {
		t.Errorf("expected fallback to X-Real-IP when trusted, got %q", ip)
	}
}

func TestResolve_IPv6(t *testing.T) {
	r := httptest.NewRequest("POST", "/", http.NoBody)
	r.RemoteAddr = "[::1]:12345"
	ip := Resolve(r, nil)
	if ip != "::1" {
		t.Errorf("expected ::1 for IPv6, got %q", ip)
	}
}

func TestResolve_RightmostNonTrustedMultiHop(t *testing.T) {
	_, cidr, _ := net.ParseCIDR("10.0.0.0/8")
	trusted := []*net.IPNet{cidr}

	// Chain: client (1.1.1.1) → CDN (2.2.2.2) → LB (10.0.0.5) → app
	r := httptest.NewRequest("POST", "/", http.NoBody)
	r.RemoteAddr = "10.0.0.5:1234"
	r.Header.Set("X-Forwarded-For", "1.1.1.1, 2.2.2.2, 10.0.0.5")
	ip := Resolve(r, trusted)
	// 10.0.0.5 is trusted, 2.2.2.2 is not — should return 2.2.2.2
	if ip != "2.2.2.2" {
		t.Errorf("expected rightmost non-trusted 2.2.2.2, got %q", ip)
	}
}

func TestResolve_SpoofPrevention(t *testing.T) {
	_, cidr, _ := net.ParseCIDR("10.0.0.0/8")
	trusted := []*net.IPNet{cidr}

	// Attacker behind trusted proxy injects a fake leftmost IP
	r := httptest.NewRequest("POST", "/", http.NoBody)
	r.RemoteAddr = "10.0.0.1:1234"
	r.Header.Set("X-Forwarded-For", "spoofed-ip, 9.9.9.9, 10.0.0.1")
	ip := Resolve(r, trusted)
	// 10.0.0.1 is trusted, 9.9.9.9 is not — should return 9.9.9.9
	if ip != "9.9.9.9" {
		t.Errorf("expected 9.9.9.9 (non-trusted), not spoofed leftmost, got %q", ip)
	}
}

func TestResolve_AllTrustedFallsBackToLeftmost(t *testing.T) {
	_, cidr, _ := net.ParseCIDR("10.0.0.0/8")
	trusted := []*net.IPNet{cidr}

	// Unusual: all XFF entries are in trusted range
	r := httptest.NewRequest("POST", "/", http.NoBody)
	r.RemoteAddr = "10.0.0.1:1234"
	r.Header.Set("X-Forwarded-For", "10.0.0.2, 10.0.0.3")
	ip := Resolve(r, trusted)
	// Falls back to leftmost
	if ip != "10.0.0.2" {
		t.Errorf("expected leftmost fallback 10.0.0.2, got %q", ip)
	}
}

func TestResolve_IPv6XFFTrusted(t *testing.T) {
	_, cidr, _ := net.ParseCIDR("2001:db8::/32")
	trusted := []*net.IPNet{cidr}

	r := httptest.NewRequest("POST", "/", http.NoBody)
	r.RemoteAddr = "[2001:db8::1]:1234"
	r.Header.Set("X-Forwarded-For", "2001:db8:1::100, 2001:db8::1")
	ip := Resolve(r, trusted)
	// 2001:db8::1 is trusted, 2001:db8:1::100 is in trusted range too
	// All trusted → falls back to leftmost
	if ip != "2001:db8:1::100" {
		t.Errorf("expected leftmost fallback 2001:db8:1::100, got %q", ip)
	}
}

func TestResolve_IPv6XFFMixed(t *testing.T) {
	_, cidr, _ := net.ParseCIDR("2001:db8::/32")
	trusted := []*net.IPNet{cidr}

	r := httptest.NewRequest("POST", "/", http.NoBody)
	r.RemoteAddr = "[2001:db8::1]:1234"
	r.Header.Set("X-Forwarded-For", "fe80::1, 2001:db8::1")
	ip := Resolve(r, trusted)
	// 2001:db8::1 is trusted, fe80::1 is not → return fe80::1
	if ip != "fe80::1" {
		t.Errorf("expected rightmost non-trusted fe80::1, got %q", ip)
	}
}

func TestResolve_AllTrustedInvalidLeftmost(t *testing.T) {
	_, cidr, _ := net.ParseCIDR("10.0.0.0/8")
	trusted := []*net.IPNet{cidr}

	r := httptest.NewRequest("POST", "/", http.NoBody)
	r.RemoteAddr = "10.0.0.1:1234"
	r.Header.Set("X-Forwarded-For", "unknown, 10.0.0.2")
	ip := Resolve(r, trusted)
	// "unknown" is not a parseable IP, so it's skipped. 10.0.0.2 is trusted.
	// Fallback to leftmost ("unknown") also fails ParseIP. Falls through
	// to RemoteAddr.
	if ip != "10.0.0.1" {
		t.Errorf("expected fallback to RemoteAddr 10.0.0.1, got %q", ip)
	}
}

func TestResolve_XFFEmptySegments(t *testing.T) {
	_, cidr, _ := net.ParseCIDR("10.0.0.0/8")
	trusted := []*net.IPNet{cidr}

	r := httptest.NewRequest("POST", "/", http.NoBody)
	r.RemoteAddr = "10.0.0.1:1234"
	r.Header.Set("X-Forwarded-For", ", , 10.0.0.2, ")
	ip := Resolve(r, trusted)
	// All entries are empty or trusted → rightmostUntrustedIP returns ""
	// → Resolve falls through to RemoteAddr
	if ip != "10.0.0.1" {
		t.Errorf("expected fallback to RemoteAddr 10.0.0.1, got %q", ip)
	}
}

func TestResolve_XFFEmptySegmentsUntrustedClient(t *testing.T) {
	_, cidr, _ := net.ParseCIDR("10.0.0.0/8")
	trusted := []*net.IPNet{cidr}

	r := httptest.NewRequest("POST", "/", http.NoBody)
	r.RemoteAddr = "10.0.0.1:1234"
	r.Header.Set("X-Forwarded-For", ", 1.2.3.4, , 10.0.0.2")
	ip := Resolve(r, trusted)
	// Walk right-to-left: 10.0.0.2 trusted, empty skipped, 1.2.3.4 NOT trusted → return it
	if ip != "1.2.3.4" {
		t.Errorf("expected 1.2.3.4, got %q", ip)
	}
}

func TestResolve_XRealIPInvalid(t *testing.T) {
	_, cidr, _ := net.ParseCIDR("10.0.0.0/8")
	trusted := []*net.IPNet{cidr}

	r := httptest.NewRequest("POST", "/", http.NoBody)
	r.RemoteAddr = "10.0.0.1:1234"
	r.Header.Set("X-Real-IP", "not-an-ip")
	ip := Resolve(r, trusted)
	// Invalid X-Real-IP should fall through to RemoteAddr
	if ip != "10.0.0.1" {
		t.Errorf("expected fallback to RemoteAddr for invalid X-Real-IP, got %q", ip)
	}
}

// ---------------------------------------------------------------------------
// Middleware + From tests
// ---------------------------------------------------------------------------

func TestMiddlewareStoresResolvedIPForFrom(t *testing.T) {
	_, cidr, _ := net.ParseCIDR("10.0.0.0/8")
	trusted := []*net.IPNet{cidr}

	var got string
	handler := Middleware(trusted)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = From(r)
	}))

	r := httptest.NewRequest("POST", "/", http.NoBody)
	r.RemoteAddr = "10.0.0.1:1234"
	r.Header.Set("X-Forwarded-For", "1.2.3.4, 10.0.0.2")
	handler.ServeHTTP(httptest.NewRecorder(), r)

	if got != "1.2.3.4" {
		t.Errorf("expected From to return the resolved XFF client 1.2.3.4, got %q", got)
	}
}

func TestMiddlewareUntrustedPeerKeepsRemoteAddr(t *testing.T) {
	var got string
	handler := Middleware(nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = From(r)
	}))

	r := httptest.NewRequest("POST", "/", http.NoBody)
	r.RemoteAddr = "192.168.1.50:4321"
	r.Header.Set("X-Forwarded-For", "1.2.3.4")
	handler.ServeHTTP(httptest.NewRecorder(), r)

	if got != "192.168.1.50" {
		t.Errorf("expected the peer address for an untrusted peer, got %q", got)
	}
}

func TestFrom_FallbackStripsPort(t *testing.T) {
	r := httptest.NewRequest("POST", "/", http.NoBody)
	r.RemoteAddr = "192.168.1.1:54321"
	if ip := From(r); ip != "192.168.1.1" {
		t.Errorf("expected port-stripped RemoteAddr, got %q", ip)
	}
}

func TestFrom_FallbackIgnoresForwardedHeaders(t *testing.T) {
	// Without Middleware there is no trusted-proxy verdict, so forwarded
	// headers must never be honored — they are attacker-writable.
	r := httptest.NewRequest("POST", "/", http.NoBody)
	r.RemoteAddr = "192.168.1.1:54321"
	r.Header.Set("X-Forwarded-For", "1.2.3.4")
	r.Header.Set("X-Real-IP", "5.6.7.8")
	if ip := From(r); ip != "192.168.1.1" {
		t.Errorf("expected forwarded headers ignored without Middleware, got %q", ip)
	}
}

func TestFrom_FallbackNoPort(t *testing.T) {
	r := httptest.NewRequest("POST", "/", http.NoBody)
	r.RemoteAddr = "192.168.1.1"
	if ip := From(r); ip != "192.168.1.1" {
		t.Errorf("expected bare RemoteAddr passthrough, got %q", ip)
	}
}

// ---------------------------------------------------------------------------
// isIPInTrustedNets tests
// ---------------------------------------------------------------------------

func TestIsIPInTrustedNets_IPv4InCIDR(t *testing.T) {
	_, cidr, _ := net.ParseCIDR("10.0.0.0/8")
	nets := []*net.IPNet{cidr}

	if !isIPInTrustedNets("10.1.2.3", nets) {
		t.Error("10.1.2.3 should be in 10.0.0.0/8")
	}
	if !isIPInTrustedNets("10.255.255.255", nets) {
		t.Error("10.255.255.255 should be in 10.0.0.0/8")
	}
}

func TestIsIPInTrustedNets_IPv4NotInCIDR(t *testing.T) {
	_, cidr, _ := net.ParseCIDR("10.0.0.0/8")
	nets := []*net.IPNet{cidr}

	if isIPInTrustedNets("192.168.1.1", nets) {
		t.Error("192.168.1.1 should not be in 10.0.0.0/8")
	}
	if isIPInTrustedNets("11.0.0.1", nets) {
		t.Error("11.0.0.1 should not be in 10.0.0.0/8")
	}
}

func TestIsIPInTrustedNets_IPv6InCIDR(t *testing.T) {
	_, cidr, _ := net.ParseCIDR("2001:db8::/32")
	nets := []*net.IPNet{cidr}

	if !isIPInTrustedNets("2001:db8::1", nets) {
		t.Error("2001:db8::1 should be in 2001:db8::/32")
	}
	if !isIPInTrustedNets("2001:db8:abcd::1", nets) {
		t.Error("2001:db8:abcd::1 should be in 2001:db8::/32")
	}
}

func TestIsIPInTrustedNets_IPv6NotInCIDR(t *testing.T) {
	_, cidr, _ := net.ParseCIDR("2001:db8::/32")
	nets := []*net.IPNet{cidr}

	if isIPInTrustedNets("fe80::1", nets) {
		t.Error("fe80::1 should not be in 2001:db8::/32")
	}
	if isIPInTrustedNets("2001:db9::1", nets) {
		t.Error("2001:db9::1 should not be in 2001:db8::/32")
	}
}

func TestIsIPInTrustedNets_InvalidIP(t *testing.T) {
	_, cidr, _ := net.ParseCIDR("10.0.0.0/8")
	nets := []*net.IPNet{cidr}

	if isIPInTrustedNets("not-an-ip", nets) {
		t.Error("invalid IP should return false")
	}
	if isIPInTrustedNets("", nets) {
		t.Error("empty string should return false")
	}
	if isIPInTrustedNets("999.999.999.999", nets) {
		t.Error("out-of-range IP should return false")
	}
}

func TestIsIPInTrustedNets_EmptyNets(t *testing.T) {
	if isIPInTrustedNets("10.0.0.1", nil) {
		t.Error("no trusted nets should return false for any IP")
	}
	if isIPInTrustedNets("10.0.0.1", []*net.IPNet{}) {
		t.Error("empty nets slice should return false for any IP")
	}
}

func TestIsIPInTrustedNets_MultipleCIDRs(t *testing.T) {
	_, cidr1, _ := net.ParseCIDR("10.0.0.0/8")
	_, cidr2, _ := net.ParseCIDR("172.16.0.0/12")
	_, cidr3, _ := net.ParseCIDR("192.168.0.0/16")
	nets := []*net.IPNet{cidr1, cidr2, cidr3}

	if !isIPInTrustedNets("10.5.5.5", nets) {
		t.Error("10.5.5.5 should match first CIDR")
	}
	if !isIPInTrustedNets("172.20.0.1", nets) {
		t.Error("172.20.0.1 should match second CIDR")
	}
	if !isIPInTrustedNets("192.168.100.50", nets) {
		t.Error("192.168.100.50 should match third CIDR")
	}
	if isIPInTrustedNets("8.8.8.8", nets) {
		t.Error("8.8.8.8 should not match any CIDR")
	}
}

func TestIsIPInTrustedNets_Slash32CIDR(t *testing.T) {
	_, cidr, _ := net.ParseCIDR("1.2.3.4/32")
	nets := []*net.IPNet{cidr}

	if !isIPInTrustedNets("1.2.3.4", nets) {
		t.Error("1.2.3.4 should match /32")
	}
	if isIPInTrustedNets("1.2.3.5", nets) {
		t.Error("1.2.3.5 should not match /32")
	}
}

func TestIsIPInTrustedNets_IPv6ZeroCompression(t *testing.T) {
	_, cidr, _ := net.ParseCIDR("2001:db8::/32")
	nets := []*net.IPNet{cidr}

	// :: zero-compression should work correctly (the reason isIPInTrustedNets exists
	// instead of relying on IsTrustedProxy which requires host:port format)
	if !isIPInTrustedNets("2001:db8::1", nets) {
		t.Error("2001:db8::1 with zero-compression should be in 2001:db8::/32")
	}
}

func TestIsIPInTrustedNets_IPv4MappedIPv6(t *testing.T) {
	_, cidr, _ := net.ParseCIDR("10.0.0.0/8")
	nets := []*net.IPNet{cidr}

	// ::ffff:10.0.0.1 is an IPv4-mapped IPv6 address; net.ParseIP will parse it
	// but it becomes a 16-byte IPv4-mapped form which differs from the 4-byte form
	// used by the CIDR. net.IPNet.Contains handles this correctly.
	if !isIPInTrustedNets("::ffff:10.0.0.1", nets) {
		t.Error("::ffff:10.0.0.1 (IPv4-mapped IPv6) should match 10.0.0.0/8")
	}
}
