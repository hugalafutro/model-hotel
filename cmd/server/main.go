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
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hugalafutro/model-hotel/internal/admin"
	"github.com/hugalafutro/model-hotel/internal/adminauth"
	"github.com/hugalafutro/model-hotel/internal/alert"
	"github.com/hugalafutro/model-hotel/internal/api"
	"github.com/hugalafutro/model-hotel/internal/audit"
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
	"github.com/hugalafutro/model-hotel/internal/pwned"
	"github.com/hugalafutro/model-hotel/internal/ratelimit"
	"github.com/hugalafutro/model-hotel/internal/settings"
	"github.com/hugalafutro/model-hotel/internal/totp"
	"github.com/hugalafutro/model-hotel/internal/user"
	"github.com/hugalafutro/model-hotel/internal/util"
	"github.com/hugalafutro/model-hotel/internal/virtualkey"
	"github.com/hugalafutro/model-hotel/internal/webauthn"
)

// version is set at build time via -ldflags -X main.version=...
var version = "dev"

//go:embed all:static
var staticFiles embed.FS

// initAppLogging routes slog output through the app log pipeline so debuglog
// calls reach the ring buffer and database (not just os.Stdout). When OTLP log
// export is enabled (OTEL_EXPORTER_OTLP_ENDPOINT), fan out to it too so the
// same structured records are pushed to an OpenTelemetry collector. Returns
// the OTLP shutdown hook, nil when export is not enabled.
func initAppLogging(ctx context.Context) func(context.Context) error {
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
	return otelLogShutdown
}

