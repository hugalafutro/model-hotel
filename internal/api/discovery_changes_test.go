package api

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

// truncateDiscoveryChanges clears the table so each test starts clean on the
// shared test database.
func truncateDiscoveryChanges(t *testing.T) {
	t.Helper()
	if _, err := apiTestDB.Pool().Exec(context.Background(), `TRUNCATE discovery_changes`); err != nil {
		t.Fatalf("truncate discovery_changes: %v", err)
	}
}

func TestDiscoveryChangesStore_RoundTrip(t *testing.T) {
	if apiTestDB == nil {
		t.Skip("test database unavailable")
	}
	truncateDiscoveryChanges(t)
	ctx := context.Background()
	pool := apiTestDB.Pool()
	providerID := uuid.New()

	diff := &DiscoveryDiff{
		Added: []ModelChange{{ModelID: "new-model", Reason: changeReasonNewModel}},
		Updated: []ModelUpdate{{
			ModelID: "priced-model",
			Changes: []FieldChange{{Field: changeFieldInputPrice, Old: fptr(1), New: fptr(2)}},
		}},
	}

	wrote, err := AppendDiscoveryChange(ctx, pool, "scheduled", &providerID, "DeepSeek", diff)
	if err != nil {
		t.Fatalf("append: %v", err)
	}
	if !wrote {
		t.Fatal("expected a row to be written for a non-empty diff")
	}

	entries, err := listPendingDiscoveryChanges(ctx, pool)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 pending entry, got %d", len(entries))
	}
	got := entries[0]
	if got.ProviderName != "DeepSeek" || got.Source != "scheduled" {
		t.Errorf("entry metadata = %q/%q, want DeepSeek/scheduled", got.ProviderName, got.Source)
	}
	if got.ProviderID != providerID.String() {
		t.Errorf("ProviderID = %q, want %q", got.ProviderID, providerID.String())
	}
	if got.Diff == nil || len(got.Diff.Added) != 1 || len(got.Diff.Updated) != 1 {
		t.Fatalf("decoded diff mismatch: %+v", got.Diff)
	}
	if countAffected(got.Diff) != 2 {
		t.Errorf("countAffected = %d, want 2", countAffected(got.Diff))
	}

	acked, err := markDiscoveryChangesSeen(ctx, pool)
	if err != nil {
		t.Fatalf("mark seen: %v", err)
	}
	// Ack returns exactly the rows it just cleared, so the client can render the
	// modal from this snapshot without a follow-up read that could race.
	if len(acked) != 1 || acked[0].ProviderName != "DeepSeek" {
		t.Fatalf("expected ack to return the cleared entry, got %+v", acked)
	}
	entries, err = listPendingDiscoveryChanges(ctx, pool)
	if err != nil {
		t.Fatalf("list pending after ack: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected no pending entries after ack, got %d", len(entries))
	}
}

func TestAppendDiscoveryChange_SkipsEmptyDiff(t *testing.T) {
	if apiTestDB == nil {
		t.Skip("test database unavailable")
	}
	truncateDiscoveryChanges(t)
	ctx := context.Background()
	pool := apiTestDB.Pool()

	wrote, err := AppendDiscoveryChange(ctx, pool, "scheduled", nil, "Empty", &DiscoveryDiff{})
	if err != nil {
		t.Fatalf("append empty: %v", err)
	}
	if wrote {
		t.Fatal("expected no row for an empty diff")
	}

	entries, err := listPendingDiscoveryChanges(ctx, pool)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected no entries, got %d", len(entries))
	}
}

func TestAppendDiscoveryChange_NilProviderID(t *testing.T) {
	if apiTestDB == nil {
		t.Skip("test database unavailable")
	}
	truncateDiscoveryChanges(t)
	ctx := context.Background()
	pool := apiTestDB.Pool()

	// The run-wide failover aggregate entry is stored with a nil provider_id
	// and empty provider_name.
	diff := &DiscoveryDiff{
		FailoverDeletedGroups: nil,
		Updated:               nil,
		Added:                 []ModelChange{{ModelID: "x", Reason: changeReasonNewModel}},
	}
	wrote, err := AppendDiscoveryChange(ctx, pool, "startup", nil, "", diff)
	if err != nil {
		t.Fatalf("append nil provider: %v", err)
	}
	if !wrote {
		t.Fatal("expected a row to be written")
	}
	entries, err := listPendingDiscoveryChanges(ctx, pool)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(entries) != 1 || entries[0].ProviderName != "" {
		t.Fatalf("expected 1 entry with empty provider name, got %+v", entries)
	}
	// A nil provider_id round-trips as an empty string, not a zero UUID.
	if entries[0].ProviderID != "" {
		t.Errorf("ProviderID = %q, want empty for a nil provider_id", entries[0].ProviderID)
	}
}
