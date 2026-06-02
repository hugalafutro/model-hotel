package proxy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// Use testDB from proxy_test.go

func TestSafeDialer_BlockedIP(t *testing.T) {
	dialer := NewSafeDialer([]string{"allowed.example.com"}, nil)

	// Test blocking of loopback
	_, err := dialer.DialContext(context.Background(), "tcp", "127.0.0.1:80")
	if err == nil {
		t.Error("expected error for loopback IP")
	} else if !strings.Contains(err.Error(), "private/reserved IP") {
		t.Errorf("expected 'private/reserved IP' error, got %v", err)
	}

	// Test blocking of private IP range
	_, err = dialer.DialContext(context.Background(), "tcp", "192.168.1.1:80")
	if err == nil {
		t.Error("expected error for private IP")
	} else if !strings.Contains(err.Error(), "private/reserved IP") {
		t.Errorf("expected 'private/reserved IP' error, got %v", err)
	}

	// Test blocking of link-local
	_, err = dialer.DialContext(context.Background(), "tcp", "169.254.169.254:80")
	if err == nil {
		t.Error("expected error for link-local IP")
	} else if !strings.Contains(err.Error(), "private/reserved IP") {
		t.Errorf("expected 'private/reserved IP' error, got %v", err)
	}
}

// TestSafeDialer_AllowedHost tests that SafeDialer allows allowed hosts

func TestSafeDialer_AllowedHost(t *testing.T) {
	dialer := NewSafeDialer([]string{"localhost", "127.0.0.1"}, nil)

	// Test that allowed host bypasses IP checks
	// We can't actually dial without a server, but we can test that it doesn't
	// return an immediate error for blocked IPs when the host is allowed
	_, err := dialer.DialContext(context.Background(), "tcp", "localhost:9999")
	// This will fail to connect, but shouldn't fail with "private/reserved IP" error
	if err != nil && strings.Contains(err.Error(), "private/reserved IP") {
		t.Errorf("allowed host should not be blocked: %v", err)
	}
}

// TestRegisterAdminChat_Routes tests that RegisterAdminChat registers the expected routes

func TestRegisterAdminChat_Routes(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	// Create a chi router and register admin chat routes
	router := chi.NewRouter()
	h.RegisterAdminChat(router)

	// Test that routes are registered by checking if they don't return 404
	// We don't need to test the full functionality, just that routes exist
	t.Run("chat route exists", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/chat", http.NoBody)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		// Route should exist (may return auth error, but not 404)
		if w.Code == http.StatusNotFound {
			t.Error("admin chat route should be registered")
		}
	})

	t.Run("arena route exists", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/arena", http.NoBody)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code == http.StatusNotFound {
			t.Error("admin arena route should be registered")
		}
	})

	t.Run("completions route exists", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/completions", http.NoBody)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code == http.StatusNotFound {
			t.Error("admin completions route should be registered")
		}
	})
}
