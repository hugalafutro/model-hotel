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

// TestLockReconciledTables_CoversTheTablesThisTransactionReplaces runs the lock
// step the way apply runs it and reads back what the transaction actually holds.
// Two things matter and neither shows up in the call: that the statements name
// tables Postgres accepts, and that the set is exactly the tables this
// transaction reconciles. model_failover_groups is reconciled after the commit,
// in applyFailoverGroups' own transaction, so locking it here would block
// dashboard group edits for the length of the import and be released before the
// delete it looks like it guards.
func TestLockReconciledTables_CoversTheTablesThisTransactionReplaces(t *testing.T) {
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
	if err := lockReconciledTables(ctx, tx); err != nil {
		t.Fatalf("lockReconciledTables: %v", err)
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
	for _, table := range []string{"providers", "virtual_keys", "users"} {
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
