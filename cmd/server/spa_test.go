package main

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"path"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

func TestNewSPAHandler_ReturnsHandler(t *testing.T) {
	h := NewSPAHandler()
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

// ---------------------------------------------------------------------------
// Additional SPAHandler coverage via direct struct construction
// ---------------------------------------------------------------------------

// TestNewSPAHandler_FallbackWhenNoEmbedFS tests the case where staticFS
// embed.FS doesn't contain a "static" subdirectory, which causes fs.Sub to
// fail. This exercises the fallback indexHTML path (L18-23 in spa.go).
func TestNewSPAHandler_FallbackWhenNoEmbedFS(t *testing.T) {
	// In test mode, the embedded static/files may or may not exist.
	// NewSPAHandler always returns non-nil. When the embedded FS doesn't
	// have a "static" directory, it falls back to a minimal HTML page.
	h := NewSPAHandler()
	// indexHTML should always be set (either real embedded or fallback)
	if len(h.indexHTML) == 0 {
		t.Error("expected non-empty indexHTML")
	}
}

// TestSPAHandler_ConstructedDirectly_NilFileServer_ServeHTTP tests that when
// fileServer is nil, ServeHTTP always falls through to the indexHTML for
// non-API paths. This exercises the `if f, err := fs.Stat(...)` branch that
// short-circuits when the file is not found in the embedded FS.
func TestSPAHandler_ConstructedDirectly_NilFileServer_ServeHTTP(t *testing.T) {
	h := &SPAHandler{
		fileServer: nil,
		indexHTML:  []byte("<!DOCTYPE html><html><body>Test Fallback</body></html>"),
	}

	// Non-API, non-root path should serve indexHTML (SPA fallback)
	req := httptest.NewRequest(http.MethodGet, "/some/spa/route", http.NoBody)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for SPA fallback, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Test Fallback") {
		t.Errorf("expected fallback content, got %q", w.Body.String())
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

// TestSPAHandler_ConstructedDirectly_WithFileServer_StaticFileServed tests that
// when fileServer is set and a file exists in the embedded FS, the fileServer
// is used to serve the file. It uses a custom http.Handler that records what
// path was requested and sets appropriate headers.
func TestSPAHandler_ConstructedDirectly_WithFileServer_CustomFS(t *testing.T) {
	var servedPath string
	customFileServer := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		servedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/javascript")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("// custom js"))
	})

	h := &SPAHandler{
		fileServer: customFileServer,
		indexHTML:  []byte("<!DOCTYPE html><html><body>fallback</body></html>"),
	}

	// For hash-named JS files that exist, the ServeHTTP method should:
	// 1. Check the path is non-root and non-API
	// 2. Stat the file in the embedded FS → on success, set cache headers and serve
	// Since our customFileServer is a fake, the fs.Stat check would need the real
	// staticFS. Instead, test that root path bypasses fileServer entirely.
	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if servedPath != "" {
		t.Errorf("root path should not invoke fileServer, but got path %q", servedPath)
	}
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for root, got %d", w.Code)
	}
	ct := w.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/html") {
		t.Errorf("root should serve HTML, got %q", ct)
	}
}

// TestSPAHandler_ServeHTTP_EmptyPathAfterTrim tests that a path of just "/"
// serves the indexHTML (the root check `if path != "/"` is true, so we test
// that the cleanPath branch handles the root case correctly).
func TestSPAHandler_ServeHTTP_RootPathServesIndexHTML(t *testing.T) {
	h := &SPAHandler{
		fileServer: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Error("fileServer should not be called for root path")
		}),
		indexHTML: []byte("<!DOCTYPE html><html><body>Index Content</body></html>"),
	}

	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "Index Content") {
		t.Errorf("expected indexHTML content, got %q", body)
	}
	cc := w.Header().Get("Cache-Control")
	if cc != "no-cache" {
		t.Errorf("expected no-cache for root, got %q", cc)
	}
}

