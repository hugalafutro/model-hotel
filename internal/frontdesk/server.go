package frontdesk

import (
	"context"
	"crypto/subtle"
	"io/fs"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	gowa "github.com/go-webauthn/webauthn/webauthn"

	"github.com/hugalafutro/model-hotel/internal/admin"
	"github.com/hugalafutro/model-hotel/internal/adminauth"
	"github.com/hugalafutro/model-hotel/internal/alert"
	"github.com/hugalafutro/model-hotel/internal/authcookie"
	"github.com/hugalafutro/model-hotel/internal/clientip"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/events"
	"github.com/hugalafutro/model-hotel/internal/totp"
	"github.com/hugalafutro/model-hotel/internal/util"
	"github.com/hugalafutro/model-hotel/internal/webauthn"
)

// This file is the Front Desk control-plane HTTP server. It exposes:
//   - GET /traefik/config: the compose-internal Traefik dynamic config
//     (Traefik's HTTP provider polls this; we record each poll for the
//     staleness watchdog). Bearer-gated when FRONTDESK_TRAEFIK_TOKEN is set,
//     open otherwise.
//   - GET /healthz: the container liveness probe; unauthenticated, store-backed,
//     discloses nothing.
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
	// HealthzLimiter bounds the unauthenticated liveness probe. It is separate
	// from IPLimiter on purpose: sharing one budget would let an anonymous
	// flood of /healthz exhaust the same IP's allowance for pairing and login,
	// which turns a probe flood into a login outage. Nil leaves the probe
	// unlimited, which is the old behaviour and what the tests without a
	// limiter get.
	HealthzLimiter adminauth.IPLimiterMiddleware
	// TraefikLimiter bounds /traefik/config while it is UNGATED. That endpoint
	// is the more expensive sibling of the liveness probe: the same two store
	// reads, plus building and marshalling a member-sized config, and its body
	// discloses member URLs and settings. It gets its own budget rather than
	// the probe's, because Traefik's poll is the data plane's lifeline and a
	// flood of one endpoint must not starve the other. Per-address, so an
	// attacker's traffic cannot spend the budget Traefik itself polls on.
	// Ignored once FRONTDESK_TRAEFIK_TOKEN is set: a caller without the token
	// is then refused before any of the work happens.
	TraefikLimiter adminauth.IPLimiterMiddleware
	UI             fs.FS  // embedded SPA; nil disables the UI mount
	MetricsToken   string // FRONTDESK_METRICS_TOKEN; bearer for /metrics scrapes (falls back to admin auth when empty)
	TraefikToken   string // FRONTDESK_TRAEFIK_TOKEN; bearer Traefik's HTTP provider sends when polling /traefik/config (endpoint stays open when empty)
	LBPort         string // host port of the LB (Traefik "web"); shown in the wizard's Done step. Defaults to 8080.
	Version        string // running build, stamped via ldflags; surfaced read-only over GET /api/version. Defaults to "dev".
	// CookieSecure controls the Secure attribute on Front Desk auth cookies:
	// "always" (default), "auto", or "never" for plain-http LAN.
	CookieSecure string
	// TrustedProxies (TRUSTED_PROXIES CIDRs) gates X-Forwarded-For trust for
	// the client addresses that reach logs and session metadata.
	TrustedProxies []*net.IPNet
}

