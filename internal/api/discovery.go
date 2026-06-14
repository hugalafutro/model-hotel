package api

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/events"
	"github.com/hugalafutro/model-hotel/internal/failover"
	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/provider"
)

// newDiscoveryService creates a DiscoveryService. NewHandler overwrites this
// with an SSRF-protected version; the default avoids nil-panics if a discovery
// call races ahead of initialization.
var newDiscoveryService = func() *provider.DiscoveryService {
	return provider.NewDiscoveryService(nil, nil)
}

// Injectable variables for test overrides.
var (
	newModelRepo    = model.NewRepository
	newFailoverRepo = failover.NewRepository
	dbExec          = func(pool *pgxpool.Pool, ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
		return pool.Exec(ctx, sql, args...)
	}
	modelRepoDisableMissing = func(repo *model.Repository, ctx context.Context, providerID uuid.UUID, providerName string, modelIDs []string) ([]model.DisabledModelRef, error) {
		return repo.DisableMissingModels(ctx, providerID, providerName, modelIDs)
	}
	failoverRepoSyncForModel = func(repo *failover.Repository, ctx context.Context, modelID string) (*failover.SyncResult, error) {
		return repo.SyncForModel(ctx, modelID)
	}
)

// ModelChange describes one model affected by a discovery scan.
type ModelChange struct {
	ModelID string `json:"model_id"`
	Reason  string `json:"reason"` // machine-readable: new_model | reappeared | not_listed
}

// Reason codes for ModelChange entries; translated client-side.
const (
	changeReasonNewModel   = "new_model"
	changeReasonReappeared = "reappeared"
	changeReasonNotListed  = "not_listed"
)

// DiscoveryDiff summarizes the state changes one provider scan caused.
type DiscoveryDiff struct {
	Added                 []ModelChange               `json:"added,omitempty"`
	Reenabled             []ModelChange               `json:"reenabled,omitempty"`
	Disabled              []ModelChange               `json:"disabled,omitempty"`
	FailoverDeletedGroups []failover.DeletedGroupInfo `json:"failover_deleted_groups,omitempty"`
	FailoverUpdatedGroups []failover.UpdatedGroupInfo `json:"failover_updated_groups,omitempty"`
}

// modelSnapshot captures a model's enabled state before a scan upserts it.
type modelSnapshot struct {
	enabled          bool
	disabledManually bool
}

// snapshotProviderModels maps model_id to its pre-scan enabled state for one provider.
func snapshotProviderModels(ctx context.Context, repo *model.Repository, providerID uuid.UUID) (map[string]modelSnapshot, error) {
	existing, err := repo.List(ctx, &providerID)
	if err != nil {
		return nil, err
	}
	snap := make(map[string]modelSnapshot, len(existing))
	for _, m := range existing {
		snap[m.ModelID] = modelSnapshot{enabled: m.Enabled, disabledManually: m.DisabledManually}
	}
	return snap, nil
}

// buildDiscoveryDiff classifies one provider scan against its before-snapshot:
// upserted models absent from the snapshot are new; snapshot models that were
// discovery-disabled (not manually — Upsert never re-enables those) count as
// reappeared; disabledRefs are the models this scan just disabled.
func buildDiscoveryDiff(snapshot map[string]modelSnapshot, upsertedModelIDs []string, disabledRefs []model.DisabledModelRef) *DiscoveryDiff {
	diff := &DiscoveryDiff{}
	for _, id := range upsertedModelIDs {
		prev, ok := snapshot[id]
		switch {
		case !ok:
			diff.Added = append(diff.Added, ModelChange{ModelID: id, Reason: changeReasonNewModel})
		case !prev.enabled && !prev.disabledManually:
			diff.Reenabled = append(diff.Reenabled, ModelChange{ModelID: id, Reason: changeReasonReappeared})
		}
	}
	for _, ref := range disabledRefs {
		diff.Disabled = append(diff.Disabled, ModelChange{ModelID: ref.ModelID, Reason: changeReasonNotListed})
	}
	return diff
}

// mergeSyncResult folds one SyncForModel result into the diff's failover
// slices. Safe on a nil diff (discover-all skips the diff when the snapshot
// failed) and a nil result.
func (d *DiscoveryDiff) mergeSyncResult(res *failover.SyncResult) {
	if d == nil || res == nil {
		return
	}
	d.FailoverDeletedGroups = append(d.FailoverDeletedGroups, res.DeletedGroups...)
	d.FailoverUpdatedGroups = append(d.FailoverUpdatedGroups, res.UpdatedGroups...)
}

