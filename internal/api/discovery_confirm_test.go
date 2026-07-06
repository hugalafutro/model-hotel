package api

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

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

func TestSleepWithContext(t *testing.T) {
	if err := sleepWithContext(context.Background(), 0); err != nil {
		t.Errorf("zero delay: expected nil, got %v", err)
	}
	if err := sleepWithContext(context.Background(), time.Millisecond); err != nil {
		t.Errorf("short sleep: expected nil, got %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := sleepWithContext(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled ctx: expected context.Canceled, got %v", err)
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
