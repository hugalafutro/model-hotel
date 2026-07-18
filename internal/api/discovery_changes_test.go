package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
		t.Fatal("test database unavailable")
	}
	truncateDiscoveryChanges(t)
	ctx := context.Background()
	pool := apiTestDB.Pool()
	providerID := uuid.New()

	diff := &DiscoveryDiff{
		Added: []ModelChange{{ModelID: "new-model", Reason: changeReasonNewModel}},
		Updated: []ModelUpdate{{
			ModelID: "priced-model",
			Changes: []FieldChange{{Field: changeFieldInputPrice, Old: new(float64(1)), New: new(float64(2))}},
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

// TestDiscoveryChangesHandlers_HTTP exercises the GET /discovery/changes and
// POST /discovery/changes/ack endpoints over the full router: the badge GET
// reports the affected-model count, and ack clears the pending rows while
// returning the just-cleared snapshot for the review modal.
func TestDiscoveryChangesHandlers_HTTP(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)
	pool := h.dbPool.Pool()
	if _, err := pool.Exec(context.Background(), `TRUNCATE discovery_changes`); err != nil {
		t.Fatalf("truncate discovery_changes: %v", err)
	}

	ctx := context.Background()
	providerID := uuid.New()
	diff := &DiscoveryDiff{
		Added: []ModelChange{{ModelID: "fresh-model", Reason: changeReasonNewModel}},
		Updated: []ModelUpdate{{
			ModelID: "repriced",
			Changes: []FieldChange{{Field: changeFieldInputPrice, Old: new(float64(1)), New: new(float64(2))}},
		}},
	}
	if _, err := AppendDiscoveryChange(ctx, pool, "scheduled", &providerID, "DeepSeek", diff); err != nil {
		t.Fatalf("seed discovery change: %v", err)
	}

	doReq := func(method, path string) DiscoveryChangesResponse {
		t.Helper()
		req := httptest.NewRequest(method, path, http.NoBody)
		req.Header.Set("Authorization", "Bearer test-admin-token")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s %s = %d, want 200; body: %s", method, path, rec.Code, rec.Body.String())
		}
		var resp DiscoveryChangesResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode %s response: %v", path, err)
		}
		return resp
	}

	// GET surfaces the pending entry and its affected-model count (2: one added,
	// one updated).
	got := doReq("GET", "/discovery/changes")
	if got.Count != 2 {
		t.Errorf("GET count = %d, want 2", got.Count)
	}
	if len(got.Entries) != 1 || got.Entries[0].ProviderName != "DeepSeek" {
		t.Fatalf("GET entries = %+v, want one DeepSeek entry", got.Entries)
	}

	// Ack clears the badge (Count 0) but echoes the cleared rows for the modal.
	acked := doReq("POST", "/discovery/changes/ack")
	if acked.Count != 0 {
		t.Errorf("ack count = %d, want 0", acked.Count)
	}
	if len(acked.Entries) != 1 {
		t.Fatalf("ack entries = %+v, want the one cleared row", acked.Entries)
	}

	// A second GET now sees nothing pending and reports an empty (non-nil) list.
	after := doReq("GET", "/discovery/changes")
	if after.Count != 0 || len(after.Entries) != 0 {
		t.Errorf("post-ack GET = %+v, want empty", after)
	}
}

func TestAppendDiscoveryChange_SkipsEmptyDiff(t *testing.T) {
	if apiTestDB == nil {
		t.Fatal("test database unavailable")
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
		t.Fatal("test database unavailable")
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
