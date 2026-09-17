package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// TestLockTables_CoversTheTablesThisTransactionReplaces runs the lock
// steps the way apply runs them and reads back what the transaction actually holds.
// Two things matter and neither shows up in the call: that the statements name
// tables Postgres accepts, and that the set is exactly the tables this
// transaction reconciles. model_failover_groups is reconciled after the commit,
// in applyFailoverGroups' own transaction, so locking it here would block
// dashboard group edits for the length of the import and be released before the
// delete it looks like it guards.
func TestLockTables_CoversTheTablesThisTransactionReplaces(t *testing.T) {
	ctx := context.Background()
	tx, err := apiTestDB.Pool().Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// apply sets this before the fence, so mirror it here: the LOCK TABLE waits
	// are bounded by nothing else.
	if _, err := tx.Exec(ctx, `SET LOCAL lock_timeout = '`+reconcileLockTimeout+`'`); err != nil {
		t.Fatalf("set lock_timeout: %v", err)
	}
	// Both stages apply takes, so the set read back is the one the transaction
	// holds by the time its last rail runs.
	if err := lockTables(ctx, tx, "providers", "virtual_keys", "users"); err != nil {
		t.Fatalf("lockTables: %v", err)
	}
	if err := lockTables(ctx, tx, "settings"); err != nil {
		t.Fatalf("lockTables(settings): %v", err)
	}

	var timeout string
	if err := tx.QueryRow(ctx, `SELECT current_setting('lock_timeout')`).Scan(&timeout); err != nil {
		t.Fatalf("read lock_timeout: %v", err)
	}
	if timeout != reconcileLockTimeout {
		t.Errorf("lock_timeout inside the transaction = %q, want %q", timeout, reconcileLockTimeout)
	}

	rows, err := tx.Query(ctx, `
		SELECT c.relname
		FROM pg_locks l JOIN pg_class c ON c.oid = l.relation
		WHERE l.pid = pg_backend_pid() AND l.locktype = 'relation'
		  AND l.mode = 'ShareRowExclusiveLock' AND l.granted`)
	if err != nil {
		t.Fatalf("query pg_locks: %v", err)
	}
	held, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatalf("collect pg_locks: %v", err)
	}
	got := map[string]bool{}
	for _, name := range held {
		got[name] = true
	}
	for _, table := range []string{"providers", "virtual_keys", "users", "settings"} {
		if !got[table] {
			t.Errorf("%s is reconciled in this transaction but not locked (held: %v)", table, held)
		}
	}
	if got["model_failover_groups"] {
		t.Error("model_failover_groups is reconciled after the commit, so locking it here only blocks the dashboard")
	}
}

// TestConfigSync_AdvisoryLockWaitIsBounded holds the fence advisory lock from
// another connection and gives the import a request deadline far longer than
// lock_timeout. The import has to give up on its own, at the timeout, rather
// than sitting on the lock until the request context expires: import against
// import is the one wait guaranteed to contend, so it is the wait the timeout
// has to cover, which it only does if it is set before the fence takes the lock.
func TestConfigSync_AdvisoryLockWaitIsBounded(t *testing.T) {
	cleanConfigTables(t)
	seedProvider(t, "openai", "sk-secret-value", configSyncMasterKey)
	r := newConfigSyncRouter(t, configSyncMasterKey)
	base := doExport(t, r)

	holder, err := apiTestDB.Pool().Acquire(context.Background())
	if err != nil {
		t.Fatalf("acquire holder conn: %v", err)
	}
	defer holder.Release()
	if _, err := holder.Exec(context.Background(), `SELECT pg_advisory_lock($1)`, fleetSourceGenLock); err != nil {
		t.Fatalf("hold advisory lock: %v", err)
	}
	defer func() { _, _ = holder.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, fleetSourceGenLock) }()

	deadline := 30 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), deadline)
	defer cancel()
	body, _ := json.Marshal(withExtraProvider(base, "extra"))
	req := httptest.NewRequest(http.MethodPost, "/config/import", bytes.NewReader(body)).WithContext(ctx)
	rec := httptest.NewRecorder()

	start := time.Now()
	r.ServeHTTP(rec, req)
	elapsed := time.Since(start)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("import blocked on the fence lock = %d, want 500 (failed closed)", rec.Code)
	}
	// Generous: the timeout is 5s and the assertion only has to separate "the
	// statement timed out" from "the request context did".
	if elapsed > deadline/2 {
		t.Errorf("import waited %v on the fence lock, want it bounded by lock_timeout (%s)", elapsed, reconcileLockTimeout)
	}
	if providerNames(t)["extra"] {
		t.Error("an import that gave up on the fence lock must not apply its config")
	}
}

