// Package adminauth holds the admin authentication HTTP surface shared by the
// main server and the HA "Front Desk" control plane: the WebAuthn/passkey and
// TOTP ceremony handlers plus the admin-or-session gate they share.
//
// The handlers depend only on interfaces (webauthn.Store, AdminAuthenticator,
// IPLimiterMiddleware) and the webauthn/totp domain packages, never on a
// database driver, so the same audited code backs Postgres (main server) and
// SQLite (Front Desk). The small response/guard helpers below are thin wrappers
// over internal/httpx, the neutral leaf package that holds their single body;
// they exist only to pin this package's log prefix and keep the call sites
// short.
package adminauth

import (
	"net/http"

	"github.com/hugalafutro/model-hotel/internal/httpx"
)

// AdminAuthenticator validates the raw admin token. Implemented by
// *admin.Manager (main server: ADMIN_TOKEN; Front Desk: FRONTDESK_TOKEN).
type AdminAuthenticator interface {
	Validate(token string) bool
}

// IPLimiterMiddleware is the per-IP rate-limiting middleware used on the public
// login routes, plus a trusted-proxy-aware client-IP extractor for keying the
// per-IP login backoff.
type IPLimiterMiddleware interface {
	Middleware(next http.Handler) http.Handler
	ClientIP(r *http.Request) string
}

// logComponent is the log prefix every helper in this package writes.
const logComponent = "adminauth"

// writeJSON encodes v as JSON.
func writeJSON(w http.ResponseWriter, v any) {
	httpx.WriteJSON(w, logComponent, v)
}

// respondError writes an error response, logging server faults.
func respondError(w http.ResponseWriter, message string, err error, code int) {
	httpx.RespondError(w, logComponent, message, err, code)
}

// respondBadRequest writes a 400 response, logging the cause server-side.
func respondBadRequest(w http.ResponseWriter, message string, err error) {
	httpx.RespondBadRequest(w, logComponent, message, err)
}

// readOnlyGuard refuses mutating requests in demo read-only mode. Only the
// webauthn/logout exemption is reachable from this package's routes.
func readOnlyGuard(next http.Handler) http.Handler {
	return httpx.ReadOnlyGuard(logComponent, next)
}
