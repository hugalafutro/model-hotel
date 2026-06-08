package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewSPAHandler_ReturnsHandler(t *testing.T) {
	h := NewSPAHandler()
	if h == nil {
		t.Fatal("NewSPAHandler() returned nil")
	}
	if h.indexHTML == nil {
		t.Error("expected non-nil indexHTML")
	}
	// fileServer may be nil when running tests without a prior frontend build;
	// the handler still works by serving the fallback indexHTML.
	if h.fileServer != nil {
		t.Log("embedded static files are available (built frontend)")
	}
}

func TestNewSPAHandler_FallbackIndexHTML(t *testing.T) {
	h := NewSPAHandler()
	// When embedded files are not available (test mode), the fallback
	// indexHTML should contain a basic HTML page.
	if len(h.indexHTML) == 0 {
		t.Error("expected non-empty fallback indexHTML")
	}
	if !strings.Contains(string(h.indexHTML), "Model Hotel") {
		t.Errorf("fallback indexHTML should contain 'Model Hotel', got: %s", string(h.indexHTML)[:min(100, len(h.indexHTML))])
	}
}

func TestSPAHandler_ServeHTTP_APIPathReturns404(t *testing.T) {
	h := NewSPAHandler()
	for _, path := range []string{"/api/providers", "/api/models", "/v1/chat/completions", "/health"} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, http.NoBody)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			if w.Code != http.StatusNotFound {
				t.Errorf("expected 404 for %s, got %d", path, w.Code)
			}
		})
	}
}

func TestSPAHandler_ServeHTTP_RootReturnsIndexHTML(t *testing.T) {
	h := NewSPAHandler()
	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	ct := w.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/html") {
		t.Errorf("expected text/html content type, got %q", ct)
	}
	cc := w.Header().Get("Cache-Control")
	if cc != "no-cache" {
		t.Errorf("expected no-cache, got %q", cc)
	}
}

func TestSPAHandler_ServeHTTP_UnknownPathServesIndexHTML(t *testing.T) {
	h := NewSPAHandler()
	req := httptest.NewRequest(http.MethodGet, "/some/spa/route", http.NoBody)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for SPA fallback, got %d", w.Code)
	}
	ct := w.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/html") {
		t.Errorf("expected text/html content type for SPA fallback, got %q", ct)
	}
}

func TestSPAHandler_ServeHTTP_StaticFileServedWhenAvailable(t *testing.T) {
	h := NewSPAHandler()
	if h.fileServer == nil {
		t.Skip("skipping: embedded static files not available (need frontend build)")
	}
	// Request a JS file that should be served by the file server
	req := httptest.NewRequest(http.MethodGet, "/assets/test-abc123.js", http.NoBody)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	// If the file exists it should be served; if not the fileServer
	// handles 404 internally. Either way we shouldn't get indexHTML.
	if w.Header().Get("Content-Type") == "text/html; charset=utf-8" {
		t.Error("static file request should not return indexHTML")
	}
}
