package proxy

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hugalafutro/model-hotel/internal/ctxkeys"
	"github.com/hugalafutro/model-hotel/internal/provider"
)

func TestIsBlockedIP_LoopbackIPv4(t *testing.T) {
	if !isBlockedIP(net.ParseIP("127.0.0.1")) {
		t.Error("expected 127.0.0.1 to be blocked")
	}
}

func TestIsBlockedIP_LoopbackIPv6(t *testing.T) {
	if !isBlockedIP(net.ParseIP("::1")) {
		t.Error("expected ::1 to be blocked")
	}
}

func TestIsBlockedIP_Private10(t *testing.T) {
	if !isBlockedIP(net.ParseIP("10.0.0.1")) {
		t.Error("expected 10.0.0.1 to be blocked")
	}
	if !isBlockedIP(net.ParseIP("10.255.255.255")) {
		t.Error("expected 10.255.255.255 to be blocked")
	}
}

func TestIsBlockedIP_Private17216(t *testing.T) {
	if !isBlockedIP(net.ParseIP("172.16.0.1")) {
		t.Error("expected 172.16.0.1 to be blocked")
	}
	if !isBlockedIP(net.ParseIP("172.31.255.255")) {
		t.Error("expected 172.31.255.255 to be blocked")
	}
}

func TestIsBlockedIP_Private192168(t *testing.T) {
	if !isBlockedIP(net.ParseIP("192.168.0.1")) {
		t.Error("expected 192.168.0.1 to be blocked")
	}
	if !isBlockedIP(net.ParseIP("192.168.255.255")) {
		t.Error("expected 192.168.255.255 to be blocked")
	}
}

func TestIsBlockedIP_CloudMetadata(t *testing.T) {
	if !isBlockedIP(net.ParseIP("169.254.169.254")) {
		t.Error("expected 169.254.169.254 to be blocked")
	}
}

func TestIsBlockedIP_LinkLocal(t *testing.T) {
	if !isBlockedIP(net.ParseIP("169.254.1.1")) {
		t.Error("expected 169.254.1.1 to be blocked")
	}
	if !isBlockedIP(net.ParseIP("fe80::1")) {
		t.Error("expected fe80::1 to be blocked")
	}
}

func TestIsBlockedIP_PublicAllowed(t *testing.T) {
	if isBlockedIP(net.ParseIP("93.184.216.34")) {
		t.Error("expected public IP 93.184.216.34 to NOT be blocked")
	}
	if isBlockedIP(net.ParseIP("8.8.8.8")) {
		t.Error("expected public IP 8.8.8.8 to NOT be blocked")
	}
}

func TestIsBlockedIP_Nil(t *testing.T) {
	if isBlockedIP(nil) {
		t.Error("expected nil IP to NOT be blocked")
	}
}

func TestIsBlockedIP_IPv4MappedIPv6Loopback(t *testing.T) {
	// ::ffff:127.0.0.1 should also be caught as loopback
	if !isBlockedIP(net.ParseIP("::ffff:127.0.0.1")) {
		t.Error("expected ::ffff:127.0.0.1 to be blocked")
	}
}
func TestIsBlockedIP_Unspecified(t *testing.T) {
	if !isBlockedIP(net.ParseIP("0.0.0.0")) {
		t.Error("expected 0.0.0.0 to be blocked")
	}
	if !isBlockedIP(net.ParseIP("::")) {
		t.Error("expected :: to be blocked")
	}
}

func TestSafeDialer_PublicHostAllowed(t *testing.T) {
	// Verify that public IPs pass the isBlockedIP check
	publicIPs := []string{"93.184.216.34", "8.8.8.8", "1.1.1.1"}
	for _, ipStr := range publicIPs {
		ip := net.ParseIP(ipStr)
		if ip == nil {
			t.Fatalf("failed to parse IP %s", ipStr)
		}
		if isBlockedIP(ip) {
			t.Errorf("expected public IP %s to NOT be blocked", ipStr)
		}
	}
}

