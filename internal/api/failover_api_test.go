package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hugalafutro/model-hotel/internal/admin"
	"github.com/hugalafutro/model-hotel/internal/config"
	"github.com/hugalafutro/model-hotel/internal/db"
	"github.com/hugalafutro/model-hotel/internal/failover"
	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/provider"
	"github.com/hugalafutro/model-hotel/internal/settings"
	"github.com/hugalafutro/model-hotel/internal/util"
	"github.com/hugalafutro/model-hotel/internal/virtualkey"
)

// ---------------------------------------------------------------------------
// Integration test database setup
// ---------------------------------------------------------------------------

var apiTestDB *db.DB
var apiTestDBURL string

func TestMain(m *testing.M) {
	// Zero the confirmation-probe backoff so any test whose mock listing drops
	// a model exercises the probe logic without real 15/45-second sleeps.
	confirmProbeDelays = []time.Duration{0, 0}
	confirmProbeSleep = func(context.Context, time.Duration) error { return nil }

	ctx := context.Background()
	var err error
	var setupErr error
	apiTestDBURL, setupErr = db.SetupTestDB("api")
	if setupErr != nil {
		log.Printf("failed to setup test DB: %v", setupErr)
		os.Exit(1)
	}
	defer db.CleanupTestDB("api")

	apiTestDB, err = db.New(ctx, apiTestDBURL, 25, 5)
	if err != nil {
		log.Printf("failed to initialize test DB: %v", err)
		os.Exit(1) //nolint:gocritic // test-only: os.Exit in TestMain is intentional
	}
	defer apiTestDB.Close()

	util.CloseDockerClient()
	os.Exit(m.Run()) //nolint:gocritic // test-only: os.Exit in TestMain is intentional
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// newIntegrationFailoverHandler creates a FailoverHandler backed by the test database.
// Returns nil if the database is unavailable.
func newIntegrationFailoverHandler() *FailoverHandler {
	if apiTestDB == nil {
		return nil
	}
	pool := apiTestDB.Pool()
	failoverRepo := failover.NewRepository(pool)
	modelRepo := model.NewRepository(pool)
	settingsRepo := settings.NewRepository(pool)
	return NewFailoverHandler(pool, failoverRepo, modelRepo, settingsRepo, nil)
}

func newFailoverRouter(h *FailoverHandler) chi.Router {
	r := chi.NewRouter()
	h.Register(r)
	return r
}

// newFailoverHandlerWithAuth creates a FailoverHandler with admin auth middleware.
// Returns nil if the database is unavailable.
func newFailoverHandlerWithAuth(t *testing.T) (*FailoverHandler, chi.Router) {
	t.Helper()
	h := newIntegrationFailoverHandler()
	if h == nil {
		return nil, nil
	}

	// Create admin auth manager
	tmpDir := t.TempDir()
	adminMgr, _, err := admin.New(tmpDir, "test-admin-token")
	if err != nil {
		t.Fatalf("failed to create admin manager: %v", err)
	}

	// Create a minimal handler to get the auth middleware
	cfg := &config.Config{
		MasterKey:          "testmasterkey1234567890abcdef",
		AllowHTTPProviders: true,
		RateLimitEnabled:   false,
		DataDir:            tmpDir,
	}
	pool := apiTestDB.Pool()
	providerRepo := provider.NewRepository(pool)
	vkRepo := virtualkey.NewRepository(pool)
	settingsRepo := settings.NewRepository(pool)

	mainHandler := NewHandler(cfg, providerRepo, apiTestDB, adminMgr, vkRepo, settingsRepo, "test", nil, nil, nil, nil)
	if mainHandler == nil {
		t.Fatal("handler is nil")
		return nil, nil
	}

	r := chi.NewRouter()
	r.Use(mainHandler.AuthMiddleware)
	h.Register(r)

	return h, r
}

// ---------------------------------------------------------------------------
// Authorization tests
// ---------------------------------------------------------------------------

func TestFailoverHandler_List_Unauthorized(t *testing.T) {
	_, r := newFailoverHandlerWithAuth(t)

	req, w := newChiRequest(http.MethodGet, "/failover-groups/", nil)
	// No Authorization header - should return 401
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusUnauthorized, w.Code, w.Body.String())
	}
}

func TestFailoverHandler_Sync_Unauthorized(t *testing.T) {
	_, r := newFailoverHandlerWithAuth(t)

	req, w := newChiRequest(http.MethodPost, "/failover-groups/sync", nil)
	// No Authorization header - should return 401
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusUnauthorized, w.Code, w.Body.String())
	}
}

func TestFailoverHandler_Delete_Unauthorized(t *testing.T) {
	_, r := newFailoverHandlerWithAuth(t)

	unknownID := uuid.New()
	req, w := newChiRequest(http.MethodDelete, "/failover-groups/"+unknownID.String(), nil)
	// No Authorization header - should return 401
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusUnauthorized, w.Code, w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// Create tests
// ---------------------------------------------------------------------------

func TestFailoverHandler_Create_Success(t *testing.T) {
	h := newIntegrationFailoverHandler()

	ctx := context.Background()

	id1, id2 := uuid.New(), uuid.New()
	displayModel := "test-create-" + uuid.New().String()[:8]
	body := `{"display_model":"` + displayModel + `","entry_ids":["` + id1.String() + `","` + id2.String() + `"]}`
	req, w := newChiRequest(http.MethodPost, "/failover-groups/", strings.NewReader(body))

	r := newFailoverRouter(h)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d; body: %s", http.StatusCreated, w.Code, w.Body.String())
	}

	var resp FailoverGroupResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.DisplayModel != displayModel {
		t.Errorf("DisplayModel = %q, want %q", resp.DisplayModel, displayModel)
	}
	// Note: Entries may be empty because modelRepo.GetByIDs can't resolve random UUIDs
	// that don't correspond to real models in the database.
	if resp.DisplayModel != displayModel {
		t.Errorf("DisplayModel = %q, want %q", resp.DisplayModel, displayModel)
	}

	_ = h.failoverRepo.Delete(ctx, displayModel)
}

func TestFailoverHandler_Create_MissingDisplayModel(t *testing.T) {
	h := newIntegrationFailoverHandler()

	body := `{"entry_ids":["` + uuid.New().String() + `","` + uuid.New().String() + `"]}`
	req, w := newChiRequest(http.MethodPost, "/failover-groups/", strings.NewReader(body))

	h.Create(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusBadRequest, w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "invalid display model") {
		t.Errorf("expected error about display_model, got: %s", w.Body.String())
	}
}

func TestFailoverHandler_Create_InsufficientEntries(t *testing.T) {
	h := newIntegrationFailoverHandler()

	body := `{"display_model":"test-one-entry","entry_ids":["` + uuid.New().String() + `"]}`
	req, w := newChiRequest(http.MethodPost, "/failover-groups/", strings.NewReader(body))

	h.Create(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusBadRequest, w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "at least 2 entries") {
		t.Errorf("expected error about entries, got: %s", w.Body.String())
	}
}

func TestFailoverHandler_Create_InvalidEntryID(t *testing.T) {
	h := newIntegrationFailoverHandler()

	body := `{"display_model":"test-bad-id","entry_ids":["not-a-uuid","` + uuid.New().String() + `"]}`
	req, w := newChiRequest(http.MethodPost, "/failover-groups/", strings.NewReader(body))

	h.Create(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusBadRequest, w.Code, w.Body.String())
	}
}