// cleanupInterruptedRequests marks request logs left in "pending" or
// "streaming" state from a previous server crash, restart, or unhandled error
// as failed. Using serverStartTime (captured before DB was ready) means we
// only reclaim rows that predate this process — a genuine streaming request
// that happens to be long-running is never touched.
func cleanupInterruptedRequests(pool *pgxpool.Pool, serverStartTime time.Time) {
	tag, err := pool.Exec(context.Background(), `
		UPDATE request_logs
		SET state = 'failed', error_kind = 'internal', error_message = 'request interrupted (server restart)'
		WHERE state IN ('pending', 'streaming')
		  AND created_at < $1`, serverStartTime)
	if err == nil && tag.RowsAffected() > 0 {
		debuglog.Info("startup: stale log cleanup", "rows", tag.RowsAffected())
		events.Publish(events.Event{
			Type:     "logs.stale_startup",
			Severity: "warning",
			Message:  fmt.Sprintf("Server restart interrupted %d pending %s", tag.RowsAffected(), util.Plural(int(tag.RowsAffected()), "request", "requests")),
			Metadata: map[string]any{"count": tag.RowsAffected()},
		})
	} else if err != nil {
		debuglog.Error("startup: stale log cleanup failed", "error", err)
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

	otelLogShutdown := initAppLogging(ctx)

	debuglog.Info("db: Database connected and migrations applied successfully")
	debuglog.Info("startup: admin token", "source", func() string {
		if isNew {
			return "generated"
		}
		return "loaded from file"
	}())
	debuglog.Info("config: Config loaded")

	serverStartTime := time.Now()
	cleanupInterruptedRequests(database.Pool(), serverStartTime)

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

	r.Use(securityHeadersMiddleware(cfg))
	r.Use(corsMiddleware(cfg))
	r.Use(maxRequestSizeMiddleware(cfg.MaxRequestSize))

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
	var webauthnHandler *adminauth.WebAuthnHandler
	webauthnRepo := webauthn.NewRepository(database.Pool())
	sessionMgr := webauthn.NewSessionManager(webauthnRepo)
	apiHandler.SetWebAuthnSessionManager(sessionMgr)

	// Multi-user accounts: the store resolves session user-handles into
	// role+grant identities in the auth middleware; the webauthn repository
	// doubles as the session revoker for disable/delete/password-reset.
	userRepo := user.NewRepository(database.Pool())
	apiHandler.SetUserAuth(userRepo, webauthnRepo)
	// Breached-password check: new dashboard passwords are screened against the
	// Have I Been Pwned range API (k-anonymity — only a 5-char SHA-1 prefix ever
	// leaves the process) unless disabled via the env kill-switch or DB toggle.
	// It fails open, so an unreachable endpoint never blocks a password change;
	// PWNED_PASSWORD_API_URL can point at a self-hosted mirror for offline or
	// egress-restricted deployments.
	apiHandler.SetPwnedChecker(pwned.New(cfg.PwnedPasswordAPIURL, nil))
	// Per-user TOTP second factor: the factory binds the shared crypto/policy
	// repository to one user's rows (user_totp tables), so login enforcement
	// and the self-service endpoints reuse the admin TOTP machinery verbatim.
	userTotpFactory := func(id uuid.UUID) *totp.Repository {
		return totp.NewRepositoryWithStore(totp.NewUserPostgresStore(database.Pool(), id), cfg.MasterKey)
	}
	userLoginHandler := adminauth.NewUserLoginHandler(userRepo, sessionMgr, ipLimiter, userTotpFactory, cfg.CookieSecure)
	apiHandler.SetUserTotp(userTotpFactory)

	// Audit trail of admin actions: middleware-recorded mutating requests on
	// the dashboard API, pruned against the audit_retention_days setting
	// (read per sweep, so changes apply without a restart).
	auditRecorder := audit.New(database.Pool(), func() int {
		return settingsRepo.GetInt(context.Background(), "audit_retention_days", audit.DefaultRetentionDays)
	})
	apiHandler.SetAudit(auditRecorder)

	go webauthnSessionCleanupLoop(webauthnRepo)

	// TOTP (RFC 6238) second-factor. Always constructed so the public status
	// and login endpoints are mounted; enforcement is driven by the cached
	// IsEnabled state wired into the Handler (AuthMiddleware gate).
	totpRepo := totp.NewRepository(database.Pool(), cfg.MasterKey)
	apiHandler.SetTotpStatus(totpRepo)
	totpHandler := adminauth.NewTotpHandler(totpRepo, adminMgr, sessionMgr, ipLimiter, cfg.DemoReadOnly, apiHandler.TotpEnabled, apiHandler.RefreshTotpEnabled, cfg.CookieSecure, true)

	// OIDC single sign-on. A third front-end to the same session token minted by
	// passkey/TOTP login: after the IdP confirms an allowlisted identity it calls
	// the same CreateAuthToken, so no downstream gate changes. Config lives in
	// settings (rebuilt lazily on change), so it is always constructed; the
	// public status/start/callback endpoints no-op until oidc_enabled is set.
	oidcHandler := adminauth.NewOIDCHandler(settingsRepo, sessionMgr, ipLimiter, cfg.MasterKey, true, cfg.CookieSecure)
	oidcHandler.SetUserResolver(userRepo)

	// GitHub SSO is a fourth admin-login front-end, alongside OIDC/passkey/TOTP.
	// GitHub is OAuth2 only (no ID token/discovery), so it has its own handler
	// that confirms an allowlisted *verified* email via the REST API and then
	// mints the same CreateAuthToken session. Always constructed; the public
	// endpoints no-op until github_sso_enabled is set.
	githubHandler := adminauth.NewGitHubHandler(settingsRepo, sessionMgr, ipLimiter, cfg.MasterKey, cfg.CookieSecure)
	githubHandler.SetUserResolver(userRepo)

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
		webauthnHandler = adminauth.NewWebAuthnHandler(webauthnRepo, rp, sessionMgr, adminMgr, ipLimiter, cfg.DemoReadOnly, apiHandler.TotpEnabled, true, cfg.CookieSecure)

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

		// Multi-user password login: public status + login exchange, minting
		// the same session tokens as every other login front-end. The user
		// store is also wired into the API auth middleware so those sessions
		// resolve to role+grant identities.
		r.Group(func(r chi.Router) {
			r.Use(middleware.Timeout(60 * time.Second))
			userLoginHandler.Register(r)
		})

		// TOTP (RFC 6238) second-factor. Mounted unconditionally (not gated
		// on WebAuthnRPID): the public status/login + admin/session-gated
		// enroll/verify/disable endpoints work even without passkeys because
		// the session manager is always wired above.
		r.Group(func(r chi.Router) {
			totpHandler.Register(r)
		})

		// OIDC SSO login endpoints (status/start/callback) — unauthenticated,
		// same group as the other login routes; they ARE the login. The timeout
		// matches the API group's posture so a slow or hostile IdP (discovery,
		// token exchange, or UserInfo) can't pin a goroutine open indefinitely;
		// the per-IP limiter and request-context cancellation already help.
		r.Group(func(r chi.Router) {
			r.Use(middleware.Timeout(60 * time.Second))
			oidcHandler.Register(r)
			githubHandler.Register(r)
		})

		// Admin-token bootstrap exchange (POST /api/auth/admin-exchange) — a
		// dashboard-only login front-end that trades a valid raw admin token for
		// an HttpOnly session cookie so the browser never stores the raw token.
		// Unauthenticated (the exchange IS the login), same posture as the other
		// login groups; the per-IP limiter above still throttles brute-force.
		r.Group(func(r chi.Router) {
			r.Use(middleware.Timeout(60 * time.Second))
			apiHandler.RegisterAuthExchange(r)
		})

		r.Group(func(r chi.Router) {
			r.Use(middleware.Timeout(60 * time.Second))
			apiHandler.Register(r)
		})
	})

	// The periodic backup scheduler must start AFTER apiHandler.Register, which
	// is where the BackupHandler is constructed and wired as h.backupScheduler.
	// Started any earlier it silently no-ops on a nil scheduler, so no automatic
	// (GFS) backups ever run no matter what backup_enabled is set to.
	apiHandler.StartBackupScheduler(context.Background())

	// Admin chat routes — admin-authenticated proxy for the Chat/Arena UI.
	// Uses streaming-aware timeout (same as /v1) and rate limiting by IP.
	r.Route("/api/chat", func(r chi.Router) {
		r.Use(ipLimiter.Middleware)
		r.Use(apiHandler.AuthMiddleware)
		r.Use(apiHandler.RequireGrant(user.GrantChat))
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

	// Discovery wiring shared by the startup run and the periodic scheduler.
	discDeps := discoveryDeps{
		cfg:          cfg,
		pool:         database.Pool(),
		providerRepo: providerRepo,
		modelRepo:    modelRepo,
		failoverRepo: failoverRepo,
		dialer:       sd,
	}

	// Load models.dev catalogue synchronously before startup discovery so
	// enrichment data is available for the first discovery run. Uses a short
	// timeout so a slow/unreachable API doesn't block startup for long.
	if cfg.ModelsDevEnabled {
		loadCtx, loadCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer loadCancel()
		// Route the catalogue fetch through the SafeDialer so a redirect from
		// models.dev to a private/reserved address can't be turned into an SSRF,
		// even though the request URL itself is a fixed constant.
		modelsDevClient := &http.Client{
			Transport:     &http.Transport{DialContext: sd.DialContext},
			CheckRedirect: sd.CheckRedirect,
		}
		if err := provider.LoadModelsDevWithClient(loadCtx, modelsDevClient); err != nil {
			debuglog.Warn("modelsdev: failed to load catalogue", "error", err)
		}
	}

	// Startup: run initial discovery for all enabled providers (if enabled).
	maybeStartupDiscovery(discDeps, settingsRepo)

	warmCaches(discDeps, settingsRepo)
	initKeyCacheTTL(settingsRepo)

	// Background maintenance loops (see background.go). The discovery scheduler
	// sleeps a full interval before its first run so it doesn't bypass the
	// discovery_on_startup setting handled by maybeStartupDiscovery above.
	go discoverySchedulerLoop(ctx, settingsRepo, func(source string) DiscoveryResult {
		return runDiscovery(discDeps, source)
	})
	go staleLogCleanupLoop(ctx, database.Pool(), settingsRepo, serverStartTime)
	go logRetentionLoop(ctx, database.Pool(), settingsRepo)
	go quotaPollLoop(ctx, settingsRepo, apiHandler.PollQuotasOnce, time.Minute)

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

	// The server has stopped accepting requests, so no new audit goroutines can
	// spawn; drain the ones already in flight before the deferred database.Close
	// so their inserts are not lost.
	auditRecorder.Wait()

	debuglog.Info("server: stopped")
}

// securityHeadersMiddleware sets the standard security headers on every
// response.
func securityHeadersMiddleware(cfg *config.Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
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
	}
}

