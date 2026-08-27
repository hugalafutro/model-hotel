package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/events"
	"github.com/hugalafutro/model-hotel/internal/quota"
)

// The quota schema-drift watch alerts when a provider changes the *shape* of
// its quota response: Ollama Cloud moving from subscription periods to usage
// credits, NeuralWatt adding overage-credit billing, any provider reshaping its
// payload. It exists because a normalizer written against an old shape fails
// silently: MiniMax's count fields read 0 on a live plan, so the exhaustion
// check could never fire, and nothing in the system would ever have said so.
//
// It watches the payload's key-path set rather than running per-provider
// parsers, because only three provider types have a parser and the two the
// operator actually asked about (ollama-cloud, neuralwatt) have none.
//
// It is deliberately alert-only. It never feeds the quota advisor, never
// changes a circuit-breaker cooldown, and is not reachable from any request
// path: the only caller is RefreshQuotaAdvice, which runs on the quota poll
// goroutine. Its worst failure mode is a spurious alert, and it must stay that
// way.

// quotaSchemaKeyPrefix namespaces the per-provider baseline in the settings
// K/V. Written with settings.Set, never SetTx: SetTx enforces the operator
// allowlist, which internal `_`-prefixed keys are deliberately not on (the
// _fleet_* keys established the precedent). The key is also absent from
// syncableSettingKeys, so a baseline never travels between fleet members --
// each member watches the shapes it has actually seen.
//
// One key per provider, not per (provider, kind): quotaKindFor maps a provider
// type to exactly one kind, so a provider has one snapshot row. If an operator
// retypes a provider by editing its base URL, the old kind's row lingers until
// it goes stale and the two shapes alternate under this key -- which the
// consecutive-sighting debounce absorbs without alerting, rather than flapping.
const quotaSchemaKeyPrefix = "_quota_schema_"

// quotaDriftConfirmations is how many *consecutive* polls must report the same
// new shape before it is alerted on. One malformed or truncated body reaching
// the snapshot table must not page anyone; two in a row is a real change.
const quotaDriftConfirmations = 2

// quotaDriftListCap bounds the added/removed key-path lists carried on the
// event, so a wholesale reshape cannot produce an enormous alert payload.
const quotaDriftListCap = 10

// quotaSchemaCandidate is the in-memory debounce state for one provider: a new
// shape that has been seen but not yet confirmed. Held on the Handler rather
// than persisted, because a restart merely re-arms the debounce -- the harmless
// direction, costing one extra poll before a real change is reported.
//
// fetchedAt identifies *which fetch* the count last advanced on. The watch runs
// per refresh pass, not per fetch, and the two are not the same thing: a
// fleet-fed member keeps one row for a whole interval while PollQuotasOnce skips
// its upstream call, and driftEligible accepts a row for three intervals. Without
// this, one malformed-but-parseable body would be re-read on the next pass and
// confirm itself, which is precisely what the debounce exists to stop.
type quotaSchemaCandidate struct {
	fingerprint string
	seen        int
	fetchedAt   time.Time
}

// quotaSchemaSettingKey is the settings key holding a provider's baseline
// key-path list.
func quotaSchemaSettingKey(providerID uuid.UUID) string {
	return quotaSchemaKeyPrefix + providerID.String()
}

// driftEligible reports whether a snapshot carries a payload worth comparing.
//
// Only a 200 does. A 204 is NeuralWatt's free tier, a 424 is a dead credential,
// and a 0 is the RecordFailure placeholder: all billing-independent failures
// whose bodies say nothing about the provider's quota schema, and all of which
// would otherwise read as a wholesale reshape the moment they appeared.
//
// Staleness uses the same bound RefreshQuotaAdvice trusts for advice, including
// its rule that a non-positive maxAge (quota polling disabled) makes nothing
// eligible: without a live poll cadence there is no way to tell a current shape
// from one recorded before a plan change.
func driftEligible(s quota.Snapshot, maxAge time.Duration, now time.Time) bool {
	if maxAge <= 0 || s.HTTPStatus != http.StatusOK || len(s.Payload) == 0 {
		return false
	}
	// Negative age (a future stamp) is untrustworthy, not eligible: see
	// snapshotWithinAge. This is the same field and the same repository feed as
	// the two staleness checks in quota_snapshot.go.
	return snapshotWithinAge(now, s.FetchedAt, maxAge)
}

