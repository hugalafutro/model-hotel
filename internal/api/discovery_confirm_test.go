package api

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hugalafutro/model-hotel/internal/events"
	"github.com/hugalafutro/model-hotel/internal/logtest"
	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/provider"
)

// ---------------------------------------------------------------------------
// ConfirmMissingModels — pure unit tests (no DB, no HTTP): the probe listing
// is injected via discoverModelsForConfirm and delays are zeroed in TestMain.
// ---------------------------------------------------------------------------

func confirmTestProvider() *provider.Provider {
	return &provider.Provider{ID: uuid.New(), Name: "confirm-test"}
}

func confirmTestSnapshot(enabled ...string) map[string]ModelSnapshot {
	snap := make(map[string]ModelSnapshot, len(enabled))
	for _, id := range enabled {
		snap[id] = ModelSnapshot{enabled: true}
	}
	return snap
}

func modelsForIDs(ids ...string) []*model.Model {
	out := make([]*model.Model, 0, len(ids))
	for _, id := range ids {
		out = append(out, &model.Model{ModelID: id})
	}
	return out
}

// overrideConfirmDiscover replaces the probe listing for one test and restores
// it afterwards. Each call to the probe returns the next listing in sequence
// (the last one repeats); errs mark probe calls that fail instead.
func overrideConfirmDiscover(t *testing.T, listings [][]*model.Model, errs []error) *int {
	t.Helper()
	orig := discoverModelsForConfirm
	t.Cleanup(func() { discoverModelsForConfirm = orig })
	calls := 0
	discoverModelsForConfirm = func(ctx context.Context, svc *provider.DiscoveryService, prov *provider.Provider, masterKey string) ([]*model.Model, error) {
		idx := calls
		calls++
		if idx < len(errs) && errs[idx] != nil {
			return nil, errs[idx]
		}
		if len(listings) == 0 {
			return nil, nil
		}
		if idx >= len(listings) {
			idx = len(listings) - 1
		}
		return listings[idx], nil
	}
	return &calls
}

func TestConfirmMissingModels_NothingMissing_NoProbes(t *testing.T) {
	calls := overrideConfirmDiscover(t, nil, nil)

	present := []string{"m1", "m2"}
	confirmed, suspect := ConfirmMissingModels(context.Background(), nil, confirmTestProvider(), "", present, confirmTestSnapshot("m1", "m2"), nil)
	if suspect {
		t.Fatal("expected suspect=false")
	}
	if len(confirmed) != 2 {
		t.Fatalf("expected confirmed membership unchanged, got %v", confirmed)
	}
	if *calls != 0 {
		t.Fatalf("expected no probe calls, got %d", *calls)
	}
}

func TestConfirmMissingModels_DisabledSnapshotModelIgnored(t *testing.T) {
	calls := overrideConfirmDiscover(t, nil, nil)

	snapshot := confirmTestSnapshot("m1")
	snapshot["already-off"] = ModelSnapshot{enabled: false}
	_, suspect := ConfirmMissingModels(context.Background(), nil, confirmTestProvider(), "", []string{"m1"}, snapshot, nil)
	if suspect || *calls != 0 {
		t.Fatalf("disabled snapshot model must not trigger probes: suspect=%v calls=%d", suspect, *calls)
	}
}

func TestConfirmMissingModels_ReappearsOnProbe(t *testing.T) {
	// Initial listing dropped m2; the first probe lists it again.
	calls := overrideConfirmDiscover(t, [][]*model.Model{modelsForIDs("m1", "m2")}, nil)

	confirmed, suspect := ConfirmMissingModels(context.Background(), nil, confirmTestProvider(), "", []string{"m1"}, confirmTestSnapshot("m1", "m2"), nil)
	if suspect {
		t.Fatal("expected suspect=false")
	}
	if *calls != 1 {
		t.Fatalf("expected exactly 1 probe (stop early once nothing is missing), got %d", *calls)
	}
	found := map[string]bool{}
	for _, id := range confirmed {
		found[id] = true
	}
	if !found["m1"] || !found["m2"] {
		t.Fatalf("expected m2 restored to confirmed membership, got %v", confirmed)
	}
}