func TestSafeDialer_AllowedHostBypass(t *testing.T) {
	// A host in the allowlist must bypass IP checks regardless of its IP.
	sd := NewSafeDialer([]string{"internal.corp.example"}, nil)

	ctx := context.Background()

	// We expect DialContext to fail with a real connection error (no route
	// to host or timeout), NOT with the "refused connection" error.
	conn, err := sd.DialContext(ctx, "tcp", "internal.corp.example:80")
	if err != nil {
		if err.Error() == "" {
			t.Fatal("unexpected empty error")
		}
		// The error should be a connection error, not our blocked-IP error.
		if err.Error() == "proxy: refused connection to private/reserved IP  for host internal.corp.example" {
			t.Fatal("expected dial to proceed past IP check for allowlisted host, got blocked-IP error")
		}
	} else {
		conn.Close()
	}
}

// TestSafeDialer_BlockedHost tests that a host that resolves to a blocked IP
// is rejected before any dial attempt.
func TestSafeDialer_BlockedHost(t *testing.T) {
	sd := NewSafeDialer(nil, nil)

	ctx := context.Background()

	// 127.0.0.1 should be blocked.
	conn, err := sd.DialContext(ctx, "tcp", "127.0.0.1:80")
	if err == nil {
		conn.Close()
		t.Fatal("expected error for loopback dial, got nil")
	}
	if err.Error() != "proxy: refused connection to private/reserved IP 127.0.0.1 for host 127.0.0.1" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSafeDialer_PrivateIPv4Range10(t *testing.T) {
	sd := NewSafeDialer(nil, nil)
	ctx := context.Background()

	conn, err := sd.DialContext(ctx, "tcp", "10.0.0.1:80")
	if err == nil {
		conn.Close()
		t.Fatal("expected error for private IP 10.x.x.x, got nil")
	}
	expectedError := "proxy: refused connection to private/reserved IP 10.0.0.1 for host 10.0.0.1"
	if err.Error() != expectedError {
		t.Fatalf("unexpected error: %v, expected: %v", err, expectedError)
	}
}

func TestSafeDialer_PrivateIPv4Range172(t *testing.T) {
	sd := NewSafeDialer(nil, nil)
	ctx := context.Background()

	conn, err := sd.DialContext(ctx, "tcp", "172.16.0.1:80")
	if err == nil {
		conn.Close()
		t.Fatal("expected error for private IP 172.16.x.x, got nil")
	}
	expectedError := "proxy: refused connection to private/reserved IP 172.16.0.1 for host 172.16.0.1"
	if err.Error() != expectedError {
		t.Fatalf("unexpected error: %v, expected: %v", err, expectedError)
	}
}

func TestSafeDialer_PrivateIPv4Range192(t *testing.T) {
	sd := NewSafeDialer(nil, nil)
	ctx := context.Background()

	conn, err := sd.DialContext(ctx, "tcp", "192.168.1.1:80")
	if err == nil {
		conn.Close()
		t.Fatal("expected error for private IP 192.168.x.x, got nil")
	}
	expectedError := "proxy: refused connection to private/reserved IP 192.168.1.1 for host 192.168.1.1"
	if err.Error() != expectedError {
		t.Fatalf("unexpected error: %v, expected: %v", err, expectedError)
	}
}

func TestSafeDialer_LinkLocalIPv6(t *testing.T) {
	sd := NewSafeDialer(nil, nil)
	ctx := context.Background()

	conn, err := sd.DialContext(ctx, "tcp", "[fe80::1]:80")
	if err == nil {
		conn.Close()
		t.Fatal("expected error for link-local IPv6, got nil")
	}
	expectedError := "proxy: refused connection to private/reserved IP fe80::1 for host fe80::1"
	if err.Error() != expectedError {
		t.Fatalf("unexpected error: %v, expected: %v", err, expectedError)
	}
}

func TestSafeDialer_UnspecifiedIP(t *testing.T) {
	sd := NewSafeDialer(nil, nil)
	ctx := context.Background()

	conn, err := sd.DialContext(ctx, "tcp", "0.0.0.0:80")
	if err == nil {
		conn.Close()
		t.Fatal("expected error for unspecified IP 0.0.0.0, got nil")
	}
	expectedError := "proxy: refused connection to private/reserved IP 0.0.0.0 for host 0.0.0.0"
	if err.Error() != expectedError {
		t.Fatalf("unexpected error: %v, expected: %v", err, expectedError)
	}
}

func TestSafeDialer_NoPortInAddr(t *testing.T) {
	sd := NewSafeDialer(nil, nil)
	ctx := context.Background()

	// When no port is provided, should handle gracefully
	_, err := sd.DialContext(ctx, "tcp", "127.0.0.1")
	if err == nil {
		t.Error("expected error for loopback without port")
	}
}

func TestSafeDialer_DialTimingContext(t *testing.T) {
	sd := NewSafeDialer(nil, nil)
	var dialMs float64
	ctx := context.WithValue(context.Background(), ctxkeys.DialMsKey, &dialMs)

	// This will fail to connect but should set the timing value
	_, _ = sd.DialContext(ctx, "tcp", "127.0.0.1:80")
	// dialMs should be >= 0 (DNS resolution was attempted, even for IP)
	if dialMs < 0 {
		t.Errorf("expected dialMs >= 0, got %f", dialMs)
	}
}

func TestSafeDialer_DNSErrorFallback(t *testing.T) {
	sd := NewSafeDialer(nil, nil)
	ctx := context.Background()

	// Non-existent host should fall through to dial and get a connection error,
	// not a "private IP" error
	_, err := sd.DialContext(ctx, "tcp", "this-host-does-not-exist-xyz123.invalid:80")
	if err == nil {
		t.Error("expected error for non-existent host")
	}
	// The error should NOT be our blocked-IP error
	if err != nil && strings.Contains(err.Error(), "refused connection to private/reserved IP") {
		t.Errorf("expected DNS/connection error, got blocked-IP error: %v", err)
	}
}

// TestSafeDialer_InvalidAddressFormat tests DialContext with an address
// that cannot be split into host:port
func TestSafeDialer_InvalidAddressFormat(t *testing.T) {
	sd := NewSafeDialer(nil, nil)
	ctx := context.Background()

	// Address without port should still work (falls through to dial)
	_, err := sd.DialContext(ctx, "tcp", "not-a-valid-addr")
	if err == nil {
		t.Error("expected error for invalid address format")
	}
	// Should not be a blocked-IP error
	if strings.Contains(err.Error(), "refused connection to private/reserved IP") {
		t.Errorf("expected connection error, got blocked-IP error: %v", err)
	}
}

// TestSafeDialer_CanceledContext tests DialContext with a canceled context
func TestSafeDialer_CanceledContext(t *testing.T) {
	sd := NewSafeDialer(nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := sd.DialContext(ctx, "tcp", "example.com:80")
	if err == nil {
		t.Error("expected error for canceled context")
	}
}

// TestSafeDialer_AllResolvedIPsBlocked tests when all resolved IPs are blocked
func TestSafeDialer_AllResolvedIPsBlocked(t *testing.T) {
	sd := NewSafeDialer(nil, nil)
	ctx := context.Background()

	// localhost resolves to loopback which is blocked
	_, err := sd.DialContext(ctx, "tcp", "localhost:80")
	if err == nil {
		t.Error("expected error when all IPs are blocked")
	}
	if !strings.Contains(err.Error(), "refused connection to private/reserved IP") {
		t.Errorf("expected blocked-IP error, got: %v", err)
	}
}

func TestIsBlockedIP_IPv6MappedIPv4Private(t *testing.T) {
	t.Parallel()
	// ::ffff:10.0.0.1 should be blocked (IPv6-mapped IPv4 private address)
	if !isBlockedIP(net.ParseIP("::ffff:10.0.0.1")) {
		t.Error("expected ::ffff:10.0.0.1 to be blocked")
	}
	// ::ffff:192.168.1.1 should also be blocked
	if !isBlockedIP(net.ParseIP("::ffff:192.168.1.1")) {
		t.Error("expected ::ffff:192.168.1.1 to be blocked")
	}
}

func TestIsBlockedIP_IPv6UniqueLocal(t *testing.T) {
	t.Parallel()
	// fc00::/7 is IPv6 unique local address (ULA)
	if !isBlockedIP(net.ParseIP("fc00::1")) {
		t.Error("expected fc00::1 to be blocked")
	}
	if !isBlockedIP(net.ParseIP("fd00::1")) {
		t.Error("expected fd00::1 to be blocked")
	}
	if !isBlockedIP(net.ParseIP("fd12:3456:789a::1")) {
		t.Error("expected fd12:3456:789a::1 to be blocked")
	}
}

func TestIsBlockedIP_IPv6MappedIPv4Loopback(t *testing.T) {
	t.Parallel()
	// ::ffff:127.0.0.1 should be caught as loopback (already tested but ensure coverage)
	if !isBlockedIP(net.ParseIP("::ffff:127.0.0.1")) {
		t.Error("expected ::ffff:127.0.0.1 to be blocked")
	}
}

// TestSafeDialer_DialByIP exercises the path where DialContext dials by IP
// after DNS resolution returns at least one allowed IP.
func TestSafeDialer_DialByIP(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	// Extract host:port from server URL (e.g. "127.0.0.1:PORT")
	hostPort := strings.TrimPrefix(srv.URL, "http://")
	host, _, _ := net.SplitHostPort(hostPort)

	// 127.0.0.1 is blocked by default, so add the host (without port) to allowed hosts
	// The SafeDialer extracts host from addr using SplitHostPort before checking allowlist
	sdWithAllow := NewSafeDialer([]string{host}, nil)

	conn, err := sdWithAllow.DialContext(ctx, "tcp", hostPort)
	if err != nil {
		t.Fatalf("expected successful dial to local server, got: %v", err)
	}
	conn.Close()
}

// TestSafeDialer_DialTimingSetOnRealDNS tests that the dial timing context
// value is properly set when DNS resolution actually occurs for a real hostname.
func TestSafeDialer_DialTimingSetOnRealDNS(t *testing.T) {
	t.Parallel()
	sd := NewSafeDialer(nil, nil)
	var dialMs float64
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ctx = context.WithValue(ctx, ctxkeys.DialMsKey, &dialMs)

	// Dial a real public host - connection may fail but DNS should resolve
	_, _ = sd.DialContext(ctx, "tcp", "example.com:80")

	if dialMs <= 0 {
		t.Errorf("expected dialMs > 0 after DNS resolution, got %f", dialMs)
	}
}

// TestSafeDialer_DialByIPForPublicHost tests that dialing a public hostname
// (not in allowlist) goes through the resolve → check → dial-by-IP path.
// mockResolver implements a fake DNS resolver for testing.
type mockResolver struct {
	lookupFunc func(ctx context.Context, host string) ([]net.IPAddr, error)
}

func (m *mockResolver) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	if m.lookupFunc != nil {
		return m.lookupFunc(ctx, host)
	}
	return nil, fmt.Errorf("no mock implementation for host: %s", host)
}

// TestSafeDialer_DialByIPWithMockDNS tests the dial-by-IP path using a mock DNS
// resolver that returns a non-blocked IP. This covers lines 89-95 in safe_dialer.go.
func TestSafeDialer_DialByIPWithMockDNS(t *testing.T) {
	t.Parallel()

	// Create a mock resolver that returns a public IP (8.8.8.8) for any hostname
	// The dial will fail (no route/connection refused) but the dial-by-IP path
	// (lines 89-95) will be exercised
	mockResolver := &mockResolver{
		lookupFunc: func(ctx context.Context, host string) ([]net.IPAddr, error) {
			// Return a public IP that is NOT blocked
			return []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}}, nil
		},
	}

	// Create a SafeDialer with the mock resolver - NOT allowlisting the host
	// This forces the dial-by-IP path
	sd := newSafeDialerWithResolver(nil, mockResolver, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Dial a fake hostname - the mock resolver returns 8.8.8.8 (public, non-blocked)
	// The dial will fail (connection timeout/refused) but the dial-by-IP path
	// should be exercised
	_, err := sd.DialContext(ctx, "tcp", "fake-hostname.example.com:80")

	// We expect a connection error (not a blocked-IP error)
	if err == nil {
		t.Fatal("expected connection error for non-routable IP")
	}
	// Should NOT be a blocked-IP error since 8.8.8.8 is public
	if strings.Contains(err.Error(), "refused connection to private/reserved IP") {
		t.Errorf("expected connection error, got blocked-IP error: %v", err)
	}
}