// syncFailoverForScan syncs failover groups for every model a scan touched:
// the still-listed models and the newly disabled ones (whose stale group
// entries must be pruned the same way a manual failover Sync would). Results
// are folded into diff. onErr reports a failed sync (disabled marks which
// loop) and returns false to abort the remaining syncs.
func syncFailoverForScan(ctx context.Context, repo *failover.Repository, upsertedModelIDs []string, disabledRefs []model.DisabledModelRef, diff *DiscoveryDiff, onErr func(modelID string, disabled bool, err error) bool) bool {
	seenModelIDs := make(map[string]bool)
	for _, mid := range upsertedModelIDs {
		seenModelIDs[mid] = true
	}
	for modelID := range seenModelIDs {
		syncRes, err := failoverRepoSyncForModel(repo, ctx, modelID)
		if err != nil {
			if !onErr(modelID, false, err) {
				return false
			}
			continue
		}
		diff.mergeSyncResult(syncRes)
	}
	for _, d := range disabledRefs {
		syncRes, err := failoverRepoSyncForModel(repo, ctx, d.ModelID)
		if err != nil {
			if !onErr(d.ModelID, true, err) {
				return false
			}
			continue
		}
		diff.mergeSyncResult(syncRes)
	}
	return true
}

// RegisterProviderDiscovery mounts provider discovery and usage routes.
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
	r.Route("/providers/{id}/account", func(r chi.Router) {
		r.Get("/", h.GetOllamaCloudAccount)
	})
}

// DiscoverProviderModels discovers and imports models from a specific provider.
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

	if !prov.AutodiscoveryEnabled {
		http.Error(w, "autodiscovery is disabled for this provider", http.StatusForbidden)
		return
	}

	discovery := newDiscoveryService()
	// Use a context decoupled from the HTTP request deadline for discovery.
	// Provider availability tests (especially for slow/unreachable providers)
	// can exhaust the 60s chi middleware timeout before DB upserts run.
	provCtx, provCancel := context.WithTimeout(context.WithoutCancel(r.Context()), 180*time.Second)
	defer provCancel()
	models, err := discovery.DiscoverModels(provCtx, prov, h.cfg.MasterKey)
	if err != nil {
		provCancel()
		respondError(w, fmt.Sprintf("failed to discover models for provider %s", prov.Name), err, http.StatusInternalServerError)
		return
	}

	events.Publish(events.Event{
		Type:     "discovery.provider_fetched",
		Severity: "success",
		Source:   "discovery",
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
				Source:   "discovery",
				Message:  fmt.Sprintf("Enriched %d/%d models from models.dev catalogue", enriched, len(models)),
				Metadata: map[string]interface{}{"provider": prov.Name, "enriched": enriched, "total": len(models)},
			})
		}
	}

	modelRepo := newModelRepo(h.dbPool.Pool())

	snapshot, err := snapshotProviderModels(provCtx, modelRepo, providerID)
	if err != nil {
		respondError(w, fmt.Sprintf("failed to snapshot models for provider %s", prov.Name), err, http.StatusInternalServerError)
		return
	}

	existingModelIDs := make([]string, 0, len(models))
	for _, m := range models {
		if err := modelRepo.Upsert(provCtx, m); err != nil {
			respondError(w, fmt.Sprintf("failed to upsert model %s for provider %s", m.ModelID, prov.Name), err, http.StatusInternalServerError)
			return
		}
		existingModelIDs = append(existingModelIDs, m.ModelID)
	}

	disabledRefs, err := modelRepoDisableMissing(modelRepo, provCtx, providerID, prov.Name, existingModelIDs)
	if err != nil {
		respondError(w, fmt.Sprintf("failed to disable missing models for provider %s", prov.Name), err, http.StatusInternalServerError)
		return
	}

	diff := buildDiscoveryDiff(snapshot, existingModelIDs, disabledRefs)

	failoverRepo := newFailoverRepo(h.dbPool.Pool())
	if !syncFailoverForScan(provCtx, failoverRepo, existingModelIDs, disabledRefs, diff, func(modelID string, disabled bool, err error) bool {
		label := "model"
		if disabled {
			label = "disabled model"
		}
		respondError(w, fmt.Sprintf("failed to sync failover group for %s %s", label, modelID), err, http.StatusInternalServerError)
		return false
	}) {
		return
	}
	stampFailoverSynced(provCtx, h.settingsRepo)

	now := time.Now()
	updateQuery := `UPDATE providers SET last_discovered_at = $1 WHERE id = $2`
	if _, err := dbExec(h.dbPool.Pool(), provCtx, updateQuery, now, providerID); err != nil {
		respondError(w, fmt.Sprintf("failed to update provider %s", prov.Name), err, http.StatusInternalServerError)
		return
	}

	response := map[string]interface{}{
		"discovered": len(models),
		"models":     models,
		"diff":       diff,
	}

	writeJSON(w, response)
}

