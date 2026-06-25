package main

import "testing"

// TestNewRelyingPartyEnforcesHTTPS pins the HTTPS-only ingress guarantee: a
// plain-http PUBLIC_ORIGIN is refused so a misconfigured deploy fails loudly,
// while loopback http (a secure context for WebAuthn) stays allowed for local use.
func TestNewRelyingPartyEnforcesHTTPS(t *testing.T) {
	cases := []struct {
		origin string
		ok     bool
	}{
		{"https://frontdesk.example.com", true},
		{"https://frontdesk.example.com:8443", true},
		{"http://frontdesk.example.com", false}, // plain http is rejected
		{"http://localhost:8090", true},         // loopback http allowed
		{"http://127.0.0.1:8090", true},
		{"http://[::1]:8090", true},
		{"ftp://frontdesk.example.com", false},
		{"", false},
		{"https://", false}, // no host
	}
	for _, c := range cases {
		_, err := newRelyingParty(c.origin)
		if c.ok && err != nil {
			t.Errorf("newRelyingParty(%q) = %v, want success", c.origin, err)
		}
		if !c.ok && err == nil {
			t.Errorf("newRelyingParty(%q) = nil, want an error", c.origin)
		}
	}
}
