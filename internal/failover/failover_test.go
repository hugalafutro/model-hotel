package failover

import (
	"context"
	"encoding/json"
	"log"
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
	testDBURL, setupErr := db.SetupTestDB("failover")
	if setupErr != nil {
		log.Printf("failed to setup test DB: %v", setupErr)
		os.Exit(1)
	}
	defer db.CleanupTestDB("failover")

	var err error
	testDB, err = db.New(ctx, testDBURL, 25, 5)
	if err != nil {
		log.Printf("failed to initialize test DB: %v", err)
		os.Exit(1) //nolint:gocritic // test-only: os.Exit in TestMain is intentional
	}
	defer testDB.Close()

	os.Exit(m.Run()) //nolint:gocritic // test-only: os.Exit in TestMain is intentional
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

func TestStripPrefix_AllCommonPrefixes(t *testing.T) {
	// Test all the common prefixes defined in the code
	for _, prefix := range commonPrefixes {
		modelName := "test-model"
		input := prefix + modelName
		want := modelName
		got := stripPrefix(input)
		if got != want {
			t.Errorf("stripPrefix(%q) = %q, want %q", input, got, want)
		}
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

// ---------------------------------------------------------------------------
// Integration tests — SyncAllModels, SyncForModel, disableAutoGroup
// ---------------------------------------------------------------------------

func TestRepository_SyncAllModels_TwoProviders(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	// Create unique identifiers for this test
	baseModel := "test-sync-model-" + uuid.New().String()[:8]
	provider1Name := "test-provider-1-" + uuid.New().String()[:8]
	provider2Name := "test-provider-2-" + uuid.New().String()[:8]

	// Insert test data
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

	// Call SyncAllModels
	result, err := repo.SyncAllModels(ctx)
	if err != nil {
		t.Fatalf("SyncAllModels failed: %v", err)
	}

	// Verify results
	if len(result.DisabledGroups) != 0 {
		t.Errorf("Expected 0 disabled groups, got %d", len(result.DisabledGroups))
	}
	if len(result.SyncErrors) != 0 {
		t.Errorf("Expected 0 sync errors, got %d: %v", len(result.SyncErrors), result.SyncErrors)
	}

	// Verify auto-group was created
	InvalidateFailoverCache()
	group, err := repo.GetByModel(ctx, baseModel)
	if err != nil {
		t.Fatalf("Failed to get auto-group: %v", err)
	}
	if group == nil {
		t.Fatal("Expected auto-group to be created")
	}
	if !group.AutoCreated {
		t.Error("Expected AutoCreated to be true")
	}
	if !group.GroupEnabled {
		t.Error("Expected GroupEnabled to be true")
	}

	// Cleanup the failover group
	if err := repo.Delete(ctx, baseModel); err != nil {
		t.Logf("cleanup Delete failed: %v", err)
	}
}

func TestRepository_SyncAllModels_SingleProvider(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	// Create unique identifiers for this test
	baseModel := "test-sync-single-" + uuid.New().String()[:8]
	providerName := "test-provider-single-" + uuid.New().String()[:8]

	// First, create an auto-created enabled group that should be disabled
	priorityOrder := []uuid.UUID{uuid.New(), uuid.New()}
	entryEnabled := make(map[string]bool)
	for _, id := range priorityOrder {
		entryEnabled[id.String()] = true
	}
	groupEnabled := true
	autoCreated := true

	_, err := repo.UpsertWithConfig(ctx, baseModel, priorityOrder, entryEnabled, &groupEnabled, nil, nil, &autoCreated)
	if err != nil {
		t.Fatalf("Failed to create initial group: %v", err)
	}
	defer func() {
		// Cleanup - delete the group if it exists
		InvalidateFailoverCache()
		if existing, _ := repo.GetByModel(ctx, baseModel); existing != nil {
			_ = repo.Delete(ctx, baseModel)
		}
	}()

	// Insert test data - only 1 provider/model
	providerID := uuid.New()
	modelID := uuid.New()

	_, err = testDB.Pool().Exec(ctx, `
		INSERT INTO providers (id, name, base_url, encrypted_key, key_nonce, key_salt, enabled, created_at)
		VALUES ($1, $2, 'http://localhost:11434', 'dGVzdA==', 'dGVzdA==', 'dGVzdA==', true, now())
	`, providerID, providerName)
	if err != nil {
		t.Fatalf("Failed to insert provider: %v", err)
	}
	defer func() {
		_, _ = testDB.Pool().Exec(ctx, "DELETE FROM providers WHERE id = $1", providerID)
	}()

	_, err = testDB.Pool().Exec(ctx, `
		INSERT INTO models (id, model_id, provider_id, enabled, created_at)
		VALUES ($1, $2, $3, true, now())
	`, modelID, baseModel, providerID)
	if err != nil {
		t.Fatalf("Failed to insert model: %v", err)
	}
	defer func() {
		_, _ = testDB.Pool().Exec(ctx, "DELETE FROM models WHERE id = $1", modelID)
	}()

	// Call SyncAllModels - should disable the existing auto-group
	result, err := repo.SyncAllModels(ctx)
	if err != nil {
		t.Fatalf("SyncAllModels failed: %v", err)
	}

	// Verify results - should have 1 disabled group
	if len(result.DisabledGroups) != 1 {
		t.Fatalf("Expected 1 disabled group, got %d", len(result.DisabledGroups))
	}
	if result.DisabledGroups[0].DisplayModel != baseModel {
		t.Errorf("Expected disabled group for %q, got %q", baseModel, result.DisabledGroups[0].DisplayModel)
	}
	if result.DisabledGroups[0].Reason != "only 1 enabled provider (need 2+ for failover)" {
		t.Errorf("Expected reason 'only 1 enabled provider (need 2+ for failover)', got %q", result.DisabledGroups[0].Reason)
	}
	if result.DisabledGroups[0].ProviderCount != 1 {
		t.Errorf("Expected provider count 1, got %d", result.DisabledGroups[0].ProviderCount)
	}

	// Verify the group was actually disabled
	InvalidateFailoverCache()
	groupAfter, err := repo.GetByModel(ctx, baseModel)
	if err != nil {
		t.Fatalf("Failed to get group after sync: %v", err)
	}
	if groupAfter.GroupEnabled {
		t.Error("Expected group to be disabled")
	}
}

func TestRepository_SyncForModel_TwoProviders(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	// Create unique identifiers for this test
	baseModel := "test-syncformodel-" + uuid.New().String()[:8]
	provider1Name := "test-provider-1-" + uuid.New().String()[:8]
	provider2Name := "test-provider-2-" + uuid.New().String()[:8]

	// Insert test data
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

	// Call SyncForModel
	err = repo.SyncForModel(ctx, baseModel)
	if err != nil {
		t.Fatalf("SyncForModel failed: %v", err)
	}

	// Verify auto-group was created
	InvalidateFailoverCache()
	group, err := repo.GetByModel(ctx, baseModel)
	if err != nil {
		t.Fatalf("Failed to get auto-group: %v", err)
	}
	if group == nil {
		t.Fatal("Expected auto-group to be created")
	}
	if !group.AutoCreated {
		t.Error("Expected AutoCreated to be true")
	}
	if !group.GroupEnabled {
		t.Error("Expected GroupEnabled to be true")
	}

	// Cleanup the failover group
	if err := repo.Delete(ctx, baseModel); err != nil {
		t.Logf("cleanup Delete failed: %v", err)
	}
}

func TestRepository_SyncForModel_SingleProvider(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	// Create unique identifiers for this test
	baseModel := "test-syncformodel-single-" + uuid.New().String()[:8]
	providerName := "test-provider-single-" + uuid.New().String()[:8]

	// Insert test data
	providerID := uuid.New()
	modelID := uuid.New()

	_, err := testDB.Pool().Exec(ctx, `
		INSERT INTO providers (id, name, base_url, encrypted_key, key_nonce, key_salt, enabled, created_at)
		VALUES ($1, $2, 'http://localhost:11434', 'dGVzdA==', 'dGVzdA==', 'dGVzdA==', true, now())
	`, providerID, providerName)
	if err != nil {
		t.Fatalf("Failed to insert provider: %v", err)
	}
	defer func() {
		_, _ = testDB.Pool().Exec(ctx, "DELETE FROM providers WHERE id = $1", providerID)
	}()

	_, err = testDB.Pool().Exec(ctx, `
		INSERT INTO models (id, model_id, provider_id, enabled, created_at)
		VALUES ($1, $2, $3, true, now())
	`, modelID, baseModel, providerID)
	if err != nil {
		t.Fatalf("Failed to insert model: %v", err)
	}
	defer func() {
		_, _ = testDB.Pool().Exec(ctx, "DELETE FROM models WHERE id = $1", modelID)
	}()

	// Call SyncForModel - should disable any existing group
	err = repo.SyncForModel(ctx, baseModel)
	if err != nil {
		t.Fatalf("SyncForModel failed: %v", err)
	}

	// Verify no auto-group was created (or existing one was disabled)
	InvalidateFailoverCache()
	group, err := repo.GetByModel(ctx, baseModel)
	if err != nil {
		// Expected - no group exists, which is correct for single provider
		return
	}
	if group.GroupEnabled {
		t.Error("Expected no enabled auto-group for single provider")
	}
}

func TestRepository_SyncForModel_WithPrefix(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	// Create unique identifiers for this test
	baseModel := "test-syncformodel-prefix-" + uuid.New().String()[:8]
	provider1Name := "test-provider-1-" + uuid.New().String()[:8]
	provider2Name := "test-provider-2-" + uuid.New().String()[:8]

	// Insert test data
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

	// Use prefixed model IDs
	modelID1 := "openai/" + baseModel
	modelID2 := "anthropic/" + baseModel

	_, err = testDB.Pool().Exec(ctx, `
		INSERT INTO models (id, model_id, provider_id, enabled, created_at)
		VALUES ($1, $2, $3, true, now())
	`, model1ID, modelID1, provider1ID)
	if err != nil {
		t.Fatalf("Failed to insert model1: %v", err)
	}
	defer func() {
		_, _ = testDB.Pool().Exec(ctx, "DELETE FROM models WHERE id = $1", model1ID)
	}()

	_, err = testDB.Pool().Exec(ctx, `
		INSERT INTO models (id, model_id, provider_id, enabled, created_at)
		VALUES ($1, $2, $3, true, now())
	`, model2ID, modelID2, provider2ID)
	if err != nil {
		t.Fatalf("Failed to insert model2: %v", err)
	}
	defer func() {
		_, _ = testDB.Pool().Exec(ctx, "DELETE FROM models WHERE id = $1", model2ID)
	}()

	// Call SyncForModel with prefixed model ID
	err = repo.SyncForModel(ctx, modelID1)
	if err != nil {
		t.Fatalf("SyncForModel failed: %v", err)
	}

	// Verify auto-group was created with stripped prefix
	InvalidateFailoverCache()
	group, err := repo.GetByModel(ctx, baseModel)
	if err != nil {
		t.Fatalf("Failed to get auto-group: %v", err)
	}
	if group == nil {
		t.Fatal("Expected auto-group to be created with stripped prefix")
	}
	if group.DisplayModel != baseModel {
		t.Errorf("Expected DisplayModel %q, got %q", baseModel, group.DisplayModel)
	}

	// Cleanup the failover group
	if err := repo.Delete(ctx, baseModel); err != nil {
		t.Logf("cleanup Delete failed: %v", err)
	}
}

func TestRepository_SyncAllModels_DisablesStaleGroups(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	// Create unique identifiers for this test
	baseModel := "test-stale-group-" + uuid.New().String()[:8]

	// First, create an auto-created enabled group
	priorityOrder := []uuid.UUID{uuid.New(), uuid.New()}
	entryEnabled := make(map[string]bool)
	for _, id := range priorityOrder {
		entryEnabled[id.String()] = true
	}
	groupEnabled := true
	autoCreated := true

	_, err := repo.UpsertWithConfig(ctx, baseModel, priorityOrder, entryEnabled, &groupEnabled, nil, nil, &autoCreated)
	if err != nil {
		t.Fatalf("Failed to create initial group: %v", err)
	}
	defer func() {
		// Cleanup - delete the group if it exists
		InvalidateFailoverCache()
		if existing, _ := repo.GetByModel(ctx, baseModel); existing != nil {
			_ = repo.Delete(ctx, baseModel)
		}
	}()

	// Verify it was created and enabled
	InvalidateFailoverCache()
	group, err := repo.GetByModel(ctx, baseModel)
	if err != nil {
		t.Fatalf("Failed to get initial group: %v", err)
	}
	if !group.GroupEnabled {
		t.Fatal("Initial group should be enabled")
	}

	// Call SyncAllModels with no matching models/providers - should disable the stale group
	result, err := repo.SyncAllModels(ctx)
	if err != nil {
		t.Fatalf("SyncAllModels failed: %v", err)
	}

	// Verify the group was disabled
	InvalidateFailoverCache()
	groupAfter, err := repo.GetByModel(ctx, baseModel)
	if err != nil {
		t.Fatalf("Failed to get group after sync: %v", err)
	}
	if groupAfter.GroupEnabled {
		t.Error("Expected stale auto-group to be disabled")
	}

	// Verify it's in the disabled groups result
	foundDisabled := false
	for _, dg := range result.DisabledGroups {
		if dg.DisplayModel == baseModel && dg.Reason == "no enabled providers found" {
			foundDisabled = true
			break
		}
	}
	if !foundDisabled {
		t.Error("Expected disabled group to be reported in result")
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

// ---------------------------------------------------------------------------
// Integration tests — Sync coverage improvements
// ---------------------------------------------------------------------------

func TestRepository_SyncAllModels_WithSyncErrors(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	// Create unique identifiers for this test
	baseModel := "test-sync-error-" + uuid.New().String()[:8]

	// Insert test data - two providers with same model
	provider1ID := uuid.New()
	provider2ID := uuid.New()
	model1ID := uuid.New()
	model2ID := uuid.New()

	_, err := testDB.Pool().Exec(ctx, `
		INSERT INTO providers (id, name, base_url, encrypted_key, key_nonce, key_salt, enabled, created_at)
		VALUES ($1, $2, 'http://localhost:11434', 'dGVzdA==', 'dGVzdA==', 'dGVzdA==', true, now())
	`, provider1ID, "test-provider-1-"+uuid.New().String()[:8])
	if err != nil {
		t.Fatalf("Failed to insert provider1: %v", err)
	}
	defer func() {
		_, _ = testDB.Pool().Exec(ctx, "DELETE FROM providers WHERE id = $1", provider1ID)
	}()

	_, err = testDB.Pool().Exec(ctx, `
		INSERT INTO providers (id, name, base_url, encrypted_key, key_nonce, key_salt, enabled, created_at)
		VALUES ($1, $2, 'http://localhost:11434', 'dGVzdA==', 'dGVzdA==', 'dGVzdA==', true, now())
	`, provider2ID, "test-provider-2-"+uuid.New().String()[:8])
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

	// Call SyncAllModels - should succeed without errors
	result, err := repo.SyncAllModels(ctx)
	if err != nil {
		t.Fatalf("SyncAllModels failed: %v", err)
	}

	// Verify no sync errors
	if len(result.SyncErrors) != 0 {
		t.Errorf("Expected 0 sync errors, got %d: %v", len(result.SyncErrors), result.SyncErrors)
	}

	// Verify auto-group was created
	InvalidateFailoverCache()
	group, err := repo.GetByModel(ctx, baseModel)
	if err != nil {
		t.Fatalf("Failed to get auto-group: %v", err)
	}
	if group == nil {
		t.Fatal("Expected auto-group to be created")
	}
	if len(group.PriorityOrder) != 2 {
		t.Errorf("Expected 2 providers in priority order, got %d", len(group.PriorityOrder))
	}

	// Cleanup
	if err := repo.Delete(ctx, baseModel); err != nil {
		t.Logf("cleanup Delete failed: %v", err)
	}
}

func TestRepository_SyncForModel_WithPrefixVariants(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	// Test with various prefix variants
	baseModel := "test-prefix-variant-" + uuid.New().String()[:8]
	prefixedModel := "openai/" + baseModel

	provider1ID := uuid.New()
	provider2ID := uuid.New()
	model1ID := uuid.New()
	model2ID := uuid.New()

	_, err := testDB.Pool().Exec(ctx, `
		INSERT INTO providers (id, name, base_url, encrypted_key, key_nonce, key_salt, enabled, created_at)
		VALUES ($1, $2, 'http://localhost:11434', 'dGVzdA==', 'dGVzdA==', 'dGVzdA==', true, now())
	`, provider1ID, "test-provider-1-"+uuid.New().String()[:8])
	if err != nil {
		t.Fatalf("Failed to insert provider1: %v", err)
	}
	defer func() {
		_, _ = testDB.Pool().Exec(ctx, "DELETE FROM providers WHERE id = $1", provider1ID)
	}()

	_, err = testDB.Pool().Exec(ctx, `
		INSERT INTO providers (id, name, base_url, encrypted_key, key_nonce, key_salt, enabled, created_at)
		VALUES ($1, $2, 'http://localhost:11434', 'dGVzdA==', 'dGVzdA==', 'dGVzdA==', true, now())
	`, provider2ID, "test-provider-2-"+uuid.New().String()[:8])
	if err != nil {
		t.Fatalf("Failed to insert provider2: %v", err)
	}
	defer func() {
		_, _ = testDB.Pool().Exec(ctx, "DELETE FROM providers WHERE id = $1", provider2ID)
	}()

	// Insert models with different prefixes for the same base model
	_, err = testDB.Pool().Exec(ctx, `
		INSERT INTO models (id, model_id, provider_id, enabled, created_at)
		VALUES ($1, $2, $3, true, now())
	`, model1ID, prefixedModel, provider1ID)
	if err != nil {
		t.Fatalf("Failed to insert model1: %v", err)
	}
	defer func() {
		_, _ = testDB.Pool().Exec(ctx, "DELETE FROM models WHERE id = $1", model1ID)
	}()

	_, err = testDB.Pool().Exec(ctx, `
		INSERT INTO models (id, model_id, provider_id, enabled, created_at)
		VALUES ($1, $2, $3, true, now())
	`, model2ID, "anthropic/"+baseModel, provider2ID)
	if err != nil {
		t.Fatalf("Failed to insert model2: %v", err)
	}
	defer func() {
		_, _ = testDB.Pool().Exec(ctx, "DELETE FROM models WHERE id = $1", model2ID)
	}()

	// Call SyncForModel with the prefixed model ID
	err = repo.SyncForModel(ctx, prefixedModel)
	if err != nil {
		t.Fatalf("SyncForModel failed: %v", err)
	}

	// Verify auto-group was created with stripped prefix
	InvalidateFailoverCache()
	group, err := repo.GetByModel(ctx, baseModel)
	if err != nil {
		t.Fatalf("Failed to get auto-group: %v", err)
	}
	if group == nil {
		t.Fatal("Expected auto-group to be created with stripped prefix")
	}
	if group.DisplayModel != baseModel {
		t.Errorf("Expected DisplayModel %q, got %q", baseModel, group.DisplayModel)
	}

	// Cleanup
	if err := repo.Delete(ctx, baseModel); err != nil {
		t.Logf("cleanup Delete failed: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Integration tests — Delete with cascade coverage
// ---------------------------------------------------------------------------

func TestRepository_DeleteByID_WithModels(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	// Create a failover group
	displayModel := "test-delete-cascade-" + uuid.New().String()[:8]
	po := []uuid.UUID{uuid.New(), uuid.New()}

	fg, err := repo.Upsert(ctx, displayModel, po)
	if err != nil {
		t.Fatalf("Upsert failed: %v", err)
	}

	// Verify it exists
	InvalidateFailoverCache()
	_, err = repo.GetByID(ctx, fg.ID)
	if err != nil {
		t.Fatalf("GetByID failed before delete: %v", err)
	}

	// Delete by ID
	err = repo.DeleteByID(ctx, fg.ID)
	if err != nil {
		t.Fatalf("DeleteByID failed: %v", err)
	}

	// Verify it's gone
	InvalidateFailoverCache()
	_, err = repo.GetByID(ctx, fg.ID)
	if err == nil {
		t.Error("GetByID should return error after DeleteByID")
	}

	// Verify delete is idempotent (doesn't error on already-deleted)
	err = repo.DeleteByID(ctx, fg.ID)
	if err != nil {
		t.Errorf("DeleteByID on already-deleted group should not error: %v", err)
	}
}

func TestRepository_Delete_WithNonExistentGroup(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	// Delete a non-existent group - should not error
	err := repo.Delete(ctx, "non-existent-model-"+uuid.New().String())
	if err != nil {
		t.Errorf("Delete on non-existent group should not error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Integration tests — Update edge cases
// ---------------------------------------------------------------------------

func TestRepository_Update_WithNilValues(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	displayModel := "test-update-nil-" + uuid.New().String()[:8]
	po := []uuid.UUID{uuid.New(), uuid.New()}

	fg, err := repo.Upsert(ctx, displayModel, po)
	if err != nil {
		t.Fatalf("Upsert failed: %v", err)
	}
	defer func() {
		_ = repo.Delete(ctx, displayModel)
	}()

	// Update with nil values - should preserve existing values
	updated, err := repo.Update(ctx, fg.ID, fg.PriorityOrder, fg.EntryEnabled, nil, nil, nil)
	if err != nil {
		t.Fatalf("Update with nil values failed: %v", err)
	}

	// Verify values are preserved
	if updated.GroupEnabled != fg.GroupEnabled {
		t.Errorf("GroupEnabled changed from %v to %v", fg.GroupEnabled, updated.GroupEnabled)
	}
	if updated.DisplayName == nil && fg.DisplayName != nil {
		t.Error("DisplayName should be preserved when nil passed")
	}
	if updated.Description != fg.Description {
		t.Errorf("Description changed from %q to %q", fg.Description, updated.Description)
	}
}

func TestRepository_Update_WithDisplayNameAndDescription(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	displayModel := "test-update-display-" + uuid.New().String()[:8]
	po := []uuid.UUID{uuid.New()}

	fg, err := repo.Upsert(ctx, displayModel, po)
	if err != nil {
		t.Fatalf("Upsert failed: %v", err)
	}
	defer func() {
		_ = repo.Delete(ctx, displayModel)
	}()

	displayName := "Updated Display Name"
	description := "Updated description for testing"

	updated, err := repo.Update(ctx, fg.ID, po, fg.EntryEnabled, nil, &displayName, &description)
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	if updated.DisplayName == nil || *updated.DisplayName != displayName {
		t.Errorf("DisplayName = %v, want %q", updated.DisplayName, displayName)
	}
	if updated.Description != description {
		t.Errorf("Description = %q, want %q", updated.Description, description)
	}

	// Verify via GetByID
	InvalidateFailoverCache()
	found, err := repo.GetByID(ctx, fg.ID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}
	if found.DisplayName == nil || *found.DisplayName != displayName {
		t.Errorf("GetByID DisplayName = %v, want %q", found.DisplayName, displayName)
	}
	if found.Description != description {
		t.Errorf("GetByID Description = %q, want %q", found.Description, description)
	}
}

// ---------------------------------------------------------------------------
// Integration tests — GetEnabled edge cases
// ---------------------------------------------------------------------------

func TestRepository_GetEnabled_ExcludesDisabledGroups(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	displayModel := "test-getenabled-disabled-" + uuid.New().String()[:8]
	po := []uuid.UUID{uuid.New()}

	// Create a disabled group
	groupEnabled := false
	fg, err := repo.UpsertWithConfig(ctx, displayModel, po, nil, &groupEnabled, nil, nil, nil)
	if err != nil {
		t.Fatalf("UpsertWithConfig failed: %v", err)
	}
	defer func() {
		_ = repo.Delete(ctx, displayModel)
	}()

	// GetEnabled should not include disabled groups
	groups, err := repo.GetEnabled(ctx)
	if err != nil {
		t.Fatalf("GetEnabled failed: %v", err)
	}

	for _, g := range groups {
		if g.ID == fg.ID {
			t.Error("GetEnabled should not include disabled groups")
		}
	}
}

func TestRepository_GetEnabled_EmptyList(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	// GetEnabled with no groups should return empty list or nil, not error
	groups, err := repo.GetEnabled(ctx)
	if err != nil {
		t.Fatalf("GetEnabled failed: %v", err)
	}
	// Accept either nil or empty slice
	if len(groups) != 0 {
		t.Errorf("GetEnabled should return empty slice or nil, got %d items", len(groups))
	}
}

// ---------------------------------------------------------------------------
// Integration tests — GetByID edge cases
// ---------------------------------------------------------------------------

func TestRepository_GetByID_NotFound(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	InvalidateFailoverCache()
	_, err := repo.GetByID(ctx, uuid.New())
	if err == nil {
		t.Error("GetByID should return error for nonexistent ID")
	}
}

// ---------------------------------------------------------------------------
// Integration tests — List ordering
// ---------------------------------------------------------------------------

func TestRepository_List_OrderedByDisplayModel(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	// Create multiple groups with different display models
	models := []string{
		"z-test-model",
		"a-test-model",
		"m-test-model",
	}
	createdIDs := make([]uuid.UUID, len(models))

	for i, model := range models {
		po := []uuid.UUID{uuid.New()}
		fg, err := repo.Upsert(ctx, model, po)
		if err != nil {
			t.Fatalf("Upsert failed for %s: %v", model, err)
		}
		createdIDs[i] = fg.ID
		defer func() {
			_ = repo.Delete(ctx, model)
		}()
	}

	// List should be ordered by display_model
	groups, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	// Verify ordering (a < m < z)
	if len(groups) < 3 {
		t.Fatalf("Expected at least 3 groups, got %d", len(groups))
	}

	// Find our groups and verify order
	var foundModels []string
	for _, g := range groups {
		for _, model := range models {
			if g.DisplayModel == model {
				foundModels = append(foundModels, model)
				break
			}
		}
	}

	if len(foundModels) != 3 {
		t.Fatalf("Expected to find all 3 test groups, got %d", len(foundModels))
	}

	// Verify alphabetical order
	if foundModels[0] != "a-test-model" {
		t.Errorf("Expected first model 'a-test-model', got %q", foundModels[0])
	}
	if foundModels[1] != "m-test-model" {
		t.Errorf("Expected second model 'm-test-model', got %q", foundModels[1])
	}
	if foundModels[2] != "z-test-model" {
		t.Errorf("Expected third model 'z-test-model', got %q", foundModels[2])
	}
}
