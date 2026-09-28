package model

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

func TestGet_InvalidationDuringQueryIsNotReinstalled(t *testing.T) {
	InvalidateModelCache()
	ctx := context.Background()
	providerID := insertTestProvider(ctx, t, "race-"+uuid.NewString())
	t.Cleanup(func() { cleanupProvider(ctx, t, providerID) })
	modelID := insertTestModel(ctx, t, providerID, "race-model")

	racing := NewRepository(tracedPool(t, InvalidateModelCache))
	if _, err := racing.Get(ctx, modelID); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if _, ok := GetCachedByUUID(modelID); ok {
		t.Fatal("a fill whose SELECT overlapped an invalidation must not install")
	}
	if _, err := racing.GetByProviderAndModelID(ctx, providerID, "race-model"); err != nil {
		t.Fatalf("GetByProviderAndModelID: %v", err)
	}
	if _, ok := GetCachedByCompositeKey(providerID, "race-model"); ok {
		t.Fatal("composite fill whose SELECT overlapped an invalidation must not install")
	}

	// Control: the same read without a racing write installs.
	if _, err := NewRepository(testPool).Get(ctx, modelID); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if _, ok := GetCachedByUUID(modelID); !ok {
		t.Fatal("an unraced read-through must install")
	}
}
