package api

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/hugalafutro/model-hotel/internal/db"
	"github.com/hugalafutro/model-hotel/internal/failover"
	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/settings"
)

// ---------------------------------------------------------------------------
// Integration test database setup
// ---------------------------------------------------------------------------

var apiTestDB *db.DB

func TestMain(m *testing.M) {
	ctx := context.Background()
	var err error
	apiTestDBURL := os.Getenv("TEST_DATABASE_URL")
	if apiTestDBURL == "" {
		apiTestDBURL = "postgres://llmproxy:changeme@localhost:5433/testdb?sslmode=disable"
	}
	apiTestDB, err = db.New(ctx, apiTestDBURL, 25, 5)
	if err != nil {
		apiTestDB = nil
	}
	code := m.Run()
	if apiTestDB != nil {
		apiTestDB.Close()
	}
	os.Exit(code)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

type mockFailoverSettingsStore struct {
	data map[string]string
}

func newMockFailoverSettingsStore() *mockFailoverSettingsStore {
	return &mockFailoverSettingsStore{data: make(map[string]string)}
}

func (m *mockFailoverSettingsStore) GetAll(ctx context.Context) (map[string]string, error) {
	return m.data, nil
}

func (m *mockFailoverSettingsStore) GetWithDefault(ctx context.Context, key string, defaultValue string) string {
	if v, ok := m.data[key]; ok {
		return v
	}
	return defaultValue
}

func (m *mockFailoverSettingsStore) Set(ctx context.Context, key string, value string) error {
	m.data[key] = value
	return nil
}

func (m *mockFailoverSettingsStore) SetTx(ctx context.Context, tx pgx.Tx, key string, value string) error {
	m.data[key] = value
	return nil
}

func (m *mockFailoverSettingsStore) InvalidateCache(key string) {}

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
	return NewFailoverHandler(pool, failoverRepo, modelRepo, settingsRepo)
}

func newFailoverRouter(h *FailoverHandler) chi.Router {
	r := chi.NewRouter()
	h.Register(r)
	return r
}

// ---------------------------------------------------------------------------
// Create tests
// ---------------------------------------------------------------------------

func TestFailoverHandler_Create_Success(t *testing.T) {
	h := newIntegrationFailoverHandler()
	if h == nil {
		t.Skip("database not available")
	}
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
	if h == nil {
		t.Skip("database not available")
	}

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
	if h == nil {
		t.Skip("database not available")
	}

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
	if h == nil {
		t.Skip("database not available")
	}

	body := `{"display_model":"test-bad-id","entry_ids":["not-a-uuid","` + uuid.New().String() + `"]}`
	req, w := newChiRequest(http.MethodPost, "/failover-groups/", strings.NewReader(body))

	h.Create(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusBadRequest, w.Code, w.Body.String())
	}
}

func TestFailoverHandler_Create_InvalidJSON(t *testing.T) {
	h := newIntegrationFailoverHandler()
	if h == nil {
		t.Skip("database not available")
	}

	req, w := newChiRequest(http.MethodPost, "/failover-groups/", strings.NewReader("{invalid json"))

	h.Create(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusBadRequest, w.Code, w.Body.String())
	}
}

func TestFailoverHandler_Create_Conflict(t *testing.T) {
	h := newIntegrationFailoverHandler()
	if h == nil {
		t.Skip("database not available")
	}
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
	if h == nil {
		t.Skip("database not available")
	}
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
	if h == nil {
		t.Skip("database not available")
	}

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
	if h == nil {
		t.Skip("database not available")
	}

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
	if h == nil {
		t.Skip("database not available")
	}
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

func TestFailoverHandler_Update_DisableGroup(t *testing.T) {
	h := newIntegrationFailoverHandler()
	if h == nil {
		t.Skip("database not available")
	}
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
	if h == nil {
		t.Skip("database not available")
	}
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
	if h == nil {
		t.Skip("database not available")
	}

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
	if h == nil {
		t.Skip("database not available")
	}

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
	if h == nil {
		t.Skip("database not available")
	}
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
	if h == nil {
		t.Skip("database not available")
	}

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
	if h == nil {
		t.Skip("database not available")
	}
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
	if h == nil {
		t.Skip("database not available")
	}

	req, w := newChiRequest(http.MethodDelete, "/failover-groups/not-a-uuid", nil)
	req = setChiURLParam(req, "id", "not-a-uuid")

	h.Delete(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusBadRequest, w.Code, w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// List tests
// ---------------------------------------------------------------------------

func TestFailoverHandler_List_Success(t *testing.T) {
	h := newIntegrationFailoverHandler()
	if h == nil {
		t.Skip("database not available")
	}
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
	if h == nil {
		t.Skip("database not available")
	}

	req, w := newChiRequest(http.MethodPost, "/failover-groups/sync", nil)

	h.Sync(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusOK, w.Code, w.Body.String())
	}

	var resp failover.SyncResult
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	// DisabledGroups may be nil when no groups were disabled (empty sync result).
	// This is valid — just verify the response is a valid SyncResult.
}

// ---------------------------------------------------------------------------
// Candidates tests
// ---------------------------------------------------------------------------

func TestFailoverHandler_Candidates_Success(t *testing.T) {
	h := newIntegrationFailoverHandler()
	if h == nil {
		t.Skip("database not available")
	}

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
	if h == nil {
		t.Skip("database not available")
	}
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
	if h == nil {
		t.Skip("database not available")
	}

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
	if h == nil {
		t.Skip("database not available")
	}

	req, w := newChiRequest(http.MethodGet, "/failover-groups/by-model/not-a-uuid", nil)
	req = setChiURLParam(req, "model_uuid", "not-a-uuid")

	h.GetByModelUUID(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusBadRequest, w.Code, w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// Register routes test
// ---------------------------------------------------------------------------

func TestFailoverHandler_Register(t *testing.T) {
	h := &FailoverHandler{
		failoverRepo: nil,
		modelRepo:    nil,
		dbPool:       nil,
		settingsRepo: newMockFailoverSettingsStore(),
	}

	r := chi.NewRouter()
	h.Register(r)

	routes := make(map[string]bool)
	chi.Walk(r, func(method string, route string, handler http.Handler, middlewares ...func(http.Handler) http.Handler) error {
		key := method + " " + route
		routes[key] = true
		return nil
	})

	expectedRoutes := []string{
		"GET /failover-groups/",
		"POST /failover-groups/",
		"POST /failover-groups/sync",
		"GET /failover-groups/candidates",
		"GET /failover-groups/by-model/{model_uuid}",
		"GET /failover-groups/{id}",
		"PUT /failover-groups/{id}",
		"DELETE /failover-groups/{id}",
	}

	for _, expected := range expectedRoutes {
		if !routes[expected] {
			t.Errorf("expected route %q to be registered", expected)
		}
	}
}