// TestConfigSync_ImportTakesSettingsAfterModels pins the lock order between the
// import and the per-model reconcile (applyModelIntent), which a previous
// import's post-commit phase can still be running when the next push lands.
// That reconcile holds models rows first and writes settings second. An import
// that removes a provider reaches models through the delete's cascade, so it
// must take the settings lock only after that point: taken first, the two
// transactions hold what the other needs and Postgres aborts one (SQLSTATE
// 40P01), failing the push for nothing but its own ordering.
//
// The reconcile is stood in for by a raw transaction taking the same two steps,
// released once the import is parked on the row it holds; both must then
// finish, which neither does when the orders oppose.
func TestConfigSync_ImportTakesSettingsAfterModels(t *testing.T) {
	cleanConfigTables(t)
	dropID := seedProvider(t, "dropme", "sk-drop-value", configSyncMasterKey)
	seedProvider(t, "openai", "sk-secret-value", configSyncMasterKey)
	seedModel(t, dropID, "m1")
	r := newConfigSyncRouter(t, configSyncMasterKey)
	env := doExport(t, r)
	kept := env.Config.Providers[:0:0]
	for _, p := range env.Config.Providers {
		if p.Name != "dropme" {
			kept = append(kept, p)
		}
	}
	env.Config.Providers = kept
	body, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}

	ctx := context.Background()
	reconcile, err := apiTestDB.Pool().Begin(ctx)
	if err != nil {
		t.Fatalf("begin reconcile: %v", err)
	}
	defer func() { _ = reconcile.Rollback(ctx) }()
	if _, err := reconcile.Exec(ctx, `UPDATE models SET enabled = false WHERE model_id = 'm1'`); err != nil {
		t.Fatalf("reconcile holds the models row: %v", err)
	}

	done := make(chan int, 1)
	go func() {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/config/import", bytes.NewReader(body)))
		done <- rec.Code
	}()

	// The import's provider delete cascades into the row the reconcile holds.
	parked := false
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline) && !parked; time.Sleep(20 * time.Millisecond) {
		var n int
		err := apiTestDB.Pool().QueryRow(ctx, `
			SELECT count(*) FROM pg_stat_activity
			WHERE pid <> pg_backend_pid() AND wait_event_type = 'Lock' AND query LIKE 'DELETE FROM providers%'`).Scan(&n)
		parked = err == nil && n > 0
	}
	if !parked {
		t.Fatalf("import never parked on the models row the reconcile holds")
	}

	// The reconcile's second step. Under the opposite order the import already
	// holds settings while waiting on models, and this is where the cycle closes.
	if _, err := reconcile.Exec(ctx, `
		INSERT INTO settings (key, value, updated_at) VALUES ('_test_model_reconcile', '[]', now())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()`); err != nil {
		t.Fatalf("reconcile's settings write against a parked import: %v", err)
	}
	if err := reconcile.Commit(ctx); err != nil {
		t.Fatalf("commit reconcile: %v", err)
	}
	if code := <-done; code != http.StatusOK {
		t.Fatalf("import after the reconcile released its row = %d, want 200", code)
	}
	if providerNames(t)["dropme"] {
		t.Error("the import committed but did not drop the provider the envelope omits")
	}
}

// TestConfigSync_SettingsLockWaitRollsBackTheImport holds the settings table
// from another session. The import takes that lock only after its provider
// delete has run, so giving up on it has to roll that delete back with the
// rest of the transaction: the member keeps the provider the envelope omitted,
// and the wait is bounded by lock_timeout, not the request deadline.
func TestConfigSync_SettingsLockWaitRollsBackTheImport(t *testing.T) {
	cleanConfigTables(t)
	seedProvider(t, "dropme", "sk-drop-value", configSyncMasterKey)
	seedProvider(t, "openai", "sk-secret-value", configSyncMasterKey)
	r := newConfigSyncRouter(t, configSyncMasterKey)
	env := doExport(t, r)
	kept := env.Config.Providers[:0:0]
	for _, p := range env.Config.Providers {
		if p.Name != "dropme" {
			kept = append(kept, p)
		}
	}
	env.Config.Providers = kept

	ctx := context.Background()
	holder, err := apiTestDB.Pool().Begin(ctx)
	if err != nil {
		t.Fatalf("begin holder: %v", err)
	}
	defer func() { _ = holder.Rollback(ctx) }()
	if _, err := holder.Exec(ctx, `LOCK TABLE settings IN SHARE ROW EXCLUSIVE MODE`); err != nil {
		t.Fatalf("hold settings: %v", err)
	}

	deadline := 30 * time.Second
	reqCtx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()
	body, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	rec := httptest.NewRecorder()
	start := time.Now()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/config/import", bytes.NewReader(body)).WithContext(reqCtx))
	elapsed := time.Since(start)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("import blocked on the settings lock = %d, want 500 (failed closed)", rec.Code)
	}
	if elapsed > deadline/2 {
		t.Errorf("import waited %v on the settings lock, want it bounded by lock_timeout (%s)", elapsed, reconcileLockTimeout)
	}
	if !providerNames(t)["dropme"] {
		t.Error("an import that gave up on the settings lock must roll back the provider delete it already ran")
	}
}
