package api

import (
	"context"
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
	"github.com/hugalafutro/model-hotel/internal/util"
)

// newDiscoveryService is the fallback DiscoveryService factory, used only by a
// Handler that has no factory of its own (a bare &Handler{} in a test). It is
// never reassigned: NewHandler sets Handler.newDiscovery instead, because a
// package-level assignment from a constructor is a data race as soon as two
// handlers are built concurrently, which parallel tests do.
var newDiscoveryService = func() *provider.DiscoveryService {
	return provider.NewDiscoveryService(nil, nil)
}

// discoveryService returns this handler's DiscoveryService. Every caller inside
// the package goes through it rather than the package variable, so a handler's
// SSRF-protected dial and redirect hooks cannot be swapped out from under it by
// another handler's construction.
func (h *Handler) discoveryService() *provider.DiscoveryService {
	if h.newDiscovery != nil {
		return h.newDiscovery()
	}
	return newDiscoveryService()
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
		r.Post("/ack", h.AckDiscoveryChanges)
	})
	r.Get("/discovery/status", h.GetDiscoveryStatus)
	// The provider rides in the path, not the body, so the audit trail can
	// name it: a modal-wide verdict fans out one call per provider, and rows
	// without an entity read as identical.
	r.Post("/discovery/{provider_id}/dismiss", h.DismissDiscoveryClaims)
	r.Post("/discovery/{provider_id}/unpin", h.UnpinDiscoveryClaims)
}

// settingKeyDiscoveryLastReviewed marks when the operator last opened the
// discrepancy modal, so flap counts can be reported "since your last visit".
const settingKeyDiscoveryLastReviewed = "_discovery_last_reviewed_at"

// DiscoveryStatusResponse powers the Models nav badge and its modal. ClaimCount
// counts Gone and Retired models: Stale, Suspect and Pinned are shown but never
// inflate the badge, so a non-zero badge always means something might actually
// be wrong.
// Retired counts because it is the same kind of fact as Gone — a model that was
// working and now is not — even though it came from the proxy refusing traffic
// rather than from the provider dropping it from its listing.
// InformationalUnseen drives the badge dot when ClaimCount is 0, and counts only
// the entries carrying something other than metadata `updated` changes: prices
// move on nearly every scan, so counting them would leave the dot permanently
// lit (see countInformationalUnseen).
// GroupClaims are the failover groups discovery disabled; they count toward
// ClaimCount alongside Gone models, because a disabled group means `hotel/`
// routing for that model has stopped working.
type DiscoveryStatusResponse struct {
	Claims              []ProviderClaims       `json:"claims"`
	GroupClaims         []GroupClaim           `json:"group_claims"`
	Informational       []DiscoveryChangeEntry `json:"informational"`
	ClaimCount          int                    `json:"claim_count"`
	InformationalUnseen int                    `json:"informational_unseen"`
}

