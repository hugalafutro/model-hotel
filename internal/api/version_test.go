package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/go-chi/chi/v5"
)

func resetVersionCache() {
	vCache.mu.Lock()
	vCache.tag = ""
	vCache.freshUntil = time.Time{}
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
	vCache.freshUntil = time.Now().Add(versionCacheTTL)
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

// A visitor who leaves while the GitHub lookup is in flight cancels the
// request context; the lookup must still finish and fill the cache.
func TestGetLatestVersion_ClientCancelStillCaches(t *testing.T) {
	resetVersionCache()

	ghServer := newGHMockServer(t,
		func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"tag_name": "v3.0.0"})
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

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/version/latest", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusOK, w.Code, w.Body.String())
	}
	vCache.mu.Lock()
	cachedTag := vCache.tag
	vCache.mu.Unlock()
	if cachedTag != "v3.0.0" {
		t.Errorf("expected cached tag 'v3.0.0' despite the cancelled request, got %q", cachedTag)
	}
}

// roundTripFunc adapts a function to http.RoundTripper.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// A burst of cache misses shares one GitHub lookup instead of one each. The
// synctest bubble makes the barrier exact: synctest.Wait returns only once
// every visitor is durably blocked, the first inside the held GitHub call and
// the rest waiting on its flight, so none can arrive late and read the cache.
func TestGetLatestVersion_ConcurrentMissesShareOneLookup(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		resetVersionCache()

		var calls atomic.Int32
		release := make(chan struct{})
		orig := githubClient
		githubClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			calls.Add(1)
			<-release
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": {"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"tag_name":"v4.0.0"}`)),
			}, nil
		})}
		t.Cleanup(func() { githubClient = orig })

		h := &Handler{
			ghReleasesURL: "https://gh.test/repos/hugalafutro/model-hotel/releases/latest",
			ghTagsURL:     "https://gh.test/repos/hugalafutro/model-hotel/tags",
		}
		r := chi.NewRouter()
		h.RegisterVersion(r)

		const visitors = 5
		codes := make(chan int, visitors)
		var wg sync.WaitGroup
		for range visitors {
			wg.Go(func() {
				w := httptest.NewRecorder()
				r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/version/latest", http.NoBody))
				codes <- w.Code
			})
		}
		synctest.Wait()
		close(release)
		wg.Wait()
		close(codes)

		for code := range codes {
			if code != http.StatusOK {
				t.Errorf("expected every visitor to get %d, got %d", http.StatusOK, code)
			}
		}
		if got := calls.Load(); got != 1 {
			t.Errorf("expected one GitHub call for %d concurrent misses, got %d", visitors, got)
		}
	})
}

// countRecords reports how many captured records carry msg, and at what levels.
func countRecords(capt *attrCaptureHandler, msg string) (n int, levels []slog.Level) {
	capt.mu.Lock()
	defer capt.mu.Unlock()
	for _, rec := range capt.records {
		if rec.msg == msg {
			n++
			levels = append(levels, rec.level)
		}
	}
	return n, levels
}

// failingGitHub swaps githubClient for one whose every call answers 500 and
// counts the calls.
func failingGitHub(t *testing.T, release <-chan struct{}) *atomic.Int32 {
	t.Helper()
	var calls atomic.Int32
	orig := githubClient
	githubClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		if release != nil {
			<-release
		}
		return &http.Response{StatusCode: http.StatusInternalServerError, Body: io.NopCloser(strings.NewReader(""))}, nil
	})}
	t.Cleanup(func() { githubClient = orig })
	return &calls
}

func versionRouter() chi.Router {
	h := &Handler{
		ghReleasesURL: "https://gh.test/repos/hugalafutro/model-hotel/releases/latest",
		ghTagsURL:     "https://gh.test/repos/hugalafutro/model-hotel/tags",
	}
	r := chi.NewRouter()
	h.RegisterVersion(r)
	return r
}

// A failed lookup shared by a burst of visitors is one failure, so it writes
// one error line, not one per visitor waiting on the flight.
func TestGetLatestVersion_SharedFailureLogsOnce(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		resetVersionCache()
		capt := captureLogs(t)
		release := make(chan struct{})
		calls := failingGitHub(t, release)
		r := versionRouter()

		const visitors = 5
		var wg sync.WaitGroup
		for range visitors {
			wg.Go(func() {
				w := httptest.NewRecorder()
				r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/version/latest", http.NoBody))
				if w.Code != http.StatusBadGateway {
					t.Errorf("status = %d, want 502 with no tag to serve", w.Code)
				}
			})
		}
		synctest.Wait()
		close(release)
		wg.Wait()

		if got := calls.Load(); got != 1 {
			t.Fatalf("GitHub calls = %d, want the burst to share one", got)
		}
		n, levels := countRecords(capt, "version: all GitHub lookups failed")
		if n != 1 || levels[0] != slog.LevelError {
			t.Errorf("failure lines = %d at %v, want exactly one error", n, levels)
		}
		if n, _ := countRecords(capt, "api: failed to fetch latest version"); n != 0 {
			t.Errorf("per-visitor failure lines = %d, want none", n)
		}
	})
}

// During a GitHub outage with a stale tag on hand, a failed lookup serves the
// tag, warns once, and holds it for versionRetryTTL: the dashboard loads that
// follow neither call GitHub nor log again until the window passes.
func TestGetLatestVersion_StaleTagHoldsThroughAnOutage(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		resetVersionCache()
		vCache.mu.Lock()
		vCache.tag = "v1.0.0"
		vCache.freshUntil = time.Now().Add(-time.Minute)
		vCache.mu.Unlock()
		capt := captureLogs(t)
		calls := failingGitHub(t, nil)
		r := versionRouter()

		load := func() {
			t.Helper()
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/version/latest", http.NoBody))
			if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "v1.0.0") {
				t.Fatalf("got %d %q, want the stale tag served", w.Code, w.Body.String())
			}
		}

		for range 3 {
			load()
		}
		if got := calls.Load(); got != 1 {
			t.Errorf("GitHub calls = %d, want 1 within the retry window", got)
		}
		if n, _ := countRecords(capt, "version: all GitHub lookups failed"); n != 0 {
			t.Errorf("error lines = %d, want none while a tag is served", n)
		}
		if n, levels := countRecords(capt, "version: GitHub lookup failed, serving the cached tag"); n != 1 || levels[0] != slog.LevelWarn {
			t.Errorf("warn lines = %d at %v, want exactly one warning", n, levels)
		}

		time.Sleep(versionRetryTTL)
		load()
		if got := calls.Load(); got != 2 {
			t.Errorf("GitHub calls = %d, want a retry once the window passed", got)
		}
	})
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
	vCache.freshUntil = time.Now().Add(-90 * time.Minute)
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

	tag, err := fetchLatestTagFromTags(context.Background(), ts.URL)
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

	_, err := fetchLatestTagFromTags(context.Background(), ts.URL)
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

	_, err := fetchLatestTagFromTags(context.Background(), ts.URL)
	if err == nil {
		t.Error("expected error for empty tags array")
	}
}

func TestFetchLatestTagFromTags_ConnectionError(t *testing.T) {
	// Use a closed server to simulate connection error
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	ts.Close()

	_, err := fetchLatestTagFromTags(context.Background(), ts.URL)
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

	_, err := fetchLatestTagFromTags(context.Background(), ts.URL)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestFetchLatestTag_Non200Status(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	_, err := fetchLatestTag(context.Background(), ts.URL)
	if err == nil {
		t.Error("expected error for non-200 status from fetchLatestTag")
	}
}

func TestFetchLatestTag_404ReturnsErrNotFound(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	_, err := fetchLatestTag(context.Background(), ts.URL)
	if err == nil {
		t.Error("expected error for 404 from fetchLatestTag")
	}
	if !errors.Is(err, errNotFound) {
		t.Errorf("expected errNotFound, got %v", err)
	}
}

func TestFetchLatestTag_ConnectionError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	ts.Close()

	_, err := fetchLatestTag(context.Background(), ts.URL)
	if err == nil {
		t.Error("expected error for connection failure from fetchLatestTag")
	}
}

func TestFetchLatestTag_MissingTagName(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{}) // no tag_name
	}))
	defer ts.Close()

	_, err := fetchLatestTag(context.Background(), ts.URL)
	if err == nil {
		t.Error("expected error for missing tag_name from fetchLatestTag")
	}
}

func TestFetchLatestTag_InvalidJSON(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{invalid}`))
	}))
	defer ts.Close()

	_, err := fetchLatestTag(context.Background(), ts.URL)
	if err == nil {
		t.Error("expected error for invalid JSON from fetchLatestTag")
	}
}

