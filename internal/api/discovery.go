package api

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
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

// priceRelTolerance bounds how far a freshly discovered price may drift from the
// stored value before discovery treats it as a real change. OpenRouter reports
// per-model pricing for whichever upstream it currently fronts, and that selection
// wiggles between scans; without a band, sub-percent rounding wiggles would
// overwrite the stored price and surface as metadata churn. Genuine repricings and
// real upstream switches move far more than this and pass through untouched.
const priceRelTolerance = 0.07

// DampenOpenRouterPriceJitter neutralizes sub-tolerance price wiggles from
// OpenRouter's volatile per-upstream pricing. For OpenRouter providers only, it
// clears the live-meta flag on any price field whose freshly discovered value sits
// within priceRelTolerance of the stored (pre-scan) value, demoting that field to
// fill-only: Upsert then keeps the stored price and diffModelFields reports no
// change. Large, genuine price moves exceed the band and stay live. No-op for
// every other provider type, for models with no snapshot, and for nil endpoints.
//
// Call it after snapshotting and before upserting, at every discovery path.
func DampenOpenRouterPriceJitter(baseURL string, snapshot map[string]ModelSnapshot, models []*model.Model) {
	if provider.DetectProviderType(baseURL) != "openrouter" {
		return
	}
	for _, m := range models {
		prev, ok := snapshot[m.ModelID]
		if !ok {
			continue
		}
		if m.LiveMeta.InputPrice && withinPriceTolerance(prev.inputPrice, m.InputPricePerMillion) {
			m.LiveMeta.InputPrice = false
			logPriceDamped(m.ModelID, "input_price", prev.inputPrice, m.InputPricePerMillion)
		}
		if m.LiveMeta.OutputPrice && withinPriceTolerance(prev.outputPrice, m.OutputPricePerMillion) {
			m.LiveMeta.OutputPrice = false
			logPriceDamped(m.ModelID, "output_price", prev.outputPrice, m.OutputPricePerMillion)
		}
		if m.LiveMeta.InputPriceCache && withinPriceTolerance(prev.inputPriceCache, m.InputPricePerMillionCacheHit) {
			m.LiveMeta.InputPriceCache = false
			logPriceDamped(m.ModelID, "input_price_cache", prev.inputPriceCache, m.InputPricePerMillionCacheHit)
		}
	}
}

// logPriceDamped records (at debug level, so it is silent unless DEBUG_LOG is on)
// that a sub-tolerance OpenRouter price wiggle was kept fill-only, so an operator
// can see why a freshly discovered price did not overwrite the stored one.
func logPriceDamped(modelID, field string, stored, fresh *float64) {
	debuglog.Debug("discovery: openrouter price within tolerance, kept stored value",
		"model_id", modelID, "field", field,
		"stored", floatPtrVal(stored), "discovered", floatPtrVal(fresh),
		"tolerance", priceRelTolerance)
}

// floatPtrVal dereferences a price pointer for logging, reporting nil as -1 (no
// real price is negative, so it is unambiguous as a sentinel).
func floatPtrVal(p *float64) float64 {
	if p == nil {
		return -1
	}
	return *p
}

// withinPriceTolerance reports whether newVal sits within priceRelTolerance of
// oldVal. Both must be set: a nil on either side is a fill or clear, a genuine
// change the caller must keep.
func withinPriceTolerance(oldVal, newVal *float64) bool {
	if oldVal == nil || newVal == nil {
		return false
	}
	o, n := *oldVal, *newVal
	denom := math.Max(math.Abs(o), math.Abs(n))
	if denom == 0 {
		return true
	}
	return math.Abs(o-n)/denom <= priceRelTolerance
}

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
	Added                  []ModelChange                `json:"added,omitempty"`
	Reenabled              []ModelChange                `json:"reenabled,omitempty"`
	Disabled               []ModelChange                `json:"disabled,omitempty"`
	Updated                []ModelUpdate                `json:"updated,omitempty"`
	FailoverDeletedGroups  []failover.DeletedGroupInfo  `json:"failover_deleted_groups,omitempty"`
	FailoverUpdatedGroups  []failover.UpdatedGroupInfo  `json:"failover_updated_groups,omitempty"`
	FailoverDisabledGroups []failover.DisabledGroupInfo `json:"failover_disabled_groups,omitempty"`
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

// Confirmation-probe schedule for ConfirmMissingModels: a model absent from
// the initial listing is only counted missing after these many extra listings,
// each preceded by the given delay (plus up to confirmProbeJitter of random
// jitter so fleet members and parallel providers don't probe in lockstep).
// Injectable so tests can zero the delays.
var confirmProbeDelays = []time.Duration{15 * time.Second, 45 * time.Second}

const confirmProbeJitter = 5 * time.Second

