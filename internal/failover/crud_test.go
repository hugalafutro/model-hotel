package failover

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ---------------------------------------------------------------------------
// NewRepository tests
// ---------------------------------------------------------------------------

func TestNewRepository(t *testing.T) {
	repo := NewRepository(nil)
	if repo == nil {
		t.Error("NewRepository(nil) should return non-nil Repository")
	}
}

func TestNewRepository_WithPool(t *testing.T) {

	repo := NewRepository(testDB.Pool())
	if repo == nil {
		t.Error("NewRepository with pool should return non-nil Repository")
	}
}

// ---------------------------------------------------------------------------
// Integration tests — Repository CRUD (requires PostgreSQL)
// ---------------------------------------------------------------------------
func TestRepository_CreateAndGetByModel(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	displayModel := "test-model-crud-" + uuid.New().String()[:8]
	po := []uuid.UUID{uuid.New(), uuid.New()}

	fg, err := repo.Upsert(ctx, displayModel, po)
	if err != nil {
		t.Fatalf("Upsert failed: %v", err)
	}

	if fg.ID == uuid.Nil {
		t.Error("Upsert returned nil ID")
	}
	if fg.DisplayModel != displayModel {
		t.Errorf("DisplayModel = %q, want %q", fg.DisplayModel, displayModel)
	}
	if len(fg.PriorityOrder) != 2 {
		t.Errorf("PriorityOrder length = %d, want 2", len(fg.PriorityOrder))
	}
	if fg.GroupEnabled != true {
		t.Errorf("GroupEnabled = %v, want true (default)", fg.GroupEnabled)
	}

	// Verify we can retrieve it
	InvalidateFailoverCache()
	found, err := repo.GetByModel(ctx, displayModel)
	if err != nil {
		t.Fatalf("GetByModel failed: %v", err)
	}
	if found.ID != fg.ID {
		t.Errorf("GetByModel ID = %v, want %v", found.ID, fg.ID)
	}

	// Cleanup
	if err := repo.Delete(ctx, displayModel); err != nil {
		t.Logf("cleanup Delete failed: %v", err)
	}
}

func TestRepository_GetByModel_NotFound(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	InvalidateFailoverCache()
	_, err := repo.GetByModel(ctx, "nonexistent-model-"+uuid.New().String())
	if err == nil {
		t.Error("GetByModel should return error for nonexistent model")
	}
}

func TestRepository_Delete(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	displayModel := "test-model-delete-" + uuid.New().String()[:8]
	po := []uuid.UUID{uuid.New(), uuid.New()}

	_, err := repo.Upsert(ctx, displayModel, po)
	if err != nil {
		t.Fatalf("Upsert failed: %v", err)
	}

	if err := repo.Delete(ctx, displayModel); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// Verify it's gone
	InvalidateFailoverCache()
	_, err = repo.GetByModel(ctx, displayModel)
	if err == nil {
		t.Error("GetByModel should return error after Delete")
	}
}

func TestRepository_DeleteByID(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	displayModel := "test-model-deletebyid-" + uuid.New().String()[:8]
	po := []uuid.UUID{uuid.New(), uuid.New()}

	fg, err := repo.Upsert(ctx, displayModel, po)
	if err != nil {
		t.Fatalf("Upsert failed: %v", err)
	}

	if err := repo.DeleteByID(ctx, fg.ID); err != nil {
		t.Fatalf("DeleteByID failed: %v", err)
	}

	// Verify it's gone
	InvalidateFailoverCache()
	_, err = repo.GetByModel(ctx, displayModel)
	if err == nil {
		t.Error("GetByModel should return error after DeleteByID")
	}
}

