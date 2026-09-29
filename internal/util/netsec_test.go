package util

import (
	"net"
	"testing"
)

func TestIsBlockedIP(t *testing.T) {
	tests := []struct {
		name    string
		ip      string
		blocked bool
	}{
		{"nil", "", false},
		{"unspecified v4", "0.0.0.0", true},
		{"unspecified v6", "::", true},
		{"loopback v4", "127.0.0.1", true},
		{"loopback v6", "::1", true},
		{"private 10", "10.0.0.1", true},
		{"private 172.16", "172.16.0.1", true},
		{"private 192.168", "192.168.1.1", true},
		{"ipv6 ULA", "fd00::1", true},
		{"link-local v4", "169.254.1.1", true},
		{"link-local v6", "fe80::1", true},
		{"link-local multicast v4", "224.0.0.1", true},
		{"link-local multicast v6", "ff02::1", true},
		{"cloud metadata", "169.254.169.254", true},
		{"cgnat low", "100.64.0.1", true},
		{"cgnat high", "100.127.255.255", true},
		{"ipv4-mapped private", "::ffff:10.0.0.1", true},
		{"ipv4-mapped cgnat", "::ffff:100.64.0.1", true},
		{"this-host block", "0.1.2.3", true},
		{"nat64 metadata", "64:ff9b::a9fe:a9fe", true},
		{"nat64 private", "64:ff9b::a00:5", true},
		{"nat64 loopback", "64:ff9b::127.0.0.1", true},
		{"nat64 public", "64:ff9b::8.8.8.8", false},
		// The local-use prefix is refused whatever it embeds: its layout is the
		// operator's choice and cannot be read off the address.
		{"nat64 local-use metadata", "64:ff9b:1::a9fe:a9fe", true},
		{"nat64 local-use public /96", "64:ff9b:1::808:808", true},
		{"nat64 local-use public /48", "64:ff9b:1:808:8:800::", true},
		{"nat64 local-use hiding 0.1.2.3 under /96", "64:ff9b:1:808:808:800:1:203", true},
		{"public v4", "8.8.8.8", false},
		{"public v4 2", "93.184.216.34", false},
		{"just below cgnat", "100.63.255.255", false},
		{"just above cgnat", "100.128.0.1", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ip := net.ParseIP(tc.ip) // nil for the "nil" case
			if got := IsBlockedIP(ip); got != tc.blocked {
				t.Errorf("IsBlockedIP(%q) = %v, want %v", tc.ip, got, tc.blocked)
			}
		})
	}
}
