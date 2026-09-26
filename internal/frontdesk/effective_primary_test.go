package frontdesk

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync/atomic"
	"testing"
	"time"
)

// TestEffectivePrimaryID pins the resolver every primary consumer shares.
func TestEffectivePrimaryID(t *testing.T) {
	a, b := &Member{ID: "a"}, &Member{ID: "b"}
	two := []*Member{a, b}
	cases := []struct {
		name    string
		members []*Member
		cfg     AutoSyncConfig
		marker  string
		want    string
	}{
		{"empty roster", nil, AutoSyncConfig{}, "", ""},
		{"lone member with nothing stored", []*Member{a}, AutoSyncConfig{}, "", "a"},
		{"lone member beats a dangling designation", []*Member{a}, AutoSyncConfig{Enabled: true, PrimaryID: "gone"}, "gone", "a"},
		{"nothing on a larger roster", two, AutoSyncConfig{}, "", ""},
		{"enabled designation beats the marker", two, AutoSyncConfig{Enabled: true, PrimaryID: "b"}, "a", "b"},
		{"marker beats a dormant designation", two, AutoSyncConfig{PrimaryID: "b"}, "a", "a"},
		{"dormant designation beats nothing", two, AutoSyncConfig{PrimaryID: "b"}, "", "b"},
		{"dangling designation falls back to the marker", two, AutoSyncConfig{Enabled: true, PrimaryID: "gone"}, "a", "a"},
		{"dangling marker names nobody", two, AutoSyncConfig{}, "gone", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := effectivePrimaryID(tc.members, tc.cfg, tc.marker); got != tc.want {
				t.Errorf("effectivePrimaryID = %q, want %q", got, tc.want)
			}
		})
	}
}

// quotaMemberStub answers the quota snapshot export with an empty list and
// counts the reads, so a test can tell whether Front Desk asked it.
func quotaMemberStub(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"snapshots":[]}`))
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

// A one-member fleet has no designation and cannot get one, but its member is
// the primary: the quota proxy asks it rather than answering "no primary".
func TestHandleQuota_LoneMemberIsAsked(t *testing.T) {
	srv, store := newTestServer(t)
	member, hits := quotaMemberStub(t)
	if _, err := store.CreateMember(t.Context(), "solo", member.URL, "tok"); err != nil {
		t.Fatalf("CreateMember: %v", err)
	}
	rr := httptest.NewRecorder()
	srv.handleQuota(rr, httptest.NewRequest(http.MethodGet, "/api/quota", http.NoBody))
	if rr.Code != http.StatusOK || hits.Load() != 1 {
		t.Fatalf("handleQuota = %d with %d primary reads, want 200 after 1 read (%s)", rr.Code, hits.Load(), rr.Body.String())
	}
}

// Distribution needs a destination: a lone primary is not read at all, while a
// fleet whose primary is named only by the marker is fed from that member.
func TestDistributeQuotaOnce_UsesTheEffectivePrimary(t *testing.T) {
	srv, store := newTestServer(t)
	ctx := t.Context()
	primary, primaryHits := quotaMemberStub(t)
	other, otherHits := quotaMemberStub(t)
	pm, err := store.CreateMember(ctx, "primary", primary.URL, "tok")
	if err != nil {
		t.Fatalf("CreateMember: %v", err)
	}

	srv.DistributeQuotaOnce(ctx)
	if primaryHits.Load() != 0 {
		t.Fatalf("lone primary read %d times, want 0: there is nobody to distribute to", primaryHits.Load())
	}

	if _, err := store.CreateMember(ctx, "other", other.URL, "tok"); err != nil {
		t.Fatalf("CreateMember: %v", err)
	}
	if err := store.SetFleetPrimaryMarker(ctx, pm.ID, pm.Name); err != nil {
		t.Fatalf("SetFleetPrimaryMarker: %v", err)
	}
	srv.DistributeQuotaOnce(ctx)
	if primaryHits.Load() != 1 || otherHits.Load() != 1 {
		t.Errorf("marker primary reads=%d, other pushes=%d, want 1 and 1", primaryHits.Load(), otherHits.Load())
	}
}

// The fleet state judges holds against the effective primary's build and never
// counts that primary as a sync target. Two members held against the build of a
// primary named only by the marker are the whole syncable fleet disagreeing.
func TestFleetState_JudgesHoldsAgainstTheEffectivePrimary(t *testing.T) {
	srv, store := newTestServer(t)
	ctx := t.Context()
	var ids []string
	for _, name := range []string{"primary", "b", "c"} {
		m, err := store.CreateMember(ctx, name, "http://127.0.0.1:9/"+name, "tok")
		if err != nil {
			t.Fatalf("CreateMember: %v", err)
		}
		ids = append(ids, m.ID)
	}
	if err := store.SetFleetPrimaryMarker(ctx, ids[0], "primary"); err != nil {
		t.Fatalf("SetFleetPrimaryMarker: %v", err)
	}
	primaryBuild := memberBuild{Version: "1.0.0", Commit: "abc"}
	srv.poller.mu.Lock()
	srv.poller.statuses[ids[0]] = MemberStatus{Health: HealthStatus{Known: true, Healthy: true}, Version: primaryBuild.Version, Commit: primaryBuild.Commit}
	srv.poller.mu.Unlock()
	srv.syncHeldMu.Lock()
	srv.syncHeld = map[string]string{ids[1]: primaryBuild.key(), ids[2]: primaryBuild.key()}
	srv.syncHeldMu.Unlock()

	_, reasons, _, err := srv.fleetStateNow(ctx)
	if err != nil {
		t.Fatalf("fleetStateNow: %v", err)
	}
	if !slices.Contains(reasons, reasonAllSyncHeld) {
		t.Errorf("reasons = %v, want %s", reasons, reasonAllSyncHeld)
	}
}

// The auto-sync status carries the effective primary for the Members page next
// to the raw designation, which stays empty on a one-member fleet.
func TestAutoSyncStatus_CarriesTheEffectivePrimary(t *testing.T) {
	srv, store := newTestServer(t)
	m, err := store.CreateMember(t.Context(), "solo", "http://127.0.0.1:9", "tok")
	if err != nil {
		t.Fatalf("CreateMember: %v", err)
	}
	rec := do(t, srv, http.MethodGet, "/api/fleet/autosync", "", true)
	var body struct {
		PrimaryID          string `json:"primary_id"`
		EffectivePrimaryID string `json:"effective_primary_id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode %d: %v (%s)", rec.Code, err, rec.Body.String())
	}
	if body.PrimaryID != "" || body.EffectivePrimaryID != m.ID {
		t.Errorf("primary_id=%q effective_primary_id=%q, want \"\" and %q", body.PrimaryID, body.EffectivePrimaryID, m.ID)
	}
}

