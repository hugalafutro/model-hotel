package main

// Package main is the entry point for the model-hotel LLM gateway server.

import (
	"context"
	"embed"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/admin"
	"github.com/hugalafutro/model-hotel/internal/adminauth"
	"github.com/hugalafutro/model-hotel/internal/alert"
	"github.com/hugalafutro/model-hotel/internal/api"
	"github.com/hugalafutro/model-hotel/internal/audit"
	"github.com/hugalafutro/model-hotel/internal/authcookie"
	"github.com/hugalafutro/model-hotel/internal/clientip"
	"github.com/hugalafutro/model-hotel/internal/config"
	"github.com/hugalafutro/model-hotel/internal/db"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/events"
	"github.com/hugalafutro/model-hotel/internal/failover"
	"github.com/hugalafutro/model-hotel/internal/httpx"
	"github.com/hugalafutro/model-hotel/internal/model"
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

func main() {
	// Initialise the structured logger before anything can log, so the
	// warnings config.Load emits (weak MASTER_KEY, ignored env values) come out
	// in the configured format instead of slog's text default. Init reads
	// DEBUG_LOG (and DEBUG_LOG_SCOPES, LOG_FORMAT) from the environment, so
	// the .env file has to be in the environment first.
	if err := config.LoadEnvFile(); err != nil {
		log.Fatalf("Failed to load .env: %v", err)
	}
	debuglog.Init(false)

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize the admin manager before the DB connection: it only needs the
	// data directory and env token, no database.
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
	// Rows written before provider_type existed, or restored from an older
	// dump, carry no type. Give them the one the legacy URL rules imply before
	// anything reads them.
	if backfilled, err := providerRepo.BackfillTypes(ctx); err != nil {
		debuglog.Error("startup: provider type backfill failed", "error", err)
	} else if backfilled > 0 {
		debuglog.Info("startup: provider types backfilled", "count", backfilled)
	}
	// Masks written with the older two-character tail are rewritten with the
	// current shape, so a rotated key is visibly different on the card.
	if backfilled, err := providerRepo.BackfillMaskedKeys(ctx, cfg.MasterKey); err != nil {
		debuglog.Error("startup: provider mask backfill failed", "error", err)
	} else if backfilled > 0 {
		debuglog.Info("startup: provider key masks backfilled", "count", backfilled)
	}
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
	// Resolve the client IP (trusted-proxy aware) once, before anything that
	// logs an address: the access logger, auth warnings, and the audit trail all
	// read the resolved value via clientip.From.
	r.Use(clientip.Middleware(cfg.TrustedProxies))
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
	apiHandler.SetCapLedger(proxyHandler.CapLedger())

	// Quota advisor: feeds per-provider quota reset deadlines to the circuit
	// breaker so an open circuit's cooldown can be pinned to the real reset
	// time instead of the default cooldown. Wired before the poll loop below
	// starts so the first poll populates a live advisor.
	quotaAdvisor := api.NewQuotaAdvisor()
	apiHandler.SetQuotaAdvisor(quotaAdvisor)
	proxyHandler.CircuitBreaker().SetQuotaAdvisor(quotaAdvisor)

	// A circuit that opens on a spent quota window needs the reading that pins
	// its cooldown now, not on the next poll pass up to an interval away, so the
	// open transition triggers a debounced single-provider poll.
	proxyHandler.CircuitBreaker().SetOnOpen(apiHandler.NudgeQuotaPoll)

	// Outbound alerting: a single consumer of the events bus that forwards
	// operator-selected events to a stateless apprise-api container.
	// Best-effort, so a missing or failing apprise-api never affects request
	// serving. Runs for the app lifetime (ctx), reading config live so toggles
	// apply without a restart.
	alertDispatcher := alert.New(alert.NewSettingsConfigProvider(settingsRepo, cfg.MasterKey), nil)
	go alertDispatcher.Run(ctx)

	// Prometheus metrics at the conventional /metrics path (root, no IP rate
	// limiter so scrapers are not throttled). Authenticated via METRICS_TOKEN or
	// the admin token, never unauthenticated.
	r.Handle("/metrics", apiHandler.MetricsHandler())

	// WebAuthn/FIDO2 passkey authentication (enabled when WEBAUTHN_RP_ID is set).
	// The session manager sits outside the WebAuthnRPID block so it is always
	// available: TOTP /totp/login reuses CreateAuthToken to mint session tokens
	// once 2FA is enabled, even when passkeys (RP) are not configured.
	var webauthnHandler *adminauth.WebAuthnHandler
	webauthnRepo := webauthn.NewRepository(database.Pool())
	sessionMgr := webauthn.NewSessionManager(webauthnRepo)
	apiHandler.SetWebAuthnSessionManager(sessionMgr)
	// The admin-token exchange stores device metadata on the session it mints;
	// the IP limiter is the trusted-proxy-aware resolver for the client IP.
	apiHandler.SetClientIPSource(ipLimiter)

	// Multi-user accounts: the store resolves session user-handles into
	// role+grant identities in the auth middleware; the webauthn repository
	// doubles as the session revoker for disable/delete/password-reset.
	userRepo := user.NewRepository(database.Pool())
	apiHandler.SetUserAuth(userRepo, webauthnRepo)
	// Breached-password check: new dashboard passwords are screened against the
	// Have I Been Pwned range API (k-anonymity: only a 5-char SHA-1 prefix
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
	totpHandler := adminauth.NewTotpHandler(totpRepo, adminMgr, sessionMgr, ipLimiter, cfg.DemoReadOnly, apiHandler.TotpEnabled, apiHandler.RefreshTotpEnabled, cfg.CookieSecure, true, authcookie.Dashboard)

	// OIDC single sign-on. A third front-end to the same session token minted by
	// passkey/TOTP login: after the IdP confirms an allowlisted identity it calls
	// the same CreateAuthToken, so no downstream gate changes. Config lives in
	// settings (rebuilt lazily on change), so it is always constructed; the
	// public status/start/callback endpoints no-op until oidc_enabled is set.
	oidcHandler := adminauth.NewOIDCHandler(settingsRepo, sessionMgr, ipLimiter, cfg.MasterKey, true, cfg.CookieSecure, authcookie.Dashboard)
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
		webauthnHandler = adminauth.NewWebAuthnHandler(webauthnRepo, rp, sessionMgr, adminMgr, ipLimiter, cfg.DemoReadOnly, apiHandler.TotpEnabled, true, cfg.CookieSecure, authcookie.Dashboard)

		debuglog.Info("webauthn: passkey authentication enabled", "rp_id", cfg.WebAuthnRPID)
	}

	// API routes. IP rate limiting protects admin auth from brute-force.
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

		// OIDC SSO login endpoints (status/start/callback): unauthenticated, in
		// the same group as the other login routes, since they ARE the login. The
		// timeout matches the API group's posture so a slow or hostile IdP
		// (discovery, token exchange, or UserInfo) cannot pin a goroutine open
		// indefinitely.
		r.Group(func(r chi.Router) {
			r.Use(middleware.Timeout(60 * time.Second))
			oidcHandler.Register(r)
			githubHandler.Register(r)
		})

		// Admin-token bootstrap exchange (POST /api/auth/admin-exchange): a
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
	// Started earlier it silently no-ops on a nil scheduler, so no automatic
	// (GFS) backups run whatever backup_enabled is set to.
	apiHandler.StartBackupScheduler(context.Background())

	// Admin chat routes: an admin-authenticated proxy for the Chat/Arena UI,
	// with the streaming-aware timeout (as on /v1) and per-IP rate limiting.
	// ChatUserContextMiddleware runs after the grant check because it reads the
	// identity AuthMiddleware resolved, and before RegisterAdminChat because the
	// consumers of what it publishes (the proxy's candidate filter and both rate
	// limiters) are mounted in there. Without it the chat surface carries
	// neither the caller's provider cap nor their per-user rate limits.
	r.Route("/api/chat", func(r chi.Router) {
		r.Use(ipLimiter.Middleware)
		r.Use(apiHandler.AuthMiddleware)
		r.Use(apiHandler.RequireGrant(user.GrantChat))
		r.Use(apiHandler.ChatUserContextMiddleware)
		r.Use(streamingAwareTimeout(5 * time.Minute))
		proxyHandler.RegisterAdminChat(r)
	})

	// Proxy routes. Streaming LLM requests can take many minutes, so there is no
	// blanket timeout here, only a streaming-aware middleware:
	//   - streaming requests: no deadline (client-disconnect detection still works)
	//   - non-streaming requests: 5-minute deadline
	// It peeks at the body to decide, which buffers the body, so it is handed to
	// Register to mount AFTER the virtual-key check (see mountProxyRoutes).
	r.Route("/v1", func(r chi.Router) {
		mountProxyRoutes(r, proxyHandler.Register)
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
		settingsRepo: settingsRepo,
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
	go proxy.PhraseStalenessLoop(ctx, database.Pool())
	go logRetentionLoop(ctx, database.Pool(), settingsRepo)
	go quotaPollLoop(ctx, settingsRepo, apiHandler.PollQuotasOnce, apiHandler.DisableQuotaAdvice, time.Minute)
	go scheduledDisableLoop(ctx, providerRepo, failoverRepo, time.Minute)

	// Listener posture (header/idle timeouts, per-request body deadline) is
	// decided once in httpx.NewServer, shared with Front Desk.
	server := httpx.NewServer(cfg.Port, r, cfg.MaxRequestSize)

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

	// End every open /api/events SSE stream. Each one is an in-flight request
	// that server.Shutdown would otherwise wait on until the deadline, so one
	// open dashboard tab costs a restart the full 10s.
	events.DefaultBus.Close()

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

	// Flush and close the OTLP log exporter (if enabled) last, so the shutdown
	// records above are exported too. Fresh context: the HTTP drain may have
	// used up shutdownCtx.
	if otelLogShutdown != nil {
		flushCtx, flushCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer flushCancel()
		if err := otelLogShutdown(flushCtx); err != nil {
			debuglog.Error("otel: OTLP log exporter shutdown failed", "error", err)
		}
	}
}