// Server is the Front Desk HTTP server.
type Server struct {
	store          *Store
	poller         *Poller
	bus            *events.Bus
	adminMgr       *admin.Manager
	sessionMgr     *webauthn.SessionManager
	totpRepo       *totp.Repository
	totpStatus     *totpEnabledCache
	probe          *http.Client // guarded client for proxying member admin APIs
	readClient     *http.Client // guarded client for interactive member admin reads (e.g. Traffic timeseries); longer deadline than the health probe, shorter than the import relay
	syncClient     *http.Client // guarded client for the config-import relay (longer deadline; import runs member-side discovery)
	backupClient   *http.Client // guarded client for a member's backup listing/delete calls (see memberBackupTimeout)
	lbPort         string       // host port of the data-plane load balancer, surfaced to the wizard
	version        string       // running build, surfaced read-only over GET /api/version
	masterKey      string       // encrypts the Apprise target secret at rest
	metricsToken   string       // dedicated bearer for Prometheus /metrics scrapes; empty falls back to admin auth
	traefikToken   string       // dedicated bearer for Traefik's /traefik/config polls; empty keeps the endpoint open (Traefik cannot log in, so admin auth is no fallback here)
	cookieSecure   string       // Secure-attribute mode for the fd_session/fd_csrf pair: "always", "auto", or "never"
	alertDisp      *alert.Dispatcher
	pairing        *pairingCodes                 // one-time Bellhop pairing codes (in-memory)
	ipLimiter      adminauth.IPLimiterMiddleware // per-IP limit reused on the public /api/pair exchange
	healthzLimiter adminauth.IPLimiterMiddleware // separate budget for the unauthenticated liveness probe
	traefikLimiter adminauth.IPLimiterMiddleware // separate budget for /traefik/config while it is ungated
	trustedProxies []*net.IPNet                  // gates XFF trust for logged/stored client addresses
	settingsMu     sync.Mutex                    // serializes the settings-row read-merge-write
	// rearmMu guards rearmCh, the in-process rearm broadcast. rearmCh is closed (and
	// replaced) whenever a rearm/repoint bumps the auto-sync generation, so an
	// in-flight convergence pass cancels synchronously instead of waiting on a poll.
	rearmMu sync.Mutex
	rearmCh chan struct{}
	// syncHeld tracks which members autosync is currently holding for version
	// skew, so config.sync_held fires once on the transition into held and
	// config.sync_recovered once on the way out rather than every pass (mirrors
	// the poller's versionFailures edge-trigger). In-memory and bounded by fleet
	// size; a restart reconciles against the event log (heldPerLog) so a hold the
	// previous process announced is still closed out, and a still-held member is
	// not re-alerted.
	syncHeldMu sync.Mutex
	// syncHeld maps a held member to the primary build its hold was judged
	// against, so a later pass can tell a hold that still means something from
	// one that has not been re-checked since the primary itself changed.
	syncHeld map[string]string
	// ungatedCommitWarned records that this process already warned that the
	// primary reports no usable commit, so the warning fires once per transition
	// rather than every pass. Guarded by syncHeldMu, alongside the hold state it
	// qualifies. See warnIfBuildGateDegraded.
	ungatedCommitWarned bool
	// holdLogChecked marks members whose persisted hold state this process has
	// already reconciled, so heldPerLog reads the event log at most once per
	// member. Guarded by syncHeldMu; in-memory and bounded by fleet size.
	holdLogChecked map[string]bool
	// syncIncomplete tracks which members took a config and when, and which of them
	// still do not serve the primary's hash. Drives the once-per-transition
	// config.sync_incomplete event and the rate-limited re-push (see
	// incompleteState). In-memory and bounded by fleet size, like syncHeld.
	syncIncompleteMu sync.Mutex
	syncIncomplete   map[string]incompleteState
	// unconfirmedSync maps a member to the primary config hash of its latest real
	// push that got no usable answer (relay deadline expired, or a 5xx that can
	// stand in front of a live import; see lostAnswer5xx) and so was never stamped as a sync, even
	// though the import may have completed member-side. The pass that later
	// measures the member holding exactly that hash stamps the last-sync marker
	// then (see measureMember); the hash binding is what keeps a member that
	// converged by any other route (a definite-failure 500 followed by an operator
	// fix, a primary that moved on) from being stamped for a push that never
	// landed. Guarded by syncIncompleteMu; in-memory and bounded by fleet size,
	// like syncIncomplete.
	unconfirmedSync map[string]string
	// backupStale tracks which members have no database backup from the last
	// memberBackupStaleAfter, so backup.stale fires once on the transition in and
	// backup.recovered once on the way out. In-memory and bounded by fleet size,
	// like syncHeld; a restart re-emits at most once per still-stale member.
	backupStaleMu sync.Mutex
	backupStale   map[string]bool
	// fleetStatePrev is the last state checkFleetState saw, guarding the
	// edge-triggered fleet.state_changed emission. Empty until the first check,
	// which seeds it from the newest persisted fleet.state_changed event
	// (lastEmittedFleetState), so a restart continues the event chain instead of
	// assuming ok: a recovery that happened while the process was down is still
	// emitted (once the inputs are warm, see fleetInputsWarm), and a degradation
	// the previous process reported is not repeated.
	fleetStateMu   sync.Mutex
	fleetStatePrev FleetState
	// autoSyncEvaluated flips once the auto-sync loop has judged the fleet this
	// process: either a convergence pass ran (rebuilding the version-skew hold
	// and incomplete-apply sets) or a tick found auto-sync disabled, in which
	// case those sets stay deliberately empty. Until then their emptiness means
	// "not looked yet", not "no holds", and fleetInputsWarm reports cold.
	autoSyncEvaluated atomic.Bool
	// startedAt anchors fleetInputsWarm's Traefik grace: with no config poll
	// recorded yet, the staleness input only counts as observed once a full
	// staleness window has passed since this process started.
	startedAt time.Time
	router    http.Handler
	// bgWG tracks every background goroutine the server owns: the detached
	// auto-sync kick, the process-lifetime poll loops, and an SSE stream's re-auth
	// ticker. Without it, work fired by a request keeps reading the store after a
	// caller (or a test) has moved on, which races store teardown.
	//
	// bgMu guards bgClosing and pairs registration with it. A sync.WaitGroup panics
	// on an Add that lifts the counter off zero while a Wait is parked, so
	// registration and the "are we draining yet" decision have to be one atomic
	// step: StartBackground holds bgMu across both, and Shutdown takes it to set
	// bgClosing before it parks. bgClosing stays set; the server is not restartable.
	bgMu      sync.Mutex
	bgClosing bool
	bgWG      sync.WaitGroup
	// shutdownCtx is cancelled at the top of Shutdown, before the drain parks.
	// Work a request starts but the server owns (the manual config sync, the
	// auto-sync kick) takes its lifetime from here, so a fleet-wide run that is
	// still going when shutdown begins stops at its next cancellation point
	// instead of outliving the drain budget and recording itself against a store
	// Shutdown has already closed. It is never cancelled anywhere else: the server
	// is not restartable, so this is a one-way end-of-life signal.
	shutdownCtx    context.Context
	shutdownCancel context.CancelFunc
}

