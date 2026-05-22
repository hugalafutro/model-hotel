package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

func resetVersionCache() {
	vCache.mu.Lock()
	vCache.tag = ""
	vCache.fetchedAt = time.Time{}
	vCache.mu.Unlock()
}

func TestGetLatestVersion_CacheHit(t *testing.T) {
	resetVersionCache()

	// Pre-populate cache
	vCache.mu.Lock()
	vCache.tag = "v1.2.3"
	vCache.fetchedAt = time.Now()
	vCache.mu.Unlock()

	h := &Handler{
		ghReleasesURL: githubReleasesURL,
	}
	r := chi.NewRouter()
	h.RegisterVersion(r)

	req := httptest.NewRequest(http.MethodGet, "/version/latest", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	var result map[string]string
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if result["tag_name"] != "v1.2.3" {
		t.Errorf("expected tag_name 'v1.2.3', got %q", result["tag_name"])
	}
}

func TestGetLatestVersion_FetchSuccess(t *testing.T) {
	resetVersionCache()

	// Mock GitHub API
	ghServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != "application/vnd.github+json" {
			t.Errorf("expected GitHub Accept header, got %q", r.Header.Get("Accept"))
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"tag_name": "v2.0.0"})
	}))
	defer ghServer.Close()

	h := &Handler{
		ghReleasesURL: ghServer.URL,
	}
	r := chi.NewRouter()
	h.RegisterVersion(r)

	req := httptest.NewRequest(http.MethodGet, "/version/latest", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusOK, w.Code, w.Body.String())
	}

	var result map[string]string
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if result["tag_name"] != "v2.0.0" {
		t.Errorf("expected tag_name 'v2.0.0', got %q", result["tag_name"])
	}

	// Verify cache was populated
	vCache.mu.Lock()
	cachedTag := vCache.tag
	vCache.mu.Unlock()
	if cachedTag != "v2.0.0" {
		t.Errorf("expected cached tag 'v2.0.0', got %q", cachedTag)
	}
}

func TestGetLatestVersion_StaleCacheFallback(t *testing.T) {
	resetVersionCache()

	// Set stale cache (expired)
	vCache.mu.Lock()
	vCache.tag = "v1.0.0"
	vCache.fetchedAt = time.Now().Add(-2 * time.Hour) // stale
	vCache.mu.Unlock()

	// Mock GitHub API returning 500
	ghServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ghServer.Close()

	h := &Handler{
		ghReleasesURL: ghServer.URL,
	}
	r := chi.NewRouter()
	h.RegisterVersion(r)

	req := httptest.NewRequest(http.MethodGet, "/version/latest", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Should fall back to stale cache
	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusOK, w.Code, w.Body.String())
	}

	var result map[string]string
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if result["tag_name"] != "v1.0.0" {
		t.Errorf("expected stale tag_name 'v1.0.0', got %q", result["tag_name"])
	}
}

func TestGetLatestVersion_NoCache_UpstreamError(t *testing.T) {
	resetVersionCache()

	// Mock GitHub API returning 500
	ghServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ghServer.Close()

	h := &Handler{
		ghReleasesURL: ghServer.URL,
	}
	r := chi.NewRouter()
	h.RegisterVersion(r)

	req := httptest.NewRequest(http.MethodGet, "/version/latest", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// With no cache and upstream error, should get 502
	if w.Code != http.StatusBadGateway {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusBadGateway, w.Code, w.Body.String())
	}
}

func TestGetLatestVersion_MissingTagName(t *testing.T) {
	resetVersionCache()

	// Mock GitHub API returning response without tag_name
	ghServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{}) // no tag_name
	}))
	defer ghServer.Close()

	h := &Handler{
		ghReleasesURL: ghServer.URL,
	}
	r := chi.NewRouter()
	h.RegisterVersion(r)

	req := httptest.NewRequest(http.MethodGet, "/version/latest", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Missing tag_name should return 502
	if w.Code != http.StatusBadGateway {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusBadGateway, w.Code, w.Body.String())
	}
}

func TestGetLatestVersion_InvalidJSON(t *testing.T) {
	resetVersionCache()

	// Mock GitHub API returning invalid JSON
	ghServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{invalid json}`))
	}))
	defer ghServer.Close()

	h := &Handler{
		ghReleasesURL: ghServer.URL,
	}
	r := chi.NewRouter()
	h.RegisterVersion(r)

	req := httptest.NewRequest(http.MethodGet, "/version/latest", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Invalid JSON should return 500
	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusInternalServerError, w.Code, w.Body.String())
	}
}
