package api

import (
	"context"
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

	// Verify cache was populated from tags fallback
	vCache.mu.Lock()
	cachedTag := vCache.tag
	vCache.mu.Unlock()
	if cachedTag != "v0.9.5" {
		t.Errorf("expected cached tag 'v0.9.5', got %q", cachedTag)
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

func TestGetLatestVersion_Releases500_NoTagsFallback(t *testing.T) {
	resetVersionCache()

	// Releases returns 500, tags would succeed — but fallback should NOT be
	// triggered because only a 404 from releases triggers the tags path.
	ghServer := newGHMockServer(t,
		func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusInternalServerError) },
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

	// Should get 502, not the tags result — proving the fallback was skipped
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

func TestGetLatestVersion_EmptyTagsArray(t *testing.T) {
	resetVersionCache()

	// Releases returns 404, tags returns empty array []
	ghServer := newGHMockServer(t,
		nil, // releases returns 404 by default
		func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode([]githubTag{}) // empty array
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

	if w.Code != http.StatusBadGateway {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusBadGateway, w.Code, w.Body.String())
	}
}

func TestGetLatestVersion_EmptyTagName(t *testing.T) {
	resetVersionCache()

	// Releases returns 404, tags returns array with empty name
	ghServer := newGHMockServer(t,
		nil, // releases returns 404 by default
		func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode([]githubTag{{Name: ""}}) // empty name
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

	if w.Code != http.StatusBadGateway {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusBadGateway, w.Code, w.Body.String())
	}
}

func TestGetLatestVersion_TagsRateLimited(t *testing.T) {
	resetVersionCache()

	// Releases returns 404, tags returns 403 (rate limited)
	ghServer := newGHMockServer(t,
		nil, // releases returns 404 by default
		func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusForbidden) // rate limited
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

	if w.Code != http.StatusBadGateway {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusBadGateway, w.Code, w.Body.String())
	}
}

func TestGetLatestVersion_EmptyTagNameFromReleases(t *testing.T) {
	resetVersionCache()

	// Releases returns 200 but tag_name is empty string
	ghServer := newGHMockServer(t,
		func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(githubRelease{TagName: ""}) // empty tag_name
		},
		nil, // tags not needed
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

func TestGetLatestVersion_InvalidJSONFromTags(t *testing.T) {
	resetVersionCache()

	// Releases returns 404, tags returns invalid JSON
	ghServer := newGHMockServer(t,
		nil, // releases returns 404 by default
		func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{invalid json}`)) // invalid JSON
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

	if w.Code != http.StatusBadGateway {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusBadGateway, w.Code, w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// fetchLatestTagFromTags direct tests
// ---------------------------------------------------------------------------

func TestFetchLatestTagFromTags_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]githubTag{{Name: "v1.0.0"}, {Name: "v0.9.0"}})
	}))
	defer ts.Close()

	h := &Handler{}
	tag, err := h.fetchLatestTagFromTags(context.Background(), ts.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tag != "v1.0.0" {
		t.Errorf("tag = %q, want v1.0.0", tag)
	}
}

func TestFetchLatestTagFromTags_Non200Status(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer ts.Close()

	h := &Handler{}
	_, err := h.fetchLatestTagFromTags(context.Background(), ts.URL)
	if err == nil {
		t.Error("expected error for non-200 status")
	}
}

func TestFetchLatestTagFromTags_EmptyTagsArray(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]githubTag{})
	}))
	defer ts.Close()

	h := &Handler{}
	_, err := h.fetchLatestTagFromTags(context.Background(), ts.URL)
	if err == nil {
		t.Error("expected error for empty tags array")
	}
}

func TestFetchLatestTagFromTags_ConnectionError(t *testing.T) {
	// Use a closed server to simulate connection error
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	ts.Close()

	h := &Handler{}
	_, err := h.fetchLatestTagFromTags(context.Background(), ts.URL)
	if err == nil {
		t.Error("expected error for connection failure")
	}
}

func TestFetchLatestTagFromTags_InvalidJSON(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`not json`))
	}))
	defer ts.Close()

	h := &Handler{}
	_, err := h.fetchLatestTagFromTags(context.Background(), ts.URL)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}
