package main

// Discovery orchestration for the server binary: the startup/scheduled
// discovery runs, their per-provider scan stage, and the failover re-sync
// that follows every run. main wires a discoveryDeps once and hands it to
// runDiscovery via the scheduler and the startup runner.

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hugalafutro/model-hotel/internal/api"
	"github.com/hugalafutro/model-hotel/internal/config"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/events"
	"github.com/hugalafutro/model-hotel/internal/failover"
	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/provider"
	"github.com/hugalafutro/model-hotel/internal/proxy"
	"github.com/hugalafutro/model-hotel/internal/settings"
	"github.com/hugalafutro/model-hotel/internal/util"
)

type DiscoveryResult struct {
	ProvidersScanned int
	ProvidersFailed  int
	ModelsDiscovered int
	ModelsDisabled   int
	FailoverSyncErrs int
	// ModelsPruned counts discovery-retired rows deleted at the end of the
	// pass (model_prune_days). Not a scan outcome: a prune failure is logged
	// and never added to Errors.
	ModelsPruned int
	Errors       []string
}

// defaultModelPruneDays matches SETTING_DEFAULTS.model_prune_days in
// web/src/pages/Settings/defaults.ts; the two must agree or the slider shows
// one horizon while the pass applies another.
const defaultModelPruneDays = 30

// maxModelPruneDays is the same ceiling the API rule for model_prune_days
// applies in internal/api/settings.go. Enforced again here because values
// reach the store past that rule (config-sync import forwards an
// above-ceiling integer between fleet members, and backup restore and direct
// DB writes bypass it too), and beyond ~106751 days the horizon arithmetic
// overflows int64 and lands in the future, which would match every retired
// row.
const maxModelPruneDays = 365

// modelPruneBatch bounds one pass's deletions so a provider that retired its
// whole catalog cannot empty a member in a single tick; the remainder goes on
// the next passes.
const modelPruneBatch = 500

// discoveryDeps carries the wiring a discovery run needs; main fills it once
// and the startup and scheduled runners share it.
type discoveryDeps struct {
	cfg          *config.Config
	pool         *pgxpool.Pool
	providerRepo *provider.Repository
	modelRepo    *model.Repository
	failoverRepo *failover.Repository
	dialer       *proxy.SafeDialer
	// settingsRepo carries the outstanding-claim alert's threshold and its
	// persisted edge latch. Typed as the interface the API package already
	// defines so this stays a dependency on the two reads and two writes the
	// alert actually needs.
	settingsRepo api.SettingsStore
}

func publishDiscoveryEvent(source string, result DiscoveryResult) {
	switch {
	case result.ProvidersScanned == 0 && len(result.Errors) > 0:
		events.Publish(events.Event{
			Type:     "discovery.complete",
			Severity: "error",
			Message:  fmt.Sprintf("Discovery failed: %s", result.Errors[0]),
			Metadata: map[string]any{"source": source, "errors": result.Errors},
		})
	case result.ProvidersFailed > 0:
		events.Publish(events.Event{
			Type:     "discovery.complete",
			Severity: "warning",
			Message:  fmt.Sprintf("Discovery partially failed: %d/%d providers OK, %s found", result.ProvidersScanned-result.ProvidersFailed, result.ProvidersScanned, util.Count(result.ModelsDiscovered, "model", "models")),
			Metadata: map[string]any{"source": source, "errors": result.Errors, "models_pruned": result.ModelsPruned},
		})
	default:
		events.Publish(events.Event{
			Type:     "discovery.complete",
			Severity: "success",
			Message:  fmt.Sprintf("%s discovery complete: %s across %s", source, util.Count(result.ModelsDiscovered, "model", "models"), util.Count(result.ProvidersScanned, "provider", "providers")),
			Metadata: map[string]any{"source": source, "models_pruned": result.ModelsPruned},
		})
	}
}