// GetDiscoveryStatus derives the current claim set from live model state and
// pairs it with the informational journal feed.
//
// With ?review=1 (the modal-open fetch) it reads the previous last-reviewed
// stamp, computes flap counts against it, and only THEN writes the new stamp.
// Reading before writing is what makes "since your last visit" describe the
// previous visit instead of collapsing to zero. The 60s badge poll omits the
// parameter and never writes.
func (h *Handler) GetDiscoveryStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	pool := h.dbPool.Pool()
	now := time.Now()
	windowStart := now.Add(-ClaimWindow)

	rows, err := listClaimRows(ctx, pool)
	if err != nil {
		respondError(w, "failed to load discovery claims", err, http.StatusInternalServerError)
		return
	}

	window, err := flapCounts(ctx, pool, windowStart)
	if err != nil {
		respondError(w, "failed to load discovery flap counts", err, http.StatusInternalServerError)
		return
	}

	// Only the modal-open variant needs the since-review numbers, so the 60s
	// badge poll never pays for the second flap query.
	review := r.URL.Query().Get("review") == "1"
	sinceReview := map[flapKey]int{}
	if review {
		lastReviewed := parseLastReviewed(ctx, h.settingsRepo)
		switch {
		case lastReviewed.IsZero():
			// First ever review: everything in the window is new to this operator.
			sinceReview = window
		case lastReviewed.Before(windowStart):
			// Journal rows are pruned at ClaimWindow (PruneDiscoveryChanges), so a
			// stamp older than the window would ask flapCounts to look further
			// back than the surviving journal actually reaches, silently
			// deriving the number from rows that no longer exist. Clamp the
			// lookback to the window: past that point the honest answer is
			// "everything we still know about", which is exactly the window
			// count already computed above.
			sinceReview = window
		default:
			if sinceReview, err = flapCounts(ctx, pool, lastReviewed); err != nil {
				respondError(w, "failed to load discovery flap counts", err, http.StatusInternalServerError)
				return
			}
		}
	}

	claims, count := buildProviderClaims(rows, window, sinceReview, now)

	// A disabled failover group stops `hotel/<model>` routing outright, so it is
	// a claim on the same footing as a gone model. Derived live from
	// model_failover_groups, never from the journal, so it resolves by itself
	// when the group comes back.
	groupClaims, err := listGroupClaims(ctx, pool)
	if err != nil {
		respondError(w, "failed to load discovery group claims", err, http.StatusInternalServerError)
		return
	}
	count += len(groupClaims)

	pending, err := listPendingDiscoveryChanges(ctx, pool)
	if err != nil {
		respondError(w, "failed to load discovery changes", err, http.StatusInternalServerError)
		return
	}
	// A group that zone 1 is already claiming must not also appear in the zone 2
	// feed; one that is NOT claimed (disabled before migration 062, so it carries
	// no provenance stamp) must, or it would be invisible everywhere. Hence the
	// live claim set rather than a blanket strip of the bucket.
	claimedGroups := make(map[string]struct{}, len(groupClaims))
	for _, g := range groupClaims {
		claimedGroups[g.DisplayModel] = struct{}{}
	}
	informational := stripClaimedBuckets(collapseRoundTrips(pending), claimedGroups)

	// Stamp last, and never in read-only mode: a GET must not 403 there, so the
	// write is skipped rather than rejected.
	if review && !h.cfg.DemoReadOnly {
		// RFC3339Nano, not RFC3339: discovery_changes.detected_at is a
		// microsecond-precision TIMESTAMPTZ (`DEFAULT now()`), and a
		// second-truncated stamp can land at or after a journal row that was
		// actually written before this review — re-counting on the very next
		// review something the operator just saw.
		if err := h.settingsRepo.Set(ctx, settingKeyDiscoveryLastReviewed, now.Format(time.RFC3339Nano)); err != nil {
			debuglog.Error("discovery: failed to stamp last-reviewed", "error", err)
		}
	}

	writeJSON(w, DiscoveryStatusResponse{
		Claims:              claims,
		GroupClaims:         groupClaims,
		Informational:       informational,
		ClaimCount:          count,
		InformationalUnseen: countInformationalUnseen(informational),
	})
}