func TestFetchLatestTagFromTags_EmptyTagNameInArray(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]githubTag{{Name: ""}})
	}))
	defer ts.Close()

	_, err := fetchLatestTagFromTags(context.Background(), ts.URL)
	if err == nil {
		t.Error("expected error for empty tag name from fetchLatestTagFromTags")
	}
}

// ---------------------------------------------------------------------------
// 8. fetchLatestTagFromTags — request creation error (invalid URL)
// ---------------------------------------------------------------------------

func TestFetchLatestTagFromTags_InvalidURL(t *testing.T) {
	_, err := fetchLatestTagFromTags(context.Background(), "http://invalid url with spaces.com/tags")
	if err == nil {
		t.Error("expected error for invalid URL")
	}
	if !strings.Contains(err.Error(), "create request") {
		t.Errorf("expected error about creating request, got: %v", err)
	}
}

// stubTransport answers every request itself and counts them, standing in for
// the shared client's transport so a lookup that built its own client instead
// would visibly miss it.
type stubTransport struct {
	mu   sync.Mutex
	n    int
	body string
}

func (s *stubTransport) RoundTrip(_ *http.Request) (*http.Response, error) {
	s.mu.Lock()
	s.n++
	s.mu.Unlock()
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(s.body)),
	}, nil
}