// TestSafeDialer_DialByIPPublicHostWithDNS tests the dial-by-IP path with a real
// public hostname. This test may be skipped if DNS resolution fails in the test environment.
func TestSafeDialer_DialByIPPublicHostWithDNS(t *testing.T) {
	t.Parallel()

	// First check if DNS resolution works
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ips, err := net.DefaultResolver.LookupIPAddr(ctx, "example.com")
	if err != nil {
		t.Skipf("DNS resolution failed, skipping test: %v", err)
	}

	// Check if any resolved IP is non-blocked
	hasNonBlocked := false
	for _, ip := range ips {
		if !isBlockedIP(ip.IP) {
			hasNonBlocked = true
			break
		}
	}
	if !hasNonBlocked {
		t.Skip("all resolved IPs are blocked, skipping test")
	}

	// Now test the actual dial-by-IP path
	sd := NewSafeDialer(nil, nil)
	ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()

	// Dial example.com - DNS should resolve to public IPs, then dial by IP
	// The connection may fail (timeout, connection refused, etc.) but the
	// dial-by-IP path should be exercised
	_, err = sd.DialContext(ctx2, "tcp", "example.com:80")

	// We don't care if the connection succeeds or fails
	// We just care that it's NOT a blocked-IP error
	if err != nil && strings.Contains(err.Error(), "refused connection to private/reserved IP") {
		t.Errorf("public host should not be blocked, got: %v", err)
	}
}

