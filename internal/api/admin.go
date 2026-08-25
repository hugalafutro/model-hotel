// Package api provides HTTP handlers and routing for the admin API.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/hugalafutro/model-hotel/internal/alert"
	"github.com/hugalafutro/model-hotel/internal/audit"
	"github.com/hugalafutro/model-hotel/internal/auth"
	"github.com/hugalafutro/model-hotel/internal/authcookie"
	"github.com/hugalafutro/model-hotel/internal/clientip"
	"github.com/hugalafutro/model-hotel/internal/config"
	"github.com/hugalafutro/model-hotel/internal/db"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/events"
	"github.com/hugalafutro/model-hotel/internal/failover"
	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/provider"
	"github.com/hugalafutro/model-hotel/internal/quota"
	"github.com/hugalafutro/model-hotel/internal/totp"
	"github.com/hugalafutro/model-hotel/internal/user"
	"github.com/hugalafutro/model-hotel/internal/util"
	"github.com/hugalafutro/model-hotel/internal/virtualkey"
	"github.com/hugalafutro/model-hotel/internal/webauthn"
)

// TotpStatus reports whether TOTP 2FA is active, used by AuthMiddleware gating.
// Implemented by *totp.Repository.
type TotpStatus interface {
	IsEnabled(ctx context.Context) (bool, error)
}

// ProviderStore defines the provider repository methods used by the API.
type ProviderStore interface {
	Create(ctx context.Context, req provider.CreateProviderRequest, encryptedKey, keyNonce, keySalt []byte) (*provider.Provider, error)
	List(ctx context.Context) ([]*provider.Provider, error)
	Get(ctx context.Context, id uuid.UUID) (*provider.Provider, error)
	GetByName(ctx context.Context, name string) (*provider.Provider, error)
	Update(ctx context.Context, id uuid.UUID, req provider.UpdateProviderRequest, encryptedKey, keyNonce, keySalt []byte) (*provider.Provider, error)
	Delete(ctx context.Context, id uuid.UUID) error
}

// VirtualKeyStore defines the virtual key repository methods used by the API.
type VirtualKeyStore interface {
	Create(ctx context.Context, name, keyHash, keyPreview string, rps *float64, burst, tpm *int, allowedProviders *[]string, stripReasoning *bool, ownerUserID *uuid.UUID) (*virtualkey.VirtualKey, error)
	List(ctx context.Context) ([]*virtualkey.VirtualKey, error)
	ListByOwner(ctx context.Context, ownerUserID uuid.UUID) ([]*virtualkey.VirtualKey, error)
	Get(ctx context.Context, id uuid.UUID) (*virtualkey.VirtualKey, error)
	Delete(ctx context.Context, id uuid.UUID) error
	Update(ctx context.Context, id uuid.UUID, name string, rps *float64, burst, tpm *int, allowedProviders *[]string, stripReasoning *bool, ownerUserID *uuid.UUID) (*virtualkey.VirtualKey, error)
}

// SettingsStore defines the settings repository methods used by the API.
type SettingsStore interface {
	GetAll(ctx context.Context) (map[string]string, error)
	GetWithDefault(ctx context.Context, key string, defaultValue string) string
	GetChecked(ctx context.Context, key string) (value string, found bool, err error)
	GetBool(ctx context.Context, key string, defaultValue bool) bool
	GetDuration(ctx context.Context, key string, defaultValue time.Duration) time.Duration
	GetInt(ctx context.Context, key string, defaultValue int) int
	Set(ctx context.Context, key string, value string) error
	SetMany(ctx context.Context, kvs [][2]string) error
	SetTx(ctx context.Context, tx pgx.Tx, key string, value string) error
	DeleteKeysTx(ctx context.Context, tx pgx.Tx, keys []string) error
	DeleteKey(ctx context.Context, key string) error
	InvalidateCache(key string)
	NotifyDeleted(key string)
}

// BackupScheduler defines the interface for the periodic backup scheduler.
type BackupScheduler interface {
	StartScheduler(ctx context.Context)
	StopScheduler()
}

// AdminAuthenticator defines admin token validation.
type AdminAuthenticator interface {
	Validate(token string) bool
}

// WebAuthnSessionManager defines webAuthn session token validation.
// It is implemented by the internal/webauthn.SessionManager.
type WebAuthnSessionManager interface {
	Validate(ctx context.Context, token string) bool
	// TokenUser validates like Validate and returns the session's user handle
	// ([]byte("admin") for legacy admin logins, a user UUID string for
	// multi-user password logins).
	TokenUser(ctx context.Context, token string) ([]byte, bool)
	// Authenticate validates like TokenUser and additionally reports the
	// session's expiry and whether this call slid it forward, so a cookie
	// caller can re-issue the cookie pair with the new lifetime.
	Authenticate(ctx context.Context, token string) (webauthn.AuthResult, bool)
	// Verify validates like Authenticate but writes nothing (no last-seen
	// stamp, no slide): for server-driven re-checks that are not use.
	Verify(ctx context.Context, token string) (webauthn.AuthResult, bool)
	RevokeAuthToken(ctx context.Context, token string) bool
	// RevokeOtherSessions signs out every session belonging to identity except
	// the one the request was made from. identity must come from the
	// authentication layer, never from a token the caller supplied; the
	// candidate tokens only decide which session is spared, and one that does
	// not belong to identity spares nothing.
	RevokeOtherSessions(ctx context.Context, identity []byte, candidateTokens ...string) (int64, error)
	// CreateAuthToken mints a new session token for the given user handle. The
	// admin-token exchange trades a valid admin token for a session cookie via
	// this method (userID is []byte("admin") for the legacy admin login). meta
	// carries the login request's device metadata onto the stored session.
	CreateAuthToken(ctx context.Context, userID, credentialID []byte, meta webauthn.SessionMeta) (string, error)
	// ListAuthSessions returns identity's live sessions for the active-sessions
	// list, marking as current the one whose token the request carried. The
	// same identity/candidate contract as RevokeOtherSessions applies.
	ListAuthSessions(ctx context.Context, identity []byte, candidateTokens ...string) ([]webauthn.AuthSessionInfo, error)
	// RevokeSessionByID deletes one of identity's sessions. It returns
	// webauthn.ErrNotFound for a session that is missing or not identity's, and
	// webauthn.ErrCurrentSession when the target is the session the request
	// itself rides on.
	RevokeSessionByID(ctx context.Context, identity []byte, id uuid.UUID, candidateTokens ...string) error
}

