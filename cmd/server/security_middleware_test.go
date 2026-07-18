package main

import (
	"crypto/tls"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hugalafutro/model-hotel/internal/config"
)

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func TestSecurityHeadersMiddleware_Default(t *testing.T) {
	mw := securityHeadersMiddleware(&config.Config{})
	rec := httptest.NewRecorder()
	mw(okHandler()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", http.NoBody))

	if got := rec.Header().Get("X-Frame-Options"); got != "DENY" {
		t.Errorf("expected X-Frame-Options DENY, got %q", got)
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("expected nosniff, got %q", got)
	}
	if got := rec.Header().Get("Strict-Transport-Security"); got != "" {
		t.Errorf("expected no HSTS over plain HTTP, got %q", got)
	}
	if csp := rec.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "frame-ancestors 'none'") {
		t.Errorf("expected frame-ancestors 'none' in CSP, got %q", csp)
	}
}

func TestSecurityHeadersMiddleware_AllowEmbed(t *testing.T) {
	mw := securityHeadersMiddleware(&config.Config{AllowEmbed: true})
	rec := httptest.NewRecorder()
	mw(okHandler()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", http.NoBody))

	if got := rec.Header().Get("X-Frame-Options"); got != "" {
		t.Errorf("expected no X-Frame-Options with ALLOW_EMBED, got %q", got)
	}
	if csp := rec.Header().Get("Content-Security-Policy"); strings.Contains(csp, "frame-ancestors") {
		t.Errorf("expected no frame-ancestors in CSP with ALLOW_EMBED, got %q", csp)
	}
}

func TestSecurityHeadersMiddleware_HSTSOverTLS(t *testing.T) {
	mw := securityHeadersMiddleware(&config.Config{})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "https://example.test/", http.NoBody)
	req.TLS = &tls.ConnectionState{}
	mw(okHandler()).ServeHTTP(rec, req)

	if got := rec.Header().Get("Strict-Transport-Security"); got == "" {
		t.Error("expected HSTS header over TLS")
	}
}

func TestCORSMiddleware(t *testing.T) {
	cfg := &config.Config{CORSOrigins: []string{"http://allowed.test"}}
	mw := corsMiddleware(cfg)

	t.Run("no_origin_passes_through", func(t *testing.T) {
		rec := httptest.NewRecorder()
		mw(okHandler()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", http.NoBody))
		if rec.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rec.Code)
		}
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Errorf("expected no CORS headers without Origin, got %q", got)
		}
	})

	t.Run("allowed_origin", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
		req.Header.Set("Origin", "http://allowed.test")
		mw(okHandler()).ServeHTTP(rec, req)
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://allowed.test" {
			t.Errorf("expected origin echoed, got %q", got)
		}
		if got := rec.Header().Get("Vary"); got != "Origin" {
			t.Errorf("expected Vary: Origin, got %q", got)
		}
	})

	t.Run("disallowed_origin", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
		req.Header.Set("Origin", "http://evil.test")
		mw(okHandler()).ServeHTTP(rec, req)
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Errorf("expected no allow-origin for disallowed origin, got %q", got)
		}
		if got := rec.Header().Get("Vary"); got != "Origin" {
			t.Errorf("expected Vary: Origin even when disallowed, got %q", got)
		}
	})

	t.Run("preflight_short_circuits", func(t *testing.T) {
		called := false
		next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true })
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodOptions, "/", http.NoBody)
		req.Header.Set("Origin", "http://allowed.test")
		mw(next).ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Errorf("expected 204 for preflight, got %d", rec.Code)
		}
		if called {
			t.Error("expected preflight to short-circuit before the handler")
		}
	})
}

func TestMaxRequestSizeMiddleware(t *testing.T) {
	mw := maxRequestSizeMiddleware(8)
	var readErr error
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, readErr = io.ReadAll(r.Body)
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("this body is longer than eight bytes"))
	mw(next).ServeHTTP(rec, req)
	if readErr == nil {
		t.Error("expected read error for oversized body")
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/", strings.NewReader("tiny"))
	mw(next).ServeHTTP(rec, req)
	if readErr != nil {
		t.Errorf("expected small body to read fine, got %v", readErr)
	}
}
