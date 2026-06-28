package frontdesk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"
	gowa "github.com/go-webauthn/webauthn/webauthn"

	"github.com/hugalafutro/model-hotel/internal/admin"
	"github.com/hugalafutro/model-hotel/internal/adminauth"
	"github.com/hugalafutro/model-hotel/internal/alert"
	"github.com/hugalafutro/model-hotel/internal/auth"
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
	alertDisp    *alert.Dispatcher
	settingsMu   sync.Mutex // serializes the settings-row read-merge-write
	router       http.Handler
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
	}

	// Outbound Apprise alerting: one consumer of the Front Desk event bus, gated by
	// the HA event catalog and the operator's picker. Built here so the settings
	// handlers can probe/test it; run as a goroutine via RunAlerts.
	s.alertDisp = alert.New(
		alertConfigProvider{store: cfg.Store, masterKey: cfg.MasterKey}, nil,
		alert.WithBus(cfg.Bus),
		alert.WithCatalog(fdCatalog),
		alert.WithTitlePrefix("Front Desk"),
		alert.WithDebounceKeys([]string{"member_id"}),
	)

	webauthnHandler := adminauth.NewWebAuthnHandler(
		webAuthnStore, cfg.RelyingParty, sessionMgr, cfg.AdminMgr, cfg.IPLimiter, false, s.totpStatus.Enabled,
	)
	totpHandler := adminauth.NewTotpHandler(
		totpRepo, cfg.AdminMgr, sessionMgr, cfg.IPLimiter, false, s.totpStatus.Enabled, s.totpStatus.Refresh,
	)

	s.router = s.buildRouter(webauthnHandler, totpHandler, cfg.UI)
	return s
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.router.ServeHTTP(w, r) }

// SessionManager exposes the session manager (used by callers wiring background
// cleanup of expired sessions).
func (s *Server) SessionManager() *webauthn.SessionManager { return s.sessionMgr }

func (s *Server) buildRouter(wa *adminauth.WebAuthnHandler, tp *adminauth.TotpHandler, ui fs.FS) http.Handler {
	r := chi.NewRouter()

	// Unauthenticated, compose-internal: Traefik's HTTP provider polls this.
	r.Get("/traefik/config", s.handleTraefikConfig)

	r.Route("/api", func(r chi.Router) {
		// Login + auth management ceremonies (gating handled inside the handlers:
		// login is public, register/disable require admin-or-session).
		wa.Register(r)
		tp.Register(r)

		// Control-plane REST + SSE, behind the admin-or-session gate.
		r.Group(func(r chi.Router) {
			r.Use(s.requireAuth)
			r.Get("/members", s.listMembers)
			r.Post("/members", s.createMember)
			r.Patch("/members/{id}", s.patchMember)
			r.Delete("/members/{id}", s.deleteMember)
			r.Post("/members/{id}/state", s.setMemberState)
			r.Get("/members/{id}/traffic", s.memberTraffic)
			r.Get("/settings", s.getSettings)
			r.Put("/settings", s.putSettings)
			r.Get("/version", s.getVersion)
			r.Get("/alert/events", s.alertEvents)
			r.Get("/alert/status", s.alertStatus)
			r.Post("/alert/test", s.alertTest)
			r.Get("/events", s.listEvents)
			r.Get("/traefik-status", s.traefikStatus)
			r.Get("/fleet/status", s.fleetStatus)
			r.Get("/fleet/last-sync", s.fleetLastSync)
			r.Get("/fleet/autosync", s.getAutoSync)
			r.Put("/fleet/autosync", s.putAutoSync)
			r.Post("/config/sync", s.configSync)
			r.Post("/logout", s.logout)
			r.Get("/sse", s.sse)
		})
	})

	if ui != nil {
		r.Handle("/*", spaHandler(ui))
	}
	return r
}

