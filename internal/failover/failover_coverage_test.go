package failover

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
)

// ---------------------------------------------------------------------------
// json.Unmarshal error paths - GetByModel
// ---------------------------------------------------------------------------

func TestGetByModel_UnmarshalPriorityError(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	displayModel := "test-unmarshal-priority-" + uuid.New().String()[:8]
	po := []uuid.UUID{uuid.New(), uuid.New()}

	_, err := repo.Upsert(ctx, displayModel, po)
	if err != nil {
		t.Fatalf("Upsert failed: %v", err)
	}
	defer func() {
		_ = repo.Delete(ctx, displayModel)
	}()

	origUnmarshal := jsonUnmarshal
	defer func() { jsonUnmarshal = origUnmarshal }()

	callCount := 0
	jsonUnmarshal = func(data []byte, v interface{}) error {
		callCount++
		if callCount == 1 {
			return fmt.Errorf("test unmarshal error")
		}
		return origUnmarshal(data, v)
	}

	InvalidateFailoverCache()
	_, err = repo.GetByModel(ctx, displayModel)
	if err == nil {
		t.Error("GetByModel should return error when unmarshal fails")
	}
}

func TestGetByModel_UnmarshalEntryEnabledError(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	displayModel := "test-unmarshal-entry-" + uuid.New().String()[:8]
	po := []uuid.UUID{uuid.New(), uuid.New()}

	_, err := repo.Upsert(ctx, displayModel, po)
	if err != nil {
		t.Fatalf("Upsert failed: %v", err)
	}
	defer func() {
		_ = repo.Delete(ctx, displayModel)
	}()

	origUnmarshal := jsonUnmarshal
	defer func() { jsonUnmarshal = origUnmarshal }()

	callCount := 0
	jsonUnmarshal = func(data []byte, v interface{}) error {
		callCount++
		if callCount == 2 {
			return fmt.Errorf("test unmarshal error")
		}
		return origUnmarshal(data, v)
	}

	InvalidateFailoverCache()
	_, err = repo.GetByModel(ctx, displayModel)
	if err == nil {
		t.Error("GetByModel should return error when unmarshal entry_enabled fails")
	}
}

// ---------------------------------------------------------------------------
// json.Unmarshal error paths - UpsertWithConfig
// ---------------------------------------------------------------------------

func TestUpsertWithConfig_UnmarshalPriorityError(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	displayModel := "test-upsert-unmarshal-priority-" + uuid.New().String()[:8]
	po := []uuid.UUID{uuid.New()}

	origUnmarshal := jsonUnmarshal
	defer func() { jsonUnmarshal = origUnmarshal }()

	callCount := 0
	jsonUnmarshal = func(data []byte, v interface{}) error {
		callCount++
		if callCount == 1 {
			return fmt.Errorf("test unmarshal error")
		}
		return origUnmarshal(data, v)
	}

	_, err := repo.UpsertWithConfig(ctx, displayModel, po, nil, nil, nil, nil, nil)
	if err == nil {
		t.Error("UpsertWithConfig should return error when unmarshal priority fails")
	}
}

func TestUpsertWithConfig_UnmarshalEntryEnabledError(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	displayModel := "test-upsert-unmarshal-entry-" + uuid.New().String()[:8]
	po := []uuid.UUID{uuid.New()}

	origUnmarshal := jsonUnmarshal
	defer func() { jsonUnmarshal = origUnmarshal }()

	callCount := 0
	jsonUnmarshal = func(data []byte, v interface{}) error {
		callCount++
		if callCount == 2 {
			return fmt.Errorf("test unmarshal error")
		}
		return origUnmarshal(data, v)
	}

	_, err := repo.UpsertWithConfig(ctx, displayModel, po, nil, nil, nil, nil, nil)
	if err == nil {
		t.Error("UpsertWithConfig should return error when unmarshal entry_enabled fails")
	}
}

// ---------------------------------------------------------------------------
// json.Unmarshal error paths - GetByID
// ---------------------------------------------------------------------------

func TestGetByID_UnmarshalPriorityError(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	displayModel := "test-getbyid-unmarshal-priority-" + uuid.New().String()[:8]
	po := []uuid.UUID{uuid.New()}

	fg, err := repo.Upsert(ctx, displayModel, po)
	if err != nil {
		t.Fatalf("Upsert failed: %v", err)
	}
	defer func() {
		_ = repo.Delete(ctx, displayModel)
	}()

	origUnmarshal := jsonUnmarshal
	defer func() { jsonUnmarshal = origUnmarshal }()

	callCount := 0
	jsonUnmarshal = func(data []byte, v interface{}) error {
		callCount++
		if callCount == 1 {
			return fmt.Errorf("test unmarshal error")
		}
		return origUnmarshal(data, v)
	}

	InvalidateFailoverCache()
	_, err = repo.GetByID(ctx, fg.ID)
	if err == nil {
		t.Error("GetByID should return error when unmarshal priority fails")
	}
}

