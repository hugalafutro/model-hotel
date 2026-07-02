package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/user"
	"github.com/hugalafutro/model-hotel/internal/virtualkey"
)

// CreateVirtualKeyRequest is the request body for creating a virtual key.
type CreateVirtualKeyRequest struct {
	Name             string    `json:"name"`
	RateLimitRPS     *float64  `json:"rate_limit_rps,omitempty"`
	RateLimitBurst   *int      `json:"rate_limit_burst,omitempty"`
	RateLimitTPM     *int      `json:"rate_limit_tpm,omitempty"`
	AllowedProviders *[]string `json:"allowed_providers,omitempty"`
	StripReasoning   *bool     `json:"strip_reasoning,omitempty"`
	// OwnerUserID assigns the key to a dashboard user (admin callers only;
	// for non-admins the key is always created as their own). Empty string or
	// null means unowned.
	OwnerUserID *string `json:"owner_user_id,omitempty"`
}

// UpdateVirtualKeyRequest is the request body for updating a virtual key.
type UpdateVirtualKeyRequest struct {
	Name             string    `json:"name"`
	RateLimitRPS     *float64  `json:"rate_limit_rps"`
	RateLimitBurst   *int      `json:"rate_limit_burst"`
	RateLimitTPM     *int      `json:"rate_limit_tpm"`
	AllowedProviders *[]string `json:"allowed_providers,omitempty"`
	StripReasoning   *bool     `json:"strip_reasoning,omitempty"`
	OwnerUserID      *string   `json:"owner_user_id,omitempty"`
	// allowedProvidersPresent tracks whether allowed_providers was in the JSON.
	// Set by UnmarshalJSON; do not set manually.
	allowedProvidersPresent bool
	// stripReasoningPresent tracks whether strip_reasoning was in the JSON.
	// Set by UnmarshalJSON; do not set manually.
	stripReasoningPresent bool
	// ownerUserIDPresent tracks whether owner_user_id was in the JSON (an
	// explicit null counts as present: an admin sending null unassigns the
	// owner). Set by UnmarshalJSON; do not set manually.
	ownerUserIDPresent bool
}

// UnmarshalJSON detects whether allowed_providers was present in the JSON.
func (r *UpdateVirtualKeyRequest) UnmarshalJSON(data []byte) error {
	// First pass: decode all fields normally
	type plain UpdateVirtualKeyRequest
	if err := json.Unmarshal(data, (*plain)(r)); err != nil {
		return err
	}
	// Second pass: check if the field key exists in the JSON
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	r.allowedProvidersPresent = raw["allowed_providers"] != nil
	r.stripReasoningPresent = raw["strip_reasoning"] != nil
	_, r.ownerUserIDPresent = raw["owner_user_id"]
	return nil
}

// RegisterVirtualKeys mounts virtual key management routes.
func (h *Handler) RegisterVirtualKeys(r chi.Router) {
	r.Route("/virtual-keys", func(r chi.Router) {
		// The virtual_keys grant covers the whole page, mutations included.
		r.Use(requireGrant(user.GrantVirtualKeys))
		r.Get("/", h.ListVirtualKeys)
		r.Get("/{id}", h.GetVirtualKey)
		// Virtual keys are synced config: a managed fleet member must not edit them
		// locally (the primary owns them and replaces them on the next sync).
		r.Group(func(r chi.Router) {
			r.Use(managedWriteGuard(h.settingsRepo))
			r.Post("/", h.CreateVirtualKey)
			r.Put("/{id}", h.UpdateVirtualKey)
			r.Delete("/{id}", h.DeleteVirtualKey)
		})
	})
}

func virtualKeyToResponse(vk *virtualkey.VirtualKey, includeKey bool, rawKey string, ownerUsername *string) virtualkey.VirtualKeyResponse {
	var lastUsed *string
	if vk.LastUsedAt != nil {
		s := vk.LastUsedAt.Format(time.RFC3339)
		lastUsed = &s
	}
	var ownerID *string
	if vk.OwnerUserID != nil {
		s := vk.OwnerUserID.String()
		ownerID = &s
	}

	return virtualkey.VirtualKeyResponse{
		ID:               vk.ID.String(),
		Name:             vk.Name,
		Key:              cond(rawKey, includeKey),
		KeyPreview:       vk.KeyPreview,
		TokensUsed:       vk.TokensUsed,
		LastUsedAt:       lastUsed,
		CreatedAt:        vk.CreatedAt.Format(time.RFC3339),
		RateLimitRPS:     vk.RateLimitRPS,
		RateLimitBurst:   vk.RateLimitBurst,
		RateLimitTPM:     vk.RateLimitTPM,
		AllowedProviders: vk.AllowedProviders,
		StripReasoning:   vk.StripReasoning,
		OwnerUserID:      ownerID,
		OwnerUsername:    ownerUsername,
	}
}