// PwnedChecker reports whether a password appears in a known breach corpus.
// Satisfied by *pwned.Checker in production and by a stub in tests; nil when
// the feature is not wired.
type PwnedChecker interface {
	Breached(ctx context.Context, password string) (bool, int, error)
}

// Handler manages admin API operations for providers, models, and virtual keys.
type Handler struct {
	cfg *config.Config
	// Shared outbound client for the alert endpoints; nil is valid and makes
	// each dispatcher build its own, which is what handlers in tests get.
	alertClient            *http.Client
	providerRepo           ProviderStore
	dbPool                 *db.DB
	adminMgr               AdminAuthenticator
	virtualKeyRepo         VirtualKeyStore
	settingsRepo           SettingsStore
	systemHandler          *SystemHandler
	backupScheduler        BackupScheduler
	appVersion             string
	ghReleasesURL          string                                             // injectable for testing; defaults to githubReleasesURL const
	ghTagsURL              string                                             // injectable for testing; defaults to githubTagsURL const
	eventBus               *events.Bus                                        // /api/events subscribes here (DefaultBus in production); publishers still use events.Publish, so a private bus only isolates the stream side
	webauthnSessionMgr     WebAuthnSessionManager                             // nil when webAuthn is not configured
	clientIPs              webauthn.ClientIPSource                            // trusted-proxy-aware client IP for session device metadata; nil falls back to the peer address
	userRepo               UserStore                                          // nil until SetUserAuth (multi-user identities)
	sessionRevoker         SessionRevoker                                     // nil until SetUserAuth (revoke on disable/delete)
	userTotp               UserTotpFactory                                    // nil until SetUserTotp (per-user 2FA endpoints)
	pwThrottle             *totp.Throttle                                     // per-user backoff on failed current-password checks
	testModelTransport     *http.Transport                                    // SSRF-protected transport for TestModel
	testModelCheckRedirect func(req *http.Request, via []*http.Request) error // SSRF-protected redirect check for TestModel
	discoveryDialCtx       func(ctx context.Context, network, addr string) (net.Conn, error)
	discoveryCheckRedirect func(req *http.Request, via []*http.Request) error
	circuitBreaker         CircuitBreakerControl
	audit                  *audit.Recorder   // nil until SetAudit (audit trail of admin actions)
	totpStatus             TotpStatus        // nil when TOTP feature not wired -> TotpEnabled() returns false (today's behavior)
	totpEnabled            atomic.Bool       // cached IsEnabled result; refreshed by enroll-verify/disable handlers after DB mutations
	quotaRepo              *quota.Repository // read-through store for polled provider quota snapshots
	quotaAdvisor           *QuotaAdvisor     // nil until SetQuotaAdvisor; populated by RefreshQuotaAdvice
	pwnedChecker           PwnedChecker      // nil until SetPwnedChecker (breached-password check on create/reset/change)

	// Debounce state for the quota schema-drift watch: a per-provider shape
	// that has been seen but not yet confirmed by a second consecutive poll.
	// Deliberately in-memory (a restart re-arms the debounce, costing one extra
	// poll before a real change is reported) and guarded because it is
	// process-wide state, even though only the poll goroutine touches it today.
	quotaSchemaMu   sync.Mutex
	quotaSchemaSeen map[uuid.UUID]quotaSchemaCandidate

	// Debounce state for the breaker-open quota nudge: when each provider was
	// last polled out of band. Guarded because a nudge arrives on whichever
	// goroutine opened the circuit, so several can land at once.
	quotaNudgeMu   sync.Mutex
	quotaNudgeLast map[uuid.UUID]time.Time
}

// NewHandler creates a new admin API handler with the given dependencies.
func NewHandler(cfg *config.Config, providerRepo ProviderStore, database *db.DB, adminMgr AdminAuthenticator, vkRepo VirtualKeyStore, settingsRepo SettingsStore, appVersion string, testModelTransport *http.Transport, testModelCheckRedirect func(req *http.Request, via []*http.Request) error, discoveryDialCtx func(ctx context.Context, network, addr string) (net.Conn, error), discoveryCheckRedirect func(req *http.Request, via []*http.Request) error) *Handler {
	h := &Handler{
		cfg:                    cfg,
		providerRepo:           providerRepo,
		dbPool:                 database,
		adminMgr:               adminMgr,
		virtualKeyRepo:         vkRepo,
		settingsRepo:           settingsRepo,
		eventBus:               events.DefaultBus,
		appVersion:             appVersion,
		ghReleasesURL:          githubReleasesURL,
		ghTagsURL:              githubTagsURL,
		testModelTransport:     testModelTransport,
		testModelCheckRedirect: testModelCheckRedirect,
		discoveryDialCtx:       discoveryDialCtx,
		discoveryCheckRedirect: discoveryCheckRedirect,
		// Same profile as the login throttles: an authenticated session must
		// not be a free brute-force oracle for the account's current password.
		pwThrottle: totp.NewThrottle(5, time.Second, 5*time.Minute),
		quotaRepo:  quota.NewRepository(database.Pool()),
		// One client for every alert probe/test this handler serves, rather than
		// one per request: see alert.NewHTTPClient.
		alertClient: alert.NewHTTPClient(),
	}
	// Wire the discovery service factory to use the SSRF-protected dial/redirect
	// functions so admin-API discovery endpoints are also protected.
	newDiscoveryService = func() *provider.DiscoveryService {
		return provider.NewDiscoveryService(h.discoveryDialCtx, h.discoveryCheckRedirect)
	}
	return h
}