// runDiscovery scans every enabled provider, upserts what each scan found,
// records confirmed-missing models, and re-syncs failover groups afterwards.
// source labels the trigger ("startup"/"scheduled") in events and the
// discovery change feed.
func runDiscovery(deps discoveryDeps, source string) DiscoveryResult {
	result := DiscoveryResult{}
	// Set when any background-discovery change row is recorded, so we can
	// publish a single live-update event for the Models nav badge.
	changesRecorded := false
	ctx := context.Background()
	providers, err := deps.providerRepo.List(ctx)
	if err != nil {
		debuglog.Error("discovery: failed to list providers", "error", err)
		result.Errors = append(result.Errors, fmt.Sprintf("failed to list providers: %v", err))
		return result
	}
	discoverySvc := provider.NewDiscoveryService(deps.dialer.DialContext, deps.dialer.CheckRedirect)
	var scannedOK []uuid.UUID
	for _, p := range providers {
		if !p.Enabled {
			continue
		}
		changed, ok := scanProvider(ctx, deps, discoverySvc, p, source, &result)
		if changed {
			changesRecorded = true
		}
		if ok {
			scannedOK = append(scannedOK, p.ID)
		}
	}

	// Prune before the failover sync so the sync sees the post-prune catalog.
	// Housekeeping like the journal prune below: it cannot fail the run.
	if pruned := pruneRetiredModels(ctx, deps, scannedOK, time.Now()); pruned > 0 {
		result.ModelsPruned = pruned
		changesRecorded = true
	}

	if syncFailoverAfterDiscovery(ctx, deps, source, providers, &result) {
		changesRecorded = true
	}

	// One live-update nudge so the Models nav badge refreshes without a
	// reload; the badge query re-fetches the authoritative count.
	if changesRecorded {
		events.Publish(events.Event{
			Type:     "discovery.changes_pending",
			Severity: "info",
			Source:   "discovery",
			Message:  "Background discovery recorded model changes",
			Metadata: map[string]any{"source": source},
		})
	}

	// Retention. Safe only because claims are derived from live model state: a
	// journal row can no longer be the sole evidence of a pending claim, so the
	// feed can be trimmed to the same window flap counts read from. Pruning is
	// housekeeping, not part of the scan: a failure here must not turn an
	// otherwise-completed run into a failed one.
	if deleted, err := api.PruneDiscoveryChanges(ctx, deps.pool, time.Now().Add(-api.ClaimWindow)); err != nil {
		debuglog.Error("discovery: prune change journal failed", "error", err)
	} else if deleted > 0 {
		debuglog.Info("discovery: pruned change journal", "rows", deleted)
	}

	// The unattended-badge nudge. Derived from the same live claim set the
	// badge reads, evaluated here because this is the only place that runs on
	// the operator's discovery cadence. Housekeeping exactly like the prune
	// above: the statement-scoped `if err :=` makes it structurally impossible
	// to touch result or abort the run, so a broken settings store or a failed
	// claim query cannot turn a completed scan into a failed one.
	if err := api.EvaluateClaimAgeAlert(ctx, deps.pool, deps.settingsRepo, time.Now()); err != nil {
		debuglog.Error("discovery: outstanding-claims alert evaluation failed", "error", err)
	}

	return result
}

