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

// newGHMockServer creates a test server that routes requests to /releases/latest
// and /tags based on the provided handlers. If a handler is nil, it returns 404.
func newGHMockServer(t *testing.T, releasesHandler, tagsHandler http.HandlerFunc) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	if releasesHandler != nil {
		mux.HandleFunc("/repos/hugalafutro/model-hotel/releases/latest", releasesHandler)
	} else {
		mux.HandleFunc("/repos/hugalafutro/model-hotel/releases/latest", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		})
	}
	if tagsHandler != nil {
		mux.HandleFunc("/repos/hugalafutro/model-hotel/tags", tagsHandler)
	} else {
		mux.HandleFunc("/repos/hugalafutro/model-hotel/tags", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		})
	}
	return httptest.NewServer(mux)
}

func TestGetLatestVersion_CacheHit(t *testing.T) {
	resetVersionCache()

	vCache.mu.Lock()
	vCache.tag = "v1.2.3"
	vCache.fetchedAt = time.Now()
	vCache.mu.Unlock()

	h := &Handler{
		ghReleasesURL: githubReleasesURL,
		ghTagsURL:     githubTagsURL,
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

	ghServer := newGHMockServer(t,
		func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"tag_name": "v2.0.0"})
		},
		nil, // tags not needed when releases succeeds
	)
	defer ghServer.Close()

	h := &Handler{
		ghReleasesURL: ghServer.URL + "/repos/hugalafutro/model-hotel/releases/latest",
		ghTagsURL:     ghServer.URL + "/repos/hugalafutro/model-hotel/tags",
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

	vCache.mu.Lock()
	cachedTag := vCache.tag
	vCache.mu.Unlock()
	if cachedTag != "v2.0.0" {
		t.Errorf("expected cached tag 'v2.0.0', got %q", cachedTag)
	}
}

func TestGetLatestVersion_TagsFallback(t *testing.T) {
	resetVersionCache()

	// Releases returns 404 (no GitHub Releases exist), tags returns the latest tag
	ghServer := newGHMockServer(t,
		nil, // releases returns 404 by default
		func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode([]map[string]string{{"name": "v0.9.5"}})
		},
	)
	defer ghServer.Close()

	h := &Handler{
		ghReleasesURL: ghServer.URL + "/repos/hugalafutro/model-hotel/releases/latest",
		ghTagsURL:     ghServer.URL + "/repos/hugalafutro/model-hotel/tags",
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
	if result["tag_name"] != "v0.9.5" {
		t.Errorf("expected tag_name 'v0.9.5', got %q", result["tag_name"])
	}
}

func TestGetLatestVersion_StaleCacheFallback(t *testing.T) {
	resetVersionCache()

	vCache.mu.Lock()
	vCache.tag = "v1.0.0"
	vCache.fetchedAt = time.Now().Add(-2 * time.Hour)
	vCache.mu.Unlock()

	// Both endpoints return errors
	ghServer := newGHMockServer(t,
		func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusInternalServerError) },
		func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusInternalServerError) },
	)
	defer ghServer.Close()

	h := &Handler{
		ghReleasesURL: ghServer.URL + "/repos/hugalafutro/model-hotel/releases/latest",
		ghTagsURL:     ghServer.URL + "/repos/hugalafutro/model-hotel/tags",
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
	if result["tag_name"] != "v1.0.0" {
		t.Errorf("expected stale tag_name 'v1.0.0', got %q", result["tag_name"])
	}
}

func TestGetLatestVersion_NoCache_UpstreamError(t *testing.T) {
	resetVersionCache()

	// Both endpoints return 500
	ghServer := newGHMockServer(t,
		func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusInternalServerError) },
		func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusInternalServerError) },
	)
	defer ghServer.Close()

	h := &Handler{
		ghReleasesURL: ghServer.URL + "/repos/hugalafutro/model-hotel/releases/latest",
		ghTagsURL:     ghServer.URL + "/repos/hugalafutro/model-hotel/tags",
	}
	r := chi.NewRouter()
	h.RegisterVersion(r)

	req := httptest.NewRequest(http.MethodGet, "/version/latest", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusBadGateway, w.Code, w.Body.String())
	}
}

func TestGetLatestVersion_MissingTagName(t *testing.T) {
	resetVersionCache()

	ghServer := newGHMockServer(t,
		func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{}) // no tag_name
		},
		nil,
	)
	defer ghServer.Close()

	h := &Handler{
		ghReleasesURL: ghServer.URL + "/repos/hugalafutro/model-hotel/releases/latest",
		ghTagsURL:     ghServer.URL + "/repos/hugalafutro/model-hotel/tags",
	}
	r := chi.NewRouter()
	h.RegisterVersion(r)

	req := httptest.NewRequest(http.MethodGet, "/version/latest", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusBadGateway, w.Code, w.Body.String())
	}
}

func TestGetLatestVersion_InvalidJSON(t *testing.T) {
	resetVersionCache()

	ghServer := newGHMockServer(t,
		func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{invalid json}`))
		},
		nil,
	)
	defer ghServer.Close()

	h := &Handler{
		ghReleasesURL: ghServer.URL + "/repos/hugalafutro/model-hotel/releases/latest",
		ghTagsURL:     ghServer.URL + "/repos/hugalafutro/model-hotel/tags",
	}
	r := chi.NewRouter()
	h.RegisterVersion(r)

	req := httptest.NewRequest(http.MethodGet, "/version/latest", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusBadGateway, w.Code, w.Body.String())
	}
}