func TestFailoverHandler_Create_InvalidJSON(t *testing.T) {
	h := newIntegrationFailoverHandler()

	req, w := newChiRequest(http.MethodPost, "/failover-groups/", strings.NewReader("{invalid json"))

	h.Create(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusBadRequest, w.Code, w.Body.String())
	}
}

func TestFailoverHandler_Create_Conflict(t *testing.T) {
	h := newIntegrationFailoverHandler()

	ctx := context.Background()

	displayModel := "test-conflict-" + uuid.New().String()[:8]
	id1, id2 := uuid.New(), uuid.New()
	po := []uuid.UUID{id1, id2}

	_, err := h.failoverRepo.Upsert(ctx, displayModel, po)
	if err != nil {
		t.Fatalf("Upsert failed: %v", err)
	}
	defer func() {
		_ = h.failoverRepo.Delete(ctx, displayModel)
	}()

	body := `{"display_model":"` + displayModel + `","entry_ids":["` + id1.String() + `","` + id2.String() + `"]}`
	req, w := newChiRequest(http.MethodPost, "/failover-groups/", strings.NewReader(body))

	h.Create(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusConflict, w.Code, w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// Get tests
// ---------------------------------------------------------------------------

func TestFailoverHandler_Get_Success(t *testing.T) {
	h := newIntegrationFailoverHandler()

	ctx := context.Background()

	displayModel := "test-get-" + uuid.New().String()[:8]
	id1, id2 := uuid.New(), uuid.New()
	po := []uuid.UUID{id1, id2}

	fg, err := h.failoverRepo.Upsert(ctx, displayModel, po)
	if err != nil {
		t.Fatalf("Upsert failed: %v", err)
	}
	defer func() {
		_ = h.failoverRepo.Delete(ctx, displayModel)
	}()

	req, w := newChiRequest(http.MethodGet, "/failover-groups/"+fg.ID.String(), nil)
	req = setChiURLParam(req, "id", fg.ID.String())

	h.Get(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusOK, w.Code, w.Body.String())
	}

	var resp FailoverGroupResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.ID != fg.ID.String() {
		t.Errorf("ID = %q, want %q", resp.ID, fg.ID.String())
	}
}

func TestFailoverHandler_Get_NotFound(t *testing.T) {
	h := newIntegrationFailoverHandler()

	unknownID := uuid.New()
	req, w := newChiRequest(http.MethodGet, "/failover-groups/"+unknownID.String(), nil)
	req = setChiURLParam(req, "id", unknownID.String())

	h.Get(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusNotFound, w.Code, w.Body.String())
	}
}

func TestFailoverHandler_Get_InvalidUUID(t *testing.T) {
	h := newIntegrationFailoverHandler()

	req, w := newChiRequest(http.MethodGet, "/failover-groups/not-a-uuid", nil)
	req = setChiURLParam(req, "id", "not-a-uuid")

	h.Get(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusBadRequest, w.Code, w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// Update tests
// ---------------------------------------------------------------------------

func TestFailoverHandler_Update_Success(t *testing.T) {
	h := newIntegrationFailoverHandler()

	ctx := context.Background()

	displayModel := "test-update-" + uuid.New().String()[:8]
	id1, id2 := uuid.New(), uuid.New()
	po := []uuid.UUID{id1, id2}

	fg, err := h.failoverRepo.Upsert(ctx, displayModel, po)
	if err != nil {
		t.Fatalf("Upsert failed: %v", err)
	}
	defer func() {
		_ = h.failoverRepo.Delete(ctx, displayModel)
	}()

	newID := uuid.New()
	body := `{"priority_order":["` + id1.String() + `","` + id2.String() + `","` + newID.String() + `"],"entry_enabled":{"` + id1.String() + `":true,"` + id2.String() + `":true,"` + newID.String() + `":true}}`
	req, w := newChiRequest(http.MethodPut, "/failover-groups/"+fg.ID.String(), strings.NewReader(body))
	req = setChiURLParam(req, "id", fg.ID.String())

	h.Update(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusOK, w.Code, w.Body.String())
	}

	var resp FailoverGroupResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	// Note: Entries may be empty because modelRepo.GetByIDs can't resolve random UUIDs
	// that don't correspond to real models in the database.
	// Verify the response is valid JSON and the group still exists.
	if resp.ID != fg.ID.String() {
		t.Errorf("ID = %q, want %q", resp.ID, fg.ID.String())
	}
}

func TestFailoverHandler_Update_InvalidatesCache(t *testing.T) {
	h := newIntegrationFailoverHandler()

	ctx := context.Background()

	displayModel := "test-cache-inval-" + uuid.New().String()[:8]
	id1, id2 := uuid.New(), uuid.New()
	po := []uuid.UUID{id1, id2}

	fg, err := h.failoverRepo.Upsert(ctx, displayModel, po)
	if err != nil {
		t.Fatalf("Upsert failed: %v", err)
	}
	defer func() {
		_ = h.failoverRepo.Delete(ctx, displayModel)
	}()

	// Populate the cache by reading the group via GetByModel.
	cached, err := h.failoverRepo.GetByModel(ctx, displayModel)
	if err != nil {
		t.Fatalf("GetByModel failed: %v", err)
	}
	if len(cached.PriorityOrder) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(cached.PriorityOrder))
	}

	// Verify the cache is populated.
	if _, ok := failover.GetCachedFailoverByModel(displayModel); !ok {
		t.Fatal("expected cache hit after GetByModel")
	}

	// Reorder via Update: swap id1 and id2.
	body := `{"priority_order":["` + id2.String() + `","` + id1.String() + `"],"entry_enabled":{"` + id1.String() + `":true,"` + id2.String() + `":true}}`
	req, w := newChiRequest(http.MethodPut, "/failover-groups/"+fg.ID.String(), strings.NewReader(body))
	req = setChiURLParam(req, "id", fg.ID.String())

	h.Update(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d; body: %s", http.StatusOK, w.Code, w.Body.String())
	}

	// After Update, GetCachedFailoverByModel may return the fresh group (the
	// repo's Update method re-caches after writing). What matters is that the
	// cached PriorityOrder reflects the new order, not the stale one.
	cachedAfter, ok := failover.GetCachedFailoverByModel(displayModel)
	if !ok {
		t.Fatal("expected cache to be populated with updated group after Update")
	}
	if cachedAfter.PriorityOrder[0] != id2 || cachedAfter.PriorityOrder[1] != id1 {
		t.Errorf("cached priority order not swapped: got %v, want [%v, %v]", cachedAfter.PriorityOrder, id2, id1)
	}

	// Verify the DB also has the reordered priority.
	updated, err := h.failoverRepo.GetByModel(ctx, displayModel)
	if err != nil {
		t.Fatalf("GetByModel after update failed: %v", err)
	}
	if updated.PriorityOrder[0] != id2 || updated.PriorityOrder[1] != id1 {
		t.Errorf("priority order not swapped: got %v", updated.PriorityOrder)
	}
}

