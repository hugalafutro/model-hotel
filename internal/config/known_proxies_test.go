package config

import (
	"net"
	"os"
	"testing"
)

func TestLoadKnownProxies_Empty(t *testing.T) {
	os.Unsetenv("KNOWN_PROXIES")
	result := LoadKnownProxies()
	if result != nil {
		t.Errorf("expected nil for empty env, got %v", result)
	}
}

func TestLoadKnownProxies_SingleCIDR(t *testing.T) {
	t.Setenv("KNOWN_PROXIES", "192.168.1.0/24")
	result := LoadKnownProxies()
	if len(result) != 1 {
		t.Fatalf("expected 1 CIDR, got %d", len(result))
	}
	_, expected, _ := net.ParseCIDR("192.168.1.0/24")
	if result[0].String() != expected.String() {
		t.Errorf("expected %s, got %s", expected, result[0])
	}
}

func TestLoadKnownProxies_MultipleCIDRs(t *testing.T) {
	t.Setenv("KNOWN_PROXIES", "10.0.0.0/8,192.168.1.0/24,172.16.0.0/12")
	result := LoadKnownProxies()
	if len(result) != 3 {
		t.Fatalf("expected 3 CIDRs, got %d", len(result))
	}
}

func TestLoadKnownProxies_SkipsInvalid(t *testing.T) {
	t.Setenv("KNOWN_PROXIES", "10.0.0.0/8,not-a-cidr,192.168.1.0/24")
	result := LoadKnownProxies()
	if len(result) != 2 {
		t.Fatalf("expected 2 valid CIDRs (invalid skipped), got %d", len(result))
	}
}

func TestLoadKnownProxies_WhitespaceTrimmed(t *testing.T) {
	t.Setenv("KNOWN_PROXIES", " 10.0.0.0/8 , 192.168.1.0/24 ")
	result := LoadKnownProxies()
	if len(result) != 2 {
		t.Fatalf("expected 2 CIDRs, got %d", len(result))
	}
}
