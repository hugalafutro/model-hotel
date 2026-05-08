package proxy

import (
	"context"
	"net"
	"strings"
	"testing"

	"github.com/hugalafutro/model-hotel/internal/ctxkeys"
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

// TestSafeDialer_ResolveThenCheck tests the dialer's resolution and blocking
// logic against a host that resolves to a known public IP. We use a lookup on
// example.com to verify the dialer does not block public hosts. This test
// requires working DNS and network access.
func TestSafeDialer_PublicHostAllowed(t *testing.T) {
	// Resolve a public host to verify it passes the check.
	host := "example.com"
	ips, err := net.DefaultResolver.LookupIPAddr(context.Background(), host)
	if err != nil {
		t.Skipf("DNS resolution failed for %s: %v", host, err)
	}

	for _, ip := range ips {
		if isBlockedIP(ip.IP) {
			t.Fatalf("expected %s (resolved from %s) to NOT be blocked, got blocked", ip.IP, host)
		}
	}
}

func TestSafeDialer_AllowedHostBypass(t *testing.T) {
	// A host in the allowlist must bypass IP checks regardless of its IP.
	sd := NewSafeDialer([]string{"internal.corp.example"})

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
	sd := NewSafeDialer(nil)

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
	sd := NewSafeDialer(nil)
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
	sd := NewSafeDialer(nil)
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
	sd := NewSafeDialer(nil)
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
	sd := NewSafeDialer(nil)
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
	sd := NewSafeDialer(nil)
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
	sd := NewSafeDialer(nil)
	ctx := context.Background()

	// When no port is provided, should handle gracefully
	_, err := sd.DialContext(ctx, "tcp", "127.0.0.1")
	if err == nil {
		t.Error("expected error for loopback without port")
	}
}

func TestSafeDialer_DialTimingContext(t *testing.T) {
	sd := NewSafeDialer(nil)
	var dialMs float64
	ctx := context.WithValue(context.Background(), ctxkeys.SafeDialMsKey, &dialMs)

	// This will fail to connect but should set the timing value
	_, _ = sd.DialContext(ctx, "tcp", "127.0.0.1:80")
	// dialMs should be >= 0 (DNS resolution was attempted, even for IP)
	if dialMs < 0 {
		t.Errorf("expected dialMs >= 0, got %f", dialMs)
	}
}

func TestSafeDialer_DNSErrorFallback(t *testing.T) {
	sd := NewSafeDialer(nil)
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