// enableGuardSeed inserts an enabled provider plus two models (each enabled per
// the flags) and a disabled custom failover group containing both, returning the
// group ID. Registers cleanup.
func enableGuardSeed(t *testing.T, h *FailoverHandler, m1Enabled, m2Enabled bool) (groupID uuid.UUID, displayModel string) {
	t.Helper()
	ctx := context.Background()
	pool := apiTestDB.Pool()
	displayModel = "test-enable-guard-" + uuid.New().String()[:8]

	providerID := uuid.New()
	_, err := pool.Exec(ctx,
		`INSERT INTO providers (id, name, base_url, enabled, created_at, updated_at)
		 VALUES ($1, $2, 'https://eg.example.com', true, now(), now())`,
		providerID, "eg-prov-"+uuid.New().String()[:8])
	if err != nil {
		t.Fatalf("insert provider: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM providers WHERE id = $1", providerID) })

	m1, m2 := uuid.New(), uuid.New()
	for _, m := range []struct {
		id      uuid.UUID
		enabled bool
	}{{m1, m1Enabled}, {m2, m2Enabled}} {
		_, err = pool.Exec(ctx,
			`INSERT INTO models (id, provider_id, model_id, name, enabled, created_at, last_seen_at)
			 VALUES ($1, $2, $3, $3, $4, now(), now())`,
			m.id, providerID, "eg-model-"+m.id.String()[:8], m.enabled)
		if err != nil {
			t.Fatalf("insert model: %v", err)
		}
		t.Cleanup(func(id uuid.UUID) func() {
			return func() { _, _ = pool.Exec(ctx, "DELETE FROM models WHERE id = $1", id) }
		}(m.id))
	}
	model.InvalidateModelCache()

	groupEnabled := false
	autoCreated := false
	entryEnabled := map[string]bool{m1.String(): true, m2.String(): true}
	fg, err := h.failoverRepo.UpsertWithConfig(ctx, displayModel, []uuid.UUID{m1, m2}, entryEnabled, &groupEnabled, nil, nil, &autoCreated)
	if err != nil {
		t.Fatalf("create custom group: %v", err)
	}
	t.Cleanup(func() { _ = h.failoverRepo.Delete(ctx, displayModel) })
	return fg.ID, displayModel
}

// Re-enabling a disabled group that discovery left with a single routable member
// must be rejected at the API boundary, not just auto-disabled on the next scan.
func TestFailoverHandler_Update_EnableRejectedWhenUndersized(t *testing.T) {
	h := newIntegrationFailoverHandler()
	groupID, _ := enableGuardSeed(t, h, true, false) // only one routable member

	body := `{"group_enabled":true}`
	req, w := newChiRequest(http.MethodPut, "/failover-groups/"+groupID.String(), strings.NewReader(body))
	req = setChiURLParam(req, "id", groupID.String())

	h.Update(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when enabling an undersized group, got %d; body: %s", w.Code, w.Body.String())
	}
}

// Enabling a disabled group that has two routable members is allowed.
func TestFailoverHandler_Update_EnableAllowedWhenTwoRoutable(t *testing.T) {
	h := newIntegrationFailoverHandler()
	groupID, _ := enableGuardSeed(t, h, true, true) // two routable members

	body := `{"group_enabled":true}`
	req, w := newChiRequest(http.MethodPut, "/failover-groups/"+groupID.String(), strings.NewReader(body))
	req = setChiURLParam(req, "id", groupID.String())

	h.Update(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 when enabling a group with 2 routable members, got %d; body: %s", w.Code, w.Body.String())
	}
}

// Listing groups self-heals: an enabled custom group left with fewer than two
// routable members (a member's model disabled outside a sync) is auto-disabled
// and returned as group_enabled=false, so the dashboard never shows an invalid
// enabled group.
func TestFailoverHandler_List_SelfHealsUndersizedGroup(t *testing.T) {
	h := newIntegrationFailoverHandler()
	ctx := context.Background()
	pool := apiTestDB.Pool()
	displayModel := "test-selfheal-" + uuid.New().String()[:8]

	providerID := uuid.New()
	if _, err := pool.Exec(ctx,
		`INSERT INTO providers (id, name, base_url, enabled, created_at, updated_at)
		 VALUES ($1, $2, 'https://sh.example.com', true, now(), now())`,
		providerID, "sh-prov-"+uuid.New().String()[:8]); err != nil {
		t.Fatalf("insert provider: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM providers WHERE id = $1", providerID) })

	m1, m2 := uuid.New(), uuid.New()
	for _, m := range []struct {
		id      uuid.UUID
		enabled bool
	}{{m1, true}, {m2, false}} { // m2's model disabled -> only one routable member
		if _, err := pool.Exec(ctx,
			`INSERT INTO models (id, provider_id, model_id, name, enabled, created_at, last_seen_at)
			 VALUES ($1, $2, $3, $3, $4, now(), now())`,
			m.id, providerID, "sh-model-"+m.id.String()[:8], m.enabled); err != nil {
			t.Fatalf("insert model: %v", err)
		}
		t.Cleanup(func(id uuid.UUID) func() {
			return func() { _, _ = pool.Exec(ctx, "DELETE FROM models WHERE id = $1", id) }
		}(m.id))
	}
	model.InvalidateModelCache()

	// Create an ENABLED custom group with the now-undersized membership.
	groupEnabled := true
	autoCreated := false
	entryEnabled := map[string]bool{m1.String(): true, m2.String(): true}
	if _, err := h.failoverRepo.UpsertWithConfig(ctx, displayModel, []uuid.UUID{m1, m2}, entryEnabled, &groupEnabled, nil, nil, &autoCreated); err != nil {
		t.Fatalf("create custom group: %v", err)
	}
	t.Cleanup(func() { _ = h.failoverRepo.Delete(ctx, displayModel) })

	req, w := newChiRequest(http.MethodGet, "/failover-groups", nil)
	h.List(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("List: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp FailoverListResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	found := false
	for _, g := range resp.Groups {
		if g.DisplayModel == displayModel {
			found = true
			if g.GroupEnabled {
				t.Errorf("expected group %q to be self-healed to disabled, got group_enabled=true", displayModel)
			}
		}
	}
	if !found {
		t.Fatalf("group %q not present in list response", displayModel)
	}
}

func TestFailoverHandler_Update_DisableGroup(t *testing.T) {
	h := newIntegrationFailoverHandler()

	ctx := context.Background()

	displayModel := "test-update-disable-" + uuid.New().String()[:8]
	id1, id2 := uuid.New(), uuid.New()
	po := []uuid.UUID{id1, id2}

	fg, err := h.failoverRepo.Upsert(ctx, displayModel, po)
	if err != nil {
		t.Fatalf("Upsert failed: %v", err)
	}
	defer func() {
		_ = h.failoverRepo.Delete(ctx, displayModel)
	}()

	body := `{"group_enabled":false}`
	req, w := newChiRequest(http.MethodPut, "/failover-groups/"+fg.ID.String(), strings.NewReader(body))
	req = setChiURLParam(req, "id", fg.ID.String())

	h.Update(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusOK, w.Code, w.Body.String())
	}

	var resp FailoverGroupResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.GroupEnabled != false {
		t.Errorf("GroupEnabled = %v, want false", resp.GroupEnabled)
	}
}

func TestFailoverHandler_Update_NoEnabledEntries(t *testing.T) {
	h := newIntegrationFailoverHandler()

	ctx := context.Background()

	displayModel := "test-update-noenabled-" + uuid.New().String()[:8]
	id1, id2 := uuid.New(), uuid.New()
	po := []uuid.UUID{id1, id2}

	fg, err := h.failoverRepo.Upsert(ctx, displayModel, po)
	if err != nil {
		t.Fatalf("Upsert failed: %v", err)
	}
	defer func() {
		_ = h.failoverRepo.Delete(ctx, displayModel)
	}()

	body := `{"entry_enabled":{"` + id1.String() + `":false,"` + id2.String() + `":false}}`
	req, w := newChiRequest(http.MethodPut, "/failover-groups/"+fg.ID.String(), strings.NewReader(body))
	req = setChiURLParam(req, "id", fg.ID.String())

	h.Update(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusBadRequest, w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "at least one entry must be enabled") {
		t.Errorf("expected error about enabled entries, got: %s", w.Body.String())
	}
}