// driftDecision is the whole per-provider decision, kept pure so the debounce
// and the alert-once rule are testable without a database or an event bus.
//
// prev is the fingerprint of the stored baseline ("" when the provider has
// never been seen), current the fingerprint of this poll's payload ("" when it
// had no readable shape), fetchedAt the snapshot's fetch time (the identity of
// the observation), and cand the candidate carried over from the previous pass.
// It returns the candidate to carry into the next pass, whether to alert, and
// whether to store current as the new baseline.
//
// The counter advances inside the decision rather than beside it: a caller that
// incremented separately could disagree with the branch that reads it, which is
// exactly the kind of split state that produces either a missed alert or one
// per poll forever.
func driftDecision(prev, current string, fetchedAt time.Time, cand quotaSchemaCandidate) (quotaSchemaCandidate, bool, bool) {
	switch {
	case current == "":
		// Nothing readable this poll. Not evidence in either direction, so an
		// armed candidate keeps its count rather than being disarmed by a
		// single unreadable body.
		return cand, false, false
	case prev == "":
		// First sighting: adopt silently. A fresh install must not alert for
		// every provider it polls.
		return quotaSchemaCandidate{}, false, true
	case current == prev:
		// Steady state: nothing to report, and no reason to rewrite the row.
		return quotaSchemaCandidate{}, false, false
	}

	if cand.fingerprint == current {
		if cand.fetchedAt.Equal(fetchedAt) {
			// The same stored row, read again by a later refresh pass. One row
			// is one sighting no matter how often it is re-read.
			//
			// Both timestamps round-trip through Postgres, so neither carries a
			// monotonic reading and Equal is a plain wall-clock comparison. Keep
			// it that way: swapping in Unix() (or any truncating comparison)
			// would collapse two genuinely distinct fetches in the same second
			// into one sighting and stall drift confirmation.
			return cand, false, false
		}
		cand.seen++
		cand.fetchedAt = fetchedAt
	} else {
		cand = quotaSchemaCandidate{fingerprint: current, seen: 1, fetchedAt: fetchedAt}
	}
	if cand.seen >= quotaDriftConfirmations {
		// Confirmed. Promoting the new shape to the baseline is what makes this
		// one alert per shape rather than one per poll.
		return quotaSchemaCandidate{}, true, true
	}
	return cand, false, false
}

// stepQuotaSchemaCandidate applies driftDecision against the handler's stored
// candidate for one provider, under a lock. RefreshQuotaAdvice runs on the poll
// goroutine today, but the map is process-wide state and a second caller must
// not be able to corrupt it.
func (h *Handler) stepQuotaSchemaCandidate(providerID uuid.UUID, prev, current string, fetchedAt time.Time) (alert, store bool) {
	h.quotaSchemaMu.Lock()
	defer h.quotaSchemaMu.Unlock()
	if h.quotaSchemaSeen == nil {
		h.quotaSchemaSeen = make(map[uuid.UUID]quotaSchemaCandidate)
	}
	next, alert, store := driftDecision(prev, current, fetchedAt, h.quotaSchemaSeen[providerID])
	if next == (quotaSchemaCandidate{}) {
		delete(h.quotaSchemaSeen, providerID)
	} else {
		h.quotaSchemaSeen[providerID] = next
	}
	return alert, store
}

// forgetQuotaSchema drops everything the drift watch remembers about a provider:
// the persisted key-path baseline in the settings K/V and the in-memory debounce
// candidate. Nothing else removed either, so a row accumulated for every
// provider an operator ever deleted and was never read again.
//
// Best effort by design, and called only after the provider row is already gone:
// a leftover baseline is housekeeping, never correctness, so a failure here must
// not turn a successful delete into a 500. A provider deleted before it was ever
// polled has no baseline at all, which DeleteKey reports as success.
func (h *Handler) forgetQuotaSchema(ctx context.Context, providerID uuid.UUID) {
	h.quotaSchemaMu.Lock()
	delete(h.quotaSchemaSeen, providerID)
	h.quotaSchemaMu.Unlock()

	if h.settingsRepo == nil {
		return
	}
	if err := h.settingsRepo.DeleteKey(ctx, quotaSchemaSettingKey(providerID)); err != nil {
		debuglog.Warn("quota: failed to remove the schema baseline of a deleted provider", "provider_id", providerID, "error", err)
	}
}

// diffSchemaPaths returns the key paths that appeared and disappeared between
// two shapes, each list capped at quotaDriftListCap. This is the operator's
// actual diagnosis: removed ["subscription_period_end"] alongside added
// ["credits.balance"] names the billing change immediately.
//
// Only paths travel, never values: quota figures are provider account data, and
// this repo never puts payload content in a log or an event.
func diffSchemaPaths(prev, current []string) (added, removed []string) {
	prevSet := make(map[string]struct{}, len(prev))
	for _, p := range prev {
		prevSet[p] = struct{}{}
	}
	currentSet := make(map[string]struct{}, len(current))
	for _, p := range current {
		currentSet[p] = struct{}{}
	}
	// Both inputs are quota.SchemaPaths output (sorted), and the stored baseline
	// is re-sorted on load, so truncation keeps the alphabetically first paths
	// rather than an arbitrary subset.
	for _, p := range current {
		if _, ok := prevSet[p]; !ok && len(added) < quotaDriftListCap {
			added = append(added, p)
		}
	}
	for _, p := range prev {
		if _, ok := currentSet[p]; !ok && len(removed) < quotaDriftListCap {
			removed = append(removed, p)
		}
	}
	return added, removed
}

