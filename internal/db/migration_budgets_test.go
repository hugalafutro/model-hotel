package db

import (
	"context"
	"io/fs"
	"testing"

	"github.com/google/uuid"
)

// TestBudgetsMigration pins the one data step in 087: a keyed request-log row
// written before the upgrade carried no owner, and the migration stamps the
// key's current owner onto it so a user's first budget period is not
// undercounted. A row whose key has no owner, and one already stamped, are
// left alone. The schema steps are idempotent, so the file re-runs cleanly.
func TestBudgetsMigration(t *testing.T) {
	b, err := fs.ReadFile(embeddedMigrations, "migrations/087_budgets.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	ctx := context.Background()
	suffix := uuid.NewString()[:8]
	var ownerID, otherID uuid.UUID
	if err := testPool.QueryRow(ctx, `INSERT INTO users (username, password_hash) VALUES ($1, 'x') RETURNING id`, "budget-mig-"+suffix).Scan(&ownerID); err != nil {
		t.Fatalf("seed owner: %v", err)
	}
	if err := testPool.QueryRow(ctx, `INSERT INTO users (username, password_hash) VALUES ($1, 'x') RETURNING id`, "budget-mig-other-"+suffix).Scan(&otherID); err != nil {
		t.Fatalf("seed other: %v", err)
	}
	var ownedKey, looseKey uuid.UUID
	if err := testPool.QueryRow(ctx, `INSERT INTO virtual_keys (name, key_hash, key_preview, owner_user_id) VALUES ($1, $2, 'sk-..bm', $3) RETURNING id`,
		"budget-mig-owned-"+suffix, "hash-budget-mig-owned-"+suffix, ownerID).Scan(&ownedKey); err != nil {
		t.Fatalf("seed owned key: %v", err)
	}
	if err := testPool.QueryRow(ctx, `INSERT INTO virtual_keys (name, key_hash, key_preview) VALUES ($1, $2, 'sk-..bl') RETURNING id`,
		"budget-mig-loose-"+suffix, "hash-budget-mig-loose-"+suffix).Scan(&looseKey); err != nil {
		t.Fatalf("seed loose key: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM request_logs WHERE model_id = $1`, "budget-mig-"+suffix)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM virtual_keys WHERE id IN ($1, $2)`, ownedKey, looseKey)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM users WHERE id IN ($1, $2)`, ownerID, otherID)
	})
	rows := map[string]uuid.UUID{"owned-unstamped": uuid.New(), "owned-stamped": uuid.New(), "loose": uuid.New()}
	for name, id := range rows {
		var vk, owner any
		switch name {
		case "owned-unstamped":
			vk = ownedKey
		case "owned-stamped":
			vk, owner = ownedKey, otherID // stamped by an older owner at request time: kept
		case "loose":
			vk = looseKey
		}
		if _, err := testPool.Exec(ctx, `INSERT INTO request_logs (id, model_id, status_code, duration_ms, virtual_key_id, owner_user_id, created_at)
			VALUES ($1, $2, 200, 1, $3, $4, NOW())`, id, "budget-mig-"+suffix, vk, owner); err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
	}

	if _, err := testPool.Exec(ctx, string(b)); err != nil {
		t.Fatalf("re-run migration: %v", err)
	}

	want := map[string]*uuid.UUID{"owned-unstamped": &ownerID, "owned-stamped": &otherID, "loose": nil}
	for name, id := range rows {
		var got *uuid.UUID
		if err := testPool.QueryRow(ctx, `SELECT owner_user_id FROM request_logs WHERE id = $1`, id).Scan(&got); err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		switch {
		case want[name] == nil && got != nil:
			t.Errorf("%s: owner = %v, want NULL", name, *got)
		case want[name] != nil && (got == nil || *got != *want[name]):
			t.Errorf("%s: owner = %v, want %v", name, got, *want[name])
		}
	}
}