func TestFailoverHandler_Update_InvalidUUID(t *testing.T) {
	h := newIntegrationFailoverHandler()

	body := `{"group_enabled":false}`
	req, w := newChiRequest(http.MethodPut, "/failover-groups/not-a-uuid", strings.NewReader(body))
	req = setChiURLParam(req, "id", "not-a-uuid")

	h.Update(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusBadRequest, w.Code, w.Body.String())
	}
}

func TestFailoverHandler_Update_NotFound(t *testing.T) {
	h := newIntegrationFailoverHandler()

	unknownID := uuid.New()
	body := `{"group_enabled":false}`
	req, w := newChiRequest(http.MethodPut, "/failover-groups/"+unknownID.String(), strings.NewReader(body))
	req = setChiURLParam(req, "id", unknownID.String())

	h.Update(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusNotFound, w.Code, w.Body.String())
	}
}

func TestFailoverHandler_Update_InvalidPriorityOrderEntry(t *testing.T) {
	h := newIntegrationFailoverHandler()

	ctx := context.Background()

	displayModel := "test-update-badorder-" + uuid.New().String()[:8]
	id1, id2 := uuid.New(), uuid.New()
	po := []uuid.UUID{id1, id2}

	fg, err := h.failoverRepo.Upsert(ctx, displayModel, po)
	if err != nil {
		t.Fatalf("Upsert failed: %v", err)
	}
	defer func() {
		_ = h.failoverRepo.Delete(ctx, displayModel)
	}()

	body := `{"priority_order":["not-a-uuid"]}`
	req, w := newChiRequest(http.MethodPut, "/failover-groups/"+fg.ID.String(), strings.NewReader(body))
	req = setChiURLParam(req, "id", fg.ID.String())

	h.Update(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusBadRequest, w.Code, w.Body.String())
	}
}

func TestFailoverHandler_Update_InvalidJSON(t *testing.T) {
	h := newIntegrationFailoverHandler()

	id := uuid.New()
	req, w := newChiRequest(http.MethodPut, "/failover-groups/"+id.String(), strings.NewReader("{invalid"))
	req = setChiURLParam(req, "id", id.String())

	h.Update(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusBadRequest, w.Code, w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// Delete tests
// ---------------------------------------------------------------------------

func TestFailoverHandler_Delete_Success(t *testing.T) {
	h := newIntegrationFailoverHandler()

	ctx := context.Background()

	displayModel := "test-delete-" + uuid.New().String()[:8]
	id1, id2 := uuid.New(), uuid.New()
	po := []uuid.UUID{id1, id2}

	fg, err := h.failoverRepo.Upsert(ctx, displayModel, po)
	if err != nil {
		t.Fatalf("Upsert failed: %v", err)
	}

	req, w := newChiRequest(http.MethodDelete, "/failover-groups/"+fg.ID.String(), nil)
	req = setChiURLParam(req, "id", fg.ID.String())

	h.Delete(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusNoContent, w.Code, w.Body.String())
	}

	failover.InvalidateFailoverCache()
	_, err = h.failoverRepo.GetByModel(ctx, displayModel)
	if err == nil {
		t.Error("group should be deleted")
	}
}

func TestFailoverHandler_Delete_InvalidUUID(t *testing.T) {
	h := newIntegrationFailoverHandler()

	req, w := newChiRequest(http.MethodDelete, "/failover-groups/not-a-uuid", nil)
	req = setChiURLParam(req, "id", "not-a-uuid")

	h.Delete(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusBadRequest, w.Code, w.Body.String())
	}
}

func TestFailoverHandler_Delete_NonExistent(t *testing.T) {
	h := newIntegrationFailoverHandler()

	unknownID := uuid.New()
	req, w := newChiRequest(http.MethodDelete, "/failover-groups/"+unknownID.String(), nil)
	req = setChiURLParam(req, "id", unknownID.String())

	h.Delete(w, req)

	// Delete returns 204 even for non-existent groups (idempotent)
	if w.Code != http.StatusNoContent {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusNoContent, w.Code, w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// List tests
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// List tests - Additional coverage
// ---------------------------------------------------------------------------

func TestFailoverHandler_List_Success(t *testing.T) {
	h := newIntegrationFailoverHandler()

	ctx := context.Background()

	displayModel := "test-list-" + uuid.New().String()[:8]
	id1, id2 := uuid.New(), uuid.New()
	po := []uuid.UUID{id1, id2}

	_, err := h.failoverRepo.Upsert(ctx, displayModel, po)
	if err != nil {
		t.Fatalf("Upsert failed: %v", err)
	}
	defer func() {
		_ = h.failoverRepo.Delete(ctx, displayModel)
	}()

	req, w := newChiRequest(http.MethodGet, "/failover-groups/", nil)

	h.List(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusOK, w.Code, w.Body.String())
	}

	var resp FailoverListResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(resp.Groups) == 0 {
		t.Error("List should return at least one group")
	}
}

// ---------------------------------------------------------------------------
// Sync tests
// ---------------------------------------------------------------------------

func TestFailoverHandler_Sync_Success(t *testing.T) {
	h := newIntegrationFailoverHandler()

	req, w := newChiRequest(http.MethodPost, "/failover-groups/sync", nil)

	h.Sync(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusOK, w.Code, w.Body.String())
	}

	var resp failover.SyncResult
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	// DeletedGroups may be nil when no groups were deleted (empty sync result).
	// This is valid — just verify the response is a valid SyncResult.
}

// ---------------------------------------------------------------------------
// Candidates tests
// ---------------------------------------------------------------------------

func TestFailoverHandler_Candidates_Success(t *testing.T) {
	h := newIntegrationFailoverHandler()

	req, w := newChiRequest(http.MethodGet, "/failover-groups/candidates", nil)

	h.Candidates(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusOK, w.Code, w.Body.String())
	}

	var resp []CandidateModelResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
}

// ---------------------------------------------------------------------------
// GetByModelUUID tests
// ---------------------------------------------------------------------------

func TestFailoverHandler_GetByModelUUID_Found(t *testing.T) {
	h := newIntegrationFailoverHandler()

	ctx := context.Background()

	displayModel := "test-byuuid-" + uuid.New().String()[:8]
	id1, id2 := uuid.New(), uuid.New()
	po := []uuid.UUID{id1, id2}

	_, err := h.failoverRepo.Upsert(ctx, displayModel, po)
	if err != nil {
		t.Fatalf("Upsert failed: %v", err)
	}
	defer func() {
		_ = h.failoverRepo.Delete(ctx, displayModel)
	}()

	req, w := newChiRequest(http.MethodGet, "/failover-groups/by-model/"+id1.String(), nil)
	req = setChiURLParam(req, "model_uuid", id1.String())

	h.GetByModelUUID(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusOK, w.Code, w.Body.String())
	}

	var resp FailoverGroupBrief
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.DisplayModel != displayModel {
		t.Errorf("DisplayModel = %q, want %q", resp.DisplayModel, displayModel)
	}
	if resp.Position != 1 {
		t.Errorf("Position = %d, want 1 (id1 is first in priority)", resp.Position)
	}
	if resp.TotalEntries != 2 {
		t.Errorf("TotalEntries = %d, want 2", resp.TotalEntries)
	}
}

