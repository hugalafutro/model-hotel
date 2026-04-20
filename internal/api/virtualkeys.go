package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/user/llm-proxy/internal/virtualkey"
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
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if req.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}

	rawKey, err := virtualkey.Generate()
	if err != nil {
		http.Error(w, "failed to generate key", http.StatusInternalServerError)
		return
	}

	keyHash := virtualkey.Hash(rawKey)
	keyPreview := rawKey[:5] + "..." + rawKey[len(rawKey)-2:]

	vk, err := h.virtualKeyRepo.Create(r.Context(), req.Name, keyHash, keyPreview)
	if err != nil {
		http.Error(w, "failed to create virtual key", http.StatusInternalServerError)
		return
	}

	resp := virtualKeyToResponse(vk, true, rawKey)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(resp)
}

func (h *Handler) ListVirtualKeys(w http.ResponseWriter, r *http.Request) {
	keys, err := h.virtualKeyRepo.List(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	responses := make([]virtualkey.VirtualKeyResponse, len(keys))
	for i, vk := range keys {
		responses[i] = virtualKeyToResponse(vk, false, "")
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(responses)
}

func (h *Handler) GetVirtualKey(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "invalid virtual key ID", http.StatusBadRequest)
		return
	}

	vk, err := h.virtualKeyRepo.Get(r.Context(), id)
	if err != nil {
		http.Error(w, "virtual key not found", http.StatusNotFound)
		return
	}

	resp := virtualKeyToResponse(vk, false, "")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (h *Handler) DeleteVirtualKey(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "invalid virtual key ID", http.StatusBadRequest)
		return
	}

	if err := h.virtualKeyRepo.Delete(r.Context(), id); err != nil {
		http.Error(w, "virtual key not found", http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