// Pool returns the database connection pool.
func (h *Handler) Pool() *db.DB {
	return h.dbPool
}

// SetPwnedChecker wires the breached-password checker used by the user
// create/reset/change flows. Leaving it unset disables the check regardless of
// config (the check is skipped, never blocking).
func (h *Handler) SetPwnedChecker(c PwnedChecker) {
	h.pwnedChecker = c
}

// SetWebAuthnSessionManager sets the optional webAuthn session manager for
// token-based authentication fallback in AuthMiddleware.
func (h *Handler) SetWebAuthnSessionManager(mgr WebAuthnSessionManager) {
	h.webauthnSessionMgr = mgr
}

// SetClientIPSource wires the trusted-proxy-aware client-IP resolver (the IP
// limiter) used for session device metadata at the admin-token exchange. Left
// nil, forwarded headers are never trusted and the peer address is stored.
func (h *Handler) SetClientIPSource(ips webauthn.ClientIPSource) {
	h.clientIPs = ips
}

// SetUserAuth wires the multi-user store and session revoker into the auth
// middleware and users admin API. Without it, sessions carrying user UUIDs
// fail closed (401) and the Users API is not mounted usefully.
func (h *Handler) SetUserAuth(users UserStore, revoker SessionRevoker) {
	h.userRepo = users
	h.sessionRevoker = revoker
}

// SetTotpStatus wires the TOTP status source and best-effort seeds the cache.
// On seed error it fails closed (treats as enabled) so a DB blip at startup
// cannot silently disable 2FA if it was previously enabled.
func (h *Handler) SetTotpStatus(src TotpStatus) {
	h.totpStatus = src
	// Seed synchronously, before the server accepts requests, so there is no
	// window where TotpEnabled() returns the false zero-value while 2FA is on.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	enabled, err := src.IsEnabled(ctx)
	if err != nil {
		debuglog.Error("totp: failed to seed enabled cache, failing closed", "error", err)
		h.totpEnabled.Store(true)
		return
	}
	h.totpEnabled.Store(enabled)
}

// TotpEnabled reports the cached TOTP-enabled state. Per-request hot path: no DB
// hit. Returns false when the feature is not wired (nil source).
func (h *Handler) TotpEnabled() bool {
	if h.totpStatus == nil {
		return false
	}
	return h.totpEnabled.Load()
}

// RefreshTotpEnabled re-reads IsEnabled from the DB and updates the cache. Called
// by the TOTP enroll-verify and disable handlers AFTER their DB mutations
// succeed. On DB error it fails closed (sets true).
func (h *Handler) RefreshTotpEnabled(ctx context.Context) {
	if h.totpStatus == nil {
		return
	}
	enabled, err := h.totpStatus.IsEnabled(ctx)
	if err != nil {
		debuglog.Error("totp: failed to refresh enabled cache, failing closed", "error", err)
		h.totpEnabled.Store(true)
		return
	}
	h.totpEnabled.Store(enabled)
}

// SetDockerStatsCollector overrides the system Docker stats collector (for testing).
func (h *Handler) SetDockerStatsCollector(fn dockerStatsCollector) {
	if h.systemHandler != nil {
		h.systemHandler.dockerStatsCollector = fn
	}
}

// SetCircuitBreaker wires the proxy's circuit breaker so the API can publish its
// status (failover page, sidebar badge, /metrics) and reset it: there is exactly
// one breaker, so status and the operator reset lever come from the same object.
func (h *Handler) SetCircuitBreaker(cb CircuitBreakerControl) {
	h.circuitBreaker = cb
}

// SetQuotaAdvisor wires the in-memory quota advisor that RefreshQuotaAdvice
// populates from stored snapshots on every poll. Call during startup wiring;
// leaving it unset makes RefreshQuotaAdvice a no-op.
func (h *Handler) SetQuotaAdvisor(a *QuotaAdvisor) {
	h.quotaAdvisor = a
}

// StartBackupScheduler starts the periodic backup scheduler if backup_enabled is true.
// Call this only after Register, which constructs the BackupHandler and assigns it as
// h.backupScheduler; calling earlier leaves the scheduler nil and no backups ever run.
func (h *Handler) StartBackupScheduler(ctx context.Context) {
	if h.backupScheduler == nil {
		debuglog.Warn("backup: StartBackupScheduler called before the scheduler was wired; no automatic backups will run")
		return
	}
	h.backupScheduler.StartScheduler(ctx)
}

// StopBackupScheduler stops the periodic backup scheduler.
func (h *Handler) StopBackupScheduler() {
	if h.backupScheduler != nil {
		h.backupScheduler.StopScheduler()
	}
}