func TestConfirmMissingModels_ReappearsOnSecondProbe(t *testing.T) {
	calls := overrideConfirmDiscover(t, [][]*model.Model{
		modelsForIDs("m1"),
		modelsForIDs("m1", "m2"),
	}, nil)

	confirmed, suspect := ConfirmMissingModels(context.Background(), nil, confirmTestProvider(), "", []string{"m1"}, confirmTestSnapshot("m1", "m2"), nil)
	if suspect {
		t.Fatal("expected suspect=false")
	}
	if *calls != 2 {
		t.Fatalf("expected 2 probes, got %d", *calls)
	}
	found := map[string]bool{}
	for _, id := range confirmed {
		found[id] = true
	}
	if !found["m2"] {
		t.Fatalf("expected m2 restored on second probe, got %v", confirmed)
	}
}

func TestConfirmMissingModels_ConfirmedMissingAfterAllProbes(t *testing.T) {
	calls := overrideConfirmDiscover(t, [][]*model.Model{modelsForIDs("m1")}, nil)

	confirmed, suspect := ConfirmMissingModels(context.Background(), nil, confirmTestProvider(), "", []string{"m1"}, confirmTestSnapshot("m1", "m2"), nil)
	if suspect {
		t.Fatal("expected suspect=false: a single confirmed-missing model is a legitimate miss")
	}
	if *calls != len(confirmProbeDelays) {
		t.Fatalf("expected %d probes, got %d", len(confirmProbeDelays), *calls)
	}
	for _, id := range confirmed {
		if id == "m2" {
			t.Fatalf("m2 must not be in confirmed membership, got %v", confirmed)
		}
	}
}

func TestConfirmMissingModels_ProbeErrorMarksSuspect(t *testing.T) {
	overrideConfirmDiscover(t, nil, []error{errors.New("dns flake")})

	_, suspect := ConfirmMissingModels(context.Background(), nil, confirmTestProvider(), "", []string{"m1"}, confirmTestSnapshot("m1", "m2"), nil)
	if !suspect {
		t.Fatal("expected suspect=true when a confirmation probe fails")
	}
}

func TestConfirmMissingModels_CancelledContextMarksSuspect(t *testing.T) {
	origSleep := confirmProbeSleep
	t.Cleanup(func() { confirmProbeSleep = origSleep })
	confirmProbeSleep = func(ctx context.Context, d time.Duration) error { return context.Canceled }

	calls := overrideConfirmDiscover(t, [][]*model.Model{modelsForIDs("m1", "m2")}, nil)

	_, suspect := ConfirmMissingModels(context.Background(), nil, confirmTestProvider(), "", []string{"m1"}, confirmTestSnapshot("m1", "m2"), nil)
	if !suspect {
		t.Fatal("expected suspect=true on cancelled backoff")
	}
	if *calls != 0 {
		t.Fatalf("expected no probe after cancelled backoff, got %d", *calls)
	}
}

func TestConfirmMissingModels_MassVanishGuard(t *testing.T) {
	// 10 enabled models, listing only returns 2: 8 missing is > floor(5) and
	// > 50% of enabled, so the scan is suspect and records nothing.
	enabled := make([]string, 10)
	for i := range enabled {
		enabled[i] = fmt.Sprintf("m%d", i)
	}
	overrideConfirmDiscover(t, [][]*model.Model{modelsForIDs("m0", "m1")}, nil)

	_, suspect := ConfirmMissingModels(context.Background(), nil, confirmTestProvider(), "", []string{"m0", "m1"}, confirmTestSnapshot(enabled...), nil)
	if !suspect {
		t.Fatal("expected suspect=true from mass-vanish guard")
	}
}

func TestConfirmMissingModels_MassVanishFloorBoundary(t *testing.T) {
	enabled := make([]string, 10)
	for i := range enabled {
		enabled[i] = fmt.Sprintf("m%d", i)
	}

	// Exactly at the floor: 5 of 10 missing. The guard uses a strict `>` on both
	// the floor (5 > 5 is false) and the ratio (5 > 5.0 is false), so this must
	// NOT trip: it is a confirmable miss, not a suspect scan.
	present5 := []string{"m0", "m1", "m2", "m3", "m4"}
	overrideConfirmDiscover(t, [][]*model.Model{modelsForIDs(present5...)}, nil)
	if _, suspect := ConfirmMissingModels(context.Background(), nil, confirmTestProvider(), "", present5, confirmTestSnapshot(enabled...), nil); suspect {
		t.Fatal("5 of 10 missing sits exactly on the floor; expected suspect=false")
	}

	// One past the floor: 6 of 10 missing clears both `>` checks and trips.
	present4 := []string{"m0", "m1", "m2", "m3"}
	overrideConfirmDiscover(t, [][]*model.Model{modelsForIDs(present4...)}, nil)
	if _, suspect := ConfirmMissingModels(context.Background(), nil, confirmTestProvider(), "", present4, confirmTestSnapshot(enabled...), nil); !suspect {
		t.Fatal("6 of 10 missing clears the floor; expected suspect=true")
	}
}