// scanProvider runs one enabled provider's discovery pass: discover, enrich
// and normalize, upsert, record confirmed-missing models, and append the
// provider's change-feed entry. It updates result's counters and reports
// whether a discovery-change row was recorded and whether the scan succeeded.
func scanProvider(ctx context.Context, deps discoveryDeps, discoverySvc *provider.DiscoveryService, p *provider.Provider, source string, result *DiscoveryResult) (changed, ok bool) {
	result.ProvidersScanned++
	models, err := discoverySvc.DiscoverModels(ctx, p, deps.cfg.MasterKey)
	if err != nil {
		debuglog.Error("discovery: failed for provider", "provider", p.Name, "error", err)
		result.ProvidersFailed++
		result.Errors = append(result.Errors, fmt.Sprintf("provider %s: %v", p.Name, err))
		// Update last_discovered_at even on failure so the UI reflects
		// that discovery was *attempted*. Without this, a chronically
		// failing provider shows a stale "Last discovered" timestamp
		// that makes the scheduled timer appear broken.
		touchLastDiscovered(ctx, deps.pool, p)
		return false, false
	}

	// Enrich models with data from models.dev.
	if cache := provider.GetModelsDevCache(); cache != nil {
		if enriched := cache.EnrichModels(models, provider.TypeOf(p)); enriched > 0 {
			debuglog.Info("discovery: enriched models from models.dev", "enriched", enriched, "total", len(models), "provider", p.Name)
		}
	}
	// Runs unconditionally: modality arrays and the derived endpoint
	// class must be consistent even when models.dev is unreachable.
	provider.NormalizeModels(models)
	result.ModelsDiscovered += len(models)

	// Snapshot pre-scan state so background metadata/membership changes
	// can be recorded for the Models nav badge. A snapshot failure only
	// disables the diff for this provider; the scan itself proceeds.
	snapshot, snapErr := api.SnapshotProviderModels(ctx, deps.modelRepo, p.ID)
	if snapErr != nil {
		debuglog.Debug("discovery: failed to snapshot models", "provider", p.Name, "error", snapErr)
	}
	api.DampenOpenRouterPriceJitter(provider.TypeOf(p), snapshot, models)

	existingModelIDs := make([]string, 0, len(models))
	upsertedModels := make([]*model.Model, 0, len(models))
	upsertFailed := false
	for _, m := range models {
		if err := deps.modelRepo.Upsert(ctx, m); err != nil {
			debuglog.Error("discovery: failed to upsert model", "model_id", m.ModelID, "error", err)
			upsertFailed = true
		} else {
			existingModelIDs = append(existingModelIDs, m.ModelID)
			upsertedModels = append(upsertedModels, m)
		}
	}
	// Miss recording needs a trustworthy membership picture: skip it
	// when the snapshot is unavailable (absentees cannot be confirmed)
	// or any upsert failed (a DB error must not count a listed model
	// as missing). Absent models get a second opinion via confirmation
	// probes, and a model is disabled only after
	// model.MissingScanThreshold consecutive confirmed-missing scans.
	var disabledRefs []model.DisabledModelRef
	if snapErr == nil && !upsertFailed {
		disabledRefs = recordMissingModels(ctx, deps, discoverySvc, p, existingModelIDs, snapshot)
	}
	if len(disabledRefs) > 0 {
		result.ModelsDisabled += len(disabledRefs)
		events.Publish(events.Event{
			Type:     "discovery.models_disabled",
			Severity: "warning",
			Message:  fmt.Sprintf("%s no longer available at '%s' and %s disabled", util.Count(len(disabledRefs), "model", "models"), p.Name, util.Plural(len(disabledRefs), "was", "were")),
			Metadata: map[string]any{"provider": p.Name, "count": len(disabledRefs)},
		})
	}

	// Record this provider's model-level diff (added/reenabled/disabled/
	// metadata-updated) for later review. Failover group churn is folded
	// in once after the global failover sync below.
	if snapErr == nil {
		diff := api.BuildDiscoveryDiff(snapshot, upsertedModels, disabledRefs)
		if wrote, err := api.AppendDiscoveryChange(ctx, deps.pool, source, &p.ID, p.Name, diff); err != nil {
			debuglog.Error("discovery: failed to record changes", "provider", p.Name, "error", err)
		} else if wrote {
			changed = wrote
		}
	}

	touchLastDiscovered(ctx, deps.pool, p)
	debuglog.Info("discovery: discovered models", "count", len(models), "provider", p.Name)
	return changed, true
}

// modelPruneWarned remembers the last model_prune_days value the pass warned
// about, so a bad value logs once rather than every interval.
var modelPruneWarned struct {
	sync.Mutex
	value string
}

