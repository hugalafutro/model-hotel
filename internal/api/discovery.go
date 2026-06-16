package api

import (
	"context"
	"fmt"
	"math"
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

// Field codes for FieldChange entries; translated client-side.
//
// max_output_tokens is deliberately NOT tracked: providers and catalogs
// disagree on it constantly (and re-edit it — OpenRouter especially), so a diff
// on it is almost always provider-side noise rather than a meaningful change.
const (
	changeFieldInputPrice      = "input_price"
	changeFieldOutputPrice     = "output_price"
	changeFieldInputPriceCache = "input_price_cache"
	changeFieldContextLength   = "context_length"
)

// contextLengthRelTolerance absorbs the binary-vs-decimal unit differences
// between sources (e.g. 262144 vs 262000, 131072 vs 128000 — at most ~4.9% at
// the M scale) so they don't register as changes. Real context-window changes
// jump between standard sizes (≥25%), well clear of this band.
const contextLengthRelTolerance = 0.07

// FieldChange describes one pricing/context metadata field whose value changed
// for an existing model between scans. Old/New ride as nullable JSON numbers
// (context ints and prices alike); a nil pointer means the field was unset. The
// Field code tells the client how to format the value.
type FieldChange struct {
	Field string   `json:"field"`
	Old   *float64 `json:"old,omitempty"`
	New   *float64 `json:"new,omitempty"`
}

// ModelUpdate groups the metadata field changes detected for one existing model.
type ModelUpdate struct {
	ModelID string        `json:"model_id"`
	Changes []FieldChange `json:"changes"`
}

// DiscoveryDiff summarizes the state changes one provider scan caused.
type DiscoveryDiff struct {
	Added                 []ModelChange               `json:"added,omitempty"`
	Reenabled             []ModelChange               `json:"reenabled,omitempty"`
	Disabled              []ModelChange               `json:"disabled,omitempty"`
	Updated               []ModelUpdate               `json:"updated,omitempty"`
	FailoverDeletedGroups []failover.DeletedGroupInfo `json:"failover_deleted_groups,omitempty"`
	FailoverUpdatedGroups []failover.UpdatedGroupInfo `json:"failover_updated_groups,omitempty"`
}

// ModelSnapshot captures a model's pre-scan state — enabled flags plus the
// pricing/context fields compared to detect metadata changes. The type is
// exported so the scheduled discovery loop (package main) can hold the snapshot
// returned by SnapshotProviderModels and pass it to BuildDiscoveryDiff; its
// fields stay package-private.
type ModelSnapshot struct {
	enabled          bool
	disabledManually bool
	inputPrice       *float64
	inputPriceCache  *float64
	outputPrice      *float64
	contextLength    *int
}

// SnapshotProviderModels maps model_id to its pre-scan state for one provider.
func SnapshotProviderModels(ctx context.Context, repo *model.Repository, providerID uuid.UUID) (map[string]ModelSnapshot, error) {
	existing, err := repo.List(ctx, &providerID)
	if err != nil {
		return nil, err
	}
	snap := make(map[string]ModelSnapshot, len(existing))
	for _, m := range existing {
		snap[m.ModelID] = ModelSnapshot{
			enabled:          m.Enabled,
			disabledManually: m.DisabledManually,
			inputPrice:       m.InputPricePerMillion,
			inputPriceCache:  m.InputPricePerMillionCacheHit,
			outputPrice:      m.OutputPricePerMillion,
			contextLength:    m.ContextLength,
		}
	}
	return snap, nil
}

// BuildDiscoveryDiff classifies one provider scan against its before-snapshot:
// upserted models absent from the snapshot are new; snapshot models that were
// discovery-disabled (not manually — Upsert never re-enables those) count as
// reappeared; an unchanged-membership model whose pricing/context fields moved
// is an update; disabledRefs are the models this scan just disabled.
func BuildDiscoveryDiff(snapshot map[string]ModelSnapshot, upserted []*model.Model, disabledRefs []model.DisabledModelRef) *DiscoveryDiff {
	diff := &DiscoveryDiff{}
	for _, m := range upserted {
		prev, ok := snapshot[m.ModelID]
		switch {
		case !ok:
			diff.Added = append(diff.Added, ModelChange{ModelID: m.ModelID, Reason: changeReasonNewModel})
		case !prev.enabled && !prev.disabledManually:
			diff.Reenabled = append(diff.Reenabled, ModelChange{ModelID: m.ModelID, Reason: changeReasonReappeared})
		case prev.disabledManually:
			// The user has manually disabled this model, so skip metadata-change
			// detection: a hidden model's price/context churn should not raise the
			// discovery-changes badge. (It is still upserted so the value stays
			// current if the user re-enables it.)
		default:
			if changes := diffModelFields(prev, m); len(changes) > 0 {
				diff.Updated = append(diff.Updated, ModelUpdate{ModelID: m.ModelID, Changes: changes})
			}
		}
	}
	for _, ref := range disabledRefs {
		diff.Disabled = append(diff.Disabled, ModelChange{ModelID: ref.ModelID, Reason: changeReasonNotListed})
	}
	return diff
}

// diffModelFields compares the pricing/context fields of an existing model's
// pre-scan snapshot against its freshly discovered (post-enrichment) values.
//
// A field's live-provenance (m.LiveMeta) gates whether a value→value change is
// reported, so the diff stays faithful to what Upsert actually persists: only a
// provider-reported (live) field overwrites a stored value, so only a live
// field can report a value→value change. A non-live field is fill-only at
// upsert — its stored value is kept — so its only reportable transition is
// filling a previously-unset (nil) value. This is what stops a flaky probe or a
// models.dev re-fetch from raising phantom "price changed" rows every restart.
func diffModelFields(prev ModelSnapshot, m *model.Model) []FieldChange {
	var changes []FieldChange
	if c, ok := diffFloatPtr(changeFieldInputPrice, prev.inputPrice, m.InputPricePerMillion, m.LiveMeta.InputPrice); ok {
		changes = append(changes, c)
	}
	if c, ok := diffFloatPtr(changeFieldOutputPrice, prev.outputPrice, m.OutputPricePerMillion, m.LiveMeta.OutputPrice); ok {
		changes = append(changes, c)
	}
	if c, ok := diffFloatPtr(changeFieldInputPriceCache, prev.inputPriceCache, m.InputPricePerMillionCacheHit, m.LiveMeta.InputPriceCache); ok {
		changes = append(changes, c)
	}
	if c, ok := diffContextLength(changeFieldContextLength, prev.contextLength, m.ContextLength, m.LiveMeta.ContextLength); ok {
		changes = append(changes, c)
	}
	return changes
}

// diffFloatPtr reports a price FieldChange when a scan changes a value. A nil
// new value is never a change: Upsert preserves the stored value when a scan
// omits a field, so reporting "value → unset" would be a phantom diff. Filling
// a previously-unset value (old nil → new set) is always a change. For two
// non-nil values the change is reported only when the field is live-sourced,
// because a non-live field is fill-only at upsert and its stored value is kept
// (reporting it would be a phantom diff). Comparison is at float32 precision
// because prices are stored in a REAL column — comparing a fresh float64
// against the float32-rounded stored value would otherwise jitter in the 7th
// decimal. Real price changes are far larger than float32 epsilon.
func diffFloatPtr(field string, oldVal, newVal *float64, live bool) (FieldChange, bool) {
	if newVal == nil {
		return FieldChange{}, false
	}
	if oldVal != nil {
		if !live || float32(*oldVal) == float32(*newVal) {
			return FieldChange{}, false
		}
	}
	return FieldChange{Field: field, Old: oldVal, New: newVal}, true
}

// diffContextLength reports a context-length FieldChange. A nil new value is
// never a change (Upsert preserves the stored value); filling a previously
// unset value always is. For two non-nil values the change is reported only
// when the field is live-sourced (a non-live field is fill-only at upsert) and
// the difference exceeds contextLengthRelTolerance (absorbing unit/representation
// noise between sources). Values ride the wire as nullable JSON numbers.
func diffContextLength(field string, oldVal, newVal *int, live bool) (FieldChange, bool) {
	if newVal == nil {
		return FieldChange{}, false
	}
	if oldVal != nil {
		o, n := float64(*oldVal), float64(*newVal)
		denom := math.Max(math.Abs(o), math.Abs(n))
		if !live || denom == 0 || math.Abs(o-n)/denom <= contextLengthRelTolerance {
			return FieldChange{}, false
		}
	}
	return FieldChange{Field: field, Old: intToFloatPtr(oldVal), New: intToFloatPtr(newVal)}, true
}

// intToFloatPtr widens an optional int to an optional float64, preserving nil.
func intToFloatPtr(v *int) *float64 {
	if v == nil {
		return nil
	}
	f := float64(*v)
	return &f
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
	count := 0
	for i := range entries {
		count += countAffected(entries[i].Diff)
	}
	writeJSON(w, DiscoveryChangesResponse{Entries: entries, Count: count})
}

// AckDiscoveryChanges marks all unseen background-discovery diffs as seen,
// clearing the badge once the user has reviewed them.
func (h *Handler) AckDiscoveryChanges(w http.ResponseWriter, r *http.Request) {
	if err := markDiscoveryChangesSeen(r.Context(), h.dbPool.Pool()); err != nil {
		respondError(w, "failed to acknowledge discovery changes", err, http.StatusInternalServerError)
		return
	}
	writeJSON(w, DiscoveryChangesResponse{Entries: []DiscoveryChangeEntry{}, Count: 0})
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

	disabledRefs, err := modelRepoDisableMissing(modelRepo, provCtx, providerID, prov.Name, existingModelIDs)
	if err != nil {
		respondError(w, fmt.Sprintf("failed to disable missing models for provider %s", prov.Name), err, http.StatusInternalServerError)
		return
	}

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

		snapshot, snapErr := SnapshotProviderModels(provCtx, modelRepo, prov.ID)
		if snapErr != nil {
			debuglog.Debug("discovery: failed to snapshot models", "provider", prov.Name, "error", snapErr)
		}

		existingModelIDs := make([]string, 0, len(models))
		upsertedModels := make([]*model.Model, 0, len(models))
		for _, m := range models {
			if err := modelRepo.Upsert(provCtx, m); err != nil {
				debuglog.Warn("discovery: failed to upsert model", "model", m.ModelID, "provider", prov.Name, "error", err)
				continue
			}
			existingModelIDs = append(existingModelIDs, m.ModelID)
			upsertedModels = append(upsertedModels, m)
		}

		disabledRefs, err := modelRepoDisableMissing(modelRepo, provCtx, prov.ID, prov.Name, existingModelIDs)
		if err != nil {
			debuglog.Debug("discovery: failed to disable missing models", "provider", prov.Name, "error", err)
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