func TestConfirmMissingModels_TotalBlackoutBelowFloorIsSuspect(t *testing.T) {
	// A small provider (3 enabled) whose initial listing and every probe return
	// nothing at all. 3 missing sits at/below the floor of 5, so the mass-vanish
	// floor+ratio guard would leave it suspect=false and RecordMissingModels's
	// empty-list no-op would then silently keep every stale model enabled. The
	// total-blackout branch must catch it instead: disable nothing, but mark the
	// scan suspect so it escalates for an operator.
	overrideConfirmDiscover(t, nil, nil) // every probe returns an empty listing

	confirmed, suspect := ConfirmMissingModels(context.Background(), nil, confirmTestProvider(), "", nil,
		confirmTestSnapshot("m0", "m1", "m2"), nil)
	if !suspect {
		t.Fatal("total blackout on a small provider must be suspect, not a silent no-op")
	}
	if len(confirmed) != 0 {
		t.Fatalf("expected empty confirmed membership, got %v", confirmed)
	}
}

func TestConfirmMissingModels_NoEnabledModelsNotSuspect(t *testing.T) {
	// Empty present list AND no enabled models (e.g. a brand-new provider with an
	// empty listing): nothing is missing, so this is not a blackout and must not
	// be flagged suspect.
	overrideConfirmDiscover(t, nil, nil)

	_, suspect := ConfirmMissingModels(context.Background(), nil, confirmTestProvider(), "", nil,
		confirmTestSnapshot(), nil)
	if suspect {
		t.Fatal("no enabled models means nothing missing; expected suspect=false")
	}
}

func TestShouldEscalateSuspect(t *testing.T) {
	// Fires on the crossing scan and once every threshold thereafter; silent
	// below the threshold and between multiples.
	cases := map[int]bool{
		0: false, 1: false, 2: false,
		3: true, 4: false, 5: false,
		6: true, 7: false,
		9: true,
	}
	for streak, want := range cases {
		if got := shouldEscalateSuspect(streak); got != want {
			t.Errorf("shouldEscalateSuspect(%d) = %v, want %v", streak, got, want)
		}
	}
}

func TestConfirmMissingModels_PinnedModelsExcludedFromMassVanishGuard(t *testing.T) {
	// 10 enabled models; the listing returns 2. The other 8 are operator pins:
	// permanently absent by design, because a pin IS the answer to "the listing
	// stopped naming this model". Counted as evidence, they mark every scan of
	// this provider suspect, which freezes listing-based auto-disable for the
	// whole provider and repeats the suspect-scan alert on every sweep.
	enabled := make([]string, 10)
	for i := range enabled {
		enabled[i] = fmt.Sprintf("m%d", i)
	}
	present := []string{"m0", "m1"}

	pinnedSnapshot := confirmTestSnapshot(enabled...)
	for i := 2; i < 10; i++ {
		pinnedSnapshot[fmt.Sprintf("m%d", i)] = ModelSnapshot{enabled: true, pinned: true}
	}
	overrideConfirmDiscover(t, [][]*model.Model{modelsForIDs(present...)}, nil)
	if _, suspect := ConfirmMissingModels(context.Background(), nil, confirmTestProvider(), "", present, pinnedSnapshot, nil); suspect {
		t.Fatal("8 pinned models absent from the listing must not trip the mass-vanish guard")
	}

	// Control: the very same shape without the pins is exactly the broken listing
	// the guard exists for, so the exclusion must not have disarmed it.
	overrideConfirmDiscover(t, [][]*model.Model{modelsForIDs(present...)}, nil)
	if _, suspect := ConfirmMissingModels(context.Background(), nil, confirmTestProvider(), "", present, confirmTestSnapshot(enabled...), nil); !suspect {
		t.Fatal("8 unpinned models absent from the listing must still trip the guard")
	}
}

