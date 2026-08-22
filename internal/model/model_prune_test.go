package model

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestPruneRetired pins every exclusion rule with one row each, plus the row
// that must go. Each row is the happy row with exactly one thing changed, so a
// rule that stops filtering shows up as that row's name in the failure.
func TestPruneRetired(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository(testPool)

	onProvider := insertTestProvider(ctx, t, "prune-on")
	t.Cleanup(func() { cleanupProvider(ctx, t, onProvider) })
	offProvider := insertTestProvider(ctx, t, "prune-off")
	t.Cleanup(func() { cleanupProvider(ctx, t, offProvider) })
	if _, err := testPool.Exec(ctx, `UPDATE providers SET enabled = false WHERE id = $1`, offProvider); err != nil {
		t.Fatalf("disable provider: %v", err)
	}
	otherProvider := insertTestProvider(ctx, t, "prune-other")
	t.Cleanup(func() { cleanupProvider(ctx, t, otherProvider) })
	// enabled = NULL is the third state the column allows; COALESCE treats it
	// as off, so its models are parked exactly like a disabled provider's.
	nullFlagProvider := insertTestProvider(ctx, t, "prune-null-flag")
	t.Cleanup(func() { cleanupProvider(ctx, t, nullFlagProvider) })
	if _, err := testPool.Exec(ctx, `UPDATE providers SET enabled = NULL WHERE id = $1`, nullFlagProvider); err != nil {
		t.Fatalf("null provider flag: %v", err)
	}

	old := time.Now().Add(-40 * 24 * time.Hour)
	recent := time.Now().Add(-2 * 24 * time.Hour)

	type row struct {
		name      string
		provider  uuid.UUID
		enabled   bool
		manual    bool
		pinnedAt  *time.Time
		retiredAt *time.Time
		lastSeen  time.Time
		dismissed bool
		// noLastSeen leaves last_seen_at NULL and backdates created_at to
		// lastSeen instead, which is the fallback the age COALESCE reads.
		noLastSeen bool
	}
	now := time.Now()
	rows := []row{
		{name: "prune-prunable", provider: onProvider, lastSeen: old},
		{name: "prune-prunable-dismissed", provider: onProvider, lastSeen: old, dismissed: true},
		{name: "prune-still-enabled", provider: onProvider, enabled: true, lastSeen: old},
		{name: "prune-operator-disabled", provider: onProvider, manual: true, lastSeen: old},
		{name: "prune-pinned", provider: onProvider, pinnedAt: &now, lastSeen: old},
		{name: "prune-traffic-retired", provider: onProvider, retiredAt: &now, lastSeen: old},
		{name: "prune-parked", provider: offProvider, lastSeen: old},
		{name: "prune-too-recent", provider: onProvider, lastSeen: recent},
		{name: "prune-flapped", provider: onProvider, lastSeen: old},
		{name: "prune-not-scanned", provider: otherProvider, lastSeen: old},
		{name: "prune-null-provider-flag", provider: nullFlagProvider, lastSeen: old},
		{name: "prune-null-last-seen", provider: onProvider, lastSeen: old, noLastSeen: true},
	}
	ids := map[string]uuid.UUID{}
	for _, r := range rows {
		id := uuid.New()
		ids[r.name] = id
		var dismissed *time.Time
		if r.dismissed {
			dismissed = &now
		}
		lastSeen := &r.lastSeen
		if r.noLastSeen {
			lastSeen = nil
		}
		if _, err := testPool.Exec(ctx, `
			INSERT INTO models (id, provider_id, model_id, name, enabled, disabled_manually,
			                    manually_enabled_at, auto_retired_at, last_seen_at, discovery_dismissed_at)
			VALUES ($1, $2, $3, $3, $4, $5, $6, $7, $8, $9)`,
			id, r.provider, r.name, r.enabled, r.manual, r.pinnedAt, r.retiredAt, lastSeen, dismissed); err != nil {
			t.Fatalf("insert %s: %v", r.name, err)
		}
		if r.noLastSeen {
			if _, err := testPool.Exec(ctx, `UPDATE models SET created_at = $2 WHERE id = $1`, id, r.lastSeen); err != nil {
				t.Fatalf("backdate %s: %v", r.name, err)
			}
		}
	}

	// The flapped row's evidence is a membership transition in the change
	// journal inside the window, exactly what the claims modal counts.
	insertJournalTransition(ctx, t, onProvider, "prune-flapped", time.Now().Add(-time.Hour))

	pruned, err := repo.PruneRetired(ctx, time.Now().Add(-30*24*time.Hour), time.Now().Add(-30*24*time.Hour),
		[]uuid.UUID{onProvider, offProvider, nullFlagProvider},
		500)
	if err != nil {
		t.Fatalf("PruneRetired: %v", err)
	}

	got := map[string]bool{}
	for _, p := range pruned {
		got[p.ModelID] = true
		if p.ProviderName != "prune-on" || p.ProviderID != onProvider || p.ID != ids[p.ModelID] {
			t.Errorf("pruned ref %+v carries wrong provider/id", p)
		}
	}
	want := map[string]bool{"prune-prunable": true, "prune-prunable-dismissed": true, "prune-null-last-seen": true}
	for _, r := range rows {
		if got[r.name] != want[r.name] {
			t.Errorf("%s: pruned=%v, want %v", r.name, got[r.name], want[r.name])
		}
	}
	// The table agrees with the return value.
	var remaining int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM models WHERE provider_id = ANY($1)`,
		[]uuid.UUID{onProvider, offProvider, otherProvider, nullFlagProvider}).Scan(&remaining); err != nil {
		t.Fatalf("count: %v", err)
	}
	if remaining != len(rows)-len(want) {
		t.Errorf("remaining rows = %d, want %d", remaining, len(rows)-len(want))
	}
}

// TestPruneRetired_CapAndOrder pins the bound: oldest rows go first and no
// more than limit go in one call, so a provider that retired its whole catalog
// cannot empty a member in one pass.
func TestPruneRetired_CapAndOrder(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository(testPool)
	prov := insertTestProvider(ctx, t, "prune-cap")
	t.Cleanup(func() { cleanupProvider(ctx, t, prov) })

	for i := range 5 {
		if _, err := testPool.Exec(ctx, `
			INSERT INTO models (id, provider_id, model_id, name, enabled, disabled_manually, last_seen_at)
			VALUES ($1, $2, $3, $3, false, false, now() - make_interval(days => $4))`,
			uuid.New(), prov, "prune-cap-"+string(rune('a'+i)), 40+i); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}
	pruned, err := repo.PruneRetired(ctx, time.Now().Add(-30*24*time.Hour), time.Now().Add(-30*24*time.Hour), []uuid.UUID{prov}, 2)
	if err != nil {
		t.Fatalf("PruneRetired: %v", err)
	}
	if len(pruned) != 2 {
		t.Fatalf("pruned %d rows, want 2", len(pruned))
	}
	// Oldest first: prune-cap-e (44 days) then prune-cap-d (43 days).
	if pruned[0].ModelID != "prune-cap-e" || pruned[1].ModelID != "prune-cap-d" {
		t.Errorf("pruned order = %s, %s; want prune-cap-e, prune-cap-d", pruned[0].ModelID, pruned[1].ModelID)
	}
}

// TestPruneRetired_NoProvidersNoop pins the scope guard at the repo level: an
// empty provider list prunes nothing even when rows qualify.
func TestPruneRetired_NoProvidersNoop(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository(testPool)
	prov := insertTestProvider(ctx, t, "prune-noop")
	t.Cleanup(func() { cleanupProvider(ctx, t, prov) })

	if _, err := testPool.Exec(ctx, `
		INSERT INTO models (id, provider_id, model_id, name, enabled, disabled_manually, last_seen_at)
		VALUES ($1, $2, 'prune-noop', 'prune-noop', false, false, now() - interval '40 days')`, uuid.New(), prov); err != nil {
		t.Fatalf("insert: %v", err)
	}
	pruned, err := repo.PruneRetired(ctx, time.Now(), time.Now(), nil, 500)
	if err != nil {
		t.Fatalf("PruneRetired: %v", err)
	}
	if len(pruned) != 0 {
		t.Errorf("pruned %d rows with no providers in scope, want 0", len(pruned))
	}
}

// insertPrunableModel inserts a model row that qualifies as a prune
// candidate: disabled, no manual pin, no traffic retirement, last seen old.
func insertPrunableModel(ctx context.Context, t *testing.T, providerID uuid.UUID, modelID string) uuid.UUID {
	t.Helper()

	id := uuid.New()
	if _, err := testPool.Exec(ctx, `
		INSERT INTO models (id, provider_id, model_id, name, enabled, disabled_manually, last_seen_at)
		VALUES ($1, $2, $3, $3, false, false, now() - interval '40 days')`,
		id, providerID, modelID); err != nil {
		t.Fatalf("insert %s: %v", modelID, err)
	}
	return id
}

// modelExists reports whether a model row is still present.
func modelExists(ctx context.Context, t *testing.T, id uuid.UUID) bool {
	t.Helper()

	var exists bool
	if err := testPool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM models WHERE id = $1)`, id).Scan(&exists); err != nil {
		t.Fatalf("check exists: %v", err)
	}
	return exists
}

