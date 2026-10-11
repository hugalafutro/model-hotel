package quota_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/db"
	"github.com/hugalafutro/model-hotel/internal/quota"
)

// TestReplaceReportsThePrior: Replace hands back the status and source of the
// row it overwrote, a zero Prior for no row and for RecordFailure's
// placeholder, so the caller can tell a flip from a first answer and a
// fleet-fed node from one that fetches for itself.
func TestReplaceReportsThePrior(t *testing.T) {
	ctx := context.Background()
	repo := quota.NewRepository(testPool)
	provID := insertTestProvider(ctx, t, "test-quota-replace")
	t.Cleanup(func() { cleanupProvider(ctx, t, provID) })

	snap := func(status int, source string) quota.Snapshot {
		return quota.Snapshot{ProviderID: provID, Kind: "usage", Payload: json.RawMessage(`null`), HTTPStatus: status, Source: source}
	}
	if prior, err := repo.Replace(ctx, snap(204, "fleet")); err != nil || prior != (quota.Prior{}) {
		t.Fatalf("first write: want zero prior, got %+v (err %v)", prior, err)
	}
	if prior, err := repo.Replace(ctx, snap(200, "poll")); err != nil || prior != (quota.Prior{HTTPStatus: 204, Source: "fleet"}) {
		t.Fatalf("second write: want prior 204/fleet, got %+v (err %v)", prior, err)
	}
	if prior, err := repo.Replace(ctx, snap(200, "poll")); err != nil || prior != (quota.Prior{HTTPStatus: 200, Source: "poll"}) {
		t.Fatalf("same status: want prior 200/poll, got %+v (err %v)", prior, err)
	}

	other := insertTestProvider(ctx, t, "test-quota-replace-placeholder")
	t.Cleanup(func() { cleanupProvider(ctx, t, other) })
	if err := repo.RecordFailure(ctx, other, "usage", "timeout"); err != nil {
		t.Fatalf("record failure: %v", err)
	}
	s := snap(200, "poll")
	s.ProviderID = other
	if prior, err := repo.Replace(ctx, s); err != nil || prior.HTTPStatus != 0 {
		t.Fatalf("after a failure placeholder: want status 0, got %+v (err %v)", prior, err)
	}
}

// TestReplaceIfNewerReportsThePrior: the fleet write reports the stored row
// whether or not it wrote, and wrote keeps the strictly-newer rule.
func TestReplaceIfNewerReportsThePrior(t *testing.T) {
	ctx := context.Background()
	repo := quota.NewRepository(testPool)
	provID := insertTestProvider(ctx, t, "test-quota-replace-newer")
	t.Cleanup(func() { cleanupProvider(ctx, t, provID) })

	older := time.Now().Add(-time.Hour).UTC()
	snap := func(status int, at time.Time) quota.Snapshot {
		return quota.Snapshot{ProviderID: provID, Kind: "usage", Payload: json.RawMessage(`{}`), HTTPStatus: status, Source: "fleet", FetchedAt: at}
	}
	wrote, prior, err := repo.ReplaceIfNewer(ctx, snap(204, older))
	if err != nil || !wrote || prior != (quota.Prior{}) {
		t.Fatalf("first write: want wrote=true zero prior, got wrote=%v prior=%+v err=%v", wrote, prior, err)
	}
	wrote, prior, err = repo.ReplaceIfNewer(ctx, snap(200, older.Add(time.Minute)))
	if err != nil || !wrote || prior.HTTPStatus != 204 {
		t.Fatalf("newer write: want wrote=true prior 204, got wrote=%v prior=%+v err=%v", wrote, prior, err)
	}
	wrote, prior, err = repo.ReplaceIfNewer(ctx, snap(204, older))
	if err != nil || wrote || prior.HTTPStatus != 200 {
		t.Fatalf("older write: want wrote=false prior 200, got wrote=%v prior=%+v err=%v", wrote, prior, err)
	}
}