func TestGetByID_UnmarshalEntryEnabledError(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	displayModel := "test-getbyid-unmarshal-entry-" + uuid.New().String()[:8]
	po := []uuid.UUID{uuid.New()}

	fg, err := repo.Upsert(ctx, displayModel, po)
	if err != nil {
		t.Fatalf("Upsert failed: %v", err)
	}
	defer func() {
		_ = repo.Delete(ctx, displayModel)
	}()

	origUnmarshal := jsonUnmarshal
	defer func() { jsonUnmarshal = origUnmarshal }()

	callCount := 0
	jsonUnmarshal = func(data []byte, v interface{}) error {
		callCount++
		if callCount == 2 {
			return fmt.Errorf("test unmarshal error")
		}
		return origUnmarshal(data, v)
	}

	InvalidateFailoverCache()
	_, err = repo.GetByID(ctx, fg.ID)
	if err == nil {
		t.Error("GetByID should return error when unmarshal entry_enabled fails")
	}
}

// ---------------------------------------------------------------------------
// json.Unmarshal error paths - Update
// ---------------------------------------------------------------------------

func TestUpdate_UnmarshalPriorityError(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	displayModel := "test-update-unmarshal-priority-" + uuid.New().String()[:8]
	po := []uuid.UUID{uuid.New()}

	fg, err := repo.Upsert(ctx, displayModel, po)
	if err != nil {
		t.Fatalf("Upsert failed: %v", err)
	}
	defer func() {
		_ = repo.Delete(ctx, displayModel)
	}()

	origUnmarshal := jsonUnmarshal
	defer func() { jsonUnmarshal = origUnmarshal }()

	callCount := 0
	jsonUnmarshal = func(data []byte, v interface{}) error {
		callCount++
		if callCount == 1 {
			return fmt.Errorf("test unmarshal error")
		}
		return origUnmarshal(data, v)
	}

	newPO := []uuid.UUID{uuid.New()}
	_, err = repo.Update(ctx, fg.ID, newPO, nil, nil, nil, nil)
	if err == nil {
		t.Error("Update should return error when unmarshal priority fails")
	}
}

func TestUpdate_UnmarshalEntryEnabledError(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	displayModel := "test-update-unmarshal-entry-" + uuid.New().String()[:8]
	po := []uuid.UUID{uuid.New()}

	fg, err := repo.Upsert(ctx, displayModel, po)
	if err != nil {
		t.Fatalf("Upsert failed: %v", err)
	}
	defer func() {
		_ = repo.Delete(ctx, displayModel)
	}()

	origUnmarshal := jsonUnmarshal
	defer func() { jsonUnmarshal = origUnmarshal }()

	callCount := 0
	jsonUnmarshal = func(data []byte, v interface{}) error {
		callCount++
		if callCount == 2 {
			return fmt.Errorf("test unmarshal error")
		}
		return origUnmarshal(data, v)
	}

	newPO := []uuid.UUID{uuid.New()}
	_, err = repo.Update(ctx, fg.ID, newPO, nil, nil, nil, nil)
	if err == nil {
		t.Error("Update should return error when unmarshal entry_enabled fails")
	}
}

// ---------------------------------------------------------------------------
// json.Marshal error paths - UpsertWithConfig
// ---------------------------------------------------------------------------

func TestUpsertWithConfig_MarshalPriorityError(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	displayModel := "test-upsert-marshal-priority-" + uuid.New().String()[:8]
	po := []uuid.UUID{uuid.New()}

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

	_, err := repo.UpsertWithConfig(ctx, displayModel, po, nil, nil, nil, nil, nil)
	if err == nil {
		t.Error("UpsertWithConfig should return error when marshal priority fails")
	}
}

func TestUpsertWithConfig_MarshalEntryEnabledError(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	displayModel := "test-upsert-marshal-entry-" + uuid.New().String()[:8]
	po := []uuid.UUID{uuid.New()}

	origMarshal := jsonMarshal
	defer func() { jsonMarshal = origMarshal }()

	callCount := 0
	jsonMarshal = func(v interface{}) ([]byte, error) {
		callCount++
		if callCount == 2 {
			return nil, fmt.Errorf("test marshal error")
		}
		return origMarshal(v)
	}

	_, err := repo.UpsertWithConfig(ctx, displayModel, po, nil, nil, nil, nil, nil)
	if err == nil {
		t.Error("UpsertWithConfig should return error when marshal entry_enabled fails")
	}
}

