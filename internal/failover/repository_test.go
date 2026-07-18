package failover

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRepository_SyncForModel_TwoProviders(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	// Create unique identifiers for this test
	baseModel := "test-syncformodel-" + uuid.New().String()[:8]
	provider1Name := "test-provider-1-" + uuid.New().String()[:8]
	provider2Name := "test-provider-2-" + uuid.New().String()[:8]

	// Insert test data
	provider1ID := uuid.New()
	provider2ID := uuid.New()
	model1ID := uuid.New()
	model2ID := uuid.New()

	_, err := testDB.Pool().Exec(ctx, `
		INSERT INTO providers (id, name, base_url, encrypted_key, key_nonce, key_salt, enabled, created_at)
		VALUES ($1, $2, 'http://localhost:11434', 'dGVzdA==', 'dGVzdA==', 'dGVzdA==', true, now())
	`, provider1ID, provider1Name)
	if err != nil {
		t.Fatalf("Failed to insert provider1: %v", err)
	}
	defer func() {
		_, _ = testDB.Pool().Exec(ctx, "DELETE FROM providers WHERE id = $1", provider1ID)
	}()

	_, err = testDB.Pool().Exec(ctx, `
		INSERT INTO providers (id, name, base_url, encrypted_key, key_nonce, key_salt, enabled, created_at)
		VALUES ($1, $2, 'http://localhost:11434', 'dGVzdA==', 'dGVzdA==', 'dGVzdA==', true, now())
	`, provider2ID, provider2Name)
	if err != nil {
		t.Fatalf("Failed to insert provider2: %v", err)
	}
	defer func() {
		_, _ = testDB.Pool().Exec(ctx, "DELETE FROM providers WHERE id = $1", provider2ID)
	}()

	_, err = testDB.Pool().Exec(ctx, `
		INSERT INTO models (id, model_id, provider_id, enabled, created_at)
		VALUES ($1, $2, $3, true, now())
	`, model1ID, baseModel, provider1ID)
	if err != nil {
		t.Fatalf("Failed to insert model1: %v", err)
	}
	defer func() {
		_, _ = testDB.Pool().Exec(ctx, "DELETE FROM models WHERE id = $1", model1ID)
	}()

	_, err = testDB.Pool().Exec(ctx, `
		INSERT INTO models (id, model_id, provider_id, enabled, created_at)
		VALUES ($1, $2, $3, true, now())
	`, model2ID, baseModel, provider2ID)
	if err != nil {
		t.Fatalf("Failed to insert model2: %v", err)
	}
	defer func() {
		_, _ = testDB.Pool().Exec(ctx, "DELETE FROM models WHERE id = $1", model2ID)
	}()

	// Call SyncForModel
	_, err = repo.SyncForModel(ctx, baseModel)
	if err != nil {
		t.Fatalf("SyncForModel failed: %v", err)
	}

	// Verify auto-group was created
	InvalidateFailoverCache()
	group, err := repo.GetByModel(ctx, baseModel)
	if err != nil {
		t.Fatalf("Failed to get auto-group: %v", err)
	}
	if group == nil {
		t.Fatal("Expected auto-group to be created")
		return
	}
	if !group.AutoCreated {
		t.Error("Expected AutoCreated to be true")
	}
	if !group.GroupEnabled {
		t.Error("Expected GroupEnabled to be true")
	}

	// Cleanup the failover group
	if err := repo.Delete(ctx, baseModel); err != nil {
		t.Logf("cleanup Delete failed: %v", err)
	}
}

func TestRepository_SyncForModel_SingleProvider(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	// Create unique identifiers for this test
	baseModel := "test-syncformodel-single-" + uuid.New().String()[:8]
	providerName := "test-provider-single-" + uuid.New().String()[:8]

	// Insert test data
	providerID := uuid.New()
	modelID := uuid.New()

	_, err := testDB.Pool().Exec(ctx, `
		INSERT INTO providers (id, name, base_url, encrypted_key, key_nonce, key_salt, enabled, created_at)
		VALUES ($1, $2, 'http://localhost:11434', 'dGVzdA==', 'dGVzdA==', 'dGVzdA==', true, now())
	`, providerID, providerName)
	if err != nil {
		t.Fatalf("Failed to insert provider: %v", err)
	}
	defer func() {
		_, _ = testDB.Pool().Exec(ctx, "DELETE FROM providers WHERE id = $1", providerID)
	}()

	_, err = testDB.Pool().Exec(ctx, `
		INSERT INTO models (id, model_id, provider_id, enabled, created_at)
		VALUES ($1, $2, $3, true, now())
	`, modelID, baseModel, providerID)
	if err != nil {
		t.Fatalf("Failed to insert model: %v", err)
	}
	defer func() {
		_, _ = testDB.Pool().Exec(ctx, "DELETE FROM models WHERE id = $1", modelID)
	}()

	// Call SyncForModel - should delete any existing auto-group
	_, err = repo.SyncForModel(ctx, baseModel)
	if err != nil {
		t.Fatalf("SyncForModel failed: %v", err)
	}

	// Verify no auto-group exists (deleted if it had ≤1 model)
	InvalidateFailoverCache()
	group, err := repo.GetByModel(ctx, baseModel)
	if err != nil {
		// Expected - no group exists, which is correct for single provider
		return
	}
	if group != nil {
		t.Error("Expected auto-group to be deleted for single provider")
	}
}