// Register mounts all admin API routes on the given router.
func (h *Handler) Register(r chi.Router) {
	r.Use(h.AuthMiddleware)

	// Audit trail: records every mutating request on this surface, including
	// ones the demo read-only guard below refuses (mounted before it on
	// purpose, so refused attempts appear with their 403).
	if h.audit != nil {
		r.Use(h.audit.Middleware)
	}

	// Demo hardening: in read-only mode every mutating request to the admin
	// CRUD surface is refused (see readOnlyGuard). Mounted here only, so the
	// admin chat and public proxy stay usable against the seeded providers.
	if h.cfg.DemoReadOnly {
		r.Use(readOnlyGuard)
	}

	// Caller identity for the SPA's navigation gating (any authenticated role).
	r.Get("/auth/me", h.Me)

	// Self-service per-user TOTP (any users-row identity manages its own).
	h.RegisterUserTotp(r)

	// Self-service password rotation for users-row identities.
	r.Post("/auth/password", h.ChangeOwnPassword)

	// Self-service session hygiene: sign this identity's other sessions out.
	// Ungated like /auth/password because the handler takes the identity from
	// this middleware rather than from the request, so a caller can only ever
	// reach their own sessions. That property is what makes it safe to leave
	// open, and auth_sessions_route_test.go pins it.
	r.Post("/auth/sessions/revoke-others", h.RevokeOtherSessions)

	// The active-sessions list and its per-row revoke. Same open-but-scoped
	// contract: both handlers resolve the identity from this middleware, so a
	// caller only ever sees or ends their own sessions.
	r.Get("/auth/sessions", h.ListAuthSessions)
	r.Delete("/auth/sessions/{id}", h.RevokeAuthSessionByID)

	// System health stats feed the sidebar widget every role sees: routing
	// metadata and process gauges only, so any authenticated caller may read it.
	sh := NewSystemHandler(h.dbPool.Pool(), h.settingsRepo)
	sh.Register(r)
	h.systemHandler = sh

	r.Route("/providers", func(r chi.Router) {
		// Reads are shared with the virtual-keys grant (the VK modal's
		// allowed-providers picker) and the usage grant (the Dashboard's
		// provider count; stats already expose the provider names).
		r.Group(func(r chi.Router) {
			r.Use(requireGrant(user.GrantVirtualKeys, user.GrantUsage))
			r.Get("/", h.ListProviders)
			r.Get("/{id}", h.GetProvider)
		})
		// Provider CRUD is synced config: a managed fleet member must not edit it
		// locally (the primary owns it and replaces it on the next sync). Discovery
		// routes under /providers (mounted via RegisterProviderDiscovery) are
		// deliberately outside this group: models regenerate and are not synced.
		r.Group(func(r chi.Router) {
			r.Use(requireAdmin)
			r.Use(managedWriteGuard(h.settingsRepo))
			r.Post("/", h.CreateProvider)
			r.Put("/{id}", h.UpdateProvider)
			r.Delete("/{id}", h.DeleteProvider)
		})
	})

	h.RegisterModels(r)
	h.RegisterLogs(r)
	h.RegisterVirtualKeys(r)
	h.RegisterVersion(r)
	h.RegisterUsers(r)

	// Usage dashboards are readable with the usage grant.
	r.Group(func(r chi.Router) {
		r.Use(requireGrant(user.GrantUsage))
		NewStatsHandler(h.dbPool.Pool(), h.adminMgr).Register(r)
	})

	// Everything below is admin-only surface: discovery, app logs, settings,
	// alerts, failover config, backup, config-sync, fleet.
	r.Group(func(r chi.Router) {
		r.Use(requireAdmin)
		h.registerAdminOnly(r)
	})
}

// registerAdminOnly mounts the route groups only admins may touch. Split out
// of Register so the requireAdmin guard visibly covers the whole set.
func (h *Handler) registerAdminOnly(r chi.Router) {
	h.RegisterProviderDiscovery(r)
	h.RegisterAudit(r)
	h.RegisterAppLogs(r)
	h.RegisterSettings(r)
	h.RegisterAlerts(r)

	failoverRepo := failover.NewRepository(h.dbPool.Pool())
	modelRepo := model.NewRepository(h.dbPool.Pool())
	NewFailoverHandler(h.dbPool.Pool(), failoverRepo, modelRepo, h.settingsRepo, h.circuitBreaker).Register(r)

	bh := NewBackupHandler(h.cfg.DatabaseURL, filepath.Join(h.cfg.DataDir, "backups"), h.adminMgr, h.settingsRepo)
	bh.SetSigningKey(h.cfg.MasterKey)
	bh.SetSessionAuth(h.webauthnSessionMgr, h.TotpEnabled)
	bh.Register(r)
	h.backupScheduler = bh

	// HA fleet config-sync endpoints (Phase 5). Always mounted: any member can be
	// a primary (export) or a replica (import). Inherits this group's admin auth.
	// The discovery callback lets an import populate this member's models so synced
	// custom failover groups resolve without a manual discover (a freshly-synced
	// member has providers but no models until discovery runs).
	NewConfigSyncHandler(h.dbPool, h.settingsRepo, h.cfg.MasterKey, h.appVersion,
		func(ctx context.Context) error {
			// Request-bound (runs inside the config-sync import HTTP handler):
			// skip miss-recording so the confirmation-probe backoff cannot
			// overrun the 60s route timeout and make the sync look failed. The
			// synced member's own scheduled sweep owns disabling.
			_, _, _, _, err := h.discoverAllProviders(ctx, false)
			return err
		}, h.cfg.ValidateProviderURL).Register(r)

	// Fleet quota snapshot export/receive (quota poller Phase 2). Same
	// fleet-authed router as config-sync; snapshots carry no key material, so
	// unlike config import there is no MASTER_KEY canary.
	NewQuotaFleetHandler(h.quotaRepo, h.providerRepo).Register(r)

	// HA fleet membership heartbeat (Phase 6). Front Desk POSTs /fleet/announce
	// on its poll; the member records the contact as instance-local _fleet_*
	// settings and surfaces fleet state on its system payload. Inherits this
	// group's admin auth so the badge cannot be forged.
	NewFleetHandler(h.settingsRepo).Register(r)
}

