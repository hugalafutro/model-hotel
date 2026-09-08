package failover

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

// TestScanFailoverGroup_MalformedJSON seeds a group whose priority_order (then
// entry_enabled) is JSON the decoder rejects, and checks every read path
// surfaces the decode error instead of returning a half-built group.
func TestScanFailoverGroup_MalformedJSON(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	for _, tc := range []struct {
		name          string
		priorityOrder string
		entryEnabled  string
		want          string
	}{
		{"priority_order", `{"not":"an array"}`, `{}`, "unmarshal priority_order"},
		{"entry_enabled", `[]`, `["not an object"]`, "unmarshal entry_enabled"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			displayModel := "test-malformed-" + tc.name + "-" + uuid.New().String()[:8]
			var id uuid.UUID
			err := testDB.Pool().QueryRow(ctx, `
				INSERT INTO model_failover_groups (display_model, priority_order, entry_enabled, group_enabled, auto_created, created_at, updated_at)
				VALUES ($1, $2, $3, true, false, now(), now())
				RETURNING id
			`, displayModel, tc.priorityOrder, tc.entryEnabled).Scan(&id)
			if err != nil {
				t.Fatalf("seed group: %v", err)
			}
			defer func() {
				_, _ = testDB.Pool().Exec(ctx, "DELETE FROM model_failover_groups WHERE id = $1", id)
			}()

			InvalidateFailoverCache()
			if _, err := repo.GetByModel(ctx, displayModel); err == nil || !containsSubstring(err.Error(), tc.want) {
				t.Errorf("GetByModel error = %v, want one containing %q", err, tc.want)
			}
			if _, err := repo.GetByID(ctx, id); err == nil || !containsSubstring(err.Error(), tc.want) {
				t.Errorf("GetByID error = %v, want one containing %q", err, tc.want)
			}
			if _, err := repo.List(ctx); err == nil || !containsSubstring(err.Error(), tc.want) {
				t.Errorf("List error = %v, want one containing %q", err, tc.want)
			}
		})
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

	fg, err := upsertGroup(ctx, t, repo, displayModel, po)
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
