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

func TestLoadTrustedProxies_Empty(t *testing.T) {
	t.Setenv("TRUSTED_PROXIES", "")
	nets := LoadTrustedProxies()
	if nets != nil {
		t.Errorf("expected nil for empty TRUSTED_PROXIES, got %v", nets)
	}
}

func TestLoadTrustedProxies_ValidCIDRs(t *testing.T) {
	t.Setenv("TRUSTED_PROXIES", "10.0.0.0/8, 192.168.0.0/16")
	nets := LoadTrustedProxies()
	if len(nets) != 2 {
		t.Fatalf("expected 2 nets, got %d", len(nets))
	}
	if !nets[0].Contains(net.ParseIP("10.0.0.1")) {
		t.Error("first CIDR should contain 10.0.0.1")
	}
	if !nets[1].Contains(net.ParseIP("192.168.1.1")) {
		t.Error("second CIDR should contain 192.168.1.1")
	}
}

func TestLoadTrustedProxies_InvalidCIDR(t *testing.T) {
	t.Setenv("TRUSTED_PROXIES", "not-a-cidr, 10.0.0.0/8")
	nets := LoadTrustedProxies()
	if len(nets) != 1 {
		t.Fatalf("expected 1 valid net (invalid skipped), got %d", len(nets))
	}
	if !nets[0].Contains(net.ParseIP("10.0.0.1")) {
		t.Error("should contain 10.0.0.1")
	}
}

func TestKnownProviderHosts(t *testing.T) {
	hosts := KnownProviderHosts()
	if len(hosts) == 0 {
		t.Error("expected non-empty known provider hosts list")
	}
}

func TestIsTrustedProxy_InvalidIP(t *testing.T) {
	_, cidr, _ := net.ParseCIDR("10.0.0.0/8")
	trusted := []*net.IPNet{cidr}

	if IsTrustedProxy("not-an-ip:1234", trusted) {
		t.Error("expected false for invalid IP")
	}
}
