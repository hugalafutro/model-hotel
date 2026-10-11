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

// TestReplaceReportsTheReplacedStatus: Replace hands back the HTTP status of
// the row it overwrote, 0 for no row and 0 for RecordFailure's placeholder, so
// the caller can tell a flip from a first answer.
func TestReplaceReportsTheReplacedStatus(t *testing.T) {
	ctx := context.Background()
	repo := quota.NewRepository(testPool)
	provID := insertTestProvider(ctx, t, "test-quota-replace")
	t.Cleanup(func() { cleanupProvider(ctx, t, provID) })

	snap := func(status int) quota.Snapshot {
		return quota.Snapshot{ProviderID: provID, Kind: "usage", Payload: json.RawMessage(`null`), HTTPStatus: status, Source: "poll"}
	}
	if prev, err := repo.Replace(ctx, snap(204)); err != nil || prev != 0 {
		t.Fatalf("first write: want prev 0, got %d (err %v)", prev, err)
	}
	if prev, err := repo.Replace(ctx, snap(200)); err != nil || prev != 204 {
		t.Fatalf("second write: want prev 204, got %d (err %v)", prev, err)
	}
	if prev, err := repo.Replace(ctx, snap(200)); err != nil || prev != 200 {
		t.Fatalf("same status: want prev 200, got %d (err %v)", prev, err)
	}

	other := insertTestProvider(ctx, t, "test-quota-replace-placeholder")
	t.Cleanup(func() { cleanupProvider(ctx, t, other) })
	if err := repo.RecordFailure(ctx, other, "usage", "timeout"); err != nil {
		t.Fatalf("record failure: %v", err)
	}
	s := snap(200)
	s.ProviderID = other
	if prev, err := repo.Replace(ctx, s); err != nil || prev != 0 {
		t.Fatalf("after a failure placeholder: want prev 0, got %d (err %v)", prev, err)
	}
}

// TestReplaceIfNewerReportsTheFoundStatus: the fleet write reports the stored
// status whether or not it wrote, and wrote keeps UpsertIfNewer's rule.
func TestReplaceIfNewerReportsTheFoundStatus(t *testing.T) {
	ctx := context.Background()
	repo := quota.NewRepository(testPool)
	provID := insertTestProvider(ctx, t, "test-quota-replace-newer")
	t.Cleanup(func() { cleanupProvider(ctx, t, provID) })

	older := time.Now().Add(-time.Hour).UTC()
	snap := func(status int, at time.Time) quota.Snapshot {
		return quota.Snapshot{ProviderID: provID, Kind: "usage", Payload: json.RawMessage(`{}`), HTTPStatus: status, Source: "fleet", FetchedAt: at}
	}
	wrote, prev, err := repo.ReplaceIfNewer(ctx, snap(204, older))
	if err != nil || !wrote || prev != 0 {
		t.Fatalf("first write: want wrote=true prev=0, got wrote=%v prev=%d err=%v", wrote, prev, err)
	}
	wrote, prev, err = repo.ReplaceIfNewer(ctx, snap(200, older.Add(time.Minute)))
	if err != nil || !wrote || prev != 204 {
		t.Fatalf("newer write: want wrote=true prev=204, got wrote=%v prev=%d err=%v", wrote, prev, err)
	}
	wrote, prev, err = repo.ReplaceIfNewer(ctx, snap(204, older))
	if err != nil || wrote || prev != 200 {
		t.Fatalf("older write: want wrote=false prev=200, got wrote=%v prev=%d err=%v", wrote, prev, err)
	}
}

// TestReplaceErrorPaths reaches the two failure branches the happy path never
// sees: a write that fails (the provider does not exist, so the FK refuses
// it) and a locked read that fails (another transaction holds the table in
// ACCESS EXCLUSIVE mode, so the FOR UPDATE select waits until a short
// statement_timeout cancels it). Both must come back as errors with nothing
// written, for Replace and ReplaceIfNewer alike.
func TestReplaceErrorPaths(t *testing.T) {
	ctx := context.Background()
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
	// what fails rather than the test hanging.
	slow, err := db.New(ctx, testDBURL, 1, 1)
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	t.Cleanup(slow.Close)
	if _, err := slow.Pool().Exec(ctx, "SET statement_timeout = '250ms'"); err != nil {
		t.Fatalf("set statement_timeout: %v", err)
	}
	provID := insertTestProvider(ctx, t, "test-quota-replace-locked")
	t.Cleanup(func() { cleanupProvider(ctx, t, provID) })
	holder, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin holder tx: %v", err)
	}
	t.Cleanup(func() { _ = holder.Rollback(ctx) })
	if _, err := holder.Exec(ctx, "LOCK TABLE provider_quota_snapshots IN ACCESS EXCLUSIVE MODE"); err != nil {
		t.Fatalf("lock table: %v", err)
	}

	locked := quota.NewRepository(slow.Pool())
	own := orphan
	own.ProviderID = provID
	if _, err := locked.Replace(ctx, own); err == nil {
		t.Fatal("Replace: want the locked read to time out")
	}
	if _, _, err := locked.ReplaceIfNewer(ctx, own); err == nil {
		t.Fatal("ReplaceIfNewer: want the locked read to time out")
	}
	_ = holder.Rollback(ctx)
	if snap, err := repo.Get(ctx, provID, "usage"); err != nil || snap != nil {
		t.Fatalf("nothing may be written when the read fails, got %+v (err %v)", snap, err)
	}
}
