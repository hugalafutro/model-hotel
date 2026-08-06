package api

import (
	"net/http"

	"github.com/hugalafutro/model-hotel/internal/authcookie"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/events"
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
func (h *Handler) RevokeOtherSessions(w http.ResponseWriter, r *http.Request) {
	if h.webauthnSessionMgr == nil {
		respondError(w, "session management is not configured", nil, http.StatusPreconditionFailed)
		return
	}

	tok, ok := authcookie.SessionToken(r)
	if !ok {
		tok, _ = util.ParseBearerToken(r)
	}

	// The identity to fall back on when the request carried the raw admin token
	// instead of a session. Only the admin login has that shape; a user login
	// always arrives as a session, so its identity comes from the session row.
	revoked, err := h.webauthnSessionMgr.RevokeOtherSessions(r.Context(), tok, []byte("admin"))
	if err != nil {
		respondError(w, "failed to revoke other sessions", err, http.StatusInternalServerError)
		return
	}

	debuglog.Info("auth: revoked other sessions", "count", revoked)
	if revoked > 0 {
		events.Publish(events.Event{
			Type:     "auth.sessions_revoked",
			Severity: "info",
			Source:   "auth",
			Message:  "Signed out " + util.Count(int(revoked), "other session", "other sessions"),
			Metadata: map[string]any{"revoked": revoked},
		})
	}

	writeJSON(w, revokeOthersResult{Revoked: revoked})
}
