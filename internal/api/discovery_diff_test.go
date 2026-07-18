package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/failover"
	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/provider"
)

func TestBuildDiscoveryDiff(t *testing.T) {
	tests := []struct {
		name          string
		snapshot      map[string]ModelSnapshot
		upserted      []*model.Model
		disabledRefs  []model.DisabledModelRef
		wantAdded     []string
		wantReenabled []string
		wantDisabled  []string
	}{
		{
			name:      "new model",
			snapshot:  map[string]ModelSnapshot{},
			upserted:  models("model-new"),
			wantAdded: []string{"model-new"},
		},
		{
			name: "reappeared model (discovery-disabled before)",
			snapshot: map[string]ModelSnapshot{
				"model-back": {enabled: false, disabledManually: false},
			},
			upserted:      models("model-back"),
			wantReenabled: []string{"model-back"},
		},
		{
			name: "manually disabled model stays excluded",
			snapshot: map[string]ModelSnapshot{
				"model-manual": {enabled: false, disabledManually: true},
			},
			upserted: models("model-manual"),
		},
		{
			name: "already enabled model is no change",
			snapshot: map[string]ModelSnapshot{
				"model-same": {enabled: true},
			},
			upserted: models("model-same"),
		},
		{
			name:     "not listed model",
			snapshot: map[string]ModelSnapshot{"model-gone": {enabled: true}},
			disabledRefs: []model.DisabledModelRef{
				{ID: uuid.New(), ModelID: "model-gone"},
			},
			wantDisabled: []string{"model-gone"},
		},
		{
			name: "mixed scan",
			snapshot: map[string]ModelSnapshot{
				"model-kept": {enabled: true},
				"model-back": {enabled: false, disabledManually: false},
				"model-gone": {enabled: true},
			},
			upserted: models("model-kept", "model-back", "model-new"),
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
			diff := BuildDiscoveryDiff(tt.snapshot, tt.upserted, tt.disabledRefs)

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

// models builds bare *model.Model values carrying only a ModelID, for the
// membership-classification cases (no pricing/context fields, so no metadata
// updates are detected).
func models(ids ...string) []*model.Model {
	out := make([]*model.Model, len(ids))
	for i, id := range ids {
		out[i] = &model.Model{ModelID: id}
	}
	return out
}

func TestBuildDiscoveryDiff_MetadataChanges(t *testing.T) {
	// An existing, still-enabled model whose pricing/context fields shift is
	// reported as an Updated entry — not Added or Reenabled.
	tests := []struct {
		name        string
		prev        ModelSnapshot
		model       *model.Model
		wantChanges []FieldChange
	}{
		{
			// A live (provider-reported) price change overwrites on upsert, so it
			// is reported.
			name: "live input price changed",
			prev: ModelSnapshot{enabled: true, inputPrice: new(float64(1))},
			model: &model.Model{
				ModelID: "m", InputPricePerMillion: new(float64(2)),
				LiveMeta: model.LiveMetaFields{InputPrice: true},
			},
			wantChanges: []FieldChange{
				{Field: changeFieldInputPrice, Old: new(float64(1)), New: new(float64(2))},
			},
		},
		{
			// THE noise killer: a non-live (catalog/models.dev) value change is
			// fill-only at upsert — the stored value is kept — so it must not be
			// reported. This is the cross-restart source oscillation that used to
			// flood the modal with phantom price flips.
			name:        "non-live value change is not reported",
			prev:        ModelSnapshot{enabled: true, inputPrice: new(float64(1))},
			model:       &model.Model{ModelID: "m", InputPricePerMillion: new(float64(2))},
			wantChanges: nil,
		},
		{
			// Filling a previously-unset value is reported even when non-live:
			// Upsert fills the gap, so it is a genuine new value.
			name:  "context length set from unset (non-live fill)",
			prev:  ModelSnapshot{enabled: true},
			model: &model.Model{ModelID: "m", ContextLength: new(8192)},
			wantChanges: []FieldChange{
				{Field: changeFieldContextLength, Old: nil, New: new(float64(8192))},
			},
		},
		{
			// A scan that omits a value must NOT report "value → unset": Upsert
			// preserves the stored value, so the diff stays quiet.
			name:        "scan omits a value — preserved, not reported",
			prev:        ModelSnapshot{enabled: true, outputPrice: new(float64(5))},
			model:       &model.Model{ModelID: "m"},
			wantChanges: nil,
		},
		{
			name:  "value gained from unset",
			prev:  ModelSnapshot{enabled: true},
			model: &model.Model{ModelID: "m", OutputPricePerMillion: new(float64(3))},
			wantChanges: []FieldChange{
				{Field: changeFieldOutputPrice, Old: nil, New: new(float64(3))},
			},
		},
		{
			name: "multiple live fields change at once",
			prev: ModelSnapshot{
				enabled:         true,
				inputPriceCache: new(0.5),
				contextLength:   new(131072),
			},
			model: &model.Model{
				ModelID:                      "m",
				InputPricePerMillionCacheHit: new(0.25),
				ContextLength:                new(262144),
				LiveMeta:                     model.LiveMetaFields{InputPriceCache: true, ContextLength: true},
			},
			wantChanges: []FieldChange{
				{Field: changeFieldInputPriceCache, Old: new(0.5), New: new(0.25)},
				{Field: changeFieldContextLength, Old: new(float64(131072)), New: new(float64(262144))},
			},
		},
		{
			// max_output_tokens is intentionally not tracked, so a change to it
			// alone produces no update.
			name: "max output tokens change is ignored",
			prev: ModelSnapshot{enabled: true},
			model: &model.Model{ModelID: "m", MaxOutputTokens: new(8192),
				LiveMeta: model.LiveMetaFields{MaxOutputTokens: true}},
			wantChanges: nil,
		},
		{
			// Binary-vs-decimal unit noise (262144 vs 262000) is within tolerance,
			// even for a live field.
			name: "context length unit difference is ignored",
			prev: ModelSnapshot{enabled: true, contextLength: new(262144)},
			model: &model.Model{ModelID: "m", ContextLength: new(262000),
				LiveMeta: model.LiveMetaFields{ContextLength: true}},
			wantChanges: nil,
		},
		{
			// A real context-window jump (200K → 256K) on a live field is well
			// past tolerance.
			name: "real live context length jump is reported",
			prev: ModelSnapshot{enabled: true, contextLength: new(200000)},
			model: &model.Model{ModelID: "m", ContextLength: new(256000),
				LiveMeta: model.LiveMetaFields{ContextLength: true}},
			wantChanges: []FieldChange{
				{Field: changeFieldContextLength, Old: new(float64(200000)), New: new(float64(256000))},
			},
		},
		{
			// Float32 storage jitter on a live price must not register.
			name: "price float32 jitter is ignored",
			prev: ModelSnapshot{enabled: true, inputPrice: new(float64(float32(0.28)))},
			model: &model.Model{ModelID: "m", InputPricePerMillion: new(0.28),
				LiveMeta: model.LiveMetaFields{InputPrice: true}},
			wantChanges: nil,
		},
		{
			name: "unchanged live values produce no update",
			prev: ModelSnapshot{enabled: true, inputPrice: new(float64(1)), contextLength: new(8192)},
			model: &model.Model{ModelID: "m", InputPricePerMillion: new(float64(1)), ContextLength: new(8192),
				LiveMeta: model.LiveMetaFields{InputPrice: true, ContextLength: true}},
			wantChanges: nil,
		},
		{
			name:        "both unset is not a change",
			prev:        ModelSnapshot{enabled: true},
			model:       &model.Model{ModelID: "m"},
			wantChanges: nil,
		},
		{
			// A model the user has manually disabled is skipped entirely: even a
			// genuine live price change on a hidden model must not raise the badge.
			name: "manually disabled model is not reported",
			prev: ModelSnapshot{enabled: false, disabledManually: true, inputPrice: new(float64(1))},
			model: &model.Model{
				ModelID: "m", InputPricePerMillion: new(float64(2)),
				LiveMeta: model.LiveMetaFields{InputPrice: true},
			},
			wantChanges: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snapshot := map[string]ModelSnapshot{"m": tt.prev}
			diff := BuildDiscoveryDiff(snapshot, []*model.Model{tt.model}, nil)

			if len(diff.Added) != 0 || len(diff.Reenabled) != 0 {
				t.Fatalf("expected no add/reenable, got added=%v reenabled=%v", diff.Added, diff.Reenabled)
			}
			if len(tt.wantChanges) == 0 {
				if len(diff.Updated) != 0 {
					t.Fatalf("expected no updates, got %+v", diff.Updated)
				}
				return
			}
			if len(diff.Updated) != 1 {
				t.Fatalf("expected 1 updated model, got %+v", diff.Updated)
			}
			got := diff.Updated[0]
			if got.ModelID != "m" {
				t.Errorf("model id = %q, want m", got.ModelID)
			}
			if !reflect.DeepEqual(got.Changes, tt.wantChanges) {
				t.Errorf("changes mismatch:\n got %+v\nwant %+v", got.Changes, tt.wantChanges)
			}
		})
	}
}

func TestBuildDiscoveryDiff_NewAndReenabledSkipMetadata(t *testing.T) {
	// A brand-new or reappearing model is classified by membership only; its
	// field values are not also diffed (no snapshot baseline to compare).
	snapshot := map[string]ModelSnapshot{
		"back": {enabled: false, disabledManually: false, inputPrice: new(float64(1))},
	}
	upserted := []*model.Model{
		{ModelID: "new", InputPricePerMillion: new(float64(9))},
		{ModelID: "back", InputPricePerMillion: new(float64(2))},
	}
	diff := BuildDiscoveryDiff(snapshot, upserted, nil)

	if len(diff.Added) != 1 || diff.Added[0].ModelID != "new" {
		t.Errorf("expected added=[new], got %+v", diff.Added)
	}
	if len(diff.Reenabled) != 1 || diff.Reenabled[0].ModelID != "back" {
		t.Errorf("expected reenabled=[back], got %+v", diff.Reenabled)
	}
	if len(diff.Updated) != 0 {
		t.Errorf("expected no metadata updates, got %+v", diff.Updated)
	}
}

func TestDiscoveryDiff_MergeSyncResult(t *testing.T) {
	res := &failover.SyncResult{
		DeletedGroups:  []failover.DeletedGroupInfo{{DisplayModel: "gone-group"}},
		UpdatedGroups:  []failover.UpdatedGroupInfo{{DisplayModel: "changed-group"}},
		DisabledGroups: []failover.DisabledGroupInfo{{DisplayModel: "undersized-group", EffectiveCount: 1}},
	}

	// A nil diff (discover-all without a snapshot) must not panic.
	var nilDiff *DiscoveryDiff
	nilDiff.mergeSyncResult(res)

	diff := &DiscoveryDiff{}
	diff.mergeSyncResult(nil)
	if len(diff.FailoverDeletedGroups) != 0 || len(diff.FailoverUpdatedGroups) != 0 || len(diff.FailoverDisabledGroups) != 0 {
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
	if len(diff.FailoverDisabledGroups) != 1 || diff.FailoverDisabledGroups[0].DisplayModel != "undersized-group" {
		t.Errorf("expected merged disabled group, got %+v", diff.FailoverDisabledGroups)
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
		data := make([]map[string]any, 0, len(listing))
		for _, id := range listing {
			data = append(data, map[string]any{"id": id, "owned_by": "test", "object": "model"})
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	defer mockServer.Close()

	// Providers 2 and 3 serve model B unconditionally so its failover group
	// survives provider 1's rename with two members. They are pointed at this
	// mock (not api.example.com) because the scan now runs through
	// discoverAllProviders, which lists every enabled provider.
	mockB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/v1/models" {
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{"id": "rename-model-b", "owned_by": "test", "object": "model"}},
		})
	}))
	defer mockB.Close()

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
			strings.NewReader(fmt.Sprintf(`{"name":"%s","base_url":"%s/v1","api_key":"sk-test"}`, name, mockB.URL)))
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

	// Miss-recording and disabling live on the background sweep, not the
	// interactive handler, so drive the real disabling path directly. Providers
	// 2 and 3 keep serving B, so scanning all three leaves B's group intact
	// while provider 1's rename is reconciled.
	discover := func() *DiscoveryDiff {
		t.Helper()
		results, _, _, _, err := h.discoverAllProviders(ctx, true)
		if err != nil {
			t.Fatalf("discoverAllProviders: %v", err)
		}
		for i := range results {
			if results[i].ProviderName == "rename-scenario-p1" {
				return results[i].Diff
			}
		}
		t.Fatalf("no diff for rename-scenario-p1 in results %+v", results)
		return nil
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

	// Second scan: B is renamed to C. C shows up as added, but B is only a
	// first confirmed miss (pending) — nothing is disabled yet.
	listingMu.Lock()
	listing = []string{"rename-model-a", "rename-model-c"}
	listingMu.Unlock()
	diff := discover()

	if len(diff.Added) != 1 || diff.Added[0].ModelID != "rename-model-c" || diff.Added[0].Reason != changeReasonNewModel {
		t.Errorf("expected added=[rename-model-c/new_model], got %+v", diff.Added)
	}
	if len(diff.Disabled) != 0 {
		t.Errorf("expected no disabled models on the first missing scan, got %+v", diff.Disabled)
	}

	// Third scan: B misses a second consecutive time and is now disabled.
	diff = discover()

	if len(diff.Disabled) != 1 || diff.Disabled[0].ModelID != "rename-model-b" || diff.Disabled[0].Reason != changeReasonNotListed {
		t.Errorf("expected disabled=[rename-model-b/not_listed], got %+v", diff.Disabled)
	}
	if len(diff.Reenabled) != 0 {
		t.Errorf("expected no reenabled models, got %+v", diff.Reenabled)
	}
	foundUpdate := false
	for _, ug := range diff.FailoverUpdatedGroups {
		if ug.DisplayModel == "rename-model-b" {
			foundUpdate = true
			if len(ug.RemovedModelIDs) != 1 || ug.RemovedModelIDs[0] != modelBUUID.String() {
				t.Errorf("expected removed_model_ids=[%s], got %v", modelBUUID, ug.RemovedModelIDs)
			}
		}
	}
	if !foundUpdate {
		t.Errorf("expected failover_updated_groups entry for rename-model-b, got %+v", diff.FailoverUpdatedGroups)
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
// sweepProviderDiff runs the background discovery sweep (the only path that
// records misses and disables models) and returns the diff for the named
// provider. Miss-recording moved off the interactive handlers so their
// confirmation-probe backoff cannot overrun the 60s route timeout, so tests of
// disable/suspect behavior drive this seam directly.
func sweepProviderDiff(t *testing.T, h *Handler, providerName string) *DiscoveryDiff {
	t.Helper()
	results, _, _, _, err := h.discoverAllProviders(context.Background(), true)
	if err != nil {
		t.Fatalf("discoverAllProviders: %v", err)
	}
	for i := range results {
		if results[i].ProviderName == providerName {
			return results[i].Diff
		}
	}
	t.Fatalf("no result for provider %q in %+v", providerName, results)
	return nil
}

// TestDiscoverSweep_DisabledModelSyncErrorTolerated verifies that when the
// background sweep disables a model whose failover-group sync then fails, the
// error is logged and skipped: the scan still completes and the disable is still
// reported in the diff.
func TestDiscoverSweep_DisabledModelSyncErrorTolerated(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	var listingMu sync.Mutex
	listing := []string{"dse-model-a", "dse-model-b"}
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/v1/models" {
			return
		}
		listingMu.Lock()
		defer listingMu.Unlock()
		data := make([]map[string]any, 0, len(listing))
		for _, id := range listing {
			data = append(data, map[string]any{"id": id, "owned_by": "test", "object": "model"})
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"data": data})
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

	_ = created // provider is discovered via the sweep, not by ID

	// First scan imports both models.
	sweepProviderDiff(t, h, "disabled-sync-error-test")

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

	// First missing scan only records a pending miss (nothing disabled yet).
	if diff := sweepProviderDiff(t, h, "disabled-sync-error-test"); len(diff.Disabled) != 0 {
		t.Errorf("first missing scan must not disable, got %+v", diff.Disabled)
	}

	// Second consecutive miss disables dse-model-b and hits the sync error. The
	// error is tolerated: the scan completes and still reports the disable.
	diff := sweepProviderDiff(t, h, "disabled-sync-error-test")
	if len(diff.Disabled) != 1 || diff.Disabled[0].ModelID != "dse-model-b" {
		t.Errorf("expected dse-model-b disabled despite its sync error, got %+v", diff.Disabled)
	}
}

