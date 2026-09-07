package clientip

import (
	"net"
	"testing"
)

func TestPeerIsTrusted_EmptyNets(t *testing.T) {
	if peerIsTrusted("10.0.0.1:1234", nil) {
		t.Error("expected false with nil trustedNets")
	}
	if peerIsTrusted("10.0.0.1:1234", []*net.IPNet{}) {
		t.Error("expected false with empty trustedNets")
	}
}

func TestPeerIsTrusted_MatchesCIDR(t *testing.T) {
	_, cidr, _ := net.ParseCIDR("10.0.0.0/8")
	trusted := []*net.IPNet{cidr}

	if !peerIsTrusted("10.0.0.1:1234", trusted) {
		t.Error("expected true for IP in 10.0.0.0/8")
	}
	if !peerIsTrusted("10.255.255.255:80", trusted) {
		t.Error("expected true for boundary IP in 10.0.0.0/8")
	}
}

func TestPeerIsTrusted_DoesNotMatchCIDR(t *testing.T) {
	_, cidr, _ := net.ParseCIDR("10.0.0.0/8")
	trusted := []*net.IPNet{cidr}

	if peerIsTrusted("192.168.1.1:54321", trusted) {
		t.Error("expected false for IP outside 10.0.0.0/8")
	}
}

func TestPeerIsTrusted_MultipleCIDRs(t *testing.T) {
	_, cidr1, _ := net.ParseCIDR("10.0.0.0/8")
	_, cidr2, _ := net.ParseCIDR("192.168.0.0/16")
	trusted := []*net.IPNet{cidr1, cidr2}

	if !peerIsTrusted("192.168.1.1:8080", trusted) {
		t.Error("expected true for IP in 192.168.0.0/16")
	}
	if peerIsTrusted("8.8.8.8:53", trusted) {
		t.Error("expected false for IP not in any trusted CIDR")
	}
}

func TestPeerIsTrusted_IPv6(t *testing.T) {
	_, cidr, _ := net.ParseCIDR("fd00::/8")
	trusted := []*net.IPNet{cidr}

	if !peerIsTrusted("[fd00::1]:12345", trusted) {
		t.Error("expected true for IPv6 in fd00::/8")
	}
	if peerIsTrusted("[::1]:12345", trusted) {
		t.Error("expected false for loopback outside fd00::/8")
	}
}

func TestPeerIsTrusted_RemoteAddrWithoutPort(t *testing.T) {
	_, cidr, _ := net.ParseCIDR("10.0.0.0/8")
	trusted := []*net.IPNet{cidr}

	if !peerIsTrusted("10.0.0.1", trusted) {
		t.Error("expected true for bare IP (no port) in CIDR")
	}
}

func TestPeerIsTrusted_InvalidIP(t *testing.T) {
	_, cidr, _ := net.ParseCIDR("10.0.0.0/8")
	trusted := []*net.IPNet{cidr}

	if peerIsTrusted("not-an-ip:1234", trusted) {
		t.Error("expected false for invalid IP")
	}
}
