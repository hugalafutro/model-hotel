package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/user/llm-proxy/internal/failover"
	"github.com/user/llm-proxy/internal/model"
	"github.com/user/llm-proxy/internal/provider"
)

func (h *Handler) RegisterProviderDiscovery(r chi.Router) {
	r.Post("/providers/discover-all", h.DiscoverAllModels)
	r.Post("/providers/refresh-quotas", h.RefreshAllQuotas)
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

	prov, err := h.providerRepo.Get(r.Context(), providerID)
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

	if _, err := modelRepo.DisableMissingModels(r.Context(), providerID, existingModelIDs); err != nil {
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

	prov, err := h.providerRepo.Get(r.Context(), providerID)
	if err != nil {
		http.Error(w, "provider not found", http.StatusNotFound)
		return
	}

	discovery := provider.NewDiscoveryService()

	switch provider.DetectProviderType(prov.BaseURL) {
	case "zai":
		quota, err := discovery.GetZAIQuota(r.Context(), prov, h.cfg.MasterKey)
		if err != nil {
			http.Error(w, "failed to fetch usage: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(quota)
		return
	case "nanogpt":
		usage, err := discovery.GetNanoGPTUsage(r.Context(), prov, h.cfg.MasterKey)
		if err != nil {
			http.Error(w, "failed to fetch usage: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(usage)
		return
	default:
		http.Error(w, "usage information not supported for this provider type", http.StatusBadRequest)
		return
	}
}

func (h *Handler) GetProviderBalance(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	providerID, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "invalid provider ID", http.StatusBadRequest)
		return
	}

	prov, err := h.providerRepo.Get(r.Context(), providerID)
	if err != nil {
		http.Error(w, "provider not found", http.StatusNotFound)
		return
	}

	discovery := provider.NewDiscoveryService()

	switch provider.DetectProviderType(prov.BaseURL) {
	case "deepseek":
		balance, err := discovery.GetDeepSeekBalance(r.Context(), prov, h.cfg.MasterKey)
		if err != nil {
			http.Error(w, "failed to fetch balance: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(balance)
		return
	default:
		http.Error(w, "balance information not supported for this provider type", http.StatusBadRequest)
		return
	}
}

type DiscoverAllResult struct {
	ProviderName string `json:"provider_name"`
	Discovered   int    `json:"discovered"`
	Error        string `json:"error,omitempty"`
}

func (h *Handler) DiscoverAllModels(w http.ResponseWriter, r *http.Request) {
	providers, err := h.providerRepo.List(r.Context())
	if err != nil {
		http.Error(w, "failed to list providers", http.StatusInternalServerError)
		return
	}

	discovery := provider.NewDiscoveryService()
	modelRepo := model.NewRepository(h.dbPool.Pool())
	failoverRepo := failover.NewRepository(h.dbPool.Pool())

	var results []DiscoverAllResult
	totalDiscovered := 0
	succeeded := 0
	failed := 0

	for _, prov := range providers {
		if !prov.Enabled {
			continue
		}

		models, discoverErr := discovery.DiscoverModels(r.Context(), prov, h.cfg.MasterKey)
		result := DiscoverAllResult{
			ProviderName: prov.Name,
		}

		if discoverErr != nil {
			result.Error = discoverErr.Error()
			failed++
			results = append(results, result)
			continue
		}

		result.Discovered = len(models)
		totalDiscovered += len(models)
		succeeded++

		existingModelIDs := make([]string, 0, len(models))
		for _, m := range models {
			if err := modelRepo.Upsert(r.Context(), m); err != nil {
				continue
			}
			existingModelIDs = append(existingModelIDs, m.ModelID)
		}

		modelRepo.DisableMissingModels(r.Context(), prov.ID, existingModelIDs)

		seenModelIDs := make(map[string]bool)
		for _, mid := range existingModelIDs {
			seenModelIDs[mid] = true
		}
		for modelID := range seenModelIDs {
			failoverRepo.SyncForModel(r.Context(), modelID)
		}

		now := time.Now()
		h.dbPool.Pool().Exec(r.Context(),
			`UPDATE providers SET last_discovered_at = $1 WHERE id = $2`, now, prov.ID)

		results = append(results, result)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"results":    results,
		"succeeded":  succeeded,
		"failed":     failed,
		"discovered": totalDiscovered,
	})
}

type QuotaRefreshResult struct {
	ProviderName string `json:"provider_name"`
	ProviderType string `json:"provider_type"`
	Refreshed    bool   `json:"refreshed"`
	Error        string `json:"error,omitempty"`
}

func (h *Handler) RefreshAllQuotas(w http.ResponseWriter, r *http.Request) {
	providers, err := h.providerRepo.List(r.Context())
	if err != nil {
		http.Error(w, "failed to list providers", http.StatusInternalServerError)
		return
	}

	discovery := provider.NewDiscoveryService()

	var results []QuotaRefreshResult
	refreshed := 0
	failed := 0
	skipped := 0

	for _, prov := range providers {
		if !prov.Enabled {
			continue
		}

		providerType := provider.DetectProviderType(prov.BaseURL)
		result := QuotaRefreshResult{
			ProviderName: prov.Name,
			ProviderType: providerType,
		}

		switch providerType {
		case "nanogpt":
			_, err := discovery.GetNanoGPTUsage(r.Context(), prov, h.cfg.MasterKey)
			if err != nil {
				result.Error = err.Error()
				failed++
			} else {
				result.Refreshed = true
				refreshed++
			}
		case "zai":
			_, err := discovery.GetZAIQuota(r.Context(), prov, h.cfg.MasterKey)
			if err != nil {
				result.Error = err.Error()
				failed++
			} else {
				result.Refreshed = true
				refreshed++
			}
		case "deepseek":
			_, err := discovery.GetDeepSeekBalance(r.Context(), prov, h.cfg.MasterKey)
			if err != nil {
				result.Error = err.Error()
				failed++
			} else {
				result.Refreshed = true
				refreshed++
			}
		default:
			skipped++
			continue
		}

		results = append(results, result)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"results":   results,
		"refreshed": refreshed,
		"failed":    failed,
		"skipped":   skipped,
	})
}