// detachedCtx pairs one context's lifetime with another's values: Deadline, Done
// and Err come from the embedded context, Value from values. Neither standard
// combinator does both, which is why this type exists — context.WithoutCancel
// keeps a request's values but yields a context nothing can ever cancel, and a
// plain child of the server's shutdown context can be cancelled but has lost the
// request's values (the actor a manual sync is attributed to).
type detachedCtx struct {
	context.Context                 // lifetime only
	values          context.Context // values only
}

// Value answers from the request. The lifetime context carries no values, so
// there is nothing to fall through to.
func (d detachedCtx) Value(key any) any { return d.values.Value(key) }

// detachedContext returns the context for work a request starts and the server
// owns: the request's values with the server's lifetime. Dropping the request's
// cancellation is what stops a client hanging up from aborting a run half-way;
// keeping the server's is what stops that same run from writing into a store
// Shutdown has closed. Hand it to StartBackground (or StartBackgroundTimeout, to
// bound the run as well), never to a bare goroutine: the lifetime only helps if
// the drain waits for the work it ends.
func (s *Server) detachedContext(r *http.Request) context.Context {
	return detachedCtx{Context: s.shutdownCtx, values: r.Context()}
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

	// Secure-by-default: an unset knob forces Secure on rather than inferring it
	// from the request, so a deployment that never sets COOKIE_SECURE cannot
	// silently ship the session cookie over cleartext. cmd/frontdesk normalizes
	// the env value; this covers programmatic callers.
	cookieSecure := cfg.CookieSecure
	if cookieSecure == "" {
		cookieSecure = "always"
	}

	s := &Server{
		store:           cfg.Store,
		poller:          cfg.Poller,
		bus:             cfg.Bus,
		adminMgr:        cfg.AdminMgr,
		sessionMgr:      sessionMgr,
		totpRepo:        totpRepo,
		totpStatus:      newTotpEnabledCache(totpRepo),
		probe:           newProbeClient(httpProbeTimeout),
		readClient:      newProbeClient(memberReadTimeout),
		syncClient:      newProbeClient(memberSyncTimeout),
		backupClient:    newProbeClient(memberBackupTimeout),
		lbPort:          lbPort,
		version:         version,
		masterKey:       cfg.MasterKey,
		metricsToken:    strings.TrimSpace(cfg.MetricsToken), // whitespace-only is treated as unset, not a live bearer
		traefikToken:    strings.TrimSpace(cfg.TraefikToken), // whitespace-only is treated as unset, not a live bearer
		cookieSecure:    cookieSecure,
		pairing:         newPairingCodes(),
		ipLimiter:       cfg.IPLimiter,
		healthzLimiter:  cfg.HealthzLimiter,
		traefikLimiter:  cfg.TraefikLimiter,
		trustedProxies:  cfg.TrustedProxies,
		rearmCh:         make(chan struct{}),
		syncHeld:        make(map[string]string),
		holdLogChecked:  make(map[string]bool),
		syncIncomplete:  make(map[string]incompleteState),
		unconfirmedSync: make(map[string]string),
		backupStale:     make(map[string]bool),
		startedAt:       time.Now(),
	}
	// The server's own lifetime, handed to detached request work. Cancelled by
	// Shutdown and by nothing else, so a server that is never shut down simply
	// never ends it.
	//nolint:gosec // G118: the cancel func is stored on the server and called by Shutdown
	s.shutdownCtx, s.shutdownCancel = context.WithCancel(context.Background())

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

	// Every login path hands the session to the browser as the Front Desk cookie
	// pair (authcookie.FrontDesk: fd_session/fd_csrf), never in the JSON body, so
	// the SPA holds no bearer token JS can read. The jar has to be named at every
	// site: a zero-value Jar mints nothing, because net/http drops cookies with
	// an empty name.
	webauthnHandler := adminauth.NewWebAuthnHandler(
		webAuthnStore, cfg.RelyingParty, sessionMgr, cfg.AdminMgr, cfg.IPLimiter, false, s.totpStatus.Enabled, true, s.cookieSecure, authcookie.FrontDesk,
	)
	totpHandler := adminauth.NewTotpHandler(
		totpRepo, cfg.AdminMgr, sessionMgr, cfg.IPLimiter, false, s.totpStatus.Enabled, s.totpStatus.Refresh, s.cookieSecure, true, authcookie.FrontDesk,
	)
	// OIDC SSO: a fourth admin-login path. The shared adminauth handler is reused
	// as-is; newOIDCSettings adapts Front Desk's typed settings row to its key/value
	// contract, and the config secret rides the same MasterKey encryption as above.
	oidcHandler := adminauth.NewOIDCHandler(newOIDCSettings(cfg.Store), sessionMgr, cfg.IPLimiter, cfg.MasterKey, true, s.cookieSecure, authcookie.FrontDesk)

	// An open /traefik/config is a deliberate default: Traefik cannot log in, so
	// there is no admin-auth fallback to give it. What the default costs is more
	// than the member list it discloses. Serving the config stamps the poll that
	// the Traefik-stalled watchdog measures silence against, so any caller that
	// reaches the endpoint keeps that watchdog quiet, and a real Traefik that
	// died is never reported. Said once, at construction, because a default
	// whose consequence is a monitor that stops monitoring should not be silent.
	if !s.traefikGated() {
		debuglog.Warn("frontdesk: /traefik/config is unauthenticated, set FRONTDESK_TRAEFIK_TOKEN to gate it",
			"consequence", "any caller that reaches it resets the Traefik staleness watchdog")
	}

	s.router = s.buildRouter(webauthnHandler, totpHandler, oidcHandler, cfg.UI)
	return s
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.router.ServeHTTP(w, r) }

