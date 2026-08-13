package api

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/authcookie"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/events"
	"github.com/hugalafutro/model-hotel/internal/user"
	"github.com/hugalafutro/model-hotel/internal/util"
	"github.com/hugalafutro/model-hotel/internal/webauthn"
)

// revokeOthersResult reports how many sessions the call ended.
type revokeOthersResult struct {
	Revoked int64 `json:"revoked"`
}

// listSessionsResult wraps the active-sessions rows.
type listSessionsResult struct {
	Sessions []webauthn.AuthSessionInfo `json:"sessions"`
}

// sessionCallerContext resolves who the caller is and which credentials their
// request carried, shared by every session-hygiene handler. The identity comes
// from the auth middleware, never from a credential read back off the request:
// the two can disagree (a valid bearer alongside a junk cookie), and the
// middleware's answer is the one that authenticated. ok is false when the
// request somehow reached the handler unauthenticated.
func sessionCallerContext(r *http.Request) (identity []byte, candidates []string, ok bool) {
	id := user.IdentityFrom(r.Context())
	if id == nil {
		return nil, nil, false
	}

	// The session handle this identity's sessions are minted under: a users-row
	// account is keyed by its UUID string, the env-token and legacy admin by
	// "admin". Same mapping CreateAuthToken uses at every login front-end.
	identity = []byte("admin")
	if id.UserID != nil {
		identity = []byte(id.UserID.String())
	}

	cookieTok, _ := authcookie.SessionToken(r)
	bearerTok, _ := util.ParseBearerToken(r)
	return identity, []string{cookieTok, bearerTok}, true
}

// ListAuthSessions returns the caller's live sessions for the settings panel:
// device metadata, timestamps, and which row is the calling session. Identity
// scoping happens in the manager off the middleware-resolved identity, so a
// caller can only ever see their own sessions.
func (h *Handler) ListAuthSessions(w http.ResponseWriter, r *http.Request) {
	if h.webauthnSessionMgr == nil {
		respondError(w, "session management is not configured", nil, http.StatusPreconditionFailed)
		return
	}
	identity, candidates, ok := sessionCallerContext(r)
	if !ok {
		respondError(w, "unauthenticated", nil, http.StatusUnauthorized)
		return
	}

	sessions, err := h.webauthnSessionMgr.ListAuthSessions(r.Context(), identity, candidates...)
	if err != nil {
		respondError(w, "failed to list sessions", err, http.StatusInternalServerError)
		return
	}
	if sessions == nil {
		sessions = []webauthn.AuthSessionInfo{}
	}
	writeJSON(w, listSessionsResult{Sessions: sessions})
}

// RevokeAuthSessionByID signs one of the caller's sessions out by id: the
// per-row action of the active-sessions list. Missing and foreign ids both
// read as 404 (a distinct answer would confirm the id exists); the session the
// request rides on is a 409, since ending it is what logout is for.
func (h *Handler) RevokeAuthSessionByID(w http.ResponseWriter, r *http.Request) {
	if h.webauthnSessionMgr == nil {
		respondError(w, "session management is not configured", nil, http.StatusPreconditionFailed)
		return
	}
	identity, candidates, ok := sessionCallerContext(r)
	if !ok {
		respondError(w, "unauthenticated", nil, http.StatusUnauthorized)
		return
	}

	sessionID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, "invalid session id", nil, http.StatusBadRequest)
		return
	}

	switch err := h.webauthnSessionMgr.RevokeSessionByID(r.Context(), identity, sessionID, candidates...); {
	case err == nil:
	case errors.Is(err, webauthn.ErrCurrentSession):
		respondError(w, "cannot sign out the current session; use logout instead", nil, http.StatusConflict)
		return
	case errors.Is(err, webauthn.ErrNotFound):
		respondError(w, "session not found", nil, http.StatusNotFound)
		return
	default:
		respondError(w, "failed to revoke session", err, http.StatusInternalServerError)
		return
	}

	id := user.IdentityFrom(r.Context())
	debuglog.Info("auth: revoked session by id", "session_id", sessionID, "identity", id.Username)
	events.Publish(events.Event{
		Type:     "auth.sessions_revoked",
		Severity: "info",
		Source:   "auth",
		Message:  "Signed out 1 other session",
		// No username here: /events is readable by any authenticated caller,
		// and the name is already in the debug log where it belongs.
		Metadata: map[string]any{"revoked": 1},
	})
	w.WriteHeader(http.StatusNoContent)
}

// RevokeOtherSessions signs the caller's other sessions out, keeping the one
// this request was made from.
//
// This is the operator's lever for the gap the session TTL only bounds: logging
// in does not revoke an existing session, so a stolen token stays usable until
// it expires. Making it an explicit action rather than an automatic
// revoke-on-login is deliberate. Three admin login front-ends mint sessions
// under one shared identity, so automatic revocation would evict the operator's
// other devices on every routine login, training them to ignore it. Here they
// ask for it, and it happens when they mean it.
//
// Whose sessions are ended comes from the identity the auth middleware
// resolved, never from a credential read back off the request. Those two can
// disagree: resolveCredentials falls through to the bearer when the session
// cookie is invalid, so a caller presenting a valid bearer alongside a junk
// cookie authenticates as themselves while the junk cookie would have decided
// the target. Reading the identity from the request is how any authenticated
// account could have aimed this at the admin handle and signed every admin
// session out.
func (h *Handler) RevokeOtherSessions(w http.ResponseWriter, r *http.Request) {
	if h.webauthnSessionMgr == nil {
		respondError(w, "session management is not configured", nil, http.StatusPreconditionFailed)
		return
	}

	// Both credentials a request can carry. Whichever one belongs to this
	// identity names the session to spare; one that does not belong to it
	// spares nothing, so a foreign or junk token can never redirect the revoke.
	identity, candidates, ok := sessionCallerContext(r)
	if !ok {
		respondError(w, "unauthenticated", nil, http.StatusUnauthorized)
		return
	}

	revoked, err := h.webauthnSessionMgr.RevokeOtherSessions(r.Context(), identity, candidates...)
	if err != nil {
		respondError(w, "failed to revoke other sessions", err, http.StatusInternalServerError)
		return
	}

	debuglog.Info("auth: revoked other sessions", "count", revoked, "identity", user.IdentityFrom(r.Context()).Username)
	if revoked > 0 {
		events.Publish(events.Event{
			Type:     "auth.sessions_revoked",
			Severity: "info",
			Source:   "auth",
			Message:  "Signed out " + util.Count(int(revoked), "other session", "other sessions"),
			// No username here: /events is readable by any authenticated
			// caller, and the name is already in the debug log and the audit
			// trail where it belongs.
			Metadata: map[string]any{"revoked": revoked},
		})
	}

	writeJSON(w, revokeOthersResult{Revoked: revoked})
}
