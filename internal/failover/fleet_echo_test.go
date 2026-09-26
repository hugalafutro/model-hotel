package failover

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

// fleetEchoRows counts the fleet auto-group echo row; see FleetAutoGroupsEchoKey.
func fleetEchoRows(t *testing.T) int {
	t.Helper()
	var n int
	if err := testDB.Pool().QueryRow(context.Background(),
		`SELECT count(*) FROM settings WHERE key = $1`, FleetAutoGroupsEchoKey).Scan(&n); err != nil {
		t.Fatalf("count echo: %v", err)
	}
	return n
}

// echoTestBase names a fresh base model for one echo test and removes the auto
// group it forms when the test ends: an orphaned auto group would be deleted by
// the next SyncAllModels in this process and miscount another test's deletions.
func echoTestBase(t *testing.T, prefix string) string {
	t.Helper()
	base := prefix + uuid.New().String()[:8]
	t.Cleanup(func() {
		_, _ = testDB.Pool().Exec(context.Background(), `DELETE FROM model_failover_groups WHERE display_model = $1`, base)
		InvalidateFailoverCache()
	})
	return base
}

func seedFleetEcho(t *testing.T) {
	t.Helper()
	if _, err := testDB.Pool().Exec(context.Background(),
		`INSERT INTO settings (key, value, updated_at) VALUES ($1, '[]', now())
		 ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`, FleetAutoGroupsEchoKey); err != nil {
		t.Fatalf("seed echo: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testDB.Pool().Exec(context.Background(), `DELETE FROM settings WHERE key = $1`, FleetAutoGroupsEchoKey)
	})
}

// A scan drops the fleet echo only when it changes an auto group: forming one,
// changing its membership, or deleting an undersized one. A scan that finds
// everything as it was leaves the echo alone, so a fleet at rest is not
// re-imported on every scan.
func TestRepository_SyncAllModels_ClearsFleetEchoOnlyOnChange(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	base := echoTestBase(t, "echo-sync-")
	_, m1 := seedProviderModel(ctx, t, base, true, true)
	_, m2 := seedProviderModel(ctx, t, base, true, true)

	seedFleetEcho(t)
	if _, err := repo.SyncAllModels(ctx); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if fleetEchoRows(t) != 0 {
		t.Fatal("forming an auto group must drop the echo")
	}

	seedFleetEcho(t)
	if _, err := repo.SyncAllModels(ctx); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if fleetEchoRows(t) != 1 {
		t.Fatal("a scan that changes nothing must leave the echo alone")
	}

	seedProviderModel(ctx, t, base, true, true)
	if _, err := repo.SyncAllModels(ctx); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if fleetEchoRows(t) != 0 {
		t.Fatal("a new member joining an auto group must drop the echo")
	}

	// Undersize it and resync just this base (a whole-table scan here would race
	// the other test shards that share the database and count our deletion).
	seedFleetEcho(t)
	for _, id := range []uuid.UUID{m1, m2} {
		if _, err := testDB.Pool().Exec(ctx, `UPDATE models SET enabled = false WHERE id = $1`, id); err != nil {
			t.Fatalf("disable model: %v", err)
		}
	}
	if _, err := repo.SyncForModel(ctx, base); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if fleetEchoRows(t) != 0 {
		t.Fatal("deleting an undersized auto group must drop the echo")
	}
}

// The per-model resync follows the same rule on both of its paths.
func TestRepository_SyncForModel_ClearsFleetEcho(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	base := echoTestBase(t, "echo-one-")
	_, m1 := seedProviderModel(ctx, t, base, true, true)
	seedProviderModel(ctx, t, base, true, true)

	seedFleetEcho(t)
	if _, err := repo.SyncForModel(ctx, base); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if fleetEchoRows(t) != 0 {
		t.Fatal("forming the group must drop the echo")
	}

	seedFleetEcho(t)
	if _, err := repo.SyncForModel(ctx, base); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if fleetEchoRows(t) != 1 {
		t.Fatal("an unchanged group must leave the echo alone")
	}

	if _, err := testDB.Pool().Exec(ctx, `UPDATE models SET enabled = false WHERE id = $1`, m1); err != nil {
		t.Fatalf("disable model: %v", err)
	}
	if _, err := repo.SyncForModel(ctx, base); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if fleetEchoRows(t) != 0 {
		t.Fatal("deleting the undersized group must drop the echo")
	}

	// Best-effort: a failed clear is logged, never returned or panicked on.
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	repo.ClearFleetAutoEcho(canceled)
}

// A scan re-enables an auto group the primary sent disabled (upsertAutoGroup
// always writes group_enabled true), so that flip counts as a change and drops
// the echo, on both scan paths.
func TestRepository_Sync_ReenablingADisabledAutoGroupClearsFleetEcho(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	base := echoTestBase(t, "echo-off-")
	seedProviderModel(ctx, t, base, true, true)
	seedProviderModel(ctx, t, base, true, true)
	if _, err := repo.SyncAllModels(ctx); err != nil {
		t.Fatalf("sync: %v", err)
	}
	disable := func() {
		t.Helper()
		if _, err := testDB.Pool().Exec(ctx, `UPDATE model_failover_groups SET group_enabled = false WHERE display_model = $1`, base); err != nil {
			t.Fatalf("disable group: %v", err)
		}
		InvalidateFailoverCache()
	}

	disable()
	seedFleetEcho(t)
	if _, err := repo.SyncAllModels(ctx); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if fleetEchoRows(t) != 0 {
		t.Fatal("SyncAllModels re-enabling a disabled auto group must drop the echo")
	}

	disable()
	seedFleetEcho(t)
	if _, err := repo.SyncForModel(ctx, base); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if fleetEchoRows(t) != 0 {
		t.Fatal("SyncForModel re-enabling a disabled auto group must drop the echo")
	}
}

// A clear that failed is retried by the next scan even when that scan changes
// nothing, so a stale echo cannot outlive the change it hides.
func TestRepository_Sync_RetriesAFailedFleetEchoClear(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	base := echoTestBase(t, "echo-retry-")
	seedProviderModel(ctx, t, base, true, true)
	seedProviderModel(ctx, t, base, true, true)
	if _, err := repo.SyncAllModels(ctx); err != nil {
		t.Fatalf("sync: %v", err)
	}

	seedFleetEcho(t)
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	repo.ClearFleetAutoEcho(canceled) // fails: the echo stays, the retry is owed
	if fleetEchoRows(t) != 1 {
		t.Fatal("precondition: the failed clear must leave the echo in place")
	}
	if _, err := repo.SyncAllModels(ctx); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if fleetEchoRows(t) != 0 {
		t.Fatal("an unchanged scan must retry the owed clear")
	}

	seedFleetEcho(t)
	repo.ClearFleetAutoEcho(canceled)
	if _, err := repo.SyncForModel(ctx, base); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if fleetEchoRows(t) != 0 {
		t.Fatal("an unchanged per-model sync must retry the owed clear")
	}
}
