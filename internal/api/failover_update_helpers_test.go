package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/failover"
)

// TestFailoverUpdateHelperDBErrors exercises the 500 paths of the Update
// validation helpers by running their repo lookups on an already-cancelled
// request context, so a DB failure mid-PATCH is covered without a broken pool.
func TestFailoverUpdateHelperDBErrors(t *testing.T) {
	h := newIntegrationFailoverHandler()
	if h == nil {
		t.Fatal("test DB unavailable")
	}
	failover.InvalidateFailoverCache()
	cctx := cancelledCtx()
	existing := &failover.FailoverGroup{DisplayModel: "old-model", GroupEnabled: false}

	t.Run("display_model_uniqueness_check_fails", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPatch, "/", http.NoBody).WithContext(cctx)
		dm := "new-model"
		body := &UpdateFailoverGroupRequest{DisplayModel: &dm}
		if h.validateDisplayModelPatch(rec, req, body, existing) {
			t.Error("expected validation to fail when GetByModel errors")
		}
		if rec.Code != http.StatusInternalServerError {
			t.Errorf("expected 500, got %d", rec.Code)
		}
	})

	t.Run("member_lookup_fails_on_enable", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPatch, "/", http.NoBody).WithContext(cctx)
		enabled := true
		body := &UpdateFailoverGroupRequest{GroupEnabled: &enabled}
		priority := []uuid.UUID{uuid.New(), uuid.New()}
		entries := map[string]bool{"a": true}
		if h.validateGroupEnabledState(rec, req, body, existing, priority, entries) {
			t.Error("expected validation to fail when GetByIDs errors")
		}
		if rec.Code != http.StatusInternalServerError {
			t.Errorf("expected 500, got %d", rec.Code)
		}
	})
}

// floorDisables and routableMembers: a member the group switched off does not
// count, and a member lookup that fails surfaces as an error rather than a
// stamp either way.
func TestFloorDisables(t *testing.T) {
	h := newIntegrationFailoverHandler()
	off := false
	t.Run("member_lookup_fails", func(t *testing.T) {
		req := &UpdateFailoverGroupRequest{GroupEnabled: &off, FloorDisabled: true}
		if _, err := h.floorDisables(cancelledCtx(), req, []uuid.UUID{uuid.New(), uuid.New()}, nil); err == nil {
			t.Error("expected the member lookup error to surface")
		}
	})
	t.Run("switched_off_member_does_not_count", func(t *testing.T) {
		groupID, _ := enableGuardSeed(t, h, true, true) // two routable members
		fg, err := h.failoverRepo.GetByID(context.Background(), groupID)
		if err != nil {
			t.Fatalf("get group: %v", err)
		}
		entries := map[string]bool{fg.PriorityOrder[0].String(): false}
		req := &UpdateFailoverGroupRequest{GroupEnabled: &off, FloorDisabled: true}
		floor, err := h.floorDisables(context.Background(), req, fg.PriorityOrder, entries)
		if err != nil {
			t.Fatalf("floorDisables: %v", err)
		}
		if !floor {
			t.Error("one member switched off leaves one routable, want the floor's disable")
		}
		on := true
		if floor, _ := h.floorDisables(context.Background(), &UpdateFailoverGroupRequest{GroupEnabled: &on, FloorDisabled: true}, fg.PriorityOrder, nil); floor {
			t.Error("an enable is never the floor's")
		}
	})
}
