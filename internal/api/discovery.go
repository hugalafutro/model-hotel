package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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
	modelRepoRecordMissing = func(repo *model.Repository, ctx context.Context, providerID uuid.UUID, providerName string, modelIDs []string) (disabled, pending []model.DisabledModelRef, err error) {
		return repo.RecordMissingModels(ctx, providerID, providerName, modelIDs)
	}
	discoverModelsForConfirm = func(ctx context.Context, svc *provider.DiscoveryService, prov *provider.Provider, masterKey string) ([]*model.Model, error) {
		return svc.DiscoverModels(ctx, prov, masterKey)
	}
	failoverRepoSyncForModel = func(repo *failover.Repository, ctx context.Context, modelID string) (*failover.SyncResult, error) {
		return repo.SyncForModel(ctx, modelID)
	}
	failoverRepoRevalidateCustomGroups = func(repo *failover.Repository, ctx context.Context) (*failover.SyncResult, error) {
		return repo.RevalidateCustomGroups(ctx)
	}
)

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
	r.Route("/discovery/changes", func(r chi.Router) {
		r.Get("/", h.GetDiscoveryChanges)
		r.Post("/ack", h.AckDiscoveryChanges)
	})
}

// GetDiscoveryChanges returns the unseen background-discovery diffs (newest
// first) and the total affected-model count powering the Models nav badge.
func (h *Handler) GetDiscoveryChanges(w http.ResponseWriter, r *http.Request) {
	entries, err := listPendingDiscoveryChanges(r.Context(), h.dbPool.Pool())
	if err != nil {
		respondError(w, "failed to load discovery changes", err, http.StatusInternalServerError)
		return
	}
	// Fold net-zero metadata round-trips so a value that swung out and back across
	// several background runs stops inflating the badge and cluttering the modal.
	entries = collapseRoundTrips(entries)
	count := 0
	for i := range entries {
		count += countAffected(entries[i].Diff)
	}
	writeJSON(w, DiscoveryChangesResponse{Entries: entries, Count: count})
}

// AckDiscoveryChanges atomically marks all unseen background-discovery diffs as
// seen and returns exactly the rows it cleared, so the client can populate the
// review modal from this response instead of a possibly-stale poll. Count is 0:
// the badge is now empty (Entries carries the just-acked rows for display only).
func (h *Handler) AckDiscoveryChanges(w http.ResponseWriter, r *http.Request) {
	entries, err := markDiscoveryChangesSeen(r.Context(), h.dbPool.Pool())
	if err != nil {
		respondError(w, "failed to acknowledge discovery changes", err, http.StatusInternalServerError)
		return
	}
	if entries == nil {
		entries = []DiscoveryChangeEntry{}
	}
	// Collapse round-trips here too so the modal populated from this ack response
	// matches the (already collapsed) badge the user clicked.
	entries = collapseRoundTrips(entries)
	writeJSON(w, DiscoveryChangesResponse{Entries: entries, Count: 0})
}

