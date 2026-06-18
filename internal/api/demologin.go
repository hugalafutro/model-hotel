package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// DemoLoginResponse carries the admin token to display on the login screen of a
// public demo instance, so operators can share only the URL. The token is a
// secret, so unlike PublicConfigResponse this is served by a dedicated endpoint
// that is only ever populated when the demo gating below is satisfied.
type DemoLoginResponse struct {
	Token string `json:"token"`
}

// RegisterDemoLogin mounts the unauthenticated demo-login route. It is
// registered outside the admin-auth group (see cmd/server) so the SPA can read
// it on the login screen, exactly like RegisterPublicConfig.
func (h *Handler) RegisterDemoLogin(r chi.Router) {
	r.Get("/demo-login", h.GetDemoLogin)
}

// GetDemoLogin returns the admin token for the login screen of a demo instance,
// or an empty token when the feature is disabled. The token is exposed only when
// DEMO_SHOW_TOKEN and DEMO_READONLY are both set (publishing the admin
// credential is acceptable only when every admin mutation is already refused).
// The response is always 200 with an empty token when disabled, keeping the
// frontend gate trivial.
//
// It publishes the configured ADMIN_TOKEN only when the admin manager actually
// accepts it. Validate works whether the token is held in plaintext or only as
// a stored hash (the normal case after a restart from a persisted volume, where
// the manager no longer has the plaintext), and returns false when ADMIN_TOKEN
// is unset or was changed without clearing the token file, so we never advertise
// a token that would fail to log in. Responses are marked no-store so a proxy or
// browser never retains the credential after rotation or after the feature is
// turned off.
func (h *Handler) GetDemoLogin(w http.ResponseWriter, _ *http.Request) {
	var token string
	if h.cfg.DemoShowToken && h.cfg.DemoReadOnly && h.cfg.AdminToken != "" &&
		h.adminMgr.Validate(h.cfg.AdminToken) {
		token = h.cfg.AdminToken
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	writeJSON(w, DemoLoginResponse{Token: token})
}