// TestDiscoverSweep_RevalidationErrorIsBestEffort verifies that a custom-group
// revalidation failure during a sweep that disabled a model is logged but does
// NOT abort the scan (unlike a per-model sync error, revalidation is
// best-effort).
func TestDiscoverSweep_RevalidationErrorIsBestEffort(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	var listingMu sync.Mutex
	listing := []string{"rev-model-a", "rev-model-b"}
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/v1/models" {
			return
		}
		listingMu.Lock()
		defer listingMu.Unlock()
		data := make([]map[string]any, 0, len(listing))
		for _, id := range listing {
			data = append(data, map[string]any{"id": id, "owned_by": "test", "object": "model"})
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	defer mockServer.Close()

	providerData := fmt.Sprintf(`{"name":"revalidation-error-test","base_url":"%s/v1","api_key":"sk-test"}`, mockServer.URL)
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

	_ = created // discovered via the sweep, not by ID

	sweepProviderDiff(t, h, "revalidation-error-test")

	// Force revalidation to error; dropping a model makes disabledRefs non-empty
	// so the revalidation branch runs.
	origRevalidate := failoverRepoRevalidateCustomGroups
	defer func() { failoverRepoRevalidateCustomGroups = origRevalidate }()
	failoverRepoRevalidateCustomGroups = func(repo *failover.Repository, ctx context.Context) (*failover.SyncResult, error) {
		return nil, errors.New("revalidation boom")
	}

	listingMu.Lock()
	listing = []string{"rev-model-a"}
	listingMu.Unlock()

	// First missing scan records a pending miss; the second disables rev-model-b,
	// which makes disabledRefs non-empty and triggers the failing revalidation.
	sweepProviderDiff(t, h, "revalidation-error-test")
	diff := sweepProviderDiff(t, h, "revalidation-error-test")
	if len(diff.Disabled) != 1 || diff.Disabled[0].ModelID != "rev-model-b" {
		t.Errorf("expected rev-model-b disabled despite the revalidation error, got %+v", diff.Disabled)
	}
}