// Shutdown drains the server's background goroutines and then closes the store,
// in that order: a goroutine still mid-query would otherwise be reading a store
// that is already closed, which is the same unowned-read race a convergence pass
// avoids by joining its rearm watcher.
//
// The drain is bounded by ctx. A goroutine that ignores its own cancellation
// therefore delays exit by the caller's budget rather than hanging the process
// forever; the store is closed either way and the timed-out drain is logged, so
// the operator sees which shutdown was untidy.
//
// Work that took the server's lifetime rather than a caller's (see
// detachedContext) is cancelled here as well, before the waiter parks, so it
// stops on its own instead of racing that budget.
//
// It takes ownership of the store it was configured with and closes it, so a
// caller holding the same *Store must not keep using it afterwards.
//
// Nothing can register another goroutine while this drains: the closing flag is
// set before the waiter parks, and StartBackground refuses from that point on.
// Callers should still stop accepting requests first (the HTTP server's own
// Shutdown), so in-flight work has its chance to finish rather than being refused.
func (s *Server) Shutdown(ctx context.Context) error {
	s.bgMu.Lock()
	s.bgClosing = true
	s.bgMu.Unlock()
	// Ending the server's lifetime before the waiter parks is what keeps the drain
	// short for work that carries no deadline of its own: a manual config sync
	// pushing the fleet on a detached context stops at its next cancellation point
	// rather than spending the whole budget while the store waits to close.
	s.shutdownCancel()

	drained := make(chan struct{})
	go func() {
		defer close(drained)
		s.Wait()
	}()
	select {
	case <-drained:
		debuglog.Info("frontdesk: background goroutines drained")
	case <-ctx.Done():
		debuglog.Warn("frontdesk: background goroutines still running at shutdown; closing the store anyway",
			"error", ctx.Err())
	}
	// Closing is last and unconditional: the drain above only decides whether it
	// happens with goroutines still live. The caller logs a failure, which is all
	// that is left to do at this point in the process's life.
	debuglog.Info("frontdesk: closing store")
	return s.store.Close()
}

