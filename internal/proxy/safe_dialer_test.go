package proxy

import (
	"net"
	"testing"
	"time"
)

func TestSafeDialFunc_NilDialer(t *testing.T) {
	fn := safeDialFunc(nil)
	if fn != nil {
		t.Error("Expected nil function for nil SafeDialer")
	}
}

func TestSafeDialFunc_WithDialer(t *testing.T) {
	sd := &SafeDialer{
		d:        &net.Dialer{Timeout: 5 * time.Second},
		hosts:    map[string]bool{"localhost": true},
		resolver: net.DefaultResolver,
	}
	fn := safeDialFunc(sd)
	if fn == nil {
		t.Fatal("Expected non-nil function for SafeDialer")
	}
	// Just verify the function is non-nil and callable
}