// TestPruneRetired_ReconcilesRevivedRows pins the two things that can change
// between selectPruneCandidates building the candidate list and
// deletePruneCandidates acting on it: a row itself getting re-enabled (a
// re-listing landed in the window), and its provider getting disabled. Both
// must leave the surviving row in place and out of the reported result,
// which the single PruneRetired-level test cannot exercise because nothing
// can be mutated between its internal select and delete.
func TestPruneRetired_ReconcilesRevivedRows(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository(testPool)

	t.Run("row revived mid-flight", func(t *testing.T) {
		prov := insertTestProvider(ctx, t, "prune-recon-a")
		t.Cleanup(func() { cleanupProvider(ctx, t, prov) })

		aliveID := insertPrunableModel(ctx, t, prov, "prune-recon-alive")
		goneID := insertPrunableModel(ctx, t, prov, "prune-recon-gone")

		candidates, err := repo.selectPruneCandidates(ctx, time.Now().Add(-30*24*time.Hour), time.Now().Add(-30*24*time.Hour), []uuid.UUID{prov}, 500)
		if err != nil {
			t.Fatalf("selectPruneCandidates: %v", err)
		}
		if len(candidates) != 2 {
			t.Fatalf("selectPruneCandidates returned %d candidates, want 2", len(candidates))
		}

		// A re-listing lands in the window: the row is re-enabled after the
		// select but before the delete.
		if _, err := testPool.Exec(ctx, `UPDATE models SET enabled = true WHERE id = $1`, aliveID); err != nil {
			t.Fatalf("revive row: %v", err)
		}

		pruned, err := repo.deletePruneCandidates(ctx, candidates, time.Now().Add(-30*24*time.Hour))
		if err != nil {
			t.Fatalf("deletePruneCandidates: %v", err)
		}
		if len(pruned) != 1 || pruned[0].ID != goneID {
			t.Errorf("pruned = %+v, want exactly [%s]", pruned, goneID)
		}
		if !modelExists(ctx, t, aliveID) {
			t.Error("revived row was deleted, want it kept")
		}
		if modelExists(ctx, t, goneID) {
			t.Error("still-retired row was kept, want it deleted")
		}
	})

	t.Run("provider disabled mid-flight", func(t *testing.T) {
		prov := insertTestProvider(ctx, t, "prune-recon-b")
		t.Cleanup(func() { cleanupProvider(ctx, t, prov) })

		id1 := insertPrunableModel(ctx, t, prov, "prune-recon-b1")
		id2 := insertPrunableModel(ctx, t, prov, "prune-recon-b2")

		candidates, err := repo.selectPruneCandidates(ctx, time.Now().Add(-30*24*time.Hour), time.Now().Add(-30*24*time.Hour), []uuid.UUID{prov}, 500)
		if err != nil {
			t.Fatalf("selectPruneCandidates: %v", err)
		}
		if len(candidates) != 2 {
			t.Fatalf("selectPruneCandidates returned %d candidates, want 2", len(candidates))
		}

		// The provider is disabled in the window: its rows must be parked, not
		// pruned, even though they were valid candidates at select time.
		if _, err := testPool.Exec(ctx, `UPDATE providers SET enabled = false WHERE id = $1`, prov); err != nil {
			t.Fatalf("disable provider: %v", err)
		}

		pruned, err := repo.deletePruneCandidates(ctx, candidates, time.Now().Add(-30*24*time.Hour))
		if err != nil {
			t.Fatalf("deletePruneCandidates: %v", err)
		}
		if len(pruned) != 0 {
			t.Errorf("pruned %d rows for a provider disabled mid-flight, want 0", len(pruned))
		}
		if !modelExists(ctx, t, id1) || !modelExists(ctx, t, id2) {
			t.Error("rows of a provider disabled mid-flight were deleted, want both kept")
		}
	})
}

