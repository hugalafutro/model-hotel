package api

import (
	"context"
	"math"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/failover"
	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/provider"
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