func TestFailoverHandler_GetByModelUUID_NotFound(t *testing.T) {
	h := newIntegrationFailoverHandler()

	unknownUUID := uuid.New()
	req, w := newChiRequest(http.MethodGet, "/failover-groups/by-model/"+unknownUUID.String(), nil)
	req = setChiURLParam(req, "model_uuid", unknownUUID.String())

	h.GetByModelUUID(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusNotFound, w.Code, w.Body.String())
	}
}

func TestFailoverHandler_GetByModelUUID_InvalidUUID(t *testing.T) {
	h := newIntegrationFailoverHandler()

	req, w := newChiRequest(http.MethodGet, "/failover-groups/by-model/not-a-uuid", nil)
	req = setChiURLParam(req, "model_uuid", "not-a-uuid")

	h.GetByModelUUID(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusBadRequest, w.Code, w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// Error handling path tests
// ---------------------------------------------------------------------------

// newClosedPool creates a new pool and immediately closes it for testing error paths
func newClosedPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, apiTestDBURL)
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}
	pool.Close()
	return pool
}

// TestFailoverHandler_List_RepoError tests when failoverRepo.List returns an error
func TestFailoverHandler_List_RepoError(t *testing.T) {
	closedPool := newClosedPool(t)
	failoverRepo := failover.NewRepository(closedPool)
	modelRepo := model.NewRepository(closedPool)
	settingsRepo := settings.NewRepository(closedPool)
	h := NewFailoverHandler(closedPool, failoverRepo, modelRepo, settingsRepo, nil)

	req, w := newChiRequest(http.MethodGet, "/failover-groups/", nil)
	h.List(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusInternalServerError, w.Code, w.Body.String())
	}
}

// TestFailoverHandler_List_GetTokenCountsError tests when getTokenCounts returns an error
// but failoverRepo.List succeeds (different pools)
func TestFailoverHandler_List_GetTokenCountsError(t *testing.T) {
	// Create handler with working repos but closed dbPool for getTokenCounts
	workingPool := apiTestDB.Pool()
	failoverRepo := failover.NewRepository(workingPool)
	modelRepo := model.NewRepository(workingPool)
	settingsRepo := settings.NewRepository(workingPool)
	closedPool := newClosedPool(t)

	h := NewFailoverHandler(closedPool, failoverRepo, modelRepo, settingsRepo, nil)

	// Create a failover group first so List has data to return
	ctx := context.Background()
	displayModel := "test-list-tokenerr-" + uuid.New().String()[:8]
	id1, id2 := uuid.New(), uuid.New()
	_, err := failoverRepo.Upsert(ctx, displayModel, []uuid.UUID{id1, id2})
	if err != nil {
		t.Fatalf("Upsert failed: %v", err)
	}
	defer func() {
		_ = failoverRepo.Delete(ctx, displayModel)
	}()

	req, w := newChiRequest(http.MethodGet, "/failover-groups/", nil)
	h.List(w, req)

	// Should still return 200 with empty token counts (error is swallowed)
	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusOK, w.Code, w.Body.String())
	}
}

// TestFailoverHandler_List_BuildGroupResponseError tests when buildGroupResponse fails
func TestFailoverHandler_List_BuildGroupResponseError(t *testing.T) {
	// Create handler where failoverRepo works but modelRepo fails
	workingPool := apiTestDB.Pool()
	failoverRepo := failover.NewRepository(workingPool)
	closedPool := newClosedPool(t)
	modelRepo := model.NewRepository(closedPool)
	settingsRepo := settings.NewRepository(workingPool)

	h := NewFailoverHandler(workingPool, failoverRepo, modelRepo, settingsRepo, nil)

	// Create a failover group first
	ctx := context.Background()
	displayModel := "test-list-builderr-" + uuid.New().String()[:8]
	id1, id2 := uuid.New(), uuid.New()
	_, err := failoverRepo.Upsert(ctx, displayModel, []uuid.UUID{id1, id2})
	if err != nil {
		t.Fatalf("Upsert failed: %v", err)
	}
	defer func() {
		_ = failoverRepo.Delete(ctx, displayModel)
	}()

	req, w := newChiRequest(http.MethodGet, "/failover-groups/", nil)
	h.List(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusInternalServerError, w.Code, w.Body.String())
	}
}

// TestFailoverHandler_Get_GetTokenCountsError tests when getTokenCounts returns an error in Get
func TestFailoverHandler_Get_GetTokenCountsError(t *testing.T) {
	workingPool := apiTestDB.Pool()
	failoverRepo := failover.NewRepository(workingPool)
	modelRepo := model.NewRepository(workingPool)
	settingsRepo := settings.NewRepository(workingPool)
	closedPool := newClosedPool(t)

	h := NewFailoverHandler(closedPool, failoverRepo, modelRepo, settingsRepo, nil)

	// Create a failover group first
	ctx := context.Background()
	displayModel := "test-get-tokenerr-" + uuid.New().String()[:8]
	id1, id2 := uuid.New(), uuid.New()
	fg, err := failoverRepo.Upsert(ctx, displayModel, []uuid.UUID{id1, id2})
	if err != nil {
		t.Fatalf("Upsert failed: %v", err)
	}
	defer func() {
		_ = failoverRepo.Delete(ctx, displayModel)
	}()

	req, w := newChiRequest(http.MethodGet, "/failover-groups/"+fg.ID.String(), nil)
	req = setChiURLParam(req, "id", fg.ID.String())
	h.Get(w, req)

	// Should still return 200 with empty token counts
	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusOK, w.Code, w.Body.String())
	}
}

// TestFailoverHandler_Get_BuildGroupResponseError tests when buildGroupResponse fails in Get
func TestFailoverHandler_Get_BuildGroupResponseError(t *testing.T) {
	workingPool := apiTestDB.Pool()
	failoverRepo := failover.NewRepository(workingPool)
	closedPool := newClosedPool(t)
	modelRepo := model.NewRepository(closedPool)
	settingsRepo := settings.NewRepository(workingPool)

	h := NewFailoverHandler(workingPool, failoverRepo, modelRepo, settingsRepo, nil)

	// Create a failover group first
	ctx := context.Background()
	displayModel := "test-get-builderr-" + uuid.New().String()[:8]
	id1, id2 := uuid.New(), uuid.New()
	fg, err := failoverRepo.Upsert(ctx, displayModel, []uuid.UUID{id1, id2})
	if err != nil {
		t.Fatalf("Upsert failed: %v", err)
	}
	defer func() {
		_ = failoverRepo.Delete(ctx, displayModel)
	}()

	req, w := newChiRequest(http.MethodGet, "/failover-groups/"+fg.ID.String(), nil)
	req = setChiURLParam(req, "id", fg.ID.String())
	h.Get(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusInternalServerError, w.Code, w.Body.String())
	}
}

// TestFailoverHandler_Create_UpsertError tests when UpsertWithConfig fails
func TestFailoverHandler_Create_UpsertError(t *testing.T) {
	closedPool := newClosedPool(t)
	failoverRepo := failover.NewRepository(closedPool)
	modelRepo := model.NewRepository(closedPool)
	settingsRepo := settings.NewRepository(closedPool)
	h := NewFailoverHandler(closedPool, failoverRepo, modelRepo, settingsRepo, nil)

	id1, id2 := uuid.New(), uuid.New()
	displayModel := "test-create-upserterr-" + uuid.New().String()[:8]
	body := `{"display_model":"` + displayModel + `","entry_ids":["` + id1.String() + `","` + id2.String() + `"]}`
	req, w := newChiRequest(http.MethodPost, "/failover-groups/", strings.NewReader(body))
	h.Create(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusInternalServerError, w.Code, w.Body.String())
	}
}

