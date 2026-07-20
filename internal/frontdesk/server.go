package frontdesk

import (
	"crypto/subtle"
	"io/fs"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	gowa "github.com/go-webauthn/webauthn/webauthn"

	"github.com/hugalafutro/model-hotel/internal/admin"
	"github.com/hugalafutro/model-hotel/internal/adminauth"
	"github.com/hugalafutro/model-hotel/internal/alert"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/events"
	"github.com/hugalafutro/model-hotel/internal/totp"
	"github.com/hugalafutro/model-hotel/internal/util"
	"github.com/hugalafutro/model-hotel/internal/webauthn"
)

// This file is the Front Desk control-plane HTTP server. It exposes:
//   - GET /traefik/config: the unauthenticated, compose-internal Traefik dynamic
//     config (Traefik's HTTP provider polls this; we record each poll for the
//     staleness watchdog).
//   - /api/webauthn/* and /api/totp/*: the shared adminauth login/management
//     ceremonies (Option B parity), backed by the SQLite stores.
//   - /api/* control-plane REST (members, settings, events, traefik-status) and
//     /api/sse, all behind the admin-or-session gate.
//   - the embedded SPA at /.
//
// Front Desk is never in the data path; these endpoints only manage membership
// and surface status.

// ServerConfig carries the dependencies for a Front Desk Server.
type ServerConfig struct {
	Store        *Store
	Poller       *Poller
	Bus          *events.Bus
	AdminMgr     *admin.Manager                // FRONTDESK_TOKEN
	MasterKey    string                        // encrypts the TOTP secret at rest
	RelyingParty *gowa.WebAuthn                // WebAuthn RP (from PUBLIC_ORIGIN); nil disables passkeys
	IPLimiter    adminauth.IPLimiterMiddleware // per-IP limit on login routes
	UI           fs.FS                         // embedded SPA; nil disables the UI mount
	MetricsToken string                        // FRONTDESK_METRICS_TOKEN; bearer for /metrics scrapes (falls back to admin auth when empty)
	LBPort       string                        // host port of the LB (Traefik "web"); shown in the wizard's Done step. Defaults to 8080.
	Version      string                        // running build, stamped via ldflags; surfaced read-only over GET /api/version. Defaults to "dev".
}

// Server is the Front Desk HTTP server.
type Server struct {
	store        *Store
	poller       *Poller
	bus          *events.Bus
	adminMgr     *admin.Manager
	sessionMgr   *webauthn.SessionManager
	totpRepo     *totp.Repository
	totpStatus   *totpEnabledCache
	probe        *http.Client // guarded client for proxying member admin APIs
	readClient   *http.Client // guarded client for interactive member admin reads (e.g. Traffic timeseries); longer deadline than the health probe, shorter than the import relay
	syncClient   *http.Client // guarded client for the config-import relay (longer deadline; import runs member-side discovery)
	backupClient *http.Client // guarded client for the pre-sync backup relay (deadline exceeds the member's pg_dump budget)
	lbPort       string       // host port of the data-plane load balancer, surfaced to the wizard
	version      string       // running build, surfaced read-only over GET /api/version
	masterKey    string       // encrypts the Apprise target secret at rest
	metricsToken string       // dedicated bearer for Prometheus /metrics scrapes; empty falls back to admin auth
	alertDisp    *alert.Dispatcher
	pairing      *pairingCodes                 // one-time Bellhop pairing codes (in-memory)
	ipLimiter    adminauth.IPLimiterMiddleware // per-IP limit reused on the public /api/pair exchange
	settingsMu   sync.Mutex                    // serializes the settings-row read-merge-write
	// rearmMu guards rearmCh, the in-process rearm broadcast. rearmCh is closed (and
	// replaced) whenever a rearm/repoint bumps the auto-sync generation, so an
	// in-flight convergence pass cancels synchronously instead of waiting on a poll.
	rearmMu sync.Mutex
	rearmCh chan struct{}
	// syncHeld tracks which members autosync is currently holding for version
	// skew, so config.sync_held fires once on the transition into held rather than
	// every pass (mirrors the poller's versionFailures edge-trigger). In-memory and
	// bounded by fleet size; a restart re-emits at most once per still-held member.
	syncHeldMu sync.Mutex
	syncHeld   map[string]bool
	// fleetStatePrev is the last state checkFleetState saw, guarding the
	// edge-triggered fleet.state_changed emission. Empty until the first check
	// (treated as ok, so a fleet that starts unhealthy alerts once on startup).
	// This mirrors checkAutoSyncStale, which re-alerts on a stale start, not
	// checkConfigStaleness, which stays quiet until it has armed.
	fleetStateMu   sync.Mutex
	fleetStatePrev FleetState
	router         http.Handler
	// bgWG tracks detached background goroutines (e.g. the auto-sync kick) so
	// callers can drain them on shutdown. Without it, a kick fired by an enable
	// keeps writing to the store after a caller (or a test) has moved on, which
	// races store teardown.
	bgWG sync.WaitGroup
}

