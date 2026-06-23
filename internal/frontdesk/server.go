package frontdesk

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"
	gowa "github.com/go-webauthn/webauthn/webauthn"

	"github.com/hugalafutro/model-hotel/internal/admin"
	"github.com/hugalafutro/model-hotel/internal/adminauth"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/events"
	"github.com/hugalafutro/model-hotel/internal/totp"
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
}

// Server is the Front Desk HTTP server.
type Server struct {
	store      *Store
	poller     *Poller
	bus        *events.Bus
	adminMgr   *admin.Manager
	sessionMgr *webauthn.SessionManager
	totpRepo   *totp.Repository
	totpStatus *totpEnabledCache
	router     http.Handler
}

// NewServer wires the control-plane HTTP server. It builds the SQLite-backed
// webauthn SessionManager and totp.Repository, seeds the TOTP-enabled cache, and
// mounts the shared adminauth handlers alongside the control-plane REST/SSE
// endpoints and the embedded UI.
func NewServer(cfg ServerConfig) *Server {
	webAuthnStore := NewWebAuthnStore(cfg.Store)
	sessionMgr := webauthn.NewSessionManager(webAuthnStore)
	totpRepo := totp.NewRepositoryWithStore(NewTOTPStore(cfg.Store), cfg.MasterKey)

	s := &Server{
		store:      cfg.Store,
		poller:     cfg.Poller,
		bus:        cfg.Bus,
		adminMgr:   cfg.AdminMgr,
		sessionMgr: sessionMgr,
		totpRepo:   totpRepo,
		totpStatus: newTotpEnabledCache(totpRepo),
	}

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
			r.Get("/settings", s.getSettings)
			r.Put("/settings", s.putSettings)
			r.Get("/events", s.listEvents)
			r.Get("/traefik-status", s.traefikStatus)
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
	s.emit(r.Context(), Event{
		Type: "member.added", Severity: "info", Source: "frontdesk",
		Message: m.Name + " added", MemberID: m.ID,
		Metadata: map[string]any{"url": m.URL},
	})
	writeJSON(w, http.StatusCreated, m)
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
	if req.Token != nil {
		if err := s.store.SetMemberToken(r.Context(), id, *req.Token); err != nil {
			writeError(w, err)
			return
		}
	}
	m, err := s.store.GetMember(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, m)
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
	writeJSON(w, http.StatusOK, set)
}

func (s *Server) putSettings(w http.ResponseWriter, r *http.Request) {
	var set Settings
	if !decodeJSON(w, r, &set) {
		return
	}
	if err := s.store.UpdateSettings(r.Context(), set); err != nil {
		writeError(w, err)
		return
	}
	s.emit(r.Context(), Event{
		Type: "settings.changed", Severity: "info", Source: "frontdesk",
		Message: "Settings updated",
	})
	writeJSON(w, http.StatusOK, set)
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
		Limit:    atoiDefault(q.Get("limit"), 100),
		Offset:   atoiDefault(q.Get("offset"), 0),
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

type totpEnabledCache struct {
	repo *totp.Repository
	val  atomic.Bool
}

func newTotpEnabledCache(repo *totp.Repository) *totpEnabledCache {
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
		debuglog.Warn("frontdesk: refreshing TOTP-enabled cache failed", "error", err)
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
	s.bus.Publish(events.Event{
		ID: stored.ID, Type: stored.Type, Severity: stored.Severity, Source: stored.Source,
		Message: stored.Message, Metadata: stored.Metadata, Timestamp: stored.CreatedAt,
	})
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

// spaHandler serves the embedded single-page app, falling back to index.html for
// client-side routes (any path without a file extension that is not found).
func spaHandler(ui fs.FS) http.Handler {
	fileServer := http.FileServer(http.FS(ui))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if path != "/" {
			if _, err := fs.Stat(ui, path[1:]); err != nil {
				// Unknown path: serve index.html so the SPA router can handle it.
				r2 := r.Clone(r.Context())
				r2.URL.Path = "/"
				fileServer.ServeHTTP(w, r2)
				return
			}
		}
		fileServer.ServeHTTP(w, r)
	})
}