func TestConfirmMissingModels_PinnedModelsDoNotDiluteTheRatio(t *testing.T) {
	// The exclusion is two-sided: pins leave the denominator as well as the
	// numerator. Left in the denominator they pad the population the listing is
	// judged against, so a real mass vanish sits under the ratio and the guard
	// never fires. 100 unpinned models with 51 missing is over the half mark
	// (51 > 50) and must trip; 8 pins on top would put it under (51 > 54 is
	// false) if the denominator counted them.
	present := make([]string, 0, 49)
	snapshot := map[string]ModelSnapshot{}
	for i := range 100 {
		id := fmt.Sprintf("m%d", i)
		snapshot[id] = ModelSnapshot{enabled: true}
		if i >= 51 {
			present = append(present, id)
		}
	}
	for i := range 8 {
		snapshot[fmt.Sprintf("pin%d", i)] = ModelSnapshot{enabled: true, pinned: true}
	}
	overrideConfirmDiscover(t, [][]*model.Model{modelsForIDs(present...)}, nil)

	if _, suspect := ConfirmMissingModels(context.Background(), nil, confirmTestProvider(), "", present, snapshot, nil); !suspect {
		t.Fatal("51 of 100 unpinned models missing is a mass vanish; the pins must not pad the denominator")
	}
}

func TestConfirmMissingModels_SmallMissBelowFloorNotSuspect(t *testing.T) {
	// 4 of 6 missing is >50% but at/below the absolute floor of 5, so it is a
	// legitimate (confirmable) miss, not a suspect scan.
	overrideConfirmDiscover(t, [][]*model.Model{modelsForIDs("m0", "m1")}, nil)

	_, suspect := ConfirmMissingModels(context.Background(), nil, confirmTestProvider(), "", []string{"m0", "m1"},
		confirmTestSnapshot("m0", "m1", "m2", "m3", "m4", "m5"), nil)
	if suspect {
		t.Fatal("expected suspect=false below the mass-vanish floor")
	}
}

// ---------------------------------------------------------------------------
// SuspectStreak — integration tests against the providers.suspect_scans column.
// ---------------------------------------------------------------------------

// insertStreakProvider seeds one provider row and removes it when the test ends.
func insertStreakProvider(t *testing.T) *provider.Provider {
	t.Helper()
	ctx := context.Background()
	prov := &provider.Provider{Name: "streak-" + uuid.NewString()}
	if err := apiTestDB.Pool().QueryRow(ctx, `
		INSERT INTO providers (name, base_url, provider_type, enabled, autodiscovery_enabled)
		VALUES ($1, 'https://streak.example/v1', 'openai', true, true) RETURNING id`, prov.Name).Scan(&prov.ID); err != nil {
		t.Fatalf("seed provider: %v", err)
	}
	t.Cleanup(func() {
		if _, err := apiTestDB.Pool().Exec(context.Background(), `DELETE FROM providers WHERE id = $1`, prov.ID); err != nil {
			t.Errorf("delete provider: %v", err)
		}
	})
	return prov
}

func suspectScans(t *testing.T, id uuid.UUID) int {
	t.Helper()
	var n int
	if err := apiTestDB.Pool().QueryRow(context.Background(),
		`SELECT suspect_scans FROM providers WHERE id = $1`, id).Scan(&n); err != nil {
		t.Fatalf("read suspect_scans: %v", err)
	}
	return n
}

// drainBulkRemovalEvents returns the bulk-removal alerts already delivered for
// one provider. Publish sends synchronously, so nothing is still in flight.
func drainBulkRemovalEvents(ch chan events.Event, id uuid.UUID) []events.Event {
	var out []events.Event
	for {
		select {
		case ev := <-ch:
			if ev.Type == "discovery.bulk_removal_suspected" && ev.Metadata["provider_id"] == id {
				out = append(out, ev)
			}
		default:
			return out
		}
	}
}

// closedPool returns a pool to the test DB that is already closed, so every
// query on it fails.
func closedPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), apiTestDBURL)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	pool.Close()
	return pool
}

