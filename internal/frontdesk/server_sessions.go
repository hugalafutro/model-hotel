package frontdesk

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/authcookie"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/util"
	"github.com/hugalafutro/model-hotel/internal/webauthn"
)

// Session hygiene for the Front Desk web UI, mirroring the main dashboard's
// active-sessions surface over the shared SessionManager. Front Desk has a
// single admin identity, so every handler acts on the "admin" session handle;
// the routes sit in the requireAdmin tier because a paired device is not the
// operator's browser and has no business seeing or ending browser sessions.

// fdAdminIdentity is the session handle every Front Desk login front-end mints
// under: there are no user accounts on the control plane.
var fdAdminIdentity = []byte("admin")

// sessionCandidates returns the credentials this request carried, for telling
// the calling session apart from the rest. A raw admin token matches no
// session row, which is the correct outcome: it has no browser session.
func sessionCandidates(r *http.Request) []string {
	cookieTok, _ := authcookie.FrontDesk.SessionToken(r)
	bearerTok, _ := util.ParseBearerToken(r)
	return []string{cookieTok, bearerTok}
}

// listAuthSessions (GET /api/auth/sessions, admin-only) returns the admin
// identity's live sessions with device metadata, marking the calling one.
func (s *Server) listAuthSessions(w http.ResponseWriter, r *http.Request) {
	sessions, err := s.sessionMgr.ListAuthSessions(r.Context(), fdAdminIdentity, sessionCandidates(r)...)
	if err != nil {
		http.Error(w, "failed to list sessions", http.StatusInternalServerError)
		return
	}
	if sessions == nil {
		sessions = []webauthn.AuthSessionInfo{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": sessions})
}

// revokeAuthSession (DELETE /api/auth/sessions/{id}, admin-only) signs one
// session out. The calling session is refused with a coded 409 — ending it is
// what logout is for — and a missing id is a 404.
func (s *Server) revokeAuthSession(w http.ResponseWriter, r *http.Request) {
	sessionID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeCodedError(w, http.StatusBadRequest, "invalid_session_id", "session id must be a UUID")
		return
	}

	switch err := s.sessionMgr.RevokeSessionByID(r.Context(), fdAdminIdentity, sessionID, sessionCandidates(r)...); {
	case err == nil:
	case errors.Is(err, webauthn.ErrCurrentSession):
		writeCodedError(w, http.StatusConflict, "current_session",
			"cannot sign out the current session; use logout instead")
		return
	case errors.Is(err, webauthn.ErrNotFound):
		writeCodedError(w, http.StatusNotFound, "session_not_found", "session not found")
		return
	default:
		http.Error(w, "failed to revoke session", http.StatusInternalServerError)
		return
	}

	debuglog.Info("frontdesk: revoked session by id", "session_id", sessionID)
	w.WriteHeader(http.StatusNoContent)
}

// revokeOtherSessions (POST /api/auth/sessions/revoke-others, admin-only)
// signs out every admin session except the one this request rides on. On the
// raw admin token nothing is spared, which is the lever an operator wants when
// they suspect a browser session is stolen but still hold the token.
func (s *Server) revokeOtherSessions(w http.ResponseWriter, r *http.Request) {
	revoked, err := s.sessionMgr.RevokeOtherSessions(r.Context(), fdAdminIdentity, sessionCandidates(r)...)
	if err != nil {
		http.Error(w, "failed to revoke other sessions", http.StatusInternalServerError)
		return
	}

	debuglog.Info("frontdesk: revoked other sessions", "count", revoked)
	writeJSON(w, http.StatusOK, map[string]int64{"revoked": revoked})
}