// Every read effectivePrimary depends on fails loudly rather than resolving
// "no primary": the quota proxy answers 500 and an add to a one-member fleet is
// refused, since it could not record the lone member as the marker.
func TestEffectivePrimary_SyncStateReadFailure(t *testing.T) {
	srv, store := newTestServer(t)
	ctx := t.Context()
	if _, err := store.CreateMember(ctx, "solo", "http://127.0.0.1:9", "tok"); err != nil {
		t.Fatalf("CreateMember: %v", err)
	}
	if _, err := store.db.ExecContext(ctx, `DROP TABLE fleet_sync_state`); err != nil {
		t.Fatalf("drop: %v", err)
	}
	rr := httptest.NewRecorder()
	srv.handleQuota(rr, httptest.NewRequest(http.MethodGet, "/api/quota", http.NoBody))
	if rr.Code != http.StatusInternalServerError {
		t.Errorf("handleQuota = %d, want 500", rr.Code)
	}
	host := systemMemberServer(t, false)
	rec := do(t, srv, http.MethodPost, "/api/members", `{"name":"second","url":"`+host.URL+`","token":"tok"}`, true)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("add = %d, want 500", rec.Code)
	}
	if members, _ := store.ListMembers(ctx); len(members) != 1 {
		t.Errorf("members = %d after the refused add, want 1", len(members))
	}
}

// A marker write that fails refuses the add rather than growing the fleet
// with nobody named primary.
func TestCreateMember_LonePrimaryMarkerWriteFailure(t *testing.T) {
	srv, store := newTestServer(t)
	ctx := t.Context()
	if _, err := store.CreateMember(ctx, "solo", "http://127.0.0.1:9", "tok"); err != nil {
		t.Fatalf("CreateMember: %v", err)
	}
	if _, err := store.db.ExecContext(ctx, `CREATE TRIGGER no_marker BEFORE INSERT ON fleet_sync_state BEGIN SELECT RAISE(ABORT, 'no marker'); END`); err != nil {
		t.Fatalf("trigger: %v", err)
	}
	host := systemMemberServer(t, false)
	rec := do(t, srv, http.MethodPost, "/api/members", `{"name":"second","url":"`+host.URL+`","token":"tok"}`, true)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("add = %d, want 500", rec.Code)
	}
}