// TestFailoverHandler_Create_BuildGroupResponseError tests when buildGroupResponse fails after Create
func TestFailoverHandler_Create_BuildGroupResponseError(t *testing.T) {
	workingPool := apiTestDB.Pool()
	failoverRepo := failover.NewRepository(workingPool)
	closedPool := newClosedPool(t)
	modelRepo := model.NewRepository(closedPool)
	settingsRepo := settings.NewRepository(workingPool)

	h := NewFailoverHandler(workingPool, failoverRepo, modelRepo, settingsRepo, nil)

	id1, id2 := uuid.New(), uuid.New()
	displayModel := "test-create-builderr-" + uuid.New().String()[:8]
	body := `{"display_model":"` + displayModel + `","entry_ids":["` + id1.String() + `","` + id2.String() + `"]}`
	req, w := newChiRequest(http.MethodPost, "/failover-groups/", strings.NewReader(body))
	h.Create(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusInternalServerError, w.Code, w.Body.String())
	}

	// Cleanup
	ctx := context.Background()
	_ = failoverRepo.Delete(ctx, displayModel)
}

// TestFailoverHandler_Update_RepoError tests when failoverRepo.Update fails
// Note: With a closed pool, GetByID fails first (404) before Update is called.
// This still exercises DB error handling. The Update-specific error path (500)
// would require repository mocking to isolate.
func TestFailoverHandler_Update_RepoError(t *testing.T) {
	workingPool := apiTestDB.Pool()
	failoverRepo := failover.NewRepository(workingPool)
	modelRepo := model.NewRepository(workingPool)
	settingsRepo := settings.NewRepository(workingPool)

	// Create a group first with working pool
	ctx := context.Background()
	displayModel := "test-update-repoerr-" + uuid.New().String()[:8]
	id1, id2 := uuid.New(), uuid.New()
	fg, err := failoverRepo.Upsert(ctx, displayModel, []uuid.UUID{id1, id2})
	if err != nil {
		t.Fatalf("Upsert failed: %v", err)
	}
	defer func() {
		_ = failoverRepo.Delete(ctx, displayModel)
	}()

	// Create handler with closed pool - GetByID will fail before Update is called
	closedPool := newClosedPool(t)
	failoverRepoClosed := failover.NewRepository(closedPool)
	h := NewFailoverHandler(closedPool, failoverRepoClosed, modelRepo, settingsRepo, nil)

	body := `{"group_enabled":false}`
	req, w := newChiRequest(http.MethodPut, "/failover-groups/"+fg.ID.String(), strings.NewReader(body))
	req = setChiURLParam(req, "id", fg.ID.String())
	h.Update(w, req)

	// With closed pool, GetByID fails first returning 404
	// The Update error path (500) would require mocking the repo
	if w.Code != http.StatusNotFound && w.Code != http.StatusInternalServerError {
		t.Errorf("expected status 404 or 500, got %d; body: %s", w.Code, w.Body.String())
	}
}

// TestFailoverHandler_Update_BuildGroupResponseError tests when buildGroupResponse fails after Update
func TestFailoverHandler_Update_BuildGroupResponseError(t *testing.T) {
	workingPool := apiTestDB.Pool()
	failoverRepo := failover.NewRepository(workingPool)
	closedPool := newClosedPool(t)
	modelRepo := model.NewRepository(closedPool)
	settingsRepo := settings.NewRepository(workingPool)

	h := NewFailoverHandler(workingPool, failoverRepo, modelRepo, settingsRepo, nil)

	// Create a group first
	ctx := context.Background()
	displayModel := "test-update-builderr-" + uuid.New().String()[:8]
	id1, id2 := uuid.New(), uuid.New()
	fg, err := failoverRepo.Upsert(ctx, displayModel, []uuid.UUID{id1, id2})
	if err != nil {
		t.Fatalf("Upsert failed: %v", err)
	}
	defer func() {
		_ = failoverRepo.Delete(ctx, displayModel)
	}()

	body := `{"group_enabled":false}`
	req, w := newChiRequest(http.MethodPut, "/failover-groups/"+fg.ID.String(), strings.NewReader(body))
	req = setChiURLParam(req, "id", fg.ID.String())
	h.Update(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusInternalServerError, w.Code, w.Body.String())
	}
}

// TestFailoverHandler_Delete_RepoError tests when failoverRepo.DeleteByID fails
func TestFailoverHandler_Delete_RepoError(t *testing.T) {
	workingPool := apiTestDB.Pool()
	failoverRepo := failover.NewRepository(workingPool)

	// Create a group first
	ctx := context.Background()
	displayModel := "test-delete-repoerr-" + uuid.New().String()[:8]
	id1, id2 := uuid.New(), uuid.New()
	fg, err := failoverRepo.Upsert(ctx, displayModel, []uuid.UUID{id1, id2})
	if err != nil {
		t.Fatalf("Upsert failed: %v", err)
	}
	defer func() {
		_ = failoverRepo.Delete(ctx, displayModel)
	}()

	// Create handler with closed pool for delete
	closedPool := newClosedPool(t)
	failoverRepoClosed := failover.NewRepository(closedPool)
	modelRepo := model.NewRepository(closedPool)
	settingsRepo := settings.NewRepository(closedPool)
	h := NewFailoverHandler(closedPool, failoverRepoClosed, modelRepo, settingsRepo, nil)

	req, w := newChiRequest(http.MethodDelete, "/failover-groups/"+fg.ID.String(), nil)
	req = setChiURLParam(req, "id", fg.ID.String())
	h.Delete(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusInternalServerError, w.Code, w.Body.String())
	}
}

// TestFailoverHandler_Sync_SettingsSetError tests when settingsRepo.Set fails (error is logged, not returned)
func TestFailoverHandler_Sync_SettingsSetError(t *testing.T) {
	workingPool := apiTestDB.Pool()
	failoverRepo := failover.NewRepository(workingPool)
	modelRepo := model.NewRepository(workingPool)

	// Create mock settings store that always fails on Set
	mockSettings := &mockSettingsStore{
		setFn: func(ctx context.Context, key, value string) error {
			return fmt.Errorf("simulated settings set error")
		},
	}

	h := NewFailoverHandler(workingPool, failoverRepo, modelRepo, mockSettings, nil)

	req, w := newChiRequest(http.MethodPost, "/failover-groups/sync", nil)
	h.Sync(w, req)

	// Sync should still return 200 even if settings.Set fails (error is only logged)
	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusOK, w.Code, w.Body.String())
	}
}

// TestFailoverHandler_GetByModelUUID_RepoError tests when failoverRepo.List fails
func TestFailoverHandler_GetByModelUUID_RepoError(t *testing.T) {
	closedPool := newClosedPool(t)
	failoverRepo := failover.NewRepository(closedPool)
	modelRepo := model.NewRepository(closedPool)
	settingsRepo := settings.NewRepository(closedPool)
	h := NewFailoverHandler(closedPool, failoverRepo, modelRepo, settingsRepo, nil)

	unknownUUID := uuid.New()
	req, w := newChiRequest(http.MethodGet, "/failover-groups/by-model/"+unknownUUID.String(), nil)
	req = setChiURLParam(req, "model_uuid", unknownUUID.String())
	h.GetByModelUUID(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusInternalServerError, w.Code, w.Body.String())
	}
}

