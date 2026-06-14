package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/virtualkey"
)

// CreateVirtualKeyRequest is the request body for creating a virtual key.
type CreateVirtualKeyRequest struct {
	Name             string    `json:"name"`
	RateLimitRPS     *float64  `json:"rate_limit_rps,omitempty"`
	RateLimitBurst   *int      `json:"rate_limit_burst,omitempty"`
	AllowedProviders *[]string `json:"allowed_providers,omitempty"`
	StripReasoning   *bool     `json:"strip_reasoning,omitempty"`
}

// UpdateVirtualKeyRequest is the request body for updating a virtual key.
type UpdateVirtualKeyRequest struct {
	Name             string    `json:"name"`
	RateLimitRPS     *float64  `json:"rate_limit_rps"`
	RateLimitBurst   *int      `json:"rate_limit_burst"`
	AllowedProviders *[]string `json:"allowed_providers,omitempty"`
	StripReasoning   *bool     `json:"strip_reasoning,omitempty"`
	// allowedProvidersPresent tracks whether allowed_providers was in the JSON.
	// Set by UnmarshalJSON; do not set manually.
	allowedProvidersPresent bool
	// stripReasoningPresent tracks whether strip_reasoning was in the JSON.
	// Set by UnmarshalJSON; do not set manually.
	stripReasoningPresent bool
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
	return nil
}

// RegisterVirtualKeys mounts virtual key management routes.
func (h *Handler) RegisterVirtualKeys(r chi.Router) {
	r.Route("/virtual-keys", func(r chi.Router) {
		r.Post("/", h.CreateVirtualKey)
		r.Get("/", h.ListVirtualKeys)
		r.Get("/{id}", h.GetVirtualKey)
		r.Put("/{id}", h.UpdateVirtualKey)
		r.Delete("/{id}", h.DeleteVirtualKey)
	})
}

func virtualKeyToResponse(vk *virtualkey.VirtualKey, includeKey bool, rawKey string) virtualkey.VirtualKeyResponse {
	var lastUsed *string
	if vk.LastUsedAt != nil {
		s := vk.LastUsedAt.Format(time.RFC3339)
		lastUsed = &s
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
		AllowedProviders: vk.AllowedProviders,
		StripReasoning:   vk.StripReasoning,
	}
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

	if err := validateRateLimits(req.RateLimitRPS, req.RateLimitBurst, w); err != nil {
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

	vk, err := h.virtualKeyRepo.Create(r.Context(), req.Name, keyHash, keyPreview, req.RateLimitRPS, req.RateLimitBurst, req.AllowedProviders, req.StripReasoning)
	if err != nil {
		debuglog.Error("virtual-keys: failed to create key", "name", req.Name, "error", err)
		respondError(w, fmt.Sprintf("failed to create virtual key %q", req.Name), err, http.StatusInternalServerError)
		return
	}
	debuglog.Info("virtual-keys: created", "name", vk.Name, "id", vk.ID)

	resp := virtualKeyToResponse(vk, true, rawKey)
	writeJSONCreated(w, resp)
}

// ListVirtualKeys returns all virtual API keys.
func (h *Handler) ListVirtualKeys(w http.ResponseWriter, r *http.Request) {
	keys, err := h.virtualKeyRepo.List(r.Context())
	if err != nil {
		respondError(w, "failed to list virtual keys", err, http.StatusInternalServerError)
		return
	}

	responses := make([]virtualkey.VirtualKeyResponse, len(keys))
	for i, vk := range keys {
		responses[i] = virtualKeyToResponse(vk, false, "")
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

	resp := virtualKeyToResponse(vk, false, "")
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

	// When allowed_providers or strip_reasoning is omitted from the request
	// body, preserve the existing values instead of clearing them. This
	// prevents external scripts that update name/rate-limits from
	// accidentally dropping restrictions or changing reasoning settings.
	// Use a single DB fetch for both guards to avoid an extra roundtrip.
	var existingVK *virtualkey.VirtualKey
	if !req.allowedProvidersPresent || !req.stripReasoningPresent {
		existing, err := h.virtualKeyRepo.Get(r.Context(), id)
		if err != nil && !errors.Is(err, virtualkey.ErrNotFound) {
			debuglog.Error("virtual-keys: failed to fetch key for update", "id", id, "error", err)
			respondError(w, "failed to update virtual key", err, http.StatusInternalServerError)
			return
		}
		existingVK = existing
	}

	if !req.allowedProvidersPresent && existingVK != nil {
		req.AllowedProviders = existingVK.AllowedProviders
	}

	if !req.stripReasoningPresent && existingVK != nil {
		req.StripReasoning = &existingVK.StripReasoning
	}

	if err := validateRateLimits(req.RateLimitRPS, req.RateLimitBurst, w); err != nil {
		return
	}

	vk, err := h.virtualKeyRepo.Update(r.Context(), id, req.Name, req.RateLimitRPS, req.RateLimitBurst, req.AllowedProviders, req.StripReasoning)
	if err != nil {
		if errors.Is(err, virtualkey.ErrNotFound) {
			http.Error(w, "virtual key not found", http.StatusNotFound)
			return
		}
		debuglog.Error("virtual-keys: failed to update key", "id", id, "error", err)
		respondError(w, "failed to update virtual key", err, http.StatusInternalServerError)
		return
	}

	resp := virtualKeyToResponse(vk, false, "")
	writeJSON(w, resp)
}

// DeleteVirtualKey deletes a virtual API key by ID.
func (h *Handler) DeleteVirtualKey(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUUIDParam(w, r, "id", "virtual key ID")
	if !ok {
		return
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
func validateRateLimits(rps *float64, burst *int, w http.ResponseWriter) error {
	if rps != nil && *rps < 0 {
		respondBadRequest(w, "rate_limit_rps must be >= 0", fmt.Errorf("got %f", *rps))
		return fmt.Errorf("invalid rate_limit_rps")
	}
	if burst != nil {
		if *burst < 0 {
			respondBadRequest(w, "rate_limit_burst must be >= 0", fmt.Errorf("got %d", *burst))
			return fmt.Errorf("invalid rate_limit_burst")
		}
		if *burst == 0 {
			respondBadRequest(w, "rate_limit_burst must be >= 1 (use null to fall back to global settings)", fmt.Errorf("got 0"))
			return fmt.Errorf("invalid rate_limit_burst")
		}
	}
	return nil
}
