package failover

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
)

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
// SyncResult / DeletedGroupInfo JSON tests
// ---------------------------------------------------------------------------

func TestSyncResult_JSONRoundTrip(t *testing.T) {
	sr := SyncResult{
		DeletedGroups: []DeletedGroupInfo{
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
		PurgedEntries: []PrunedEntryInfo{
			{
				GroupDisplayModel: "claude-3",
				PrunedModelIDs:    []string{"uuid-1", "uuid-2"},
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

	if len(sr2.DeletedGroups) != len(sr.DeletedGroups) {
		t.Fatalf("DeletedGroups length = %d, want %d", len(sr2.DeletedGroups), len(sr.DeletedGroups))
	}
	for i, dg := range sr.DeletedGroups {
		if sr2.DeletedGroups[i].DisplayModel != dg.DisplayModel {
			t.Errorf("DeletedGroups[%d].DisplayModel = %q, want %q", i, sr2.DeletedGroups[i].DisplayModel, dg.DisplayModel)
		}
		if sr2.DeletedGroups[i].Reason != dg.Reason {
			t.Errorf("DeletedGroups[%d].Reason = %q, want %q", i, sr2.DeletedGroups[i].Reason, dg.Reason)
		}
		if sr2.DeletedGroups[i].ProviderCount != dg.ProviderCount {
			t.Errorf("DeletedGroups[%d].ProviderCount = %d, want %d", i, sr2.DeletedGroups[i].ProviderCount, dg.ProviderCount)
		}
	}
	if len(sr2.PurgedEntries) != len(sr.PurgedEntries) {
		t.Fatalf("PurgedEntries length = %d, want %d", len(sr2.PurgedEntries), len(sr.PurgedEntries))
	}
	if sr2.PurgedEntries[0].GroupDisplayModel != sr.PurgedEntries[0].GroupDisplayModel {
		t.Errorf("PurgedEntries[0].GroupDisplayModel = %q, want %q", sr2.PurgedEntries[0].GroupDisplayModel, sr.PurgedEntries[0].GroupDisplayModel)
	}
	if len(sr2.PurgedEntries[0].PrunedModelIDs) != len(sr.PurgedEntries[0].PrunedModelIDs) {
		t.Errorf("PurgedEntries[0].PrunedModelIDs length = %d, want %d", len(sr2.PurgedEntries[0].PrunedModelIDs), len(sr.PurgedEntries[0].PrunedModelIDs))
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

	if len(sr2.DeletedGroups) != 0 {
		t.Errorf("DeletedGroups length = %d, want 0", len(sr2.DeletedGroups))
	}
	// omitempty means SyncErrors should be omitted from JSON when nil
	if sr2.SyncErrors != nil {
		t.Errorf("SyncErrors = %v, want nil", sr2.SyncErrors)
	}
}

func TestDeletedGroupInfo_JSONRoundTrip(t *testing.T) {
	dgi := DeletedGroupInfo{
		DisplayModel:  "claude-3",
		Reason:        "only 1 enabled provider (need 2+ for failover)",
		ProviderCount: 1,
		ProviderNames: []string{"anthropic"},
	}

	data, err := json.Marshal(dgi)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	var dgi2 DeletedGroupInfo
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

func TestDeletedGroupInfo_JSONEmptyProviderNames(t *testing.T) {
	dgi := DeletedGroupInfo{
		DisplayModel:  "empty-providers",
		Reason:        "no enabled providers found",
		ProviderCount: 0,
		ProviderNames: []string{},
	}

	data, err := json.Marshal(dgi)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	var dgi2 DeletedGroupInfo
	if err := json.Unmarshal(data, &dgi2); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	if dgi2.ProviderCount != 0 {
		t.Errorf("ProviderCount = %d, want 0", dgi2.ProviderCount)
	}
}