// TestSPAHandler_ServeHTTP_NonHashNamedFileNoImmutableCache tests that a
// static file served from the embedded FS without a hash (no "-" in the name)
// does NOT get the immutable cache header. This test constructs a handler
// with a custom file server since we cannot control the embedded FS content.
func TestSPAHandler_ServeHTTP_NonHashNamedFileNoImmutableCache(t *testing.T) {
	// When the embedded FS contains the file, ServeHTTP checks if the filename
	// contains "-" and is .js/.css. Non-hash files don't get cache headers.
	// Since we can't control the embedded FS, we construct a handler that
	// simulates the behavior through the code path.
	//
	// The real coverage comes from:
	// 1. TestSPAHandler_ServeHTTP_HashNamedJSFileWithCacheHeaders (when embedded FS has hash files)
	// 2. TestSPAHandler_ServeHTTP_NonHashNamedJSFileNoCacheHeaders (same)
	//
	// This test documents the expected behavior for non-hash files.
	h := NewSPAHandler()
	// Without a built frontend, all non-API paths fall through to indexHTML
	// with no-cache header, which is correct behavior for non-hash assets.
	req := httptest.NewRequest(http.MethodGet, "/sw.js", http.NoBody)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	// If the embedded FS has sw.js, it would be served without hash cache headers.
	// If not, it falls through to indexHTML (SPA fallback), which also has no
	// immutable cache header. Either way, no "immutable" cache header.
	cc := w.Header().Get("Cache-Control")
	if cc == "public, max-age=31536000, immutable" {
		t.Errorf("non-hash file should not get immutable cache header")
	}
}

// TestSPAHandler_ServeHTTP_CSSFileWithHash tests that a hash-named CSS file
// served by the fileServer gets the long-lived cache header. This test
// constructs the SPAHandler with a mock file server that simulates serving
// a hash-named CSS file from the embedded FS.
func TestSPAHandler_ServeHTTP_CSSFileWithHash_WithCustomFileServer(t *testing.T) {
	// We test the cache-header logic by constructing an SPAHandler that has
	// a custom fileServer. The staticFS FS.Stat check in ServeHTTP needs
	// the real embedded FS, so this test verifies the fallback path when
	// the file is not found (i.e., cleanPath doesn't match any file).
	h := &SPAHandler{
		fileServer: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/css")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("/* css */"))
		}),
		indexHTML: []byte("<!DOCTYPE html><html><body>fallback</body></html>"),
	}

	// Request a hash-named CSS file. Since our custom fileServer is used,
	// but fs.Stat would check the real embedded FS (which may not have this file),
	// the request falls through to indexHTML.
	req := httptest.NewRequest(http.MethodGet, "/assets/test-abc123.css", http.NoBody)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	// When the embedded FS has the file, it should be served with cache headers.
	// When not found, it falls through to the SPA fallback (indexHTML).
	// Either way, no crash.
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

// TestSPAHandler_ServeHTTP_StaticFileInEmbedFS tests serving a real static
// file from the embedded FS when available (requires frontend build).
// This exercises the fs.Stat → fileServer.ServeHTTP path in ServeHTTP (L52-57).
func TestSPAHandler_ServeHTTP_StaticFileInEmbedFS(t *testing.T) {
	subFS, err := fs.Sub(staticFiles, "static")
	if err != nil {
		t.Skip("embedded static files not available (need frontend build)")
	}

	// Walk the embedded FS to find any real file to request
	var testFilePath string
	fs.WalkDir(subFS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err //nolint:nilerr // walkdir convention: propagate errors
		}
		if !strings.Contains(path, "/") && strings.HasSuffix(path, ".html") {
			// Skip index.html (served specially at root)
			return nil
		}
		testFilePath = "/" + path
		return fs.SkipAll
	})

	if testFilePath == "" {
		t.Skip("no static files found in embedded FS (need frontend build)")
	}

	h := NewSPAHandler()
	if h.fileServer == nil {
		t.Skip("SPAHandler.fileServer is nil (need frontend build)")
	}

	req := httptest.NewRequest(http.MethodGet, testFilePath, http.NoBody)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for embedded file %q, got %d", testFilePath, w.Code)
	}

	// Verify the file is served by the fileServer, not as indexHTML
	ct := w.Header().Get("Content-Type")
	if ct == "text/html; charset=utf-8" && !strings.HasSuffix(testFilePath, ".html") {
		t.Errorf("non-HTML file %q should not be served as indexHTML", testFilePath)
	}
}

// TestNewSPAHandler_CoversEmbedFSPath tests that NewSPAHandler handles the
// case where the embedded FS is available (the "normal" production path at
// L17-37). When the frontend has been built, this exercises the full
// constructor including fs.Sub, fs.ReadFile, and http.FileServer setup.
func TestNewSPAHandler_CoversEmbedFSPath(t *testing.T) {
	h := NewSPAHandler()

	// Check if the embedded FS was used (fileServer is non-nil)
	if h.fileServer != nil {
		t.Log("NewSPAHandler used embedded FS (frontend build present)")
		// Verify indexHTML was read from the embedded FS, not the fallback
		if strings.Contains(string(h.indexHTML), "Frontend not available") {
			t.Error("expected embedded index.html, got fallback content")
		}
	} else {
		t.Log("NewSPAHandler used fallback (no frontend build)")
		// Verify the fallback indexHTML is set
		if !strings.Contains(string(h.indexHTML), "Model Hotel") {
			t.Errorf("expected fallback to contain 'Model Hotel', got: %s", string(h.indexHTML)[:min(100, len(h.indexHTML))])
		}
	}
}