// TestPruneRetired_QueryErrorsSurface pins that a failing database call on any
// of the three prune steps comes back as an error (or, for the reconciliation
// read, as the full candidate list) rather than as a silent "nothing to do".
// A cancelled context is the cheapest way to make pgx refuse every query.
func TestPruneRetired_QueryErrorsSurface(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository(testPool)
	prov := insertTestProvider(ctx, t, "prune-errors")
	t.Cleanup(func() { cleanupProvider(ctx, t, prov) })
	id := insertPrunableModel(ctx, t, prov, "prune-errors-row")
	candidates := []PrunedModel{{ID: id, ProviderID: prov, ProviderName: "prune-errors", ModelID: "prune-errors-row"}}

	cancelled, cancel := context.WithCancel(ctx)
	cancel()

	if _, err := repo.selectPruneCandidates(cancelled, time.Now(), time.Now(), []uuid.UUID{prov}, 10); err == nil {
		t.Error("selectPruneCandidates: cancelled context returned no error")
	}
	if _, err := repo.PruneRetired(cancelled, time.Now(), time.Now(), []uuid.UUID{prov}, 10); err == nil {
		t.Error("PruneRetired: cancelled context returned no error")
	}
	if _, err := repo.deletePruneCandidates(cancelled, candidates, time.Now()); err == nil {
		t.Error("deletePruneCandidates: cancelled context returned no error")
	}
	if _, err := repo.aliveModelIDs(cancelled, []uuid.UUID{id}); err == nil {
		t.Error("aliveModelIDs: cancelled context returned no error")
	}
	if got, err := repo.deletePruneCandidates(ctx, nil, time.Now()); err != nil || got != nil {
		t.Errorf("deletePruneCandidates(nil) = %v, %v; want nil, nil", got, err)
	}
	if !modelExists(ctx, t, id) {
		t.Error("a refused query must not delete the row")
	}
}

