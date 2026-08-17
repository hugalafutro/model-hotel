package api

import (
	"context"
	"math"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/failover"
	"github.com/hugalafutro/model-hotel/internal/model"
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
// REPLACES any freshly discovered price sitting within priceRelTolerance of the
// stored (pre-scan) value with that stored value, so Upsert persists the same
// number (prices follow the incoming value on unpinned rows) and
// diffModelFields sees no change. Large, genuine price moves exceed the band
// and pass through untouched. No-op for every other provider type, for models
// with no snapshot, and for nil endpoints.
//
// Call it after snapshotting and before upserting, at every discovery path.
func DampenOpenRouterPriceJitter(providerType string, snapshot map[string]ModelSnapshot, models []*model.Model) {
	if providerType != "openrouter" {
		return
	}
	for _, m := range models {
		prev, ok := snapshot[m.ModelID]
		if !ok {
			continue
		}
		if withinPriceTolerance(prev.inputPrice, m.InputPricePerMillion) {
			logPriceDamped(m.ModelID, "input_price", prev.inputPrice, m.InputPricePerMillion)
			m.InputPricePerMillion = prev.inputPrice
		}
		if withinPriceTolerance(prev.outputPrice, m.OutputPricePerMillion) {
			logPriceDamped(m.ModelID, "output_price", prev.outputPrice, m.OutputPricePerMillion)
			m.OutputPricePerMillion = prev.outputPrice
		}
		if withinPriceTolerance(prev.inputPriceCache, m.InputPricePerMillionCacheHit) {
			logPriceDamped(m.ModelID, "input_price_cache", prev.inputPriceCache, m.InputPricePerMillionCacheHit)
			m.InputPricePerMillionCacheHit = prev.inputPriceCache
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

// ModelSnapshot captures a model's pre-scan state — whether it was routable and
// whether the operator pinned it, plus the pricing/context fields compared to
// detect metadata changes. Why it carries no disabled_manually or
// auto_retired_at: what the scan did about either is read off the row Upsert
// returned, not re-derived here. The type is exported so the scheduled discovery
// loop (package main) can hold the snapshot returned by SnapshotProviderModels
// and pass it to BuildDiscoveryDiff; its fields stay package-private.
type ModelSnapshot struct {
	enabled bool
	// pinned mirrors models.manually_enabled_at IS NOT NULL: the operator enabled
	// this model by hand, so the listing no longer governs it. Read by
	// ConfirmMissingModels, which keeps such rows out of its mass-vanish guard.
	pinned bool
	// priceCustomized mirrors models.price_customized: the operator pinned the
	// prices, so Upsert keeps the stored values and a value→value price move
	// must not be reported (it did not persist).
	priceCustomized bool
	inputPrice      *float64
	inputPriceCache *float64
	outputPrice     *float64
	contextLength   *int
}

// SnapshotProviderModels maps model_id to its pre-scan state for one provider.
// The pins come from their own query because Model carries no field for them.
func SnapshotProviderModels(ctx context.Context, repo *model.Repository, providerID uuid.UUID) (map[string]ModelSnapshot, error) {
	existing, err := repo.List(ctx, &providerID)
	if err != nil {
		return nil, err
	}
	pinned, err := repo.PinnedModelIDs(ctx, providerID)
	if err != nil {
		return nil, err
	}
	snap := make(map[string]ModelSnapshot, len(existing))
	for _, m := range existing {
		snap[m.ModelID] = ModelSnapshot{
			enabled:         m.Enabled,
			pinned:          pinned[m.ModelID],
			priceCustomized: m.PriceCustomized,
			inputPrice:      m.InputPricePerMillion,
			inputPriceCache: m.InputPricePerMillionCacheHit,
			outputPrice:     m.OutputPricePerMillion,
			contextLength:   m.ContextLength,
		}
	}
	return snap, nil
}

// BuildDiscoveryDiff classifies one provider scan against its before-snapshot:
// upserted models absent from the snapshot are new; a snapshot model the scan
// actually brought back counts as reappeared; an unchanged-membership model
// whose pricing/context fields moved is an update; disabledRefs are the models
// this scan just disabled.
//
// The re-enable is read off the row Upsert returned rather than re-derived from
// the conditions Upsert applies. Those conditions have grown — a model the
// operator disabled by hand stays off, and so does one the proxy retired from
// traffic (auto_retired_at, migration 063) — and a copy of them here drifted
// from the SQL and reported a revival that never happened. An auto-retired model
// is the case that made it permanent rather than occasional: it never left the
// listing, so it is sighted on every single scan, and every scan claimed to have
// re-enabled it while the write correctly declined to.
func BuildDiscoveryDiff(snapshot map[string]ModelSnapshot, upserted []*model.Model, disabledRefs []model.DisabledModelRef) *DiscoveryDiff {
	diff := &DiscoveryDiff{}
	for _, m := range upserted {
		prev, ok := snapshot[m.ModelID]
		switch {
		case !ok:
			diff.Added = append(diff.Added, ModelChange{ModelID: m.ModelID, Reason: changeReasonNewModel})
		case !m.Enabled:
			// Still off after the sighting, so the sighting changed nothing worth
			// reporting. Metadata-change detection is skipped for the same reason
			// it is skipped for any disabled model: a hidden model's price and
			// context churn must not raise the discovery-changes badge. (It is
			// still upserted, so the values stay current for whenever it comes
			// back.)
		case !prev.enabled:
			diff.Reenabled = append(diff.Reenabled, ModelChange{ModelID: m.ModelID, Reason: changeReasonReappeared})
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
// Prices persist by follow-the-source on unpinned rows, so any value→value
// price move the scan carries is real and gets reported — whichever source
// (live API, catalog, models.dev) supplied it. On a price-pinned row Upsert
// keeps the stored values, so value→value moves are suppressed as phantom.
// Context length still gates on live-provenance (m.LiveMeta): a non-live
// context value is fill-only at upsert, so only a live one can genuinely
// change the stored value.
func diffModelFields(prev ModelSnapshot, m *model.Model) []FieldChange {
	var changes []FieldChange
	if c, ok := diffFloatPtr(changeFieldInputPrice, prev.inputPrice, m.InputPricePerMillion, prev.priceCustomized); ok {
		changes = append(changes, c)
	}
	if c, ok := diffFloatPtr(changeFieldOutputPrice, prev.outputPrice, m.OutputPricePerMillion, prev.priceCustomized); ok {
		changes = append(changes, c)
	}
	if c, ok := diffFloatPtr(changeFieldInputPriceCache, prev.inputPriceCache, m.InputPricePerMillionCacheHit, prev.priceCustomized); ok {
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
// a previously-unset value (old nil → new set) is always a change (Upsert
// fills gaps even on pinned rows). For two non-nil values the change is
// reported unless the row's prices are operator-pinned — a pinned row's stored
// value survives the upsert, so the move never persisted. Comparison is at
// float32 precision because prices are stored in a REAL column — comparing a
// fresh float64 against the float32-rounded stored value would otherwise
// jitter in the 7th decimal. Real price changes are far larger than float32
// epsilon.
func diffFloatPtr(field string, oldVal, newVal *float64, pricePinned bool) (FieldChange, bool) {
	if newVal == nil {
		return FieldChange{}, false
	}
	if oldVal != nil {
		if pricePinned || float32(*oldVal) == float32(*newVal) {
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