// TestDiscoverAllModels_DisabledSyncError verifies that in the sweep a failover
// sync failure for a newly disabled model is logged and skipped without failing
// the scan, and the rest of the diff is still reported.
func TestDiscoverAllModels_DisabledSyncError(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	var listingMu sync.Mutex
	listing := []string{"dase-model-a", "dase-model-b"}
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/v1/models" {
			return
		}
		listingMu.Lock()
		defer listingMu.Unlock()
		data := make([]map[string]any, 0, len(listing))
		for _, id := range listing {
			data = append(data, map[string]any{"id": id, "owned_by": "test", "object": "model"})
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"data": data})
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

	sweepProviderDiff(t, h, "dase-test")

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

	// First missing scan is pending-only; the second consecutive miss disables
	// dase-model-b and exercises the disabled-model sync error path, which the
	// sweep must log and skip while still reporting the disable in the diff.
	sweepProviderDiff(t, h, "dase-test")
	diff := sweepProviderDiff(t, h, "dase-test")
	if diff == nil {
		t.Fatal("expected diff for dase-test")
	}
	if len(diff.Disabled) != 1 || diff.Disabled[0].ModelID != "dase-model-b" {
		t.Errorf("expected disabled=[dase-model-b] despite sync error, got %+v", diff.Disabled)
	}
}