// resolveCredentials authenticates the request and returns the caller's
// identity. cookieAuth reports that the identity came from the session cookie,
// the only ambient-credential path and therefore the only one needing a CSRF
// check; the caller applies that check because only it can answer with a 403.
//
// Shared by AuthMiddleware and by the long-lived SSE stream, which re-runs it
// mid-connection so a revoked session or changed grants take effect without
// waiting for the client to disconnect. It writes nothing to the response:
// when the cookie session's expiry just slid forward, refresh carries what the
// middleware needs to re-issue the cookie pair (token + new expiry).
//
// use says whether this request counts as the person using the session: the
// middleware passes true (stamp last-seen, slide the expiry); the SSE re-check
// passes false and gets a pure lookup, because a heartbeat the server drives
// is not use, must not keep an untouched tab's session alive by itself, and
// could not carry a re-issued cookie anyway (its headers are long gone).
func (h *Handler) resolveCredentials(r *http.Request, use bool) (id *user.Identity, cookieAuth, ok bool, refresh *cookieRefresh) {
	// Resolved lazily: both call sites below sit behind the nil guard on
	// webauthnSessionMgr, and a method value taken from a nil interface would
	// not.
	authenticate := func(ctx context.Context, tok string) (webauthn.AuthResult, bool) {
		if use {
			return h.webauthnSessionMgr.Authenticate(ctx, tok)
		}
		return h.webauthnSessionMgr.Verify(ctx, tok)
	}
	// Cookie path (browser). The session token rides an HttpOnly cookie. This
	// branch is additive: an invalid/expired cookie falls through to the header
	// logic below, and header (admin-token / bearer) callers are unaffected.
	if tok, found := authcookie.SessionToken(r); found && h.webauthnSessionMgr != nil {
		if res, valid := authenticate(r.Context(), tok); valid {
			if id, resolved := h.resolveIdentity(r.Context(), res.UserID); resolved {
				if res.Extended {
					refresh = &cookieRefresh{token: tok, result: res}
				}
				return id, true, true, refresh
			}
		}
		// Invalid/expired cookie: fall through to header logic.
	}

	token, found := util.ParseBearerToken(r)
	if !found {
		return nil, false, false, nil
	}

	// Fast path: admin token (in-memory hash comparison) -- only when TOTP
	// 2FA is disabled. With TOTP enabled, the raw admin token is a first
	// factor only and must be exchanged for a session token via POST
	// /api/totp/login; a bare admin token bearer is rejected so the second
	// factor cannot be bypassed.
	if !h.TotpEnabled() && h.adminMgr.Validate(token) {
		return user.AdminIdentity(), false, true, nil
	}

	// Fallback: session token (DB-backed SHA-256 hash lookup). The session's
	// user handle resolves to an identity: legacy admin sessions stay admin,
	// UUID handles must match an enabled users row (disabled/deleted users
	// are rejected here even if their token has not been revoked yet).
	if h.webauthnSessionMgr != nil {
		if res, valid := authenticate(r.Context(), token); valid {
			if id, resolved := h.resolveIdentity(r.Context(), res.UserID); resolved {
				return id, false, true, nil
			}
		}
	}

	return nil, false, false, nil
}

// cookieRefresh is what AuthMiddleware needs to re-issue the session cookie
// pair after the session's expiry slid: the token the cookie carries and the
// authentication result holding the new expiry.
type cookieRefresh struct {
	token  string
	result webauthn.AuthResult
}

// AuthMiddleware validates admin token or webAuthn session token authentication.
// Admin token has priority (fast in-memory hash comparison).
// If the admin token is invalid, the session-based token is tried as a fallback.
func (h *Handler) AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, cookieAuth, ok, refresh := h.resolveCredentials(r, true)
		if !ok {
			// Warn (not Error) with the remote address — never the token — so
			// repeated admin-auth failures are visible for abuse detection
			// without polluting the operator-actionable Error stream.
			if _, hasBearer := util.ParseBearerToken(r); !hasBearer {
				debuglog.Warn("auth: admin request missing bearer token", "remote_addr", clientip.From(r), "path", r.URL.Path)
				http.Error(w, "Authorization header required (Bearer token)", http.StatusUnauthorized)
				return
			}
			debuglog.Warn("auth: admin request with invalid token", "remote_addr", clientip.From(r), "path", r.URL.Path)
			http.Error(w, "Invalid admin token", http.StatusUnauthorized)
			return
		}

		// Cookie-authenticated unsafe methods must also present a matching CSRF
		// header. Bearer callers are exempt: an explicit header is not a
		// credential the browser attaches on its own.
		if cookieAuth && !authcookie.IsSafeMethod(r.Method) && !authcookie.ValidCSRF(r) {
			debuglog.Warn("auth: CSRF check failed", "remote_addr", clientip.From(r), "path", r.URL.Path)
			http.Error(w, "CSRF token missing or invalid", http.StatusForbidden)
			return
		}

		// The session slid forward on this request: hand the browser the new
		// lifetime, or it drops the cookie on the original schedule.
		if refresh != nil {
			if err := authcookie.Dashboard.RefreshSession(w, r, refresh.token,
				authcookie.Secure(r, h.cfg.CookieSecure), time.Until(refresh.result.ExpiresAt)); err != nil {
				// Best-effort: the authentication passed; the browser merely keeps
				// the older lifetime until the next successful refresh.
				debuglog.Error("auth: failed to re-issue session cookie", "error", err)
			}
		}

		next.ServeHTTP(w, r.WithContext(user.WithIdentity(r.Context(), id)))
	})
}

// RegisterEvents registers the SSE endpoint on a route group that is
// exempt from the chi Timeout middleware.  SSE connections are long-lived
// and must not be killed by a 60-second request deadline; the handler
// detects client disconnect via r.Context().Done() instead.
func (h *Handler) RegisterEvents(r chi.Router) {
	r.Use(h.AuthMiddleware)
	r.Get("/events", h.StreamEvents)
}