// corsMiddleware allows the configured origins (CORS_ORIGINS) and answers
// preflight requests.
func corsMiddleware(cfg *config.Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin == "" {
				next.ServeHTTP(w, r)
				return
			}

			allowed := slices.Contains(cfg.CORSOrigins, origin)

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
	}
}

// maxRequestSizeMiddleware caps every request body at maxBytes.
func maxRequestSizeMiddleware(maxBytes int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
			next.ServeHTTP(w, r)
		})
	}
}

// warmCaches pre-warms caches synchronously before accepting connections.
// Provider, model, and failover lookups are fast (simple SELECT queries),
// but key warming (Argon2id) can take ~150ms per provider. The total
// warm-up cost is typically under 1s for a handful of providers —
// far better than letting the first request pay the cold-cache penalty
// of ~170ms+ in failover + model + provider + key decryption DB queries.
func warmCaches(deps discoveryDeps, settingsRepo *settings.Repository) {
	ctx := context.Background()

	providers, err := deps.providerRepo.List(ctx)
	if err != nil {
		debuglog.Error("cache: warm failed to list providers", "error", err)
	} else {
		enabledProviders := make([]*provider.Provider, 0, len(providers))
		for _, p := range providers {
			if !p.Enabled || !p.AutodiscoveryEnabled {
				continue
			}
			if len(p.EncryptedKey) > 0 {
				auth.WarmKeyCache(p.EncryptedKey, p.KeyNonce, p.KeySalt, deps.cfg.MasterKey)
			}
			enabledProviders = append(enabledProviders, p)
		}
		provider.WarmProviderCache(enabledProviders)
	}

	enabledModels, err := deps.modelRepo.ListEnabled(ctx)
	if err != nil {
		debuglog.Error("cache: warm failed to list models", "error", err)
	} else {
		model.WarmModelCache(enabledModels)
	}

	failoverGroups, err := deps.failoverRepo.List(ctx)
	if err != nil {
		debuglog.Error("cache: warm failed to list failover groups", "error", err)
	} else {
		failover.WarmFailoverCache(failoverGroups)
	}

	settingsRepo.WarmCache(ctx)

	debuglog.Info("cache: key, provider, model, failover, and settings caches warmed")
}

