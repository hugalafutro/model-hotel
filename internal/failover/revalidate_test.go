package failover

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

// seedProviderModel inserts a provider and a single model under it (sharing the
// given base model name across calls), registering cleanup. The enabled flags
// let a test simulate discovery disabling a model or an operator disabling a
// provider.
func seedProviderModel(ctx context.Context, t *testing.T, baseModel string, providerEnabled, modelEnabled bool) (providerID, modelID uuid.UUID) {
	t.Helper()
	providerID = uuid.New()
	modelID = uuid.New()
	name := "rv-prov-" + uuid.New().String()[:8]
	_, err := testDB.Pool().Exec(ctx, `
		INSERT INTO providers (id, name, base_url, encrypted_key, key_nonce, key_salt, enabled, created_at)
		VALUES ($1, $2, 'http://localhost:11434', 'dGVzdA==', 'dGVzdA==', 'dGVzdA==', $3, now())
	`, providerID, name, providerEnabled)
	if err != nil {
		t.Fatalf("insert provider: %v", err)
	}
	t.Cleanup(func() { _, _ = testDB.Pool().Exec(ctx, "DELETE FROM providers WHERE id = $1", providerID) })

	_, err = testDB.Pool().Exec(ctx, `
		INSERT INTO models (id, model_id, provider_id, enabled, created_at)
		VALUES ($1, $2, $3, $4, now())
	`, modelID, baseModel, providerID, modelEnabled)
	if err != nil {
		t.Fatalf("insert model: %v", err)
	}
	t.Cleanup(func() { _, _ = testDB.Pool().Exec(ctx, "DELETE FROM models WHERE id = $1", modelID) })
	return providerID, modelID
}

// newCustomGroup creates an enabled, hand-built (auto_created=false) failover
// group with all members toggled on, registering cleanup.
func newCustomGroup(ctx context.Context, t *testing.T, repo *Repository, base string, order []uuid.UUID) *FailoverGroup {
	t.Helper()
	entryEnabled := make(map[string]bool, len(order))
	for _, id := range order {
		entryEnabled[id.String()] = true
	}
	groupEnabled := true
	autoCreated := false
	g, err := repo.UpsertWithConfig(ctx, base, order, entryEnabled, &groupEnabled, nil, nil, &autoCreated)
	if err != nil {
		t.Fatalf("create custom group: %v", err)
	}
	t.Cleanup(func() { _, _ = testDB.Pool().Exec(ctx, "DELETE FROM model_failover_groups WHERE id = $1", g.ID) })
	return g
}

// A custom 2-member group whose second member's model was disabled (e.g. by
// discovery dropping a model the provider stopped listing) must be auto-disabled
// — its model row still exists, so pruneStaleEntries leaves it, and routing
// would silently degrade to a single live member without this guard.
func TestRepository_RevalidateCustomGroups_AutoDisablesUndersized(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	base := "rv-" + uuid.New().String()[:8]

	_, m1 := seedProviderModel(ctx, t, base, true, true)
	_, m2 := seedProviderModel(ctx, t, base, true, false) // model disabled by "discovery"
	g := newCustomGroup(ctx, t, repo, base, []uuid.UUID{m1, m2})

	res, err := repo.RevalidateCustomGroups(ctx)
	if err != nil {
		t.Fatalf("revalidate: %v", err)
	}
	if len(res.DisabledGroups) != 1 {
		t.Fatalf("expected 1 disabled group, got %d (%+v)", len(res.DisabledGroups), res.DisabledGroups)
	}
	if res.DisabledGroups[0].DisplayModel != base {
		t.Errorf("expected disabled group %q, got %q", base, res.DisabledGroups[0].DisplayModel)
	}
	if res.DisabledGroups[0].EffectiveCount != 1 {
		t.Errorf("expected effective count 1, got %d", res.DisabledGroups[0].EffectiveCount)
	}

	InvalidateFailoverCache()
	got, err := repo.GetByID(ctx, g.ID)
	if err != nil {
		t.Fatalf("get group: %v", err)
	}
	if got.GroupEnabled {
		t.Error("expected group to be auto-disabled (group_enabled=false)")
	}

	// Idempotent: a second pass finds the group already disabled and reports nothing.
	res2, err := repo.RevalidateCustomGroups(ctx)
	if err != nil {
		t.Fatalf("revalidate (2nd): %v", err)
	}
	if len(res2.DisabledGroups) != 0 {
		t.Errorf("expected 0 disabled groups on 2nd pass, got %d", len(res2.DisabledGroups))
	}
}

