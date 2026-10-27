package provider

import (
	"context"
	"testing"

	"github.com/hugalafutro/model-hotel/internal/dbtest"
)

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

	// A discovery stamp evicts this provider while the SELECT is in flight.
	racing := NewRepository(dbtest.TracedPool(t, testDBURL, func() { EvictProviderCacheByID(p.ID) }))
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