// initKeyCacheTTL seeds the key cache TTL from settings and reacts to changes.
func initKeyCacheTTL(settingsRepo *settings.Repository) {
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
		// Match the noise allowlist against a slash-normalized path so a trailing
		// slash (from a client or a reverse proxy) can't defeat an exact match and
		// leak the request back to Info. Root "/" is preserved.
		np := path
		if len(np) > 1 {
			np = strings.TrimRight(np, "/")
		}
		isNoisy := np == "/health" ||
			strings.HasPrefix(np, "/api/logs/app") ||
			(np == "/api/logs" && r.Method == "GET") ||
			(np == "/api/system" && r.Method == "GET") ||
			(np == "/api/events" && r.Method == "GET") ||
			(np == "/api/stats" && r.Method == "GET") ||
			(np == "/api/stats/timeseries" && r.Method == "GET") ||
			(np == "/api/stats/provider-distribution" && r.Method == "GET") ||
			(np == "/api/models" && r.Method == "GET") ||
			(np == "/api/providers" && r.Method == "GET") ||
			// Fleet heartbeat: Front Desk pings every member ~every 2.5s with an
			// announce POST and polls its version via GET /api/settings. Both are
			// machine-to-machine liveness traffic, not human activity, and at
			// ~24/min/member they otherwise flood app_logs (the App Logs page).
			np == "/api/fleet/announce" ||
			(np == "/api/settings" && r.Method == "GET")
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
