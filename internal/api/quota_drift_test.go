package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/events"
	"github.com/hugalafutro/model-hotel/internal/quota"
)

// ---------------------------------------------------------------------------
// The pure decision: baseline adoption, debounce, alert-once
// ---------------------------------------------------------------------------

// fetchN is a distinct snapshot fetch time, standing for the Nth upstream fetch.
// The debounce keys off the fetched_at it observed, so the pure tests must feed
// distinct values wherever they mean "a later poll fetched a fresh row".
func fetchN(n int) time.Time {
	return time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC).Add(time.Duration(n) * 5 * time.Minute)
}

// TestQuotaDriftDecisionFirstSightingAdoptsBaselineSilently verifies a fresh
// install (or a newly added provider) records what it saw without alerting.
// Every provider would otherwise page the operator the first time it is polled.
func TestQuotaDriftDecisionFirstSightingAdoptsBaselineSilently(t *testing.T) {
	next, alert, store := driftDecision("", "abc123", fetchN(1), quotaSchemaCandidate{})
	if alert {
		t.Error("the first sighting of a provider must never alert")
	}
	if !store {
		t.Error("the first sighting must be stored as the baseline")
	}
	if next != (quotaSchemaCandidate{}) {
		t.Errorf("adopting a baseline must leave no pending candidate, got %+v", next)
	}
}

// TestQuotaDriftDecisionUnchangedShapeNeitherAlertsNorRewrites verifies the
// steady state: the fingerprint matches the baseline on every poll, so there is
// nothing to report and no reason to write the settings row again.
func TestQuotaDriftDecisionUnchangedShapeNeitherAlertsNorRewrites(t *testing.T) {
	next, alert, store := driftDecision("abc123", "abc123", fetchN(1), quotaSchemaCandidate{})
	if alert || store {
		t.Errorf("an unchanged fingerprint must do nothing, got alert=%v store=%v", alert, store)
	}
	if next != (quotaSchemaCandidate{}) {
		t.Errorf("an unchanged fingerprint must leave no pending candidate, got %+v", next)
	}
}

// TestQuotaDriftDecisionChangedShapeNeedsTwoConsecutiveSightings is the
// debounce: one malformed or truncated error body reaching the snapshot table
// must not page anyone, so a new shape only counts once a *later fetch* repeats it.
func TestQuotaDriftDecisionChangedShapeNeedsTwoConsecutiveSightings(t *testing.T) {
	const baseline, changed = "old00000", "new00000"

	first, alert, store := driftDecision(baseline, changed, fetchN(1), quotaSchemaCandidate{})
	if alert || store {
		t.Fatalf("a single sighting of a new shape must not alert or store, got alert=%v store=%v", alert, store)
	}
	if first.fingerprint != changed || first.seen != 1 {
		t.Fatalf("the new shape must be armed as a candidate, got %+v", first)
	}
	if !first.fetchedAt.Equal(fetchN(1)) {
		t.Fatalf("the candidate must record which fetch it saw, got %+v", first)
	}

	second, alert, store := driftDecision(baseline, changed, fetchN(2), first)
	if !alert {
		t.Error("a second consecutive sighting of the same new shape must alert")
	}
	if !store {
		t.Error("alerting must promote the new shape to the baseline")
	}
	if second != (quotaSchemaCandidate{}) {
		t.Errorf("alerting must clear the candidate, got %+v", second)
	}
}

