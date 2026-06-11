package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/failover"
	"github.com/hugalafutro/model-hotel/internal/model"
)

func TestBuildDiscoveryDiff(t *testing.T) {
	tests := []struct {
		name          string
		snapshot      map[string]modelSnapshot
		upsertedIDs   []string
		disabledRefs  []model.DisabledModelRef
		wantAdded     []string
		wantReenabled []string
		wantDisabled  []string
	}{
		{
			name:        "new model",
			snapshot:    map[string]modelSnapshot{},
			upsertedIDs: []string{"model-new"},
			wantAdded:   []string{"model-new"},
		},
		{
			name: "reappeared model (discovery-disabled before)",
			snapshot: map[string]modelSnapshot{
				"model-back": {enabled: false, disabledManually: false},
			},
			upsertedIDs:   []string{"model-back"},
			wantReenabled: []string{"model-back"},
		},
		{
			name: "manually disabled model stays excluded",
			snapshot: map[string]modelSnapshot{
				"model-manual": {enabled: false, disabledManually: true},
			},
			upsertedIDs: []string{"model-manual"},
		},
		{
			name: "already enabled model is no change",
			snapshot: map[string]modelSnapshot{
				"model-same": {enabled: true},
			},
			upsertedIDs: []string{"model-same"},
		},
		{
			name:     "not listed model",
			snapshot: map[string]modelSnapshot{"model-gone": {enabled: true}},
			disabledRefs: []model.DisabledModelRef{
				{ID: uuid.New(), ModelID: "model-gone"},
			},
			wantDisabled: []string{"model-gone"},
		},
		{
			name: "mixed scan",
			snapshot: map[string]modelSnapshot{
				"model-kept": {enabled: true},
				"model-back": {enabled: false, disabledManually: false},
				"model-gone": {enabled: true},
			},
			upsertedIDs: []string{"model-kept", "model-back", "model-new"},
			disabledRefs: []model.DisabledModelRef{
				{ID: uuid.New(), ModelID: "model-gone"},
			},
			wantAdded:     []string{"model-new"},
			wantReenabled: []string{"model-back"},
			wantDisabled:  []string{"model-gone"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			diff := buildDiscoveryDiff(tt.snapshot, tt.upsertedIDs, tt.disabledRefs)

			assertChanges := func(field string, got []ModelChange, wantIDs []string, wantReason string) {
				t.Helper()
				if len(got) != len(wantIDs) {
					t.Fatalf("%s: expected %d changes, got %+v", field, len(wantIDs), got)
				}
				for i, want := range wantIDs {
					if got[i].ModelID != want {
						t.Errorf("%s[%d]: expected model %q, got %q", field, i, want, got[i].ModelID)
					}
					if got[i].Reason != wantReason {
						t.Errorf("%s[%d]: expected reason %q, got %q", field, i, wantReason, got[i].Reason)
					}
				}
			}
			assertChanges("added", diff.Added, tt.wantAdded, changeReasonNewModel)
			assertChanges("reenabled", diff.Reenabled, tt.wantReenabled, changeReasonReappeared)
			assertChanges("disabled", diff.Disabled, tt.wantDisabled, changeReasonNotListed)
		})
	}
}

func TestDiscoveryDiff_MergeSyncResult(t *testing.T) {
	res := &failover.SyncResult{
		DeletedGroups: []failover.DeletedGroupInfo{{DisplayModel: "gone-group"}},
		UpdatedGroups: []failover.UpdatedGroupInfo{{DisplayModel: "changed-group"}},
	}

	// A nil diff (discover-all without a snapshot) must not panic.
	var nilDiff *DiscoveryDiff
	nilDiff.mergeSyncResult(res)

	diff := &DiscoveryDiff{}
	diff.mergeSyncResult(nil)
	if len(diff.FailoverDeletedGroups) != 0 || len(diff.FailoverUpdatedGroups) != 0 {
		t.Errorf("expected no failover changes after nil merge, got %+v", diff)
	}

	diff.mergeSyncResult(res)
	diff.mergeSyncResult(&failover.SyncResult{})
	if len(diff.FailoverDeletedGroups) != 1 || diff.FailoverDeletedGroups[0].DisplayModel != "gone-group" {
		t.Errorf("expected merged deleted group, got %+v", diff.FailoverDeletedGroups)
	}
	if len(diff.FailoverUpdatedGroups) != 1 || diff.FailoverUpdatedGroups[0].DisplayModel != "changed-group" {
		t.Errorf("expected merged updated group, got %+v", diff.FailoverUpdatedGroups)
	}
}