// defaultLBPort is the load-balancer host port assumed when FLEET_LB_PORT is
// unset; it mirrors the LB_PORT default in deploy/ha/.env.example.
const defaultLBPort = "8080"

// NewServer wires the control-plane HTTP server. It builds the SQLite-backed
// webauthn SessionManager and totp.Repository, seeds the TOTP-enabled cache, and
// mounts the shared adminauth handlers alongside the control-plane REST/SSE
// endpoints and the embedded UI.
func NewServer(cfg ServerConfig) *Server {
	webAuthnStore := NewWebAuthnStore(cfg.Store)
	sessionMgr := webauthn.NewSessionManager(webAuthnStore)
	totpRepo := totp.NewRepositoryWithStore(NewTOTPStore(cfg.Store), cfg.MasterKey)

	lbPort := cfg.LBPort
	if lbPort == "" {
		lbPort = defaultLBPort
	}

	version := cfg.Version
	if version == "" {
		version = "dev"
	}

	s := &Server{
		store:        cfg.Store,
		poller:       cfg.Poller,
		bus:          cfg.Bus,
		adminMgr:     cfg.AdminMgr,
		sessionMgr:   sessionMgr,
		totpRepo:     totpRepo,
		totpStatus:   newTotpEnabledCache(totpRepo),
		probe:        newProbeClient(httpProbeTimeout),
		readClient:   newProbeClient(memberReadTimeout),
		syncClient:   newProbeClient(memberSyncTimeout),
		backupClient: newProbeClient(memberBackupTimeout),
		lbPort:       lbPort,
		version:      version,
		masterKey:    cfg.MasterKey,
		metricsToken: strings.TrimSpace(cfg.MetricsToken), // whitespace-only is treated as unset, not a live bearer
		pairing:      newPairingCodes(),
		ipLimiter:    cfg.IPLimiter,
		rearmCh:      make(chan struct{}),
		syncHeld:     make(map[string]bool),
	}

	// Bind the scrape-time member-fleet collector to this server's store and
	// poller so /metrics always reflects current state (one server per process
	// in production; tests rebind freely).
	setMemberMetricsSource(s.collectMemberMetrics)

	// Outbound Apprise alerting: one consumer of the Front Desk event bus, gated by
	// the HA event catalog and the operator's picker. Built here so the settings
	// handlers can probe/test it; run as a goroutine via RunAlerts.
	s.alertDisp = alert.New(
		alertConfigProvider{store: cfg.Store, masterKey: cfg.MasterKey}, nil,
		alert.WithBus(cfg.Bus),
		alert.WithCatalog(fdCatalog),
		alert.WithTitlePrefix("Front Desk"),
		alert.WithDebounceKeys([]string{"member_id"}),
		alert.WithResultHook(recordAlertDispatch),
	)

	webauthnHandler := adminauth.NewWebAuthnHandler(
		webAuthnStore, cfg.RelyingParty, sessionMgr, cfg.AdminMgr, cfg.IPLimiter, false, s.totpStatus.Enabled, false, "auto",
	)
	// NOTE: Front Desk's own web client (frontdesk/web) still consumes the
	// TOTP login/enroll-verify session token from the JSON body (bearer
	// auth), not the HttpOnly cookie the main dashboard reads. useCookieAuth
	// is false here so the legacy token-in-body shape is preserved
	// byte-for-byte until Front Desk's frontend migrates to cookie auth in a
	// follow-up. "auto" still keeps the cookie Secure attribute sane in case
	// that migration lands, but is otherwise unused while useCookieAuth is
	// false.
	totpHandler := adminauth.NewTotpHandler(
		totpRepo, cfg.AdminMgr, sessionMgr, cfg.IPLimiter, false, s.totpStatus.Enabled, s.totpStatus.Refresh, "auto", false,
	)
	// OIDC SSO: a fourth admin-login path. The shared adminauth handler is reused
	// as-is; newOIDCSettings adapts Front Desk's typed settings row to its key/value
	// contract, and the config secret rides the same MasterKey encryption as above.
	oidcHandler := adminauth.NewOIDCHandler(newOIDCSettings(cfg.Store), sessionMgr, cfg.IPLimiter, cfg.MasterKey, false, "auto")

	s.router = s.buildRouter(webauthnHandler, totpHandler, oidcHandler, cfg.UI)
	return s
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.router.ServeHTTP(w, r) }

