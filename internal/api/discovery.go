package api

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/hugalafutro/model-hotel/internal/events"
	"github.com/hugalafutro/model-hotel/internal/failover"
	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/provider"
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
	providerID, ok := parseUUIDParam(w, r, "id", "provider ID")
	if !ok {
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
	// Use a context decoupled from the HTTP request deadline for discovery.
	// Provider availability tests (especially for slow/unreachable providers)
	// can exhaust the 60s chi middleware timeout before DB upserts run.
	provCtx, provCancel := context.WithTimeout(context.WithoutCancel(r.Context()), 180*time.Second)
	defer provCancel()
	models, err := discovery.DiscoverModels(provCtx, prov, h.cfg.MasterKey)
	if err != nil {
		provCancel()
		respondError(w, "failed to discover models", err, http.StatusInternalServerError)
		return
	}

	events.Publish(events.Event{
		Type:     "discovery.provider_fetched",
		Severity: "success",
		Message:  fmt.Sprintf("Fetched %d models from %s", len(models), prov.Name),
		Metadata: map[string]interface{}{"provider": prov.Name, "count": len(models)},
	})

	// Enrich models with data from models.dev (fills gaps for models not
	// covered by hardcoded catalogs).
	if cache := provider.GetModelsDevCache(); cache != nil {
		enriched := cache.EnrichModels(models)
		if enriched > 0 {
			events.Publish(events.Event{
				Type:     "discovery.enriched",
				Severity: "info",
				Message:  fmt.Sprintf("Enriched %d/%d models from models.dev catalogue", enriched, len(models)),
				Metadata: map[string]interface{}{"provider": prov.Name, "enriched": enriched, "total": len(models)},
			})
		}
	}

	modelRepo := model.NewRepository(h.dbPool.Pool())

	existingModelIDs := make([]string, 0, len(models))
	for _, m := range models {
		if err := modelRepo.Upsert(provCtx, m); err != nil {
			respondError(w, "failed to upsert model", err, http.StatusInternalServerError)
			return
		}
		existingModelIDs = append(existingModelIDs, m.ModelID)
	}

	if _, err := modelRepo.DisableMissingModels(provCtx, providerID, existingModelIDs); err != nil {
		respondError(w, "failed to disable missing models", err, http.StatusInternalServerError)
		return
	}

	failoverRepo := failover.NewRepository(h.dbPool.Pool())
	seenModelIDs := make(map[string]bool)
	for _, mid := range existingModelIDs {
		seenModelIDs[mid] = true
	}
	for modelID := range seenModelIDs {
		if err := failoverRepo.SyncForModel(provCtx, modelID); err != nil {
			respondError(w, "failed to sync failover group", err, http.StatusInternalServerError)
			return
		}
	}

	now := time.Now()
	updateQuery := `UPDATE providers SET last_discovered_at = $1 WHERE id = $2`
	if _, err := h.dbPool.Pool().Exec(provCtx, updateQuery, now, providerID); err != nil {
		respondError(w, "failed to update provider", nil, http.StatusInternalServerError)
		return
	}

	response := map[string]interface{}{
		"discovered": len(models),
		"models":     models,
	}

	writeJSON(w, response)
}

func (h *Handler) GetProviderUsage(w http.ResponseWriter, r *http.Request) {
	providerID, ok := parseUUIDParam(w, r, "id", "provider ID")
	if !ok {
		return
	}

	prov, err := h.providerRepo.Get(r.Context(), providerID)
	if err != nil {
		http.Error(w, "provider not found", http.StatusNotFound)
		return
	}

	discovery := provider.NewDiscoveryService()

	switch provider.DetectProviderType(prov.BaseURL) {
	case "zai-coding":
		quota, err := discovery.GetZAICodingQuota(r.Context(), prov, h.cfg.MasterKey)
		if err != nil {
			respondError(w, "failed to fetch usage", err, http.StatusInternalServerError)
			return
		}
		writeJSON(w, quota)
		return
	case "nanogpt":
		usage, err := discovery.GetNanoGPTUsage(r.Context(), prov, h.cfg.MasterKey)
		if err != nil {
			respondError(w, "failed to fetch usage", err, http.StatusInternalServerError)
			return
		}
		writeJSON(w, usage)
		return
	case "openrouter":
		keyBalance, err := discovery.GetOpenRouterBalance(r.Context(), prov, h.cfg.MasterKey)
		if err != nil {
			respondError(w, "failed to fetch key balance", err, http.StatusInternalServerError)
			return
		}
		writeJSON(w, keyBalance)
		return
	default:
		http.Error(w, "usage information not supported for this provider type", http.StatusBadRequest)
		return
	}
}