// TestReplaceSerialisesConcurrentWriters pins the lock. A holder transaction
// takes the provider+kind advisory lock and changes the row to 204 without
// committing; a concurrent Replace(200) must wait for it and report 204, the
// row as the holder left it. Without the lock it would read the committed 200
// and the flip to 204 would never be announced. The same lock also covers the
// first write, where FOR UPDATE on the (absent) row would lock nothing.
func TestReplaceSerialisesConcurrentWriters(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	repo := quota.NewRepository(testPool)
	provID := insertTestProvider(ctx, t, "test-quota-replace-race")
	t.Cleanup(func() { cleanupProvider(context.Background(), t, provID) })
	snap := func(status int) quota.Snapshot {
		return quota.Snapshot{ProviderID: provID, Kind: "usage", Payload: json.RawMessage(`{}`), HTTPStatus: status, Source: "poll"}
	}
	if _, err := repo.Replace(ctx, snap(200)); err != nil {
		t.Fatalf("seed: %v", err)
	}

	holder, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin holder: %v", err)
	}
	t.Cleanup(func() { _ = holder.Rollback(context.Background()) })
	if _, err := holder.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1::text || '/' || $2))`, provID, "usage"); err != nil {
		t.Fatalf("holder lock: %v", err)
	}
	if _, err := holder.Exec(ctx, `UPDATE provider_quota_snapshots SET http_status = 204 WHERE provider_id = $1 AND kind = 'usage'`, provID); err != nil {
		t.Fatalf("holder update: %v", err)
	}

	type result struct {
		prior quota.Prior
		err   error
	}
	done := make(chan result, 1)
	go func() {
		p, err := repo.Replace(ctx, snap(200))
		done <- result{p, err}
	}()
	select {
	case r := <-done:
		t.Fatalf("Replace returned while the holder still had the lock: %+v (err %v)", r.prior, r.err)
	case <-time.After(300 * time.Millisecond):
	}
	if err := holder.Commit(ctx); err != nil {
		t.Fatalf("holder commit: %v", err)
	}
	select {
	case r := <-done:
		if r.err != nil || r.prior.HTTPStatus != 204 {
			t.Fatalf("want prior 204 (the holder's write), got %+v (err %v)", r.prior, r.err)
		}
	case <-ctx.Done():
		t.Fatal("Replace never returned after the holder committed")
	}
}

// TestReplaceErrorPaths reaches the failure branches the happy path never
// sees: a transaction that cannot begin (cancelled context), a write that
// fails (the provider does not exist, so the FK refuses it) and a locked read
// that fails (another transaction holds the table in ACCESS EXCLUSIVE mode, so
// the read waits until a short statement_timeout cancels it). All must come
// back as errors with nothing written, for Replace and ReplaceIfNewer alike.
func TestReplaceErrorPaths(t *testing.T) {
	ctx, cancelAll := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancelAll()
	orphan := quota.Snapshot{ProviderID: uuid.New(), Kind: "usage", Payload: json.RawMessage(`{}`), HTTPStatus: 200, Source: "poll"}

	repo := quota.NewRepository(testPool)
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := repo.Replace(cancelled, orphan); err == nil {
		t.Fatal("Replace: want an error when the transaction cannot begin")
	}
	if _, err := repo.Replace(ctx, orphan); err == nil {
		t.Fatal("Replace: want an error for a provider that does not exist")
	}
	if _, _, err := repo.ReplaceIfNewer(ctx, orphan); err == nil {
		t.Fatal("ReplaceIfNewer: want an error for a provider that does not exist")
	}

	// A one-connection pool with a statement timeout, so the locked read is
	// what fails rather than the test hanging; the setting is asserted because
	// without it the calls below would wait on the lock for the whole test
	// timeout instead.
	slow, err := db.New(ctx, testDBURL, 1, 1)
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	t.Cleanup(slow.Close)
	if _, err := slow.Pool().Exec(ctx, "SET statement_timeout = '250ms'"); err != nil {
		t.Fatalf("set statement_timeout: %v", err)
	}
	var timeout string
	if err := slow.Pool().QueryRow(ctx, "SELECT current_setting('statement_timeout')").Scan(&timeout); err != nil || timeout != "250ms" {
		t.Fatalf("statement_timeout = %q (%v), want 250ms", timeout, err)
	}
	provID := insertTestProvider(ctx, t, "test-quota-replace-locked")
	t.Cleanup(func() { cleanupProvider(context.Background(), t, provID) })
	holder, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin holder tx: %v", err)
	}
	t.Cleanup(func() { _ = holder.Rollback(context.Background()) })
	if _, err := holder.Exec(ctx, "LOCK TABLE provider_quota_snapshots IN ACCESS EXCLUSIVE MODE"); err != nil {
		t.Fatalf("lock table: %v", err)
	}

	locked := quota.NewRepository(slow.Pool())
	own := orphan
	own.ProviderID = provID
	bounded, cancelBounded := context.WithTimeout(ctx, 5*time.Second)
	defer cancelBounded()
	if _, err := locked.Replace(bounded, own); err == nil {
		t.Fatal("Replace: want the locked read to time out")
	}
	if _, _, err := locked.ReplaceIfNewer(bounded, own); err == nil {
		t.Fatal("ReplaceIfNewer: want the locked read to time out")
	}
	_ = holder.Rollback(ctx)
	if snap, err := repo.Get(ctx, provID, "usage"); err != nil || snap != nil {
		t.Fatalf("nothing may be written when the read fails, got %+v (err %v)", snap, err)
	}
}