// ownerUsername resolves a key owner's username for display. Best-effort:
// nil when the key is unowned, the user store is not wired, or the row is
// gone (a stale reference must not fail the whole response).
func (h *Handler) ownerUsername(ctx context.Context, ownerID *uuid.UUID) *string {
	if ownerID == nil || h.userRepo == nil {
		return nil
	}
	u, err := h.userRepo.Get(ctx, *ownerID)
	if err != nil || u == nil {
		return nil
	}
	return &u.Username
}

// resolveWriteOwner decides the owner a create/update writes. Non-admins
// always write their own id: they can neither assign keys to others nor
// orphan their own. Admins get what they asked for (requested, which may be
// nil to unassign); a caller without a users row (the env-token admin)
// behaves like any admin.
func resolveWriteOwner(id *user.Identity, requested *string) (*uuid.UUID, error) {
	if id != nil && !id.IsAdmin() {
		return id.UserID, nil
	}
	if requested == nil || *requested == "" {
		return nil, nil //nolint:nilnil // nil owner = unowned key, not an error sentinel
	}
	uid, err := uuid.Parse(*requested)
	if err != nil {
		return nil, fmt.Errorf("invalid owner_user_id: %w", err)
	}
	return &uid, nil
}

// canTouchKey reports whether the caller may see or modify the key: admins
// always, non-admins only for keys they own. Deny reads as 404 so the key
// listing and the detail routes tell a consistent story (no existence
// oracle for other users' keys).
func canTouchKey(id *user.Identity, vk *virtualkey.VirtualKey) bool {
	if id == nil || id.IsAdmin() {
		return true
	}
	return id.UserID != nil && vk.OwnerUserID != nil && *vk.OwnerUserID == *id.UserID
}

func cond(val string, condition bool) string {
	if condition {
		return val
	}
	return ""
}

// CreateVirtualKey creates a new virtual API key.
func (h *Handler) CreateVirtualKey(w http.ResponseWriter, r *http.Request) {
	var req CreateVirtualKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondBadRequest(w, "invalid request body", err)
		return
	}

	trimmed, err := validateNameString("name", req.Name, 1, 100)
	if err != nil {
		respondBadRequest(w, "invalid name", err)
		return
	}
	req.Name = trimmed

	for _, reserved := range []string{"chat", "arena", "completions", "admin"} {
		if strings.EqualFold(req.Name, reserved) {
			http.Error(w, fmt.Sprintf("name %q is reserved", reserved), http.StatusBadRequest)
			return
		}
	}

	// Reject empty allowed_providers array (non-nil but len==0).
	// nil means "no restriction", empty slice means "deny all" which is rejected.
	if req.AllowedProviders != nil && len(*req.AllowedProviders) == 0 {
		http.Error(w, "allowed_providers must be null or contain at least one provider ID", http.StatusBadRequest)
		return
	}

	if err := validateRateLimits(req.RateLimitRPS, req.RateLimitBurst, req.RateLimitTPM, w); err != nil {
		return
	}

	caller := user.IdentityFrom(r.Context())
	owner, err := resolveWriteOwner(caller, req.OwnerUserID)
	if err != nil {
		respondBadRequest(w, err.Error(), nil)
		return
	}

	rawKey, err := virtualkey.Generate()
	if err != nil {
		debuglog.Error("virtual-keys: failed to generate key", "error", err)
		respondError(w, fmt.Sprintf("failed to generate key %q", req.Name), err, http.StatusInternalServerError)
		return
	}

	keyHash := virtualkey.Hash(rawKey)
	keyPreview := rawKey[:3] + "..." + rawKey[len(rawKey)-2:]

	vk, err := h.virtualKeyRepo.Create(r.Context(), req.Name, keyHash, keyPreview, req.RateLimitRPS, req.RateLimitBurst, req.RateLimitTPM, req.AllowedProviders, req.StripReasoning, owner)
	if err != nil {
		if isForeignKeyViolation(err) {
			respondBadRequest(w, "owner_user_id does not match any user", nil)
			return
		}
		debuglog.Error("virtual-keys: failed to create key", "name", req.Name, "error", err)
		respondError(w, fmt.Sprintf("failed to create virtual key %q", req.Name), err, http.StatusInternalServerError)
		return
	}
	debuglog.Info("virtual-keys: created", "name", vk.Name, "id", vk.ID)

	resp := virtualKeyToResponse(vk, true, rawKey, h.ownerUsername(r.Context(), vk.OwnerUserID))
	writeJSONCreated(w, resp)
}

