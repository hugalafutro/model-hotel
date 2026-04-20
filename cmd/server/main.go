package main

import (
	"context"
	"embed"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/user/llm-proxy/internal/admin"
	"github.com/user/llm-proxy/internal/api"
	"github.com/user/llm-proxy/internal/config"
	"github.com/user/llm-proxy/internal/db"
	"github.com/user/llm-proxy/internal/model"
	"github.com/user/llm-proxy/internal/provider"
	"github.com/user/llm-proxy/internal/proxy"
	"github.com/user/llm-proxy/internal/settings"
)

//go:embed all:static
var staticFiles embed.FS

type SPAHandler struct {
	fileServer http.Handler
	indexHTML  []byte
	staticDir  string // empty if using embedded FS
	useEmbed   bool
}

func NewSPAHandler() *SPAHandler {
	// Try embedded filesystem first (production)
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

	// Fall back to filesystem (for development)
	staticDir := "./web/dist"
	indexHTML, err := os.ReadFile(staticDir + "/index.html")
	if err != nil {
		log.Printf("WARNING: Could not read frontend files: %v", err)
		return &SPAHandler{
			indexHTML: []byte("<!DOCTYPE html><html><body><h1>LLM-Proxy</h1><p>Frontend not available. Run <code>cd web && npm run build</code></p></body></html>"),
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

	// Don't intercept API or proxy routes
	if strings.HasPrefix(path, "/api") || strings.HasPrefix(path, "/v1") || path == "/health" {
		http.NotFound(w, r)
		return
	}

	// Try to serve static assets (js, css, images, etc.) 
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

	// SPA fallback: serve index.html
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write(h.indexHTML)
}

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	log.Printf("Starting LLM-Proxy with configuration:\n%s", cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	database, err := db.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer database.Close()

	if err := database.WaitForReady(ctx, 30); err != nil {
		log.Fatalf("Database not ready: %v", err)
	}

	adminMgr, err := admin.New(cfg.DataDir)
	if err != nil {
		log.Fatalf("Failed to initialize admin manager: %v", err)
	}

	log.Printf("Admin token: %s", adminMgr.Token())

	providerRepo := provider.NewRepository(database.Pool())
	modelRepo := model.NewRepository(database.Pool())

	r := chi.NewRouter()

	// Global middleware
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))
	r.Use(middleware.Compress(5))

	// Security headers
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("X-Frame-Options", "DENY")
			w.Header().Set("X-XSS-Protection", "1; mode=block")
			next.ServeHTTP(w, r)
		})
	})

	// CORS middleware
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin == "" {
				next.ServeHTTP(w, r)
				return
			}

			allowed := false
			for _, pattern := range cfg.CORSOrigins {
				if origin == pattern {
					allowed = true
					break
				}
			}

			// Allow any HTTPS origin (production deployments)
			if strings.HasPrefix(origin, "https://") {
				allowed = true
			}

			if allowed {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
				w.Header().Set("Access-Control-Allow-Credentials", "true")
				w.Header().Set("Access-Control-Max-Age", "86400")
			}

			if r.Method == "OPTIONS" {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	})

	// Request size limit
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, cfg.MaxRequestSize)
			next.ServeHTTP(w, r)
		})
	})

	// Health check
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("OK"))
	})

	// API routes
	r.Route("/api", func(r chi.Router) {
		apiHandler := api.NewHandler(cfg, providerRepo, database, adminMgr)
		apiHandler.Register(r)
	})

	// Proxy routes
	r.Route("/v1", func(r chi.Router) {
		proxyHandler := proxy.NewHandler(cfg, providerRepo, modelRepo, database.Pool())
		proxyHandler.Register(r)
	})

	// SPA handler for frontend
	spaHandler := NewSPAHandler()
	r.Get("/*", spaHandler.ServeHTTP)

	// Startup: run initial discovery for all enabled providers (if enabled)
	settingsRepo := settings.NewRepository(database.Pool())
	runDiscovery := func() {
		ctx := context.Background()
		providers, err := providerRepo.List(ctx)
		if err != nil {
			log.Printf("Discovery: failed to list providers: %v", err)
			return
		}
		discoverySvc := provider.NewDiscoveryService()
		for _, p := range providers {
			if !p.Enabled {
				continue
			}
			models, err := discoverySvc.DiscoverModels(ctx, p, cfg.MasterKey)
			if err != nil {
				log.Printf("Discovery: failed for provider %s: %v", p.Name, err)
				continue
			}
			existingModelIDs := make([]string, 0, len(models))
			for _, m := range models {
				if err := modelRepo.Upsert(ctx, m); err != nil {
					log.Printf("Discovery: failed to upsert model %s: %v", m.ModelID, err)
				} else {
					existingModelIDs = append(existingModelIDs, m.ModelID)
				}
			}
			if err := modelRepo.DisableMissingModels(ctx, p.ID, existingModelIDs); err != nil {
				log.Printf("Discovery: failed to disable missing models for %s: %v", p.Name, err)
			}
			log.Printf("Discovery: discovered %d models for provider %s", len(models), p.Name)
		}
	}

	if settingsRepo.GetBool(context.Background(), "discovery_on_startup", true) {
		recentlyDiscovered := false
		providers, err := providerRepo.List(context.Background())
		if err == nil {
			for _, p := range providers {
				if p.LastDiscoveredAt != nil && time.Since(*p.LastDiscoveredAt) < 5*time.Minute {
					recentlyDiscovered = true
					break
				}
			}
		}
		if recentlyDiscovered {
			log.Println("Skipping startup discovery: last discovery was within 5 minutes")
		} else {
			go runDiscovery()
		}
	}

	// Periodic discovery based on settings interval
	go func() {
		for {
			interval := settingsRepo.GetDuration(context.Background(), "discovery_interval", 6*time.Hour)
			if interval == 0 {
				time.Sleep(1 * time.Minute)
				continue
			}
			time.Sleep(interval)
			if settingsRepo.GetBool(context.Background(), "discovery_on_startup", true) {
				runDiscovery()
			}
		}
	}()

	server := &http.Server{
		Addr:    cfg.Port,
		Handler: r,
	}

	go func() {
		log.Printf("Server listening on %s", cfg.Port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed to start: %v", err)
		}
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	log.Println("Shutting down server gracefully...")
	shutdownCtx, shutdownCancel := context.WithTimeout(ctx, 10*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("Error during server shutdown: %v", err)
	}

	log.Println("Server stopped")
}