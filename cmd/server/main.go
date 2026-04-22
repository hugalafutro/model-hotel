package main

import (
	"context"
	"embed"
	"log"
	"net/http"
	"os"
	"os/signal"

	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/user/llm-proxy/internal/admin"
	"github.com/user/llm-proxy/internal/api"
	"github.com/user/llm-proxy/internal/auth"
	"github.com/user/llm-proxy/internal/config"
	"github.com/user/llm-proxy/internal/db"
	"github.com/user/llm-proxy/internal/failover"
	"github.com/user/llm-proxy/internal/model"
	"github.com/user/llm-proxy/internal/provider"
	"github.com/user/llm-proxy/internal/proxy"
	"github.com/user/llm-proxy/internal/settings"
	"github.com/user/llm-proxy/internal/virtualkey"
)

//go:embed all:static
var staticFiles embed.FS

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	log.Printf("Starting Model Hotel with configuration:\n%s", cfg)

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
	virtualKeyRepo := virtualkey.NewRepository(database.Pool())
	settingsRepo := settings.NewRepository(database.Pool())
	failoverRepo := failover.NewRepository(database.Pool())

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
			w.Header().Set("X-Content-Type-Options", "nosiff")
			w.Header().Set("X-Frame-Options", "DENY")
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

			w.Header().Set("Vary", "Origin")

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
		apiHandler := api.NewHandler(cfg, providerRepo, database, adminMgr, virtualKeyRepo)
		apiHandler.Register(r)
	})

	// Proxy routes
	r.Route("/v1", func(r chi.Router) {
		proxyHandler := proxy.NewHandler(cfg, providerRepo, modelRepo, database.Pool(), virtualKeyRepo, failoverRepo, settingsRepo)
		proxyHandler.Register(r)
	})

	// SPA handler for frontend
	spaHandler := NewSPAHandler()
	r.Get("/*", spaHandler.ServeHTTP)

	// Startup: run initial discovery for all enabled providers (if enabled)
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
			now := time.Now()
			if _, err := database.Pool().Exec(ctx, `UPDATE providers SET last_discovered_at = $1 WHERE id = $2`, now, p.ID); err != nil {
				log.Printf("Discovery: failed to update last_discovered_at for %s: %v", p.Name, err)
			}
			log.Printf("Discovery: discovered %d models for provider %s", len(models), p.Name)
		}

		seenModelIDs := make(map[string]bool)
		for _, p := range providers {
			if !p.Enabled {
				continue
			}
			models, _ := modelRepo.List(ctx, &p.ID)
			for _, m := range models {
				seenModelIDs[m.ModelID] = true
			}
		}
		for modelID := range seenModelIDs {
			if err := failoverRepo.SyncForModel(ctx, modelID); err != nil {
				log.Printf("Discovery: failed to sync failover for model %s: %v", modelID, err)
			}
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

	// Pre-warm caches: key decryption, provider, model, and failover
	go func() {
		ctx := context.Background()

		providers, err := providerRepo.List(ctx)
		if err != nil {
			log.Printf("Cache warm: failed to list providers: %v", err)
			return
		}
		enabledProviders := make([]*provider.Provider, 0, len(providers))
		for _, p := range providers {
			if !p.Enabled {
				continue
			}
			auth.WarmKeyCache(p.EncryptedKey, p.KeyNonce, p.KeySalt, cfg.MasterKey)
			enabledProviders = append(enabledProviders, p)
		}
		provider.WarmProviderCache(enabledProviders)

		enabledModels, err := modelRepo.ListEnabled(ctx)
		if err != nil {
			log.Printf("Cache warm: failed to list models: %v", err)
		} else {
			model.WarmModelCache(enabledModels)
		}

		failoverGroups, err := failoverRepo.List(ctx)
		if err != nil {
			log.Printf("Cache warm: failed to list failover groups: %v", err)
		} else {
			failover.WarmFailoverCache(failoverGroups)
		}

		log.Println("Key, provider, model, and failover caches warmed")
	}()

	// Periodic discovery based on settings interval
	go func() {
		for {
			interval := settingsRepo.GetDuration(context.Background(), "discovery_interval", 6*time.Hour)
			if interval == 0 {
				time.Sleep(1 * time.Minute)
				continue
			}
			runDiscovery()
			time.Sleep(interval)
		}
	}()

	// Log retention cleanup
	go func() {
		for {
			time.Sleep(1 * time.Hour)
			retention := settingsRepo.GetWithDefault(context.Background(), "log_retention", "")
			if retention == "" {
				continue
			}
			var cutoff time.Time
			switch retention {
			case "1h":
				cutoff = time.Now().Add(-1 * time.Hour)
			case "1d":
				cutoff = time.Now().Add(-24 * time.Hour)
			case "1w":
				cutoff = time.Now().Add(-7 * 24 * time.Hour)
			case "1m":
				cutoff = time.Now().Add(-30 * 24 * time.Hour)
			default:
				continue
			}
			tag, err := database.Pool().Exec(context.Background(),
				`DELETE FROM request_logs WHERE created_at < $1`, cutoff)
			if err == nil {
				log.Printf("Log retention (%s): deleted %d old entries", retention, tag.RowsAffected())
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