// suspectMissingFloor and suspectMissingRatio form the mass-vanish guard: a
// scan whose confirmed-missing set exceeds the floor AND the ratio of the
// provider's enabled models is treated as a broken listing, not a real
// removal, and records no misses. False-disabling dozens of models (which HA
// then propagates fleet-wide) is far worse than keeping a stale model enabled
// until an operator looks at the warning event.
const (
	suspectMissingFloor = 5
	suspectMissingRatio = 0.5
)

// suspectStreakThreshold is how many consecutive mass-vanish scans a provider
// must trip before discovery escalates from the quiet per-scan warning to a
// distinct, actionable "this looks like a real bulk removal" alert. Kept well
// above MissingScanThreshold so a brief upstream outage never reaches it: a
// transient broken listing recovers within a scan or two and resets the streak,
// whereas a genuine catalog sunset stays missing every scan. We never
// auto-disable on this signal (disabling half a provider's catalog propagates
// fleet-wide through HA); the alert asks an operator to disable by hand.
const suspectStreakThreshold = 3

// shouldEscalateSuspect reports whether a just-incremented consecutive
// mass-vanish count should raise the escalation alert. It fires on the crossing
// scan and then re-fires once every suspectStreakThreshold scans, so a
// persistent condition re-pings periodically without alerting on every scan.
func shouldEscalateSuspect(streak int) bool {
	return streak >= suspectStreakThreshold && streak%suspectStreakThreshold == 0
}

// SuspectStreak persists the per-provider consecutive-mass-vanish counter. It is
// nil on paths that must not touch the counter (unit tests, and any future
// caller without a pool); ConfirmMissingModels guards every use.
type SuspectStreak struct {
	exec func(pool *pgxpool.Pool, ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	pool *pgxpool.Pool
}

// NewSuspectStreak builds a pool-backed SuspectStreak for the discovery sweep.
func NewSuspectStreak(pool *pgxpool.Pool) *SuspectStreak {
	return &SuspectStreak{exec: dbExec, pool: pool}
}

// bump increments the provider's consecutive mass-vanish counter and, every
// suspectStreakThreshold consecutive scans, emits the louder escalation event.
// Counter errors are logged, not fatal: a missed escalation must never abort a
// scan.
func (s *SuspectStreak) bump(ctx context.Context, prov *provider.Provider, missing, enabled int) {
	if s == nil {
		return
	}
	var streak int
	if err := s.pool.QueryRow(ctx,
		`UPDATE providers SET suspect_scans = suspect_scans + 1 WHERE id = $1 RETURNING suspect_scans`,
		prov.ID,
	).Scan(&streak); err != nil {
		debuglog.Warn("discovery: failed to bump provider suspect streak", "provider", prov.Name, "error", err)
		return
	}
	if shouldEscalateSuspect(streak) {
		debuglog.Warn("discovery: provider suspected of bulk-removing models",
			"provider", prov.Name, "provider_id", prov.ID, "missing", missing, "enabled", enabled, "consecutive_scans", streak)
		events.Publish(events.Event{
			Type:     "discovery.bulk_removal_suspected",
			Severity: "error",
			Source:   "discovery",
			Message:  fmt.Sprintf("Provider '%s' has been missing %d of %d enabled models for %d consecutive scans. This looks like a real bulk removal rather than a broken listing; discovery will not auto-disable them. Review and disable the retired models by hand.", prov.Name, missing, enabled, streak),
			Metadata: map[string]interface{}{"provider": prov.Name, "provider_id": prov.ID, "missing": missing, "enabled": enabled, "consecutive_scans": streak},
		})
	}
}

// reset clears the counter after a healthy scan (listing recovered). It is a
// no-op write when the counter is already zero, so the common healthy scan does
// not churn the row.
func (s *SuspectStreak) reset(ctx context.Context, prov *provider.Provider) {
	if s == nil {
		return
	}
	if _, err := s.exec(s.pool, ctx,
		`UPDATE providers SET suspect_scans = 0 WHERE id = $1 AND suspect_scans <> 0`, prov.ID); err != nil {
		debuglog.Warn("discovery: failed to reset provider suspect streak", "provider", prov.Name, "error", err)
	}
}

// confirmProbeSleep waits for the probe backoff, honouring ctx cancellation.
// Injectable for tests (which also exercise sleepWithContext directly).
var confirmProbeSleep = sleepWithContext

func sleepWithContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}

