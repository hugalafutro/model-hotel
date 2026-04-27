package main

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io"
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
	"github.com/user/llm-proxy/internal/ctxkeys"
	"github.com/user/llm-proxy/internal/db"
	"github.com/user/llm-proxy/internal/events"
	"github.com/user/llm-proxy/internal/failover"
	"github.com/user/llm-proxy/internal/model"
	"github.com/user/llm-proxy/internal/provider"
	"github.com/user/llm-proxy/internal/proxy"
	"github.com/user/llm-proxy/internal/ratelimit"
	"github.com/user/llm-proxy/internal/settings"
	"github.com/user/llm-proxy/internal/util"
	"github.com/user/llm-proxy/internal/virtualkey"
)

//go:embed all:static
var staticFiles embed.FS

type DiscoveryResult struct {
	ProvidersScanned int
	ProvidersFailed  int
	ModelsDiscovered int
	ModelsDisabled   int
	FailoverSyncErrs int
	Errors           []string
}

func publishDiscoveryEvent(source string, result DiscoveryResult) {
	switch {
	case result.ProvidersScanned == 0 && len(result.Errors) > 0:
		events.Publish(events.Event{
			Type:     "discovery.complete",
			Severity: "error",
			Message:  fmt.Sprintf("Discovery failed: %s", result.Errors[0]),
			Metadata: map[string]interface{}{"source": source, "errors": result.Errors},
		})
	case result.ProvidersFailed > 0:
		events.Publish(events.Event{
			Type:     "discovery.complete",
			Severity: "warning",
			Message:  fmt.Sprintf("Discovery partially failed: %d/%d providers OK, %d models found", result.ProvidersScanned-result.ProvidersFailed, result.ProvidersScanned, result.ModelsDiscovered),
			Metadata: map[string]interface{}{"source": source, "errors": result.Errors},
		})
	default:
		events.Publish(events.Event{
			Type:     "discovery.complete",
			Severity: "success",
			Message:  fmt.Sprintf("%s discovery complete: %d models across %d providers", source, result.ModelsDiscovered, result.ProvidersScanned),
			Metadata: map[string]interface{}{"source": source},
		})
	}
}

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

	// Clean up stale request logs left in "pending" or "streaming" state
	// from a previous server crash, restart, or unhandled error.
	// Using serverStartTime (captured before DB was ready) means we only
	// reclaim rows that predate this process — a genuine streaming request
	// that happens to be long-running is never touched.
	serverStartTime := time.Now()
	tag, err := database.Pool().Exec(context.Background(), `
		UPDATE request_logs
		SET state = 'failed', error_message = 'request interrupted (server restart)'
		WHERE state IN ('pending', 'streaming')
		  AND created_at < $1`, serverStartTime)
	if err == nil && tag.RowsAffected() > 0 {
		log.Printf("Startup cleanup: marked %d stale pending/streaming logs as failed", tag.RowsAffected())
		events.Publish(events.Event{
			Type:     "logs.stale_startup",
			Severity: "warning",
			Message:  fmt.Sprintf("Server restart interrupted %d pending requests", tag.RowsAffected()),
			Metadata: map[string]interface{}{"count": tag.RowsAffected()},
		})
	} else if err != nil {
		log.Printf("Startup cleanup: failed to clean stale logs: %v", err)
	}

	adminMgr, isNew, err := admin.New(cfg.DataDir, cfg.AdminToken)
	if err != nil {
		log.Fatalf("Failed to initialize admin manager: %v", err)
	}

	if isNew {
		token := adminMgr.Token()
		log.Printf(`
╔══════════════════════════════════════════════════════════════╗
║  ADMIN TOKEN (save now — this will NOT be shown again):     ║
║                                                              ║
║  %s                                                         ║
║                                                              ║
║  To regenerate: delete the admin-token file and restart.     ║
╚══════════════════════════════════════════════════════════════╝`, token)
		log.Printf("ADMIN_TOKEN=%s", token)
	} else {
		log.Println("Admin token loaded from file (already initialized)")
	}

	providerRepo := provider.NewRepository(database.Pool())
	modelRepo := model.NewRepository(database.Pool())
	virtualKeyRepo := virtualkey.NewRepository(database.Pool())
	settingsRepo := settings.NewRepository(database.Pool())
	failoverRepo := failover.NewRepository(database.Pool())
	rateLimiter := ratelimit.NewLimiter(settingsRepo)
	defer rateLimiter.Stop()

	r := chi.NewRouter()

	// Global middleware
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
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

	// Handlers shared across route groups
	apiHandler := api.NewHandler(cfg, providerRepo, database, adminMgr, virtualKeyRepo, settingsRepo)
	proxyHandler := proxy.NewHandler(cfg, providerRepo, modelRepo, database.Pool(), virtualKeyRepo, failoverRepo, settingsRepo, rateLimiter)

	// API routes
	r.Route("/api", func(r chi.Router) {
		// SSE endpoint — long-lived connection, must NOT have a request
		// timeout.  The handler detects client disconnect via
		// r.Context().Done() instead.
		r.Group(func(r chi.Router) {
			apiHandler.RegisterEvents(r)
		})

		// All other API routes — standard 60s timeout is appropriate for
		// admin/API calls.
		r.Group(func(r chi.Router) {
			r.Use(middleware.Timeout(60 * time.Second))
			apiHandler.Register(r)
		})
	})

	// Admin chat routes — admin-authenticated proxy for the Chat/Arena UI.
	// Uses streaming-aware timeout (same as /v1) and rate limiting by IP.
	r.Route("/api/chat", func(r chi.Router) {
		r.Use(apiHandler.AuthMiddleware)
		r.Use(streamingAwareTimeout(5 * time.Minute))
		proxyHandler.RegisterAdminChat(r)
	})

	// Proxy routes — streaming LLM requests can take many minutes.
	// We must NOT apply a blanket timeout here; instead we use a
	// streaming-aware middleware that:
	//   - streaming requests: no deadline (client-disconnect detection still works)
	//   - non-streaming requests: 5-minute deadline
	r.Route("/v1", func(r chi.Router) {
		r.Use(streamingAwareTimeout(5 * time.Minute))
		proxyHandler.Register(r)
	})

	// SPA handler for frontend
	spaHandler := NewSPAHandler()
	r.Get("/*", spaHandler.ServeHTTP)

	// Startup: run initial discovery for all enabled providers (if enabled)
	runDiscovery := func() DiscoveryResult {
		result := DiscoveryResult{}
		ctx := context.Background()
		providers, err := providerRepo.List(ctx)
		if err != nil {
			log.Printf("Discovery: failed to list providers: %v", err)
			result.Errors = append(result.Errors, fmt.Sprintf("failed to list providers: %v", err))
			return result
		}
		discoverySvc := provider.NewDiscoveryService()
		for _, p := range providers {
			if !p.Enabled {
				continue
			}
			result.ProvidersScanned++
			models, err := discoverySvc.DiscoverModels(ctx, p, cfg.MasterKey)
			if err != nil {
				log.Printf("Discovery: failed for provider %s: %v", p.Name, err)
				result.ProvidersFailed++
				result.Errors = append(result.Errors, fmt.Sprintf("provider %s: %v", p.Name, err))
				continue
			}
			result.ModelsDiscovered += len(models)
			existingModelIDs := make([]string, 0, len(models))
			for _, m := range models {
				if err := modelRepo.Upsert(ctx, m); err != nil {
					log.Printf("Discovery: failed to upsert model %s: %v", m.ModelID, err)
				} else {
					existingModelIDs = append(existingModelIDs, m.ModelID)
				}
			}
			disabledCount, err := modelRepo.DisableMissingModels(ctx, p.ID, existingModelIDs)
			if err != nil {
				log.Printf("Discovery: failed to disable missing models for %s: %v", p.Name, err)
			} else if disabledCount > 0 {
				result.ModelsDisabled += int(disabledCount)
				events.Publish(events.Event{
					Type:     "discovery.models_disabled",
					Severity: "warning",
					Message:  fmt.Sprintf("%d models no longer available at '%s' and were disabled", disabledCount, p.Name),
					Metadata: map[string]interface{}{"provider": p.Name, "count": disabledCount},
				})
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
				result.FailoverSyncErrs++
				events.Publish(events.Event{
					Type:     "failover.sync_error",
					Severity: "warning",
					Message:  fmt.Sprintf("Failover sync failed for model '%s'", modelID),
					Metadata: map[string]interface{}{"error": err.Error(), "model_id": modelID},
				})
			}
		}
		return result
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
			go func() {
				result := runDiscovery()
				publishDiscoveryEvent("Startup", result)
			}()
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
	// Sleep before the first run so we don't bypass the discovery_on_startup setting.
	// When discovery_on_startup is true, the startup go runDiscovery() above already
	// handles immediate discovery; when false, we must not discover on startup either.
	go func() {
		// Use a reusable timer instead of time.After, which leaks a
		// timer until it fires (the GC eventually collects it, but at
		// high intervals the accumulation is wasteful).
		interval := settingsRepo.GetDuration(context.Background(), "discovery_interval", 6*time.Hour)
		if interval == 0 {
			interval = 1 * time.Minute
		}
		timer := time.NewTimer(interval)
		defer timer.Stop()

		for {
			select {
			case <-timer.C:
				result := runDiscovery()
				publishDiscoveryEvent("Scheduled", result)
				// Re-read interval in case the setting changed.
				interval = settingsRepo.GetDuration(context.Background(), "discovery_interval", 6*time.Hour)
				if interval == 0 {
					interval = 1 * time.Minute
				}
				timer.Reset(interval)
			case <-ctx.Done():
				return
			}
		}
	}()

	// Periodic stale log cleanup: mark rows stuck in "pending"/"streaming"
	// as "failed". Two strategies are combined in a single pass:
	//
	//   1. Server-start-time check: any in-progress row that predates this
	//      process is definitively orphaned (the previous process is dead).
	//      This has zero false-positive risk regardless of request duration.
	//
	//   2. Age-based check: rows older than stale_request_timeout (default
	//      30m, configurable via Settings) are also marked failed. This
	//      catches in-process orphans (e.g. a panic skips the final
	//      updateRequestLog). The timeout is generous to avoid killing
	//      legitimate long-running streaming requests.
	go func() {
		timer := time.NewTimer(5 * time.Minute)
		defer timer.Stop()
		for {
			select {
			case <-timer.C:
			case <-ctx.Done():
				return
			}
			staleTimeout := settingsRepo.GetDuration(context.Background(), "stale_request_timeout", 30*time.Minute)
			if staleTimeout <= 0 {
				timer.Reset(5 * time.Minute)
				continue
			}
			// Build a PostgreSQL-safe interval string from the parsed duration.
			// Truncate to whole seconds to avoid fractional-unit issues (e.g. "30.5 minutes").
			totalSecs := int64(staleTimeout.Seconds())
			hours := totalSecs / 3600
			mins := (totalSecs % 3600) / 60
			secs := totalSecs % 60
			intervalStr := fmt.Sprintf("%d hours %d minutes %d seconds", hours, mins, secs)
			tag, err := database.Pool().Exec(context.Background(), `
				UPDATE request_logs
				SET state = 'failed', error_message = 'request interrupted (stale)'
				WHERE state IN ('pending', 'streaming')
				  AND (created_at < $1 OR created_at < NOW() - $2::interval)`,
				serverStartTime, intervalStr)
			if err == nil && tag.RowsAffected() > 0 {
				log.Printf("Stale log cleanup: marked %d stuck logs as failed", tag.RowsAffected())
				events.Publish(events.Event{
					Type:     "logs.stale_cleanup",
					Severity: "warning",
					Message:  fmt.Sprintf("Marked %d stale requests as interrupted", tag.RowsAffected()),
					Metadata: map[string]interface{}{"count": tag.RowsAffected()},
				})
			} else if err != nil {
				log.Printf("Stale log cleanup: failed: %v", err)
			}
			timer.Reset(5 * time.Minute)
		}
	}()

	// Log retention cleanup
	go func() {
		timer := time.NewTimer(1 * time.Hour)
		defer timer.Stop()
		for {
			select {
			case <-timer.C:
			case <-ctx.Done():
				return
			}
			retention := settingsRepo.GetWithDefault(context.Background(), "log_retention", "")
			if retention == "" {
				timer.Reset(1 * time.Hour)
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
				timer.Reset(1 * time.Hour)
				continue
			}
			tag, err := database.Pool().Exec(context.Background(),
				`DELETE FROM request_logs WHERE created_at < $1`, cutoff)
			if err == nil {
				log.Printf("Log retention (%s): deleted %d old entries", retention, tag.RowsAffected())
			}
			timer.Reset(1 * time.Hour)
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

	// Release goroutine-leaking resources before draining HTTP connections.
	proxyHandler.Close()
	util.CloseDockerClient()

	shutdownCtx, shutdownCancel := context.WithTimeout(ctx, 10*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("Error during server shutdown: %v", err)
	}

	log.Println("Server stopped")
}

// streamingAwareTimeout returns middleware that sets a request deadline only
// for non-streaming requests. Streaming LLM calls (e.g. code generation that
// runs for 10+ minutes) must not be killed by a short server-side timeout.
//
// It works by peeking at the request body to check the "stream" field:
//   - stream=true  → no context deadline (client disconnect detection still works)
//   - stream=false/absent → context deadline of maxNonStreamingDur
//
// The request body is stored in the context so downstream handlers can
// reuse it without a second allocation, and also restored as r.Body for
// any handler that reads it directly.
func streamingAwareTimeout(maxNonStreamingDur time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Only POST /v1/chat/completions carries a stream flag;
			// other routes (e.g. GET /v1/models) get the non-streaming timeout.
			if r.Method != http.MethodPost {
				ctx, cancel := context.WithTimeout(r.Context(), maxNonStreamingDur)
				defer cancel()
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			body, err := io.ReadAll(r.Body)
			r.Body.Close()
			if err != nil {
				http.Error(w, "failed to read request body", http.StatusBadRequest)
				return
			}

			isStreaming := false
			var parsed struct {
				Stream bool `json:"stream"`
			}
			if json.Unmarshal(body, &parsed) == nil {
				isStreaming = parsed.Stream
			}

			// Restore the body so downstream handlers can read it
			r.Body = io.NopCloser(bytes.NewReader(body))

			// Store body bytes in context so proxy handler can reuse them
			ctx := context.WithValue(r.Context(), ctxkeys.RequestBodyKey, body)

			if isStreaming {
				next.ServeHTTP(w, r.WithContext(ctx))
			} else {
				ctx, cancel := context.WithTimeout(ctx, maxNonStreamingDur)
				defer cancel()
				next.ServeHTTP(w, r.WithContext(ctx))
			}
		})
	}
}
