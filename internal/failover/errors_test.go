package failover

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
)

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
	defer func() {
		_ = repo.Delete(ctx, displayModel)
	}()
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
	defer func() {
		_ = repo.Delete(ctx, displayModel)
	}()
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
	_, err = repo.Update(ctx, fg.ID, newPO, nil, nil, nil, nil, nil)
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
	_, err = repo.Update(ctx, fg.ID, newPO, nil, nil, nil, nil, nil)
	if err == nil {
		t.Error("Update should return error when unmarshal entry_enabled fails")
	}
}
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
	defer func() {
		_ = repo.Delete(ctx, displayModel)
	}()
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
	defer func() {
		_ = repo.Delete(ctx, displayModel)
	}()
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
	_, err = repo.Update(ctx, fg.ID, newPO, nil, nil, nil, nil, nil)
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
	_, err = repo.Update(ctx, fg.ID, newPO, nil, nil, nil, nil, nil)
	if err == nil {
		t.Error("Update should return error when marshal entry_enabled fails")
	}
}
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
	_, err = repo.Update(cancelCtx, fg.ID, newPO, nil, nil, nil, nil, nil)
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

	_, err := repo.SyncForModel(ctx, "test-model")
	if err == nil {
		t.Error("SyncForModel should return error with canceled context")
	}
}