// loadSchemaBaseline reads a provider's persisted key-path baseline. It returns
// ok=false only for a real read failure, which the caller must treat as "cannot
// compare this pass" rather than as a first sighting: guessing would silently
// discard a good baseline. A missing or corrupt value yields (nil, true), which
// driftDecision adopts silently on this pass.
func (h *Handler) loadSchemaBaseline(ctx context.Context, providerID uuid.UUID) ([]string, bool) {
	raw, found, err := h.settingsRepo.GetChecked(ctx, quotaSchemaSettingKey(providerID))
	if err != nil {
		return nil, false
	}
	if !found || raw == "" {
		return nil, true
	}
	var paths []string
	if uerr := json.Unmarshal([]byte(raw), &paths); uerr != nil {
		// A value we wrote wrong is re-adopted, not alerted on.
		return nil, true
	}
	// The fingerprint is order-sensitive; sorting here means a hand-edited or
	// reordered row cannot masquerade as a schema change.
	sort.Strings(paths)
	return paths, true
}

// checkQuotaDrift compares each eligible snapshot's payload shape against the
// provider's stored baseline and alerts on a confirmed change. It reuses the
// snapshot list and provider maps RefreshQuotaAdvice already built, so it costs
// no extra query.
func (h *Handler) checkQuotaDrift(ctx context.Context, snaps []quota.Snapshot, typeByID, nameByID map[uuid.UUID]string, maxAge time.Duration) {
	now := time.Now()
	for _, s := range snaps {
		if !driftEligible(s, maxAge, now) {
			continue
		}
		name, known := nameByID[s.ProviderID]
		if !known {
			// The provider is gone; there is nothing to name in an alert.
			continue
		}

		// An unreadable payload yields an empty fingerprint, which
		// driftDecision treats as "no evidence" rather than as a new shape.
		current := ""
		paths, readable := quota.SchemaPaths(s.Payload)
		if readable {
			current = quota.FingerprintPaths(paths)
		}

		prevPaths, ok := h.loadSchemaBaseline(ctx, s.ProviderID)
		if !ok {
			debuglog.Warn("quota: schema baseline unreadable, skipping drift check", "provider", name)
			continue
		}
		prev := ""
		if len(prevPaths) > 0 {
			prev = quota.FingerprintPaths(prevPaths)
		}

		alert, store := h.stepQuotaSchemaCandidate(s.ProviderID, prev, current, s.FetchedAt)
		if alert {
			added, removed := diffSchemaPaths(prevPaths, paths)
			events.Publish(events.Event{
				Type:     "quota.schema_drift",
				Severity: "warning",
				Source:   "quota",
				Message:  fmt.Sprintf("Provider %s changed the shape of its quota response", name),
				Metadata: map[string]any{
					"provider_id":   s.ProviderID.String(),
					"provider":      name,
					"provider_type": typeByID[s.ProviderID],
					"kind":          s.Kind,
					"added":         added,
					"removed":       removed,
				},
			})
			debuglog.Warn("quota: provider changed its quota response shape",
				"provider", name, "added_paths", len(added), "removed_paths", len(removed))
		}
		if store {
			h.storeSchemaBaseline(ctx, s.ProviderID, name, paths)
		}
	}
}

// storeSchemaBaseline persists the current key-path set as the provider's
// baseline. A write failure is logged and dropped: the next poll re-arms the
// debounce and tries again, which is preferable to failing a poll pass over an
// advisory watch.
func (h *Handler) storeSchemaBaseline(ctx context.Context, providerID uuid.UUID, name string, paths []string) {
	// Unreachable in practice and deliberately kept: json.Marshal of a []string
	// has no failure mode (no unsupported types, no cycles, no custom marshaler,
	// and invalid UTF-8 is coerced to U+FFFD rather than rejected). No test can
	// induce it honestly, so it stays uncovered rather than being reached by a
	// contrived one — but dropping the check would be an unchecked error.
	b, err := json.Marshal(paths)
	if err != nil {
		debuglog.Warn("quota: encode schema baseline failed", "provider", name, "error", err)
		return
	}
	if serr := h.settingsRepo.Set(ctx, quotaSchemaSettingKey(providerID), string(b)); serr != nil {
		debuglog.Warn("quota: store schema baseline failed", "provider", name, "error", serr)
	}
}