// CreateProvider creates a new provider.
func (h *Handler) CreateProvider(w http.ResponseWriter, r *http.Request) {
	var req provider.CreateProviderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondBadRequest(w, "invalid request body", err)
		return
	}

	trimmed, err := validateNameString("name", req.Name, 1, 100)
	if err != nil {
		respondBadRequest(w, "invalid name", err)
		return
	}
	req.Name = trimmed

	if req.BaseURL == "" {
		http.Error(w, "base_url is required", http.StatusBadRequest)
		return
	}

	// The dashboard sends the type the operator picked. A client that omits it
	// gets the vendor-hostname derivation: enough to keep scripted adds of
	// cloud providers working, without resurrecting the port guessing for
	// self-hosted servers (which must be named to be added).
	derivedType := req.ProviderType == ""
	if derivedType {
		req.ProviderType = provider.TypeFromHostname(req.BaseURL)
	}
	if !provider.IsKnownType(req.ProviderType) {
		http.Error(w, "unknown provider_type", http.StatusBadRequest)
		return
	}
	// Self-hosted servers serve their OpenAI-compatible API under /v1 and their
	// native endpoints at the root, so the /v1 half is a convenience the
	// operator should not have to get right.
	req.BaseURL = provider.NormalizeLocalBaseURL(req.ProviderType, req.BaseURL)

	// Measured after normalization, so the value that is actually stored is the
	// one that has to fit.
	if len(req.BaseURL) > 500 {
		http.Error(w, "base_url must be less than 500 characters", http.StatusBadRequest)
		return
	}

	// An address that matches no vendor host and was given no type is a
	// generic OpenAI endpoint. That is right for a gateway and wrong for a
	// self-hosted server the caller forgot to name, and the difference is
	// invisible afterwards, so say so once.
	if derivedType && req.ProviderType == "openai" {
		debuglog.Info("provider: no provider_type given, treating as a generic OpenAI-compatible endpoint",
			"name", req.Name, "hint", "self-hosted servers (ollama, lmstudio, koboldcpp) must name their type to get native discovery")
	}

	// Some providers (e.g. OpenCode Zen) support keyless access for free models.
	// Allow empty API key only for providers that support it.
	if req.APIKey == "" && !providerTypeAllowsEmptyKey(req.ProviderType) {
		http.Error(w, "api_key is required for this provider type", http.StatusBadRequest)
		return
	}

	if len(req.APIKey) > 500 {
		http.Error(w, "api_key must be less than 500 characters", http.StatusBadRequest)
		return
	}

	if !h.acceptProviderURLShape(w, req.BaseURL) {
		return
	}

	// Application-level duplicate name check
	existing, err := h.providerRepo.GetByName(r.Context(), req.Name)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		// A DB error here silently looks like "no duplicate", so the app-level
		// guard is bypassed (the DB unique constraint is the backstop). Surface
		// it so a flaky DB doesn't quietly admit dupes.
		debuglog.Warn("provider create: duplicate-name check failed, relying on DB constraint", "name", req.Name, "error", err)
	}
	if existing != nil {
		http.Error(w, "a provider with this name already exists", http.StatusConflict)
		return
	}

	if !h.rejectDuplicateLocalServer(w, r, req.ProviderType, req.BaseURL, uuid.Nil) {
		return
	}

	// Last, because it is the only check that waits on the network: a bad name
	// or URL should fail immediately rather than after a probe timeout.
	if !h.confirmLocalServerType(w, r, req.ProviderType, req.BaseURL, req.APIKey) {
		return
	}

	var encryptedKey *auth.KeyPair
	if req.APIKey != "" {
		var encErr error
		encryptedKey, encErr = auth.Encrypt(req.APIKey, h.cfg.MasterKey)
		if encErr != nil {
			respondError(w, fmt.Sprintf("failed to encrypt API key for provider %q", req.Name), encErr, http.StatusInternalServerError)
			return
		}
	}

	var encCiphertext, encNonce, encSalt []byte
	if encryptedKey != nil {
		encCiphertext = encryptedKey.Ciphertext
		encNonce = encryptedKey.Nonce
		encSalt = encryptedKey.Salt
	}

	p, err := h.providerRepo.Create(r.Context(), req, encCiphertext, encNonce, encSalt)
	if err != nil {
		if db.IsUniqueViolation(err) {
			http.Error(w, "a provider with this name already exists", http.StatusConflict)
			return
		}
		respondError(w, fmt.Sprintf("failed to create provider %q", req.Name), err, http.StatusInternalServerError)
		return
	}

	// Skip key cache warming for keyless providers (nil encrypted key bytes)
	if len(p.EncryptedKey) > 0 {
		go auth.WarmKeyCache(p.EncryptedKey, p.KeyNonce, p.KeySalt, h.cfg.MasterKey)
	}

	response := provider.ToResponse(p)
	writeJSONCreated(w, response)
}