// SessionManager exposes the session manager for tests that need to mint or
// inspect sessions directly.
//
// It used to claim it existed for "callers wiring background cleanup of expired
// sessions". No such caller was ever written, and the cleanup that eventually
// arrived (cmd/frontdesk) builds its own WebAuthnStore over the same database
// instead, since it needs the store's cleanup method rather than the manager.
// The comment is corrected rather than the accessor deleted: the tests use it.
func (s *Server) SessionManager() *webauthn.SessionManager { return s.sessionMgr }

func (s *Server) buildRouter(wa *adminauth.WebAuthnHandler, tp *adminauth.TotpHandler, oidc *adminauth.OIDCHandler, ui fs.FS) http.Handler {
	r := chi.NewRouter()

	// Resolve the client IP (trusted-proxy aware) once, before anything that
	// logs an address, so every warn/error line reports the real client.
	r.Use(clientip.Middleware(s.trustedProxies))

	// One line per request, in the gateway's own access-log shape, immediately
	// inside the address resolution so it reports the real client. Front Desk
	// otherwise leaves no trace of a rejected request at all.
	r.Use(accessLogger)

	// Security headers. The Front Desk admin UI manages the whole HA fleet
	// (member admin tokens, device pairing, config sync, OIDC/alert settings),
	// so framing it is only ever an attack: a same-origin frame inherits the
	// session outright, and any frame invites clickjacking / UI redress on a
	// privileged console. Deny framing outright and mirror the main server's
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

	// Traefik's HTTP provider polls this. With FRONTDESK_TRAEFIK_TOKEN set,
	// only a poll carrying that bearer is served (the compose wires the same
	// value into Traefik's provider headers); without one the endpoint stays
	// open for compose-internal use, relying on the deployment boundary and
	// the reverse-proxy 404 block the HA wiki prescribes.
	//
	// Ungated it also rides a per-IP budget of its own: it is the costlier
	// sibling of the probe below (the same two store reads plus a member-sized
	// config build) and its body discloses member URLs. Not the probe's budget,
	// because Traefik's poll is what keeps the data plane routable and a flood
	// of one must not starve the other. Once the token is set an unauthenticated
	// caller is refused before any of that work, so the limiter is skipped.
	r.Group(func(r chi.Router) {
		if s.traefikLimiter != nil && !s.traefikGated() {
			r.Use(s.traefikLimiter.Middleware)
		}
		r.Get("/traefik/config", s.traefikAuth(s.handleTraefikConfig))
	})

	// Container liveness probe (the image's HEALTHCHECK). Unauthenticated on
	// purpose: it must keep answering when FRONTDESK_TRAEFIK_TOKEN gates the
	// config endpoint, so it discloses nothing and only proves the server and
	// its store answer.
	//
	// Not free, though: each hit runs the same two store reads a Traefik poll
	// does (see handleHealthz), so line-rate anonymous probing spends the
	// control plane's CPU, and the cost grows with the fleet. Its own per-IP
	// budget bounds that, separate from the login limiter's so a probe flood
	// cannot spend what pairing and login need from the same address, and far
	// above the 30-second container HEALTHCHECK cadence. Throttled rather than
	// cached because TestHealthzTracksTraefikConfigDependencies pins per-request
	// freshness: a cached answer would delay a degraded-store report.
	r.Group(func(r chi.Router) {
		if s.healthzLimiter != nil {
			r.Use(s.healthzLimiter.Middleware)
		}
		r.Get("/healthz", s.handleHealthz)
	})

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

		// Public, login-like routes: unauthenticated by design, so they ride the
		// same per-IP limiter as the login ceremonies.
		r.Group(func(r chi.Router) {
			if s.ipLimiter != nil {
				r.Use(s.ipLimiter.Middleware)
			}
			// Pairing exchange (Bellhop): validates a one-time code and mints a
			// device token.
			r.Post("/pair", s.handlePair)
			// Login front-end: trades the raw FRONTDESK_TOKEN for the HttpOnly
			// session cookie pair so the SPA never stores the raw token.
			// A bare interface conversion of a nil *IPLimiter would be a typed
			// non-nil ClientIPSource that panics on use, hence the explicit guard.
			var exchangeIPs webauthn.ClientIPSource
			if s.ipLimiter != nil {
				exchangeIPs = s.ipLimiter
			}
			r.Post("/auth/admin-exchange", adminauth.TokenExchange(
				s.adminMgr, s.sessionMgr, s.totpStatus.Enabled, authcookie.FrontDesk, s.cookieSecure,
				exchangeIPs))
			// Auth-exempt like the dashboard's logout: it must work for an
			// already-expired session, and it only revokes/clears.
			r.Post("/logout", s.logout)
		})

		// Control-plane REST + SSE, behind the admin-or-session-or-device gate.
		// Three tiers: reads (any bearer incl. monitor devices), the whitelisted
		// mutations (operator devices and up), and admin-only administration
		// (never a device token, regardless of role).
		r.Group(func(r chi.Router) {
			r.Use(s.requireAuth)
			r.Get("/members", s.listMembers)
			r.Get("/quota", s.handleQuota)
			r.Post("/quota/refresh", s.handleQuotaRefresh)
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
			r.Get("/sse", s.sse)
			r.Delete("/devices/self", s.revokeSelf)

			r.Group(func(r chi.Router) {
				r.Use(s.requireOperator)
				r.Post("/members/{id}/state", s.setMemberState)
				r.Put("/fleet/autosync", s.putAutoSync)
				r.Post("/config/sync", s.configSync)
				r.Post("/fleet/version-check", s.fleetVersionCheck)
				r.Post("/fleet/circuit-breaker/reset", s.fleetCircuitReset)
				r.Get("/fleet/failover-groups", s.fleetFailoverGroups)
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
				r.Post("/alert/probe", s.alertProbe)
				r.Get("/alert/targets", s.alertTargets)
				r.Post("/pair/start", s.pairStart)
				r.Post("/pair/status", s.pairStatus)
				r.Get("/devices", s.listDevices)
				r.Delete("/devices/{id}", s.revokeDevice)
				// Browser-session hygiene (list / revoke one / revoke others).
				// Admin-tier because a paired device is not the operator's
				// browser: sessions are none of its business.
				r.Get("/auth/sessions", s.listAuthSessions)
				r.Delete("/auth/sessions/{id}", s.revokeAuthSession)
				r.Post("/auth/sessions/revoke-others", s.revokeOtherSessions)
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
// config need not hold the admin token) takes precedence; without one, admin
// auth applies — authenticated AND not a paired device. The token must be presented as an
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
				debuglog.Warn("frontdesk: metrics scrape missing bearer token", "remote_addr", clientip.From(r))
			} else {
				debuglog.Warn("frontdesk: metrics scrape with invalid token", "remote_addr", clientip.From(r))
			}
			http.Error(w, "invalid metrics token", http.StatusUnauthorized)
			return
		}
		// No dedicated token configured — fall back to ADMIN auth. requireAuth
		// alone admits every authenticated caller including a PAIRED DEVICE,
		// which is exactly what requireAdmin exists to refuse: a phone paired as
		// operator or viewer would otherwise scrape the fleet's Prometheus
		// counters. Same reasoning as the main server's metricsAuth.
		s.requireAuth(s.requireAdmin(next)).ServeHTTP(w, r)
	})
}