// requireAuth gates control-plane endpoints on the FRONTDESK_TOKEN or a session
// token (when TOTP is on, the raw token is a first factor only).
func (s *Server) requireAuth(next http.Handler) http.Handler {
	return adminauth.RequireAdminOrSession(s.adminMgr, s.sessionMgr, s.totpStatus.Enabled, next)
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

// ---------------------------------------------------------------------------
// Members
// ---------------------------------------------------------------------------

// memberView is a member plus its live poller status for the Members tab.
type memberView struct {
	*Member
	Status MemberStatus `json:"status"`
}

func (s *Server) listMembers(w http.ResponseWriter, r *http.Request) {
	members, err := s.store.ListMembers(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	snap := s.poller.Snapshot()
	views := make([]memberView, len(members))
	for i, m := range members {
		views[i] = memberView{Member: m, Status: snap[m.ID]}
	}
	writeJSON(w, http.StatusOK, views)
}

type createMemberRequest struct {
	Name  string `json:"name"`
	URL   string `json:"url"`
	Token string `json:"token"`
}

// memberResponse is a Member plus an optional, non-fatal warning surfaced after
// an add/edit when the admin token could not be confirmed (the member was
// offline, or answered without a 200). The frontend toasts token_warning when
// it is present; a token the member positively refused is a 400 instead.
type memberResponse struct {
	*Member
	TokenWarning string `json:"token_warning,omitempty"`
}

func (s *Server) createMember(w http.ResponseWriter, r *http.Request) {
	var req createMemberRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	m, err := s.store.CreateMember(r.Context(), req.Name, req.URL, req.Token)
	if err != nil {
		writeError(w, err)
		return
	}
	// Verify the token against the (now canonical) member URL before announcing
	// the add. A token the member refuses is a typo: roll the add back so the
	// operator fixes it now instead of discovering a dead member later.
	var tokenWarning string
	if req.Token != "" {
		p := s.probeMemberToken(r.Context(), m.URL, req.Token)
		if p.rejected() {
			if delErr := s.store.DeleteMember(r.Context(), m.ID); delErr != nil {
				// Rollback failed: the member is stranded with the rejected token, so a
				// retry would hit the duplicate-URL constraint. Tell the operator to
				// remove it by hand rather than leaving a silent inconsistency.
				http.Error(w, fmt.Sprintf("This member rejected the admin token (HTTP %d) and rolling back the add failed (%v). Remove it from the Members list and try again.", p.status, delErr), http.StatusInternalServerError)
				return
			}
			http.Error(w, fmt.Sprintf("This member rejected the admin token (HTTP %d). Double-check the token and try again.", p.status), http.StatusBadRequest)
			return
		}
		tokenWarning = p.warning()
		// A newly added member with a valid token is stale relative to the primary;
		// re-arm auto-sync so the next tick brings it in line (no-op when disabled).
		s.rearmAutoSync(r.Context())
	}
	s.emit(r.Context(), Event{
		Type: "member.added", Severity: "info", Source: "frontdesk",
		Message: m.Name + " added", MemberID: m.ID,
		Metadata: map[string]any{"url": m.URL},
	})
	writeJSON(w, http.StatusCreated, memberResponse{Member: m, TokenWarning: tokenWarning})
}

type patchMemberRequest struct {
	Name  *string `json:"name,omitempty"`
	Token *string `json:"token,omitempty"` // "" clears the stored token
}

func (s *Server) patchMember(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req patchMemberRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Name != nil {
		if err := s.store.RenameMember(r.Context(), id, *req.Name); err != nil {
			writeError(w, err)
			return
		}
	}
	var tokenWarning string
	if req.Token != nil {
		// Verify a non-empty new token before storing it, so a refused token is
		// rejected now rather than persisted. Clearing the token ("") never probes.
		if *req.Token != "" {
			m0, err := s.store.GetMember(r.Context(), id)
			if err != nil {
				writeError(w, err)
				return
			}
			p := s.probeMemberToken(r.Context(), m0.URL, *req.Token)
			if p.rejected() {
				http.Error(w, fmt.Sprintf("This member rejected the admin token (HTTP %d). Double-check the token and try again.", p.status), http.StatusBadRequest)
				return
			}
			tokenWarning = p.warning()
		}
		if err := s.store.SetMemberToken(r.Context(), id, *req.Token); err != nil {
			writeError(w, err)
			return
		}
		if *req.Token != "" {
			// The member just gained an admin token: it is now syncable but stale, so
			// re-arm auto-sync to converge it (no-op when disabled).
			s.rearmAutoSync(r.Context())
		}
	}
	m, err := s.store.GetMember(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, memberResponse{Member: m, TokenWarning: tokenWarning})
}

func (s *Server) deleteMember(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	m, err := s.store.GetMember(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := s.store.DeleteMember(r.Context(), id); err != nil {
		writeError(w, err)
		return
	}
	s.emit(r.Context(), Event{
		Type: "member.removed", Severity: "info", Source: "frontdesk",
		Message: m.Name + " removed", MemberID: m.ID,
	})
	w.WriteHeader(http.StatusNoContent)
}

type memberStateRequest struct {
	State MemberState `json:"state"`
}

func (s *Server) setMemberState(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req memberStateRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := s.store.SetMemberState(r.Context(), id, req.State); err != nil {
		writeError(w, err)
		return
	}
	m, err := s.store.GetMember(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	severity := "info"
	if req.State == StateDrained {
		severity = "warning"
	}
	s.emit(r.Context(), Event{
		Type: "member.state_changed", Severity: severity, Source: "frontdesk",
		Message: m.Name + " set to " + string(req.State), MemberID: m.ID,
		Metadata: map[string]any{"state": string(req.State)},
	})
	writeJSON(w, http.StatusOK, m)
}

// ---------------------------------------------------------------------------
// Settings
// ---------------------------------------------------------------------------

func (s *Server) getSettings(w http.ResponseWriter, r *http.Request) {
	set, err := s.store.GetSettings(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	// Never expose the encrypted Apprise target: replace a stored secret with a
	// mask the UI can echo back unchanged to preserve it.
	if set.AlertAppriseTargets != "" {
		set.AlertAppriseTargets = alertMaskValue
	}
	writeJSON(w, http.StatusOK, set)
}

func (s *Server) putSettings(w http.ResponseWriter, r *http.Request) {
	// Partial-merge onto the current row: decode the request body on top of the
	// stored settings so a field the caller omits is preserved, not zeroed. The
	// polling form and the Alerts panel each PUT only the fields they own, so
	// neither clobbers the other; and an older client that never sends the alert
	// fields can no longer wipe the encrypted target. The target is presented as
	// the mask first, so an omitted or echoed value both resolve to "preserve"
	// (and the raw ciphertext is never re-encrypted into itself).
	//
	// The read-merge-write is serialized: two concurrent PUTs (e.g. both panels at
	// once) must not both read the same snapshot and have the later write restore
	// the other's pre-merge values. putSettings is the only writer of this row.
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()

	set, err := s.store.GetSettings(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	if set.AlertAppriseTargets != "" {
		set.AlertAppriseTargets = alertMaskValue
	}
	if !decodeJSON(w, r, &set) {
		return
	}
	// Resolve the Apprise target secret before storing: a masked submission keeps
	// the existing ciphertext, a new value is encrypted at rest, a blank clears it.
	resolved, err := s.resolveAlertTarget(r.Context(), set.AlertAppriseTargets)
	if err != nil {
		writeError(w, err)
		return
	}
	set.AlertAppriseTargets = resolved

	if err := s.store.UpdateSettings(r.Context(), set); err != nil {
		writeError(w, err)
		return
	}
	s.emit(r.Context(), Event{
		Type: "settings.changed", Severity: "info", Source: "frontdesk",
		Message: "Settings updated",
	})
	// Re-mask before echoing the saved settings back to the client.
	if set.AlertAppriseTargets != "" {
		set.AlertAppriseTargets = alertMaskValue
	}
	writeJSON(w, http.StatusOK, set)
}

// resolveAlertTarget maps a submitted Apprise target field to the value to store:
// the mask sentinel preserves the stored ciphertext, a blank clears it, and any
// other value is encrypted at rest with the Front Desk master key.
func (s *Server) resolveAlertTarget(ctx context.Context, submitted string) (string, error) {
	switch submitted {
	case alertMaskValue:
		cur, err := s.store.GetSettings(ctx)
		if err != nil {
			return "", err
		}
		return cur.AlertAppriseTargets, nil
	case "":
		return "", nil
	default:
		return auth.EncryptString(submitted, s.masterKey)
	}
}

// getAutoSync returns the automatic config-propagation setup (enabled + the
// designated primary). The internal drift hash is never included.
func (s *Server) getAutoSync(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.store.GetAutoSync(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

// putAutoSync sets the auto-sync toggle and designated primary. Enabling without
// a primary, or naming a primary that is unknown or has no stored admin token, is
// rejected: the loop could not authenticate to pull its config, so the choice
// would silently do nothing.
//
// Repointing or clearing an already-configured primary is high-impact (it changes
// which instance's config gets pushed across the whole fleet), so it is gated on a
// fresh admin-token confirmation. The bearer may be a passkey/TOTP session token
// rather than the raw FRONTDESK_TOKEN, so the check happens here against AdminMgr
// rather than client-side. The first primary selection (none configured yet) and
// changes that leave the primary untouched (e.g. just toggling enabled) need no
// confirmation.
func (s *Server) putAutoSync(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enabled      bool   `json:"enabled"`
		PrimaryID    string `json:"primary_id"`
		ConfirmToken string `json:"confirm_token"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.PrimaryID != "" {
		m, err := s.store.GetMember(r.Context(), req.PrimaryID)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				http.Error(w, "that primary is not a known member", http.StatusBadRequest)
				return
			}
			writeError(w, err)
			return
		}
		if !m.HasToken {
			http.Error(w, "store an admin token for that primary first; auto-sync needs it to read the primary's config", http.StatusBadRequest)
			return
		}
	} else if req.Enabled {
		http.Error(w, "choose a primary before enabling auto-sync", http.StatusBadRequest)
		return
	}
	// Repointing or clearing an already-configured primary is high-impact (it
	// changes which instance's config the whole fleet copies), so it requires a
	// valid admin token. The bearer here may be a passkey/TOTP session token
	// rather than the raw FRONTDESK_TOKEN, so the operator re-supplies the token
	// in the body and we check it against AdminMgr. The guard is enforced inside
	// the same UPDATE that writes (SetAutoSyncGuarded), so there is no
	// read-modify-write window for a concurrent repoint to slip through.
	tokenValid := s.adminMgr.Validate(strings.TrimSpace(req.ConfirmToken))
	applied, err := s.store.SetAutoSyncGuarded(r.Context(), req.Enabled, req.PrimaryID, tokenValid)
	if err != nil {
		writeError(w, err)
		return
	}
	if !applied {
		// The change would repoint/clear a configured primary without a valid
		// token (or lost a concurrent repoint). No secrets are logged, only that a
		// confirmation-required change was refused.
		debuglog.Warn("frontdesk: refused unconfirmed auto-sync primary change", "to", req.PrimaryID)
		http.Error(w, "confirm the admin token to change or clear the configured primary", http.StatusForbidden)
		return
	}
	s.emit(r.Context(), Event{
		Type: "settings.changed", Severity: "info", Source: "frontdesk",
		Message: fmt.Sprintf("Auto-sync %s", enabledWord(req.Enabled)),
	})
	cfg, err := s.store.GetAutoSync(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	// When auto-sync is left on, converge the fleet right away instead of waiting
	// up to two ticks for the loop: the operator opted in deliberately, so this is
	// the "sync now" they expect from the toggle. Detached from the request
	// context (which ends when we respond) but time-bounded so a stuck pass cannot
	// leak the goroutine. A no-op when nothing has drifted; the loop still owns the
	// steady-state watch. Disabling (or no primary) never kicks.
	if cfg.Enabled && cfg.PrimaryID != "" {
		kickCtx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), autoSyncKickTimeout)
		go func() {
			defer cancel()
			s.forceAutoSyncNow(kickCtx)
		}()
	}
	writeJSON(w, http.StatusOK, cfg)
}

func enabledWord(b bool) string {
	if b {
		return "enabled"
	}
	return "disabled"
}

// rearmAutoSync clears the last-applied config hash so the auto-sync loop runs a
// full convergence pass on its next tick. The loop's fast path skips work when the
// primary's hash is unchanged, so a member that becomes newly syncable (just
// added, or just given an admin token) would otherwise stay stale until the
// primary's config next changed. Clearing the marker re-arms the loop; members
// already matching the primary are still skipped by their own dry-run diff, so the
// re-run only syncs the one(s) that actually need it. It is a no-op in effect when
// auto-sync is disabled (the loop reads the marker but does nothing).
func (s *Server) rearmAutoSync(ctx context.Context) {
	if err := s.store.SetAutoSyncLastHash(ctx, ""); err != nil {
		debuglog.Warn("frontdesk: re-arm auto-sync after membership change", "error", err)
	}
}

// ---------------------------------------------------------------------------
// Events
// ---------------------------------------------------------------------------

func (s *Server) listEvents(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := EventFilter{
		MemberID: q.Get("member_id"),
		Type:     q.Get("type"),
		Severity: q.Get("severity"),
		Since:    parseRFC3339(q.Get("since")),
		Until:    parseRFC3339(q.Get("until")),
		Limit:    clampEventsLimit(atoiDefault(q.Get("limit"), defaultEventsLimit)),
		Offset:   max(atoiDefault(q.Get("offset"), 0), 0),
	}
	evs, total, err := s.store.ListEvents(r.Context(), f)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": evs, "total": total})
}

// ---------------------------------------------------------------------------
// Status + SSE
// ---------------------------------------------------------------------------

func (s *Server) traefikStatus(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.poller.Snapshot())
}

// buildCommit is the source commit SHA this Front Desk binary was built from,
// stamped at build time via -ldflags -X (see the Makefile / Dockerfile.frontdesk)
// and surfaced read-only as app_commit so the UI footer can show which commit a
// `dev` build corresponds to. Defaults to "unknown" for un-stamped builds.
var buildCommit = "unknown"

// shortCommit normalizes a stamped commit SHA to a fixed-length short prefix so
// app_commit reads the same across build paths (a local git SHA vs CI's full
// github.sha). The "unknown" sentinel and any empty value pass through unchanged.
// It mirrors internal/api.shortCommit so both surfaces present the same prefix.
func shortCommit(c string) string {
	const shortLen = 12
	if c == "" || c == "unknown" {
		return c
	}
	if len(c) > shortLen {
		return c[:shortLen]
	}
	return c
}

// getVersion returns the running build's version and source commit so the UI
// footer can show which Front Desk build is deployed (and link a `dev` build to
// its commit on GitHub). app_version is "dev" for un-stamped builds; app_commit
// is normalized to a short prefix so it reads the same across build paths.
func (s *Server) getVersion(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"app_version": s.version,
		"app_commit":  shortCommit(buildCommit),
	})
}

// sseHeartbeat keeps idle SSE connections alive through proxies.
const sseHeartbeat = 25 * time.Second

func (s *Server) sse(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	ch := s.bus.Subscribe()
	defer s.bus.Unsubscribe(ch)

	ticker := time.NewTicker(sseHeartbeat)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			if _, err := w.Write([]byte(": keep-alive\n\n")); err != nil {
				return
			}
			flusher.Flush()
		case ev, ok := <-ch:
			if !ok {
				return
			}
			data, err := json.Marshal(ev)
			if err != nil {
				continue
			}
			if _, err := w.Write([]byte("data: " + string(data) + "\n\n")); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// ---------------------------------------------------------------------------
// TOTP-enabled cache (mirrors the main server's cache so the gate stays DB-free
// on the hot path)
// ---------------------------------------------------------------------------

// totpStatusReader is the one method of *totp.Repository the cache depends on.
// It is an interface so the fail-closed behaviour on a read error is testable
// without a live database.
type totpStatusReader interface {
	IsEnabled(ctx context.Context) (bool, error)
}

type totpEnabledCache struct {
	repo totpStatusReader
	val  atomic.Bool
}

func newTotpEnabledCache(repo totpStatusReader) *totpEnabledCache {
	c := &totpEnabledCache{repo: repo}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	enabled, err := repo.IsEnabled(ctx)
	if err != nil {
		// Fail closed: treat as enabled so a startup DB blip cannot silently
		// weaken the gate.
		debuglog.Error("frontdesk: seeding TOTP-enabled cache failed, failing closed", "error", err)
		enabled = true
	}
	c.val.Store(enabled)
	return c
}

func (c *totpEnabledCache) Enabled() bool { return c.val.Load() }

func (c *totpEnabledCache) Refresh(ctx context.Context) {
	enabled, err := c.repo.IsEnabled(ctx)
	if err != nil {
		// Fail closed, matching the main server's RefreshTotpEnabled: a failed
		// re-read must never leave a stale "disabled" cached, which would let a
		// raw FRONTDESK_TOKEN through as a full session after TOTP was enabled.
		debuglog.Error("frontdesk: refreshing TOTP-enabled cache failed, failing closed", "error", err)
		c.val.Store(true)
		return
	}
	c.val.Store(enabled)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// emit persists a control-plane event and publishes it on the SSE bus.
func (s *Server) emit(ctx context.Context, e Event) {
	stored, err := s.store.InsertEvent(ctx, e)
	if err != nil {
		debuglog.Warn("frontdesk: persist event", "type", e.Type, "error", err)
		stored = e
	}
	s.bus.Publish(busEvent(stored))
}

// busEvent maps a stored Front Desk Event to a bus event. When the event concerns
// a member, its MemberID is copied into the metadata as "member_id" (on a copy, so
// the persisted metadata is untouched) so the alert dispatcher debounces per
// member. Shared by Server.emit and Poller.recordEvent.
func busEvent(e Event) events.Event {
	meta := e.Metadata
	if e.MemberID != "" {
		meta = make(map[string]any, len(e.Metadata)+1)
		for k, v := range e.Metadata {
			meta[k] = v
		}
		meta["member_id"] = e.MemberID
	}
	return events.Event{
		ID: e.ID, Type: e.Type, Severity: e.Severity, Source: e.Source,
		Message: e.Message, Metadata: meta, Timestamp: e.CreatedAt,
	}
}

// RunAlerts runs the outbound Apprise dispatcher until ctx is cancelled. Started
// as a goroutine in cmd/frontdesk; best-effort, never blocks request serving.
func (s *Server) RunAlerts(ctx context.Context) { s.alertDisp.Run(ctx) }

// alertEvents serves the Front Desk alert catalog so the UI renders its picker.
func (s *Server) alertEvents(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, fdCatalog)
}

// alertStatus reports whether the configured apprise-api is reachable. A
// reachable host is not enough: if the stored target cannot be decrypted (master
// key rotated, ciphertext corrupted) every dispatch fails silently, so that is
// surfaced as unhealthy with a reason rather than a falsely green pill.
func (s *Server) alertStatus(w http.ResponseWriter, r *http.Request) {
	st, err := s.alertDisp.Probe(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	if st.Configured {
		if set, gerr := s.store.GetSettings(r.Context()); gerr == nil {
			switch set.AlertAppriseTargets {
			case "":
				// A reachable apprise-api with no target still cannot deliver, so it
				// must not show a green pill.
				st.Healthy = false
				st.Detail = "no notification target configured"
			default:
				if _, derr := auth.DecryptString(set.AlertAppriseTargets, s.masterKey); derr != nil {
					st.Healthy = false
					st.Detail = "stored target cannot be decrypted (master key rotated?)"
				}
			}
		}
	}
	writeJSON(w, http.StatusOK, st)
}

// alertTest sends a test notification to the configured target(s). A delivery or
// configuration failure is reported as 502 with the reason, so the UI can show it.
func (s *Server) alertTest(w http.ResponseWriter, r *http.Request) {
	if err := s.alertDisp.TestSend(r.Context()); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		debuglog.Error("frontdesk: encode response", "error", err)
	}
}

// writeError maps store errors to HTTP status codes.
func writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		http.Error(w, err.Error(), http.StatusNotFound)
	case errors.Is(err, ErrValidation), errors.Is(err, ErrDuplicateURL):
		http.Error(w, err.Error(), http.StatusBadRequest)
	default:
		debuglog.Error("frontdesk: request failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

// decodeJSON decodes the request body, writing a 400 and returning false on
// failure.
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return false
	}
	return true
}

func atoiDefault(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}

// Event listing page-size bounds. A request with no/blank limit gets the
// default; a non-positive limit would otherwise disable the store's LIMIT clause
// (unbounded query), and an over-large one could return the whole table, so both
// ends are clamped here.
const (
	defaultEventsLimit = 100
	maxEventsLimit     = 500
)

// clampEventsLimit forces an events page size into [1, maxEventsLimit].
func clampEventsLimit(n int) int {
	if n < 1 {
		return defaultEventsLimit
	}
	if n > maxEventsLimit {
		return maxEventsLimit
	}
	return n
}

// parseRFC3339 parses an RFC3339 timestamp from a query value, returning the
// zero time (which EventFilter treats as "no bound") when empty or malformed.
func parseRFC3339(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

// spaHandler serves the embedded single-page app, falling back to index.html for
// client-side routes (any path without a file extension that is not found).
func spaHandler(ui fs.FS) http.Handler {
	fileServer := http.FileServer(http.FS(ui))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// fs.ValidPath + the embedded FS are the traversal boundary: "../" or an
		// absolute name is rejected here and falls through to the SPA index, and
		// http.FileServer additionally cleans the path it serves. Only serve a
		// concrete asset when it exists and the name is valid.
		name := strings.TrimPrefix(r.URL.Path, "/")
		if name != "" && fs.ValidPath(name) {
			if _, err := fs.Stat(ui, name); err == nil {
				fileServer.ServeHTTP(w, r)
				return
			}
		}
		// Root, invalid, or unknown path: serve index.html so the SPA router can
		// handle the route client-side.
		r2 := r.Clone(r.Context())
		r2.URL.Path = "/"
		fileServer.ServeHTTP(w, r2)
	})
}