// TestDiscoverProviderModels_RenameScenario reproduces the original bug end to
// end: a provider renames model B to C between two scans while two other
// providers keep serving B. The second scan must disable B, prune its UUID
// from the failover group (self-heal), add C, and report all of it in the
// response diff.
func TestDiscoverProviderModels_RenameScenario(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)
	ctx := context.Background()
	pool := h.Pool().Pool()

	var listingMu sync.Mutex
	listing := []string{"rename-model-a", "rename-model-b"}
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/v1/models" {
			return
		}
		listingMu.Lock()
		defer listingMu.Unlock()
		data := make([]map[string]interface{}, 0, len(listing))
		for _, id := range listing {
			data = append(data, map[string]interface{}{"id": id, "owned_by": "test", "object": "model"})
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"data": data})
	}))
	defer mockServer.Close()

	// Provider 1 is the one being rediscovered.
	providerData := fmt.Sprintf(`{"name":"rename-scenario-p1","base_url":"%s/v1","api_key":"sk-test"}`, mockServer.URL)
	req := httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create provider1: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var provider1 struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &provider1); err != nil {
		t.Fatalf("decode provider1: %v", err)
	}

	// Providers 2 and 3 keep serving model B so its failover group survives
	// provider 1's rename with two members (updated, not deleted).
	for i, name := range []string{"rename-scenario-p2", "rename-scenario-p3"} {
		req = httptest.NewRequest(http.MethodPost, "/providers",
			strings.NewReader(fmt.Sprintf(`{"name":"%s","base_url":"https://api.example.com/v1","api_key":"sk-test"}`, name)))
		req.Header.Set("Authorization", "Bearer test-admin-token")
		req.Header.Set("Content-Type", "application/json")
		w = httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("create %s: expected 201, got %d: %s", name, w.Code, w.Body.String())
		}
		var created struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
			t.Fatalf("decode %s: %v", name, err)
		}
		_, err := pool.Exec(ctx,
			`INSERT INTO models (id, provider_id, model_id, name, enabled) VALUES ($1, $2, $3, $4, true)`,
			uuid.New(), created.ID, "rename-model-b", fmt.Sprintf("Rename Model B %d", i+2))
		if err != nil {
			t.Fatalf("insert model-b for %s: %v", name, err)
		}
	}
	model.InvalidateModelCache()

	discover := func() *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/providers/"+provider1.ID+"/discover", http.NoBody)
		req.Header.Set("Authorization", "Bearer test-admin-token")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("discover: expected 200, got %d: %s", w.Code, w.Body.String())
		}
		return w
	}

	// First scan: provider 1 lists A and B; the model-b group forms with 3 members.
	discover()

	var modelBUUID uuid.UUID
	if err := pool.QueryRow(ctx,
		`SELECT id FROM models WHERE provider_id = $1 AND model_id = 'rename-model-b'`,
		provider1.ID,
	).Scan(&modelBUUID); err != nil {
		t.Fatalf("lookup provider1 model-b UUID: %v", err)
	}

	var groupOrder string
	if err := pool.QueryRow(ctx,
		`SELECT priority_order::text FROM model_failover_groups WHERE display_model = 'rename-model-b'`,
	).Scan(&groupOrder); err != nil {
		t.Fatalf("expected model-b failover group after first scan: %v", err)
	}
	if !strings.Contains(groupOrder, modelBUUID.String()) {
		t.Fatalf("expected provider1 model-b %s in group order %s", modelBUUID, groupOrder)
	}

	// Second scan: B is renamed to C.
	listingMu.Lock()
	listing = []string{"rename-model-a", "rename-model-c"}
	listingMu.Unlock()
	w = discover()

	var resp struct {
		Diff DiscoveryDiff `json:"diff"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode discover response: %v", err)
	}

	if len(resp.Diff.Added) != 1 || resp.Diff.Added[0].ModelID != "rename-model-c" || resp.Diff.Added[0].Reason != changeReasonNewModel {
		t.Errorf("expected added=[rename-model-c/new_model], got %+v", resp.Diff.Added)
	}
	if len(resp.Diff.Disabled) != 1 || resp.Diff.Disabled[0].ModelID != "rename-model-b" || resp.Diff.Disabled[0].Reason != changeReasonNotListed {
		t.Errorf("expected disabled=[rename-model-b/not_listed], got %+v", resp.Diff.Disabled)
	}
	if len(resp.Diff.Reenabled) != 0 {
		t.Errorf("expected no reenabled models, got %+v", resp.Diff.Reenabled)
	}
	foundUpdate := false
	for _, ug := range resp.Diff.FailoverUpdatedGroups {
		if ug.DisplayModel == "rename-model-b" {
			foundUpdate = true
			if len(ug.RemovedModelIDs) != 1 || ug.RemovedModelIDs[0] != modelBUUID.String() {
				t.Errorf("expected removed_model_ids=[%s], got %v", modelBUUID, ug.RemovedModelIDs)
			}
		}
	}
	if !foundUpdate {
		t.Errorf("expected failover_updated_groups entry for rename-model-b, got %+v", resp.Diff.FailoverUpdatedGroups)
	}

	// DB state: B disabled (not manually), C present, B's UUID pruned from the group.
	var enabled, disabledManually bool
	if err := pool.QueryRow(ctx,
		`SELECT enabled, disabled_manually FROM models WHERE id = $1`, modelBUUID,
	).Scan(&enabled, &disabledManually); err != nil {
		t.Fatalf("lookup model-b state: %v", err)
	}
	if enabled || disabledManually {
		t.Errorf("expected model-b discovery-disabled (enabled=false, disabled_manually=false), got enabled=%v disabled_manually=%v", enabled, disabledManually)
	}

	var modelCCount int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM models WHERE provider_id = $1 AND model_id = 'rename-model-c' AND enabled = true`,
		provider1.ID,
	).Scan(&modelCCount); err != nil {
		t.Fatalf("count model-c: %v", err)
	}
	if modelCCount != 1 {
		t.Errorf("expected 1 enabled rename-model-c for provider1, got %d", modelCCount)
	}

	if err := pool.QueryRow(ctx,
		`SELECT priority_order::text FROM model_failover_groups WHERE display_model = 'rename-model-b'`,
	).Scan(&groupOrder); err != nil {
		t.Fatalf("expected model-b group to survive with 2 members: %v", err)
	}
	if strings.Contains(groupOrder, modelBUUID.String()) {
		t.Errorf("expected provider1 model-b %s pruned from group order, still present: %s", modelBUUID, groupOrder)
	}
}

