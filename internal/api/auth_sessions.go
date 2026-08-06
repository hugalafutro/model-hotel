package api

import (
	"net/http"

	"github.com/hugalafutro/model-hotel/internal/authcookie"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/events"
	"github.com/hugalafutro/model-hotel/internal/user"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// revokeOthersResult reports how many sessions the call ended.
type revokeOthersResult struct {
	Revoked int64 `json:"revoked"`
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

	id := user.IdentityFrom(r.Context())
	if id == nil {
		respondError(w, "unauthenticated", nil, http.StatusUnauthorized)
		return
	}

	// The session handle this identity's sessions are minted under: a users-row
	// account is keyed by its UUID string, the env-token and legacy admin by
	// "admin". Same mapping CreateAuthToken uses at every login front-end.
	identity := []byte("admin")
	if id.UserID != nil {
		identity = []byte(id.UserID.String())
	}

	// Both credentials a request can carry. Whichever one belongs to this
	// identity names the session to spare; one that does not belong to it
	// spares nothing, so a foreign or junk token can never redirect the revoke.
	cookieTok, _ := authcookie.SessionToken(r)
	bearerTok, _ := util.ParseBearerToken(r)

	revoked, err := h.webauthnSessionMgr.RevokeOtherSessions(r.Context(), identity, cookieTok, bearerTok)
	if err != nil {
		respondError(w, "failed to revoke other sessions", err, http.StatusInternalServerError)
		return
	}

	debuglog.Info("auth: revoked other sessions", "count", revoked, "identity", id.Username)
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
