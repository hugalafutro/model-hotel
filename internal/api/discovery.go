package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/user/llm-proxy/internal/model"
	"github.com/user/llm-proxy/internal/provider"
)

type DiscoveryService interface {
	DiscoverModels(ctx context.Context, provider *provider.Provider, masterKey string) ([]*model.Model, error)
}

func (h *Handler) RegisterProviderDiscovery(r chi.Router) {
	r.Route("/providers/{id}/discover", func(r chi.Router) {
		r.Use(h.AuthMiddleware)
		r.Post("/", h.DiscoverProviderModels)
	})
}

func (h *Handler) DiscoverProviderModels(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	providerID, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "invalid provider ID", http.StatusBadRequest)
		return
	}

	prov, err := h.db.Get(r.Context(), providerID)
	if err != nil {
		http.Error(w, "provider not found", http.StatusNotFound)
		return
	}

	if !prov.Enabled {
		http.Error(w, "provider is disabled", http.StatusBadRequest)
		return
	}

	discovery := provider.NewDiscoveryService()
	models, err := discovery.DiscoverModels(r.Context(), prov, h.cfg.MasterKey)
	if err != nil {
		http.Error(w, "failed to discover models: "+err.Error(), http.StatusInternalServerError)
		return
	}

	modelRepo := model.NewRepository(h.dbPool.Pool())

	existingModelIDs := make([]string, 0, len(models))
	for _, m := range models {
		if err := modelRepo.Upsert(r.Context(), m); err != nil {
			http.Error(w, "failed to upsert model: "+err.Error(), http.StatusInternalServerError)
			return
		}
		existingModelIDs = append(existingModelIDs, m.ModelID)
	}

	if err := modelRepo.DisableMissingModels(r.Context(), providerID, existingModelIDs); err != nil {
		http.Error(w, "failed to disable missing models: "+err.Error(), http.StatusInternalServerError)
		return
	}

	now := time.Now()
	updateQuery := `UPDATE providers SET last_discovered_at = $1 WHERE id = $2`
	if _, err := h.dbPool.Pool().Exec(r.Context(), updateQuery, now, providerID); err != nil {
		http.Error(w, "failed to update provider", http.StatusInternalServerError)
		return
	}

	response := map[string]interface{}{
		"discovered": len(models),
		"models":     models,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
