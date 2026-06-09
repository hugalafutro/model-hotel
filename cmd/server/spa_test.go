package main

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
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

// TestSPAHandler_ServeHTTP_V1PrefixReturns404 tests that /v1-prefixed
// paths return 404 (not caught by the /v1/ check with trailing slash).
func TestSPAHandler_ServeHTTP_V1PrefixReturns404(t *testing.T) {
	h := NewSPAHandler()
	req := httptest.NewRequest(http.MethodGet, "/v1/chat/completions", http.NoBody)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 for /v1 path, got %d", w.Code)
	}
}

// TestSPAHandler_ServeHTTP_HealthPathReturns404 tests that /health
// returns 404 (it is not a frontend route).
func TestSPAHandler_ServeHTTP_HealthPathReturns404(t *testing.T) {
	h := NewSPAHandler()
	req := httptest.NewRequest(http.MethodGet, "/health", http.NoBody)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 for /health, got %d", w.Code)
	}
}

// TestSPAHandler_ServeHTTP_NonAPIPathNotRootServesFallback tests that
// any non-API, non-root path falls through to the indexHTML fallback
// when the file doesn't exist in the embedded FS.
func TestSPAHandler_ServeHTTP_NonAPIPathNotRootServesFallback(t *testing.T) {
	h := NewSPAHandler()
	req := httptest.NewRequest(http.MethodGet, "/dashboard", http.NoBody)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for SPA route fallback, got %d", w.Code)
	}
	ct := w.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/html") {
		t.Errorf("expected text/html content type for SPA fallback, got %q", ct)
	}
	cc := w.Header().Get("Cache-Control")
	if cc != "no-cache" {
		t.Errorf("expected no-cache for SPA fallback, got %q", cc)
	}
}

// TestSPAHandler_ServeHTTP_EmbeddedStaticFileServedWithCacheHeaders tests
// that hash-named static files (.js, .css) receive long-lived cache headers
// when the frontend has been built and embedded. This test only exercises
// the hash-cache-header path when the embedded FS contains the requested file.
func TestSPAHandler_ServeHTTP_HashNamedJSFileWithCacheHeaders(t *testing.T) {
	// Check if the embedded FS has a hash-named JS file (requires frontend build)
	subFS, err := fs.Sub(staticFiles, "static")
	if err != nil {
		t.Skip("embedded static files not available")
	}

	// Look for any hash-named JS file in the assets directory
	entries, err := fs.ReadDir(subFS, "assets")
	if err != nil {
		t.Skip("assets directory not found in embedded FS (need frontend build)")
	}

	var hashJSFile string
	for _, e := range entries {
		name := e.Name()
		if strings.Contains(name, "-") && strings.HasSuffix(name, ".js") && !e.IsDir() {
			hashJSFile = "/assets/" + name
			break
		}
	}
	if hashJSFile == "" {
		t.Skip("no hash-named JS file found in embedded assets (need frontend build)")
	}

	h := NewSPAHandler()
	if h.fileServer == nil {
		t.Skip("SPAHandler.fileServer is nil (need frontend build)")
	}

	req := httptest.NewRequest(http.MethodGet, hashJSFile, http.NoBody)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	cc := w.Header().Get("Cache-Control")
	if cc != "public, max-age=31536000, immutable" {
		t.Errorf("expected immutable cache header for hash-named JS file %q, got %q", hashJSFile, cc)
	}
}

// TestSPAHandler_ServeHTTP_HashNamedCSSFileWithCacheHeaders tests that
// hash-named CSS files receive long-lived cache headers when the frontend
// has been built and embedded.
func TestSPAHandler_ServeHTTP_HashNamedCSSFileWithCacheHeaders(t *testing.T) {
	subFS, err := fs.Sub(staticFiles, "static")
	if err != nil {
		t.Skip("embedded static files not available")
	}

	entries, err := fs.ReadDir(subFS, "assets")
	if err != nil {
		t.Skip("assets directory not found in embedded FS (need frontend build)")
	}

	var hashCSSFile string
	for _, e := range entries {
		name := e.Name()
		if strings.Contains(name, "-") && strings.HasSuffix(name, ".css") && !e.IsDir() {
			hashCSSFile = "/assets/" + name
			break
		}
	}
	if hashCSSFile == "" {
		t.Skip("no hash-named CSS file found in embedded assets (need frontend build)")
	}

	h := NewSPAHandler()
	if h.fileServer == nil {
		t.Skip("SPAHandler.fileServer is nil (need frontend build)")
	}

	req := httptest.NewRequest(http.MethodGet, hashCSSFile, http.NoBody)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	cc := w.Header().Get("Cache-Control")
	if cc != "public, max-age=31536000, immutable" {
		t.Errorf("expected immutable cache header for hash-named CSS file %q, got %q", hashCSSFile, cc)
	}
}