// TestDiscoverProviderModels_DisabledSyncError verifies that a failover sync
// failure for a model that just left the listing fails the request.
func TestDiscoverProviderModels_DisabledSyncError(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	var listingMu sync.Mutex
	listing := []string{"dse-model-a", "dse-model-b"}
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/v1/models" {
			return
		}
		listingMu.Lock()
		defer listingMu.Unlock()
		data := make([]map[string]interface{}, 0, len(listing))
		for _, id := range listing {
			data = append(data, map[string]interface{}{"id": id, "owned_by": "test", "object": "model"})
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"data": data})
	}))
	defer mockServer.Close()

	providerData := fmt.Sprintf(`{"name":"disabled-sync-error-test","base_url":"%s/v1","api_key":"sk-test"}`, mockServer.URL)
	req := httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create provider: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode created provider: %v", err)
	}

	discover := func() *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/providers/"+created.ID+"/discover", http.NoBody)
		req.Header.Set("Authorization", "Bearer test-admin-token")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	// First scan succeeds and imports both models.
	if w := discover(); w.Code != http.StatusOK {
		t.Fatalf("first discover: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Fail the sync only for the model that leaves the listing, so the
	// discovered-models sync loop passes and the disabled-refs loop errors.
	origFailoverRepoSyncForModel := failoverRepoSyncForModel
	defer func() { failoverRepoSyncForModel = origFailoverRepoSyncForModel }()
	failoverRepoSyncForModel = func(repo *failover.Repository, ctx context.Context, modelID string) (*failover.SyncResult, error) {
		if modelID == "dse-model-b" {
			return nil, errors.New("sync for disabled model error")
		}
		return repo.SyncForModel(ctx, modelID)
	}

	listingMu.Lock()
	listing = []string{"dse-model-a"}
	listingMu.Unlock()

	w = discover()
	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "failed to sync failover group for disabled model") {
		t.Errorf("expected error about disabled-model sync, got %q", w.Body.String())
	}
}

