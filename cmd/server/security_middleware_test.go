package main

import (
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

	// The restore upload sets its own, larger bound in its handler; the
	// general cap must not cut it first.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/backups/restore", strings.NewReader("a dump larger than eight bytes"))
	mw(next).ServeHTTP(rec, req)
	if readErr != nil {
		t.Errorf("the restore upload was capped by the general limit: %v", readErr)
	}

	// The exemption is the one route, not a suffix. chi runs a subrouter's
	// middleware on paths that match no route in it, so any unmatched path
	// ending in /backups/restore still reaches the body-buffering
	// streamingAwareTimeout middleware and must keep the general cap.
	// The encoded targets are the ones a Path-only test gets wrong: they decode
	// to the exempt path but chi routes on the escaped form, so they match no
	// route while the cap would already have come off.
	for _, path := range []string{
		"/v1/models/backups/restore",
		"/api/chat/x/backups/restore",
		"/api/backups/restore/",
		"/api/backups%2Frestore",
		"/%61pi/backups/restore",
	} {
		rec = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodPost, path, strings.NewReader("a body larger than eight bytes"))
		mw(next).ServeHTTP(rec, req)
		if readErr == nil {
			t.Errorf("%s kept no size cap: only POST /api/backups/restore is exempt", path)
		}
	}

	// The exemption is bound to the upload's method too, so a GET that happens
	// to name the restore path cannot shed the cap.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/backups/restore", strings.NewReader("a body larger than eight bytes"))
	mw(next).ServeHTTP(rec, req)
	if readErr == nil {
		t.Error("GET /api/backups/restore kept no size cap: only POST is exempt")
	}
}