// ConfirmMissingModels gives absent models a second opinion before any miss is
// recorded. presentIDs is the initial listing's membership; every snapshot
// model that is enabled but unlisted triggers up to len(confirmProbeDelays)
// fresh listings with backoff, and the union of all probes' model IDs is
// returned as the confirmed membership. suspect=true means this scan's
// membership cannot be trusted (a confirmation probe failed, ctx was
// cancelled, or the mass-vanish guard tripped) and the caller must skip miss
// recording entirely. Probes affect membership only; metadata still comes
// exclusively from the initial listing's upserts.
// streak, when non-nil, tracks consecutive mass-vanish scans per provider: it is
// reset on any healthy scan and bumped (with escalation) when the mass-vanish
// guard trips, so a genuine bulk removal eventually raises a loud alert while a
// one-off broken listing does not. It is nil on paths that must not touch the
// counter (unit tests).
func ConfirmMissingModels(ctx context.Context, svc *provider.DiscoveryService, prov *provider.Provider, masterKey string, presentIDs []string, snapshot map[string]ModelSnapshot, streak *SuspectStreak) (confirmedPresent []string, suspect bool) {
	present := make(map[string]bool, len(presentIDs))
	for _, id := range presentIDs {
		present[id] = true
	}
	countMissing := func() int {
		n := 0
		for id, snap := range snapshot {
			if snap.enabled && !present[id] {
				n++
			}
		}
		return n
	}

	confirmedPresent = presentIDs
	missing := countMissing()
	if missing == 0 {
		streak.reset(ctx, prov)
		return confirmedPresent, false
	}

	for probe, delay := range confirmProbeDelays {
		//nolint:gosec // jitter, not crypto
		wait := delay + time.Duration(rand.Int64N(int64(confirmProbeJitter)))
		debuglog.Info("discovery: models absent from listing, running confirmation probe",
			"provider", prov.Name, "provider_id", prov.ID, "missing", missing,
			"probe", probe+1, "max_probes", len(confirmProbeDelays), "delay", wait)
		if err := confirmProbeSleep(ctx, wait); err != nil {
			return confirmedPresent, true
		}
		models, err := discoverModelsForConfirm(ctx, svc, prov, masterKey)
		if err != nil {
			// The provider answered the initial listing but not the probe: the
			// upstream is flapping, so this scan's membership proves nothing.
			debuglog.Warn("discovery: confirmation probe failed, treating scan as suspect",
				"provider", prov.Name, "provider_id", prov.ID, "probe", probe+1, "error", err)
			return confirmedPresent, true
		}
		for _, m := range models {
			if !present[m.ModelID] {
				present[m.ModelID] = true
				confirmedPresent = append(confirmedPresent, m.ModelID)
			}
		}
		missing = countMissing()
		if missing == 0 {
			debuglog.Info("discovery: confirmation probe found all absent models, no misses recorded",
				"provider", prov.Name, "provider_id", prov.ID, "probe", probe+1)
			streak.reset(ctx, prov)
			return confirmedPresent, false
		}
	}

	enabledCount := 0
	for _, snap := range snapshot {
		if snap.enabled {
			enabledCount++
		}
	}
	if missing > suspectMissingFloor && float64(missing) > suspectMissingRatio*float64(enabledCount) {
		debuglog.Warn("discovery: mass-vanish guard tripped, treating scan as suspect",
			"provider", prov.Name, "provider_id", prov.ID, "missing", missing, "enabled", enabledCount)
		events.Publish(events.Event{
			Type:     "discovery.suspect_scan",
			Severity: "warning",
			Source:   "discovery",
			Message:  fmt.Sprintf("Discovery scan for %s is missing %d of %d enabled models even after confirmation probes; treating the listing as broken and disabling nothing", prov.Name, missing, enabledCount),
			Metadata: map[string]interface{}{"provider": prov.Name, "provider_id": prov.ID, "missing": missing, "enabled": enabledCount},
		})
		// A recovered listing resets the streak; a persistent one crosses the
		// escalation threshold and raises the louder bulk-removal alert.
		streak.bump(ctx, prov, missing, enabledCount)
		return confirmedPresent, true
	}

	streak.reset(ctx, prov)
	return confirmedPresent, false
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
	d.FailoverDisabledGroups = append(d.FailoverDisabledGroups, res.DisabledGroups...)
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

	// SyncForModel only rebuilds auto-groups; a custom group whose member was
	// just disabled (not deleted) keeps its stale size. Revalidate custom groups
	// so any that dropped below two routable members get auto-disabled and
	// reported. Only worth doing when this scan actually disabled a model: new or
	// reappeared models never shrink a group, so we skip the extra List+query
	// (and avoid re-running it for every provider in a discover-all sweep).
	// Best-effort: a failure here must not abort the scan.
	if len(disabledRefs) > 0 {
		if revRes, err := failoverRepoRevalidateCustomGroups(repo, ctx); err != nil {
			debuglog.Error("discovery: custom-group revalidation failed", "error", err)
		} else {
			diff.mergeSyncResult(revRes)
		}
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
