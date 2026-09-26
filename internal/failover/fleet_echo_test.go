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
	base := "echo-sync-" + uuid.New().String()[:8]
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

	seedFleetEcho(t)
	for _, id := range []uuid.UUID{m1, m2} {
		if _, err := testDB.Pool().Exec(ctx, `UPDATE models SET enabled = false WHERE id = $1`, id); err != nil {
			t.Fatalf("disable model: %v", err)
		}
	}
	if _, err := repo.SyncAllModels(ctx); err != nil {
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
	base := "echo-one-" + uuid.New().String()[:8]
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
