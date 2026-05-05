package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/hugalafutro/model-hotel/internal/virtualkey"
)

type CreateVirtualKeyRequest struct {
	Name string `json:"name"`
}

func (h *Handler) RegisterVirtualKeys(r chi.Router) {
	r.Route("/virtual-keys", func(r chi.Router) {
		r.Post("/", h.CreateVirtualKey)
		r.Get("/", h.ListVirtualKeys)
		r.Get("/{id}", h.GetVirtualKey)
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
		ID:         vk.ID.String(),
		Name:       vk.Name,
		Key:        cond(rawKey, includeKey),
		KeyPreview: vk.KeyPreview,
		TokensUsed: vk.TokensUsed,
		LastUsedAt: lastUsed,
		CreatedAt:  vk.CreatedAt.Format(time.RFC3339),
	}
}

func cond(val string, condition bool) string {
	if condition {
		return val
	}
	return ""
}

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

	rawKey, err := virtualkey.Generate()
	if err != nil {
		respondError(w, "failed to generate key", err, http.StatusInternalServerError)
		return
	}

	keyHash := virtualkey.Hash(rawKey)
	keyPreview := rawKey[:3] + "..." + rawKey[len(rawKey)-2:]

	vk, err := h.virtualKeyRepo.Create(r.Context(), req.Name, keyHash, keyPreview)
	if err != nil {
		respondError(w, "failed to create virtual key", err, http.StatusInternalServerError)
		return
	}

	resp := virtualKeyToResponse(vk, true, rawKey)
	writeJSONCreated(w, resp)
}

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

func (h *Handler) GetVirtualKey(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUUIDParam(w, r, "id", "virtual key ID")
	if !ok {
		return
	}

	vk, err := h.virtualKeyRepo.Get(r.Context(), id)
	if err != nil {
		http.Error(w, "virtual key not found", http.StatusNotFound)
		return
	}

	resp := virtualKeyToResponse(vk, false, "")
	writeJSON(w, resp)
}

func (h *Handler) DeleteVirtualKey(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUUIDParam(w, r, "id", "virtual key ID")
	if !ok {
		return
	}

	if err := h.virtualKeyRepo.Delete(r.Context(), id); err != nil {
		http.Error(w, "virtual key not found", http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
