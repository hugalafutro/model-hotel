package failover

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/db"
)

// ---------------------------------------------------------------------------
// TestMain — integration test database setup
// ---------------------------------------------------------------------------

var testDB *db.DB

func TestMain(m *testing.M) {
	ctx := context.Background()
	var err error
	testDBURL := os.Getenv("TEST_DATABASE_URL")
	if testDBURL == "" {
		testDBURL = "postgres://llmproxy:changeme@localhost:5433/testdb?sslmode=disable"
	}
	testDB, err = db.New(ctx, testDBURL, 25, 5)
	if err != nil {
		testDB = nil
	}
	code := m.Run()
	if testDB != nil {
		testDB.Close()
	}
	os.Exit(code)
}

// ---------------------------------------------------------------------------
// stripPrefix tests
// ---------------------------------------------------------------------------

func TestStripPrefix_CommonPrefixes(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"zai-org/llama-3", "llama-3"},
		{"deepseek/deepseek-r1", "deepseek-r1"},
		{"meta-llama/llama-3-70b", "llama-3-70b"},
		{"mistralai/mistral-large", "mistral-large"},
		{"openai/gpt-4o", "gpt-4o"},
		{"anthropic/claude-3-opus", "claude-3-opus"},
		{"google/gemini-pro", "gemini-pro"},
		{"allenai/olmo", "olmo"},
		{"bigscience/bloom", "bloom"},
		{"facebook/opt-66b", "opt-66b"},
		{"microsoft/phi-3", "phi-3"},
		{"nvidia/nemotron", "nemotron"},
		{"stabilityai/stablelm", "stablelm"},
		{"tiiuae/falcon-180b", "falcon-180b"},
		{"databricks/dbrx", "dbrx"},
		{"EleutherAI/gpt-j-6b", "gpt-j-6b"},
		{"mosaicml/mpt-30b", "mpt-30b"},
		{"togethercomputer/RedPajama", "RedPajama"},
	}
	for _, tt := range tests {
		got := stripPrefix(tt.input)
		if got != tt.want {
			t.Errorf("stripPrefix(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestStripPrefix_NoPrefix(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"gpt-4o", "gpt-4o"},
		{"claude-3-opus", "claude-3-opus"},
		{"llama-3-70b", "llama-3-70b"},
		{"my-custom-model", "my-custom-model"},
	}
	for _, tt := range tests {
		got := stripPrefix(tt.input)
		if got != tt.want {
			t.Errorf("stripPrefix(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestStripPrefix_PartialPrefixDoesNotMatch(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		// "open" is a partial prefix of "openai/" but should NOT be stripped
		{"open-sesame", "open-sesame"},
		// "deep" is a partial prefix of "deepseek/" but should NOT be stripped
		{"deep-model", "deep-model"},
		// "meta" without the dash should NOT be stripped
		{"meta-model", "meta-model"},
	}
	for _, tt := range tests {
		got := stripPrefix(tt.input)
		if got != tt.want {
			t.Errorf("stripPrefix(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestStripPrefix_EmptyString(t *testing.T) {
	got := stripPrefix("")
	if got != "" {
		t.Errorf("stripPrefix(%q) = %q, want %q", "", got, "")
	}
}

func TestStripPrefix_ExactPrefix(t *testing.T) {
	// If the input is exactly the prefix (no model name after it),
	// stripPrefix should return the empty string after removing the prefix.
	got := stripPrefix("openai/")
	if got != "" {
		t.Errorf("stripPrefix(%q) = %q, want %q", "openai/", got, "")
	}
}

// ---------------------------------------------------------------------------
// FailoverGroup JSON tests
// ---------------------------------------------------------------------------

func TestFailoverGroup_JSONRoundTrip(t *testing.T) {
	id := uuid.New()
	po := []uuid.UUID{uuid.New(), uuid.New()}
	ee := map[string]bool{po[0].String(): true, po[1].String(): false}
	now := time.Date(2024, 6, 15, 10, 30, 0, 0, time.UTC)
	dn := "Test Group"
	fg := FailoverGroup{
		ID:            id,
		DisplayModel:  "gpt-4o",
		DisplayName:   &dn,
		Description:   "A test group",
		PriorityOrder: po,
		EntryEnabled:  ee,
		GroupEnabled:  true,
		AutoCreated:   false,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	data, err := json.Marshal(fg)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	var fg2 FailoverGroup
	if err := json.Unmarshal(data, &fg2); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	if fg2.ID != fg.ID {
		t.Errorf("ID = %v, want %v", fg2.ID, fg.ID)
	}
	if fg2.DisplayModel != fg.DisplayModel {
		t.Errorf("DisplayModel = %q, want %q", fg2.DisplayModel, fg.DisplayModel)
	}
	if fg2.DisplayName == nil || *fg2.DisplayName != dn {
		t.Errorf("DisplayName = %v, want %q", fg2.DisplayName, dn)
	}
	if fg2.Description != fg.Description {
		t.Errorf("Description = %q, want %q", fg2.Description, fg.Description)
	}
	if fg2.GroupEnabled != fg.GroupEnabled {
		t.Errorf("GroupEnabled = %v, want %v", fg2.GroupEnabled, fg.GroupEnabled)
	}
	if fg2.AutoCreated != fg.AutoCreated {
		t.Errorf("AutoCreated = %v, want %v", fg2.AutoCreated, fg.AutoCreated)
	}
	if len(fg2.PriorityOrder) != len(fg.PriorityOrder) {
		t.Fatalf("PriorityOrder length = %d, want %d", len(fg2.PriorityOrder), len(fg.PriorityOrder))
	}
	for i, id := range fg.PriorityOrder {
		if fg2.PriorityOrder[i] != id {
			t.Errorf("PriorityOrder[%d] = %v, want %v", i, fg2.PriorityOrder[i], id)
		}
	}
	for k, v := range fg.EntryEnabled {
		if fg2.EntryEnabled[k] != v {
			t.Errorf("EntryEnabled[%q] = %v, want %v", k, fg2.EntryEnabled[k], v)
		}
	}
}

func TestFailoverGroup_JSONNilDisplayName(t *testing.T) {
	fg := FailoverGroup{
		ID:            uuid.New(),
		DisplayModel:  "claude-3-opus",
		DisplayName:   nil,
		Description:   "",
		PriorityOrder: []uuid.UUID{uuid.New()},
		EntryEnabled:  map[string]bool{},
		GroupEnabled:  true,
		AutoCreated:   true,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}

	data, err := json.Marshal(fg)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	var fg2 FailoverGroup
	if err := json.Unmarshal(data, &fg2); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	if fg2.DisplayName != nil {
		t.Errorf("DisplayName = %v, want nil", fg2.DisplayName)
	}
}

func TestFailoverGroup_JSONEmptySlices(t *testing.T) {
	fg := FailoverGroup{
		ID:            uuid.New(),
		DisplayModel:  "empty-group",
		PriorityOrder: nil,
		EntryEnabled:  nil,
		GroupEnabled:  false,
	}

	data, err := json.Marshal(fg)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	var fg2 FailoverGroup
	if err := json.Unmarshal(data, &fg2); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	if fg2.DisplayModel != "empty-group" {
		t.Errorf("DisplayModel = %q, want %q", fg2.DisplayModel, "empty-group")
	}
	if fg2.GroupEnabled != false {
		t.Errorf("GroupEnabled = %v, want false", fg2.GroupEnabled)
	}
}

// ---------------------------------------------------------------------------
// SyncResult / DisabledGroupInfo JSON tests
// ---------------------------------------------------------------------------

func TestSyncResult_JSONRoundTrip(t *testing.T) {
	sr := SyncResult{
		DisabledGroups: []DisabledGroupInfo{
			{
				DisplayModel:  "gpt-4o",
				Reason:        "only 1 enabled provider (need 2+ for failover)",
				ProviderCount: 1,
				ProviderNames: []string{"openai"},
			},
			{
				DisplayModel:  "llama-3",
				Reason:        "no enabled providers found",
				ProviderCount: 0,
				ProviderNames: []string{},
			},
		},
		SyncErrors: []string{"model-x: some error"},
	}

	data, err := json.Marshal(sr)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	var sr2 SyncResult
	if err := json.Unmarshal(data, &sr2); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	if len(sr2.DisabledGroups) != len(sr.DisabledGroups) {
		t.Fatalf("DisabledGroups length = %d, want %d", len(sr2.DisabledGroups), len(sr.DisabledGroups))
	}
	for i, dg := range sr.DisabledGroups {
		if sr2.DisabledGroups[i].DisplayModel != dg.DisplayModel {
			t.Errorf("DisabledGroups[%d].DisplayModel = %q, want %q", i, sr2.DisabledGroups[i].DisplayModel, dg.DisplayModel)
		}
		if sr2.DisabledGroups[i].Reason != dg.Reason {
			t.Errorf("DisabledGroups[%d].Reason = %q, want %q", i, sr2.DisabledGroups[i].Reason, dg.Reason)
		}
		if sr2.DisabledGroups[i].ProviderCount != dg.ProviderCount {
			t.Errorf("DisabledGroups[%d].ProviderCount = %d, want %d", i, sr2.DisabledGroups[i].ProviderCount, dg.ProviderCount)
		}
	}
	if len(sr2.SyncErrors) != len(sr.SyncErrors) {
		t.Fatalf("SyncErrors length = %d, want %d", len(sr2.SyncErrors), len(sr.SyncErrors))
	}
	if sr2.SyncErrors[0] != sr.SyncErrors[0] {
		t.Errorf("SyncErrors[0] = %q, want %q", sr2.SyncErrors[0], sr.SyncErrors[0])
	}
}

func TestSyncResult_JSONEmpty(t *testing.T) {
	sr := SyncResult{}

	data, err := json.Marshal(sr)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	var sr2 SyncResult
	if err := json.Unmarshal(data, &sr2); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	if len(sr2.DisabledGroups) != 0 {
		t.Errorf("DisabledGroups length = %d, want 0", len(sr2.DisabledGroups))
	}
	// omitempty means SyncErrors should be omitted from JSON when nil
	if sr2.SyncErrors != nil {
		t.Errorf("SyncErrors = %v, want nil", sr2.SyncErrors)
	}
}

func TestDisabledGroupInfo_JSONRoundTrip(t *testing.T) {
	dgi := DisabledGroupInfo{
		DisplayModel:  "claude-3",
		Reason:        "only 1 enabled provider (need 2+ for failover)",
		ProviderCount: 1,
		ProviderNames: []string{"anthropic"},
	}

	data, err := json.Marshal(dgi)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	var dgi2 DisabledGroupInfo
	if err := json.Unmarshal(data, &dgi2); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	if dgi2.DisplayModel != dgi.DisplayModel {
		t.Errorf("DisplayModel = %q, want %q", dgi2.DisplayModel, dgi.DisplayModel)
	}
	if dgi2.Reason != dgi.Reason {
		t.Errorf("Reason = %q, want %q", dgi2.Reason, dgi.Reason)
	}
	if dgi2.ProviderCount != dgi.ProviderCount {
		t.Errorf("ProviderCount = %d, want %d", dgi2.ProviderCount, dgi.ProviderCount)
	}
	if len(dgi2.ProviderNames) != len(dgi.ProviderNames) {
		t.Fatalf("ProviderNames length = %d, want %d", len(dgi2.ProviderNames), len(dgi.ProviderNames))
	}
	if dgi2.ProviderNames[0] != dgi.ProviderNames[0] {
		t.Errorf("ProviderNames[0] = %q, want %q", dgi2.ProviderNames[0], dgi.ProviderNames[0])
	}
}

func TestDisabledGroupInfo_JSONEmptyProviderNames(t *testing.T) {
	dgi := DisabledGroupInfo{
		DisplayModel:  "empty-providers",
		Reason:        "no enabled providers found",
		ProviderCount: 0,
		ProviderNames: []string{},
	}

	data, err := json.Marshal(dgi)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	var dgi2 DisabledGroupInfo
	if err := json.Unmarshal(data, &dgi2); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	if dgi2.ProviderCount != 0 {
		t.Errorf("ProviderCount = %d, want 0", dgi2.ProviderCount)
	}
}

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
	if testDB == nil {
		t.Skip("database not available")
	}
	repo := NewRepository(testDB.Pool())
	if repo == nil {
		t.Error("NewRepository with pool should return non-nil Repository")
	}
}

// ---------------------------------------------------------------------------
// Integration tests — Repository CRUD (requires PostgreSQL)
// ---------------------------------------------------------------------------

func newTestRepo(t *testing.T) *Repository {
	t.Helper()
	if testDB == nil {
		t.Skip("database not available")
	}
	return NewRepository(testDB.Pool())
}

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

	updated, err := repo.Update(ctx, fg.ID, newPO, newEE, &groupEnabled, nil, nil)
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
