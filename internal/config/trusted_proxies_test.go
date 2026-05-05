package config

import (
	"net"
	"testing"
)

func TestIsTrustedProxy_EmptyNets(t *testing.T) {
	if IsTrustedProxy("10.0.0.1:1234", nil) {
		t.Error("expected false with nil trustedNets")
	}
	if IsTrustedProxy("10.0.0.1:1234", []*net.IPNet{}) {
		t.Error("expected false with empty trustedNets")
	}
}

func TestIsTrustedProxy_MatchesCIDR(t *testing.T) {
	_, cidr, _ := net.ParseCIDR("10.0.0.0/8")
	trusted := []*net.IPNet{cidr}

	if !IsTrustedProxy("10.0.0.1:1234", trusted) {
		t.Error("expected true for IP in 10.0.0.0/8")
	}
	if !IsTrustedProxy("10.255.255.255:80", trusted) {
		t.Error("expected true for boundary IP in 10.0.0.0/8")
	}
}

func TestIsTrustedProxy_DoesNotMatchCIDR(t *testing.T) {
	_, cidr, _ := net.ParseCIDR("10.0.0.0/8")
	trusted := []*net.IPNet{cidr}

	if IsTrustedProxy("192.168.1.1:54321", trusted) {
		t.Error("expected false for IP outside 10.0.0.0/8")
	}
}

func TestIsTrustedProxy_MultipleCIDRs(t *testing.T) {
	_, cidr1, _ := net.ParseCIDR("10.0.0.0/8")
	_, cidr2, _ := net.ParseCIDR("192.168.0.0/16")
	trusted := []*net.IPNet{cidr1, cidr2}

	if !IsTrustedProxy("192.168.1.1:8080", trusted) {
		t.Error("expected true for IP in 192.168.0.0/16")
	}
	if IsTrustedProxy("8.8.8.8:53", trusted) {
		t.Error("expected false for IP not in any trusted CIDR")
	}
}

func TestIsTrustedProxy_IPv6(t *testing.T) {
	_, cidr, _ := net.ParseCIDR("fd00::/8")
	trusted := []*net.IPNet{cidr}

	if !IsTrustedProxy("[fd00::1]:12345", trusted) {
		t.Error("expected true for IPv6 in fd00::/8")
	}
	if IsTrustedProxy("[::1]:12345", trusted) {
		t.Error("expected false for loopback outside fd00::/8")
	}
}

func TestIsTrustedProxy_RemoteAddrWithoutPort(t *testing.T) {
	_, cidr, _ := net.ParseCIDR("10.0.0.0/8")
	trusted := []*net.IPNet{cidr}

	if !IsTrustedProxy("10.0.0.1", trusted) {
		t.Error("expected true for bare IP (no port) in CIDR")
	}
}

func TestIsTrustedProxy_InvalidIP(t *testing.T) {
	_, cidr, _ := net.ParseCIDR("10.0.0.0/8")
	trusted := []*net.IPNet{cidr}

	if IsTrustedProxy("not-an-ip:1234", trusted) {
		t.Error("expected false for invalid IP")
	}
}
