package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// AdminTokenManager exposes the admin-token hash for the HA token-hash sync and
// reset flows (Section 5 of the HA plan). Implemented by *admin.Manager.
type AdminTokenManager interface {
	Hash() string
	SetHash(value string) error
}

// AdminTokenHandler serves the admin-token-hash endpoints the HA "Front Desk"
// control plane uses to converge a member group on a single admin token, and to
// reset it. Only the sha256:<hex> hash is ever read or written here; the
// plaintext admin token never transits these endpoints.
//
// Register MUST be mounted behind admin authentication. It is: callers mount it
// inside the /api group, which already applies AuthMiddleware (admin token when
// TOTP is off, else a session token). A caller able to overwrite the hash
// controls dashboard access, so no weaker gate is acceptable.
type AdminTokenHandler struct {
	mgr AdminTokenManager
}

// NewAdminTokenHandler builds the handler over an AdminTokenManager.
func NewAdminTokenHandler(mgr AdminTokenManager) *AdminTokenHandler {
	return &AdminTokenHandler{mgr: mgr}
}

// Register mounts GET/POST /admin/token-hash. The parent router must apply admin
// auth (see type doc).
func (h *AdminTokenHandler) Register(r chi.Router) {
	r.Get("/admin/token-hash", h.Get)
	r.Post("/admin/token-hash", h.Post)
}

type adminTokenHashResponse struct {
	Hash string `json:"hash"`
}

type adminTokenHashRequest struct {
	Hash string `json:"hash"`
}

// Get returns the current admin-token hash as sha256:<hex>, or an empty string
// when no token is set, so Front Desk can compare members before syncing.
func (h *AdminTokenHandler) Get(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, adminTokenHashResponse{Hash: h.mgr.Hash()})
}

// Post overwrites the admin-token hash and hot-reloads it in place (no restart),
// then echoes the now-current hash. A malformed body or invalid hash is a 400.
func (h *AdminTokenHandler) Post(w http.ResponseWriter, r *http.Request) {
	var req adminTokenHashRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	if err := h.mgr.SetHash(req.Hash); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, adminTokenHashResponse{Hash: h.mgr.Hash()})
}