// ListVirtualKeys returns virtual API keys: all of them for admins, only the
// caller's own for grant-holding users.
func (h *Handler) ListVirtualKeys(w http.ResponseWriter, r *http.Request) {
	caller := user.IdentityFrom(r.Context())

	var keys []*virtualkey.VirtualKey
	var err error
	if caller != nil && !caller.IsAdmin() {
		if caller.UserID == nil {
			// A non-admin identity without a users row cannot own keys.
			writeJSON(w, []virtualkey.VirtualKeyResponse{})
			return
		}
		keys, err = h.virtualKeyRepo.ListByOwner(r.Context(), *caller.UserID)
	} else {
		keys, err = h.virtualKeyRepo.List(r.Context())
	}
	if err != nil {
		respondError(w, "failed to list virtual keys", err, http.StatusInternalServerError)
		return
	}

	// Resolve owner usernames in one pass instead of a query per key.
	usernames := map[uuid.UUID]string{}
	if h.userRepo != nil {
		if users, uerr := h.userRepo.List(r.Context()); uerr == nil {
			for _, u := range users {
				usernames[u.ID] = u.Username
			}
		}
	}

	responses := make([]virtualkey.VirtualKeyResponse, len(keys))
	for i, vk := range keys {
		var ownerName *string
		if vk.OwnerUserID != nil {
			if name, ok := usernames[*vk.OwnerUserID]; ok {
				ownerName = &name
			}
		}
		responses[i] = virtualKeyToResponse(vk, false, "", ownerName)
	}

	writeJSON(w, responses)
}

// GetVirtualKey retrieves a virtual API key by ID.
func (h *Handler) GetVirtualKey(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUUIDParam(w, r, "id", "virtual key ID")
	if !ok {
		return
	}

	vk, err := h.virtualKeyRepo.Get(r.Context(), id)
	if err != nil {
		respondLookupError(w, err, virtualkey.ErrNotFound, "virtual key not found", "failed to load virtual key")
		return
	}
	if !canTouchKey(user.IdentityFrom(r.Context()), vk) {
		http.Error(w, "virtual key not found", http.StatusNotFound)
		return
	}

	resp := virtualKeyToResponse(vk, false, "", h.ownerUsername(r.Context(), vk.OwnerUserID))
	writeJSON(w, resp)
}