// logout revokes the caller's server-side session so a manual or idle
// auto-logout drops the session everywhere, not just in the calling browser
// tab. The token comes from the fd_session cookie first, with a bearer-header
// fallback for header-mode clients; the raw FRONTDESK_TOKEN has no session row,
// so RevokeAuthToken is a harmless no-op for it. Always returns 200: logging out
// an expired or absent session is a no-op, not an error, so the SPA can always
// converge to the login screen.
//
// Credential-gated against forced-logout CSRF, exactly like the dashboard's
// AuthLogout. A cross-site POST cannot carry either Front Desk cookie (both are
// SameSite=Strict) nor an Authorization header, so it arrives here bare;
// emitting Set-Cookie deletions unconditionally would let any third-party page
// log the victim out. Requests with no credential at all therefore get a success
// answer with no cookie headers - there is nothing to log out. Same-site logout
// is unaffected, including an already-revoked or otherwise invalid session,
// because the cookies still ride along.
//
// Deliberately not gated on the CSRF double-submit header instead: SameSite
// strips the CSRF cookie from a cross-site request too, so the server cannot
// tell "no session" from "cross-site", and requiring the header would break
// bearer-token callers that never have one.
func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	token, ok := authcookie.FrontDesk.SessionToken(r)
	if !ok {
		token, ok = util.ParseBearerToken(r)
	}
	if ok {
		s.sessionMgr.RevokeAuthToken(r.Context(), token)
	}

	// A stray CSRF cookie with no session still counts as the caller's own
	// state, so clear on either cookie rather than only on a full session.
	// Non-empty, matching SessionToken's treatment of the session cookie: a bare
	// "fd_csrf=" carries no state and must not stand in for a credential.
	csrf, err := r.Cookie(authcookie.FrontDesk.CSRFCookie)
	csrfPresent := err == nil && csrf.Value != ""

	if ok || csrfPresent {
		authcookie.FrontDesk.ClearSession(w, authcookie.Secure(r, s.cookieSecure))
	}

	writeJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// ---------------------------------------------------------------------------