func (s *stubTransport) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.n
}

// TestVersionLookupsShareOneClient pins that both GitHub lookups go through the
// package client rather than each constructing its own: a per-call client is a
// per-call allocation and a second place the timeout can drift. Swapping the
// package client for a counting transport makes the difference observable, and
// the real server standing beside it must never be reached.
func TestVersionLookupsShareOneClient(t *testing.T) {
	hits := 0
	var hitsMu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hitsMu.Lock()
		hits++
		hitsMu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tag_name":"v0.0.0"}`))
	}))
	defer srv.Close()

	stub := &stubTransport{body: `{"tag_name":"v9.9.9"}`}
	original := githubClient
	githubClient = &http.Client{Transport: stub, Timeout: original.Timeout}
	t.Cleanup(func() { githubClient = original })

	h := &Handler{ghReleasesURL: srv.URL, ghTagsURL: srv.URL}
	if _, err := fetchLatestTag(context.Background(), h.ghReleasesURL); err != nil {
		t.Fatalf("fetchLatestTag failed: %v", err)
	}
	// The tags body is a list, which the stub above cannot serve, so the second
	// lookup is expected to fail decoding; reaching the stub at all is the point.
	_, _ = fetchLatestTagFromTags(context.Background(), h.ghTagsURL)

	if got := stub.count(); got != 2 {
		t.Errorf("shared client carried %d of the 2 lookups", got)
	}
	hitsMu.Lock()
	defer hitsMu.Unlock()
	if hits != 0 {
		t.Errorf("a lookup bypassed the shared client and reached the server %d times", hits)
	}
}
