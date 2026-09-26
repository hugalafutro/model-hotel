package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"golang.org/x/sync/singleflight"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/httpx"
)

const (
	githubReleasesURL = "https://api.github.com/repos/hugalafutro/model-hotel/releases/latest"
	githubTagsURL     = "https://api.github.com/repos/hugalafutro/model-hotel/tags?per_page=1"
	versionCacheTTL   = 30 * time.Minute
	// versionRetryTTL is how long a failed lookup keeps serving the stale tag
	// before trying GitHub again, so an outage costs one lookup and one log
	// line per window instead of one per dashboard load.
	versionRetryTTL = 5 * time.Minute
	// versionLookupTimeout bounds a whole lookup, both GitHub calls included.
	// The lookup is detached from the visitor but still runs inside a handler,
	// so it is kept under the 10s server.Shutdown drain in cmd/server: a
	// lookup started just before shutdown ends before the drain gives up.
	versionLookupTimeout = 8 * time.Second
)

// githubRelease matches the subset of GitHub's release API response we need.
type githubRelease struct {
	TagName string `json:"tag_name"`
}

// githubTag matches the subset of GitHub's tags API response we need.
type githubTag struct {
	Name string `json:"name"`
}

// versionCache holds the cached latest release tag and when it goes stale.
// A lookup that failed with no tag to serve leaves tag empty and holds its
// error in lastErr until freshUntil, so the outage answers from the cache too.
type versionCache struct {
	mu         sync.Mutex
	tag        string
	lastErr    error
	freshUntil time.Time
}

var vCache versionCache

// versionLookupGroup coalesces concurrent cache misses into one GitHub lookup,
// so a burst of dashboards loading after a restart or cache expiry costs one
// call against GitHub's unauthenticated rate limit, not one per visitor.
var versionLookupGroup singleflight.Group

// githubClient is the single client both GitHub lookups use, holding the 10s
// per-request bound each of them runs under. Its nil Transport is
// http.DefaultTransport, so connections to api.github.com pool across calls
// either way; one client is about having one definition of that timeout rather
// than two that can drift, and no per-call allocation on a polled route.
var githubClient = &http.Client{Timeout: 10 * time.Second}

// errNotFound indicates the GitHub releases endpoint returned 404.
var errNotFound = errors.New("no releases found")

// RegisterVersion mounts the version check route.
func (h *Handler) RegisterVersion(r chi.Router) {
	r.Get("/version/latest", h.GetLatestVersion)
}

// GetLatestVersion proxies the GitHub latest-release API with server-side caching.
// This avoids CSP connect-src violations in the browser (the frontend fetches
// /api/version/latest instead of api.github.com directly).
func (h *Handler) GetLatestVersion(w http.ResponseWriter, r *http.Request) {
	if tag, fresh, err := cachedTag(); fresh {
		writeTagOrFailure(w, tag, err)
		return
	}

	// The lookup fills a cache every later visitor reads, so a visitor who
	// leaves mid-fetch (reload, navigation) must not abort it and log a
	// failure that is not one; versionLookupTimeout bounds it instead.
	detached := context.WithoutCancel(r.Context())
	v, err, _ := versionLookupGroup.Do(h.ghReleasesURL, func() (any, error) {
		ctx, cancel := context.WithTimeout(detached, versionLookupTimeout)
		defer cancel()
		return refreshLatestTag(ctx, h.ghReleasesURL, h.ghTagsURL)
	})
	tagName, _ := v.(string)
	writeTagOrFailure(w, tagName, err)
}

// cachedTag returns the cached answer and whether it is still inside its
// window: a tag, or the error of a lookup that failed with no tag to serve.
func cachedTag() (tag string, fresh bool, err error) {
	vCache.mu.Lock()
	defer vCache.mu.Unlock()
	fresh = time.Now().Before(vCache.freshUntil) && (vCache.tag != "" || vCache.lastErr != nil)
	return vCache.tag, fresh, vCache.lastErr
}

