package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/user/llm-proxy/internal/failover"
	"github.com/user/llm-proxy/internal/model"
	"github.com/user/llm-proxy/internal/provider"
)

type DiscoveryService interface {
	DiscoverModels(ctx context.Context, provider *provider.Provider, masterKey string) ([]*model.Model, error)
}

func (h *Handler) RegisterProviderDiscovery(r chi.Router) {
	r.Route("/providers/{id}/discover", func(r chi.Router) {
		r.Post("/", h.DiscoverProviderModels)
	})
	r.Route("/providers/{id}/usage", func(r chi.Router) {
		r.Get("/", h.GetProviderUsage)
	})
	r.Route("/providers/{id}/balance", func(r chi.Router) {
		r.Get("/", h.GetProviderBalance)
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

	failoverRepo := failover.NewRepository(h.dbPool.Pool())
	seenModelIDs := make(map[string]bool)
	for _, mid := range existingModelIDs {
		seenModelIDs[mid] = true
	}
	for modelID := range seenModelIDs {
		if err := failoverRepo.SyncForModel(r.Context(), modelID); err != nil {
			http.Error(w, "failed to sync failover group: "+err.Error(), http.StatusInternalServerError)
			return
		}
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

func (h *Handler) GetProviderUsage(w http.ResponseWriter, r *http.Request) {
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

	discovery := provider.NewDiscoveryService()

	usage, err := discovery.GetNanoGPTUsage(r.Context(), prov, h.cfg.MasterKey)
	if err != nil {
		http.Error(w, "failed to fetch usage: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(usage)
}

func (h *Handler) GetProviderBalance(w http.ResponseWriter, r *http.Request) {
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

	discovery := provider.NewDiscoveryService()

	balance, err := discovery.GetDeepSeekBalance(r.Context(), prov, h.cfg.MasterKey)
	if err != nil {
		http.Error(w, "failed to fetch balance: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(balance)
}