// A disabled provider makes its member unroutable just like a disabled model.
func TestRepository_RevalidateCustomGroups_DisabledProviderCountsUnroutable(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	base := "rv-" + uuid.New().String()[:8]

	_, m1 := seedProviderModel(ctx, t, base, true, true)
	_, m2 := seedProviderModel(ctx, t, base, false, true) // provider disabled
	g := newCustomGroup(ctx, t, repo, base, []uuid.UUID{m1, m2})

	res, err := repo.RevalidateCustomGroups(ctx)
	if err != nil {
		t.Fatalf("revalidate: %v", err)
	}
	if len(res.DisabledGroups) != 1 {
		t.Fatalf("expected 1 disabled group, got %d", len(res.DisabledGroups))
	}

	InvalidateFailoverCache()
	got, err := repo.GetByID(ctx, g.ID)
	if err != nil {
		t.Fatalf("get group: %v", err)
	}
	if got.GroupEnabled {
		t.Error("expected group to be auto-disabled")
	}
}

// A custom group whose members are all routable stays enabled and is not reported.
func TestRepository_RevalidateCustomGroups_ViableGroupUntouched(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	base := "rv-" + uuid.New().String()[:8]

	_, m1 := seedProviderModel(ctx, t, base, true, true)
	_, m2 := seedProviderModel(ctx, t, base, true, true)
	g := newCustomGroup(ctx, t, repo, base, []uuid.UUID{m1, m2})

	res, err := repo.RevalidateCustomGroups(ctx)
	if err != nil {
		t.Fatalf("revalidate: %v", err)
	}
	if len(res.DisabledGroups) != 0 {
		t.Fatalf("expected 0 disabled groups, got %d", len(res.DisabledGroups))
	}

	InvalidateFailoverCache()
	got, err := repo.GetByID(ctx, g.ID)
	if err != nil {
		t.Fatalf("get group: %v", err)
	}
	if !got.GroupEnabled {
		t.Error("expected viable group to stay enabled")
	}
}

// Auto-created groups are owned by the sync rebuild/delete path, so revalidation
// must leave them alone even when undersized.
func TestRepository_RevalidateCustomGroups_SkipsAutoGroups(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	base := "rv-" + uuid.New().String()[:8]

	_, m1 := seedProviderModel(ctx, t, base, true, true)
	_, m2 := seedProviderModel(ctx, t, base, true, false) // disabled member

	entryEnabled := map[string]bool{m1.String(): true, m2.String(): true}
	groupEnabled := true
	autoCreated := true
	g, err := repo.UpsertWithConfig(ctx, base, []uuid.UUID{m1, m2}, entryEnabled, &groupEnabled, nil, nil, &autoCreated)
	if err != nil {
		t.Fatalf("create auto group: %v", err)
	}
	t.Cleanup(func() { _, _ = testDB.Pool().Exec(ctx, "DELETE FROM model_failover_groups WHERE id = $1", g.ID) })

	res, err := repo.RevalidateCustomGroups(ctx)
	if err != nil {
		t.Fatalf("revalidate: %v", err)
	}
	if len(res.DisabledGroups) != 0 {
		t.Errorf("expected revalidation to skip auto groups, got %d disabled", len(res.DisabledGroups))
	}
}

// SyncAllModels (the manual "Sync" button path) must run revalidation: a custom
// group whose member is disabled after creation gets auto-disabled on sync.
func TestRepository_SyncAllModels_AutoDisablesUndersizedCustomGroup(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	base := "rv-" + uuid.New().String()[:8]

	_, m1 := seedProviderModel(ctx, t, base, true, true)
	_, m2 := seedProviderModel(ctx, t, base, true, true)
	g := newCustomGroup(ctx, t, repo, base, []uuid.UUID{m1, m2})

	// Disable one member's model, as discovery would for a vanished model.
	if _, err := testDB.Pool().Exec(ctx, "UPDATE models SET enabled = false WHERE id = $1", m2); err != nil {
		t.Fatalf("disable model: %v", err)
	}

	res, err := repo.SyncAllModels(ctx)
	if err != nil {
		t.Fatalf("SyncAllModels: %v", err)
	}

	found := false
	for _, dg := range res.DisabledGroups {
		if dg.DisplayModel == base {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected SyncAllModels to report %q in DisabledGroups, got %+v", base, res.DisabledGroups)
	}

	InvalidateFailoverCache()
	got, err := repo.GetByID(ctx, g.ID)
	if err != nil {
		t.Fatalf("get group: %v", err)
	}
	if got.GroupEnabled {
		t.Error("expected group to be auto-disabled after SyncAllModels")
	}
}
