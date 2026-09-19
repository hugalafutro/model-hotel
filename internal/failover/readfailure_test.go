package failover

import (
	"context"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/db"
)

// TestSyncAllModels_GroupTableReadFailure drives the two read-failure branches
// of the sync with a deterministic seam: a repository over a one-connection
// pool with a short statement_timeout, and an ACCESS EXCLUSIVE lock on
// model_failover_groups held by another transaction. The first (unlocked) run
// prepares the statements; under the lock the existing-group lookup in
// upsertAutoGroup fails and is recorded, then the stale-group List fails and
// the sync returns that error instead of reporting success.
func TestSyncAllModels_GroupTableReadFailure(t *testing.T) {
	ctx := context.Background()
	u, err := url.Parse(testDBURL)
	if err != nil {
		t.Fatalf("parse test DB URL: %v", err)
	}
	q := u.Query()
	q.Set("statement_timeout", "250")
	u.RawQuery = q.Encode()
	slow, err := db.New(ctx, u.String(), 1, 1)
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	defer slow.Close()
	repo := NewRepository(slow.Pool())

	base := "readfail-" + uuid.New().String()[:8]
	var providerIDs []uuid.UUID
	for i := range 2 {
		pid, mid := uuid.New(), uuid.New()
		providerIDs = append(providerIDs, pid)
		if _, err := testDB.Pool().Exec(ctx, `
			INSERT INTO providers (id, name, base_url, encrypted_key, key_nonce, key_salt, enabled, created_at)
			VALUES ($1, $2, 'http://localhost:11434', 'dGVzdA==', 'dGVzdA==', 'dGVzdA==', true, now())`,
			pid, base+"-p"+string(rune('a'+i))); err != nil {
			t.Fatalf("insert provider: %v", err)
		}
		if _, err := testDB.Pool().Exec(ctx, `
			INSERT INTO models (id, model_id, provider_id, enabled, created_at) VALUES ($1, $2, $3, true, now())`,
			mid, base, pid); err != nil {
			t.Fatalf("insert model: %v", err)
		}
	}
	defer func() {
		for _, pid := range providerIDs {
			_, _ = testDB.Pool().Exec(ctx, "DELETE FROM providers WHERE id = $1", pid)
		}
		_, _ = testDB.Pool().Exec(ctx, "DELETE FROM model_failover_groups WHERE display_model = $1", base)
	}()

	if _, err := repo.SyncAllModels(ctx); err != nil {
		t.Fatalf("warm-up sync: %v", err)
	}
	InvalidateFailoverCache()

	holder, err := testDB.Pool().Begin(ctx)
	if err != nil {
		t.Fatalf("begin holder: %v", err)
	}
	defer func() { _ = holder.Rollback(ctx) }()
	if _, err := holder.Exec(ctx, "LOCK TABLE model_failover_groups IN ACCESS EXCLUSIVE MODE"); err != nil {
		t.Fatalf("lock: %v", err)
	}

	_, err = repo.SyncAllModels(ctx)
	if err == nil || !strings.Contains(err.Error(), "list failover groups") {
		t.Fatalf("sync under lock: err = %v, want the group-list read failure", err)
	}
}

// TestPruneStaleEntries_ModelReadFailure holds the contract that a failed read
// of the models table aborts the prune. Pruning treats "not in the result set"
// as "model deleted" and strips the member from every group it belongs to, so a
// read that returns fewer rows than the table holds would destroy live
// memberships that nothing puts back.
func TestPruneStaleEntries_ModelReadFailure(t *testing.T) {
	ctx := context.Background()
	u, err := url.Parse(testDBURL)
	if err != nil {
		t.Fatalf("parse test DB URL: %v", err)
	}
	q := u.Query()
	q.Set("statement_timeout", "250")
	u.RawQuery = q.Encode()
	slow, err := db.New(ctx, u.String(), 1, 1)
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	defer slow.Close()
	repo := NewRepository(slow.Pool())

	base := "prunefail-" + uuid.New().String()[:8]
	pid, keptModel, goneModel := uuid.New(), uuid.New(), uuid.New()
	if _, err := testDB.Pool().Exec(ctx, `
		INSERT INTO providers (id, name, base_url, encrypted_key, key_nonce, key_salt, enabled, created_at)
		VALUES ($1, $2, 'http://localhost:11434', 'dGVzdA==', 'dGVzdA==', 'dGVzdA==', true, now())`,
		pid, base+"-p"); err != nil {
		t.Fatalf("insert provider: %v", err)
	}
	defer func() {
		_, _ = testDB.Pool().Exec(ctx, "DELETE FROM providers WHERE id = $1", pid)
		_, _ = testDB.Pool().Exec(ctx, "DELETE FROM model_failover_groups WHERE display_model = $1", base)
	}()
	if _, err := testDB.Pool().Exec(ctx, `
		INSERT INTO models (id, model_id, provider_id, enabled, created_at) VALUES ($1, $2, $3, true, now())`,
		keptModel, base, pid); err != nil {
		t.Fatalf("insert model: %v", err)
	}

	// goneModel has no row at all, so a successful read would prune it and, with
	// one valid member left, delete the group outright.
	g, err := upsertGroup(ctx, t, repo, base, []uuid.UUID{keptModel, goneModel})
	if err != nil {
		t.Fatalf("create group: %v", err)
	}

	holder, err := testDB.Pool().Begin(ctx)
	if err != nil {
		t.Fatalf("begin holder: %v", err)
	}
	defer func() { _ = holder.Rollback(ctx) }()
	if _, err := holder.Exec(ctx, "LOCK TABLE models IN ACCESS EXCLUSIVE MODE"); err != nil {
		t.Fatalf("lock: %v", err)
	}

	result := &SyncResult{}
	repo.pruneStaleEntries(ctx, []*FailoverGroup{g}, result)

	if len(result.PurgedEntries) != 0 || len(result.DeletedGroups) != 0 {
		t.Errorf("prune acted on an unreadable models table: purged=%v deleted=%v",
			result.PurgedEntries, result.DeletedGroups)
	}
	_ = holder.Rollback(ctx)
	InvalidateFailoverCache()
	after, err := NewRepository(testDB.Pool()).GetByModel(ctx, base)
	if err != nil {
		t.Fatalf("re-read group: %v", err)
	}
	if len(after.PriorityOrder) != 2 {
		t.Errorf("membership changed: got %d entries, want 2", len(after.PriorityOrder))
	}
}

// TestDeleteUndersizedAutoGroup_RecordsDeleteFailure covers the delete seam the
// other way round: a failed DELETE used to be indistinguishable from "no auto
// group here", so a sync that could not reach the database reported a clean
// run. A closed pool is the cheapest deterministic failure, and the assertion
// is that the error reaches result.SyncErrors and nothing is reported deleted.
func TestDeleteUndersizedAutoGroup_RecordsDeleteFailure(t *testing.T) {
	ctx := context.Background()
	dead, err := db.New(ctx, testDBURL, 1, 1)
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	repo := NewRepository(dead.Pool())
	dead.Close()

	result := &SyncResult{}
	repo.deleteUndersizedAutoGroup(ctx, "gone-model", 1, []string{"p"}, result)

	if len(result.DeletedGroups) != 0 {
		t.Errorf("reported %d deleted group(s) for a delete that never ran", len(result.DeletedGroups))
	}
	if len(result.SyncErrors) != 1 || !strings.Contains(result.SyncErrors[0], "gone-model") {
		t.Errorf("SyncErrors = %v, want one entry naming the model whose delete failed", result.SyncErrors)
	}
}