func TestSuspectStreak_BumpCountsAndEscalatesEveryThreshold(t *testing.T) {
	prov := insertStreakProvider(t)
	streak := NewSuspectStreak(apiTestDB.Pool())
	sub := events.Subscribe()
	defer events.Unsubscribe(sub)

	// Through two thresholds: the alert fires on the crossing scan, stays quiet
	// in between, and re-fires on the next multiple.
	for scan := 1; scan <= 2*suspectStreakThreshold; scan++ {
		streak.bump(context.Background(), prov, 8, 10)

		if got := suspectScans(t, prov.ID); got != scan {
			t.Fatalf("after bump %d: suspect_scans = %d, want %d", scan, got, scan)
		}
		evs := drainBulkRemovalEvents(sub, prov.ID)
		if scan%suspectStreakThreshold != 0 {
			if len(evs) != 0 {
				t.Fatalf("after bump %d: got %d bulk-removal event(s), want none below/between thresholds", scan, len(evs))
			}
			continue
		}
		if len(evs) != 1 {
			t.Fatalf("after bump %d: got %d bulk-removal event(s), want exactly 1", scan, len(evs))
		}
		ev := evs[0]
		if ev.Severity != "error" || ev.Source != "discovery" {
			t.Errorf("after bump %d: severity/source = %q/%q, want error/discovery", scan, ev.Severity, ev.Source)
		}
		if ev.Metadata["consecutive_scans"] != scan || ev.Metadata["missing"] != 8 || ev.Metadata["enabled"] != 10 {
			t.Errorf("after bump %d: metadata = %v, want consecutive_scans=%d missing=8 enabled=10", scan, ev.Metadata, scan)
		}
	}
}

func TestSuspectStreak_ResetClearsCounter(t *testing.T) {
	prov := insertStreakProvider(t)
	streak := NewSuspectStreak(apiTestDB.Pool())
	for range 2 {
		streak.bump(context.Background(), prov, 8, 10)
	}
	if got := suspectScans(t, prov.ID); got != 2 {
		t.Fatalf("suspect_scans = %d before reset, want 2", got)
	}

	streak.reset(context.Background(), prov)
	if got := suspectScans(t, prov.ID); got != 0 {
		t.Fatalf("suspect_scans = %d after reset, want 0", got)
	}

	// A streak broken by a healthy scan starts over: the next bump is scan 1,
	// not scan 3, so it must not escalate.
	sub := events.Subscribe()
	defer events.Unsubscribe(sub)
	streak.bump(context.Background(), prov, 8, 10)
	if got := suspectScans(t, prov.ID); got != 1 {
		t.Fatalf("suspect_scans = %d after reset+bump, want 1", got)
	}
	if evs := drainBulkRemovalEvents(sub, prov.ID); len(evs) != 0 {
		t.Fatalf("got %d bulk-removal event(s) after a reset streak restarted, want none", len(evs))
	}
}

func TestSuspectStreak_DBFailuresAreLoggedNotFatal(t *testing.T) {
	logs := logtest.Capture(t)
	prov := confirmTestProvider()
	streak := NewSuspectStreak(closedPool(t))
	sub := events.Subscribe()
	defer events.Unsubscribe(sub)

	streak.bump(context.Background(), prov, 8, 10)
	rec, ok := logs.Find("discovery: failed to bump provider suspect streak")
	if !ok {
		t.Fatal("bump on a closed pool must log the bump-failure warning")
	}
	if rec.Attrs["provider"] != prov.Name || rec.Attrs["error"] == nil {
		t.Errorf("bump warning attrs = %v, want provider=%q and an error", rec.Attrs, prov.Name)
	}
	if evs := drainBulkRemovalEvents(sub, prov.ID); len(evs) != 0 {
		t.Errorf("a failed bump has no streak to escalate, got %d event(s)", len(evs))
	}

	streak.reset(context.Background(), prov)
	rec, ok = logs.Find("discovery: failed to reset provider suspect streak")
	if !ok {
		t.Fatal("reset on a closed pool must log the reset-failure warning")
	}
	if rec.Attrs["provider"] != prov.Name || rec.Attrs["error"] == nil {
		t.Errorf("reset warning attrs = %v, want provider=%q and an error", rec.Attrs, prov.Name)
	}
}
