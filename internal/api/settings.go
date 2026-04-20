package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/user/llm-proxy/internal/settings"
)

func (h *Handler) RegisterSettings(r chi.Router) {
	r.Route("/settings", func(r chi.Router) {
		r.Get("/", h.GetSettings)
		r.Put("/", h.UpdateSettings)
	})
}

func (h *Handler) GetSettings(w http.ResponseWriter, r *http.Request) {
	settingsRepo := settings.NewRepository(h.dbPool.Pool())
	all, err := settingsRepo.GetAll(r.Context())
	if err != nil {
		http.Error(w, "failed to load settings", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(all)
}

func (h *Handler) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	var req map[string]string
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	settingsRepo := settings.NewRepository(h.dbPool.Pool())
	for key, value := range req {
		if err := settingsRepo.Set(r.Context(), key, value); err != nil {
			http.Error(w, "failed to save setting", http.StatusInternalServerError)
			return
		}
	}

	all, _ := settingsRepo.GetAll(r.Context())

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(all)
}