// ListProviders returns all configured providers.
func (h *Handler) ListProviders(w http.ResponseWriter, r *http.Request) {
	providers, err := h.providerRepo.List(r.Context())
	if err != nil {
		respondError(w, "failed to list providers", err, http.StatusInternalServerError)
		return
	}

	rows, err := h.dbPool.Pool().Query(r.Context(), "SELECT provider_id, COUNT(*) FROM models GROUP BY provider_id")
	if err != nil {
		respondError(w, "failed to query model counts", err, http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	modelCounts := make(map[string]int)
	for rows.Next() {
		var providerID string
		var count int
		if err := rows.Scan(&providerID, &count); err != nil {
			respondError(w, "failed to scan model count row", err, http.StatusInternalServerError)
			return
		}
		modelCounts[providerID] = count
	}

	// Non-admins only ever see their own traffic in these totals: the same
	// owner predicate the logs/stats surfaces apply, so a usage-granted user
	// cannot read other tenants' aggregate volume off the provider list.
	ownerFrag, ownerArgs := ownerFilterFragment(ownerScopeFromIdentity(r), 1)
	tokenRows, err := h.dbPool.Pool().Query(r.Context(), "SELECT rl.provider_id, SUM(COALESCE(rl.tokens_prompt, 0) + COALESCE(rl.tokens_completion, 0)) FROM request_logs rl WHERE rl.provider_id IS NOT NULL"+ownerFrag+" GROUP BY rl.provider_id", ownerArgs...)
	if err != nil {
		respondError(w, "failed to query token counts", err, http.StatusInternalServerError)
		return
	}
	defer tokenRows.Close()

	tokenCounts := make(map[string]int)
	for tokenRows.Next() {
		var providerID string
		var total int
		if err := tokenRows.Scan(&providerID, &total); err != nil {
			respondError(w, "failed to scan token count row", err, http.StatusInternalServerError)
			return
		}
		tokenCounts[providerID] = total
	}

	responses := make([]provider.ProviderResponse, len(providers))
	for i, p := range providers {
		responses[i] = provider.ToResponse(p)
		responses[i].ModelCount = modelCounts[p.ID.String()]
		responses[i].TotalTokens = tokenCounts[p.ID.String()]
	}

	writeJSON(w, responses)
}

// GetProvider returns a single provider by ID.
func (h *Handler) GetProvider(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUUIDParam(w, r, "id", "provider ID")
	if !ok {
		return
	}

	p, err := h.providerRepo.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.Error(w, "provider not found", http.StatusNotFound)
			return
		}
		respondError(w, fmt.Sprintf("failed to get provider %s", id), err, http.StatusInternalServerError)
		return
	}

	response := provider.ToResponse(p)

	var modelCount int
	if err := h.dbPool.Pool().QueryRow(r.Context(), "SELECT COUNT(*) FROM models WHERE provider_id = $1", p.ID).Scan(&modelCount); err == nil {
		response.ModelCount = modelCount
	}

	writeJSON(w, response)
}

// acceptProviderIdentity validates the two fields that decide where a provider
// points and how it is driven: its base URL and its type. Either change has to
// be confirmed against the server that answers, so they are handled together.
// It writes the error response and reports false when the change must not be
// saved.
func (h *Handler) acceptProviderIdentity(w http.ResponseWriter, r *http.Request, id uuid.UUID, req *provider.UpdateProviderRequest) bool {
	if req.BaseURL == nil && req.ProviderType == nil {
		return true
	}
	if req.ProviderType != nil && !provider.IsKnownType(*req.ProviderType) {
		http.Error(w, "unknown provider_type", http.StatusBadRequest)
		return false
	}
	// Checked before the provider is even loaded, so a refused address fails
	// fast and for the right reason.
	if req.BaseURL != nil && !h.acceptProviderURLShape(w, *req.BaseURL) {
		return false
	}

	current, err := h.providerRepo.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.Error(w, "provider not found", http.StatusNotFound)
			return false
		}
		respondError(w, fmt.Sprintf("failed to load provider %s", id), err, http.StatusInternalServerError)
		return false
	}

	// The type the provider will have once this update lands drives everything
	// below: a new address must answer as it, and a corrected type must match
	// the address already stored.
	effectiveType := provider.TypeOf(current)
	typeChanged := false
	if req.ProviderType != nil && *req.ProviderType != effectiveType {
		effectiveType = *req.ProviderType
		typeChanged = true
	}

	address := current.BaseURL
	if req.BaseURL != nil {
		address = *req.BaseURL
	}
	normalized := provider.NormalizeLocalBaseURL(effectiveType, address)
	if req.BaseURL != nil {
		req.BaseURL = &normalized
	}

	// A type-only change probes the address already stored, which was checked
	// when it was set. Re-check it: ALLOWED_PROVIDER_HOSTS may have been
	// narrowed since, and every address this handler probes must be one the
	// SSRF rules accept right now.
	if req.BaseURL == nil && !h.acceptProviderURLShape(w, normalized) {
		return false
	}

	// Nothing that decides where requests land is actually changing: an update
	// that only renames the provider must not fail because the server happens
	// to be down. Both sides are normalized first, so a client echoing back a
	// stored URL written before the /v1 form was canonical does not trip it.
	if !typeChanged && normalized == provider.NormalizeLocalBaseURL(effectiveType, current.BaseURL) {
		return true
	}

	if !h.rejectDuplicateLocalServer(w, r, effectiveType, normalized, id) {
		return false
	}

	// The probe needs a key for a password-protected server: the update's own
	// key when it carries one, otherwise the stored key.
	apiKey := ""
	if req.APIKey != nil {
		apiKey = *req.APIKey
	} else if len(current.EncryptedKey) > 0 {
		plain, decErr := auth.Decrypt(current.EncryptedKey, current.KeyNonce, current.KeySalt, h.cfg.MasterKey)
		if decErr != nil {
			// Not fatal: the probe simply goes out unauthenticated, and an
			// unreadable key is the update's problem to report, not this check's.
			debuglog.Warn("provider: could not decrypt key for the type probe", "provider_id", id, "error", decErr)
		} else {
			apiKey = plain
		}
	}
	return h.confirmLocalServerType(w, r, effectiveType, normalized, apiKey)
}

// acceptProviderURLShape applies the scheme and SSRF rules a base URL must
// satisfy before anything is done with it.
func (h *Handler) acceptProviderURLShape(w http.ResponseWriter, baseURL string) bool {
	if !h.cfg.AllowHTTPProviders {
		parsed, err := url.Parse(strings.TrimSpace(baseURL))
		if err != nil || parsed.Scheme != "https" {
			http.Error(w, "base_url must use HTTPS (set ALLOW_HTTP_PROVIDERS=true for HTTP)", http.StatusBadRequest)
			return false
		}
	}
	if err := h.cfg.ValidateProviderURL(baseURL); err != nil {
		// The reason matters to the operator: "not in ALLOWED_PROVIDER_HOSTS"
		// and "resolves to a private address" call for different fixes, and a
		// containerised Model Hotel cannot reach the operator's localhost at
		// all. This endpoint is admin-only, so echoing the reason leaks nothing.
		debuglog.Info("provider: base URL rejected", "error", err)
		writeCodedError(w, http.StatusBadRequest, codeProviderURLRejected, err.Error())
		return false
	}
	return true
}

