package proxy

import (
	"net"
	"testing"
)

func TestSafeDialFunc_NilDialer(t *testing.T) {
	fn := safeDialFunc(nil)
	if fn != nil {
		t.Error("Expected nil function for nil SafeDialer")
	}
}

func TestSafeDialFunc_WithDialer(t *testing.T) {
	sd := newSafeDialerWithResolver(
		[]string{"localhost", "API.OPENAI.COM"},
		net.DefaultResolver,
		nil,
	)
	fn := safeDialFunc(sd)
	if fn == nil {
		t.Fatal("Expected non-nil function for SafeDialer")
	}
	// Verify host normalization
	if !sd.hosts["localhost"] {
		t.Error("Expected localhost in hosts")
	}
	if !sd.hosts["api.openai.com"] {
		t.Error("Expected api.openai.com (lowercased) in hosts")
	}
	if sd.hosts["API.OPENAI.COM"] {
		t.Error("Expected original case not in hosts")
	}
}