// writeTagOrFailure answers with the tag, or a 502 when the lookup failed with
// no tag to serve. The failure is logged once inside the flight that met it,
// not once per visitor sharing it or reading it from the cache.
func writeTagOrFailure(w http.ResponseWriter, tag string, err error) {
	if err != nil {
		http.Error(w, "failed to fetch latest version", http.StatusBadGateway)
		return
	}
	writeJSON(w, map[string]string{"tag_name": tag})
}

// refreshLatestTag fetches the latest tag and stores it in vCache, logging a
// failure once for every visitor sharing the flight. It first rechecks the
// cache: a request that missed it just before another flight finished filling
// it reuses that result instead of calling GitHub again.
//
// A failed lookup keeps its answer for versionRetryTTL before GitHub is tried
// again: with a stale tag on hand it serves that tag and warns; with none it
// is an error, and the error is what the window serves.
//
// It tries the releases endpoint first. When the repo has tags but no formal
// GitHub Releases, that endpoint returns 404; only then does it fall back to
// the tags API. Other errors (5xx, timeout) skip the fallback to avoid
// doubling worst-case latency.
func refreshLatestTag(ctx context.Context, releasesURL, tagsURL string) (string, error) {
	tag, fresh, cachedErr := cachedTag()
	if fresh {
		return tag, cachedErr
	}

	tagName, err := fetchLatestTag(ctx, releasesURL)
	if errors.Is(err, errNotFound) {
		tagName, err = fetchLatestTagFromTags(ctx, tagsURL)
	}
	if err != nil {
		if tag == "" {
			debuglog.Error("version: all GitHub lookups failed", "error", err, "retry_in", versionRetryTTL)
			vCache.mu.Lock()
			vCache.lastErr = err
			vCache.freshUntil = time.Now().Add(versionRetryTTL)
			vCache.mu.Unlock()
			return "", err
		}
		debuglog.Warn("version: GitHub lookup failed, serving the cached tag", "error", err, "tag", tag, "retry_in", versionRetryTTL)
		tagName = tag
	}

	vCache.mu.Lock()
	vCache.tag = tagName
	vCache.lastErr = nil
	if err != nil {
		vCache.freshUntil = time.Now().Add(versionRetryTTL)
	} else {
		vCache.freshUntil = time.Now().Add(versionCacheTTL)
	}
	vCache.mu.Unlock()
	return tagName, nil
}

// githubGetJSON performs one GitHub API GET and decodes the body into out. The
// HTTP status is returned alongside the error so a caller can tell a missing
// resource from a failure.
func githubGetJSON(ctx context.Context, url string, out any) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return 0, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := githubClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("request failed: %w", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			debuglog.Error("version: failed to close response body", "error", closeErr)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, fmt.Errorf("GitHub returned status %d", resp.StatusCode)
	}
	if err := httpx.DecodeCappedJSON(resp.Body, httpx.MaxUpstreamBody, out); err != nil {
		return resp.StatusCode, fmt.Errorf("decode response: %w", err)
	}
	return resp.StatusCode, nil
}

// fetchLatestTag fetches the latest release tag from GitHub. A repository with
// no releases answers 404, reported as errNotFound so the caller can fall back
// to the tags API.
func fetchLatestTag(ctx context.Context, url string) (string, error) {
	var release githubRelease
	status, err := githubGetJSON(ctx, url, &release)
	if status == http.StatusNotFound {
		return "", errNotFound
	}
	if err != nil {
		return "", err
	}
	if release.TagName == "" {
		return "", fmt.Errorf("response missing tag_name")
	}
	return release.TagName, nil
}

// fetchLatestTagFromTags falls back to the tags API when no GitHub Releases exist.
// The tags are returned most-recent-first, so per_page=1 gives us the latest tag.
func fetchLatestTagFromTags(ctx context.Context, url string) (string, error) {
	var tags []githubTag
	if _, err := githubGetJSON(ctx, url, &tags); err != nil {
		return "", err
	}
	if len(tags) == 0 || tags[0].Name == "" {
		return "", fmt.Errorf("no tags found")
	}
	return tags[0].Name, nil
}