func TestFailoverHandler_Create_InvalidDisplayName(t *testing.T) {
	h := newIntegrationFailoverHandler()

	id1, id2 := uuid.New(), uuid.New()
	body := `{"display_model":"test-model","display_name":"bad\u0001name","entry_ids":["` + id1.String() + `","` + id2.String() + `"]}`
	req, w := newChiRequest(http.MethodPost, "/failover-groups/", strings.NewReader(body))
	h.Create(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusBadRequest, w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "invalid display name") {
		t.Errorf("expected error about display_name, got: %s", w.Body.String())
	}
}

func TestFailoverHandler_Create_InvalidDescription(t *testing.T) {
	h := newIntegrationFailoverHandler()

	id1, id2 := uuid.New(), uuid.New()
	longDesc := strings.Repeat("x", 501)
	body := `{"display_model":"test-model","description":"` + longDesc + `","entry_ids":["` + id1.String() + `","` + id2.String() + `"]}`
	req, w := newChiRequest(http.MethodPost, "/failover-groups/", strings.NewReader(body))
	h.Create(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusBadRequest, w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "invalid description") {
		t.Errorf("expected error about description, got: %s", w.Body.String())
	}
}

func TestFailoverHandler_Update_InvalidDisplayName(t *testing.T) {
	h := newIntegrationFailoverHandler()
	ctx := context.Background()

	displayModel := "test-update-invname-" + uuid.New().String()[:8]
	id1, id2 := uuid.New(), uuid.New()
	fg, err := h.failoverRepo.Upsert(ctx, displayModel, []uuid.UUID{id1, id2})
	if err != nil {
		t.Fatalf("Upsert failed: %v", err)
	}
	defer func() { _ = h.failoverRepo.Delete(ctx, displayModel) }()

	body := `{"display_name":"bad\u0001name"}`
	req, w := newChiRequest(http.MethodPut, "/failover-groups/"+fg.ID.String(), strings.NewReader(body))
	req = setChiURLParam(req, "id", fg.ID.String())
	h.Update(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusBadRequest, w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "invalid display name") {
		t.Errorf("expected error about display_name, got: %s", w.Body.String())
	}
}

func TestFailoverHandler_Update_InvalidDescription(t *testing.T) {
	h := newIntegrationFailoverHandler()
	ctx := context.Background()

	displayModel := "test-update-invdesc-" + uuid.New().String()[:8]
	id1, id2 := uuid.New(), uuid.New()
	fg, err := h.failoverRepo.Upsert(ctx, displayModel, []uuid.UUID{id1, id2})
	if err != nil {
		t.Fatalf("Upsert failed: %v", err)
	}
	defer func() { _ = h.failoverRepo.Delete(ctx, displayModel) }()

	longDesc := strings.Repeat("x", 501)
	body := `{"description":"` + longDesc + `"}`
	req, w := newChiRequest(http.MethodPut, "/failover-groups/"+fg.ID.String(), strings.NewReader(body))
	req = setChiURLParam(req, "id", fg.ID.String())
	h.Update(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusBadRequest, w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "invalid description") {
		t.Errorf("expected error about description, got: %s", w.Body.String())
	}
}

func TestFailoverHandler_Update_InvalidEntryEnabled(t *testing.T) {
	h := newIntegrationFailoverHandler()
	ctx := context.Background()

	displayModel := "test-update-inventry-" + uuid.New().String()[:8]
	id1, id2 := uuid.New(), uuid.New()
	fg, err := h.failoverRepo.Upsert(ctx, displayModel, []uuid.UUID{id1, id2})
	if err != nil {
		t.Fatalf("Upsert failed: %v", err)
	}
	defer func() { _ = h.failoverRepo.Delete(ctx, displayModel) }()

	// Build entry_enabled with 101 entries
	entryEnabled := make(map[string]bool)
	for range 101 {
		entryEnabled[uuid.New().String()] = true
	}
	eeJSON, _ := json.Marshal(entryEnabled)
	body := `{"entry_enabled":` + string(eeJSON) + `}`
	req, w := newChiRequest(http.MethodPut, "/failover-groups/"+fg.ID.String(), strings.NewReader(body))
	req = setChiURLParam(req, "id", fg.ID.String())
	h.Update(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusBadRequest, w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "invalid entry_enabled") {
		t.Errorf("expected error about entry_enabled, got: %s", w.Body.String())
	}
}

func TestFailoverHandler_Sync_CancelledContext(t *testing.T) {
	workingPool := apiTestDB.Pool()
	failoverRepo := failover.NewRepository(workingPool)
	modelRepo := model.NewRepository(workingPool)
	settingsRepo := settings.NewRepository(workingPool)
	h := NewFailoverHandler(workingPool, failoverRepo, modelRepo, settingsRepo, nil)

	cancelCtx, cancel := context.WithCancel(context.Background())
	cancel()

	req, w := newChiRequest(http.MethodPost, "/failover-groups/sync", nil)
	req = req.WithContext(cancelCtx)
	h.Sync(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusInternalServerError, w.Code, w.Body.String())
	}
}

