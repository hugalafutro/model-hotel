package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// PublicConfigResponse is the unauthenticated subset of server configuration the
// SPA needs to render correctly before/independent of admin auth. It must only
// ever carry non-sensitive feature flags — never tokens, URLs, or secrets.
type PublicConfigResponse struct {
	ReadOnly bool `json:"read_only"`
}

// RegisterPublicConfig mounts the unauthenticated public-config route. It is
// deliberately registered outside the admin-auth group (see cmd/server) so the
// frontend can read it on the login screen as well as inside the dashboard.
func (h *Handler) RegisterPublicConfig(r chi.Router) {
	r.Get("/public-config", h.GetPublicConfig)
}

// GetPublicConfig returns the feature flags that are safe to expose without
// authentication. Currently just read-only (demo) mode.
func (h *Handler) GetPublicConfig(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, PublicConfigResponse{ReadOnly: h.cfg.DemoReadOnly})
}