// UpdateVirtualKey updates a virtual API key.
func (h *Handler) UpdateVirtualKey(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUUIDParam(w, r, "id", "virtual key ID")
	if !ok {
		return
	}

	var req UpdateVirtualKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondBadRequest(w, "invalid request body", err)
		return
	}

	trimmed, err := validateNameString("name", req.Name, 1, 100)
	if err != nil {
		respondBadRequest(w, "invalid name", err)
		return
	}
	req.Name = trimmed

	for _, reserved := range []string{"chat", "arena", "completions", "admin"} {
		if strings.EqualFold(req.Name, reserved) {
			http.Error(w, fmt.Sprintf("name %q is reserved", reserved), http.StatusBadRequest)
			return
		}
	}

	// Reject empty allowed_providers array (non-nil but len==0).
	// nil means "no restriction", empty slice means "deny all" which is rejected.
	if req.AllowedProviders != nil && len(*req.AllowedProviders) == 0 {
		http.Error(w, "allowed_providers must be null or contain at least one provider ID", http.StatusBadRequest)
		return
	}

	if err := validateRateLimits(req.RateLimitRPS, req.RateLimitBurst, req.RateLimitTPM, w); err != nil {
		return
	}

	// The existing row is always fetched: it backs the ownership check, the
	// omitted-field preservation for allowed_providers/strip_reasoning (so
	// external scripts that update name/rate-limits do not accidentally drop
	// restrictions), and owner preservation when owner_user_id is omitted.
	existingVK, err := h.virtualKeyRepo.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, virtualkey.ErrNotFound) {
			http.Error(w, "virtual key not found", http.StatusNotFound)
			return
		}
		debuglog.Error("virtual-keys: failed to fetch key for update", "id", id, "error", err)
		respondError(w, "failed to update virtual key", err, http.StatusInternalServerError)
		return
	}
	caller := user.IdentityFrom(r.Context())
	if !canTouchKey(caller, existingVK) {
		http.Error(w, "virtual key not found", http.StatusNotFound)
		return
	}

	if !req.allowedProvidersPresent {
		req.AllowedProviders = existingVK.AllowedProviders
	}

	if !req.stripReasoningPresent {
		req.StripReasoning = &existingVK.StripReasoning
	}

	// Owner: non-admins always keep themselves (resolveWriteOwner); admins
	// who omit the field keep the current owner, an explicit null clears it.
	owner := existingVK.OwnerUserID
	if caller != nil && !caller.IsAdmin() || req.ownerUserIDPresent {
		owner, err = resolveWriteOwner(caller, req.OwnerUserID)
		if err != nil {
			respondBadRequest(w, err.Error(), nil)
			return
		}
	}

	vk, err := h.virtualKeyRepo.Update(r.Context(), id, req.Name, req.RateLimitRPS, req.RateLimitBurst, req.RateLimitTPM, req.AllowedProviders, req.StripReasoning, owner)
	if err != nil {
		if errors.Is(err, virtualkey.ErrNotFound) {
			http.Error(w, "virtual key not found", http.StatusNotFound)
			return
		}
		if isForeignKeyViolation(err) {
			respondBadRequest(w, "owner_user_id does not match any user", nil)
			return
		}
		debuglog.Error("virtual-keys: failed to update key", "id", id, "error", err)
		respondError(w, "failed to update virtual key", err, http.StatusInternalServerError)
		return
	}

	resp := virtualKeyToResponse(vk, false, "", h.ownerUsername(r.Context(), vk.OwnerUserID))
	writeJSON(w, resp)
}

// DeleteVirtualKey deletes a virtual API key by ID.
func (h *Handler) DeleteVirtualKey(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUUIDParam(w, r, "id", "virtual key ID")
	if !ok {
		return
	}

	// Non-admins may only delete their own keys; the ownership check needs
	// the row, and a foreign key reads as missing (404) to them.
	caller := user.IdentityFrom(r.Context())
	if caller != nil && !caller.IsAdmin() {
		existing, err := h.virtualKeyRepo.Get(r.Context(), id)
		if err != nil {
			respondLookupError(w, err, virtualkey.ErrNotFound, "virtual key not found", "failed to delete virtual key")
			return
		}
		if !canTouchKey(caller, existing) {
			http.Error(w, "virtual key not found", http.StatusNotFound)
			return
		}
	}

	if err := h.virtualKeyRepo.Delete(r.Context(), id); err != nil {
		if errors.Is(err, virtualkey.ErrNotFound) {
			http.Error(w, "virtual key not found", http.StatusNotFound)
			return
		}
		debuglog.Error("virtual-keys: failed to delete key", "id", id, "error", err)
		respondError(w, "failed to delete virtual key", err, http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// validateRateLimits checks that per-key rate limit overrides are non-negative
// and that burst is at least 1 (burst=0 rejects all requests). Use null to fall
// back to global settings.
// Returns a non-nil error (already written to w) if validation fails.
func validateRateLimits(rps *float64, burst, tpm *int, w http.ResponseWriter) error {
	if rps != nil && *rps < 0 {
		respondBadRequest(w, "rate_limit_rps must be >= 0", fmt.Errorf("got %f", *rps))
		return fmt.Errorf("invalid rate_limit_rps")
	}
	if burst != nil && *burst < 1 {
		respondBadRequest(w, "rate_limit_burst must be >= 1 (use null to fall back to global settings)", fmt.Errorf("got %d", *burst))
		return fmt.Errorf("invalid rate_limit_burst")
	}
	if tpm != nil && *tpm < 1 {
		respondBadRequest(w, "rate_limit_tpm must be >= 1 (use null for no cap / global default)", fmt.Errorf("got %d", *tpm))
		return fmt.Errorf("invalid rate_limit_tpm")
	}
	return nil
}