// DiscoverProviderModels discovers and imports models from a specific provider.
func (h *Handler) DiscoverProviderModels(w http.ResponseWriter, r *http.Request) {
	providerID, ok := parseUUIDParam(w, r, "id", "provider ID")
	if !ok {
		return
	}

	prov, err := h.providerRepo.Get(r.Context(), providerID)
	if err != nil {
		respondLookupError(w, err, pgx.ErrNoRows, "provider not found", "failed to load provider")
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

	snapshot, err := SnapshotProviderModels(provCtx, modelRepo, providerID)
	if err != nil {
		respondError(w, fmt.Sprintf("failed to snapshot models for provider %s", prov.Name), err, http.StatusInternalServerError)
		return
	}
	DampenOpenRouterPriceJitter(prov.BaseURL, snapshot, models)

	existingModelIDs := make([]string, 0, len(models))
	upsertedModels := make([]*model.Model, 0, len(models))
	for _, m := range models {
		if err := modelRepo.Upsert(provCtx, m); err != nil {
			respondError(w, fmt.Sprintf("failed to upsert model %s for provider %s", m.ModelID, prov.Name), err, http.StatusInternalServerError)
			return
		}
		existingModelIDs = append(existingModelIDs, m.ModelID)
		upsertedModels = append(upsertedModels, m)
	}

	// An interactive Discover never disables models. Disabling requires
	// MissingScanThreshold consecutive confirmed-missing scans, so a single
	// on-demand scan can never reach the threshold on its own; the only thing
	// running the confirmation probes here would achieve is stalling this
	// request for the full probe backoff (~70s), which overruns the 60s HTTP
	// timeout on this route and, for the HA config-sync import path, makes the
	// initiating member's sync appear to fail. The scheduled/background sweep
	// (cmd/server/main.go) owns miss-recording and disabling; this handler just
	// fetches, upserts, and syncs failover for the models it did see.
	var disabledRefs []model.DisabledModelRef

	diff := BuildDiscoveryDiff(snapshot, upsertedModels, disabledRefs)

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

// respondQuotaError maps a quota/usage/balance fetch error to an HTTP response.
// A dead upstream credential (provider.ErrProviderKeyInvalid) is a provider
// configuration problem, not a server fault: respond 424 Failed Dependency - a
// 4xx, so the access log records it at WARN rather than ERROR - and skip the
// ERROR line respondError would emit, since the fetch layer already logged the
// rejection once at WARN. The sidebar badge hides on any non-2xx, so the dead
// provider simply shows no badge instead of spamming errors on every poll.
// Anything else stays a logged 500.
func respondQuotaError(w http.ResponseWriter, providerName, resource string, err error) {
	if errors.Is(err, provider.ErrProviderKeyInvalid) {
		http.Error(w, fmt.Sprintf("provider key invalid or inactive for %s", providerName), http.StatusFailedDependency)
		return
	}
	respondError(w, fmt.Sprintf("failed to fetch %s for provider %s", resource, providerName), err, http.StatusInternalServerError)
}

// GetProviderUsage fetches usage/quota information for a provider.
func (h *Handler) GetProviderUsage(w http.ResponseWriter, r *http.Request) {
	providerID, ok := parseUUIDParam(w, r, "id", "provider ID")
	if !ok {
		return
	}

	prov, err := h.providerRepo.Get(r.Context(), providerID)
	if err != nil {
		respondLookupError(w, err, pgx.ErrNoRows, "provider not found", "failed to load provider")
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
			respondQuotaError(w, prov.Name, "usage", err)
			return
		}
		writeJSON(w, quota)
		return
	case "nanogpt":
		usage, err := discovery.GetNanoGPTUsage(quotaCtx, prov, h.cfg.MasterKey)
		if err != nil {
			respondQuotaError(w, prov.Name, "usage", err)
			return
		}
		writeJSON(w, usage)
		return
	case "openrouter":
		keyBalance, err := discovery.GetOpenRouterBalance(quotaCtx, prov, h.cfg.MasterKey)
		if err != nil {
			respondQuotaError(w, prov.Name, "key balance", err)
			return
		}
		writeJSON(w, keyBalance)
		return
	case "neuralwatt":
		quota, err := discovery.GetNeuralWattQuota(quotaCtx, prov, h.cfg.MasterKey)
		if err != nil {
			respondQuotaError(w, prov.Name, "quota", err)
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
		respondLookupError(w, err, pgx.ErrNoRows, "provider not found", "failed to load provider")
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
			respondQuotaError(w, prov.Name, "balance", err)
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
		respondLookupError(w, err, pgx.ErrNoRows, "provider not found", "failed to load provider")
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
		respondQuotaError(w, prov.Name, "ollama cloud account", err)
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
	// Request-bound: skip miss-recording so the confirmation-probe backoff
	// cannot overrun this route's 60s timeout. The scheduled sweep disables.
	results, succeeded, failed, totalDiscovered, err := h.discoverAllProviders(r.Context(), false)
	if err != nil {
		respondError(w, "failed to list providers", nil, http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{
		"results":    results,
		"succeeded":  succeeded,
		"failed":     failed,
		"discovered": totalDiscovered,
	})
}

// discoverAllProviders runs discovery for every enabled, autodiscovery-enabled
// provider and upserts the resulting models. It is the shared core behind the
// DiscoverAllModels HTTP handler and the config-sync import (so a freshly-synced
// member populates its models without a manual discover click). The returned
// error is non-nil only when the provider list cannot be read; per-provider
// failures are recorded in the results. ctx governs cancellation; each provider
// runs under its own detached timeout so one client disconnect does not abort
// the sweep.
//
// recordMisses gates the confirmation-probe + miss-recording layer. It must be
// false on any caller that runs under an HTTP request deadline (the manual
// "Discover All" button and the HA config-sync import), because the probes can
// sleep for the full backoff (~70s) and overrun the 60s route timeout, writing
// the response to a dead connection and making a config sync look like it
// failed. A single on-demand scan can never disable a model anyway (that needs
// MissingScanThreshold consecutive confirmed scans), so the scheduled/background
// sweep is the only path that records misses.
func (h *Handler) discoverAllProviders(ctx context.Context, recordMisses bool) (results []DiscoverAllResult, succeeded, failed, totalDiscovered int, err error) {
	providers, err := h.providerRepo.List(ctx)
	if err != nil {
		return nil, 0, 0, 0, err
	}

	discovery := newDiscoveryService()
	modelRepo := newModelRepo(h.dbPool.Pool())
	failoverRepo := newFailoverRepo(h.dbPool.Pool())

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

		provCtx, provCancel := context.WithTimeout(context.WithoutCancel(ctx), 180*time.Second)
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

		snapshot, snapErr := SnapshotProviderModels(provCtx, modelRepo, prov.ID)
		if snapErr != nil {
			debuglog.Debug("discovery: failed to snapshot models", "provider", prov.Name, "error", snapErr)
		}
		DampenOpenRouterPriceJitter(prov.BaseURL, snapshot, models)

		existingModelIDs := make([]string, 0, len(models))
		upsertedModels := make([]*model.Model, 0, len(models))
		upsertFailed := false
		for _, m := range models {
			if err := modelRepo.Upsert(provCtx, m); err != nil {
				debuglog.Warn("discovery: failed to upsert model", "model", m.ModelID, "provider", prov.Name, "error", err)
				upsertFailed = true
				continue
			}
			existingModelIDs = append(existingModelIDs, m.ModelID)
			upsertedModels = append(upsertedModels, m)
		}

		// Miss recording needs a trustworthy membership picture: skip it when a
		// snapshot is unavailable (cannot confirm absentees), when any upsert
		// failed (a DB error must not count a listed model as missing), or when
		// the confirmation probes flag the scan as suspect. Disabling happens
		// only after MissingScanThreshold consecutive confirmed-missing scans.
		// recordMisses is false on request-bound callers so the ~70s probe
		// backoff never overruns their HTTP timeout (see the doc comment).
		var disabledRefs []model.DisabledModelRef
		if recordMisses && snapErr == nil && !upsertFailed {
			confirmedIDs, suspect := ConfirmMissingModels(provCtx, discovery, prov, h.cfg.MasterKey, existingModelIDs, snapshot, NewSuspectStreak(h.dbPool.Pool()))
			if suspect {
				debuglog.Warn("discovery: suspect scan, skipping missing-model recording", "provider", prov.Name, "provider_id", prov.ID)
			} else {
				var pendingRefs []model.DisabledModelRef
				disabledRefs, pendingRefs, err = modelRepoRecordMissing(modelRepo, provCtx, prov.ID, prov.Name, confirmedIDs)
				if err != nil {
					debuglog.Debug("discovery: failed to record missing models", "provider", prov.Name, "error", err)
				}
				if len(pendingRefs) > 0 {
					debuglog.Info("discovery: models confirmed missing but below disable threshold",
						"provider", prov.Name, "provider_id", prov.ID, "pending", len(pendingRefs), "threshold", model.MissingScanThreshold)
				}
			}
		}

		// Without the before-snapshot the diff cannot be classified; the scan
		// itself still completes and the result just omits the diff.
		var diff *DiscoveryDiff
		if snapErr == nil {
			diff = BuildDiscoveryDiff(snapshot, upsertedModels, disabledRefs)
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
		stampFailoverSynced(context.WithoutCancel(ctx), h.settingsRepo)
	}

	return results, succeeded, failed, totalDiscovered, nil
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