// insertJournalTransition records one membership transition for (provider,
// model) in the discovery change journal at the given time: the raw evidence
// behind "this model flapped".
func insertJournalTransition(ctx context.Context, t *testing.T, providerID uuid.UUID, modelID string, at time.Time) {
	t.Helper()
	if _, err := testPool.Exec(ctx, `
		INSERT INTO discovery_changes (detected_at, source, provider_id, provider_name, diff, seen)
		VALUES ($1, 'test', $2, 'test', jsonb_build_object('disabled', jsonb_build_array(jsonb_build_object('model_id', $3::text))), true)`,
		at, providerID, modelID); err != nil {
		t.Fatalf("insert journal transition for %s: %v", modelID, err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM discovery_changes WHERE provider_id = $1`, providerID)
	})
}

// TestPruneRetired_FlapLandingMidFlightKeepsRow pins that flap eligibility is
// decided by the DELETE itself: a membership transition recorded after the
// candidates were selected still saves the row, because the same journal
// predicate runs inside the DELETE rather than against an earlier snapshot.
func TestPruneRetired_FlapLandingMidFlightKeepsRow(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository(testPool)
	prov := insertTestProvider(ctx, t, "prune-flap-race")
	t.Cleanup(func() { cleanupProvider(ctx, t, prov) })
	id := insertPrunableModel(ctx, t, prov, "prune-flap-race-row")
	since := time.Now().Add(-30 * 24 * time.Hour)

	candidates, err := repo.selectPruneCandidates(ctx, since, since, []uuid.UUID{prov}, 500)
	if err != nil || len(candidates) != 1 {
		t.Fatalf("selectPruneCandidates = %v, %v; want the one row", candidates, err)
	}

	// A scan lands between select and delete and journals a transition.
	insertJournalTransition(ctx, t, prov, "prune-flap-race-row", time.Now())

	pruned, err := repo.deletePruneCandidates(ctx, candidates, since)
	if err != nil {
		t.Fatalf("deletePruneCandidates: %v", err)
	}
	if len(pruned) != 0 {
		t.Errorf("pruned %v, want nothing: the row flapped after selection", pruned)
	}
	if !modelExists(ctx, t, id) {
		t.Error("the row was deleted although a transition landed before the DELETE")
	}
}