func TestFailoverHandler_Candidates_CancelledContext(t *testing.T) {
	workingPool := apiTestDB.Pool()
	failoverRepo := failover.NewRepository(workingPool)
	modelRepo := model.NewRepository(workingPool)
	settingsRepo := settings.NewRepository(workingPool)
	h := NewFailoverHandler(workingPool, failoverRepo, modelRepo, settingsRepo, nil)

	cancelCtx, cancel := context.WithCancel(context.Background())
	cancel()

	req, w := newChiRequest(http.MethodGet, "/failover-groups/candidates", nil)
	req = req.WithContext(cancelCtx)
	h.Candidates(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusInternalServerError, w.Code, w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// Tests moved from coverage_gap_test.go
// ---------------------------------------------------------------------------

// TestGetTokenCounts tests FailoverHandler.getTokenCounts() which queries
// request_logs for models with hotel/ prefix and sums tokens in the last 30 days.
func TestGetTokenCounts(t *testing.T) {
	h := newIntegrationFailoverHandler()

	ctx := context.Background()
	pool := h.dbPool

	providerID := "00000000-0000-0000-0000-000000000001"

	// Clean slate: remove stale data from previous runs
	_, _ = pool.Exec(ctx, `DELETE FROM request_logs WHERE request_hash LIKE 'test-gtc-%'`)
	_, _ = pool.Exec(ctx, `DELETE FROM providers WHERE id = $1`, providerID)

	// Insert test provider (FK dependency for request_logs)
	_, err := pool.Exec(ctx, `
		INSERT INTO providers (id, name, base_url, encrypted_key, key_salt, masked_key, created_at, updated_at)
		VALUES ($1, 'test-provider', 'https://api.example.com', '', '', 'sk-***', now(), now())
	`, providerID)
	if err != nil {
		t.Fatalf("failed to insert test provider: %v", err)
	}

	// Insert test data into request_logs:
	// - hotel/gpt-4o: (100+50) + (200+100) + (0+0) = 450 total tokens
	// - hotel/claude-3: (150+75) = 225 total tokens
	// - openai/gpt-4: should NOT appear (not hotel/ prefix)
	_, err = pool.Exec(ctx, `
		INSERT INTO request_logs (provider_id, model_id, request_hash, status_code, tokens_prompt, tokens_completion, streaming, state, created_at)
		VALUES
			($1, 'hotel/gpt-4o', 'test-gtc-hash-1', 200, 100, 50, false, 'success', now()),
			($1, 'hotel/gpt-4o', 'test-gtc-hash-2', 200, 200, 100, false, 'success', now()),
			($1, 'hotel/claude-3', 'test-gtc-hash-3', 200, 150, 75, false, 'success', now()),
			($1, 'openai/gpt-4', 'test-gtc-hash-4', 200, 500, 250, false, 'success', now()),
			($1, 'hotel/gpt-4o', 'test-gtc-hash-5', 200, 0, 0, false, 'success', now())
	`, providerID)
	if err != nil {
		t.Fatalf("failed to insert test data: %v", err)
	}

	// Clean up after test
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM request_logs WHERE request_hash LIKE 'test-gtc-%'`)
		_, _ = pool.Exec(ctx, `DELETE FROM providers WHERE id = $1`, providerID)
	})

	// Verify token counts are correct for hotel/ models
	counts, err := h.getTokenCounts(ctx)
	if err != nil {
		t.Fatalf("getTokenCounts failed: %v", err)
	}

	if counts["hotel/gpt-4o"] != 450 {
		t.Errorf("hotel/gpt-4o count = %d, want 450", counts["hotel/gpt-4o"])
	}

	if counts["hotel/claude-3"] != 225 {
		t.Errorf("hotel/claude-3 count = %d, want 225", counts["hotel/claude-3"])
	}

	// openai/gpt-4 should NOT be in the map (not hotel/ prefix)
	if _, exists := counts["openai/gpt-4"]; exists {
		t.Error("openai/gpt-4 should not be in counts (not hotel/ prefix)")
	}

	// Empty case: delete hotel/ rows, should return empty map
	_, err = pool.Exec(ctx, `DELETE FROM request_logs WHERE request_hash LIKE 'test-gtc-%'`)
	if err != nil {
		t.Fatalf("failed to delete test rows: %v", err)
	}

	counts, err = h.getTokenCounts(ctx)
	if err != nil {
		t.Fatalf("getTokenCounts failed on empty case: %v", err)
	}

	if len(counts) != 0 {
		t.Errorf("expected empty map when no hotel/ rows exist, got %d entries", len(counts))
	}
}

// TestFailoverSync_Integration tests the Sync endpoint.
func TestFailoverSync_Integration(t *testing.T) {
	h := newIntegrationFailoverHandler()

	req, w := newChiRequest(http.MethodPost, "/failover-groups/sync", nil)

	h.Sync(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200 OK, got %d: %s", w.Code, w.Body.String())
	}

	var response map[string]any
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Verify response has expected structure (DeletedGroups, SyncErrors)
	if _, ok := response["deleted_groups"]; !ok {
		t.Error("expected 'deleted_groups' field in sync response")
	}
}

// ---------------------------------------------------------------------------
// Update DisplayModel tests
// ---------------------------------------------------------------------------

func TestFailoverHandler_Update_DisplayModel_Success(t *testing.T) {
	h := newIntegrationFailoverHandler()
	ctx := context.Background()

	displayModel := "test-rename-" + uuid.New().String()[:8]
	id1, id2 := uuid.New(), uuid.New()
	po := []uuid.UUID{id1, id2}

	fg, err := h.failoverRepo.Upsert(ctx, displayModel, po)
	if err != nil {
		t.Fatalf("Upsert failed: %v", err)
	}
	oldName := displayModel
	newName := "renamed-" + uuid.New().String()[:8]
	defer func() {
		_ = h.failoverRepo.Delete(ctx, oldName)
		_ = h.failoverRepo.Delete(ctx, newName)
	}()

	body := `{"display_model":"` + newName + `","priority_order":["` + id1.String() + `","` + id2.String() + `"],"entry_enabled":{"` + id1.String() + `":true,"` + id2.String() + `":true}}`
	req, w := newChiRequest(http.MethodPut, "/failover-groups/"+fg.ID.String(), strings.NewReader(body))
	req = setChiURLParam(req, "id", fg.ID.String())

	h.Update(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusOK, w.Code, w.Body.String())
	}

	var resp FailoverGroupResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.DisplayModel != newName {
		t.Errorf("DisplayModel = %q, want %q", resp.DisplayModel, newName)
	}
}

func TestFailoverHandler_Update_DisplayModel_Conflict(t *testing.T) {
	h := newIntegrationFailoverHandler()
	ctx := context.Background()

	// Create two groups
	name1 := "test-rename-first-" + uuid.New().String()[:8]
	name2 := "test-rename-second-" + uuid.New().String()[:8]
	id1, id2 := uuid.New(), uuid.New()
	po := []uuid.UUID{id1, id2}

	fg1, err := h.failoverRepo.Upsert(ctx, name1, po)
	if err != nil {
		t.Fatalf("Upsert failed for first group: %v", err)
	}
	_, err = h.failoverRepo.Upsert(ctx, name2, po)
	if err != nil {
		t.Fatalf("Upsert failed for second group: %v", err)
	}
	defer func() {
		_ = h.failoverRepo.Delete(ctx, name1)
		_ = h.failoverRepo.Delete(ctx, name2)
	}()

	// Try to rename first group to second group's name (should conflict)
	body := `{"display_model":"` + name2 + `"}`
	req, w := newChiRequest(http.MethodPut, "/failover-groups/"+fg1.ID.String(), strings.NewReader(body))
	req = setChiURLParam(req, "id", fg1.ID.String())

	h.Update(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusConflict, w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "already exists") {
		t.Errorf("expected error about already exists, got: %s", w.Body.String())
	}
}

func TestFailoverHandler_Update_DisplayModel_InvalidEmpty(t *testing.T) {
	h := newIntegrationFailoverHandler()
	ctx := context.Background()

	displayModel := "test-rename-empty-" + uuid.New().String()[:8]
	id1, id2 := uuid.New(), uuid.New()
	po := []uuid.UUID{id1, id2}

	fg, err := h.failoverRepo.Upsert(ctx, displayModel, po)
	if err != nil {
		t.Fatalf("Upsert failed: %v", err)
	}
	defer func() {
		_ = h.failoverRepo.Delete(ctx, displayModel)
	}()

	body := `{"display_model":""}`
	req, w := newChiRequest(http.MethodPut, "/failover-groups/"+fg.ID.String(), strings.NewReader(body))
	req = setChiURLParam(req, "id", fg.ID.String())

	h.Update(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusBadRequest, w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "invalid display model") {
		t.Errorf("expected error about invalid display model, got: %s", w.Body.String())
	}
}

func TestFailoverHandler_Update_DisplayModel_SameName(t *testing.T) {
	h := newIntegrationFailoverHandler()
	ctx := context.Background()

	displayModel := "test-samename-" + uuid.New().String()[:8]
	id1, id2 := uuid.New(), uuid.New()
	po := []uuid.UUID{id1, id2}

	fg, err := h.failoverRepo.Upsert(ctx, displayModel, po)
	if err != nil {
		t.Fatalf("Upsert failed: %v", err)
	}
	defer func() {
		_ = h.failoverRepo.Delete(ctx, displayModel)
	}()

	// Send update with display_model set to the SAME name (should skip uniqueness check)
	body := `{"display_model":"` + displayModel + `","group_enabled":false}`
	req, w := newChiRequest(http.MethodPut, "/failover-groups/"+fg.ID.String(), strings.NewReader(body))
	req = setChiURLParam(req, "id", fg.ID.String())

	h.Update(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusOK, w.Code, w.Body.String())
	}

	var resp FailoverGroupResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.DisplayModel != displayModel {
		t.Errorf("DisplayModel = %q, want %q", resp.DisplayModel, displayModel)
	}
	if resp.GroupEnabled != false {
		t.Errorf("GroupEnabled = %v, want false", resp.GroupEnabled)
	}
}
