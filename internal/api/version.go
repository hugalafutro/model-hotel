package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/httpx"
)

const (
	githubReleasesURL = "https://api.github.com/repos/hugalafutro/model-hotel/releases/latest"
	githubTagsURL     = "https://api.github.com/repos/hugalafutro/model-hotel/tags?per_page=1"
	versionCacheTTL   = 30 * time.Minute
)

// githubRelease matches the subset of GitHub's release API response we need.
type githubRelease struct {
	TagName string `json:"tag_name"`
}

// githubTag matches the subset of GitHub's tags API response we need.
type githubTag struct {
	Name string `json:"name"`
}

// versionCache holds the cached latest release tag and expiry.
type versionCache struct {
	mu        sync.Mutex
	tag       string
	fetchedAt time.Time
}

var vCache versionCache

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
	vCache.mu.Lock()
	tag := vCache.tag
	fetchedAt := vCache.fetchedAt
	vCache.mu.Unlock()

	if tag != "" && time.Since(fetchedAt) < versionCacheTTL {
		writeJSON(w, map[string]string{"tag_name": tag})
		return
	}

	// Try the releases endpoint first. When the repo has tags but no formal
	// GitHub Releases, the endpoint returns 404 — fall back to the tags API
	// only in that case. For other errors (5xx, timeout) skip the fallback to
	// avoid doubling worst-case latency.
	tagName, err := fetchLatestTag(r.Context(), h.ghReleasesURL)
	if errors.Is(err, errNotFound) {
		tagName, err = fetchLatestTagFromTags(r.Context(), h.ghTagsURL)
	}
	if err != nil {
		debuglog.Error("version: all GitHub lookups failed", "error", err)
		if tag != "" {
			writeJSON(w, map[string]string{"tag_name": tag})
			return
		}
		respondError(w, "failed to fetch latest version", err, http.StatusBadGateway)
		return
	}

	vCache.mu.Lock()
	vCache.tag = tagName
	vCache.fetchedAt = time.Now()
	vCache.mu.Unlock()

	writeJSON(w, map[string]string{"tag_name": tagName})
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
