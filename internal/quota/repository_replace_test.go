package quota_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

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
