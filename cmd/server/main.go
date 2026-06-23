package main

// Package main is the entry point for the model-hotel LLM gateway server.

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
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/hugalafutro/model-hotel/internal/admin"
	"github.com/hugalafutro/model-hotel/internal/alert"
	"github.com/hugalafutro/model-hotel/internal/api"
	"github.com/hugalafutro/model-hotel/internal/auth"
	"github.com/hugalafutro/model-hotel/internal/config"
	"github.com/hugalafutro/model-hotel/internal/ctxkeys"
	"github.com/hugalafutro/model-hotel/internal/db"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/events"
	"github.com/hugalafutro/model-hotel/internal/failover"
	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/otelexport"
	"github.com/hugalafutro/model-hotel/internal/provider"
	"github.com/hugalafutro/model-hotel/internal/proxy"
	"github.com/hugalafutro/model-hotel/internal/ratelimit"
	"github.com/hugalafutro/model-hotel/internal/settings"
	"github.com/hugalafutro/model-hotel/internal/totp"
	"github.com/hugalafutro/model-hotel/internal/util"
	"github.com/hugalafutro/model-hotel/internal/virtualkey"
	"github.com/hugalafutro/model-hotel/internal/webauthn"
)

// version is set at build time via -ldflags -X main.version=...
var version = "dev"

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

	// Initialise structured logger (reads DEBUG_LOG env var).
	// Must happen after config load so the env is available.
	debuglog.Init(cfg.DebugLog)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize admin manager before DB connection — it only needs the
	// data directory and env token, no database dependency.
	adminMgr, isNew, err := admin.New(cfg.DataDir, cfg.AdminToken)
	if err != nil {
		debuglog.Fatal("startup: failed to initialize admin manager", "error", err)
	}

	database, err := db.New(ctx, cfg.DatabaseURL, cfg.DBMaxConns, cfg.DBMinConns)
	if err != nil {
		debuglog.Fatal("startup: failed to connect to database", "error", err)
	}
	defer database.Close()

	if err := database.WaitForReady(ctx, 30); err != nil {
		debuglog.Fatal("startup: database not ready", "error", err)
	}

	api.InitAppLogBuffer(database.Pool())

	// Route slog output through the app log pipeline so debuglog calls reach the
	// ring buffer and database (not just os.Stdout). When OTLP log export is
	// enabled (OTEL_EXPORTER_OTLP_ENDPOINT), fan out to it too so the same
	// structured records are pushed to an OpenTelemetry collector.
	appLogHandler := api.NewAppSlogHandler(debuglog.Level())
	var otelLogShutdown func(context.Context) error
	if otelexport.LogsEnabled() {
		otelHandler, shutdown, oerr := otelexport.NewSlogHandler(ctx, "model-hotel", debuglog.Level())
		if oerr != nil {
			debuglog.Error("otel: OTLP log export init failed; continuing without it", "error", oerr)
		} else {
			appLogHandler = debuglog.NewFanout(appLogHandler, otelHandler)
			otelLogShutdown = shutdown
		}
	}
	debuglog.SetHandler(appLogHandler)
	// Logged after SetHandler installs the fan-out, so the confirmation itself
	// is also exported to the OTLP collector.
	if otelLogShutdown != nil {
		debuglog.Info("otel: OTLP log export enabled")
	}

	debuglog.Info("db: Database connected and migrations applied successfully")
	debuglog.Info("startup: admin token", "source", func() string {
		if isNew {
			return "generated"
		}
		return "loaded from file"
	}())
	debuglog.Info("config: Config loaded")

	// Clean up stale request logs left in "pending" or "streaming" state
	// from a previous server crash, restart, or unhandled error.
	// Using serverStartTime (captured before DB was ready) means we only
	// reclaim rows that predate this process — a genuine streaming request
	// that happens to be long-running is never touched.
	serverStartTime := time.Now()
	tag, err := database.Pool().Exec(context.Background(), `
		UPDATE request_logs
		SET state = 'failed', error_kind = 'internal', error_message = 'request interrupted (server restart)'
		WHERE state IN ('pending', 'streaming')
		  AND created_at < $1`, serverStartTime)
	if err == nil && tag.RowsAffected() > 0 {
		debuglog.Info("startup: stale log cleanup", "rows", tag.RowsAffected())
		events.Publish(events.Event{
			Type:     "logs.stale_startup",
			Severity: "warning",
			Message:  fmt.Sprintf("Server restart interrupted %d pending requests", tag.RowsAffected()),
			Metadata: map[string]interface{}{"count": tag.RowsAffected()},
		})
	} else if err != nil {
		debuglog.Error("startup: stale log cleanup failed", "error", err)
	}

	providerRepo := provider.NewRepository(database.Pool())
	modelRepo := model.NewRepository(database.Pool())
	virtualKeyRepo := virtualkey.NewRepository(database.Pool())
	settingsRepo := settings.NewRepository(database.Pool())
	failoverRepo := failover.NewRepository(database.Pool())
	rateLimiter := ratelimit.NewLimiter(settingsRepo)
	defer rateLimiter.Stop()

	tpmLimiter := ratelimit.NewTPMLimiter(settingsRepo)
	defer tpmLimiter.Stop()

	ipLimiter := ratelimit.NewIPLimiter(cfg.RateLimitIPRPS, cfg.RateLimitIPBurst, cfg.TrustedProxies, settingsRepo)
	defer ipLimiter.Stop()

	r := chi.NewRouter()

	// Global middleware
	r.Use(middleware.RequestID)
	r.Use(silentLogger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Compress(5))

	// Security headers
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Content-Type-Options", "nosniff")
			// When ALLOW_EMBED=true, X-Frame-Options and CSP frame-ancestors
			// are omitted entirely so any origin can embed the page in an
			// iframe (e.g. workspace browsers, Home Assistant).
			if !cfg.AllowEmbed {
				w.Header().Set("X-Frame-Options", "DENY")
			}
			w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
			// HSTS only over TLS. Plain HTTP (e.g. behind a reverse proxy that
			// terminates TLS) must not set HSTS or browsers will cache a broken
			// redirect to a non-existent HTTPS listener. Currently the server
			// only serves plain HTTP (ListenAndServe), so this guard is a
			// forward-compatible placeholder: it will activate automatically if
			// TLS is added later via ListenAndServeTLS.
			if r.TLS != nil {
				w.Header().Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains; preload")
			}
			// CSP allows same-origin scripts/styles (needed for embedded SPA).
			// Style 'unsafe-inline' is required for Vite's injected style tags (CSS-based
			// animations and dynamic theme overrides). Script 'unsafe-inline' is NOT
			// needed: Vite outputs module scripts, not inline ones.
			if cfg.AllowEmbed {
				w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; connect-src 'self'; base-uri 'self'; form-action 'self'")
			} else {
				w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
			}
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

	// Health check: reports database reachability (cached) so a load balancer
	// stops routing to an instance whose Postgres is down.
	r.Get("/health", api.NewHealthHandler(database.Pool()).ServeHTTP)

	// Handlers shared across route groups
	sd := proxy.NewSafeDialer(append(cfg.AllowedProviderHosts, config.KnownProviderHosts()...), cfg.KnownProxies)
	testModelTransport := &http.Transport{
		DialContext:           sd.DialContext,
		ResponseHeaderTimeout: 30 * time.Second,
		IdleConnTimeout:       30 * time.Second,
	}
	apiHandler := api.NewHandler(cfg, providerRepo, database, adminMgr, virtualKeyRepo, settingsRepo, version, testModelTransport, sd.CheckRedirect, sd.DialContext, sd.CheckRedirect)
	proxyHandler := proxy.NewHandler(cfg, providerRepo, modelRepo, database.Pool(), virtualKeyRepo, failoverRepo, settingsRepo, rateLimiter, tpmLimiter, ipLimiter, sd)
	apiHandler.SetCircuitBreaker(proxyHandler.CircuitBreaker())
	apiHandler.StartBackupScheduler(context.Background())

	// Outbound alerting: a single consumer of the events bus that forwards
	// operator-selected events to a stateless apprise-api container. Best-effort
	// — a missing/failing apprise-api never affects request serving. Runs for the
	// app lifetime (ctx), reading config live so toggles apply without a restart.
	alertDispatcher := alert.New(alert.NewSettingsConfigProvider(settingsRepo, cfg.MasterKey), nil)
	go alertDispatcher.Run(ctx)

	// Prometheus metrics at the conventional /metrics path (root, no IP rate
	// limiter so scrapers aren't throttled). Authenticated via METRICS_TOKEN or
	// the admin token — never unauthenticated.
	r.Handle("/metrics", apiHandler.MetricsHandler())

	// WebAuthn/FIDO2 passkey authentication (enabled when WEBAUTHN_RP_ID is set).
	// The session manager is hoisted out of the WebAuthnRPID block so it is
	// always available -- TOTP /totp/login reuses CreateAuthToken to mint session
	// tokens once 2FA is enabled, even when passkeys (RP) are not configured.
	var webauthnHandler *api.WebAuthnHandler
	webauthnRepo := webauthn.NewRepository(database.Pool())
	sessionMgr := webauthn.NewSessionManager(webauthnRepo)
	apiHandler.SetWebAuthnSessionManager(sessionMgr)

	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			if n, err := webauthnRepo.CleanupExpiredSessions(context.Background()); err != nil {
				debuglog.Error("webauthn: session cleanup failed", "error", err)
			} else if n > 0 {
				debuglog.Info("webauthn: cleaned up expired sessions", "count", n)
			}
		}
	}()

	// TOTP (RFC 6238) second-factor. Always constructed so the public status
	// and login endpoints are mounted; enforcement is driven by the cached
	// IsEnabled state wired into the Handler (AuthMiddleware gate).
	totpRepo := totp.NewRepository(database.Pool(), cfg.MasterKey)
	apiHandler.SetTotpStatus(totpRepo)
	totpHandler := api.NewTotpHandler(totpRepo, adminMgr, sessionMgr, ipLimiter, cfg.DemoReadOnly, apiHandler.TotpEnabled, apiHandler.RefreshTotpEnabled)

	if cfg.WebAuthnRPID != "" {
		rpOrigins := make([]string, len(cfg.WebAuthnRPOrigins))
		copy(rpOrigins, cfg.WebAuthnRPOrigins)
		if len(rpOrigins) == 0 {
			rpOrigins = make([]string, len(cfg.CORSOrigins))
			copy(rpOrigins, cfg.CORSOrigins)
		}
		if len(rpOrigins) == 0 {
			rpOrigins = []string{"http://localhost:" + strings.TrimPrefix(cfg.Port, ":")}
		}
		rp, err := webauthn.NewRelyingParty(cfg.WebAuthnRPID, cfg.WebAuthnRPDisplayName, rpOrigins)
		if err != nil {
			debuglog.Fatal("startup: failed to initialize WebAuthn relying party", "error", err)
		}
		webauthnHandler = api.NewWebAuthnHandler(webauthnRepo, rp, sessionMgr, adminMgr, ipLimiter, cfg.DemoReadOnly, apiHandler.TotpEnabled)

		debuglog.Info("webauthn: passkey authentication enabled", "rp_id", cfg.WebAuthnRPID)
	}

	// API routes — IP rate limiting protects admin auth from brute-force.
	r.Route("/api", func(r chi.Router) {
		r.Use(ipLimiter.Middleware)

		r.Group(func(r chi.Router) {
			apiHandler.RegisterEvents(r)
		})

		// Public, unauthenticated feature flags (e.g. read-only demo mode) so
		// the SPA can render correctly even on the login screen.
		r.Group(func(r chi.Router) {
			apiHandler.RegisterPublicConfig(r)
			apiHandler.RegisterDemoLogin(r)
		})

		if webauthnHandler != nil {
			r.Group(func(r chi.Router) {
				webauthnHandler.Register(r)
			})
		}

		// TOTP (RFC 6238) second-factor. Mounted unconditionally (not gated
		// on WebAuthnRPID): the public status/login + admin/session-gated
		// enroll/verify/disable endpoints work even without passkeys because
		// the session manager is always wired above.
		r.Group(func(r chi.Router) {
			totpHandler.Register(r)
		})

		r.Group(func(r chi.Router) {
			r.Use(middleware.Timeout(60 * time.Second))
			apiHandler.Register(r)
		})
	})

	// Admin chat routes — admin-authenticated proxy for the Chat/Arena UI.
	// Uses streaming-aware timeout (same as /v1) and rate limiting by IP.
	r.Route("/api/chat", func(r chi.Router) {
		r.Use(ipLimiter.Middleware)
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
	runDiscovery := func(source string) DiscoveryResult {
		result := DiscoveryResult{}
		// Set when any background-discovery change row is recorded, so we can
		// publish a single live-update event for the Models nav badge.
		changesRecorded := false
		ctx := context.Background()
		providers, err := providerRepo.List(ctx)
		if err != nil {
			debuglog.Error("discovery: failed to list providers", "error", err)
			result.Errors = append(result.Errors, fmt.Sprintf("failed to list providers: %v", err))
			return result
		}
		discoverySvc := provider.NewDiscoveryService(sd.DialContext, sd.CheckRedirect)
		for _, p := range providers {
			if !p.Enabled {
				continue
			}
			result.ProvidersScanned++
			models, err := discoverySvc.DiscoverModels(ctx, p, cfg.MasterKey)
			if err != nil {
				debuglog.Error("discovery: failed for provider", "provider", p.Name, "error", err)
				result.ProvidersFailed++
				result.Errors = append(result.Errors, fmt.Sprintf("provider %s: %v", p.Name, err))
				// Update last_discovered_at even on failure so the UI reflects
				// that discovery was *attempted*. Without this, a chronically
				// failing provider shows a stale "Last discovered" timestamp
				// that makes the scheduled timer appear broken.
				now := time.Now()
				if _, err := database.Pool().Exec(ctx, `UPDATE providers SET last_discovered_at = $1 WHERE id = $2`, now, p.ID); err != nil {
					debuglog.Error("discovery: failed to update last_discovered_at", "provider", p.Name, "error", err)
				}
				continue
			}

			// Enrich models with data from models.dev.
			if cache := provider.GetModelsDevCache(); cache != nil {
				if enriched := cache.EnrichModels(models); enriched > 0 {
					debuglog.Info("discovery: enriched models from models.dev", "enriched", enriched, "total", len(models), "provider", p.Name)
				}
			}
			result.ModelsDiscovered += len(models)

			// Snapshot pre-scan state so background metadata/membership changes
			// can be recorded for the Models nav badge. A snapshot failure only
			// disables the diff for this provider; the scan itself proceeds.
			snapshot, snapErr := api.SnapshotProviderModels(ctx, modelRepo, p.ID)
			if snapErr != nil {
				debuglog.Debug("discovery: failed to snapshot models", "provider", p.Name, "error", snapErr)
			}
			api.DampenOpenRouterPriceJitter(p.BaseURL, snapshot, models)

			existingModelIDs := make([]string, 0, len(models))
			upsertedModels := make([]*model.Model, 0, len(models))
			for _, m := range models {
				if err := modelRepo.Upsert(ctx, m); err != nil {
					debuglog.Error("discovery: failed to upsert model", "model_id", m.ModelID, "error", err)
				} else {
					existingModelIDs = append(existingModelIDs, m.ModelID)
					upsertedModels = append(upsertedModels, m)
				}
			}
			disabledRefs, err := modelRepo.DisableMissingModels(ctx, p.ID, p.Name, existingModelIDs)
			if err != nil {
				debuglog.Error("discovery: failed to disable missing models", "provider", p.Name, "error", err)
			} else if len(disabledRefs) > 0 {
				result.ModelsDisabled += len(disabledRefs)
				events.Publish(events.Event{
					Type:     "discovery.models_disabled",
					Severity: "warning",
					Message:  fmt.Sprintf("%d models no longer available at '%s' and were disabled", len(disabledRefs), p.Name),
					Metadata: map[string]interface{}{"provider": p.Name, "count": len(disabledRefs)},
				})
			}

			// Record this provider's model-level diff (added/reenabled/disabled/
			// metadata-updated) for later review. Failover group churn is folded
			// in once after the global failover sync below.
			if snapErr == nil {
				diff := api.BuildDiscoveryDiff(snapshot, upsertedModels, disabledRefs)
				if wrote, err := api.AppendDiscoveryChange(ctx, database.Pool(), source, &p.ID, p.Name, diff); err != nil {
					debuglog.Error("discovery: failed to record changes", "provider", p.Name, "error", err)
				} else if wrote {
					changesRecorded = true
				}
			}

			now := time.Now()
			if _, err := database.Pool().Exec(ctx, `UPDATE providers SET last_discovered_at = $1 WHERE id = $2`, now, p.ID); err != nil {
				debuglog.Error("discovery: failed to update last_discovered_at", "provider", p.Name, "error", err)
			}
			debuglog.Info("discovery: discovered models", "count", len(models), "provider", p.Name)
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
		failoverDiff := &api.DiscoveryDiff{}
		for modelID := range seenModelIDs {
			syncRes, err := failoverRepo.SyncForModel(ctx, modelID)
			if err != nil {
				debuglog.Error("discovery: failed to sync failover", "model_id", modelID, "error", err)
				result.FailoverSyncErrs++
				events.Publish(events.Event{
					Type:     "failover.sync_error",
					Severity: "warning",
					Message:  fmt.Sprintf("Failover sync failed for model '%s'", modelID),
					Metadata: map[string]interface{}{"error": err.Error(), "model_id": modelID},
				})
				continue
			}
			if syncRes != nil {
				failoverDiff.FailoverDeletedGroups = append(failoverDiff.FailoverDeletedGroups, syncRes.DeletedGroups...)
				failoverDiff.FailoverUpdatedGroups = append(failoverDiff.FailoverUpdatedGroups, syncRes.UpdatedGroups...)
			}
		}
		// SyncForModel only rebuilds auto-groups; custom groups whose member was
		// just disabled (not deleted) keep their stale size. Revalidate once per
		// cycle so background discovery auto-disables any custom group left with
		// fewer than two routable members — the headless path the manual Sync and
		// interactive discover already cover.
		if revRes, err := failoverRepo.RevalidateCustomGroups(ctx); err != nil {
			debuglog.Error("discovery: failed to revalidate custom failover groups", "error", err)
		} else if revRes != nil {
			failoverDiff.FailoverDisabledGroups = append(failoverDiff.FailoverDisabledGroups, revRes.DisabledGroups...)
		}
		// Record failover group churn as one aggregate entry (the global sync is
		// not per-provider). An empty provider_name flags this to the frontend as
		// the run-wide failover entry, which it labels accordingly.
		if wrote, err := api.AppendDiscoveryChange(ctx, database.Pool(), source, nil, "", failoverDiff); err != nil {
			debuglog.Error("discovery: failed to record failover changes", "error", err)
		} else if wrote {
			changesRecorded = true
		}

		// One live-update nudge so the Models nav badge refreshes without a
		// reload; the badge query re-fetches the authoritative count.
		if changesRecorded {
			events.Publish(events.Event{
				Type:     "discovery.changes_pending",
				Severity: "info",
				Source:   "discovery",
				Message:  "Background discovery recorded model changes",
				Metadata: map[string]interface{}{"source": source},
			})
		}
		return result
	}

	// Load models.dev catalogue synchronously before startup discovery so
	// enrichment data is available for the first discovery run. Uses a short
	// timeout so a slow/unreachable API doesn't block startup for long.
	if cfg.ModelsDevEnabled {
		loadCtx, loadCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer loadCancel()
		if err := provider.LoadModelsDev(loadCtx); err != nil {
			debuglog.Warn("modelsdev: failed to load catalogue", "error", err)
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
			debuglog.Info("discovery: skipping startup — last discovery within 5 minutes")
		} else {
			go func() {
				result := runDiscovery("startup")
				publishDiscoveryEvent("Startup", result)
			}()
		}
	}

	// Pre-warm caches synchronously before accepting connections.
	// Provider, model, and failover lookups are fast (simple SELECT queries),
	// but key warming (Argon2id) can take ~150ms per provider. The total
	// warm-up cost is typically under 1s for a handful of providers —
	// far better than letting the first request pay the cold-cache penalty
	// of ~170ms+ in failover + model + provider + key decryption DB queries.
	{
		ctx := context.Background()

		providers, err := providerRepo.List(ctx)
		if err != nil {
			debuglog.Error("cache: warm failed to list providers", "error", err)
		} else {
			enabledProviders := make([]*provider.Provider, 0, len(providers))
			for _, p := range providers {
				if !p.Enabled || !p.AutodiscoveryEnabled {
					continue
				}
				if len(p.EncryptedKey) > 0 {
					auth.WarmKeyCache(p.EncryptedKey, p.KeyNonce, p.KeySalt, cfg.MasterKey)
				}
				enabledProviders = append(enabledProviders, p)
			}
			provider.WarmProviderCache(enabledProviders)
		}

		enabledModels, err := modelRepo.ListEnabled(ctx)
		if err != nil {
			debuglog.Error("cache: warm failed to list models", "error", err)
		} else {
			model.WarmModelCache(enabledModels)
		}

		failoverGroups, err := failoverRepo.List(ctx)
		if err != nil {
			debuglog.Error("cache: warm failed to list failover groups", "error", err)
		} else {
			failover.WarmFailoverCache(failoverGroups)
		}

		settingsRepo.WarmCache(ctx)

		debuglog.Info("cache: key, provider, model, failover, and settings caches warmed")
	}

	// Initialize key cache TTL from settings and react to changes.
	auth.SetKeyCacheTTL(settingsRepo.GetDuration(context.Background(), "key_cache_ttl", auth.DefaultKeyCacheTTL))
	settingsRepo.RegisterOnChange(func(key, value string) {
		if key == "key_cache_ttl" {
			d, err := time.ParseDuration(value)
			if err != nil || d <= 0 {
				debuglog.Warn("keycache: invalid key_cache_ttl setting, keeping current value", "value", value, "error", err)
				return
			}
			auth.SetKeyCacheTTL(d)
			debuglog.Info("keycache: TTL updated", "ttl", d)
		}
	})

	// Periodic discovery based on settings interval.
	// Sleep before the first run so we don't bypass the discovery_on_startup setting.
	// When discovery_on_startup is true, the startup go runDiscovery() above already
	// handles immediate discovery; when false, we must not discover on startup either.
	//
	// TODO: Extract the three inline goroutines below (discovery loop, stale log
	// cleanup, log retention) into internal/scheduler/ for maintainability. main.go
	// is ~800 lines; each goroutine body should be a named function in its own file.
	//
	// Bug fixes applied:
	//   1. Timer now reacts immediately to discovery_interval changes via the
	//      settings subscription channel, instead of waiting for the current
	//      timer to expire.
	//   2. An interval of 0 ("Disabled") now truly disables periodic discovery
	//      rather than resetting to 1 minute. The goroutine blocks on the
	//      subscription channel until a non-zero value arrives.
	go func() {
		const defaultInterval = 6 * time.Hour

		readInterval := func() time.Duration {
			return settingsRepo.GetDuration(context.Background(), "discovery_interval", defaultInterval)
		}

		settingsSub := settingsRepo.Subscribe()
		defer settingsSub.Unsubscribe()

		// applyInterval sets up the timer for the given interval. If the
		// interval is <= 0 (disabled) the timer is stopped and set to nil.
		// A nil timer channel blocks forever in select, which is exactly what
		// we want for the disabled state — only the settings subscription
		// and the context cancellation can wake us.
		var timer *time.Timer
		var timerC <-chan time.Time
		applyInterval := func(d time.Duration) {
			if d <= 0 {
				// Transitioning to disabled: stop and drain the existing timer.
				if timer != nil {
					timer.Stop()
					// Drain the channel if the timer already fired, so the
					// receive does not leak into the next cycle.
					select {
					case <-timer.C:
					default:
					}
					timer = nil
					timerC = nil
				}
			} else {
				// Transitioning to enabled: reset (or create) the timer.
				if timer != nil {
					timer.Stop()
					select {
					case <-timer.C:
					default:
					}
					timer.Reset(d)
				} else {
					timer = time.NewTimer(d)
					// NewTimer already starts with duration d; no Reset needed.
				}
				timerC = timer.C
			}
		}

		interval := readInterval()
		applyInterval(interval)

		defer func() {
			if timer != nil {
				timer.Stop()
			}
		}()

		for {
			if interval <= 0 {
				// Discovery is disabled. Block until the setting changes
				// or the server shuts down. We cannot reach the main
				// select because timerC is nil (blocks forever).
				select {
				case <-settingsSub.Events():
					interval = readInterval()
					applyInterval(interval)
				case <-ctx.Done():
					return
				}
				continue
			}

			select {
			case <-timerC:
				result := runDiscovery("scheduled")
				publishDiscoveryEvent("Scheduled", result)
				// Re-read interval in case it changed since the last
				// subscription event was processed.
				interval = readInterval()
				applyInterval(interval)

			case <-settingsSub.Events():
				// Re-read from DB (the source of truth) rather than
				// parsing the event value, which may be empty if the
				// setting was deleted or the lookup failed.
				newInterval := readInterval()
				if newInterval != interval {
					interval = newInterval
					applyInterval(interval)
				}

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
				SET state = 'failed', error_kind = 'internal', error_message = 'request interrupted (stale)'
				WHERE state IN ('pending', 'streaming')
				  AND (created_at < $1 OR created_at < NOW() - $2::interval)`,
				serverStartTime, intervalStr)
			if err == nil && tag.RowsAffected() > 0 {
				debuglog.Info("retention: stale log cleanup", "rows", tag.RowsAffected())
				events.Publish(events.Event{
					Type:     "logs.stale_cleanup",
					Severity: "warning",
					Message:  fmt.Sprintf("Marked %d stale requests as interrupted", tag.RowsAffected()),
					Metadata: map[string]interface{}{"count": tag.RowsAffected()},
				})
			} else if err != nil {
				debuglog.Error("retention: stale log cleanup failed", "error", err)
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
			case "1d", "24h":
				cutoff = time.Now().Add(-24 * time.Hour)
			case "1w", "168h":
				cutoff = time.Now().Add(-7 * 24 * time.Hour)
			case "1m", "720h":
				cutoff = time.Now().Add(-30 * 24 * time.Hour)
			default:
				// Unrecognised value (including "0" for disabled): skip
				// cleanup this cycle and re-check next hour.
				timer.Reset(1 * time.Hour)
				continue
			}
			tag, err := database.Pool().Exec(context.Background(),
				`DELETE FROM request_logs WHERE created_at < $1`, cutoff)
			if err == nil {
				debuglog.Info("retention: log retention deleted old entries", "retention", retention, "rows", tag.RowsAffected())
			}
			// Clean app_logs with same retention
			tag, err = database.Pool().Exec(context.Background(),
				`DELETE FROM app_logs WHERE created_at < $1`, cutoff)
			if err == nil {
				debuglog.Info("retention: app log retention deleted old entries", "retention", retention, "rows", tag.RowsAffected())
			}
			timer.Reset(1 * time.Hour)
		}
	}()

	server := &http.Server{
		Addr:              cfg.Port,
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		// Startup banner (direct stdout: slog escapes \n, making ASCII art
		// unreadable). Printed as the very last startup output so Docker
		// Compose log interleaving from other containers (e.g. postgres)
		// can't split the banner, config table, and ready message.
		printStartupBannerStdout(cfg)
		if isNew {
			printAdminTokenBoxStdout(adminMgr.Token())
		}
		printReadyMessageStdout(version)

		debuglog.Info("server: listening", "port", cfg.Port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			debuglog.Fatal("server: failed to start", "error", err)
		}
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	debuglog.Info("server: shutting down gracefully")

	// Release goroutine-leaking resources before draining HTTP connections.
	proxyHandler.Close()
	apiHandler.StopBackupScheduler()
	util.CloseDockerClient()

	// Flush and close the OTLP log exporter (if enabled) so batched records are
	// not lost on shutdown.
	if otelLogShutdown != nil {
		flushCtx, flushCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer flushCancel()
		if err := otelLogShutdown(flushCtx); err != nil {
			debuglog.Error("otel: OTLP log exporter shutdown failed", "error", err)
		}
	}

	// Flush pending app log DB writes before closing the database.
	api.StopAppLogWriter()

	shutdownCtx, shutdownCancel := context.WithTimeout(ctx, 10*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		debuglog.Error("server: error during shutdown", "error", err)
	}

	debuglog.Info("server: stopped")
}

// silentLogger is like chi's middleware.Logger but suppresses request log
// lines for high-frequency polling endpoints that would flood docker logs.
func silentLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		t1 := time.Now()
		next.ServeHTTP(ww, r)
		duration := time.Since(t1)

		path := r.URL.Path
		isStatic := strings.HasPrefix(path, "/assets/") || strings.HasPrefix(path, "/favicon")
		isNoisy := path == "/health" ||
			strings.HasPrefix(path, "/api/logs/app") ||
			(path == "/api/logs" && r.Method == "GET") ||
			(path == "/api/system" && r.Method == "GET") ||
			(path == "/api/events" && r.Method == "GET") ||
			(path == "/api/stats" && r.Method == "GET") ||
			(path == "/api/stats/timeseries" && r.Method == "GET") ||
			(path == "/api/stats/provider-distribution" && r.Method == "GET") ||
			(path == "/api/models" && r.Method == "GET") ||
			(path == "/api/providers" && r.Method == "GET")
		if isStatic && ww.Status() < 400 {
			return
		}
		status := ww.Status()
		switch {
		case status >= 500:
			debuglog.Error("access: request",
				"method", r.Method,
				"host", r.Host,
				"path", r.URL.Path,
				"remote", r.RemoteAddr,
				"status", status,
				"bytes", ww.BytesWritten(),
				"duration", duration)
		case status >= 400:
			debuglog.Warn("access: request",
				"method", r.Method,
				"host", r.Host,
				"path", r.URL.Path,
				"remote", r.RemoteAddr,
				"status", status,
				"bytes", ww.BytesWritten(),
				"duration", duration)
		case isNoisy:
			debuglog.Debug("access: request",
				"method", r.Method,
				"host", r.Host,
				"path", r.URL.Path,
				"remote", r.RemoteAddr,
				"status", status,
				"bytes", ww.BytesWritten(),
				"duration", duration)
		default:
			debuglog.Info("access: request",
				"method", r.Method,
				"host", r.Host,
				"path", r.URL.Path,
				"remote", r.RemoteAddr,
				"status", status,
				"bytes", ww.BytesWritten(),
				"duration", duration)
		}
	})
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
// isLongRunningPath reports whether the request targets a multimodal proxy
// endpoint whose legitimate latency exceeds the non-streaming deadline:
// image generation/edits and audio synthesis/transcription regularly take
// minutes. The proxy's per-attempt failover timeout and overall deadline
// still bound these requests.
func isLongRunningPath(path string) bool {
	return strings.HasPrefix(path, "/v1/images/") || strings.HasPrefix(path, "/v1/audio/")
}

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

			// Multipart uploads (audio transcription/translation, image
			// edits/variations) are never buffered here: reading megabytes
			// into memory before auth would let unauthenticated clients pin
			// large allocations, and the JSON peek cannot apply anyway (the
			// model field lives in the form, parsed by the handler after
			// auth). These routes are long-running, so no deadline either.
			if strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "multipart/") {
				if isLongRunningPath(r.URL.Path) {
					next.ServeHTTP(w, r)
					return
				}
				ctx, cancel := context.WithTimeout(r.Context(), maxNonStreamingDur)
				defer cancel()
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			parseStart := time.Now()
			body, err := io.ReadAll(r.Body)
			_ = r.Body.Close()
			if err != nil {
				util.WriteOpenAIError(w, "failed to read request body", http.StatusBadRequest)
				return
			}

			// Extract both stream and model in a single unmarshal so
			// downstream handlers can skip re-parsing cached bytes. The peek
			// runs regardless of Content-Type: clients send JSON chat bodies
			// with text/plain or form-urlencoded headers, and skipping them
			// would wrongly impose the non-streaming deadline on their streams.
			var parsed struct {
				Stream bool   `json:"stream"`
				Model  string `json:"model"`
			}
			isStreaming := false
			modelName := ""
			if json.Unmarshal(body, &parsed) == nil {
				isStreaming = parsed.Stream
				modelName = parsed.Model
			}
			parseMs := float64(time.Since(parseStart).Microseconds()) / 1000.0

			// Restore the body so downstream handlers can read it
			r.Body = io.NopCloser(bytes.NewReader(body))

			// Store body bytes + extracted fields + timing in context
			ctx := context.WithValue(r.Context(), ctxkeys.RequestBodyKey, body)
			ctx = context.WithValue(ctx, ctxkeys.RequestBodyParseMsKey, parseMs)
			ctx = context.WithValue(ctx, ctxkeys.RequestModelKey, modelName)
			ctx = context.WithValue(ctx, ctxkeys.IsStreamingKey, isStreaming)

			// Long-running multimodal routes (image generation, audio) get the
			// streaming treatment even without a body stream flag: their
			// legitimate latencies (image models, large transcriptions, SSE
			// synthesis) exceed the non-streaming deadline. The proxy's
			// per-attempt failover timeout still bounds each upstream call.
			if isStreaming || isLongRunningPath(r.URL.Path) {
				next.ServeHTTP(w, r.WithContext(ctx))
			} else {
				ctx, cancel := context.WithTimeout(ctx, maxNonStreamingDur)
				defer cancel()
				next.ServeHTTP(w, r.WithContext(ctx))
			}
		})
	}
}