// GetProviderUsage fetches usage/quota information for a provider.
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

	discovery := newDiscoveryService()

	// Use a context decoupled from the HTTP request deadline for outbound
	// API calls. Client disconnects (navigation, tab close) cancel r.Context(),
	// which would abort in-flight provider API requests mid-flight.
	quotaCtx, quotaCancel := context.WithTimeout(context.WithoutCancel(r.Context()), 30*time.Second)
	defer quotaCancel()

	switch provider.DetectProviderType(prov.BaseURL) {
	case "zai-coding":
		quota, err := discovery.GetZAICodingQuota(quotaCtx, prov, h.cfg.MasterKey)
		if err != nil {
			respondError(w, fmt.Sprintf("failed to fetch usage for provider %s", prov.Name), err, http.StatusInternalServerError)
			return
		}
		writeJSON(w, quota)
		return
	case "nanogpt":
		usage, err := discovery.GetNanoGPTUsage(quotaCtx, prov, h.cfg.MasterKey)
		if err != nil {
			respondError(w, fmt.Sprintf("failed to fetch usage for provider %s", prov.Name), err, http.StatusInternalServerError)
			return
		}
		writeJSON(w, usage)
		return
	case "openrouter":
		keyBalance, err := discovery.GetOpenRouterBalance(quotaCtx, prov, h.cfg.MasterKey)
		if err != nil {
			respondError(w, fmt.Sprintf("failed to fetch key balance for provider %s", prov.Name), err, http.StatusInternalServerError)
			return
		}
		writeJSON(w, keyBalance)
		return
	case "neuralwatt":
		quota, err := discovery.GetNeuralWattQuota(quotaCtx, prov, h.cfg.MasterKey)
		if err != nil {
			respondError(w, fmt.Sprintf("failed to fetch quota for provider %s", prov.Name), err, http.StatusInternalServerError)
			return
		}
		if quota == nil {
			// Free tier or no quota data available
			w.WriteHeader(http.StatusNoContent)
			return
		}
		writeJSON(w, quota)
		return
	default:
		http.Error(w, "usage information not supported for this provider type", http.StatusBadRequest)
		return
	}
}

// GetProviderBalance fetches balance information for a provider.
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

	discovery := newDiscoveryService()

	// Use a context decoupled from the HTTP request deadline for outbound API calls.
	balanceCtx, balanceCancel := context.WithTimeout(context.WithoutCancel(r.Context()), 30*time.Second)
	defer balanceCancel()

	switch provider.DetectProviderType(prov.BaseURL) {
	case "deepseek":
		balance, err := discovery.GetDeepSeekBalance(balanceCtx, prov, h.cfg.MasterKey)
		if err != nil {
			respondError(w, fmt.Sprintf("failed to fetch balance for provider %s", prov.Name), err, http.StatusInternalServerError)
			return
		}
		writeJSON(w, balance)
		return
	default:
		http.Error(w, "balance information not supported for this provider type", http.StatusBadRequest)
		return
	}
}

// GetOllamaCloudAccount fetches account info from Ollama Cloud.
func (h *Handler) GetOllamaCloudAccount(w http.ResponseWriter, r *http.Request) {
	providerID, ok := parseUUIDParam(w, r, "id", "provider ID")
	if !ok {
		return
	}

	prov, err := h.providerRepo.Get(r.Context(), providerID)
	if err != nil {
		http.Error(w, "provider not found", http.StatusNotFound)
		return
	}

	if provider.DetectProviderType(prov.BaseURL) != "ollama-cloud" {
		http.Error(w, "account information not supported for this provider type", http.StatusBadRequest)
		return
	}

	discovery := newDiscoveryService()

	// Use a context decoupled from the HTTP request deadline for outbound API calls.
	accountCtx, accountCancel := context.WithTimeout(context.WithoutCancel(r.Context()), 30*time.Second)
	defer accountCancel()

	account, err := discovery.GetOllamaCloudAccount(accountCtx, prov, h.cfg.MasterKey)
	if err != nil {
		respondError(w, fmt.Sprintf("failed to fetch ollama cloud account for provider %s", prov.Name), err, http.StatusInternalServerError)
		return
	}
	writeJSON(w, account)
}

// DiscoverAllResult holds the result of discovering models from a single provider.
type DiscoverAllResult struct {
	ProviderName string         `json:"provider_name"`
	Discovered   int            `json:"discovered"`
	Diff         *DiscoveryDiff `json:"diff,omitempty"`
	Error        string         `json:"error,omitempty"`
}

