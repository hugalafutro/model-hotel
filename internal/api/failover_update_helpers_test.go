package api

import (
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
		t.Skip("test DB unavailable")
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