// ---------------------------------------------------------------------------
// json.Marshal error paths - Update
// ---------------------------------------------------------------------------

func TestUpdate_MarshalPriorityError(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	displayModel := "test-update-marshal-priority-" + uuid.New().String()[:8]
	po := []uuid.UUID{uuid.New()}

	fg, err := repo.Upsert(ctx, displayModel, po)
	if err != nil {
		t.Fatalf("Upsert failed: %v", err)
	}
	defer func() {
		_ = repo.Delete(ctx, displayModel)
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

	newPO := []uuid.UUID{uuid.New()}
	_, err = repo.Update(ctx, fg.ID, newPO, nil, nil, nil, nil)
	if err == nil {
		t.Error("Update should return error when marshal priority fails")
	}
}

func TestUpdate_MarshalEntryEnabledError(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	displayModel := "test-update-marshal-entry-" + uuid.New().String()[:8]
	po := []uuid.UUID{uuid.New()}

	fg, err := repo.Upsert(ctx, displayModel, po)
	if err != nil {
		t.Fatalf("Upsert failed: %v", err)
	}
	defer func() {
		_ = repo.Delete(ctx, displayModel)
	}()

	origMarshal := jsonMarshal
	defer func() { jsonMarshal = origMarshal }()

	callCount := 0
	jsonMarshal = func(v interface{}) ([]byte, error) {
		callCount++
		if callCount == 2 {
			return nil, fmt.Errorf("test marshal error")
		}
		return origMarshal(v)
	}

	newPO := []uuid.UUID{uuid.New()}
	_, err = repo.Update(ctx, fg.ID, newPO, nil, nil, nil, nil)
	if err == nil {
		t.Error("Update should return error when marshal entry_enabled fails")
	}
}

// ---------------------------------------------------------------------------
// DB error paths - canceled context
// ---------------------------------------------------------------------------

func TestUpsertWithConfig_DBError(t *testing.T) {
	repo := newTestRepo(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	displayModel := "test-upsert-dberror-" + uuid.New().String()[:8]
	po := []uuid.UUID{uuid.New()}

	_, err := repo.UpsertWithConfig(ctx, displayModel, po, nil, nil, nil, nil, nil)
	if err == nil {
		t.Error("UpsertWithConfig should return error with canceled context")
	}
}

func TestGetEnabled_DBError(t *testing.T) {
	repo := newTestRepo(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := repo.GetEnabled(ctx)
	if err == nil {
		t.Error("GetEnabled should return error with canceled context")
	}
}

func TestUpdate_DBError(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	displayModel := "test-update-dberror-" + uuid.New().String()[:8]
	po := []uuid.UUID{uuid.New()}

	fg, err := repo.Upsert(ctx, displayModel, po)
	if err != nil {
		t.Fatalf("Upsert failed: %v", err)
	}
	defer func() {
		_ = repo.Delete(ctx, displayModel)
	}()

	cancelCtx, cancel := context.WithCancel(context.Background())
	cancel()

	newPO := []uuid.UUID{uuid.New()}
	_, err = repo.Update(cancelCtx, fg.ID, newPO, nil, nil, nil, nil)
	if err == nil {
		t.Error("Update should return error with canceled context")
	}
}

func TestList_DBError(t *testing.T) {
	repo := newTestRepo(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := repo.List(ctx)
	if err == nil {
		t.Error("List should return error with canceled context")
	}
}

func TestSyncAllModels_DBError(t *testing.T) {
	repo := newTestRepo(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := repo.SyncAllModels(ctx)
	if err == nil {
		t.Error("SyncAllModels should return error with canceled context")
	}
}

func TestSyncForModel_DBError(t *testing.T) {
	repo := newTestRepo(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := repo.SyncForModel(ctx, "test-model")
	if err == nil {
		t.Error("SyncForModel should return error with canceled context")
	}
}

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

	err = repo.SyncForModel(ctx, baseModel)
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
	jsonMarshal = func(v interface{}) ([]byte, error) {
		callCount++
		if callCount == 1 {
			return nil, fmt.Errorf("test marshal error")
		}
		return origMarshal(v)
	}

	err = repo.SyncForModel(ctx, baseModel)
	if err == nil {
		t.Error("SyncForModel should return error when UpsertWithConfig fails")
	}

	_ = repo.Delete(ctx, baseModel)
}