// TestDiscoverAllModels_DisabledSyncError verifies that in discover-all a
// failover sync failure for a newly disabled model is logged and skipped
// without failing the scan, and the rest of the diff is still reported.
func TestDiscoverAllModels_DisabledSyncError(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	var listingMu sync.Mutex
	listing := []string{"dase-model-a", "dase-model-b"}
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/v1/models" {
			return
		}
		listingMu.Lock()
		defer listingMu.Unlock()
		data := make([]map[string]interface{}, 0, len(listing))
		for _, id := range listing {
			data = append(data, map[string]interface{}{"id": id, "owned_by": "test", "object": "model"})
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"data": data})
	}))
	defer mockServer.Close()

	providerData := fmt.Sprintf(`{"name":"dase-test","base_url":"%s/v1","api_key":"sk-test"}`, mockServer.URL)
	req := httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create provider: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	discoverAll := func() *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/providers/discover-all", http.NoBody)
		req.Header.Set("Authorization", "Bearer test-admin-token")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	if w := discoverAll(); w.Code != http.StatusOK {
		t.Fatalf("first discover-all: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	origFailoverRepoSyncForModel := failoverRepoSyncForModel
	defer func() { failoverRepoSyncForModel = origFailoverRepoSyncForModel }()
	failoverRepoSyncForModel = func(repo *failover.Repository, ctx context.Context, modelID string) (*failover.SyncResult, error) {
		if modelID == "dase-model-b" {
			return nil, errors.New("sync for disabled model error")
		}
		return repo.SyncForModel(ctx, modelID)
	}

	listingMu.Lock()
	listing = []string{"dase-model-a"}
	listingMu.Unlock()

	w = discoverAll()
	if w.Code != http.StatusOK {
		t.Fatalf("second discover-all: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Results []DiscoverAllResult `json:"results"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode discover-all response: %v", err)
	}
	if len(resp.Results) != 1 {
		t.Fatalf("expected 1 result, got %+v", resp.Results)
	}
	result := resp.Results[0]
	if result.Error != "" {
		t.Errorf("expected scan to succeed despite sync error, got error %q", result.Error)
	}
	if result.Diff == nil {
		t.Fatal("expected diff in result")
	}
	if len(result.Diff.Disabled) != 1 || result.Diff.Disabled[0].ModelID != "dase-model-b" {
		t.Errorf("expected disabled=[dase-model-b], got %+v", result.Diff.Disabled)
	}
}
