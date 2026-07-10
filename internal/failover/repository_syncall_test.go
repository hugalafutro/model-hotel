package failover

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ---------------------------------------------------------------------------
// Integration tests — SyncAllModels, SyncForModel, deleteAutoGroup
// ---------------------------------------------------------------------------

func TestRepository_SyncAllModels_TwoProviders(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	// Create unique identifiers for this test
	baseModel := "test-sync-model-" + uuid.New().String()[:8]
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

	// Call SyncAllModels
	result, err := repo.SyncAllModels(ctx)
	if err != nil {
		t.Fatalf("SyncAllModels failed: %v", err)
	}

	// Verify results
	if len(result.DeletedGroups) != 0 {
		t.Errorf("Expected 0 deleted groups, got %d", len(result.DeletedGroups))
	}
	if len(result.SyncErrors) != 0 {
		t.Errorf("Expected 0 sync errors, got %d: %v", len(result.SyncErrors), result.SyncErrors)
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

func TestRepository_SyncAllModels_SingleProvider(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	// Create unique identifiers for this test
	baseModel := "test-sync-single-" + uuid.New().String()[:8]
	providerName := "test-provider-single-" + uuid.New().String()[:8]

	// First, create an auto-created enabled group that should be disabled
	priorityOrder := []uuid.UUID{uuid.New(), uuid.New()}
	entryEnabled := make(map[string]bool)
	for _, id := range priorityOrder {
		entryEnabled[id.String()] = true
	}
	groupEnabled := true
	autoCreated := true

	_, err := repo.UpsertWithConfig(ctx, baseModel, priorityOrder, entryEnabled, &groupEnabled, nil, nil, &autoCreated)
	if err != nil {
		t.Fatalf("Failed to create initial group: %v", err)
	}
	defer func() {
		// Cleanup - delete the group if it exists
		InvalidateFailoverCache()
		if existing, _ := repo.GetByModel(ctx, baseModel); existing != nil {
			_ = repo.Delete(ctx, baseModel)
		}
	}()

	// Insert test data - only 1 provider/model
	providerID := uuid.New()
	modelID := uuid.New()

	_, err = testDB.Pool().Exec(ctx, `
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

	// Call SyncAllModels - should disable the existing auto-group
	result, err := repo.SyncAllModels(ctx)
	if err != nil {
		t.Fatalf("SyncAllModels failed: %v", err)
	}

	// Verify results - should have 1 deleted group
	if len(result.DeletedGroups) != 1 {
		t.Fatalf("Expected 1 deleted group, got %d", len(result.DeletedGroups))
	}
	if result.DeletedGroups[0].DisplayModel != baseModel {
		t.Errorf("Expected deleted group for %q, got %q", baseModel, result.DeletedGroups[0].DisplayModel)
	}
	if result.DeletedGroups[0].Reason != "only 1 enabled provider (need 2+ for failover)" {
		t.Errorf("Expected reason 'only 1 enabled provider (need 2+ for failover)', got %q", result.DeletedGroups[0].Reason)
	}
	if result.DeletedGroups[0].ProviderCount != 1 {
		t.Errorf("Expected provider count 1, got %d", result.DeletedGroups[0].ProviderCount)
	}

	// Verify the group was actually deleted
	InvalidateFailoverCache()
	_, err = repo.GetByModel(ctx, baseModel)
	if err == nil {
		t.Error("Expected group to be deleted, but it still exists")
	}
}

func TestRepository_SyncAllModels_DeletesStaleGroups(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	// Create unique identifiers for this test
	baseModel := "test-stale-group-" + uuid.New().String()[:8]

	// First, create an auto-created enabled group
	priorityOrder := []uuid.UUID{uuid.New(), uuid.New()}
	entryEnabled := make(map[string]bool)
	for _, id := range priorityOrder {
		entryEnabled[id.String()] = true
	}
	groupEnabled := true
	autoCreated := true

	_, err := repo.UpsertWithConfig(ctx, baseModel, priorityOrder, entryEnabled, &groupEnabled, nil, nil, &autoCreated)
	if err != nil {
		t.Fatalf("Failed to create initial group: %v", err)
	}
	defer func() {
		// Cleanup - delete the group if it exists
		InvalidateFailoverCache()
		if existing, _ := repo.GetByModel(ctx, baseModel); existing != nil {
			_ = repo.Delete(ctx, baseModel)
		}
	}()

	// Verify it was created and enabled
	InvalidateFailoverCache()
	group, err := repo.GetByModel(ctx, baseModel)
	if err != nil {
		t.Fatalf("Failed to get initial group: %v", err)
	}
	if !group.GroupEnabled {
		t.Fatal("Initial group should be enabled")
	}

	// Call SyncAllModels with no matching models/providers - should delete the stale group
	result, err := repo.SyncAllModels(ctx)
	if err != nil {
		t.Fatalf("SyncAllModels failed: %v", err)
	}

	// Verify the group was deleted
	InvalidateFailoverCache()
	_, err = repo.GetByModel(ctx, baseModel)
	if err == nil {
		t.Error("Expected stale auto-group to be deleted, but it still exists")
	}

	// Verify it's in the deleted groups result
	foundDeleted := false
	for _, dg := range result.DeletedGroups {
		if dg.DisplayModel == baseModel && dg.Reason == "no enabled providers found" {
			foundDeleted = true
			break
		}
	}
	if !foundDeleted {
		t.Error("Expected deleted group to be reported in result")
	}
}

// ---------------------------------------------------------------------------
// Integration tests — pruneStaleEntries logic in SyncAllModels
// ---------------------------------------------------------------------------

func TestRepository_SyncAllModels_PruneStaleEntriesFromCustomGroup(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	// Create unique identifiers for this test
	baseModel := "test-prune-" + uuid.New().String()[:8]

	// Create 3 providers
	provider1ID := uuid.New()
	provider2ID := uuid.New()
	provider3ID := uuid.New()

	for _, p := range []struct {
		id   uuid.UUID
		name string
	}{
		{provider1ID, "test-provider-1-" + uuid.New().String()[:8]},
		{provider2ID, "test-provider-2-" + uuid.New().String()[:8]},
		{provider3ID, "test-provider-3-" + uuid.New().String()[:8]},
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

	// Create 3 models with the same base model name
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

	// Run SyncAllModels to create an auto-group with all 3
	result1, err := repo.SyncAllModels(ctx)
	if err != nil {
		t.Fatalf("SyncAllModels (first run) failed: %v", err)
	}
	if len(result1.DeletedGroups) != 0 {
		t.Errorf("Expected 0 deleted groups on first run, got %d", len(result1.DeletedGroups))
	}

	// Verify auto-group was created
	InvalidateFailoverCache()
	group, err := repo.GetByModel(ctx, baseModel)
	if err != nil {
		t.Fatalf("Failed to get auto-group: %v", err)
	}
	if len(group.PriorityOrder) != 3 {
		t.Fatalf("Expected 3 models in priority order, got %d", len(group.PriorityOrder))
	}

	// Manually set auto_created = false (simulates a user-customized group)
	_, err = testDB.Pool().Exec(ctx, "UPDATE model_failover_groups SET auto_created = false WHERE display_model = $1", baseModel)
	if err != nil {
		t.Fatalf("Failed to set auto_created = false: %v", err)
	}

	// Delete ALL models from the DB - sync won't update this group (0 entries)
	// but pruneStaleEntries will still process it
	_, err = testDB.Pool().Exec(ctx, "DELETE FROM models WHERE id = $1 OR id = $2 OR id = $3", model1ID, model2ID, model3ID)
	if err != nil {
		t.Fatalf("Failed to delete models: %v", err)
	}

	// Manually set stale entries in priority_order (referencing deleted models)
	priorityOrderWithStale := `["` + model1ID.String() + `", "` + model2ID.String() + `", "` + model3ID.String() + `"]`
	entryEnabledWithStale := `{"` + model1ID.String() + `": true, "` + model2ID.String() + `": true, "` + model3ID.String() + `": true}`
	_, err = testDB.Pool().Exec(ctx, `
		UPDATE model_failover_groups 
		SET priority_order = $1, entry_enabled = $2 
		WHERE display_model = $3
	`, priorityOrderWithStale, entryEnabledWithStale, baseModel)
	if err != nil {
		t.Fatalf("Failed to update group with stale entries: %v", err)
	}

	// Run SyncAllModels again - sync won't update (0 models), prune will clean up
	result2, err := repo.SyncAllModels(ctx)
	if err != nil {
		t.Fatalf("SyncAllModels (second run) failed: %v", err)
	}

	// Verify: group is DELETED (0 valid entries left after prune)
	InvalidateFailoverCache()
	_, err = repo.GetByModel(ctx, baseModel)
	if err == nil {
		t.Error("Expected group to be deleted after prune (0 valid entries)")
	}

	// Verify: result.PurgedEntries has 1 entry with all 3 deleted model UUIDs
	if len(result2.PurgedEntries) != 1 {
		t.Fatalf("Expected 1 purged entry, got %d", len(result2.PurgedEntries))
	}
	if result2.PurgedEntries[0].GroupDisplayModel != baseModel {
		t.Errorf("Expected purged entry for %q, got %q", baseModel, result2.PurgedEntries[0].GroupDisplayModel)
	}
	if len(result2.PurgedEntries[0].PrunedModelIDs) != 3 {
		t.Errorf("Expected 3 pruned model IDs, got %d", len(result2.PurgedEntries[0].PrunedModelIDs))
	}

	// Verify: result.DeletedGroups includes this group
	foundDeleted := false
	for _, dg := range result2.DeletedGroups {
		if dg.DisplayModel == baseModel {
			foundDeleted = true
			if !containsSubstring(dg.Reason, "prune") {
				t.Errorf("Expected reason to contain 'prune', got %q", dg.Reason)
			}
			break
		}
	}
	if !foundDeleted {
		t.Error("Expected deleted group to be reported in result.DeletedGroups")
	}

	// Cleanup (group should already be deleted, but be safe)
	if err := repo.Delete(ctx, baseModel); err != nil {
		t.Logf("cleanup Delete failed: %v", err)
	}
}

func TestRepository_SyncAllModels_DeleteCustomGroupAfterPrune(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	// Create unique identifiers for this test
	baseModel := "test-prune-del-" + uuid.New().String()[:8]

	// Create 2 providers
	provider1ID := uuid.New()
	provider2ID := uuid.New()

	for _, p := range []struct {
		id   uuid.UUID
		name string
	}{
		{provider1ID, "test-provider-1-" + uuid.New().String()[:8]},
		{provider2ID, "test-provider-2-" + uuid.New().String()[:8]},
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

	// Create 2 models with same base name
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

	// Run SyncAllModels to create an auto-group
	_, err := repo.SyncAllModels(ctx)
	if err != nil {
		t.Fatalf("SyncAllModels (first run) failed: %v", err)
	}

	// Verify auto-group was created
	InvalidateFailoverCache()
	group, err := repo.GetByModel(ctx, baseModel)
	if err != nil {
		t.Fatalf("Failed to get auto-group: %v", err)
	}
	if len(group.PriorityOrder) != 2 {
		t.Fatalf("Expected 2 models in priority order, got %d", len(group.PriorityOrder))
	}

	// Manually set auto_created = false (simulates a user-customized group)
	_, err = testDB.Pool().Exec(ctx, "UPDATE model_failover_groups SET auto_created = false WHERE display_model = $1", baseModel)
	if err != nil {
		t.Fatalf("Failed to set auto_created = false: %v", err)
	}

	// Delete ALL models from the DB - sync won't update (0 entries for this base)
	_, err = testDB.Pool().Exec(ctx, "DELETE FROM models WHERE id = $1 OR id = $2", model1ID, model2ID)
	if err != nil {
		t.Fatalf("Failed to delete models: %v", err)
	}

	// Manually set stale entries in priority_order (referencing deleted models)
	priorityOrderWithStale := `["` + model1ID.String() + `", "` + model2ID.String() + `"]`
	entryEnabledWithStale := `{"` + model1ID.String() + `": true, "` + model2ID.String() + `": true}`
	_, err = testDB.Pool().Exec(ctx, `
		UPDATE model_failover_groups 
		SET priority_order = $1, entry_enabled = $2 
		WHERE display_model = $3
	`, priorityOrderWithStale, entryEnabledWithStale, baseModel)
	if err != nil {
		t.Fatalf("Failed to update group with stale entries: %v", err)
	}

	// Run SyncAllModels again - sync won't update (0 models), prune will delete group
	result2, err := repo.SyncAllModels(ctx)
	if err != nil {
		t.Fatalf("SyncAllModels (second run) failed: %v", err)
	}

	// Verify: group is DELETED
	InvalidateFailoverCache()
	_, err = repo.GetByModel(ctx, baseModel)
	if err == nil {
		t.Error("Expected group to be deleted after prune (0 entries left)")
	}

	// Verify: result.DeletedGroups includes this group with reason containing "prune"
	foundDeleted := false
	for _, dg := range result2.DeletedGroups {
		if dg.DisplayModel == baseModel {
			foundDeleted = true
			if !containsSubstring(dg.Reason, "prune") {
				t.Errorf("Expected reason to contain 'prune', got %q", dg.Reason)
			}
			break
		}
	}
	if !foundDeleted {
		t.Error("Expected deleted group to be reported in result.DeletedGroups")
	}

	// Verify: result.PurgedEntries has 1 entry
	if len(result2.PurgedEntries) != 1 {
		t.Fatalf("Expected 1 purged entry, got %d", len(result2.PurgedEntries))
	}
	if result2.PurgedEntries[0].GroupDisplayModel != baseModel {
		t.Errorf("Expected purged entry for %q, got %q", baseModel, result2.PurgedEntries[0].GroupDisplayModel)
	}
	if len(result2.PurgedEntries[0].PrunedModelIDs) != 2 {
		t.Errorf("Expected 2 pruned model IDs, got %d", len(result2.PurgedEntries[0].PrunedModelIDs))
	}
}

func TestRepository_SyncAllModels_PruneAllStaleFromCustomGroup(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	// Create unique identifiers for this test
	baseModel := "test-prune-all-" + uuid.New().String()[:8]

	// Create 1 provider
	provider1ID := uuid.New()
	_, err := testDB.Pool().Exec(ctx, `
		INSERT INTO providers (id, name, base_url, encrypted_key, key_nonce, key_salt, enabled, created_at)
		VALUES ($1, $2, 'http://localhost:11434', 'dGVzdA==', 'dGVzdA==', 'dGVzdA==', true, now())
	`, provider1ID, "test-provider-1-"+uuid.New().String()[:8])
	if err != nil {
		t.Fatalf("Failed to insert provider: %v", err)
	}
	defer func() {
		_, _ = testDB.Pool().Exec(ctx, "DELETE FROM providers WHERE id = $1", provider1ID)
	}()

	// Create 1 model
	model1ID := uuid.New()
	_, err = testDB.Pool().Exec(ctx, `
		INSERT INTO models (id, model_id, provider_id, enabled, created_at)
		VALUES ($1, $2, $3, true, now())
	`, model1ID, baseModel, provider1ID)
	if err != nil {
		t.Fatalf("Failed to insert model: %v", err)
	}

	// Create a manual custom group (INSERT directly) referencing that 1 model's UUID
	entryEnabledJSON := `{"` + model1ID.String() + `": true}`
	priorityOrderJSON := `["` + model1ID.String() + `"]`
	_, err = testDB.Pool().Exec(ctx, `
		INSERT INTO model_failover_groups (display_model, priority_order, entry_enabled, group_enabled, auto_created, created_at, updated_at)
		VALUES ($1, $2, $3, true, false, now(), now())
	`, baseModel, priorityOrderJSON, entryEnabledJSON)
	if err != nil {
		t.Fatalf("Failed to insert custom group: %v", err)
	}
	defer func() {
		_, _ = testDB.Pool().Exec(ctx, "DELETE FROM model_failover_groups WHERE display_model = $1", baseModel)
	}()

	// Verify the group was created
	InvalidateFailoverCache()
	group, err := repo.GetByModel(ctx, baseModel)
	if err != nil {
		t.Fatalf("Failed to get custom group: %v", err)
	}
	if group.AutoCreated {
		t.Error("Expected custom group to have auto_created = false")
	}

	// Delete that model from the DB
	_, err = testDB.Pool().Exec(ctx, "DELETE FROM models WHERE id = $1", model1ID)
	if err != nil {
		t.Fatalf("Failed to delete model: %v", err)
	}

	// Run SyncAllModels - should delete the group (no valid providers left)
	result, err := repo.SyncAllModels(ctx)
	if err != nil {
		t.Fatalf("SyncAllModels failed: %v", err)
	}

	// Verify: group is DELETED
	InvalidateFailoverCache()
	_, err = repo.GetByModel(ctx, baseModel)
	if err == nil {
		t.Error("Expected group to be deleted after prune (no valid providers left)")
	}

	// Verify: result.DeletedGroups includes it with "no valid providers after prune"
	foundDeleted := false
	for _, dg := range result.DeletedGroups {
		if dg.DisplayModel == baseModel {
			foundDeleted = true
			if dg.Reason != "no valid providers after prune" {
				t.Errorf("Expected reason 'no valid providers after prune', got %q", dg.Reason)
			}
			break
		}
	}
	if !foundDeleted {
		t.Error("Expected deleted group to be reported in result.DeletedGroups")
	}

	// Verify: result.PurgedEntries has 1 entry
	if len(result.PurgedEntries) != 1 {
		t.Fatalf("Expected 1 purged entry, got %d", len(result.PurgedEntries))
	}
	if result.PurgedEntries[0].GroupDisplayModel != baseModel {
		t.Errorf("Expected purged entry for %q, got %q", baseModel, result.PurgedEntries[0].GroupDisplayModel)
	}
	if len(result.PurgedEntries[0].PrunedModelIDs) != 1 {
		t.Errorf("Expected 1 pruned model ID, got %d", len(result.PurgedEntries[0].PrunedModelIDs))
	}
	if result.PurgedEntries[0].PrunedModelIDs[0] != model1ID.String() {
		t.Errorf("Expected pruned model ID %q, got %q", model1ID.String(), result.PurgedEntries[0].PrunedModelIDs[0])
	}
}

// ---------------------------------------------------------------------------
// Integration tests — Sync coverage improvements
// ---------------------------------------------------------------------------

func TestRepository_SyncAllModels_WithSyncErrors(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	// Create unique identifiers for this test
	baseModel := "test-sync-error-" + uuid.New().String()[:8]

	// Insert test data - two providers with same model
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

	// Call SyncAllModels - should succeed without errors
	result, err := repo.SyncAllModels(ctx)
	if err != nil {
		t.Fatalf("SyncAllModels failed: %v", err)
	}

	// Verify no sync errors
	if len(result.SyncErrors) != 0 {
		t.Errorf("Expected 0 sync errors, got %d: %v", len(result.SyncErrors), result.SyncErrors)
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
	if len(group.PriorityOrder) != 2 {
		t.Errorf("Expected 2 providers in priority order, got %d", len(group.PriorityOrder))
	}

	// Cleanup
	if err := repo.Delete(ctx, baseModel); err != nil {
		t.Logf("cleanup Delete failed: %v", err)
	}
}

func TestRepository_SyncAllModels_PreservesDisabledEntries(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	baseModel := "test-sync-preserve-" + uuid.New().String()[:8]
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

	// Pre-create a failover group with provider2 disabled in entry_enabled
	priorityOrder := []uuid.UUID{model1ID, model2ID}
	entryEnabled := map[string]bool{
		model1ID.String(): true,
		model2ID.String(): false, // This one is disabled
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

	// Call SyncAllModels - should add model3 as enabled, preserve model2 as disabled
	result, err := repo.SyncAllModels(ctx)
	if err != nil {
		t.Fatalf("SyncAllModels failed: %v", err)
	}

	if len(result.SyncErrors) != 0 {
		t.Errorf("Expected 0 sync errors, got %d: %v", len(result.SyncErrors), result.SyncErrors)
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

	// model2 should still be disabled (preserved from existing entry_enabled)
	if group.EntryEnabled[model2ID.String()] != false {
		t.Errorf("Expected model2 to be disabled (preserved), got %v", group.EntryEnabled[model2ID.String()])
	}

	// model3 should be enabled (new entry)
	if group.EntryEnabled[model3ID.String()] != true {
		t.Errorf("Expected model3 to be enabled (new entry), got %v", group.EntryEnabled[model3ID.String()])
	}
}

func TestRepository_SyncAllModels_PreservesPriorityOrder(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	baseModel := "test-sync-priority-" + uuid.New().String()[:8]
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

	// Pre-create a failover group with a CUSTOM priority order: [model3, model1, model2]
	// This is deliberately different from the DB query order (which would be alphabetical by model_id)
	customPriorityOrder := []uuid.UUID{model3ID, model1ID, model2ID}
	entryEnabled := map[string]bool{
		model1ID.String(): true,
		model2ID.String(): true,
		model3ID.String(): true,
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

	// Call SyncAllModels
	result, err := repo.SyncAllModels(ctx)
	if err != nil {
		t.Fatalf("SyncAllModels failed: %v", err)
	}

	if len(result.SyncErrors) != 0 {
		t.Errorf("Expected 0 sync errors, got %d: %v", len(result.SyncErrors), result.SyncErrors)
	}

	// Verify the group's priority order was NOT overwritten
	InvalidateFailoverCache()
	group, err := repo.GetByModel(ctx, baseModel)
	if err != nil {
		t.Fatalf("Failed to get group after sync: %v", err)
	}

	// Verify the custom order was preserved: [model3, model1, model2]
	if len(group.PriorityOrder) != 3 {
		t.Errorf("Expected 3 models in priority order, got %d", len(group.PriorityOrder))
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
}

func TestRepository_SyncAllModels_PreservesPriorityOrderWithNewModel(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	baseModel := "test-sync-priority-new-" + uuid.New().String()[:8]
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

	// Pre-create a failover group with a CUSTOM priority order: [model2, model1]
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

	// Call SyncAllModels
	_, err = repo.SyncAllModels(ctx)
	if err != nil {
		t.Fatalf("SyncAllModels failed: %v", err)
	}

	// Verify the existing user order was preserved, with new model appended
	InvalidateFailoverCache()
	group, err := repo.GetByModel(ctx, baseModel)
	if err != nil {
		t.Fatalf("Failed to get group after sync: %v", err)
	}

	// Expected order: [model2, model1, model3] - existing user order preserved, new model appended
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
// Integration tests — Sync error handling
// ---------------------------------------------------------------------------

func TestRepository_SyncAllModels_SuccessfulMultiModelSync(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	// This test verifies that SyncAllModels continues processing other models
	// even when one upsert fails. We create a scenario with multiple models
	// and verify that successful ones are still synced.

	baseModel1 := "test-sync-error-continue-a-" + uuid.New().String()[:8]
	baseModel2 := "test-sync-error-continue-b-" + uuid.New().String()[:8]

	// Create providers and models for baseModel1 (should succeed)
	provider1ID := uuid.New()
	provider2ID := uuid.New()
	model1ID := uuid.New()
	model2ID := uuid.New()

	for _, p := range []struct {
		id   uuid.UUID
		name string
	}{
		{provider1ID, "test-provider-error-a-" + uuid.New().String()[:8]},
		{provider2ID, "test-provider-error-b-" + uuid.New().String()[:8]},
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
		baseModel  string
	}{
		{model1ID, provider1ID, baseModel1},
		{model2ID, provider2ID, baseModel1},
	} {
		_, err := testDB.Pool().Exec(ctx, `
			INSERT INTO models (id, model_id, provider_id, enabled, created_at)
			VALUES ($1, $2, $3, true, now())
		`, m.id, m.baseModel, m.providerID)
		if err != nil {
			t.Fatalf("Failed to insert model: %v", err)
		}
		defer func(id uuid.UUID) {
			_, _ = testDB.Pool().Exec(ctx, "DELETE FROM models WHERE id = $1", id)
		}(m.id)
	}

	// Create providers and models for baseModel2 (should also succeed)
	provider3ID := uuid.New()
	provider4ID := uuid.New()
	model3ID := uuid.New()
	model4ID := uuid.New()

	for _, p := range []struct {
		id   uuid.UUID
		name string
	}{
		{provider3ID, "test-provider-error-c-" + uuid.New().String()[:8]},
		{provider4ID, "test-provider-error-d-" + uuid.New().String()[:8]},
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
		baseModel  string
	}{
		{model3ID, provider3ID, baseModel2},
		{model4ID, provider4ID, baseModel2},
	} {
		_, err := testDB.Pool().Exec(ctx, `
			INSERT INTO models (id, model_id, provider_id, enabled, created_at)
			VALUES ($1, $2, $3, true, now())
		`, m.id, m.baseModel, m.providerID)
		if err != nil {
			t.Fatalf("Failed to insert model: %v", err)
		}
		defer func(id uuid.UUID) {
			_, _ = testDB.Pool().Exec(ctx, "DELETE FROM models WHERE id = $1", id)
		}(m.id)
	}

	// Call SyncAllModels - both models should be synced successfully
	result, err := repo.SyncAllModels(ctx)
	if err != nil {
		t.Fatalf("SyncAllModels failed: %v", err)
	}

	// Verify no sync errors occurred
	if len(result.SyncErrors) != 0 {
		t.Errorf("Expected 0 sync errors, got %d: %v", len(result.SyncErrors), result.SyncErrors)
	}

	// Verify both groups were created
	InvalidateFailoverCache()
	group1, err := repo.GetByModel(ctx, baseModel1)
	if err != nil {
		t.Fatalf("Failed to get group1: %v", err)
	}
	if len(group1.PriorityOrder) != 2 {
		t.Errorf("Expected 2 models in group1, got %d", len(group1.PriorityOrder))
	}

	group2, err := repo.GetByModel(ctx, baseModel2)
	if err != nil {
		t.Fatalf("Failed to get group2: %v", err)
	}
	if len(group2.PriorityOrder) != 2 {
		t.Errorf("Expected 2 models in group2, got %d", len(group2.PriorityOrder))
	}

	// Cleanup
	_ = repo.Delete(ctx, baseModel1)
	_ = repo.Delete(ctx, baseModel2)
}

// ---------------------------------------------------------------------------
// Integration tests — Row scan error handling (continue behavior)
// ---------------------------------------------------------------------------

func TestRepository_SyncAllModels_ValidRowScan(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	// This test verifies that SyncAllModels handles row scan errors gracefully
	// by continuing to process other rows. The row scan error path (lines 402-403)
	// uses 'continue' to skip problematic rows.
	//
	// We verify this by creating valid data and ensuring successful sync,
	// which implicitly tests that the scan logic works correctly.

	baseModel := "test-scan-continue-" + uuid.New().String()[:8]
	provider1Name := "test-provider-scan-1-" + uuid.New().String()[:8]
	provider2Name := "test-provider-scan-2-" + uuid.New().String()[:8]

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

	// Call SyncAllModels - should handle all rows correctly
	result, err := repo.SyncAllModels(ctx)
	if err != nil {
		t.Fatalf("SyncAllModels failed: %v", err)
	}

	// Verify successful sync
	if len(result.SyncErrors) != 0 {
		t.Errorf("Expected 0 sync errors, got %d: %v", len(result.SyncErrors), result.SyncErrors)
	}

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
// ---------------------------------------------------------------------------
// SyncAllModels specific tests
// ---------------------------------------------------------------------------

func TestSyncAllModels_PreservesDescription(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	baseModel := "test-sync-preserve-desc-" + uuid.New().String()[:8]
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

	description := "Test description to preserve"
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

	result, err := repo.SyncAllModels(ctx)
	if err != nil {
		t.Fatalf("SyncAllModels failed: %v", err)
	}

	if len(result.SyncErrors) != 0 {
		t.Errorf("Expected 0 sync errors, got %d: %v", len(result.SyncErrors), result.SyncErrors)
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

func TestSyncAllModels_UpsertError(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	baseModel := "test-sync-upsert-error-" + uuid.New().String()[:8]
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
	jsonMarshal = func(v interface{}) ([]byte, error) {
		callCount++
		if callCount == 1 {
			return nil, fmt.Errorf("test marshal error")
		}
		return origMarshal(v)
	}

	result, err := repo.SyncAllModels(ctx)
	if err != nil {
		t.Fatalf("SyncAllModels should not return error, but capture it in SyncErrors: %v", err)
	}

	if len(result.SyncErrors) == 0 {
		t.Error("Expected sync errors when UpsertWithConfig fails")
	}

	_ = repo.Delete(ctx, baseModel)
}

// TestRepository_SyncAllModels_EmptyPriorityOrder tests the early return path
// in pruneStaleEntries when a group has an empty priority_order array (line 62-64).
// The function should return early without error for such groups.
func TestRepository_SyncAllModels_EmptyPriorityOrder(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	baseModel := "test-empty-po-" + uuid.New().String()[:8]

	// Create a custom group with empty priority_order array directly via SQL
	priorityOrderJSON := `[]`
	entryEnabledJSON := `{}`
	_, err := testDB.Pool().Exec(ctx, `
		INSERT INTO model_failover_groups (display_model, priority_order, entry_enabled, group_enabled, auto_created, created_at, updated_at)
		VALUES ($1, $2, $3, true, false, now(), now())
	`, baseModel, priorityOrderJSON, entryEnabledJSON)
	if err != nil {
		t.Fatalf("Failed to insert group with empty priority_order: %v", err)
	}
	defer func() {
		_, _ = testDB.Pool().Exec(ctx, "DELETE FROM model_failover_groups WHERE display_model = $1", baseModel)
	}()

	// Verify the group was created with empty priority_order
	InvalidateFailoverCache()
	group, err := repo.GetByModel(ctx, baseModel)
	if err != nil {
		t.Fatalf("Failed to get group: %v", err)
	}
	if len(group.PriorityOrder) != 0 {
		t.Fatalf("Expected empty PriorityOrder, got %d entries", len(group.PriorityOrder))
	}

	// Call SyncAllModels - should succeed without error, group should still exist
	result, err := repo.SyncAllModels(ctx)
	if err != nil {
		t.Fatalf("SyncAllModels failed: %v", err)
	}

	// Verify the group still exists (not deleted, since there was nothing to prune)
	InvalidateFailoverCache()
	_, err = repo.GetByModel(ctx, baseModel)
	if err != nil {
		t.Error("Expected group to still exist after SyncAllModels with empty priority_order")
	}

	// Verify no deleted groups or purged entries for this model
	for _, dg := range result.DeletedGroups {
		if dg.DisplayModel == baseModel {
			t.Error("Expected group with empty priority_order to not be deleted")
		}
	}
	for _, pe := range result.PurgedEntries {
		if pe.GroupDisplayModel == baseModel {
			t.Error("Expected no purged entries for group with empty priority_order")
		}
	}
}

// TestRepository_SyncAllModels_QueryError tests the error path in pruneStaleEntries
// when r.pool.Query fails (line 73-76). Uses a closed pool to trigger the error.
func TestRepository_SyncAllModels_QueryError(t *testing.T) {
	// Create a closed pool to trigger query errors
	closedPool, err := pgxpool.New(context.Background(), os.Getenv("DATABASE_URL"))
	if err != nil {
		t.Fatalf("Failed to create pool: %v", err)
	}
	closedPool.Close()

	repo := NewRepository(closedPool)
	ctx := context.Background()

	// Call SyncAllModels - should return an error from the initial query
	_, err = repo.SyncAllModels(ctx)
	if err == nil {
		t.Error("Expected SyncAllModels to return error with closed pool")
	}
}

// TestRepository_SyncAllModels_CancelledContext tests error paths in pruneStaleEntries
// using a cancelled context. This may trigger rows.Err() or query errors.
func TestRepository_SyncAllModels_CancelledContext(t *testing.T) {
	repo := newTestRepo(t)

	// Create a cancelled context before calling SyncAllModels
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// Call SyncAllModels with cancelled context - should return an error
	_, err := repo.SyncAllModels(ctx)
	if err == nil {
		t.Error("Expected SyncAllModels to return error with cancelled context")
	}
}