// Wait blocks until every detached background goroutine the server has spawned
// (currently the auto-sync kick) has returned. Use it on graceful shutdown, or
// in tests before tearing down the backing store, so a still-running kick can't
// write into a store or temp dir that is being removed.
func (s *Server) Wait() { s.bgWG.Wait() }

// SessionManager exposes the session manager (used by callers wiring background
// cleanup of expired sessions).
func (s *Server) SessionManager() *webauthn.SessionManager { return s.sessionMgr }

func (s *Server) buildRouter(wa *adminauth.WebAuthnHandler, tp *adminauth.TotpHandler, oidc *adminauth.OIDCHandler, ui fs.FS) http.Handler {
	r := chi.NewRouter()

	// Security headers. The Front Desk admin UI manages the whole HA fleet
	// (member admin tokens, device pairing, config sync, OIDC/alert settings)
	// and keeps its bearer in localStorage, so a framed copy of this origin
	// auto-authenticates. Deny framing outright and mirror the main server's
	// content-type / referrer / CSP hardening (cmd/server/main.go). Front Desk
	// is never embedded, so there is no ALLOW_EMBED escape hatch here.
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("X-Frame-Options", "DENY")
			w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
			// HSTS only over TLS. Front Desk serves plain HTTP behind a proxy
			// that terminates TLS, so this guard is a forward-compatible
			// placeholder: setting HSTS on plain HTTP would cache a broken
			// redirect to a non-existent HTTPS listener.
			if r.TLS != nil {
				w.Header().Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains; preload")
			}
			// Same-origin scripts (Vite module output, no inline scripts).
			// style 'unsafe-inline' is required for Vite's injected style tags;
			// img data:/blob: covers QR codes and canvas-rendered previews.
			w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
			next.ServeHTTP(w, r)
		})
	})

	// Unauthenticated, compose-internal: Traefik's HTTP provider polls this.
	r.Get("/traefik/config", s.handleTraefikConfig)

	// Prometheus scrape endpoint. Outside /api (matching the main server's
	// mount) and never rate-limited by IP so scrapers aren't throttled; auth
	// is FRONTDESK_METRICS_TOKEN or, without one, the admin-or-session gate.
	r.Handle("/metrics", s.metricsAuth(metricsHTTPHandler()))

	r.Route("/api", func(r chi.Router) {
		// Login + auth management ceremonies (gating handled inside the handlers:
		// login is public, register/disable require admin-or-session). The OIDC
		// routes (/auth/oidc/{status,start,callback}) are public because they ARE
		// the login flow; the email allowlist, not a bearer, gates completion.
		wa.Register(r)
		tp.Register(r)
		// OIDC is the one login path that makes outbound third-party calls
		// (discovery on fingerprint change, token exchange + UserInfo per login),
		// so bound it with a per-request timeout so a slow or hostile IdP can't
		// pin a goroutine open indefinitely. Matches the main dashboard's posture
		// (the SQLite server sets no WriteTimeout, so this is the actual cap).
		r.Group(func(r chi.Router) {
			r.Use(middleware.Timeout(60 * time.Second))
			oidc.Register(r)
		})

		// Public pairing exchange (Bellhop): validates a one-time code and mints
		// a device token. Login-like and unauthenticated, so it rides the same
		// per-IP limiter as the login ceremonies.
		r.Group(func(r chi.Router) {
			if s.ipLimiter != nil {
				r.Use(s.ipLimiter.Middleware)
			}
			r.Post("/pair", s.handlePair)
		})

		// Control-plane REST + SSE, behind the admin-or-session-or-device gate.
		// Three tiers: reads (any bearer incl. monitor devices), the whitelisted
		// mutations (operator devices and up), and admin-only administration
		// (never a device token, regardless of role).
		r.Group(func(r chi.Router) {
			r.Use(s.requireAuth)
			r.Get("/members", s.listMembers)
			r.Get("/members/{id}/traffic", s.memberTraffic)
			r.Get("/version", s.getVersion)
			r.Get("/observability", s.getObservability)
			r.Get("/alert/events", s.alertEvents)
			r.Get("/alert/status", s.alertStatus)
			r.Get("/alert/selection", s.getAlertSelection)
			r.Get("/events", s.listEvents)
			r.Get("/traefik-status", s.traefikStatus)
			r.Get("/fleet/status", s.fleetStatus)
			r.Get("/fleet/last-sync", s.fleetLastSync)
			r.Get("/fleet/autosync", s.getAutoSync)
			r.Post("/logout", s.logout)
			r.Get("/sse", s.sse)
			r.Delete("/devices/self", s.revokeSelf)

			r.Group(func(r chi.Router) {
				r.Use(s.requireOperator)
				r.Post("/members/{id}/state", s.setMemberState)
				r.Put("/fleet/autosync", s.putAutoSync)
				r.Post("/config/sync", s.configSync)
				r.Post("/fleet/version-check", s.fleetVersionCheck)
				r.Post("/alert/selection", s.putAlertSelection)
			})

			r.Group(func(r chi.Router) {
				r.Use(s.requireAdmin)
				r.Post("/members", s.createMember)
				r.Patch("/members/{id}", s.patchMember)
				r.Delete("/members/{id}", s.deleteMember)
				r.Get("/settings", s.getSettings)
				r.Put("/settings", s.putSettings)
				r.Post("/alert/test", s.alertTest)
				r.Post("/pair/start", s.pairStart)
				r.Post("/pair/status", s.pairStatus)
				r.Get("/devices", s.listDevices)
				r.Delete("/devices/{id}", s.revokeDevice)
			})
		})
	})

	if ui != nil {
		r.Handle("/*", spaHandler(ui))
	}
	return r
}

