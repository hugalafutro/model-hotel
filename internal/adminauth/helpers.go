// Package adminauth holds the admin authentication HTTP surface shared by the
// main server and the HA "Front Desk" control plane: the WebAuthn/passkey and
// TOTP ceremony handlers plus the admin-or-session gate they share.
//
// The handlers depend only on interfaces (webauthn.Store, AdminAuthenticator,
// IPLimiterMiddleware) and the webauthn/totp domain packages, never on a
// database driver, so the same audited code backs Postgres (main server) and
// SQLite (Front Desk). The small response/guard helpers below are copied from
// internal/api (their single source before this extraction) to keep this
// package free of an import dependency on api.
package adminauth

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
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

// writeJSON encodes v as JSON. Copied from internal/api/helpers.go.
func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		debuglog.Error("adminauth: failed to encode JSON response", "error", err)
	}
}

// respondError writes an error response, logging server faults. Copied from
// internal/api/helpers.go.
func respondError(w http.ResponseWriter, message string, err error, code int) {
	if err != nil {
		debuglog.Error("adminauth: "+message, "error", err)
	} else if code >= 500 {
		debuglog.Error("adminauth: " + message)
	}
	http.Error(w, message, code)
}

// respondBadRequest writes a 400 response. Copied from internal/api/helpers.go.
func respondBadRequest(w http.ResponseWriter, message string, err error) {
	if err != nil {
		debuglog.Info("adminauth: bad request: "+message, "error", err)
	}
	http.Error(w, message, http.StatusBadRequest)
}

// readOnlyGuard refuses mutating requests in demo read-only mode. Copied from
// internal/api/readonly.go (the logout exemption mirrors that source).
func readOnlyGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
		case http.MethodPost:
			if isReadOnlyExemptPost(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}
			fallthrough
		default:
			respondError(w, "this is a read-only demo — creating, editing, and deleting are disabled", nil, http.StatusForbidden)
		}
	})
}

// isReadOnlyExemptPost lists POST paths allowed in read-only mode. Copied from
// internal/api/readonly.go (only the webauthn/logout case is reachable here).
func isReadOnlyExemptPost(path string) bool {
	return strings.HasSuffix(path, "/discovery/changes/ack") ||
		strings.HasSuffix(path, "/webauthn/logout")
}