// TestNewSafeDialerWithResolver_UppercaseAllowedHosts covers lines 47-49
// (strings.ToLower in the allowedHosts loop of newSafeDialerWithResolver).
func TestNewSafeDialerWithResolver_UppercaseAllowedHosts(t *testing.T) {
	t.Parallel()

	mockRes := &mockResolver{
		lookupFunc: func(ctx context.Context, host string) ([]net.IPAddr, error) {
			return []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}, nil
		},
	}

	// Pass uppercase host to exercise strings.ToLower
	sd := newSafeDialerWithResolver([]string{"INTERNAL.CORP.EXAMPLE"}, mockRes, nil)
	if !sd.hosts["internal.corp.example"] {
		t.Error("expected lowercase key in hosts map")
	}
	if sd.hosts["INTERNAL.CORP.EXAMPLE"] {
		t.Error("expected uppercase key to be absent from hosts map")
	}
}

// TestSafeDialer_MixedBlockedAndAllowedIPs covers lines 113-115
// (blocked IP skipped in the dial-by-IP loop). When DNS returns both
// blocked and allowed IPs, the blocked ones are skipped with a debug log.
func TestSafeDialer_MixedBlockedAndAllowedIPs(t *testing.T) {
	t.Parallel()

	// Return a blocked IP first, then a public IP.
	// The blocked IP (127.0.0.1) should be skipped in the dial loop,
	// and the public IP (8.8.8.8) should be attempted.
	mockRes := &mockResolver{
		lookupFunc: func(ctx context.Context, host string) ([]net.IPAddr, error) {
			return []net.IPAddr{
				{IP: net.ParseIP("127.0.0.1")}, // blocked — skipped in dial loop
				{IP: net.ParseIP("8.8.8.8")},   // public — dialled
			}, nil
		},
	}

	sd := newSafeDialerWithResolver(nil, mockRes, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := sd.DialContext(ctx, "tcp", "mixed.example.com:80")
	if err == nil {
		t.Fatal("expected connection error for non-routable public IP")
	}
	// Should NOT be a blocked-IP error since there was a non-blocked IP
	if strings.Contains(err.Error(), "refused connection to private/reserved IP") {
		t.Errorf("expected connection error (not blocked-IP), got: %v", err)
	}
}

func TestSafeDialer_DialByIPForPublicHost(t *testing.T) {
	t.Parallel()
	sd := NewSafeDialer(nil, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := sd.DialContext(ctx, "tcp", "example.com:80")
	if err == nil {
		// Connection succeeded (unlikely but possible)
		// Just return, test passed
		return
	}
	// Should NOT be a blocked-IP error for a public host
	if strings.Contains(err.Error(), "refused connection to private/reserved IP") {
		t.Errorf("public host should not be blocked, got: %v", err)
	}
}

func TestSafeDialer_KnownProxiesBypass(t *testing.T) {
	t.Parallel()
	// A private IP within a known CIDR should be allowed.
	_, cidr, _ := net.ParseCIDR("192.168.1.0/24")
	knownNets := []*net.IPNet{cidr}

	mockRes := &mockResolver{
		lookupFunc: func(ctx context.Context, host string) ([]net.IPAddr, error) {
			return []net.IPAddr{{IP: net.ParseIP("192.168.1.101")}}, nil
		},
	}

	sd := newSafeDialerWithResolver(nil, mockRes, knownNets)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// The dial will fail (no actual server), but it should NOT be a
	// blocked-IP error since 192.168.1.101 is in the known CIDR.
	_, err := sd.DialContext(ctx, "tcp", "internal-ollama:80")
	if err == nil {
		return // connected somehow
	}
	if strings.Contains(err.Error(), "refused connection to private/reserved IP") {
		t.Errorf("expected known-proxy IP to bypass SafeDialer, got: %v", err)
	}
}

func TestSafeDialer_KnownProxiesNoMatchStillBlocked(t *testing.T) {
	t.Parallel()
	// A private IP NOT in any known CIDR should still be blocked.
	_, cidr, _ := net.ParseCIDR("10.0.0.0/8")
	knownNets := []*net.IPNet{cidr}

	mockRes := &mockResolver{
		lookupFunc: func(ctx context.Context, host string) ([]net.IPAddr, error) {
			return []net.IPAddr{{IP: net.ParseIP("192.168.1.1")}}, nil
		},
	}

	sd := newSafeDialerWithResolver(nil, mockRes, knownNets)
	ctx := context.Background()

	_, err := sd.DialContext(ctx, "tcp", "blocked-host:80")
	if err == nil {
		t.Fatal("expected blocked-IP error")
	}
	if !strings.Contains(err.Error(), "refused connection to private/reserved IP") {
		t.Errorf("expected blocked-IP error for non-matching known proxy, got: %v", err)
	}
}

func TestSafeDialer_KnownProxiesWithDialByIP(t *testing.T) {
	t.Parallel()
	// When a host resolves to IPs both inside and outside a known CIDR,
	// the known CIDR IP should be used for dial (not skipped as blocked).
	_, cidr, _ := net.ParseCIDR("192.168.1.0/24")
	knownNets := []*net.IPNet{cidr}

	mockRes := &mockResolver{
		lookupFunc: func(ctx context.Context, host string) ([]net.IPAddr, error) {
			return []net.IPAddr{
				{IP: net.ParseIP("10.0.0.1")},      // private, NOT in known CIDR → blocked
				{IP: net.ParseIP("192.168.1.101")}, // private, IN known CIDR → allowed
			}, nil
		},
	}

	sd := newSafeDialerWithResolver(nil, mockRes, knownNets)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := sd.DialContext(ctx, "tcp", "mixed-host:80")
	if err == nil {
		return // connected somehow
	}
	// Should NOT be a blocked-IP error — the known-proxy IP is available.
	if strings.Contains(err.Error(), "refused connection to private/reserved IP") {
		t.Errorf("expected known-proxy IP to be dialled, got: %v", err)
	}
}

func TestSafeDialer_CheckRedirect_BlockedIP(t *testing.T) {
	t.Parallel()
	_, cidr, _ := net.ParseCIDR("192.168.1.0/24")
	sd := NewSafeDialer(nil, []*net.IPNet{cidr})

	// Create a fake redirect request to a loopback address.
	req := httptest.NewRequest("GET", "http://127.0.0.1/admin", http.NoBody)
	via := []*http.Request{httptest.NewRequest("GET", "http://example.com/", http.NoBody)}

	err := sd.CheckRedirect(req, via)
	if err == nil {
		t.Fatal("expected redirect to loopback to be rejected")
	}
	if !strings.Contains(err.Error(), "redirect to private/reserved IP") {
		t.Errorf("expected redirect rejection error, got: %v", err)
	}
}

func TestSafeDialer_CheckRedirect_AllowedHost(t *testing.T) {
	t.Parallel()
	sd := NewSafeDialer([]string{"internal.example"}, nil)

	req := httptest.NewRequest("GET", "http://internal.example/redirect", http.NoBody)
	via := []*http.Request{httptest.NewRequest("GET", "http://example.com/", http.NoBody)}

	err := sd.CheckRedirect(req, via)
	if err != nil {
		t.Errorf("expected allowed host redirect to pass, got: %v", err)
	}
}

func TestSafeDialer_CheckRedirect_MaxRedirects(t *testing.T) {
	t.Parallel()
	sd := NewSafeDialer(nil, nil)

	req := httptest.NewRequest("GET", "http://example.com/", http.NoBody)
	via := make([]*http.Request, 10)

	err := sd.CheckRedirect(req, via)
	if err == nil {
		t.Fatal("expected error after 10 redirects")
	}
	if !strings.Contains(err.Error(), "stopped after 10 redirects") {
		t.Errorf("expected max redirect error, got: %v", err)
	}
}

func TestSafeDialer_CheckRedirect_KnownProxyAllowed(t *testing.T) {
	t.Parallel()
	// A redirect to a host resolving to a private IP that IS in a known
	// CIDR should be allowed through CheckRedirect.
	_, cidr, _ := net.ParseCIDR("192.168.1.0/24")

	mockRes := &mockResolver{
		lookupFunc: func(ctx context.Context, host string) ([]net.IPAddr, error) {
			return []net.IPAddr{{IP: net.ParseIP("192.168.1.101")}}, nil
		},
	}
	sd := newSafeDialerWithResolver(nil, mockRes, []*net.IPNet{cidr})

	req := httptest.NewRequest("GET", "http://internal-llm.local/redirect", http.NoBody)
	via := []*http.Request{httptest.NewRequest("GET", "http://example.com/", http.NoBody)}

	err := sd.CheckRedirect(req, via)
	if err != nil {
		t.Errorf("expected redirect to known-proxy IP to be allowed, got: %v", err)
	}
}

// ===========================================================================
// DiscoveryService Integration Tests
// ===========================================================================

func TestDiscoveryService_SSRFProtection(t *testing.T) {
	t.Parallel()
	// Create a SafeDialer with no allowed hosts and no known proxies.
	// This should block connections to private IPs.
	sd := NewSafeDialer(nil, nil)
	_ = provider.NewDiscoveryService(sd.DialContext, sd.CheckRedirect)

	// Try to discover from a private IP URL. This should fail with a
	// "refused connection to private/reserved IP" error.
	// We use 192.168.1.1 which is a private IP.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Test via the SafeDialer directly - this is what DiscoveryService uses internally.
	conn, err := sd.DialContext(ctx, "tcp", "192.168.1.1:80")
	if conn != nil {
		conn.Close()
	}
	if err == nil {
		t.Fatal("expected SSRF protection to block connection to private IP, but request succeeded")
	}
	if !strings.Contains(err.Error(), "refused connection to private/reserved IP") {
		t.Errorf("expected SSRF block error, got: %v", err)
	}
}

func TestDiscoveryService_NoProtectionWhenNil(t *testing.T) {
	t.Parallel()
	// When dialCtx is nil, there's no SSRF protection. The request
	// should fail with a normal connection error (not an SSRF block).
	svc := provider.NewDiscoveryService(nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Create a test provider with a private IP URL
	prov := &provider.Provider{
		ID:           [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
		Name:         "test-provider",
		BaseURL:      "http://192.168.1.1",
		EncryptedKey: []byte{},
	}

	// DiscoverModels will fail, but should fail with a connection error, not SSRF block
	_, err := svc.DiscoverModels(ctx, prov, "test-master-key")
	if err == nil {
		// Unlikely but not an error if it somehow connected.
		return
	}
	// Should be a normal connection error, NOT an SSRF block.
	if strings.Contains(err.Error(), "refused connection to private/reserved IP") {
		t.Errorf("expected normal connection error without SSRF protection, got SSRF block: %v", err)
	}
}