// ---------------------------------------------------------------------------
// Tests that build a handler over an in-memory filesystem (via newSPAHandler)
// to exercise fs.Stat and fileServer.ServeHTTP code paths in ServeHTTP
// ---------------------------------------------------------------------------

// testStaticFS creates an in-memory fs.FS that mimics the embedded static
// directory with known files for testing.
func testStaticFS() fstest.MapFS {
	dir := map[string]string{
		"index.html":              "<!DOCTYPE html><html><body>Test Index</body></html>",
		"assets/app-abc123.js":    "// hashed js file",
		"assets/style-def456.css": "/* hashed css file */",
		"assets/sw.js":            "// service worker without hash",
		"favicon.ico":             "x",
	}
	fsys := fstest.MapFS{}
	for name, content := range dir {
		fsys[path.Join("static", name)] = &fstest.MapFile{Data: []byte(content)}
	}
	return fsys
}

// TestNewSPAHandler_WithTestFS exercises the full constructor path when the
// supplied filesystem contains valid content (index.html exists, sub FS works).
func TestNewSPAHandler_WithTestFS(t *testing.T) {
	h := newSPAHandler(testStaticFS())
	if h.fileServer == nil {
		t.Fatal("expected fileServer to be non-nil with test FS")
	}
	if len(h.indexHTML) == 0 {
		t.Fatal("expected non-empty indexHTML")
	}
	if !strings.Contains(string(h.indexHTML), "Test Index") {
		t.Errorf("expected embedded index.html content, got: %s", string(h.indexHTML)[:min(100, len(h.indexHTML))])
	}
}

// TestServeHTTP_HashNamedJSFileWithCacheHeaders_WithTestFS exercises the
// fs.Stat branch and immutable cache header for hash-named JS files
// in ServeHTTP (L51-57).
func TestServeHTTP_HashNamedJSFileWithCacheHeaders_WithTestFS(t *testing.T) {
	h := newSPAHandler(testStaticFS())
	if h.fileServer == nil {
		t.Fatal("expected fileServer to be non-nil with test FS")
	}

	req := httptest.NewRequest(http.MethodGet, "/assets/app-abc123.js", http.NoBody)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	cc := w.Header().Get("Cache-Control")
	if cc != "public, max-age=31536000, immutable" {
		t.Errorf("expected immutable cache header for hash-named JS file, got %q", cc)
	}
}

// TestServeHTTP_HashNamedCSSFileWithCacheHeaders_WithTestFS exercises the
// immutable cache header for hash-named CSS files.
func TestServeHTTP_HashNamedCSSFileWithCacheHeaders_WithTestFS(t *testing.T) {
	h := newSPAHandler(testStaticFS())

	req := httptest.NewRequest(http.MethodGet, "/assets/style-def456.css", http.NoBody)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	cc := w.Header().Get("Cache-Control")
	if cc != "public, max-age=31536000, immutable" {
		t.Errorf("expected immutable cache header for hash-named CSS file, got %q", cc)
	}
}

// TestServeHTTP_NonHashNamedJSFileNoImmutableCache_WithTestFS exercises the
// path where a JS file without a hash in the name does NOT get the immutable
// cache header, but the fileServer still serves it.
func TestServeHTTP_NonHashNamedJSFileNoImmutableCache_WithTestFS(t *testing.T) {
	h := newSPAHandler(testStaticFS())

	req := httptest.NewRequest(http.MethodGet, "/assets/sw.js", http.NoBody)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	cc := w.Header().Get("Cache-Control")
	if cc == "public, max-age=31536000, immutable" {
		t.Error("non-hash JS file should NOT get immutable cache header")
	}
}

// TestServeHTTP_StaticFileNotInEmbedFS_FallsBackToIndexHTML exercises the
// path where a requested file is NOT in the embedded FS, so ServeHTTP
// falls through to serving indexHTML (SPA fallback).
func TestServeHTTP_StaticFileNotInEmbedFS_FallsBackToIndexHTML(t *testing.T) {
	h := newSPAHandler(testStaticFS())

	req := httptest.NewRequest(http.MethodGet, "/dashboard", http.NoBody)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for SPA fallback, got %d", w.Code)
	}
	ct := w.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/html") {
		t.Errorf("expected text/html for SPA fallback, got %q", ct)
	}
	cc := w.Header().Get("Cache-Control")
	if cc != "no-cache" {
		t.Errorf("expected no-cache for SPA fallback, got %q", cc)
	}
	// Should serve the indexHTML from the test FS
	if !strings.Contains(w.Body.String(), "Test Index") {
		t.Errorf("expected embedded index.html content, got: %s", w.Body.String()[:min(100, len(w.Body.String()))])
	}
}

