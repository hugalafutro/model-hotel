package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
)

func (h *Handler) RegisterSettings(r chi.Router) {
	r.Route("/settings", func(r chi.Router) {
		r.Get("/", h.GetSettings)
		r.Put("/", h.UpdateSettings)
	})
}

func (h *Handler) GetSettings(w http.ResponseWriter, r *http.Request) {
	all, err := h.settingsRepo.GetAll(r.Context())
	if err != nil {
		http.Error(w, "failed to load settings", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(all); err != nil {
		http.Error(w, "failed to encode response", http.StatusInternalServerError)
	}
}

func (h *Handler) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	var req map[string]string
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	tx, err := h.dbPool.Begin(r.Context())
	if err != nil {
		http.Error(w, "failed to begin transaction", http.StatusInternalServerError)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()

	for key, value := range req {
		if err := h.settingsRepo.SetTx(r.Context(), tx, key, value); err != nil {
			http.Error(w, "failed to save setting", http.StatusInternalServerError)
			return
		}
	}

	if err := tx.Commit(r.Context()); err != nil {
		http.Error(w, "failed to commit transaction", http.StatusInternalServerError)
		return
	}

	// Invalidate cache for updated keys after successful commit
	for key := range req {
		h.settingsRepo.InvalidateCache(key)
	}

	all, _ := h.settingsRepo.GetAll(r.Context())

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(all); err != nil {
		http.Error(w, "failed to encode response", http.StatusInternalServerError)
	}
}
