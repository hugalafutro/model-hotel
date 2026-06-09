package main

import (
	"io/fs"
	"net/http"
	"strings"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

type SPAHandler struct {
	fileServer http.Handler
	indexHTML  []byte
	// staticFS is the sub-filesystem rooted at the static directory. It is
	// non-nil only when a real index.html was found; ServeHTTP uses it to
	// resolve static files. When nil, every non-API path falls back to indexHTML.
	staticFS fs.FS
}

// NewSPAHandler builds the SPA handler from the embedded frontend assets.
func NewSPAHandler() *SPAHandler {
	return newSPAHandler(staticFiles)
}

// newSPAHandler builds an SPAHandler from the given filesystem (which must
// contain a "static" subdirectory). Splitting this out lets tests supply an
// in-memory filesystem instead of the embedded production assets.
func newSPAHandler(embed fs.FS) *SPAHandler {
	fallback := []byte("<!DOCTYPE html><html><body><h1>Model Hotel</h1><p>Frontend not available.</p></body></html>")

	subFS, err := fs.Sub(embed, "static")
	if err != nil {
		debuglog.Error("server: embedded static files not found", "error", err)
		return &SPAHandler{indexHTML: fallback}
	}

	indexHTML, err := fs.ReadFile(subFS, "index.html")
	if err != nil || len(indexHTML) == 0 {
		debuglog.Error("server: embedded index.html not found", "error", err)
		return &SPAHandler{indexHTML: fallback}
	}

	debuglog.Info("server: serving frontend from embedded files")
	return &SPAHandler{
		fileServer: http.FileServer(http.FS(subFS)),
		indexHTML:  indexHTML,
		staticFS:   subFS,
	}
}

func (h *SPAHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	if strings.HasPrefix(path, "/api") || strings.HasPrefix(path, "/v1") || path == "/health" {
		http.NotFound(w, r)
		return
	}

	if path != "/" {
		cleanPath := strings.TrimPrefix(path, "/")
		if cleanPath != "" && h.staticFS != nil {
			if f, err := fs.Stat(h.staticFS, cleanPath); err == nil && !f.IsDir() {
				if strings.Contains(cleanPath, "-") && (strings.HasSuffix(cleanPath, ".js") || strings.HasSuffix(cleanPath, ".css")) {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				h.fileServer.ServeHTTP(w, r)
				return
			}
		}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	//nolint:gosec // content-type set to text/html; template output is sanitized
	_, _ = w.Write(h.indexHTML)
}
