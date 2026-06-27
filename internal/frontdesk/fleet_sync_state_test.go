package frontdesk

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

// The last-run marker is absent until a sync records one, then round-trips the
// primary it converged onto and a recent timestamp.
func TestFleetSyncStateRoundtrip(t *testing.T) {
	store := newTestStore(t)

	if _, found, err := store.GetFleetSyncState(t.Context()); err != nil || found {
		t.Fatalf("initial GetFleetSyncState = found %v, err %v; want not found, nil", found, err)
	}

	at := time.Now().UTC().Truncate(time.Second)
	if err := store.SetFleetSyncState(t.Context(), "p1", "hotel-1", at); err != nil {
		t.Fatalf("SetFleetSyncState: %v", err)
	}
	got, found, err := store.GetFleetSyncState(t.Context())
	if err != nil || !found {
		t.Fatalf("GetFleetSyncState = found %v, err %v; want found, nil", found, err)
	}
	if got.PrimaryID != "p1" || got.PrimaryName != "hotel-1" || !got.LastRunAt.Equal(at) {
		t.Errorf("state = %+v, want {p1 hotel-1 %v}", got, at)
	}

	// Upsert (single row): a second sync replaces the marker.
	later := at.Add(time.Hour)
	if err := store.SetFleetSyncState(t.Context(), "p2", "hotel-2", later); err != nil {
		t.Fatalf("SetFleetSyncState (upsert): %v", err)
	}
	got, _, _ = store.GetFleetSyncState(t.Context())
	if got.PrimaryID != "p2" || got.PrimaryName != "hotel-2" || !got.LastRunAt.Equal(later) {
		t.Errorf("after upsert state = %+v, want {p2 hotel-2 %v}", got, later)
	}
}

// A failed store read/write surfaces as an error rather than being swallowed.
// Closing the DB is the deterministic, offline way to force the failure.
func TestFleetSyncStateStoreErrors(t *testing.T) {
	store := newTestStore(t)
	if err := store.db.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}
	if _, _, err := store.GetFleetSyncState(t.Context()); err == nil {
		t.Error("GetFleetSyncState on a closed DB should error")
	}
	if err := store.SetFleetSyncState(t.Context(), "p1", "hotel-1", time.Now().UTC()); err == nil {
		t.Error("SetFleetSyncState on a closed DB should error")
	}
}

// GET /api/fleet/last-sync returns 500 (not 204) when the store read fails, so a
// broken store is not mistaken for "never synced".
func TestFleetLastSyncEndpointStoreError(t *testing.T) {
	srv, store := newTestServer(t)
	if err := store.db.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}
	rec := do(t, srv, http.MethodGet, "/api/fleet/last-sync", "", true)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("last-sync with a broken store = %d, want 500", rec.Code)
	}
}

// GET /api/fleet/last-sync is 204 before any run and 200 with the marker after.
func TestFleetLastSyncEndpoint(t *testing.T) {
	srv, store := newTestServer(t)

	if rec := do(t, srv, http.MethodGet, "/api/fleet/last-sync", "", true); rec.Code != http.StatusNoContent {
		t.Fatalf("last-sync before any run = %d, want 204", rec.Code)
	}

	if err := store.SetFleetSyncState(t.Context(), "p1", "hotel-1", time.Now().UTC()); err != nil {
		t.Fatalf("SetFleetSyncState: %v", err)
	}
	rec := do(t, srv, http.MethodGet, "/api/fleet/last-sync", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("last-sync after a run = %d, want 200", rec.Code)
	}
	var got FleetSyncState
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.PrimaryID != "p1" || got.PrimaryName != "hotel-1" {
		t.Errorf("body = %+v, want primary p1/hotel-1", got)
	}
}