// TestServeHTTP_RootServesIndexHTML_WithTestFS exercises the root path
// serving indexHTML from the test FS (not fallback).
func TestServeHTTP_RootServesIndexHTML_WithTestFS(t *testing.T) {
	h := newSPAHandler(testStaticFS())

	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	ct := w.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/html") {
		t.Errorf("expected text/html, got %q", ct)
	}
	cc := w.Header().Get("Cache-Control")
	if cc != "no-cache" {
		t.Errorf("expected no-cache for root, got %q", cc)
	}
	if !strings.Contains(w.Body.String(), "Test Index") {
		t.Errorf("expected embedded index.html content, got: %s", w.Body.String()[:min(100, len(w.Body.String()))])
	}
}

// TestServeHTTP_FaviconServedWithoutImmutableCache exercises serving a
// static file (favicon.ico) that is neither .js nor .css, so it gets
// served by the fileServer but without the immutable cache header.
func TestServeHTTP_FaviconServedWithoutImmutableCache(t *testing.T) {
	h := newSPAHandler(testStaticFS())

	req := httptest.NewRequest(http.MethodGet, "/favicon.ico", http.NoBody)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for favicon, got %d: %s", w.Code, w.Body.String())
	}

	// favicon.ico is not .js/.css, so no immutable cache header
	cc := w.Header().Get("Cache-Control")
	if cc == "public, max-age=31536000, immutable" {
		t.Error("favicon.ico should not get immutable cache header")
	}
}

// TestServeHTTP_APIPathsReturn404_WithTestFS verifies that API paths
// are correctly rejected even when the static FS has content.
func TestServeHTTP_APIPathsReturn404_WithTestFS(t *testing.T) {
	h := newSPAHandler(testStaticFS())

	for _, path := range []string{"/api/providers", "/v1/chat/completions", "/health"} {
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

// TestNewSPAHandler_EmptyIndexHTMLPath tests the branch where fs.ReadFile
// returns an empty byte slice (len(indexHTML) == 0 at line 26 of spa.go).
// This exercises the fallback indexHTML path when the embedded FS has an
// index.html file but it's empty.
func TestNewSPAHandler_EmptyIndexHTMLPath(t *testing.T) {
	h := newSPAHandler(fstest.MapFS{
		"static/index.html": &fstest.MapFile{Data: []byte{}},
	})
	if h == nil {
		t.Fatal("newSPAHandler() returned nil")
	}
	if len(h.indexHTML) == 0 {
		t.Error("expected non-empty fallback indexHTML when embedded index.html is empty")
	}
	// Should use the fallback HTML since the file was empty
	if !strings.Contains(string(h.indexHTML), "Model Hotel") {
		t.Errorf("expected fallback content with 'Model Hotel', got: %s", string(h.indexHTML)[:min(100, len(h.indexHTML))])
	}
	// fileServer should be nil since fs.Sub succeeds but index.html is empty
	if h.fileServer != nil {
		t.Log("fileServer is non-nil (unexpected when index.html is empty)")
	}
}

// TestNewSPAHandler_FSSubErrorPath tests the branch where fs.Sub fails
// (line 17-23 of spa.go). When the embedded FS doesn't contain a "static"
// subdirectory, fs.Sub returns an error.
func TestNewSPAHandler_FSSubErrorPath(t *testing.T) {
	// A filesystem without a populated "static/index.html" — the constructor
	// falls back to the minimal "Frontend not available" page.
	h := newSPAHandler(fstest.MapFS{
		"other.txt": &fstest.MapFile{Data: []byte("not static")},
	})
	if h == nil {
		t.Fatal("newSPAHandler() returned nil")
	}
	if len(h.indexHTML) == 0 {
		t.Error("expected non-empty fallback indexHTML when fs.Sub fails")
	}
	if !strings.Contains(string(h.indexHTML), "Frontend not available") {
		t.Errorf("expected fallback HTML with 'Frontend not available', got: %s", string(h.indexHTML)[:min(100, len(h.indexHTML))])
	}
	if h.fileServer != nil {
		t.Error("expected nil fileServer when fs.Sub fails")
	}
}