func TestRepository_SyncForModel_WithPrefix(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	// Create unique identifiers for this test
	baseModel := "test-syncformodel-prefix-" + uuid.New().String()[:8]
	provider1Name := "test-provider-1-" + uuid.New().String()[:8]
	provider2Name := "test-provider-2-" + uuid.New().String()[:8]

	// Insert test data
	provider1ID := uuid.New()
	provider2ID := uuid.New()
	model1ID := uuid.New()
	model2ID := uuid.New()

	_, err := testDB.Pool().Exec(ctx, `
		INSERT INTO providers (id, name, base_url, encrypted_key, key_nonce, key_salt, enabled, created_at)
		VALUES ($1, $2, 'http://localhost:11434', 'dGVzdA==', 'dGVzdA==', 'dGVzdA==', true, now())
	`, provider1ID, provider1Name)
	if err != nil {
		t.Fatalf("Failed to insert provider1: %v", err)
	}
	defer func() {
		_, _ = testDB.Pool().Exec(ctx, "DELETE FROM providers WHERE id = $1", provider1ID)
	}()

	_, err = testDB.Pool().Exec(ctx, `
		INSERT INTO providers (id, name, base_url, encrypted_key, key_nonce, key_salt, enabled, created_at)
		VALUES ($1, $2, 'http://localhost:11434', 'dGVzdA==', 'dGVzdA==', 'dGVzdA==', true, now())
	`, provider2ID, provider2Name)
	if err != nil {
		t.Fatalf("Failed to insert provider2: %v", err)
	}
	defer func() {
		_, _ = testDB.Pool().Exec(ctx, "DELETE FROM providers WHERE id = $1", provider2ID)
	}()

	// Use prefixed model IDs
	modelID1 := "openai/" + baseModel
	modelID2 := "anthropic/" + baseModel

	_, err = testDB.Pool().Exec(ctx, `
		INSERT INTO models (id, model_id, provider_id, enabled, created_at)
		VALUES ($1, $2, $3, true, now())
	`, model1ID, modelID1, provider1ID)
	if err != nil {
		t.Fatalf("Failed to insert model1: %v", err)
	}
	defer func() {
		_, _ = testDB.Pool().Exec(ctx, "DELETE FROM models WHERE id = $1", model1ID)
	}()

	_, err = testDB.Pool().Exec(ctx, `
		INSERT INTO models (id, model_id, provider_id, enabled, created_at)
		VALUES ($1, $2, $3, true, now())
	`, model2ID, modelID2, provider2ID)
	if err != nil {
		t.Fatalf("Failed to insert model2: %v", err)
	}
	defer func() {
		_, _ = testDB.Pool().Exec(ctx, "DELETE FROM models WHERE id = $1", model2ID)
	}()

	// Call SyncForModel with prefixed model ID
	_, err = repo.SyncForModel(ctx, modelID1)
	if err != nil {
		t.Fatalf("SyncForModel failed: %v", err)
	}

	// Verify auto-group was created with stripped prefix
	InvalidateFailoverCache()
	group, err := repo.GetByModel(ctx, baseModel)
	if err != nil {
		t.Fatalf("Failed to get auto-group: %v", err)
	}
	if group == nil {
		t.Fatal("Expected auto-group to be created with stripped prefix")
		return
	}
	if group.DisplayModel != baseModel {
		t.Errorf("Expected DisplayModel %q, got %q", baseModel, group.DisplayModel)
	}

	// Cleanup the failover group
	if err := repo.Delete(ctx, baseModel); err != nil {
		t.Logf("cleanup Delete failed: %v", err)
	}
}

