package ratelimit

import (
	"net"
	"testing"
)

func mustParseCIDR(s string) *net.IPNet {
	_, ipNet, err := net.ParseCIDR(s)
	if err != nil {
		panic(err)
	}
	return ipNet
}

func TestRightmostUntrustedIP_SingleIP(t *testing.T) {
	result := rightmostUntrustedIP("1.2.3.4", nil)
	if result != "1.2.3.4" {
		t.Errorf("Expected '1.2.3.4', got %q", result)
	}
}

func TestRightmostUntrustedIP_AllTrusted(t *testing.T) {
	// All IPs are in the trusted network, falls back to leftmost
	trusted := []*net.IPNet{mustParseCIDR("10.0.0.0/8")}
	result := rightmostUntrustedIP("10.0.0.1, 10.0.0.2", trusted)
	if result != "10.0.0.1" {
		t.Errorf("Expected leftmost '10.0.0.1', got %q", result)
	}
}

func TestRightmostUntrustedIP_Mixed(t *testing.T) {
	// 10.0.0.1 is trusted, 1.2.3.4 is not
	trusted := []*net.IPNet{mustParseCIDR("10.0.0.0/8")}
	result := rightmostUntrustedIP("10.0.0.1, 1.2.3.4", trusted)
	if result != "1.2.3.4" {
		t.Errorf("Expected '1.2.3.4', got %q", result)
	}
}

func TestRightmostUntrustedIP_RightmostFirst(t *testing.T) {
	// Scans from right to left, returns first untrusted
	trusted := []*net.IPNet{mustParseCIDR("10.0.0.0/8")}
	result := rightmostUntrustedIP("1.2.3.4, 10.0.0.1, 5.6.7.8", trusted)
	if result != "5.6.7.8" {
		t.Errorf("Expected rightmost untrusted '5.6.7.8', got %q", result)
	}
}

func TestRightmostUntrustedIP_Empty(t *testing.T) {
	result := rightmostUntrustedIP("", nil)
	if result != "" {
		t.Errorf("Expected empty, got %q", result)
	}
}

func TestRightmostUntrustedIP_Unparseable(t *testing.T) {
	// "unknown" should be skipped
	result := rightmostUntrustedIP("1.2.3.4, unknown", nil)
	if result != "1.2.3.4" {
		t.Errorf("Expected '1.2.3.4' (skipping 'unknown'), got %q", result)
	}
}

func TestRightmostUntrustedIP_AllUnparseable(t *testing.T) {
	result := rightmostUntrustedIP("unknown, invalid", nil)
	if result != "" {
		t.Errorf("Expected empty for all unparseable, got %q", result)
	}
}

func TestRightmostUntrustedIP_IPv6(t *testing.T) {
	result := rightmostUntrustedIP("::1, 2001:db8::1", nil)
	if result != "2001:db8::1" {
		t.Errorf("Expected '2001:db8::1', got %q", result)
	}
}

func TestRightmostUntrustedIP_TrustedIPv6(t *testing.T) {
	trusted := []*net.IPNet{mustParseCIDR("::1/128")}
	result := rightmostUntrustedIP("::1, 2001:db8::1", trusted)
	if result != "2001:db8::1" {
		t.Errorf("Expected '2001:db8::1', got %q", result)
	}
}

func TestIsIPInTrustedNets_Match(t *testing.T) {
	trusted := []*net.IPNet{mustParseCIDR("10.0.0.0/8"), mustParseCIDR("172.16.0.0/12")}
	if !isIPInTrustedNets("10.1.2.3", trusted) {
		t.Error("10.1.2.3 should be in 10.0.0.0/8")
	}
	if !isIPInTrustedNets("172.20.0.1", trusted) {
		t.Error("172.20.0.1 should be in 172.16.0.0/12")
	}
}

func TestIsIPInTrustedNets_NoMatch(t *testing.T) {
	trusted := []*net.IPNet{mustParseCIDR("10.0.0.0/8")}
	if isIPInTrustedNets("1.2.3.4", trusted) {
		t.Error("1.2.3.4 should not be in 10.0.0.0/8")
	}
}

func TestIsIPInTrustedNets_InvalidIP(t *testing.T) {
	trusted := []*net.IPNet{mustParseCIDR("10.0.0.0/8")}
	if isIPInTrustedNets("not-an-ip", trusted) {
		t.Error("Invalid IP should not match")
	}
}

func TestIsIPInTrustedNets_EmptyNets(t *testing.T) {
	if isIPInTrustedNets("1.2.3.4", nil) {
		t.Error("No trusted nets should not match any IP")
	}
}