// DiscoverAllModels discovers and imports models from all enabled providers.
func (h *Handler) DiscoverAllModels(w http.ResponseWriter, r *http.Request) {
	providers, err := h.providerRepo.List(r.Context())
	if err != nil {
		respondError(w, "failed to list providers", nil, http.StatusInternalServerError)
		return
	}

	discovery := newDiscoveryService()
	modelRepo := newModelRepo(h.dbPool.Pool())
	failoverRepo := newFailoverRepo(h.dbPool.Pool())

	var results []DiscoverAllResult
	totalDiscovered := 0
	succeeded := 0
	failed := 0

	for _, prov := range providers {
		if !prov.Enabled || !prov.AutodiscoveryEnabled {
			continue
		}

		events.Publish(events.Event{
			Type:     "request.discovery.provider_starting",
			Severity: "info",
			Source:   "proxy",
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
				Source:   "discovery",
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
			Source:   "discovery",
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
					Source:   "discovery",
					Message:  fmt.Sprintf("Enriched %d/%d models from models.dev catalogue", enriched, len(models)),
					Metadata: map[string]interface{}{"provider": prov.Name, "enriched": enriched, "total": len(models)},
				})
			}
		}

		snapshot, snapErr := snapshotProviderModels(provCtx, modelRepo, prov.ID)
		if snapErr != nil {
			debuglog.Debug("discovery: failed to snapshot models", "provider", prov.Name, "error", snapErr)
		}

		existingModelIDs := make([]string, 0, len(models))
		for _, m := range models {
			if err := modelRepo.Upsert(provCtx, m); err != nil {
				debuglog.Warn("discovery: failed to upsert model", "model", m.ModelID, "provider", prov.Name, "error", err)
				continue
			}
			existingModelIDs = append(existingModelIDs, m.ModelID)
		}

		disabledRefs, err := modelRepoDisableMissing(modelRepo, provCtx, prov.ID, prov.Name, existingModelIDs)
		if err != nil {
			debuglog.Debug("discovery: failed to disable missing models", "provider", prov.Name, "error", err)
		}

		// Without the before-snapshot the diff cannot be classified; the scan
		// itself still completes and the result just omits the diff.
		var diff *DiscoveryDiff
		if snapErr == nil {
			diff = buildDiscoveryDiff(snapshot, existingModelIDs, disabledRefs)
		}

		syncFailoverForScan(provCtx, failoverRepo, existingModelIDs, disabledRefs, diff, func(modelID string, disabled bool, err error) bool {
			label := "model"
			if disabled {
				label = "disabled model"
			}
			debuglog.Debug("discovery: failed to sync failover for "+label, "model_id", modelID, "error", err)
			return true
		})
		result.Diff = diff

		now := time.Now()
		if _, err := dbExec(h.dbPool.Pool(), provCtx,
			`UPDATE providers SET last_discovered_at = $1 WHERE id = $2`, now, prov.ID); err != nil {
			debuglog.Debug("discovery: failed to update last_discovered_at", "provider_id", prov.ID, "error", err)
		}

		provCancel()
		results = append(results, result)
	}

	// Reflect the discovery in the failover "Last Sync" label whenever at least
	// one provider's groups were actually (re)synced.
	if succeeded > 0 {
		stampFailoverSynced(context.WithoutCancel(r.Context()), h.settingsRepo)
	}

	writeJSON(w, map[string]interface{}{
		"results":    results,
		"succeeded":  succeeded,
		"failed":     failed,
		"discovered": totalDiscovered,
	})
}

// QuotaRefreshResult holds the result of refreshing quotas for a single provider.
type QuotaRefreshResult struct {
	ProviderName string `json:"provider_name"`
	ProviderType string `json:"provider_type"`
	Refreshed    bool   `json:"refreshed"`
	Error        string `json:"error,omitempty"`
}

// RefreshAllQuotas refreshes quota information for all providers that support it.
func (h *Handler) RefreshAllQuotas(w http.ResponseWriter, r *http.Request) {
	providers, err := h.providerRepo.List(r.Context())
	if err != nil {
		respondError(w, "failed to list providers", nil, http.StatusInternalServerError)
		return
	}

	discovery := newDiscoveryService()

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
		case "ollama-cloud":
			_, err := discovery.GetOllamaCloudAccount(provCtx, prov, h.cfg.MasterKey)
			if err != nil {
				result.Error = err.Error()
				failed++
			} else {
				result.Refreshed = true
				refreshed++
			}
		case "neuralwatt":
			_, err := discovery.GetNeuralWattQuota(provCtx, prov, h.cfg.MasterKey)
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