// TestQuotaDriftDecisionSameSnapshotObservedTwiceIsOneSighting is the fleet-dedup
// hazard: RefreshQuotaAdvice re-reads whatever rows are in the table on every
// pass, and driftEligible accepts a row for three refresh intervals while
// PollQuotasOnce skips its own fetch whenever a fleet-distributed row is younger
// than one interval. So the *same* row is routinely observed by two consecutive
// passes. Counting that as two sightings would let one malformed-but-parseable
// body confirm itself, which is exactly what the debounce exists to prevent.
func TestQuotaDriftDecisionSameSnapshotObservedTwiceIsOneSighting(t *testing.T) {
	const baseline, changed = "old00000", "new00000"
	sameFetch := fetchN(1)

	armed, _, _ := driftDecision(baseline, changed, sameFetch, quotaSchemaCandidate{})
	if armed.seen != 1 {
		t.Fatalf("first observation must arm at seen=1, got %+v", armed)
	}

	// The poll pass runs again before a new fetch lands: same row, same fetched_at.
	for pass := 2; pass <= 4; pass++ {
		next, alert, store := driftDecision(baseline, changed, sameFetch, armed)
		if alert || store {
			t.Fatalf("pass %d: re-reading one stored row must not confirm it, got alert=%v store=%v", pass, alert, store)
		}
		if next != armed {
			t.Fatalf("pass %d: re-reading one stored row must leave the candidate untouched, got %+v want %+v", pass, next, armed)
		}
		armed = next
	}

	// A genuinely new fetch of the same shape does confirm it.
	if _, alert, store := driftDecision(baseline, changed, fetchN(2), armed); !alert || !store {
		t.Errorf("a fresh fetch of the same new shape must confirm it, got alert=%v store=%v", alert, store)
	}
}

// TestQuotaDriftDecisionAlertsOncePerShapeNotOncePerPoll verifies the promoted
// baseline silences the shape that was just alerted on, and that a *further*
// change still gets its own single alert rather than being suppressed.
func TestQuotaDriftDecisionAlertsOncePerShapeNotOncePerPoll(t *testing.T) {
	const first, second, third = "shape001", "shape002", "shape003"

	// After alerting on `second`, it is the baseline: polling it again is quiet.
	if _, alert, store := driftDecision(second, second, fetchN(1), quotaSchemaCandidate{}); alert || store {
		t.Errorf("the shape just alerted on became the baseline and must go quiet, got alert=%v store=%v", alert, store)
	}

	// A third shape arms and then alerts on its own, independently.
	cand, alert, _ := driftDecision(second, third, fetchN(2), quotaSchemaCandidate{})
	if alert {
		t.Fatal("a further change must still be debounced, not alerted immediately")
	}
	if _, alert, store := driftDecision(second, third, fetchN(3), cand); !alert || !store {
		t.Errorf("a further confirmed change must alert on its own, got alert=%v store=%v", alert, store)
	}

	// And the original shape is not what the alert is about any more.
	if _, alert, _ := driftDecision(second, first, fetchN(4), quotaSchemaCandidate{}); alert {
		t.Error("reverting to an older shape must still be debounced like any change")
	}
}

// TestQuotaDriftDecisionFlappingShapeNeverConfirms verifies the debounce counts
// *consecutive* sightings: a provider alternating between two shapes never
// reaches two in a row, so it must stay silent rather than alert every poll.
func TestQuotaDriftDecisionFlappingShapeNeverConfirms(t *testing.T) {
	const baseline = "steady00"
	cand := quotaSchemaCandidate{}
	for i, fp := range []string{"flapA000", "flapB000", "flapA000", "flapB000", "flapA000"} {
		var alert, store bool
		cand, alert, store = driftDecision(baseline, fp, fetchN(i+1), cand)
		if alert || store {
			t.Fatalf("poll %d (%s): alternating shapes never confirm, got alert=%v store=%v", i, fp, alert, store)
		}
		if cand.fingerprint != fp || cand.seen != 1 {
			t.Fatalf("poll %d (%s): a different shape must re-arm the candidate at seen=1, got %+v", i, fp, cand)
		}
	}
}

// TestQuotaDriftDecisionUnreadablePayloadLeavesCandidateArmed verifies a poll
// whose payload has no fingerprint at all (empty digest) is a no-op in both
// directions: it must not alert, and it must not disarm a candidate that a
// previous poll armed, because "we could not read it" is not evidence either way.
func TestQuotaDriftDecisionUnreadablePayloadLeavesCandidateArmed(t *testing.T) {
	armed := quotaSchemaCandidate{fingerprint: "new00000", seen: 1, fetchedAt: fetchN(1)}

	next, alert, store := driftDecision("old00000", "", fetchN(2), armed)
	if alert || store {
		t.Errorf("an unreadable payload must not alert or store, got alert=%v store=%v", alert, store)
	}
	if next != armed {
		t.Errorf("an unreadable payload must leave the candidate untouched, got %+v want %+v", next, armed)
	}
}