// pruneRetiredModels deletes rows discovery retired more than model_prune_days
// ago for the providers that scanned successfully this pass, then resyncs the
// failover groups those rows belonged to. Returns how many rows went. Off
// (0) and unreadable values skip; unreadable values warn once per distinct
// value.
func pruneRetiredModels(ctx context.Context, deps discoveryDeps, scannedOK []uuid.UUID, now time.Time) int {
	if len(scannedOK) == 0 {
		return 0
	}
	raw := deps.settingsRepo.GetWithDefault(ctx, "model_prune_days", strconv.Itoa(defaultModelPruneDays))
	days, err := strconv.Atoi(raw)
	if err != nil || days < 0 || days > maxModelPruneDays {
		modelPruneWarned.Lock()
		unseen := modelPruneWarned.value != raw
		modelPruneWarned.value = raw
		modelPruneWarned.Unlock()
		if unseen {
			debuglog.Warn("discovery: model_prune_days value not understood, skipping prune", "value", raw)
		}
		return 0
	}
	if days == 0 {
		return 0
	}

	flapped, err := api.FlappedModels(ctx, deps.pool, now.Add(-api.ClaimWindow))
	if err != nil {
		debuglog.Error("discovery: prune skipped, flap history unavailable", "error", err)
		return 0
	}
	horizon := now.Add(-time.Duration(days) * 24 * time.Hour)
	pruned, err := deps.modelRepo.PruneRetired(ctx, horizon, scannedOK, flapped, modelPruneBatch)
	if err != nil {
		debuglog.Error("discovery: prune retired models failed", "error", err)
		return 0
	}
	if len(pruned) == 0 {
		return 0
	}

	seen := map[string]bool{}
	var modelIDs []string
	ids := make([]uuid.UUID, 0, len(pruned))
	for _, p := range pruned {
		ids = append(ids, p.ID)
		if !seen[p.ModelID] {
			seen[p.ModelID] = true
			modelIDs = append(modelIDs, p.ModelID)
		}
		debuglog.Debug("discovery: pruned retired model", "provider", p.ProviderName, "model_id", p.ModelID)
	}
	api.ResyncFailoverAfterModelDelete(ctx, deps.failoverRepo, modelIDs, ids)

	debuglog.Info("discovery: pruned retired models", "count", len(pruned), "horizon_days", days)
	if len(pruned) == modelPruneBatch {
		debuglog.Warn("discovery: prune hit the per-pass cap, the rest goes next pass", "cap", modelPruneBatch)
	}
	return len(pruned)
}

// recordMissingModels confirms which of the snapshot's models this scan no
// longer sees (via confirmation probes) and records the misses; a model is
// disabled only after model.MissingScanThreshold consecutive confirmed-missing
// scans. Returns the refs that crossed the threshold and were disabled.
func recordMissingModels(ctx context.Context, deps discoveryDeps, discoverySvc *provider.DiscoveryService, p *provider.Provider, existingModelIDs []string, snapshot map[string]api.ModelSnapshot) []model.DisabledModelRef {
	confirmedIDs, suspect := api.ConfirmMissingModels(ctx, discoverySvc, p, deps.cfg.MasterKey, existingModelIDs, snapshot, api.NewSuspectStreak(deps.pool))
	if suspect {
		debuglog.Warn("discovery: suspect scan, skipping missing-model recording", "provider", p.Name)
		return nil
	}
	disabledRefs, pendingRefs, err := deps.modelRepo.RecordMissingModels(ctx, p.ID, p.Name, confirmedIDs)
	if err != nil {
		debuglog.Error("discovery: failed to record missing models", "provider", p.Name, "error", err)
	}
	if len(pendingRefs) > 0 {
		debuglog.Info("discovery: models confirmed missing but below disable threshold",
			"provider", p.Name, "pending", len(pendingRefs), "threshold", model.MissingScanThreshold)
	}
	return disabledRefs
}