// Traefik config (bearer-gated when FRONTDESK_TRAEFIK_TOKEN is set)
// ---------------------------------------------------------------------------

// traefikAuth gates the Traefik dynamic-config endpoint. With a dedicated
// FRONTDESK_TRAEFIK_TOKEN configured, only a poll carrying that bearer is
// served; Traefik's HTTP provider sends it via providers.http.headers, wired
// to the same value in the HA compose. Without a token the endpoint stays
// open: Traefik polls credential-less, so unlike /metrics there is no
// admin-auth fallback — gating an unconfigured fleet would 401 its own data
// plane and take the front door down on the next Traefik restart.
// traefikGated reports whether /traefik/config requires the bearer. One
// predicate for all three readers (the startup warning, the router's decision
// to mount a limiter, and this gate), because the router decides once at build
// time and the gate decides per request: two spellings of the same condition
// is how a future reloadable token would leave the router silently stale.
func (s *Server) traefikGated() bool { return s.traefikToken != "" }

func (s *Server) traefikAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.traefikGated() {
			next(w, r)
			return
		}
		tok, ok := util.ParseBearerToken(r)
		if subtle.ConstantTimeCompare([]byte(tok), []byte(s.traefikToken)) == 1 {
			next(w, r)
			return
		}
		if !ok || tok == "" {
			debuglog.Warn("frontdesk: traefik config poll missing bearer token", "remote_addr", clientip.From(r))
		} else {
			debuglog.Warn("frontdesk: traefik config poll with invalid token", "remote_addr", clientip.From(r))
		}
		http.Error(w, "invalid traefik token", http.StatusUnauthorized)
	}
}

// handleHealthz answers the container health probe with the same store reads
// a Traefik config refresh performs (members, then settings), so the probe
// fails exactly when Traefik's own poll would — the depth the old spider on
// /traefik/config provided — without exposing any fleet data on an open route.
func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if _, err := s.store.ListMembers(r.Context()); err != nil {
		writeError(w, err)
		return
	}
	if _, err := s.store.GetSettings(r.Context()); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleTraefikConfig(w http.ResponseWriter, r *http.Request) {
	// Recorded after the gate, so a rejected poll cannot keep the staleness
	// watchdog quiet while a token mismatch is starving the real Traefik.
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