func (h *Handler) GetProviderBalance(w http.ResponseWriter, r *http.Request) {
	providerID, ok := parseUUIDParam(w, r, "id", "provider ID")
	if !ok {
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
			respondError(w, "failed to fetch balance", err, http.StatusInternalServerError)
			return
		}
		writeJSON(w, balance)
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
		respondError(w, "failed to list providers", nil, http.StatusInternalServerError)
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

		events.Publish(events.Event{
			Type:     "request.discovery.provider_starting",
			Severity: "info",
			Message:  fmt.Sprintf("Discovering models from %s…", prov.Name),
			Metadata: map[string]interface{}{"provider_id": prov.ID, "provider": prov.Name},
		})

		provCtx, provCancel := context.WithTimeout(context.WithoutCancel(r.Context()), 180*time.Second)
		result := DiscoverAllResult{
			ProviderName: prov.Name,
		}

		models, discoverErr := discovery.DiscoverModels(provCtx, prov, h.cfg.MasterKey)

		if discoverErr != nil {
			result.Error = discoverErr.Error()
			failed++
			provCancel()
			events.Publish(events.Event{
				Type:     "discovery.provider_failed",
				Severity: "error",
				Message:  fmt.Sprintf("Failed to discover models from %s: %s", prov.Name, discoverErr.Error()),
				Metadata: map[string]interface{}{"provider": prov.Name, "error": discoverErr.Error()},
			})
			results = append(results, result)
			continue
		}

		result.Discovered = len(models)
		totalDiscovered += len(models)
		succeeded++

		events.Publish(events.Event{
			Type:     "discovery.provider_fetched",
			Severity: "success",
			Message:  fmt.Sprintf("Fetched %d models from %s", len(models), prov.Name),
			Metadata: map[string]interface{}{"provider": prov.Name, "count": len(models)},
		})

		// Enrich models with data from models.dev.
		if cache := provider.GetModelsDevCache(); cache != nil {
			enriched := cache.EnrichModels(models)
			if enriched > 0 {
				events.Publish(events.Event{
					Type:     "discovery.enriched",
					Severity: "info",
					Message:  fmt.Sprintf("Enriched %d/%d models from models.dev catalogue", enriched, len(models)),
					Metadata: map[string]interface{}{"provider": prov.Name, "enriched": enriched, "total": len(models)},
				})
			}
		}

		existingModelIDs := make([]string, 0, len(models))
		for _, m := range models {
			if err := modelRepo.Upsert(provCtx, m); err != nil {
				log.Printf("[discovery] warning: failed to upsert model %s for provider %s: %v", m.ModelID, prov.Name, err)
				continue
			}
			existingModelIDs = append(existingModelIDs, m.ModelID)
		}

		_, _ = modelRepo.DisableMissingModels(provCtx, prov.ID, existingModelIDs)

		seenModelIDs := make(map[string]bool)
		for _, mid := range existingModelIDs {
			seenModelIDs[mid] = true
		}
		for modelID := range seenModelIDs {
			_ = failoverRepo.SyncForModel(provCtx, modelID)
		}

		now := time.Now()
		_, _ = h.dbPool.Pool().Exec(provCtx,
			`UPDATE providers SET last_discovered_at = $1 WHERE id = $2`, now, prov.ID)

		provCancel()
		results = append(results, result)
	}

	writeJSON(w, map[string]interface{}{
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
		respondError(w, "failed to list providers", nil, http.StatusInternalServerError)
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

		provCtx, provCancel := context.WithTimeout(context.Background(), 30*time.Second)

		providerType := provider.DetectProviderType(prov.BaseURL)
		result := QuotaRefreshResult{
			ProviderName: prov.Name,
			ProviderType: providerType,
		}

		switch providerType {
		case "nanogpt":
			_, err := discovery.GetNanoGPTUsage(provCtx, prov, h.cfg.MasterKey)
			if err != nil {
				result.Error = err.Error()
				failed++
			} else {
				result.Refreshed = true
				refreshed++
			}
		case "zai-coding":
			_, err := discovery.GetZAICodingQuota(provCtx, prov, h.cfg.MasterKey)
			if err != nil {
				result.Error = err.Error()
				failed++
			} else {
				result.Refreshed = true
				refreshed++
			}
		case "openrouter":
			_, err := discovery.GetOpenRouterBalance(provCtx, prov, h.cfg.MasterKey)
			if err != nil {
				result.Error = err.Error()
				failed++
			} else {
				result.Refreshed = true
				refreshed++
			}
		case "deepseek":
			_, err := discovery.GetDeepSeekBalance(provCtx, prov, h.cfg.MasterKey)
			if err != nil {
				result.Error = err.Error()
				failed++
			} else {
				result.Refreshed = true
				refreshed++
			}
		default:
			provCancel()
			skipped++
			continue
		}

		provCancel()
		results = append(results, result)
	}

	writeJSON(w, map[string]interface{}{
		"results":   results,
		"refreshed": refreshed,
		"failed":    failed,
		"skipped":   skipped,
	})
}