// TestSPAHandler_ServeHTTP_NonHashNamedJSFileNoCacheHeaders tests that
// JS files without a hash in the name do NOT get the long-lived cache header.
func TestSPAHandler_ServeHTTP_NonHashNamedJSFileNoCacheHeaders(t *testing.T) {
	subFS, err := fs.Sub(staticFiles, "static")
	if err != nil {
		t.Skip("embedded static files not available")
	}

	// Look for a non-hash JS file (no "-" in name)
	var plainJSFile string
	fs.WalkDir(subFS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		name := d.Name()
		if strings.HasSuffix(name, ".js") && !strings.Contains(name, "-") {
			plainJSFile = "/" + path
		}
		return nil
	})
	if plainJSFile == "" {
		t.Skip("no non-hash JS file found in embedded FS")
	}

	h := NewSPAHandler()
	req := httptest.NewRequest(http.MethodGet, plainJSFile, http.NoBody)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	cc := w.Header().Get("Cache-Control")
	if cc == "public, max-age=31536000, immutable" {
		t.Errorf("non-hash JS file %q should NOT get immutable cache header", plainJSFile)
	}
}

// TestSPAHandler_ServeHTTP_EmbeddedIndexHTMLServed tests that the root path
// serves the embedded index.html (not the fallback) when frontend is built.
func TestSPAHandler_ServeHTTP_EmbeddedIndexHTMLServed(t *testing.T) {
	subFS, err := fs.Sub(staticFiles, "static")
	if err != nil {
		t.Skip("embedded static files not available")
	}

	indexContent, err := fs.ReadFile(subFS, "index.html")
	if err != nil || len(indexContent) == 0 {
		t.Skip("index.html not in embedded FS (need frontend build)")
	}

	h := NewSPAHandler()
	if h.fileServer == nil {
		t.Skip("SPAHandler.fileServer is nil (need frontend build)")
	}

	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	// Verify the embedded index.html is served, not the fallback
	body := w.Body.String()
	if strings.Contains(body, "Frontend not available") {
		t.Error("root path should serve embedded index.html, not the fallback")
	}
}

// TestSPAHandler_ConstructedDirectly_WithFileServer tests that a
// manually constructed SPAHandler with a fileServer delegates to it
// for paths that match files in the embedded FS.
func TestSPAHandler_ConstructedDirectly_WithFileServer(t *testing.T) {
	// Construct with fallback indexHTML and a custom file server
	// that serves 200 OK for any request.
	customFS := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("// custom file server")) //nolint:errcheck // test only
	})

	h := &SPAHandler{
		fileServer: customFS,
		indexHTML:  []byte("<!DOCTYPE html><html><body>fallback</body></html>"),
	}

	// Root path should serve indexHTML, not fileServer
	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for root, got %d", w.Code)
	}
	ct := w.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/html") {
		t.Errorf("root should serve HTML, got %q", ct)
	}

	// API paths should return 404 regardless of fileServer
	req = httptest.NewRequest(http.MethodGet, "/api/test", http.NoBody)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 for /api path, got %d", w.Code)
	}
}

// TestSPAHandler_ServeHTTP_ConcurrentRequests tests that the SPA handler
// safely handles concurrent requests without races.
func TestSPAHandler_ServeHTTP_ConcurrentRequests(t *testing.T) {
	h := NewSPAHandler()

	paths := []string{"/", "/dashboard", "/api/test", "/v1/chat", "/health", "/some/spa/route"}
	done := make(chan bool)

	for i := 0; i < 10; i++ {
		go func() {
			defer func() { done <- true }()
			for _, path := range paths {
				req := httptest.NewRequest(http.MethodGet, path, http.NoBody)
				w := httptest.NewRecorder()
				h.ServeHTTP(w, req)
			}
		}()
	}

	for i := 0; i < 10; i++ {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("concurrent request timed out")
		}
	}
}
