package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
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

	// Try the releases endpoint first (returns 404 when no GitHub Releases exist,
	// even if the repo has tags). Fall back to the tags endpoint in that case.
	tagName, err := h.fetchLatestTag(r.Context(), h.ghReleasesURL)
	if err != nil {
		// 404 from /releases/latest is expected when a repo has tags but no
		// formal releases. Fall back to /tags which always works.
		tagName, err = h.fetchLatestTagFromTags(r.Context(), h.ghTagsURL)
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

// fetchLatestTag fetches the latest release tag from GitHub.
func (h *Handler) fetchLatestTag(ctx context.Context, url string) (string, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			debuglog.Error("version: failed to close response body", "error", closeErr)
		}
	}()

	if resp.StatusCode == http.StatusNotFound {
		return "", fmt.Errorf("no releases found")
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub returned status %d", resp.StatusCode)
	}

	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}
	if release.TagName == "" {
		return "", fmt.Errorf("response missing tag_name")
	}
	return release.TagName, nil
}

// fetchLatestTagFromTags falls back to the tags API when no GitHub Releases exist.
// The tags are returned most-recent-first, so per_page=1 gives us the latest tag.
func (h *Handler) fetchLatestTagFromTags(ctx context.Context, url string) (string, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return "", fmt.Errorf("create tags request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("tags request failed: %w", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			debuglog.Error("version: failed to close tags response body", "error", closeErr)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("tags API returned status %d", resp.StatusCode)
	}

	var tags []githubTag
	if err := json.NewDecoder(resp.Body).Decode(&tags); err != nil {
		return "", fmt.Errorf("decode tags response: %w", err)
	}
	if len(tags) == 0 || tags[0].Name == "" {
		return "", fmt.Errorf("no tags found")
	}
	return tags[0].Name, nil
}
