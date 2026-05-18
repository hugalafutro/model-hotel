package provider

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

// ===========================================================================
// Coverage gap tests for GetByIDs and List
// ===========================================================================

// TestGetByIDs_AllCached tests the path where all requested IDs are in cache.
// This exercises the early return path: if len(uncachedIDs) == 0 { return result, nil }
func TestGetByIDs_AllCached(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	// Create two providers
	p1, err := repo.Create(ctx, CreateProviderRequest{
		Name: uniqueName(t), BaseURL: "https://allcached1.example.com", APIKey: "sk-cached1",
	}, []byte("enc"), []byte("nonce"), []byte("salt"))
	if err != nil {
		t.Fatalf("Create p1: %v", err)
	}

	p2, err := repo.Create(ctx, CreateProviderRequest{
		Name: uniqueName(t), BaseURL: "https://allcached2.example.com", APIKey: "sk-cached2",
	}, []byte("enc"), []byte("nonce"), []byte("salt"))
	if err != nil {
		t.Fatalf("Create p2: %v", err)
	}

	// Pre-populate cache by calling Get for both providers
	_, err = repo.Get(ctx, p1.ID)
	if err != nil {
		t.Fatalf("Get p1: %v", err)
	}
	_, err = repo.Get(ctx, p2.ID)
	if err != nil {
		t.Fatalf("Get p2: %v", err)
	}

	// GetByIDs should return both from cache without DB query
	result, err := repo.GetByIDs(ctx, []uuid.UUID{p1.ID, p2.ID})
	if err != nil {
		t.Fatalf("GetByIDs: %v", err)
	}
	if len(result) != 2 {
		t.Errorf("expected 2 results from cache, got %d", len(result))
	}
	if _, ok := result[p1.ID]; !ok {
		t.Error("p1 not found in cached result")
	}
	if _, ok := result[p2.ID]; !ok {
		t.Error("p2 not found in cached result")
	}
}

// TestList_Empty tests the path where the providers table is empty.
// This ensures List handles zero rows gracefully.
func TestList_Empty(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	// Delete all providers to ensure empty table
	// First, list existing providers
	existing, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("List initial: %v", err)
	}

	// Delete each existing provider
	for _, p := range existing {
		if err := repo.Delete(ctx, p.ID); err != nil {
			t.Fatalf("Delete %v: %v", p.ID, err)
		}
	}

	// Now List should return empty slice, no error
	providers, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("List on empty table: %v", err)
	}
	if len(providers) != 0 {
		t.Errorf("expected 0 providers on empty table, got %d", len(providers))
	}
}
