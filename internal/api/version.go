package api

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

const (
	githubReleasesURL = "https://api.github.com/repos/hugalafutro/model-hotel/releases/latest"
	versionCacheTTL   = 30 * time.Minute
)

// githubRelease matches the subset of GitHub's release API response we need.
type githubRelease struct {
	TagName string `json:"tag_name"`
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

	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, h.ghReleasesURL, http.NoBody)
	if err != nil {
		respondError(w, "failed to create GitHub request", err, http.StatusInternalServerError)
		return
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := client.Do(req)
	if err != nil {
		debuglog.Error("version: GitHub request failed", "error", err)
		// Return stale cache if available, otherwise 502
		if tag != "" {
			writeJSON(w, map[string]string{"tag_name": tag})
			return
		}
		respondError(w, "failed to fetch latest release", err, http.StatusBadGateway)
		return
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			debuglog.Error("version: failed to close response body", "error", closeErr)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		debuglog.Error("version: GitHub returned non-200", "status", resp.StatusCode)
		if tag != "" {
			writeJSON(w, map[string]string{"tag_name": tag})
			return
		}
		http.Error(w, "upstream error", http.StatusBadGateway)
		return
	}

	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		debuglog.Error("version: failed to decode GitHub response", "error", err)
		respondError(w, "failed to decode release", err, http.StatusInternalServerError)
		return
	}

	if release.TagName == "" {
		debuglog.Error("version: GitHub response missing tag_name")
		http.Error(w, "no tag_name in response", http.StatusBadGateway)
		return
	}

	vCache.mu.Lock()
	vCache.tag = release.TagName
	vCache.fetchedAt = time.Now()
	vCache.mu.Unlock()

	writeJSON(w, map[string]string{"tag_name": release.TagName})
}