// parseLastReviewed returns the stored review stamp, or the zero time when it
// is unset or unparseable. A corrupt value degrades to "never reviewed", which
// over-reports flaps rather than hiding them.
func parseLastReviewed(ctx context.Context, store SettingsStore) time.Time {
	raw := store.GetWithDefault(ctx, settingKeyDiscoveryLastReviewed, "")
	if raw == "" {
		return time.Time{}
	}
	ts, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return time.Time{}
	}
	return ts
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
// It keeps its own request-bound copy of the scan rather than calling
// discoverOne because every failure here is answered as an HTTP error, while
// the shared scan records failures and carries on.
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

	discovery := h.discoveryService()
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
		Message:  fmt.Sprintf("Fetched %s from %s", util.Count(len(models), "model", "models"), prov.Name),
		Metadata: map[string]any{"provider": prov.Name, "count": len(models)},
	})

	// Enrich models with data from models.dev (fills gaps for models not
	// covered by hardcoded catalogs).
	if cache := provider.GetModelsDevCache(); cache != nil {
		enriched := cache.EnrichModels(models, provider.TypeOf(prov))
		if enriched > 0 {
			events.Publish(events.Event{
				Type:     "discovery.enriched",
				Severity: "info",
				Source:   "discovery",
				Message:  fmt.Sprintf("Enriched %d/%d models from models.dev catalogue", enriched, len(models)),
				Metadata: map[string]any{"provider": prov.Name, "enriched": enriched, "total": len(models)},
			})
		}
	}
	// Runs unconditionally: modality arrays and the derived endpoint class
	// must be consistent even when models.dev is unreachable.
	provider.NormalizeModels(models)

	modelRepo := newModelRepo(h.dbPool.Pool())

	snapshot, err := SnapshotProviderModels(provCtx, modelRepo, providerID)
	if err != nil {
		respondError(w, fmt.Sprintf("failed to snapshot models for provider %s", prov.Name), err, http.StatusInternalServerError)
		return
	}
	DampenOpenRouterPriceJitter(provider.TypeOf(prov), snapshot, models)

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
	// Raw UPDATE bypasses the repository, so the read-through provider cache
	// still holds the old last_discovered_at; evict this provider's entries.
	provider.EvictProviderCacheByID(providerID)

	response := map[string]any{
		"discovered": len(models),
		"models":     models,
		"diff":       diff,
	}

	writeJSON(w, response)
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
	writeJSON(w, map[string]any{
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

	discovery := h.discoveryService()
	modelRepo := newModelRepo(h.dbPool.Pool())
	failoverRepo := newFailoverRepo(h.dbPool.Pool())

	for _, prov := range providers {
		if !prov.Enabled || !prov.AutodiscoveryEnabled {
			continue
		}
		result := h.discoverOne(ctx, discovery, modelRepo, failoverRepo, prov, recordMisses)
		if result.Error != "" {
			failed++
		} else {
			succeeded++
			totalDiscovered += result.Discovered
		}
		results = append(results, result)
	}

	return results, succeeded, failed, totalDiscovered, nil
}

// discoverOne scans a single provider and upserts what it lists: the
// per-provider half of discoverAllProviders, shared with the rediscovery an
// enabled provider gets when a save changes its address, type, key or enabled
// flag. Each run has its own detached timeout so a caller's cancellation never
// aborts a scan mid-upsert. Success is result.Error == "".
func (h *Handler) discoverOne(ctx context.Context, discovery *provider.DiscoveryService, modelRepo *model.Repository, failoverRepo *failover.Repository, prov *provider.Provider, recordMisses bool) DiscoverAllResult {
	events.Publish(events.Event{
		Type:     "request.discovery.provider_starting",
		Severity: "info",
		Source:   "proxy",
		Message:  fmt.Sprintf("Discovering models from %s…", prov.Name),
		Metadata: map[string]any{"provider_id": prov.ID, "provider": prov.Name},
	})

	provCtx, provCancel := context.WithTimeout(context.WithoutCancel(ctx), 180*time.Second)
	defer provCancel()
	result := DiscoverAllResult{
		ProviderName: prov.Name,
	}

	models, discoverErr := discovery.DiscoverModels(provCtx, prov, h.cfg.MasterKey)

	if discoverErr != nil {
		result.Error = discoverErr.Error()
		events.Publish(events.Event{
			Type:     "discovery.provider_failed",
			Severity: "error",
			Source:   "discovery",
			Message:  fmt.Sprintf("Failed to discover models from %s: %s", prov.Name, discoverErr.Error()),
			Metadata: map[string]any{"provider": prov.Name, "error": discoverErr.Error()},
		})
		return result
	}

	result.Discovered = len(models)

	events.Publish(events.Event{
		Type:     "discovery.provider_fetched",
		Severity: "success",
		Source:   "discovery",
		Message:  fmt.Sprintf("Fetched %s from %s", util.Count(len(models), "model", "models"), prov.Name),
		Metadata: map[string]any{"provider": prov.Name, "count": len(models)},
	})

	// Enrich models with data from models.dev.
	if cache := provider.GetModelsDevCache(); cache != nil {
		enriched := cache.EnrichModels(models, provider.TypeOf(prov))
		if enriched > 0 {
			events.Publish(events.Event{
				Type:     "discovery.enriched",
				Severity: "info",
				Source:   "discovery",
				Message:  fmt.Sprintf("Enriched %d/%d models from models.dev catalogue", enriched, len(models)),
				Metadata: map[string]any{"provider": prov.Name, "enriched": enriched, "total": len(models)},
			})
		}
	}
	// Runs unconditionally: modality arrays and the derived endpoint class
	// must be consistent even when models.dev is unreachable.
	provider.NormalizeModels(models)

	snapshot, snapErr := SnapshotProviderModels(provCtx, modelRepo, prov.ID)
	if snapErr != nil {
		debuglog.Debug("discovery: failed to snapshot models", "provider", prov.Name, "error", snapErr)
	}
	DampenOpenRouterPriceJitter(provider.TypeOf(prov), snapshot, models)

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
			var err error
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
	// Reflect the scan in the failover "Last Sync" label.
	stampFailoverSynced(provCtx, h.settingsRepo)

	now := time.Now()
	if _, err := dbExec(h.dbPool.Pool(), provCtx,
		`UPDATE providers SET last_discovered_at = $1 WHERE id = $2`, now, prov.ID); err != nil {
		debuglog.Debug("discovery: failed to update last_discovered_at", "provider_id", prov.ID, "error", err)
	} else {
		// Raw UPDATE bypasses the repository; evict this provider's cache
		// entries so read-through Get sees the new last_discovered_at.
		provider.EvictProviderCacheByID(prov.ID)
	}
	// Last, once every row this scan writes is committed: the dashboard re-reads
	// its lists on this event, and the earlier fetched/enriched events fire
	// before the upserts, so a re-read on those would still see the old
	// catalogue. The request. prefix keeps it out of the toast stream like the
	// matching provider_starting event.
	events.Publish(events.Event{
		Type:     "request.discovery.provider_completed",
		Severity: "info",
		Source:   "discovery",
		Message:  fmt.Sprintf("Finished discovery for %s", prov.Name),
		Metadata: map[string]any{"provider_id": prov.ID, "provider": prov.Name, "count": len(models)},
	})
	return result
}
