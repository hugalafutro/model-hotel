package api

import (
	"net/http"

	"github.com/hugalafutro/model-hotel/internal/util"
	"github.com/hugalafutro/model-hotel/internal/webauthn"
)

// requireAdminOrSession wraps next so the request proceeds only when the bearer
// token is either the raw admin token or a valid WebAuthn/TOTP session token.
//
// Raw admin token is accepted ONLY when TOTP is disabled (mirrors
// Handler.AuthMiddleware). With TOTP on, the raw admin token is a first factor
// only and must not unlock admin-gated endpoints (passkey/TOTP management), or a
// bare admin-token bearer could bypass the second factor.
//
// This is the single implementation shared by WebAuthnHandler and TotpHandler,
// which previously each carried an identical copy.
func requireAdminOrSession(
	adminMgr AdminAuthenticator,
	sessionMgr *webauthn.SessionManager,
	totpEnabled func() bool,
	next http.Handler,
) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := util.ParseBearerToken(r)
		if !ok {
			http.Error(w, "Authorization header required (Bearer token)", http.StatusUnauthorized)
			return
		}

		if (totpEnabled == nil || !totpEnabled()) && adminMgr.Validate(token) {
			next.ServeHTTP(w, r)
			return
		}

		if sessionMgr != nil && sessionMgr.Validate(r.Context(), token) {
			next.ServeHTTP(w, r)
			return
		}

		http.Error(w, "Invalid admin token or session token", http.StatusUnauthorized)
	})
}