// ---------------------------------------------------------------------------
// Snapshot eligibility
// ---------------------------------------------------------------------------

// TestQuotaDriftEligibleSkipsNon200AndStaleSnapshots verifies the watch only
// looks at snapshots that actually carry a billing payload. A 204 (NeuralWatt
// free tier), a 424 (dead credential) and a 0 (RecordFailure placeholder) are
// billing-independent failures, not schema drift, and a snapshot older than the
// window RefreshQuotaAdvice trusts cannot be compared against today's shape.
func TestQuotaDriftEligibleSkipsNon200AndStaleSnapshots(t *testing.T) {
	now := time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)
	const maxAge = 15 * time.Minute
	body := json.RawMessage(`{"a":1}`)

	cases := []struct {
		name string
		snap quota.Snapshot
		want bool
	}{
		{"fresh 200", quota.Snapshot{HTTPStatus: 200, Payload: body, FetchedAt: now.Add(-time.Minute)}, true},
		{"204 free tier", quota.Snapshot{HTTPStatus: 204, Payload: json.RawMessage(`null`), FetchedAt: now}, false},
		{"424 dead credential", quota.Snapshot{HTTPStatus: 424, Payload: body, FetchedAt: now}, false},
		// Deliberately status 0 *with* a payload. RecordFailure's placeholder has
		// no payload, but pairing the two here would let this case pass on the
		// empty-payload clause alone and keep passing if the status guard were
		// deleted. The payload axis is owned by "200 but empty payload" below.
		{"0 failure placeholder", quota.Snapshot{HTTPStatus: 0, Payload: body, FetchedAt: now}, false},
		{"500 upstream error", quota.Snapshot{HTTPStatus: 500, Payload: body, FetchedAt: now}, false},
		{"200 but empty payload", quota.Snapshot{HTTPStatus: 200, Payload: nil, FetchedAt: now}, false},
		{"200 but stale", quota.Snapshot{HTTPStatus: 200, Payload: body, FetchedAt: now.Add(-16 * time.Minute)}, false},
		{"200 at the age boundary", quota.Snapshot{HTTPStatus: 200, Payload: body, FetchedAt: now.Add(-maxAge)}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := driftEligible(tc.snap, maxAge, now); got != tc.want {
				t.Errorf("driftEligible = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestQuotaDriftEligibleRefusesEverythingWhenPollingIsDisabled verifies the
// watch inherits RefreshQuotaAdvice's rule for a non-positive interval: with no
// live poll cadence there is nothing to bound snapshot age against, so a shape
// recorded before a plan change must not be compared against.
func TestQuotaDriftEligibleRefusesEverythingWhenPollingIsDisabled(t *testing.T) {
	now := time.Now()
	fresh := quota.Snapshot{HTTPStatus: 200, Payload: json.RawMessage(`{"a":1}`), FetchedAt: now}
	if driftEligible(fresh, 0, now) {
		t.Error("a non-positive maxAge means quota polling is disabled; nothing is eligible")
	}
}

// ---------------------------------------------------------------------------
// The operator-facing diff
// ---------------------------------------------------------------------------

// TestQuotaDriftPathDiffNamesTheBillingChange verifies the added/removed lists
// are exactly the key paths that appeared and disappeared, which is the whole
// diagnostic value of the alert.
func TestQuotaDriftPathDiffNamesTheBillingChange(t *testing.T) {
	prev := []string{"account", "account.plan", "account.subscription_period_end"}
	current := []string{"account", "account.credits", "account.credits.balance", "account.plan"}

	added, removed := diffSchemaPaths(prev, current)

	wantAdded := []string{"account.credits", "account.credits.balance"}
	wantRemoved := []string{"account.subscription_period_end"}
	if strings.Join(added, ",") != strings.Join(wantAdded, ",") {
		t.Errorf("added = %v, want %v", added, wantAdded)
	}
	if strings.Join(removed, ",") != strings.Join(wantRemoved, ",") {
		t.Errorf("removed = %v, want %v", removed, wantRemoved)
	}
}

// TestQuotaDriftPathDiffCapsEachListKeepingTheSortedPrefix verifies a wholesale
// reshape cannot build an unbounded event payload, and that what survives the
// cap is the alphabetically first paths as diffSchemaPaths documents. The
// fixtures are sorted because quota.SchemaPaths output and the re-sorted stored
// baseline both are: an unsorted fixture would prove the cap but not the
// retained prefix, and the prefix is what makes the truncated alert readable.
func TestQuotaDriftPathDiffCapsEachListKeepingTheSortedPrefix(t *testing.T) {
	const total = 40
	prev := make([]string, 0, total)
	current := make([]string, 0, total)
	for i := range total {
		prev = append(prev, fmt.Sprintf("gone%03d", i))
		current = append(current, fmt.Sprintf("new%03d", i))
	}
	if !sort.StringsAreSorted(prev) || !sort.StringsAreSorted(current) {
		t.Fatal("fixtures must be sorted to stand in for real SchemaPaths output")
	}

	added, removed := diffSchemaPaths(prev, current)

	wantAdded := current[:quotaDriftListCap]
	wantRemoved := prev[:quotaDriftListCap]
	if strings.Join(added, ",") != strings.Join(wantAdded, ",") {
		t.Errorf("added = %v, want the first %d sorted paths %v", added, quotaDriftListCap, wantAdded)
	}
	if strings.Join(removed, ",") != strings.Join(wantRemoved, ",") {
		t.Errorf("removed = %v, want the first %d sorted paths %v", removed, quotaDriftListCap, wantRemoved)
	}
}

// ---------------------------------------------------------------------------
// Degradation paths: a settings row that cannot be read, parsed, or written
// ---------------------------------------------------------------------------

// driftHandler builds the minimal Handler checkQuotaDrift needs. The watch
// touches only the settings store and its own debounce map, so a struct literal
// exercises the real code without a database.
func driftHandler(store SettingsStore) *Handler {
	return &Handler{settingsRepo: store}
}

// oneAccountSnapshot is a single eligible snapshot: 200, fresh, with a payload
// whose shape is readable.
func oneAccountSnapshot(id uuid.UUID, payload string) []quota.Snapshot {
	return []quota.Snapshot{{
		ProviderID: id, Kind: "account", HTTPStatus: 200,
		Payload: json.RawMessage(payload), FetchedAt: time.Now(),
	}}
}

// TestQuotaDriftBaselineReadFailureSkipsTheProviderEntirely verifies the
// distinction loadSchemaBaseline exists to make: a settings row that could not
// be *read* is not the same as a provider that has never been seen. Treating the
// failure as a first sighting would silently overwrite a good baseline with
// whatever this poll happened to carry, permanently losing the shape the alert
// would have fired on. The pass must instead do nothing at all.
func TestQuotaDriftBaselineReadFailureSkipsTheProviderEntirely(t *testing.T) {
	id := uuid.New()
	writes := 0
	h := driftHandler(&mockSettingsStore{
		getCheckedFn: func(context.Context, string) (string, bool, error) {
			return "", false, errors.New("settings read boom")
		},
		setFn: func(context.Context, string, string) error { writes++; return nil },
	})

	sub := events.Subscribe()
	defer events.Unsubscribe(sub)

	snaps := oneAccountSnapshot(id, `{"plan":"pro"}`)
	// Twice, so a debounce cannot be what keeps this quiet: a pass that cannot
	// read the baseline must not even arm a candidate.
	for range 2 {
		h.checkQuotaDrift(context.Background(), snaps,
			map[uuid.UUID]string{id: "ollama-cloud"}, map[uuid.UUID]string{id: "ollama"}, 15*time.Minute)
	}

	if evs := collectSchemaDriftEvents(t, sub); len(evs) != 0 {
		t.Errorf("a baseline that cannot be read is not evidence of drift, got %d event(s)", len(evs))
	}
	if writes != 0 {
		t.Errorf("a failed read must not be mistaken for a first sighting and overwrite the baseline, got %d write(s)", writes)
	}
	if len(h.quotaSchemaSeen) != 0 {
		t.Errorf("a skipped provider must leave no debounce candidate behind, got %+v", h.quotaSchemaSeen)
	}
}

// TestQuotaDriftCorruptStoredBaselineIsReadoptedSilently verifies the other half
// of that distinction: a row that reads fine but is not a JSON path list is our
// own bug, not the provider changing shape. It must be re-adopted quietly, which
// both silences a false alert and repairs the row for the next poll.
func TestQuotaDriftCorruptStoredBaselineIsReadoptedSilently(t *testing.T) {
	id := uuid.New()
	stored := map[string]string{quotaSchemaSettingKey(id): "}not-a-json-array{"}
	h := driftHandler(&mockSettingsStore{
		getCheckedFn: func(_ context.Context, key string) (string, bool, error) {
			v, ok := stored[key]
			return v, ok, nil
		},
		setFn: func(_ context.Context, key, value string) error { stored[key] = value; return nil },
	})

	sub := events.Subscribe()
	defer events.Unsubscribe(sub)

	h.checkQuotaDrift(context.Background(), oneAccountSnapshot(id, `{"plan":"pro"}`),
		map[uuid.UUID]string{id: "ollama-cloud"}, map[uuid.UUID]string{id: "ollama"}, 15*time.Minute)

	if evs := collectSchemaDriftEvents(t, sub); len(evs) != 0 {
		t.Errorf("a baseline we wrote wrong must not be alerted on as a provider change, got %d event(s)", len(evs))
	}
	if got := stored[quotaSchemaSettingKey(id)]; got != `["plan"]` {
		t.Errorf("a corrupt baseline must be repaired with the shape just seen, got %q", got)
	}
}

// TestQuotaDriftSkipsASnapshotWhoseProviderIsGone verifies a snapshot left
// behind by a deleted provider is dropped before anything else happens. The
// alert's entire content is the provider's name and the paths that moved, so
// there is nothing to publish — and the row must not consume a settings read or
// leave a baseline for an ID that no longer exists.
func TestQuotaDriftSkipsASnapshotWhoseProviderIsGone(t *testing.T) {
	gone := uuid.New()
	reads, writes := 0, 0
	h := driftHandler(&mockSettingsStore{
		getCheckedFn: func(context.Context, string) (string, bool, error) { reads++; return "", false, nil },
		setFn:        func(context.Context, string, string) error { writes++; return nil },
	})

	sub := events.Subscribe()
	defer events.Unsubscribe(sub)

	// Type map still knows the ID; the name map (built from the live provider
	// list) does not. That is exactly the state a deleted provider leaves.
	h.checkQuotaDrift(context.Background(), oneAccountSnapshot(gone, `{"plan":"pro"}`),
		map[uuid.UUID]string{gone: "ollama-cloud"}, map[uuid.UUID]string{}, 15*time.Minute)

	if evs := collectSchemaDriftEvents(t, sub); len(evs) != 0 {
		t.Errorf("a deleted provider has nothing to name in an alert, got %d event(s)", len(evs))
	}
	if reads != 0 || writes != 0 {
		t.Errorf("a deleted provider's snapshot must not touch settings at all, got %d read(s) and %d write(s)", reads, writes)
	}
}

// TestQuotaDriftBaselineWriteFailureIsRetriedNextPass verifies a failed baseline
// write is dropped rather than propagated: this is an advisory watch and must
// never fail a poll pass. It must also not be recorded as if it had succeeded —
// the next pass has to try again, or a provider whose first write failed would
// never acquire a baseline and never be able to alert.
func TestQuotaDriftBaselineWriteFailureIsRetriedNextPass(t *testing.T) {
	id := uuid.New()
	writes := 0
	h := driftHandler(&mockSettingsStore{
		getCheckedFn: func(context.Context, string) (string, bool, error) { return "", false, nil },
		setFn: func(context.Context, string, string) error {
			writes++
			return errors.New("settings write boom")
		},
	})

	sub := events.Subscribe()
	defer events.Unsubscribe(sub)

	snaps := oneAccountSnapshot(id, `{"plan":"pro"}`)
	for range 2 {
		h.checkQuotaDrift(context.Background(), snaps,
			map[uuid.UUID]string{id: "ollama-cloud"}, map[uuid.UUID]string{id: "ollama"}, 15*time.Minute)
	}

	if evs := collectSchemaDriftEvents(t, sub); len(evs) != 0 {
		t.Errorf("a write failure is not drift, got %d event(s)", len(evs))
	}
	if writes != 2 {
		t.Errorf("got %d write attempt(s); a dropped write must be retried on the next pass, not assumed stored", writes)
	}
}

// ---------------------------------------------------------------------------
// End to end through RefreshQuotaAdvice
// ---------------------------------------------------------------------------

// collectSchemaDriftEvents drains ch and returns only quota.schema_drift events,
// so an unrelated event published by another part of the refresh cannot make an
// assertion pass or fail for the wrong reason.
func collectSchemaDriftEvents(t *testing.T, ch chan events.Event) []events.Event {
	t.Helper()
	var out []events.Event
	for {
		select {
		case ev := <-ch:
			if ev.Type == "quota.schema_drift" {
				out = append(out, ev)
			}
		case <-time.After(50 * time.Millisecond):
			return out
		}
	}
}

// storedSchemaBaseline reads back the persisted key-path baseline for a provider.
func storedSchemaBaseline(t *testing.T, h *Handler, id uuid.UUID) ([]string, bool) {
	t.Helper()
	raw, found, err := h.settingsRepo.GetChecked(context.Background(), quotaSchemaSettingKey(id))
	if err != nil {
		t.Fatalf("read stored baseline: %v", err)
	}
	if !found {
		return nil, false
	}
	var paths []string
	if uerr := json.Unmarshal([]byte(raw), &paths); uerr != nil {
		t.Fatalf("stored baseline is not a JSON path list (%q): %v", raw, uerr)
	}
	return paths, true
}

// TestQuotaDriftWatchAlertsOnlyAfterTheChangeRepeats drives the whole watch
// through RefreshQuotaAdvice against a real snapshot table and settings row:
// first poll adopts a baseline silently, the changed shape is debounced for one
// poll, the second consecutive sighting alerts exactly once and promotes the new
// baseline, and a further poll of the same shape is quiet again.
func TestQuotaDriftWatchAlertsOnlyAfterTheChangeRepeats(t *testing.T) {
	h := newTestHandler(t)
	h.SetQuotaAdvisor(NewQuotaAdvisor())
	ctx := context.Background()

	id := insertQuotaPollProvider(t, h.dbPool.Pool(), "ollama-drift", "https://ollama.com", true)

	// Each seed stands for one upstream fetch and gets its own fetched_at, since
	// that is the identity the debounce counts. Ages descend so every row stays
	// well inside the 15-minute eligibility window.
	fetch := 0
	seed := func(payload string) {
		t.Helper()
		fetch++
		if err := h.quotaRepo.Upsert(ctx, quota.Snapshot{
			ProviderID: id, Kind: "account", Payload: json.RawMessage(payload),
			HTTPStatus: 200, Source: "poll",
			FetchedAt: time.Now().Add(-time.Duration(10-fetch) * time.Minute),
		}); err != nil {
			t.Fatalf("seed snapshot: %v", err)
		}
	}

	sub := events.Subscribe()
	defer events.Unsubscribe(sub)

	// Poll 1: first sighting of the subscription-period shape.
	seed(`{"plan":"pro","subscription_period_end":"2026-08-01T00:00:00Z"}`)
	h.RefreshQuotaAdvice(ctx)
	if evs := collectSchemaDriftEvents(t, sub); len(evs) != 0 {
		t.Fatalf("the first sighting must be silent, got %d event(s)", len(evs))
	}
	baseline, found := storedSchemaBaseline(t, h, id)
	if !found {
		t.Fatal("the first sighting must persist a baseline")
	}
	if strings.Join(baseline, ",") != "plan,subscription_period_end" {
		t.Fatalf("stored baseline = %v, want the payload's key paths", baseline)
	}

	// Poll 2: the provider moves to usage credits. Debounced, not yet reported.
	const credits = `{"plan":"pro","credits":{"balance":12.5}}`
	seed(credits)
	h.RefreshQuotaAdvice(ctx)
	if evs := collectSchemaDriftEvents(t, sub); len(evs) != 0 {
		t.Fatalf("a single sighting of the new shape must be debounced, got %d event(s)", len(evs))
	}
	if again, _ := storedSchemaBaseline(t, h, id); strings.Join(again, ",") != "plan,subscription_period_end" {
		t.Fatalf("a debounced change must not move the baseline, got %v", again)
	}

	// Poll 3: same new shape again. One alert, naming the billing change.
	seed(credits)
	h.RefreshQuotaAdvice(ctx)
	evs := collectSchemaDriftEvents(t, sub)
	if len(evs) != 1 {
		t.Fatalf("a confirmed change must publish exactly one alert, got %d", len(evs))
	}
	ev := evs[0]
	if ev.Severity != "warning" || ev.Source != "quota" {
		t.Errorf("event severity/source = %q/%q, want warning/quota", ev.Severity, ev.Source)
	}
	if got, _ := ev.Metadata["provider_id"].(string); got != id.String() {
		t.Errorf("metadata provider_id = %q, want %q", got, id)
	}
	if got, _ := ev.Metadata["provider_type"].(string); got != "ollama-cloud" {
		t.Errorf("metadata provider_type = %q, want ollama-cloud", got)
	}
	if got, _ := ev.Metadata["kind"].(string); got != "account" {
		t.Errorf("metadata kind = %q, want account", got)
	}
	added, _ := ev.Metadata["added"].([]string)
	removed, _ := ev.Metadata["removed"].([]string)
	if strings.Join(added, ",") != "credits,credits.balance" {
		t.Errorf("metadata added = %v, want the new credit paths", added)
	}
	if strings.Join(removed, ",") != "subscription_period_end" {
		t.Errorf("metadata removed = %v, want the dropped subscription path", removed)
	}
	if promoted, _ := storedSchemaBaseline(t, h, id); strings.Join(promoted, ",") != "credits,credits.balance,plan" {
		t.Errorf("alerting must promote the new shape to the baseline, got %v", promoted)
	}

	// Poll 4: the new shape is now the baseline, so it must go quiet.
	seed(credits)
	h.RefreshQuotaAdvice(ctx)
	if evs := collectSchemaDriftEvents(t, sub); len(evs) != 0 {
		t.Fatalf("the promoted shape must alert once, not once per poll, got %d further event(s)", len(evs))
	}
}

// TestQuotaDriftWatchIgnoresNon200Snapshot verifies the eligibility filter holds
// end to end: a provider whose credential died (424) keeps its baseline and
// never alerts, even though the stored payload no longer matches it.
func TestQuotaDriftWatchIgnoresNon200Snapshot(t *testing.T) {
	h := newTestHandler(t)
	h.SetQuotaAdvisor(NewQuotaAdvisor())
	ctx := context.Background()

	id := insertQuotaPollProvider(t, h.dbPool.Pool(), "ollama-dead-key", "https://ollama.com", true)

	if err := h.quotaRepo.Upsert(ctx, quota.Snapshot{
		ProviderID: id, Kind: "account", Payload: json.RawMessage(`{"plan":"pro"}`),
		HTTPStatus: 200, Source: "poll", FetchedAt: time.Now(),
	}); err != nil {
		t.Fatalf("seed good snapshot: %v", err)
	}
	h.RefreshQuotaAdvice(ctx)

	sub := events.Subscribe()
	defer events.Unsubscribe(sub)

	// The credential dies: the endpoint now answers 424 with an error body of a
	// completely different shape. Twice, so the debounce is not what saves us.
	for range 2 {
		if err := h.quotaRepo.Upsert(ctx, quota.Snapshot{
			ProviderID: id, Kind: "account", Payload: json.RawMessage(`{"error":{"code":"invalid_key"}}`),
			HTTPStatus: 424, Source: "poll", FetchedAt: time.Now(),
		}); err != nil {
			t.Fatalf("seed dead-credential snapshot: %v", err)
		}
		h.RefreshQuotaAdvice(ctx)
	}

	if evs := collectSchemaDriftEvents(t, sub); len(evs) != 0 {
		t.Fatalf("a non-200 snapshot is a billing-independent failure, not drift, got %d event(s)", len(evs))
	}
	if baseline, _ := storedSchemaBaseline(t, h, id); strings.Join(baseline, ",") != "plan" {
		t.Errorf("a non-200 snapshot must not overwrite the baseline, got %v", baseline)
	}
}

// TestQuotaDriftWatchDoesNotConfirmOnARefetchedRow is the fleet-dedup case end to
// end. A member fed by Front Desk keeps a snapshot row for a full interval while
// PollQuotasOnce skips its own upstream call, and RefreshQuotaAdvice still runs
// each pass: the same row is read again with no new fetch behind it. Three
// further passes over that one row must not confirm the change, and a genuinely
// re-fetched row must.
func TestQuotaDriftWatchDoesNotConfirmOnARefetchedRow(t *testing.T) {
	h := newTestHandler(t)
	h.SetQuotaAdvisor(NewQuotaAdvisor())
	ctx := context.Background()

	id := insertQuotaPollProvider(t, h.dbPool.Pool(), "ollama-fleet-fed", "https://ollama.com", true)

	upsert := func(payload string, fetchedAt time.Time) {
		t.Helper()
		if err := h.quotaRepo.Upsert(ctx, quota.Snapshot{
			ProviderID: id, Kind: "account", Payload: json.RawMessage(payload),
			HTTPStatus: 200, Source: "fleet", FetchedAt: fetchedAt,
		}); err != nil {
			t.Fatalf("seed snapshot: %v", err)
		}
	}

	// Baseline adopted from the first fetch.
	firstFetch := time.Now().Add(-9 * time.Minute)
	upsert(`{"plan":"pro","subscription_period_end":"2026-08-01T00:00:00Z"}`, firstFetch)
	h.RefreshQuotaAdvice(ctx)

	sub := events.Subscribe()
	defer events.Unsubscribe(sub)

	// One fleet-distributed fetch carries a new shape. It then sits in the table
	// while three more refresh passes run over it.
	const credits = `{"plan":"pro","credits":{"balance":12.5}}`
	driftFetch := time.Now().Add(-8 * time.Minute)
	upsert(credits, driftFetch)
	for range 4 {
		h.RefreshQuotaAdvice(ctx)
	}

	if evs := collectSchemaDriftEvents(t, sub); len(evs) != 0 {
		t.Fatalf("re-reading one stored row must not confirm a change, got %d event(s)", len(evs))
	}
	if baseline, _ := storedSchemaBaseline(t, h, id); strings.Join(baseline, ",") != "plan,subscription_period_end" {
		t.Fatalf("an unconfirmed change must not move the baseline, got %v", baseline)
	}

	// A second, genuinely distinct fetch of the same shape confirms it.
	upsert(credits, time.Now().Add(-7*time.Minute))
	h.RefreshQuotaAdvice(ctx)

	if evs := collectSchemaDriftEvents(t, sub); len(evs) != 1 {
		t.Fatalf("a second real fetch of the same new shape must alert exactly once, got %d", len(evs))
	}
	if baseline, _ := storedSchemaBaseline(t, h, id); strings.Join(baseline, ",") != "credits,credits.balance,plan" {
		t.Errorf("the confirmed shape must become the baseline, got %v", baseline)
	}
}
