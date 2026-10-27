package failover

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/db"
)

func TestGetByModel_InvalidationDuringQueryIsNotReinstalled(t *testing.T) {
	InvalidateFailoverCache()
	ctx := context.Background()
	displayModel := "race-" + uuid.NewString()
	fg, err := upsertGroup(ctx, t, newTestRepo(t), displayModel, []uuid.UUID{uuid.New()})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	t.Cleanup(func() { _ = newTestRepo(t).Delete(ctx, displayModel) })
	if IsCachedByModel(displayModel) {
		t.Fatal("a write path must not install its RETURNING row")
	}

	racing := NewRepository(db.TracedPool(t, testDBURL, func() { InvalidateFailoverCacheKey(displayModel) }))
	if _, err := racing.GetByModel(ctx, displayModel); err != nil {
		t.Fatalf("GetByModel: %v", err)
	}
	if IsCachedByModel(displayModel) {
		t.Fatal("a fill whose SELECT overlapped an invalidation of its key must not install")
	}
	if _, err := racing.GetByID(ctx, fg.ID); err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if IsCachedByModel(displayModel) {
		t.Fatal("a GetByID fill whose SELECT overlapped an invalidation of its key must not install")
	}

	// Control: the same read without a racing write installs.
	if _, err := newTestRepo(t).GetByModel(ctx, displayModel); err != nil {
		t.Fatalf("GetByModel: %v", err)
	}
	if !IsCachedByModel(displayModel) {
		t.Fatal("an unraced read-through must install")
	}
}
