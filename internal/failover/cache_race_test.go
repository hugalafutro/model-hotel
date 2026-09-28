package failover

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// invalidateOnQuery is a pgx tracer that runs fn as each query starts: the
// invalidation lands while the read-through's SELECT is in flight, which is
// the interleaving the generation guard exists for.
type invalidateOnQuery struct{ fn func() }

func (t invalidateOnQuery) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	t.fn()
	return ctx
}

func (invalidateOnQuery) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func tracedPool(t *testing.T, fn func()) *pgxpool.Pool {
	t.Helper()
	cfg, err := pgxpool.ParseConfig(testDBURL)
	if err != nil {
		t.Fatalf("parse test DB URL: %v", err)
	}
	cfg.ConnConfig.Tracer = invalidateOnQuery{fn: fn}
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("traced pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

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

	racing := NewRepository(tracedPool(t, func() { InvalidateFailoverCacheKey(displayModel) }))
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
