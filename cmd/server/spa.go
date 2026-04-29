package main

import (
	"embed"
	"io/fs"
	"log"
	"net/http"
	"os"
	"strings"
)

var _ embed.FS

type SPAHandler struct {
	fileServer http.Handler
	indexHTML  []byte
	staticDir  string
	useEmbed   bool
}

func NewSPAHandler() *SPAHandler {
	subFS, err := fs.Sub(staticFiles, "static")
	if err == nil {
		indexHTML, readErr := fs.ReadFile(subFS, "index.html")
		if readErr == nil && len(indexHTML) > 0 {
			log.Println("Using embedded static files")
			return &SPAHandler{
				fileServer: http.FileServer(http.FS(subFS)),
				indexHTML:  indexHTML,
				useEmbed:   true,
			}
		}
	}

	staticDir := "./web/dist"
	indexHTML, err := os.ReadFile(staticDir + "/index.html")
	if err != nil {
		log.Printf("WARNING: Could not read frontend files: %v", err)
		return &SPAHandler{
			indexHTML: []byte("<!DOCTYPE html><html><body><h1>Model Hotel</h1><p>Frontend not available. Run <code>cd web && npm run build</code></p></body></html>"),
			useEmbed:  false,
			staticDir: staticDir,
		}
	}

	log.Println("Using filesystem static files from " + staticDir)
	return &SPAHandler{
		fileServer: http.FileServer(http.Dir(staticDir)),
		indexHTML:  indexHTML,
		useEmbed:   false,
		staticDir:  staticDir,
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
		if cleanPath != "" {
			var exists bool
			if h.useEmbed {
				subFS, _ := fs.Sub(staticFiles, "static")
				if f, err := fs.Stat(subFS, cleanPath); err == nil && !f.IsDir() {
					exists = true
				}
			} else {
				if _, err := os.Stat(h.staticDir + "/" + cleanPath); err == nil {
					exists = true
				}
			}

			if exists {
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
	_, _ = w.Write(h.indexHTML)
}