// UpdateProvider updates an existing provider by ID.
func (h *Handler) UpdateProvider(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUUIDParam(w, r, "id", "provider ID")
	if !ok {
		return
	}

	var req provider.UpdateProviderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondBadRequest(w, "invalid request body", err)
		return
	}

	// Validate field lengths
	if req.Name != nil {
		trimmed, err := validateNamePtr("name", req.Name, 1, 100)
		if err != nil {
			respondBadRequest(w, "invalid name", err)
			return
		}
		req.Name = trimmed
	}

	if req.BaseURL != nil {
		trimmed := trimString(*req.BaseURL)
		req.BaseURL = &trimmed
		if err := validateStringPtrLength("base_url", req.BaseURL, 1, 500); err != nil {
			respondBadRequest(w, "invalid base URL", err)
			return
		}
	}

	if req.APIKey != nil {
		if len(*req.APIKey) > 500 {
			http.Error(w, "api_key must be at most 500 characters", http.StatusBadRequest)
			return
		}
	}

	if req.ScheduledDisableOn.Set && req.ScheduledDisableOn.Value != nil {
		v := *req.ScheduledDisableOn.Value
		if _, err := time.Parse("2006-01-02", v); err != nil {
			respondBadRequest(w, "invalid scheduled_disable_on", err)
			return
		}
		// ISO dates compare correctly as strings. The client's calendar floors
		// at browser-tomorrow, but the server accepts its own today: a browser
		// lagging the server clock would otherwise get a 400 on its earliest
		// selectable day. A server-today schedule is due immediately, and the
		// sweep fires it on its next tick.
		if v < time.Now().Format("2006-01-02") {
			http.Error(w, "scheduled_disable_on must not be in the past", http.StatusBadRequest)
			return
		}
	}

	// Application-level duplicate name check when renaming
	if req.Name != nil {
		existing, _ := h.providerRepo.GetByName(r.Context(), *req.Name)
		if existing != nil && existing.ID != id {
			http.Error(w, "a provider with this name already exists", http.StatusConflict)
			return
		}
	}

	// Validate the address and the type together: either one changing has to
	// be confirmed against the server that answers.
	if !h.acceptProviderIdentity(w, r, id, &req) {
		return
	}

	var encryptedKey []byte
	var keyNonce []byte
	var keySalt []byte

	if req.APIKey != nil {
		enc, encErr := auth.Encrypt(*req.APIKey, h.cfg.MasterKey)
		if encErr != nil {
			respondError(w, "failed to encrypt API key", encErr, http.StatusInternalServerError)
			return
		}
		encryptedKey = enc.Ciphertext
		keyNonce = enc.Nonce
		keySalt = enc.Salt
	}

	p, err := h.providerRepo.Update(r.Context(), id, req, encryptedKey, keyNonce, keySalt)
	if err != nil {
		if db.IsUniqueViolation(err) {
			http.Error(w, "a provider with this name already exists", http.StatusConflict)
			return
		}
		if errors.Is(err, pgx.ErrNoRows) {
			http.Error(w, "provider not found", http.StatusNotFound)
			return
		}
		respondError(w, fmt.Sprintf("failed to update provider %s", id), err, http.StatusInternalServerError)
		return
	}

	// If the provider was just disabled, sync failover groups to remove
	// stale entries from auto-created groups (routing already skips them,
	// but the UI and group membership should reflect the new state).
	if req.Enabled != nil && !*req.Enabled {
		if h.dbPool != nil {
			failoverRepo := failover.NewRepository(h.dbPool.Pool())
			if _, err := failoverRepo.SyncAllModels(context.WithoutCancel(r.Context())); err != nil {
				debuglog.Info("admin: failed to sync failover groups after provider disable", "error", err)
			}
		}
	}

	response := provider.ToResponse(p)
	writeJSON(w, response)
}

// DeleteProvider removes a provider by ID and cleans up associated data.
func (h *Handler) DeleteProvider(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUUIDParam(w, r, "id", "provider ID")
	if !ok {
		return
	}

	if err := h.providerRepo.Delete(r.Context(), id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.Error(w, "provider not found", http.StatusNotFound)
			return
		}
		respondError(w, fmt.Sprintf("failed to delete provider %s", id), err, http.StatusInternalServerError)
		return
	}

	// The quota drift watch keeps a per-provider schema baseline in the settings
	// K/V; nothing else removes it, so it would outlive the provider forever.
	// Detached from the request context like the sync below: the row is already
	// deleted, and a client that hangs up now must not leave the orphan behind.
	h.forgetQuotaSchema(context.WithoutCancel(r.Context()), id)

	// Sync failover groups since the cascade-deleted models may leave
	// groups with stale entries or zero candidates.
	// Guarded because unit tests pass nil dbPool.
	if h.dbPool != nil {
		failoverRepo := failover.NewRepository(h.dbPool.Pool())
		if _, err := failoverRepo.SyncAllModels(context.WithoutCancel(r.Context())); err != nil {
			// Log but don't fail the delete — the provider is already gone.
			debuglog.Info("admin: failed to sync failover groups after provider delete", "error", err)
		}
	}

	w.WriteHeader(http.StatusNoContent)
}

// providerTypeAllowsEmptyKey returns true for provider types that support keyless
// access (e.g. OpenCode Zen, and self-hosted servers, which serve their models
// without an API key).
func providerTypeAllowsEmptyKey(providerType string) bool {
	switch providerType {
	case "opencode-zen", "ollama", "koboldcpp", "lmstudio", "custom":
		return true
	default:
		return false
	}
}

// isForeignKeyViolation checks if the error is a PostgreSQL foreign key violation (error code 23503).
func isForeignKeyViolation(err error) bool {
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok {
		return pgErr.Code == "23503"
	}
	return false
}
