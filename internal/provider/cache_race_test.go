package provider

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// invalidateOnQuery is a pgx tracer that runs fn as each query starts: the
// eviction lands while the read-through's SELECT is in flight, which is the
// interleaving the generation guard exists for.
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

func TestGetByName_EvictionDuringQueryIsNotReinstalled(t *testing.T) {
	InvalidateProviderCache()
	ctx := context.Background()
	p, err := newTestRepo(t).Create(ctx, CreateProviderRequest{
		Name: uniqueName(t), BaseURL: "https://race.example.com", APIKey: "sk-race",
	}, []byte("enc"), []byte("nonce"), []byte("salt"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { _ = newTestRepo(t).Delete(ctx, p.ID) })

	// A TouchLastUsed of this provider lands while the SELECT is in flight.
	racing := NewRepository(tracedPool(t, func() { EvictProviderCacheByID(p.ID) }))
	if _, err := racing.GetByName(ctx, p.Name); err != nil {
		t.Fatalf("GetByName: %v", err)
	}
	if IsCachedByID(p.ID) || IsCachedByName(p.Name) {
		t.Fatal("a fill whose SELECT overlapped an eviction of its row must not install")
	}
	if _, err := racing.Get(ctx, p.ID); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if IsCachedByID(p.ID) {
		t.Fatal("a Get fill whose SELECT overlapped an eviction of its row must not install")
	}

	// Control: the same read without a racing write installs.
	if _, err := newTestRepo(t).GetByName(ctx, p.Name); err != nil {
		t.Fatalf("GetByName: %v", err)
	}
	if !IsCachedByID(p.ID) {
		t.Fatal("an unraced read-through must install")
	}
}
