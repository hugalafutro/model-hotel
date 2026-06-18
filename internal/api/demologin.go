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
// The token comes from the admin manager (the actually-active token), not the
// ADMIN_TOKEN env var, so a rotated or auto-generated token is reported
// correctly. Responses are marked no-store so a proxy or browser never retains
// the credential after it is rotated or the feature is turned off.
func (h *Handler) GetDemoLogin(w http.ResponseWriter, _ *http.Request) {
	var token string
	if h.cfg.DemoShowToken && h.cfg.DemoReadOnly {
		token = h.adminMgr.Token()
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	writeJSON(w, DemoLoginResponse{Token: token})
}