func TestRepository_SyncForModel_WithPrefixVariants(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	// Test with various prefix variants
	baseModel := "test-prefix-variant-" + uuid.New().String()[:8]
	prefixedModel := "openai/" + baseModel

	provider1ID := uuid.New()
	provider2ID := uuid.New()
	model1ID := uuid.New()
	model2ID := uuid.New()

	_, err := testDB.Pool().Exec(ctx, `
		INSERT INTO providers (id, name, base_url, encrypted_key, key_nonce, key_salt, enabled, created_at)
		VALUES ($1, $2, 'http://localhost:11434', 'dGVzdA==', 'dGVzdA==', 'dGVzdA==', true, now())
	`, provider1ID, "test-provider-1-"+uuid.New().String()[:8])
	if err != nil {
		t.Fatalf("Failed to insert provider1: %v", err)
	}
	defer func() {
		_, _ = testDB.Pool().Exec(ctx, "DELETE FROM providers WHERE id = $1", provider1ID)
	}()

	_, err = testDB.Pool().Exec(ctx, `
		INSERT INTO providers (id, name, base_url, encrypted_key, key_nonce, key_salt, enabled, created_at)
		VALUES ($1, $2, 'http://localhost:11434', 'dGVzdA==', 'dGVzdA==', 'dGVzdA==', true, now())
	`, provider2ID, "test-provider-2-"+uuid.New().String()[:8])
	if err != nil {
		t.Fatalf("Failed to insert provider2: %v", err)
	}
	defer func() {
		_, _ = testDB.Pool().Exec(ctx, "DELETE FROM providers WHERE id = $1", provider2ID)
	}()

	// Insert models with different prefixes for the same base model
	_, err = testDB.Pool().Exec(ctx, `
		INSERT INTO models (id, model_id, provider_id, enabled, created_at)
		VALUES ($1, $2, $3, true, now())
	`, model1ID, prefixedModel, provider1ID)
	if err != nil {
		t.Fatalf("Failed to insert model1: %v", err)
	}
	defer func() {
		_, _ = testDB.Pool().Exec(ctx, "DELETE FROM models WHERE id = $1", model1ID)
	}()

	_, err = testDB.Pool().Exec(ctx, `
		INSERT INTO models (id, model_id, provider_id, enabled, created_at)
		VALUES ($1, $2, $3, true, now())
	`, model2ID, "anthropic/"+baseModel, provider2ID)
	if err != nil {
		t.Fatalf("Failed to insert model2: %v", err)
	}
	defer func() {
		_, _ = testDB.Pool().Exec(ctx, "DELETE FROM models WHERE id = $1", model2ID)
	}()

	// Call SyncForModel with the prefixed model ID
	_, err = repo.SyncForModel(ctx, prefixedModel)
	if err != nil {
		t.Fatalf("SyncForModel failed: %v", err)
	}

	// Verify auto-group was created with stripped prefix
	InvalidateFailoverCache()
	group, err := repo.GetByModel(ctx, baseModel)
	if err != nil {
		t.Fatalf("Failed to get auto-group: %v", err)
	}
	if group == nil {
		t.Fatal("Expected auto-group to be created with stripped prefix")
		return
	}
	if group.DisplayModel != baseModel {
		t.Errorf("Expected DisplayModel %q, got %q", baseModel, group.DisplayModel)
	}

	// Cleanup
	if err := repo.Delete(ctx, baseModel); err != nil {
		t.Logf("cleanup Delete failed: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Integration tests — Sync priority order preservation
// ---------------------------------------------------------------------------

func TestMergePriorityOrder(t *testing.T) {
	a := uuid.New()
	b := uuid.New()
	c := uuid.New()
	d := uuid.New()

	tests := []struct {
		name          string
		existingOrder []uuid.UUID
		currentIDs    []uuid.UUID
		want          []uuid.UUID
	}{
		{
			name:          "empty existing, new entries",
			existingOrder: nil,
			currentIDs:    []uuid.UUID{a, b, c},
			want:          []uuid.UUID{a, b, c},
		},
		{
			name:          "existing order preserved when all present",
			existingOrder: []uuid.UUID{c, a, b},
			currentIDs:    []uuid.UUID{a, b, c},
			want:          []uuid.UUID{c, a, b},
		},
		{
			name:          "removed entries dropped, new entries appended",
			existingOrder: []uuid.UUID{d, a, b},
			currentIDs:    []uuid.UUID{a, b, c},
			want:          []uuid.UUID{a, b, c},
		},
		{
			name:          "new entries appended at end",
			existingOrder: []uuid.UUID{b, a},
			currentIDs:    []uuid.UUID{a, b, c, d},
			want:          []uuid.UUID{b, a, c, d},
		},
		{
			name:          "all entries removed",
			existingOrder: []uuid.UUID{d},
			currentIDs:    []uuid.UUID{a, b},
			want:          []uuid.UUID{a, b},
		},
		{
			name:          "same order no change",
			existingOrder: []uuid.UUID{a, b, c},
			currentIDs:    []uuid.UUID{a, b, c},
			want:          []uuid.UUID{a, b, c},
		},
		{
			name:          "both inputs empty",
			existingOrder: nil,
			currentIDs:    nil,
			want:          nil,
		},
		{
			name:          "empty currentIDs removes all existing",
			existingOrder: []uuid.UUID{a, b},
			currentIDs:    nil,
			want:          nil,
		},
		{
			name:          "duplicate in existingOrder deduplicated",
			existingOrder: []uuid.UUID{a, a, b},
			currentIDs:    []uuid.UUID{a, b},
			want:          []uuid.UUID{a, b},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mergePriorityOrder(tt.existingOrder, tt.currentIDs)
			if len(got) != len(tt.want) {
				t.Fatalf("mergePriorityOrder() length = %d, want %d", len(got), len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("mergePriorityOrder()[%d] = %v, want %v", i, got[i], tt.want[i])
				}
			}
		})
	}
}
func TestRepository_SyncForModel_PreservesDisabledEntries(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	baseModel := "test-syncformodel-preserve-" + uuid.New().String()[:8]
	provider1Name := "test-provider-1-" + uuid.New().String()[:8]
	provider2Name := "test-provider-2-" + uuid.New().String()[:8]
	provider3Name := "test-provider-3-" + uuid.New().String()[:8]

	// Create 3 providers
	provider1ID := uuid.New()
	provider2ID := uuid.New()
	provider3ID := uuid.New()

	for _, p := range []struct {
		id   uuid.UUID
		name string
	}{
		{provider1ID, provider1Name},
		{provider2ID, provider2Name},
		{provider3ID, provider3Name},
	} {
		_, err := testDB.Pool().Exec(ctx, `
			INSERT INTO providers (id, name, base_url, encrypted_key, key_nonce, key_salt, enabled, created_at)
			VALUES ($1, $2, 'http://localhost:11434', 'dGVzdA==', 'dGVzdA==', 'dGVzdA==', true, now())
		`, p.id, p.name)
		if err != nil {
			t.Fatalf("Failed to insert provider %s: %v", p.name, err)
		}
		defer func(id uuid.UUID) {
			_, _ = testDB.Pool().Exec(ctx, "DELETE FROM providers WHERE id = $1", id)
		}(p.id)
	}

	// Create 3 models (one per provider)
	model1ID := uuid.New()
	model2ID := uuid.New()
	model3ID := uuid.New()

	for _, m := range []struct {
		id         uuid.UUID
		providerID uuid.UUID
	}{
		{model1ID, provider1ID},
		{model2ID, provider2ID},
		{model3ID, provider3ID},
	} {
		_, err := testDB.Pool().Exec(ctx, `
			INSERT INTO models (id, model_id, provider_id, enabled, created_at)
			VALUES ($1, $2, $3, true, now())
		`, m.id, baseModel, m.providerID)
		if err != nil {
			t.Fatalf("Failed to insert model: %v", err)
		}
		defer func(id uuid.UUID) {
			_, _ = testDB.Pool().Exec(ctx, "DELETE FROM models WHERE id = $1", id)
		}(m.id)
	}

	// Pre-create a failover group with model2 disabled
	priorityOrder := []uuid.UUID{model1ID, model2ID}
	entryEnabled := map[string]bool{
		model1ID.String(): true,
		model2ID.String(): false,
	}
	groupEnabled := true
	autoCreated := true

	_, err := repo.UpsertWithConfig(ctx, baseModel, priorityOrder, entryEnabled, &groupEnabled, nil, nil, &autoCreated)
	if err != nil {
		t.Fatalf("Failed to create initial group: %v", err)
	}
	defer func() {
		InvalidateFailoverCache()
		_ = repo.Delete(ctx, baseModel)
	}()

	// Call SyncForModel - should add model3 as enabled, preserve model2 as disabled
	_, err = repo.SyncForModel(ctx, baseModel)
	if err != nil {
		t.Fatalf("SyncForModel failed: %v", err)
	}

	// Verify the group was updated correctly
	InvalidateFailoverCache()
	group, err := repo.GetByModel(ctx, baseModel)
	if err != nil {
		t.Fatalf("Failed to get group after sync: %v", err)
	}

	// Should have all 3 models in priority order
	if len(group.PriorityOrder) != 3 {
		t.Errorf("Expected 3 models in priority order, got %d", len(group.PriorityOrder))
	}

	// model1 should still be enabled
	if group.EntryEnabled[model1ID.String()] != true {
		t.Errorf("Expected model1 to be enabled, got %v", group.EntryEnabled[model1ID.String()])
	}

	// model2 should still be disabled (preserved)
	if group.EntryEnabled[model2ID.String()] != false {
		t.Errorf("Expected model2 to be disabled (preserved), got %v", group.EntryEnabled[model2ID.String()])
	}

	// model3 should be enabled (new entry)
	if group.EntryEnabled[model3ID.String()] != true {
		t.Errorf("Expected model3 to be enabled (new entry), got %v", group.EntryEnabled[model3ID.String()])
	}
}

func TestRepository_SyncForModel_SuccessfulSync(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	// This test verifies that SyncForModel returns an error when upsert fails.
	// We test the normal successful path and verify error propagation structure.

	baseModel := "test-syncformodel-error-" + uuid.New().String()[:8]
	provider1Name := "test-provider-err-1-" + uuid.New().String()[:8]
	provider2Name := "test-provider-err-2-" + uuid.New().String()[:8]

	provider1ID := uuid.New()
	provider2ID := uuid.New()
	model1ID := uuid.New()
	model2ID := uuid.New()

	for _, p := range []struct {
		id   uuid.UUID
		name string
	}{
		{provider1ID, provider1Name},
		{provider2ID, provider2Name},
	} {
		_, err := testDB.Pool().Exec(ctx, `
			INSERT INTO providers (id, name, base_url, encrypted_key, key_nonce, key_salt, enabled, created_at)
			VALUES ($1, $2, 'http://localhost:11434', 'dGVzdA==', 'dGVzdA==', 'dGVzdA==', true, now())
		`, p.id, p.name)
		if err != nil {
			t.Fatalf("Failed to insert provider: %v", err)
		}
		defer func(id uuid.UUID) {
			_, _ = testDB.Pool().Exec(ctx, "DELETE FROM providers WHERE id = $1", id)
		}(p.id)
	}

	for _, m := range []struct {
		id         uuid.UUID
		providerID uuid.UUID
	}{
		{model1ID, provider1ID},
		{model2ID, provider2ID},
	} {
		_, err := testDB.Pool().Exec(ctx, `
			INSERT INTO models (id, model_id, provider_id, enabled, created_at)
			VALUES ($1, $2, $3, true, now())
		`, m.id, baseModel, m.providerID)
		if err != nil {
			t.Fatalf("Failed to insert model: %v", err)
		}
		defer func(id uuid.UUID) {
			_, _ = testDB.Pool().Exec(ctx, "DELETE FROM models WHERE id = $1", id)
		}(m.id)
	}

	// Call SyncForModel - should succeed normally
	_, err := repo.SyncForModel(ctx, baseModel)
	if err != nil {
		t.Fatalf("SyncForModel failed: %v", err)
	}

	// Verify the group was created
	InvalidateFailoverCache()
	group, err := repo.GetByModel(ctx, baseModel)
	if err != nil {
		t.Fatalf("Failed to get group: %v", err)
	}
	if len(group.PriorityOrder) != 2 {
		t.Errorf("Expected 2 models in group, got %d", len(group.PriorityOrder))
	}

	// Cleanup
	_ = repo.Delete(ctx, baseModel)
}

func TestRepository_SyncForModel_ValidRowScan(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	// This test verifies that SyncForModel handles row scan errors gracefully
	// by continuing to process other rows. The row scan error path (lines 518-519)
	// uses 'continue' to skip problematic rows.

	baseModel := "test-syncformodel-scan-" + uuid.New().String()[:8]
	provider1Name := "test-provider-fscan-1-" + uuid.New().String()[:8]
	provider2Name := "test-provider-fscan-2-" + uuid.New().String()[:8]

	provider1ID := uuid.New()
	provider2ID := uuid.New()
	model1ID := uuid.New()
	model2ID := uuid.New()

	for _, p := range []struct {
		id   uuid.UUID
		name string
	}{
		{provider1ID, provider1Name},
		{provider2ID, provider2Name},
	} {
		_, err := testDB.Pool().Exec(ctx, `
			INSERT INTO providers (id, name, base_url, encrypted_key, key_nonce, key_salt, enabled, created_at)
			VALUES ($1, $2, 'http://localhost:11434', 'dGVzdA==', 'dGVzdA==', 'dGVzdA==', true, now())
		`, p.id, p.name)
		if err != nil {
			t.Fatalf("Failed to insert provider: %v", err)
		}
		defer func(id uuid.UUID) {
			_, _ = testDB.Pool().Exec(ctx, "DELETE FROM providers WHERE id = $1", id)
		}(p.id)
	}

	for _, m := range []struct {
		id         uuid.UUID
		providerID uuid.UUID
	}{
		{model1ID, provider1ID},
		{model2ID, provider2ID},
	} {
		_, err := testDB.Pool().Exec(ctx, `
			INSERT INTO models (id, model_id, provider_id, enabled, created_at)
			VALUES ($1, $2, $3, true, now())
		`, m.id, baseModel, m.providerID)
		if err != nil {
			t.Fatalf("Failed to insert model: %v", err)
		}
		defer func(id uuid.UUID) {
			_, _ = testDB.Pool().Exec(ctx, "DELETE FROM models WHERE id = $1", id)
		}(m.id)
	}

	// Call SyncForModel - should handle all rows correctly
	_, err := repo.SyncForModel(ctx, baseModel)
	if err != nil {
		t.Fatalf("SyncForModel failed: %v", err)
	}

	// Verify successful sync
	InvalidateFailoverCache()
	group, err := repo.GetByModel(ctx, baseModel)
	if err != nil {
		t.Fatalf("Failed to get group: %v", err)
	}
	if len(group.PriorityOrder) != 2 {
		t.Errorf("Expected 2 models in group, got %d", len(group.PriorityOrder))
	}

	// Cleanup
	_ = repo.Delete(ctx, baseModel)
}

// ---------------------------------------------------------------------------
// Integration tests — SyncForModel priority order preservation
// ---------------------------------------------------------------------------

func TestRepository_SyncForModel_PreservesPriorityOrder(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	baseModel := "test-syncformodel-priority-" + uuid.New().String()[:8]
	provider1Name := "test-provider-1-" + uuid.New().String()[:8]
	provider2Name := "test-provider-2-" + uuid.New().String()[:8]

	// Create 2 providers initially
	provider1ID := uuid.New()
	provider2ID := uuid.New()

	for _, p := range []struct {
		id   uuid.UUID
		name string
	}{
		{provider1ID, provider1Name},
		{provider2ID, provider2Name},
	} {
		_, err := testDB.Pool().Exec(ctx, `
			INSERT INTO providers (id, name, base_url, encrypted_key, key_nonce, key_salt, enabled, created_at)
			VALUES ($1, $2, 'http://localhost:11434', 'dGVzdA==', 'dGVzdA==', 'dGVzdA==', true, now())
		`, p.id, p.name)
		if err != nil {
			t.Fatalf("Failed to insert provider %s: %v", p.name, err)
		}
		defer func(id uuid.UUID) {
			_, _ = testDB.Pool().Exec(ctx, "DELETE FROM providers WHERE id = $1", id)
		}(p.id)
	}

	// Create 2 models initially
	model1ID := uuid.New()
	model2ID := uuid.New()

	for _, m := range []struct {
		id         uuid.UUID
		providerID uuid.UUID
	}{
		{model1ID, provider1ID},
		{model2ID, provider2ID},
	} {
		_, err := testDB.Pool().Exec(ctx, `
			INSERT INTO models (id, model_id, provider_id, enabled, created_at)
			VALUES ($1, $2, $3, true, now())
		`, m.id, baseModel, m.providerID)
		if err != nil {
			t.Fatalf("Failed to insert model: %v", err)
		}
		defer func(id uuid.UUID) {
			_, _ = testDB.Pool().Exec(ctx, "DELETE FROM models WHERE id = $1", id)
		}(m.id)
	}

	// Pre-create a failover group with custom priority order: [model2, model1] (reversed)
	customPriorityOrder := []uuid.UUID{model2ID, model1ID}
	entryEnabled := map[string]bool{
		model1ID.String(): true,
		model2ID.String(): true,
	}
	groupEnabled := true
	autoCreated := false

	_, err := repo.UpsertWithConfig(ctx, baseModel, customPriorityOrder, entryEnabled, &groupEnabled, nil, nil, &autoCreated)
	if err != nil {
		t.Fatalf("Failed to create initial group: %v", err)
	}
	defer func() {
		InvalidateFailoverCache()
		_ = repo.Delete(ctx, baseModel)
	}()

	// Call SyncForModel - should preserve the custom order
	_, err = repo.SyncForModel(ctx, baseModel)
	if err != nil {
		t.Fatalf("SyncForModel failed: %v", err)
	}

	// Verify the custom order was preserved: [model2, model1]
	InvalidateFailoverCache()
	group, err := repo.GetByModel(ctx, baseModel)
	if err != nil {
		t.Fatalf("Failed to get group after sync: %v", err)
	}

	if len(group.PriorityOrder) != 2 {
		t.Errorf("Expected 2 models in priority order, got %d", len(group.PriorityOrder))
	}
	for i, expectedID := range customPriorityOrder {
		if i >= len(group.PriorityOrder) {
			t.Errorf("PriorityOrder[%d] missing, expected %v", i, expectedID)
			continue
		}
		if group.PriorityOrder[i] != expectedID {
			t.Errorf("PriorityOrder[%d] = %v, want %v", i, group.PriorityOrder[i], expectedID)
		}
	}

	// Add a 3rd provider and model
	provider3Name := "test-provider-3-" + uuid.New().String()[:8]
	provider3ID := uuid.New()
	model3ID := uuid.New()

	_, err = testDB.Pool().Exec(ctx, `
		INSERT INTO providers (id, name, base_url, encrypted_key, key_nonce, key_salt, enabled, created_at)
		VALUES ($1, $2, 'http://localhost:11434', 'dGVzdA==', 'dGVzdA==', 'dGVzdA==', true, now())
	`, provider3ID, provider3Name)
	if err != nil {
		t.Fatalf("Failed to insert provider3: %v", err)
	}
	defer func() {
		_, _ = testDB.Pool().Exec(ctx, "DELETE FROM providers WHERE id = $1", provider3ID)
	}()

	_, err = testDB.Pool().Exec(ctx, `
		INSERT INTO models (id, model_id, provider_id, enabled, created_at)
		VALUES ($1, $2, $3, true, now())
	`, model3ID, baseModel, provider3ID)
	if err != nil {
		t.Fatalf("Failed to insert model3: %v", err)
	}
	defer func() {
		_, _ = testDB.Pool().Exec(ctx, "DELETE FROM models WHERE id = $1", model3ID)
	}()

	// Call SyncForModel again - should preserve existing order and append new model
	_, err = repo.SyncForModel(ctx, baseModel)
	if err != nil {
		t.Fatalf("SyncForModel failed on second call: %v", err)
	}

	// Verify the existing order was preserved with new model appended: [model2, model1, model3]
	InvalidateFailoverCache()
	group, err = repo.GetByModel(ctx, baseModel)
	if err != nil {
		t.Fatalf("Failed to get group after second sync: %v", err)
	}

	expectedOrder := []uuid.UUID{model2ID, model1ID, model3ID}
	if len(group.PriorityOrder) != 3 {
		t.Errorf("Expected 3 models in priority order, got %d", len(group.PriorityOrder))
	}
	for i, expectedID := range expectedOrder {
		if i >= len(group.PriorityOrder) {
			t.Errorf("PriorityOrder[%d] missing, expected %v", i, expectedID)
			continue
		}
		if group.PriorityOrder[i] != expectedID {
			t.Errorf("PriorityOrder[%d] = %v, want %v", i, group.PriorityOrder[i], expectedID)
		}
	}
}

// ---------------------------------------------------------------------------
// SyncForModel specific tests
// ---------------------------------------------------------------------------

func TestSyncForModel_PreservesDescription(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	baseModel := "test-syncformodel-preserve-desc-" + uuid.New().String()[:8]
	provider1Name := "test-provider-1-" + uuid.New().String()[:8]
	provider2Name := "test-provider-2-" + uuid.New().String()[:8]

	provider1ID := uuid.New()
	provider2ID := uuid.New()
	model1ID := uuid.New()
	model2ID := uuid.New()

	_, err := testDB.Pool().Exec(ctx, `
		INSERT INTO providers (id, name, base_url, encrypted_key, key_nonce, key_salt, enabled, created_at)
		VALUES ($1, $2, 'http://localhost:11434', 'dGVzdA==', 'dGVzdA==', 'dGVzdA==', true, now())
	`, provider1ID, provider1Name)
	if err != nil {
		t.Fatalf("Failed to insert provider1: %v", err)
	}
	defer func() {
		_, _ = testDB.Pool().Exec(ctx, "DELETE FROM providers WHERE id = $1", provider1ID)
	}()

	_, err = testDB.Pool().Exec(ctx, `
		INSERT INTO providers (id, name, base_url, encrypted_key, key_nonce, key_salt, enabled, created_at)
		VALUES ($1, $2, 'http://localhost:11434', 'dGVzdA==', 'dGVzdA==', 'dGVzdA==', true, now())
	`, provider2ID, provider2Name)
	if err != nil {
		t.Fatalf("Failed to insert provider2: %v", err)
	}
	defer func() {
		_, _ = testDB.Pool().Exec(ctx, "DELETE FROM providers WHERE id = $1", provider2ID)
	}()

	_, err = testDB.Pool().Exec(ctx, `
		INSERT INTO models (id, model_id, provider_id, enabled, created_at)
		VALUES ($1, $2, $3, true, now())
	`, model1ID, baseModel, provider1ID)
	if err != nil {
		t.Fatalf("Failed to insert model1: %v", err)
	}
	defer func() {
		_, _ = testDB.Pool().Exec(ctx, "DELETE FROM models WHERE id = $1", model1ID)
	}()

	_, err = testDB.Pool().Exec(ctx, `
		INSERT INTO models (id, model_id, provider_id, enabled, created_at)
		VALUES ($1, $2, $3, true, now())
	`, model2ID, baseModel, provider2ID)
	if err != nil {
		t.Fatalf("Failed to insert model2: %v", err)
	}
	defer func() {
		_, _ = testDB.Pool().Exec(ctx, "DELETE FROM models WHERE id = $1", model2ID)
	}()

	description := "Test description to preserve in SyncForModel"
	priorityOrder := []uuid.UUID{model1ID}
	entryEnabled := map[string]bool{model1ID.String(): true}
	groupEnabled := true
	autoCreated := false

	_, err = repo.UpsertWithConfig(ctx, baseModel, priorityOrder, entryEnabled, &groupEnabled, nil, &description, &autoCreated)
	if err != nil {
		t.Fatalf("Failed to create initial group: %v", err)
	}
	defer func() {
		_ = repo.Delete(ctx, baseModel)
	}()

	_, err = repo.SyncForModel(ctx, baseModel)
	if err != nil {
		t.Fatalf("SyncForModel failed: %v", err)
	}

	InvalidateFailoverCache()
	group, err := repo.GetByModel(ctx, baseModel)
	if err != nil {
		t.Fatalf("Failed to get group after sync: %v", err)
	}
	if group.Description != description {
		t.Errorf("Expected description %q, got %q", description, group.Description)
	}
}

func TestSyncForModel_UpsertError(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	baseModel := "test-syncformodel-upsert-error-" + uuid.New().String()[:8]
	provider1Name := "test-provider-1-" + uuid.New().String()[:8]
	provider2Name := "test-provider-2-" + uuid.New().String()[:8]

	provider1ID := uuid.New()
	provider2ID := uuid.New()
	model1ID := uuid.New()
	model2ID := uuid.New()

	_, err := testDB.Pool().Exec(ctx, `
		INSERT INTO providers (id, name, base_url, encrypted_key, key_nonce, key_salt, enabled, created_at)
		VALUES ($1, $2, 'http://localhost:11434', 'dGVzdA==', 'dGVzdA==', 'dGVzdA==', true, now())
	`, provider1ID, provider1Name)
	if err != nil {
		t.Fatalf("Failed to insert provider1: %v", err)
	}
	defer func() {
		_, _ = testDB.Pool().Exec(ctx, "DELETE FROM providers WHERE id = $1", provider1ID)
	}()

	_, err = testDB.Pool().Exec(ctx, `
		INSERT INTO providers (id, name, base_url, encrypted_key, key_nonce, key_salt, enabled, created_at)
		VALUES ($1, $2, 'http://localhost:11434', 'dGVzdA==', 'dGVzdA==', 'dGVzdA==', true, now())
	`, provider2ID, provider2Name)
	if err != nil {
		t.Fatalf("Failed to insert provider2: %v", err)
	}
	defer func() {
		_, _ = testDB.Pool().Exec(ctx, "DELETE FROM providers WHERE id = $1", provider2ID)
	}()

	_, err = testDB.Pool().Exec(ctx, `
		INSERT INTO models (id, model_id, provider_id, enabled, created_at)
		VALUES ($1, $2, $3, true, now())
	`, model1ID, baseModel, provider1ID)
	if err != nil {
		t.Fatalf("Failed to insert model1: %v", err)
	}
	defer func() {
		_, _ = testDB.Pool().Exec(ctx, "DELETE FROM models WHERE id = $1", model1ID)
	}()

	_, err = testDB.Pool().Exec(ctx, `
		INSERT INTO models (id, model_id, provider_id, enabled, created_at)
		VALUES ($1, $2, $3, true, now())
	`, model2ID, baseModel, provider2ID)
	if err != nil {
		t.Fatalf("Failed to insert model2: %v", err)
	}
	defer func() {
		_, _ = testDB.Pool().Exec(ctx, "DELETE FROM models WHERE id = $1", model2ID)
	}()

	origMarshal := jsonMarshal
	defer func() { jsonMarshal = origMarshal }()

	callCount := 0
	jsonMarshal = func(v any) ([]byte, error) {
		callCount++
		if callCount == 1 {
			return nil, fmt.Errorf("test marshal error")
		}
		return origMarshal(v)
	}

	_, err = repo.SyncForModel(ctx, baseModel)
	if err == nil {
		t.Error("SyncForModel should return error when UpsertWithConfig fails")
	}

	_ = repo.Delete(ctx, baseModel)
}

// TestRepository_SyncForModel_QueryError tests the rows.Err() guard in SyncForModel
// when a closed pool causes the rows iteration to fail.
func TestRepository_SyncForModel_QueryError(t *testing.T) {
	// Create a closed pool to trigger query errors
	closedPool, err := pgxpool.New(context.Background(), os.Getenv("DATABASE_URL"))
	if err != nil {
		t.Fatalf("Failed to create pool: %v", err)
	}
	closedPool.Close()

	repo := NewRepository(closedPool)
	ctx := context.Background()

	// SyncForModel should return an error from the query/rows.Err()
	_, err = repo.SyncForModel(ctx, "gpt-4o-mini")
	if err == nil {
		t.Error("Expected SyncForModel to return error with closed pool")
	}
}

// TestRepository_SyncForModel_ReportsMembershipChanges verifies the SyncResult
// returned by SyncForModel: UpdatedGroups carries removed/added model UUIDs,
// a no-change sync returns an empty result, and dropping to a single enabled
// member reports the group deletion.
func TestRepository_SyncForModel_ReportsMembershipChanges(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	baseModel := "test-syncresult-" + uuid.New().String()[:8]

	providerIDs := make([]uuid.UUID, 3)
	modelIDs := make([]uuid.UUID, 3)
	for i := range providerIDs {
		providerIDs[i] = uuid.New()
		modelIDs[i] = uuid.New()
		providerName := fmt.Sprintf("test-syncresult-prov-%d-%s", i, uuid.New().String()[:8])
		_, err := testDB.Pool().Exec(ctx, `
			INSERT INTO providers (id, name, base_url, encrypted_key, key_nonce, key_salt, enabled, created_at)
			VALUES ($1, $2, 'http://localhost:11434', 'dGVzdA==', 'dGVzdA==', 'dGVzdA==', true, now())
		`, providerIDs[i], providerName)
		if err != nil {
			t.Fatalf("Failed to insert provider %d: %v", i, err)
		}
		pid := providerIDs[i]
		defer func() {
			_, _ = testDB.Pool().Exec(ctx, "DELETE FROM providers WHERE id = $1", pid)
		}()

		_, err = testDB.Pool().Exec(ctx, `
			INSERT INTO models (id, model_id, provider_id, enabled, created_at)
			VALUES ($1, $2, $3, true, now())
		`, modelIDs[i], baseModel, providerIDs[i])
		if err != nil {
			t.Fatalf("Failed to insert model %d: %v", i, err)
		}
		mid := modelIDs[i]
		defer func() {
			_, _ = testDB.Pool().Exec(ctx, "DELETE FROM models WHERE id = $1", mid)
		}()
	}
	defer func() {
		InvalidateFailoverCache()
		if existing, _ := repo.GetByModel(ctx, baseModel); existing != nil {
			_ = repo.Delete(ctx, baseModel)
		}
	}()

	// First sync creates the group; the creation is reported as an update
	// with every member listed as added.
	result, err := repo.SyncForModel(ctx, baseModel)
	if err != nil {
		t.Fatalf("SyncForModel (create) failed: %v", err)
	}
	if len(result.DeletedGroups) != 0 {
		t.Errorf("expected no deleted groups on creation, got %+v", result.DeletedGroups)
	}
	if len(result.UpdatedGroups) != 1 {
		t.Fatalf("expected 1 updated group on creation, got %+v", result.UpdatedGroups)
	}
	created := result.UpdatedGroups[0]
	if created.DisplayModel != baseModel {
		t.Errorf("expected created group %q, got %q", baseModel, created.DisplayModel)
	}
	if len(created.AddedModelIDs) != 3 || len(created.RemovedModelIDs) != 0 {
		t.Errorf("expected all 3 members added and none removed on creation, got %+v", created)
	}

	// No-change sync returns an empty result.
	InvalidateFailoverCache()
	result, err = repo.SyncForModel(ctx, baseModel)
	if err != nil {
		t.Fatalf("SyncForModel (no-op) failed: %v", err)
	}
	if len(result.UpdatedGroups) != 0 || len(result.DeletedGroups) != 0 {
		t.Errorf("expected empty result on no-change sync, got %+v", result)
	}

	// Disable one member: the next sync must report its UUID as removed.
	if _, err := testDB.Pool().Exec(ctx, `UPDATE models SET enabled = false WHERE id = $1`, modelIDs[2]); err != nil {
		t.Fatalf("Failed to disable model: %v", err)
	}
	InvalidateFailoverCache()
	result, err = repo.SyncForModel(ctx, baseModel)
	if err != nil {
		t.Fatalf("SyncForModel (member disabled) failed: %v", err)
	}
	if len(result.UpdatedGroups) != 1 {
		t.Fatalf("expected 1 updated group, got %+v", result)
	}
	updated := result.UpdatedGroups[0]
	if updated.DisplayModel != baseModel {
		t.Errorf("expected updated group %q, got %q", baseModel, updated.DisplayModel)
	}
	if len(updated.RemovedModelIDs) != 1 || updated.RemovedModelIDs[0] != modelIDs[2].String() {
		t.Errorf("expected removed model %s, got %v", modelIDs[2], updated.RemovedModelIDs)
	}
	if len(updated.AddedModelIDs) != 0 {
		t.Errorf("expected no added models, got %v", updated.AddedModelIDs)
	}

	// Re-enable the member: the next sync must report its UUID as added.
	if _, err := testDB.Pool().Exec(ctx, `UPDATE models SET enabled = true WHERE id = $1`, modelIDs[2]); err != nil {
		t.Fatalf("Failed to re-enable model: %v", err)
	}
	InvalidateFailoverCache()
	result, err = repo.SyncForModel(ctx, baseModel)
	if err != nil {
		t.Fatalf("SyncForModel (member re-enabled) failed: %v", err)
	}
	if len(result.UpdatedGroups) != 1 {
		t.Fatalf("expected 1 updated group after re-enable, got %+v", result)
	}
	updated = result.UpdatedGroups[0]
	if len(updated.AddedModelIDs) != 1 || updated.AddedModelIDs[0] != modelIDs[2].String() {
		t.Errorf("expected added model %s, got %v", modelIDs[2], updated.AddedModelIDs)
	}

	// Disable two members: the group drops to one enabled member and the
	// sync must report the deletion.
	if _, err := testDB.Pool().Exec(ctx, `UPDATE models SET enabled = false WHERE id = ANY($1)`, []uuid.UUID{modelIDs[1], modelIDs[2]}); err != nil {
		t.Fatalf("Failed to disable models: %v", err)
	}
	InvalidateFailoverCache()
	result, err = repo.SyncForModel(ctx, baseModel)
	if err != nil {
		t.Fatalf("SyncForModel (drop to one) failed: %v", err)
	}
	if len(result.DeletedGroups) != 1 {
		t.Fatalf("expected 1 deleted group, got %+v", result)
	}
	deleted := result.DeletedGroups[0]
	if deleted.DisplayModel != baseModel {
		t.Errorf("expected deleted group %q, got %q", baseModel, deleted.DisplayModel)
	}
	if deleted.Reason != "only 1 enabled provider (need 2+ for failover)" {
		t.Errorf("unexpected deletion reason %q", deleted.Reason)
	}
	if deleted.ProviderCount != 1 {
		t.Errorf("expected provider count 1, got %d", deleted.ProviderCount)
	}
}