// touchLastDiscovered stamps the provider's last_discovered_at, on success and
// failure alike, so the UI reflects that discovery was attempted.
func touchLastDiscovered(ctx context.Context, pool *pgxpool.Pool, p *provider.Provider) {
	now := time.Now()
	if _, err := pool.Exec(ctx, `UPDATE providers SET last_discovered_at = $1 WHERE id = $2`, now, p.ID); err != nil {
		debuglog.Error("discovery: failed to update last_discovered_at", "provider", p.Name, "error", err)
	}
}

// syncFailoverAfterDiscovery rebuilds auto failover groups for every model
// visible on an enabled provider, revalidates custom groups, and records the
// run-wide failover change entry. Reports whether a change row was recorded.
func syncFailoverAfterDiscovery(ctx context.Context, deps discoveryDeps, source string, providers []*provider.Provider, result *DiscoveryResult) (changed bool) {
	seenModelIDs := make(map[string]bool)
	for _, p := range providers {
		if !p.Enabled {
			continue
		}
		models, _ := deps.modelRepo.List(ctx, &p.ID)
		for _, m := range models {
			seenModelIDs[m.ModelID] = true
		}
	}
	failoverDiff := &api.DiscoveryDiff{}
	for modelID := range seenModelIDs {
		syncRes, err := deps.failoverRepo.SyncForModel(ctx, modelID)
		if err != nil {
			debuglog.Error("discovery: failed to sync failover", "model_id", modelID, "error", err)
			result.FailoverSyncErrs++
			events.Publish(events.Event{
				Type:     "failover.sync_error",
				Severity: "warning",
				Message:  fmt.Sprintf("Failover sync failed for model '%s'", modelID),
				Metadata: map[string]any{"error": err.Error(), "model_id": modelID},
			})
			continue
		}
		if syncRes != nil {
			failoverDiff.FailoverDeletedGroups = append(failoverDiff.FailoverDeletedGroups, syncRes.DeletedGroups...)
			failoverDiff.FailoverUpdatedGroups = append(failoverDiff.FailoverUpdatedGroups, syncRes.UpdatedGroups...)
		}
	}
	// SyncForModel only rebuilds auto-groups; custom groups whose member was
	// just disabled (not deleted) keep their stale size. Revalidate once per
	// cycle so background discovery auto-disables any custom group left with
	// fewer than two routable members — the headless path the manual Sync and
	// interactive discover already cover.
	if revRes, err := deps.failoverRepo.RevalidateCustomGroups(ctx); err != nil {
		debuglog.Error("discovery: failed to revalidate custom failover groups", "error", err)
	} else if revRes != nil {
		failoverDiff.FailoverDisabledGroups = append(failoverDiff.FailoverDisabledGroups, revRes.DisabledGroups...)
	}
	// Record failover group churn as one aggregate entry (the global sync is
	// not per-provider). An empty provider_name flags this to the frontend as
	// the run-wide failover entry, which it labels accordingly.
	if wrote, err := api.AppendDiscoveryChange(ctx, deps.pool, source, nil, "", failoverDiff); err != nil {
		debuglog.Error("discovery: failed to record failover changes", "error", err)
	} else if wrote {
		changed = wrote
	}
	return changed
}

// maybeStartupDiscovery launches the initial discovery run in the background,
// unless discovery_on_startup is off or any provider was already discovered
// within the last 5 minutes (a restart loop must not hammer providers).
func maybeStartupDiscovery(deps discoveryDeps, settingsRepo *settings.Repository) {
	if !settingsRepo.GetBool(context.Background(), "discovery_on_startup", true) {
		return
	}
	recentlyDiscovered := false
	providers, err := deps.providerRepo.List(context.Background())
	if err == nil {
		for _, p := range providers {
			if p.LastDiscoveredAt != nil && time.Since(*p.LastDiscoveredAt) < 5*time.Minute {
				recentlyDiscovered = true
				break
			}
		}
	}
	if recentlyDiscovered {
		debuglog.Info("discovery: skipping startup — last discovery within 5 minutes")
		return
	}
	go func() {
		result := runDiscovery(deps, "startup")
		publishDiscoveryEvent("Startup", result)
	}()
}