// TestDiscoverSweep_SuspectScanSkipsDisable verifies that when the confirmation
// probe cannot re-list the provider (a flapping upstream), the sweep treats the
// scan as suspect: nothing is disabled, nothing appears in the diff, and the
// dropped model keeps a zero miss streak so it cannot drift toward a disable.
func TestDiscoverSweep_SuspectScanSkipsDisable(t *testing.T) {
	handler, r := newTestHandlerWithRouter(t)
	pool := handler.dbPool.Pool()
	ctx := context.Background()

	var listingMu sync.Mutex
	listing := []string{"sus-model-a", "sus-model-b"}
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/v1/models" {
			return
		}
		listingMu.Lock()
		defer listingMu.Unlock()
		data := make([]map[string]any, 0, len(listing))
		for _, id := range listing {
			data = append(data, map[string]any{"id": id, "owned_by": "test", "object": "model"})
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	defer mockServer.Close()

	providerData := fmt.Sprintf(`{"name":"suspect-scan-test","base_url":"%s/v1","api_key":"sk-test"}`, mockServer.URL)
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

	_ = created.ID // discovered via the sweep, not by ID

	sweepProviderDiff(t, handler, "suspect-scan-test")

	// Drop a model from the listing AND fail the confirmation probe, so the
	// scan cannot get its second opinion and must record nothing.
	origDiscoverForConfirm := discoverModelsForConfirm
	defer func() { discoverModelsForConfirm = origDiscoverForConfirm }()
	discoverModelsForConfirm = func(ctx context.Context, svc *provider.DiscoveryService, prov *provider.Provider, masterKey string) ([]*model.Model, error) {
		return nil, errors.New("probe flake")
	}
	listingMu.Lock()
	listing = []string{"sus-model-a"}
	listingMu.Unlock()

	diff := sweepProviderDiff(t, handler, "suspect-scan-test")
	if len(diff.Disabled) != 0 {
		t.Errorf("expected no disabled models on a suspect scan, got %+v", diff.Disabled)
	}

	// The suspect exit must record nothing: the dropped model stays enabled with
	// a zero miss streak, so the flapping upstream cannot advance it toward a
	// disable.
	var enabled bool
	var streak int
	if err := pool.QueryRow(ctx,
		`SELECT enabled, missing_scans FROM models WHERE provider_id = $1 AND model_id = 'sus-model-b'`,
		created.ID,
	).Scan(&enabled, &streak); err != nil {
		t.Fatalf("lookup sus-model-b: %v", err)
	}
	if !enabled || streak != 0 {
		t.Errorf("expected sus-model-b untouched by suspect scan, got enabled=%v missing_scans=%d", enabled, streak)
	}
}