func TestRepository_List(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	displayModel := "test-model-list-" + uuid.New().String()[:8]
	po := []uuid.UUID{uuid.New(), uuid.New()}

	_, err := repo.Upsert(ctx, displayModel, po)
	if err != nil {
		t.Fatalf("Upsert failed: %v", err)
	}
	defer func() {
		_ = repo.Delete(ctx, displayModel)
	}()

	groups, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(groups) == 0 {
		t.Error("List should return at least one group")
	}

	found := false
	for _, g := range groups {
		if g.DisplayModel == displayModel {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("List did not include group with display_model %q", displayModel)
	}
}

func TestRepository_GetEnabled(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	displayModel := "test-model-getenabled-" + uuid.New().String()[:8]
	po := []uuid.UUID{uuid.New(), uuid.New()}

	fg, err := repo.Upsert(ctx, displayModel, po)
	if err != nil {
		t.Fatalf("Upsert failed: %v", err)
	}
	defer func() {
		_ = repo.Delete(ctx, displayModel)
	}()

	// By default, GroupEnabled is true
	if !fg.GroupEnabled {
		t.Error("Upsert should create group with GroupEnabled=true by default")
	}

	groups, err := repo.GetEnabled(ctx)
	if err != nil {
		t.Fatalf("GetEnabled failed: %v", err)
	}
	found := false
	for _, g := range groups {
		if g.ID == fg.ID {
			found = true
			if !g.GroupEnabled {
				t.Error("GetEnabled returned a group that is not enabled")
			}
			break
		}
	}
	if !found {
		t.Error("GetEnabled should include the newly created group")
	}
}

func TestRepository_Update(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	displayModel := "test-model-update-" + uuid.New().String()[:8]
	po := []uuid.UUID{uuid.New(), uuid.New()}

	fg, err := repo.Upsert(ctx, displayModel, po)
	if err != nil {
		t.Fatalf("Upsert failed: %v", err)
	}
	defer func() {
		_ = repo.Delete(ctx, displayModel)
	}()

	// Update priority order
	newPO := []uuid.UUID{po[1], po[0], uuid.New()}
	newEE := map[string]bool{po[0].String(): false, po[1].String(): true, newPO[2].String(): true}
	groupEnabled := false

	updated, err := repo.Update(ctx, fg.ID, newPO, newEE, &groupEnabled, nil, nil, nil)
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	if len(updated.PriorityOrder) != 3 {
		t.Errorf("PriorityOrder length = %d, want 3", len(updated.PriorityOrder))
	}
	if updated.GroupEnabled != false {
		t.Errorf("GroupEnabled = %v, want false", updated.GroupEnabled)
	}
	if updated.EntryEnabled[po[0].String()] != false {
		t.Errorf("EntryEnabled[%q] = %v, want false", po[0].String(), updated.EntryEnabled[po[0].String()])
	}
	if updated.EntryEnabled[po[1].String()] != true {
		t.Errorf("EntryEnabled[%q] = %v, want true", po[1].String(), updated.EntryEnabled[po[1].String()])
	}

	// Verify via GetByID
	InvalidateFailoverCache()
	found, err := repo.GetByID(ctx, fg.ID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}
	if len(found.PriorityOrder) != 3 {
		t.Errorf("GetByID PriorityOrder length = %d, want 3", len(found.PriorityOrder))
	}
	if found.GroupEnabled != false {
		t.Errorf("GetByID GroupEnabled = %v, want false", found.GroupEnabled)
	}
}

func TestRepository_DeleteByID_WithModels(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	// Create a failover group
	displayModel := "test-delete-cascade-" + uuid.New().String()[:8]
	po := []uuid.UUID{uuid.New(), uuid.New()}

	fg, err := repo.Upsert(ctx, displayModel, po)
	if err != nil {
		t.Fatalf("Upsert failed: %v", err)
	}

	// Verify it exists
	InvalidateFailoverCache()
	_, err = repo.GetByID(ctx, fg.ID)
	if err != nil {
		t.Fatalf("GetByID failed before delete: %v", err)
	}

	// Delete by ID
	err = repo.DeleteByID(ctx, fg.ID)
	if err != nil {
		t.Fatalf("DeleteByID failed: %v", err)
	}

	// Verify it's gone
	InvalidateFailoverCache()
	_, err = repo.GetByID(ctx, fg.ID)
	if err == nil {
		t.Error("GetByID should return error after DeleteByID")
	}

	// Verify delete is idempotent (doesn't error on already-deleted)
	err = repo.DeleteByID(ctx, fg.ID)
	if err != nil {
		t.Errorf("DeleteByID on already-deleted group should not error: %v", err)
	}
}

func TestRepository_Delete_WithNonExistentGroup(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	// Delete a non-existent group - should not error
	err := repo.Delete(ctx, "non-existent-model-"+uuid.New().String())
	if err != nil {
		t.Errorf("Delete on non-existent group should not error: %v", err)
	}
}

func TestRepository_Update_WithNilValues(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	displayModel := "test-update-nil-" + uuid.New().String()[:8]
	po := []uuid.UUID{uuid.New(), uuid.New()}

	fg, err := repo.Upsert(ctx, displayModel, po)
	if err != nil {
		t.Fatalf("Upsert failed: %v", err)
	}
	defer func() {
		_ = repo.Delete(ctx, displayModel)
	}()

	// Update with nil values - should preserve existing values
	updated, err := repo.Update(ctx, fg.ID, fg.PriorityOrder, fg.EntryEnabled, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("Update with nil values failed: %v", err)
	}

	// Verify values are preserved
	if updated.GroupEnabled != fg.GroupEnabled {
		t.Errorf("GroupEnabled changed from %v to %v", fg.GroupEnabled, updated.GroupEnabled)
	}
	if updated.DisplayName == nil && fg.DisplayName != nil {
		t.Error("DisplayName should be preserved when nil passed")
	}
	if updated.Description != fg.Description {
		t.Errorf("Description changed from %q to %q", fg.Description, updated.Description)
	}
}

func TestRepository_Update_WithDisplayNameAndDescription(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	displayModel := "test-update-display-" + uuid.New().String()[:8]
	po := []uuid.UUID{uuid.New()}

	fg, err := repo.Upsert(ctx, displayModel, po)
	if err != nil {
		t.Fatalf("Upsert failed: %v", err)
	}
	defer func() {
		_ = repo.Delete(ctx, displayModel)
	}()

	displayName := "Updated Display Name"
	description := "Updated description for testing"

	updated, err := repo.Update(ctx, fg.ID, po, fg.EntryEnabled, nil, &displayName, &description, nil)
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	if updated.DisplayName == nil || *updated.DisplayName != displayName {
		t.Errorf("DisplayName = %v, want %q", updated.DisplayName, displayName)
	}
	if updated.Description != description {
		t.Errorf("Description = %q, want %q", updated.Description, description)
	}

	// Verify via GetByID
	InvalidateFailoverCache()
	found, err := repo.GetByID(ctx, fg.ID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}
	if found.DisplayName == nil || *found.DisplayName != displayName {
		t.Errorf("GetByID DisplayName = %v, want %q", found.DisplayName, displayName)
	}
	if found.Description != description {
		t.Errorf("GetByID Description = %q, want %q", found.Description, description)
	}
}

func TestRepository_Update_WithDisplayModel(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	displayModel := "test-update-displaymodel-" + uuid.New().String()[:8]
	po := []uuid.UUID{uuid.New()}

	fg, err := repo.Upsert(ctx, displayModel, po)
	if err != nil {
		t.Fatalf("Upsert failed: %v", err)
	}
	originalModel := displayModel
	// Defer cleanup for the old name in case of failure
	defer func() {
		_ = repo.Delete(ctx, originalModel)
	}()

	newModelName := "test-renamed-" + uuid.New().String()[:8]

	updated, err := repo.Update(ctx, fg.ID, po, fg.EntryEnabled, nil, nil, nil, &newModelName)
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	if updated.DisplayModel != newModelName {
		t.Errorf("DisplayModel = %q, want %q", updated.DisplayModel, newModelName)
	}

	// Verify via GetByModel returns the updated group under the new name
	InvalidateFailoverCache()
	found, err := repo.GetByModel(ctx, newModelName)
	if err != nil {
		t.Fatalf("GetByModel failed: %v", err)
	}
	if found.ID != fg.ID {
		t.Errorf("GetByModel ID = %v, want %v", found.ID, fg.ID)
	}
	if found.DisplayModel != newModelName {
		t.Errorf("GetByModel DisplayModel = %q, want %q", found.DisplayModel, newModelName)
	}

	// Clean up: delete the renamed group
	_ = repo.Delete(ctx, newModelName)
}

func TestRepository_GetEnabled_ExcludesDisabledGroups(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	displayModel := "test-getenabled-disabled-" + uuid.New().String()[:8]
	po := []uuid.UUID{uuid.New()}

	// Create a disabled group
	groupEnabled := false
	fg, err := repo.UpsertWithConfig(ctx, displayModel, po, nil, &groupEnabled, nil, nil, nil)
	if err != nil {
		t.Fatalf("UpsertWithConfig failed: %v", err)
	}
	defer func() {
		_ = repo.Delete(ctx, displayModel)
	}()

	// GetEnabled should not include disabled groups
	groups, err := repo.GetEnabled(ctx)
	if err != nil {
		t.Fatalf("GetEnabled failed: %v", err)
	}

	for _, g := range groups {
		if g.ID == fg.ID {
			t.Error("GetEnabled should not include disabled groups")
		}
	}
}

func TestRepository_GetEnabled_EmptyList(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	// Clean up any auto-created groups from previous tests
	allGroups, _ := repo.List(ctx)
	for _, g := range allGroups {
		_ = repo.DeleteByID(ctx, g.ID)
	}
	InvalidateFailoverCache()

	// GetEnabled with no groups should return empty list or nil, not error
	groups, err := repo.GetEnabled(ctx)
	if err != nil {
		t.Fatalf("GetEnabled failed: %v", err)
	}
	// Accept either nil or empty slice
	if len(groups) != 0 {
		t.Errorf("GetEnabled should return empty slice or nil, got %d items", len(groups))
	}
}

// ---------------------------------------------------------------------------
// Integration tests — List edge cases
// ---------------------------------------------------------------------------

func TestRepository_List_Empty(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	// Clean up any groups from previous tests
	allGroups, _ := repo.List(ctx)
	for _, g := range allGroups {
		_ = repo.DeleteByID(ctx, g.ID)
	}
	InvalidateFailoverCache()

	// List with no groups should return empty slice, not error
	groups, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(groups) != 0 {
		t.Errorf("List should return empty slice, got %d items", len(groups))
	}
}

// ---------------------------------------------------------------------------
func TestRepository_UpsertWithConfig_FullConfiguration(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	displayModel := "test-upsert-config-" + uuid.New().String()[:8]
	po := []uuid.UUID{uuid.New(), uuid.New()}
	entryEnabled := map[string]bool{po[0].String(): true, po[1].String(): false}
	groupEnabled := true
	displayName := "Test Display Name"
	description := "Test description for failover group"
	autoCreated := false

	fg, err := repo.UpsertWithConfig(ctx, displayModel, po, entryEnabled, &groupEnabled, &displayName, &description, &autoCreated)
	if err != nil {
		t.Fatalf("UpsertWithConfig failed: %v", err)
	}
	defer func() {
		_ = repo.Delete(ctx, displayModel)
	}()

	// Verify all fields were set
	if fg.DisplayModel != displayModel {
		t.Errorf("DisplayModel = %q, want %q", fg.DisplayModel, displayModel)
	}
	if fg.DisplayName == nil || *fg.DisplayName != displayName {
		t.Errorf("DisplayName = %v, want %q", fg.DisplayName, displayName)
	}
	if fg.Description != description {
		t.Errorf("Description = %q, want %q", fg.Description, description)
	}
	if fg.GroupEnabled != true {
		t.Errorf("GroupEnabled = %v, want true", fg.GroupEnabled)
	}
	if fg.AutoCreated != false {
		t.Errorf("AutoCreated = %v, want false", fg.AutoCreated)
	}
	if len(fg.PriorityOrder) != 2 {
		t.Errorf("PriorityOrder length = %d, want 2", len(fg.PriorityOrder))
	}
	if fg.EntryEnabled[po[0].String()] != true {
		t.Errorf("EntryEnabled[%q] = %v, want true", po[0].String(), fg.EntryEnabled[po[0].String()])
	}
	if fg.EntryEnabled[po[1].String()] != false {
		t.Errorf("EntryEnabled[%q] = %v, want false", po[1].String(), fg.EntryEnabled[po[1].String()])
	}

	// Verify via GetByModel
	InvalidateFailoverCache()
	found, err := repo.GetByModel(ctx, displayModel)
	if err != nil {
		t.Fatalf("GetByModel failed: %v", err)
	}
	if found.DisplayName == nil || *found.DisplayName != displayName {
		t.Errorf("GetByModel DisplayName = %v, want %q", found.DisplayName, displayName)
	}
	if found.Description != description {
		t.Errorf("GetByModel Description = %q, want %q", found.Description, description)
	}
}

// ---------------------------------------------------------------------------
// Integration tests — GetByID edge cases
// ---------------------------------------------------------------------------

func TestRepository_GetByID_NotFound(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	InvalidateFailoverCache()
	_, err := repo.GetByID(ctx, uuid.New())
	if err == nil {
		t.Error("GetByID should return error for nonexistent ID")
	}
}

// ---------------------------------------------------------------------------
// Integration tests — List ordering
// ---------------------------------------------------------------------------

func TestRepository_List_OrderedByDisplayModel(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	// Create multiple groups with different display models
	models := []string{
		"z-test-model",
		"a-test-model",
		"m-test-model",
	}
	createdIDs := make([]uuid.UUID, len(models))

	for i, model := range models {
		po := []uuid.UUID{uuid.New()}
		fg, err := repo.Upsert(ctx, model, po)
		if err != nil {
			t.Fatalf("Upsert failed for %s: %v", model, err)
		}
		createdIDs[i] = fg.ID
		defer func() {
			_ = repo.Delete(ctx, model)
		}()
	}

	// List should be ordered by display_model
	groups, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	// Verify ordering (a < m < z)
	if len(groups) < 3 {
		t.Fatalf("Expected at least 3 groups, got %d", len(groups))
	}

	// Find our groups and verify order
	var foundModels []string
	for _, g := range groups {
		for _, model := range models {
			if g.DisplayModel == model {
				foundModels = append(foundModels, model)
				break
			}
		}
	}

	if len(foundModels) != 3 {
		t.Fatalf("Expected to find all 3 test groups, got %d", len(foundModels))
	}

	// Verify alphabetical order
	if foundModels[0] != "a-test-model" {
		t.Errorf("Expected first model 'a-test-model', got %q", foundModels[0])
	}
	if foundModels[1] != "m-test-model" {
		t.Errorf("Expected second model 'm-test-model', got %q", foundModels[1])
	}
	if foundModels[2] != "z-test-model" {
		t.Errorf("Expected third model 'z-test-model', got %q", foundModels[2])
	}
}

func TestRepository_PruneModelUUID_NoGroupsContainUUID(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	// Prune a UUID that doesn't exist in any group — should return nil
	err := repo.PruneModelUUID(ctx, uuid.New())
	if err != nil {
		t.Errorf("Expected nil error when no groups contain UUID, got: %v", err)
	}
}

func TestRepository_PruneModelUUID_PrunesStaleFromGroup(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	baseModel := "test-prune-uuid-stale-" + uuid.New().String()[:8]

	// Create 3 providers + 3 models
	provider1ID := uuid.New()
	provider2ID := uuid.New()
	provider3ID := uuid.New()
	model1ID := uuid.New()
	model2ID := uuid.New()
	model3ID := uuid.New()

	for _, p := range []struct {
		pid  uuid.UUID
		name string
	}{
		{provider1ID, "test-provider-p1-" + uuid.New().String()[:8]},
		{provider2ID, "test-provider-p2-" + uuid.New().String()[:8]},
		{provider3ID, "test-provider-p3-" + uuid.New().String()[:8]},
	} {
		_, err := testDB.Pool().Exec(ctx, `
			INSERT INTO providers (id, name, base_url, encrypted_key, key_nonce, key_salt, enabled, created_at)
			VALUES ($1, $2, 'http://localhost:11434', 'dGVzdA==', 'dGVzdA==', 'dGVzdA==', true, now())
		`, p.pid, p.name)
		if err != nil {
			t.Fatalf("Failed to insert provider %s: %v", p.name, err)
		}
		defer func(id uuid.UUID) {
			_, _ = testDB.Pool().Exec(ctx, "DELETE FROM providers WHERE id = $1", id)
		}(p.pid)
	}

	for _, m := range []struct {
		mid   uuid.UUID
		pid   uuid.UUID
		mname string
	}{
		{model1ID, provider1ID, baseModel},
		{model2ID, provider2ID, baseModel},
		{model3ID, provider3ID, baseModel},
	} {
		_, err := testDB.Pool().Exec(ctx, `
			INSERT INTO models (id, model_id, provider_id, enabled, created_at)
			VALUES ($1, $2, $3, true, now())
		`, m.mid, m.mname, m.pid)
		if err != nil {
			t.Fatalf("Failed to insert model: %v", err)
		}
	}

	// Create a custom group with all 3 models
	priorityOrder := []uuid.UUID{model1ID, model2ID, model3ID}
	entryEnabled := map[string]bool{
		model1ID.String(): true,
		model2ID.String(): true,
		model3ID.String(): true,
	}
	_, err := repo.Upsert(ctx, baseModel, priorityOrder)
	if err != nil {
		t.Fatalf("Upsert failed: %v", err)
	}
	// Set entry_enabled on the group
	InvalidateFailoverCache()
	group, err := repo.GetByModel(ctx, baseModel)
	if err != nil {
		t.Fatalf("GetByModel failed: %v", err)
	}
	_, err = repo.Update(ctx, group.ID, priorityOrder, entryEnabled, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("Update entry_enabled failed: %v", err)
	}
	defer func() {
		_ = repo.Delete(ctx, baseModel)
	}()

	// Delete model3 — makes it stale
	_, err = testDB.Pool().Exec(ctx, "DELETE FROM models WHERE id = $1", model3ID)
	if err != nil {
		t.Fatalf("Failed to delete model3: %v", err)
	}

	// Prune for model3's UUID
	err = repo.PruneModelUUID(ctx, model3ID)
	if err != nil {
		t.Fatalf("PruneModelUUID failed: %v", err)
	}

	// Verify: group still exists with 2 valid entries
	InvalidateFailoverCache()
	updated, err := repo.GetByModel(ctx, baseModel)
	if err != nil {
		t.Fatalf("Expected group to still exist, got error: %v", err)
	}
	if len(updated.PriorityOrder) != 2 {
		t.Errorf("Expected 2 entries after prune, got %d", len(updated.PriorityOrder))
	}
}

func TestRepository_PruneModelUUID_DeletesGroupWithOneEntry(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	baseModel := "test-prune-uuid-delete-" + uuid.New().String()[:8]

	// Create 2 providers + 2 models
	provider1ID := uuid.New()
	provider2ID := uuid.New()
	model1ID := uuid.New()
	model2ID := uuid.New()

	for _, p := range []struct {
		pid  uuid.UUID
		name string
	}{
		{provider1ID, "test-provider-d1-" + uuid.New().String()[:8]},
		{provider2ID, "test-provider-d2-" + uuid.New().String()[:8]},
	} {
		_, err := testDB.Pool().Exec(ctx, `
			INSERT INTO providers (id, name, base_url, encrypted_key, key_nonce, key_salt, enabled, created_at)
			VALUES ($1, $2, 'http://localhost:11434', 'dGVzdA==', 'dGVzdA==', 'dGVzdA==', true, now())
		`, p.pid, p.name)
		if err != nil {
			t.Fatalf("Failed to insert provider %s: %v", p.name, err)
		}
		defer func(id uuid.UUID) {
			_, _ = testDB.Pool().Exec(ctx, "DELETE FROM providers WHERE id = $1", id)
		}(p.pid)
	}

	for _, m := range []struct {
		mid   uuid.UUID
		pid   uuid.UUID
		mname string
	}{
		{model1ID, provider1ID, baseModel},
		{model2ID, provider2ID, baseModel},
	} {
		_, err := testDB.Pool().Exec(ctx, `
			INSERT INTO models (id, model_id, provider_id, enabled, created_at)
			VALUES ($1, $2, $3, true, now())
		`, m.mid, m.mname, m.pid)
		if err != nil {
			t.Fatalf("Failed to insert model: %v", err)
		}
	}

	// Create a custom group with both models
	priorityOrder := []uuid.UUID{model1ID, model2ID}
	_, err := repo.Upsert(ctx, baseModel, priorityOrder)
	if err != nil {
		t.Fatalf("Upsert failed: %v", err)
	}
	defer func() {
		_ = repo.Delete(ctx, baseModel)
	}()

	// Delete model2 — leaves only 1 valid entry
	_, err = testDB.Pool().Exec(ctx, "DELETE FROM models WHERE id = $1", model2ID)
	if err != nil {
		t.Fatalf("Failed to delete model2: %v", err)
	}

	// Prune for model2's UUID — should delete the group (only 1 valid entry)
	err = repo.PruneModelUUID(ctx, model2ID)
	if err != nil {
		t.Fatalf("PruneModelUUID failed: %v", err)
	}

	// Verify: group is deleted
	InvalidateFailoverCache()
	_, err = repo.GetByModel(ctx, baseModel)
	if err == nil {
		t.Error("Expected group to be deleted (only 1 valid entry after prune)")
	}
}

func TestRepository_PruneModelUUID_PreservesValidGroup(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	baseModel := "test-prune-uuid-valid-" + uuid.New().String()[:8]

	// Create 3 providers + 3 models
	provider1ID := uuid.New()
	provider2ID := uuid.New()
	provider3ID := uuid.New()
	model1ID := uuid.New()
	model2ID := uuid.New()
	model3ID := uuid.New()

	for _, p := range []struct {
		pid  uuid.UUID
		name string
	}{
		{provider1ID, "test-provider-v1-" + uuid.New().String()[:8]},
		{provider2ID, "test-provider-v2-" + uuid.New().String()[:8]},
		{provider3ID, "test-provider-v3-" + uuid.New().String()[:8]},
	} {
		_, err := testDB.Pool().Exec(ctx, `
			INSERT INTO providers (id, name, base_url, encrypted_key, key_nonce, key_salt, enabled, created_at)
			VALUES ($1, $2, 'http://localhost:11434', 'dGVzdA==', 'dGVzdA==', 'dGVzdA==', true, now())
		`, p.pid, p.name)
		if err != nil {
			t.Fatalf("Failed to insert provider %s: %v", p.name, err)
		}
		defer func(id uuid.UUID) {
			_, _ = testDB.Pool().Exec(ctx, "DELETE FROM providers WHERE id = $1", id)
		}(p.pid)
	}

	for _, m := range []struct {
		mid   uuid.UUID
		pid   uuid.UUID
		mname string
	}{
		{model1ID, provider1ID, baseModel},
		{model2ID, provider2ID, baseModel},
		{model3ID, provider3ID, baseModel},
	} {
		_, err := testDB.Pool().Exec(ctx, `
			INSERT INTO models (id, model_id, provider_id, enabled, created_at)
			VALUES ($1, $2, $3, true, now())
		`, m.mid, m.mname, m.pid)
		if err != nil {
			t.Fatalf("Failed to insert model: %v", err)
		}
	}

	// Create a custom group with all 3 models
	priorityOrder := []uuid.UUID{model1ID, model2ID, model3ID}
	_, err := repo.Upsert(ctx, baseModel, priorityOrder)
	if err != nil {
		t.Fatalf("Upsert failed: %v", err)
	}
	defer func() {
		_ = repo.Delete(ctx, baseModel)
	}()

	// Prune for model1's UUID — all models still exist, nothing to prune
	err = repo.PruneModelUUID(ctx, model1ID)
	if err != nil {
		t.Fatalf("PruneModelUUID failed: %v", err)
	}

	// Verify: group still exists with all 3 entries unchanged
	InvalidateFailoverCache()
	group, err := repo.GetByModel(ctx, baseModel)
	if err != nil {
		t.Fatalf("Expected group to still exist, got error: %v", err)
	}
	if len(group.PriorityOrder) != 3 {
		t.Errorf("Expected 3 entries (no pruning needed), got %d", len(group.PriorityOrder))
	}
}

// TestRepository_SyncForModel_QueryError tests the rows.Err() guard in SyncForModel
// when a closed pool causes the rows iteration to fail.
func TestRepository_PruneModelUUID_QueryError(t *testing.T) {
	// Create a closed pool to trigger query errors
	closedPool, err := pgxpool.New(context.Background(), os.Getenv("DATABASE_URL"))
	if err != nil {
		t.Fatalf("Failed to create pool: %v", err)
	}
	closedPool.Close()

	repo := NewRepository(closedPool)
	ctx := context.Background()

	err = repo.PruneModelUUID(ctx, uuid.New())
	if err == nil {
		t.Error("Expected error from PruneModelUUID with closed pool")
	}
}