// metricsAuth gates the Prometheus scrape endpoint, mirroring the main
// server's metricsAuth. A dedicated FRONTDESK_METRICS_TOKEN (so the scrape
// config need not hold the admin token) takes precedence; without one, the
// standard admin-or-session auth applies. The token must be presented as an
// Authorization: Bearer header — not a query parameter — so it does not leak
// into reverse-proxy access logs, browser history, or referrers. The endpoint
// is never served unauthenticated.
func (s *Server) metricsAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.metricsToken != "" {
			tok, ok := util.ParseBearerToken(r)
			if subtle.ConstantTimeCompare([]byte(tok), []byte(s.metricsToken)) == 1 {
				next.ServeHTTP(w, r)
				return
			}
			if !ok || tok == "" {
				debuglog.Warn("frontdesk: metrics scrape missing bearer token", "remote_addr", r.RemoteAddr)
			} else {
				debuglog.Warn("frontdesk: metrics scrape with invalid token", "remote_addr", r.RemoteAddr)
			}
			http.Error(w, "invalid metrics token", http.StatusUnauthorized)
			return
		}
		// No dedicated token configured — fall back to admin auth.
		s.requireAuth(next).ServeHTTP(w, r)
	})
}

// logout revokes the caller's server-side session so a manual or idle auto-logout
// drops the session everywhere, not just in the calling browser tab. The bearer
// is either a passkey/TOTP session token (revoked here) or the raw FRONTDESK_TOKEN
// (no session row, so RevokeAuthToken is a harmless no-op). Always returns 200;
// the client clears its local token regardless.
func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if token, ok := util.ParseBearerToken(r); ok {
		s.sessionMgr.RevokeAuthToken(r.Context(), token)
	}
	writeJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// ---------------------------------------------------------------------------
// Traefik config (unauthenticated, compose-internal)
// ---------------------------------------------------------------------------

func (s *Server) handleTraefikConfig(w http.ResponseWriter, r *http.Request) {
	s.poller.RecordConfigPoll()

	members, err := s.store.ListMembers(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	set, err := s.store.GetSettings(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, BuildTraefikConfig(members, set))
}