// Both checks that ask whose primary role a host holds need this desk's own
// id; failing to read it is an error, never a guess.
func TestOwnFrontdeskIDFailure(t *testing.T) {
	srv, store := newTestServer(t)
	ctx := t.Context()
	for _, q := range []string{
		`UPDATE settings SET frontdesk_id = '' WHERE id = 1`,
		`CREATE TRIGGER no_fd_id BEFORE UPDATE OF frontdesk_id ON settings BEGIN SELECT RAISE(ABORT, 'no id'); END`,
	} {
		if _, err := store.db.ExecContext(ctx, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	host := fleetIdentityStub(t, `{"state":"warning","is_primary":true,"frontdesk_id":"fd-x"}`, "iid-x")
	rec := do(t, srv, http.MethodPost, "/api/members", `{"name":"x","url":"`+host.URL+`","token":"tok"}`, true)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("add = %d, want 500", rec.Code)
	}
	m, err := store.CreateMember(ctx, "candidate", host.URL, "tok")
	if err != nil {
		t.Fatalf("CreateMember: %v", err)
	}
	if _, err := srv.repointTargetsCurrentPrimary(ctx, AutoSyncConfig{PrimaryID: "other"}, m); err == nil {
		t.Error("repointTargetsCurrentPrimary succeeded without this desk's id")
	}
}

// The marker write never claims a run, and never erases one recorded against
// the member it names.
func TestSetFleetPrimaryMarker(t *testing.T) {
	store := newTestStore(t)
	ctx := t.Context()
	at := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	if err := store.SetFleetSyncState(ctx, "a", "hotel-a", at); err != nil {
		t.Fatalf("SetFleetSyncState: %v", err)
	}
	if err := store.SetFleetPrimaryMarker(ctx, "a", "hotel-a"); err != nil {
		t.Fatalf("SetFleetPrimaryMarker(a): %v", err)
	}
	if st, found, err := store.GetFleetSyncState(ctx); err != nil || !found || !st.LastRunAt.Equal(at) {
		t.Fatalf("same member: (%+v, found %v, err %v), want the recorded run kept", st, found, err)
	}
	if err := store.SetFleetPrimaryMarker(ctx, "b", "hotel-b"); err != nil {
		t.Fatalf("SetFleetPrimaryMarker(b): %v", err)
	}
	if st, found, err := store.GetFleetSyncState(ctx); err != nil || found || st.PrimaryID != "b" || !st.LastRunAt.IsZero() {
		t.Fatalf("other member: (%+v, found %v, err %v), want b with no run", st, found, err)
	}
}

// The delete guard follows the resolver: on a 3-member fleet whose primary is
// named only by the sync-state marker, that member is refused like a
// designated primary while any other member is removable.
func TestDeleteMember_RefusesTheMarkerPrimary(t *testing.T) {
	srv, store := newTestServer(t)
	ctx := t.Context()
	var ids []string
	for _, name := range []string{"a", "b", "c"} {
		m, err := store.CreateMember(ctx, name, "http://127.0.0.1:9/"+name, "tok")
		if err != nil {
			t.Fatalf("CreateMember: %v", err)
		}
		ids = append(ids, m.ID)
	}
	if err := store.SetFleetPrimaryMarker(ctx, ids[0], "a"); err != nil {
		t.Fatalf("SetFleetPrimaryMarker: %v", err)
	}
	if rec := do(t, srv, http.MethodDelete, "/api/members/"+ids[0], "", true); rec.Code != http.StatusConflict {
		t.Errorf("DELETE marker primary = %d, want 409", rec.Code)
	}
	if rec := do(t, srv, http.MethodDelete, "/api/members/"+ids[1], "", true); rec.Code != http.StatusNoContent {
		t.Errorf("DELETE other member = %d, want 204", rec.Code)
	}
}

// A primary that cannot be resolved refuses the delete rather than guessing.
func TestDeleteMember_PrimaryReadFailure(t *testing.T) {
	srv, store := newTestServer(t)
	ctx := t.Context()
	var last string
	for _, name := range []string{"a", "b", "c"} {
		m, err := store.CreateMember(ctx, name, "http://127.0.0.1:9/"+name, "tok")
		if err != nil {
			t.Fatalf("CreateMember: %v", err)
		}
		last = m.ID
	}
	if _, err := store.db.ExecContext(ctx, `DROP TABLE fleet_sync_state`); err != nil {
		t.Fatalf("drop: %v", err)
	}
	if rec := do(t, srv, http.MethodDelete, "/api/members/"+last, "", true); rec.Code != http.StatusInternalServerError {
		t.Errorf("DELETE = %d, want 500", rec.Code)
	}
	if members, _ := store.ListMembers(ctx); len(members) != 3 {
		t.Errorf("members = %d, want 3", len(members))
	}
}
