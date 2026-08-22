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
	}
	ids := map[string]uuid.UUID{}
	for _, r := range rows {
		id := uuid.New()
		ids[r.name] = id
		var dismissed *time.Time
		if r.dismissed {
			dismissed = &now
		}
		if _, err := testPool.Exec(ctx, `
			INSERT INTO models (id, provider_id, model_id, name, enabled, disabled_manually,
			                    manually_enabled_at, auto_retired_at, last_seen_at, discovery_dismissed_at)
			VALUES ($1, $2, $3, $3, $4, $5, $6, $7, $8, $9)`,
			id, r.provider, r.name, r.enabled, r.manual, r.pinnedAt, r.retiredAt, r.lastSeen, dismissed); err != nil {
			t.Fatalf("insert %s: %v", r.name, err)
		}
	}

	pruned, err := repo.PruneRetired(ctx, time.Now().Add(-30*24*time.Hour),
		[]uuid.UUID{onProvider, offProvider},
		map[ProviderModelKey]bool{{ProviderID: onProvider, ModelID: "prune-flapped"}: true},
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
	want := map[string]bool{"prune-prunable": true, "prune-prunable-dismissed": true}
	for _, r := range rows {
		if got[r.name] != want[r.name] {
			t.Errorf("%s: pruned=%v, want %v", r.name, got[r.name], want[r.name])
		}
	}
	// The table agrees with the return value.
	var remaining int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM models WHERE provider_id = ANY($1)`,
		[]uuid.UUID{onProvider, offProvider, otherProvider}).Scan(&remaining); err != nil {
		t.Fatalf("count: %v", err)
	}
	if remaining != len(rows)-2 {
		t.Errorf("remaining rows = %d, want %d", remaining, len(rows)-2)
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
	pruned, err := repo.PruneRetired(ctx, time.Now().Add(-30*24*time.Hour), []uuid.UUID{prov}, nil, 2)
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
	pruned, err := repo.PruneRetired(ctx, time.Now(), nil, nil, 500)
	if err != nil {
		t.Fatalf("PruneRetired: %v", err)
	}
	if len(pruned) != 0 {
		t.Errorf("pruned %d rows with no providers in scope, want 0", len(pruned))
	}
}
