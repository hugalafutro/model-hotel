package frontdesk

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
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

// Distribution needs a destination and a fleet that has been set up: a lone
// primary is not read at all, a fleet that only grew from one member (the
// no-run marker) is not fed either, since its same-named providers may be
// different accounts, and once a real sync run is recorded against the marker
// member the fleet is fed from it.
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
	if primaryHits.Load() != 0 || otherHits.Load() != 0 {
		t.Fatalf("no-run marker: primary reads=%d, other pushes=%d, want 0 and 0", primaryHits.Load(), otherHits.Load())
	}

	if err := store.SetFleetSyncState(ctx, pm.ID, pm.Name, time.Now()); err != nil {
		t.Fatalf("SetFleetSyncState: %v", err)
	}
	srv.DistributeQuotaOnce(ctx)
	if primaryHits.Load() != 1 || otherHits.Load() != 1 {
		t.Errorf("recorded run: primary reads=%d, other pushes=%d, want 1 and 1", primaryHits.Load(), otherHits.Load())
	}
}

// Designating a primary supersedes the no-run marker a 1->2 add leaves, so
// pausing auto-sync afterwards cannot hand the primary back to the marker
// member.
func TestDesignationSupersedesTheNoRunMarker(t *testing.T) {
	srv, store := newTestServer(t)
	ctx := t.Context()
	for _, name := range []string{"a", "b"} {
		host := systemMemberServer(t, false)
		if rec := do(t, srv, http.MethodPost, "/api/members", `{"name":"`+name+`","url":"`+host.URL+`","token":"tok"}`, true); rec.Code != http.StatusCreated {
			t.Fatalf("add %s = %d, want 201", name, rec.Code)
		}
	}
	members, err := store.ListMembers(ctx)
	if err != nil {
		t.Fatalf("ListMembers: %v", err)
	}
	var b *Member
	for _, m := range members {
		if m.Name == "b" {
			b = m
		}
	}
	for _, enabled := range []bool{true, false} {
		if ok, err := store.SetAutoSyncGuarded(ctx, enabled, b.ID, false); err != nil || !ok {
			t.Fatalf("SetAutoSyncGuarded(%v, b) = (%v, %v), want applied", enabled, ok, err)
		}
	}
	if _, name, _ := srv.poller.fleetPrimary(ctx, members); name != "b" {
		t.Fatalf("primary after pausing auto-sync = %q, want b (the designation)", name)
	}
}

// The marker clear shares the designation's transaction: if it fails, the
// designation is not written either.
func TestSetAutoSyncGuarded_MarkerClearFailure(t *testing.T) {
	store := newTestStore(t)
	ctx := t.Context()
	var ids []string
	for _, name := range []string{"a", "b"} {
		m, err := store.CreateMember(ctx, name, "http://127.0.0.1:9/"+name, "tok")
		if err != nil {
			t.Fatalf("CreateMember: %v", err)
		}
		ids = append(ids, m.ID)
	}
	if _, err := store.db.ExecContext(ctx, `CREATE TRIGGER boom BEFORE DELETE ON fleet_sync_state BEGIN SELECT RAISE(ABORT, 'boom'); END`); err != nil {
		t.Fatalf("trigger: %v", err)
	}
	if err := store.SetFleetPrimaryMarker(ctx, ids[0], "a"); err != nil {
		t.Fatalf("SetFleetPrimaryMarker: %v", err)
	}
	if _, err := store.SetAutoSyncGuarded(ctx, true, ids[1], false); err == nil {
		t.Fatal("SetAutoSyncGuarded succeeded although the marker clear failed")
	}
	if cfg, _ := store.GetAutoSync(ctx); cfg.PrimaryID != "" {
		t.Errorf("designation = %q after the failed write, want none", cfg.PrimaryID)
	}
	// Regression pin: a closed store refuses the write.
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := store.SetAutoSyncGuarded(ctx, true, ids[1], false); err == nil {
		t.Error("SetAutoSyncGuarded succeeded on a closed store")
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
	if members, _ := store.ListMembers(ctx); len(members) != 1 {
		t.Errorf("members = %d after the failed add, want 1 (the insert rolled back)", len(members))
	}
}

// Both checks that ask whose primary role a host holds need this desk's own
// id; failing to read it is an error, never a guess. Regression pin for the
// add: it refuses with 500 when the id cannot be read.
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

// The lone-primary marker rides the add that grows the roster to two: it never
// claims a run, never erases one recorded against the member it names, and a
// later add leaves it alone.
func TestCreateVerifiedMember_LonePrimaryMarker(t *testing.T) {
	ctx := t.Context()
	// Regression pin: a run recorded against the lone member stays recorded.
	t.Run("keeps a run recorded against the lone member", func(t *testing.T) {
		store := newTestStore(t)
		a, err := store.CreateMember(ctx, "a", "http://127.0.0.1:9/a", "tok")
		if err != nil {
			t.Fatalf("CreateMember: %v", err)
		}
		at := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
		if err := store.SetFleetSyncState(ctx, a.ID, "a", at); err != nil {
			t.Fatalf("SetFleetSyncState: %v", err)
		}
		if _, err := store.CreateVerifiedMember(ctx, "b", "http://127.0.0.1:9/b", "tok", "iid-b"); err != nil {
			t.Fatalf("CreateVerifiedMember: %v", err)
		}
		if st, found, err := store.GetFleetSyncState(ctx); err != nil || !found || st.PrimaryID != a.ID || !st.LastRunAt.Equal(at) {
			t.Fatalf("(%+v, found %v, err %v), want a with the recorded run", st, found, err)
		}
	})
	t.Run("replaces a ghost and survives a third add", func(t *testing.T) {
		store := newTestStore(t)
		if err := store.SetFleetSyncState(ctx, "ghost", "ghost", time.Now()); err != nil {
			t.Fatalf("SetFleetSyncState: %v", err)
		}
		a, err := store.CreateMember(ctx, "a", "http://127.0.0.1:9/a", "tok")
		if err != nil {
			t.Fatalf("CreateMember: %v", err)
		}
		for _, n := range []string{"b", "c"} {
			if _, err := store.CreateVerifiedMember(ctx, n, "http://127.0.0.1:9/"+n, "tok", "iid-"+n); err != nil {
				t.Fatalf("CreateVerifiedMember(%s): %v", n, err)
			}
		}
		if st, found, err := store.GetFleetSyncState(ctx); err != nil || found || st.PrimaryID != a.ID || !st.LastRunAt.IsZero() {
			t.Fatalf("(%+v, found %v, err %v), want a with no run", st, found, err)
		}
	})
	// Two adds racing on a one-member roster serialize on the insert's
	// transaction: whichever lands second sees three rows, so the marker names
	// the original member either way.
	t.Run("concurrent adds keep the original member", func(t *testing.T) {
		store := newTestStore(t)
		a, err := store.CreateMember(ctx, "a", "http://127.0.0.1:9/a", "tok")
		if err != nil {
			t.Fatalf("CreateMember: %v", err)
		}
		var wg sync.WaitGroup
		errs := make([]error, 2)
		for i, n := range []string{"b", "c"} {
			wg.Go(func() {
				_, errs[i] = store.CreateVerifiedMember(ctx, n, "http://127.0.0.1:9/"+n, "tok", "iid-"+n)
			})
		}
		wg.Wait()
		for _, err := range errs {
			if err != nil {
				t.Fatalf("CreateVerifiedMember: %v", err)
			}
		}
		if st, _, err := store.GetFleetSyncState(ctx); err != nil || st.PrimaryID != a.ID {
			t.Fatalf("marker = (%+v, err %v), want a", st, err)
		}
	})
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
// Only the auto-sync flag column is removed, which nothing else on the delete
// path reads, so the refusal comes from the primary resolution alone.
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
	if _, err := store.db.ExecContext(ctx, `ALTER TABLE settings DROP COLUMN auto_sync_enabled`); err != nil {
		t.Fatalf("drop column: %v", err)
	}
	if rec := do(t, srv, http.MethodDelete, "/api/members/"+last, "", true); rec.Code != http.StatusInternalServerError {
		t.Errorf("DELETE = %d, want 500", rec.Code)
	}
	if members, _ := store.ListMembers(ctx); len(members) != 3 {
		t.Errorf("members = %d, want 3", len(members))
	}
}

// On a two-member fleet grown from one, the marker-only primary is refused
// like a designated one; removing the other member disbands as before.
func TestDeleteMember_TwoMemberMarkerPrimary(t *testing.T) {
	srv, store := newTestServer(t)
	ctx := t.Context()
	var ids []string
	for _, name := range []string{"a", "b"} {
		m, err := store.CreateVerifiedMember(ctx, name, "http://127.0.0.1:9/"+name, "tok", "iid-"+name)
		if err != nil {
			t.Fatalf("CreateVerifiedMember: %v", err)
		}
		ids = append(ids, m.ID)
	}
	if rec := do(t, srv, http.MethodDelete, "/api/members/"+ids[0], "", true); rec.Code != http.StatusConflict {
		t.Fatalf("DELETE marker primary = %d, want 409", rec.Code)
	}
	if rec := do(t, srv, http.MethodDelete, "/api/members/"+ids[1], "", true); rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE other member = %d, want 204", rec.Code)
	}
	if members, _ := store.ListMembers(ctx); len(members) != 0 {
		t.Errorf("members = %d after the disband, want 0", len(members))
	}
}

// Adds and a disband, in every order, starting from a one-member fleet: each
// is one transaction, so after every step the fleet is either empty or has an
// effective primary, never two members with nobody named.
func TestLonePrimaryMarker_SurvivesAddDisbandInterleavings(t *testing.T) {
	orders := [][]string{
		{"add", "add", "disband"}, {"add", "disband", "add"}, {"disband", "add", "add"},
	}
	for _, order := range orders {
		t.Run(strings.Join(order, "-"), func(t *testing.T) {
			store := newTestStore(t)
			ctx := t.Context()
			if _, err := store.CreateVerifiedMember(ctx, "orig", "http://127.0.0.1:9/orig", "tok", "iid-orig"); err != nil {
				t.Fatalf("seed: %v", err)
			}
			for i, op := range order {
				members, err := store.ListMembers(ctx)
				if err != nil {
					t.Fatalf("ListMembers: %v", err)
				}
				switch op {
				case "add":
					n := "m" + strconv.Itoa(i)
					if _, err := store.CreateVerifiedMember(ctx, n, "http://127.0.0.1:9/"+n, "tok", "iid-"+n); err != nil {
						t.Fatalf("add: %v", err)
					}
				case "disband":
					// The newest row is never the primary, so removing it disbands a
					// fleet of up to two and plainly removes one from three.
					if _, _, err := store.DeleteMemberOrDisband(ctx, members[len(members)-1].ID); err != nil {
						t.Fatalf("disband: %v", err)
					}
				}
				members, _ = store.ListMembers(ctx)
				cfg, _ := store.GetAutoSync(ctx)
				st, _, _ := store.GetFleetSyncState(ctx)
				if len(members) > 0 && effectivePrimaryID(members, cfg, st.PrimaryID) == "" {
					t.Fatalf("after %s: %d members and no effective primary", op, len(members))
				}
			}
		})
	}
}

// A host claiming to be this fleet's primary (own desk id) is refused while a
// roster row cannot be identified: that row may be the same host under an
// address that no longer answers.
func TestCreateMember_OwnPrimaryRefusedBesideAnUnidentifiedRow(t *testing.T) {
	srv, store := newTestServer(t)
	ctx := t.Context()
	ownID, err := store.EnsureFrontdeskID(ctx)
	if err != nil {
		t.Fatalf("EnsureFrontdeskID: %v", err)
	}
	// A legacy row: no instance_id stored, and its address does not answer.
	if _, err := store.CreateMember(ctx, "legacy", "http://127.0.0.1:9", "tok"); err != nil {
		t.Fatalf("CreateMember: %v", err)
	}
	host := fleetIdentityStub(t, `{"state":"primary","is_primary":true,"frontdesk_id":"`+ownID+`"}`, "iid-primary")
	rec := do(t, srv, http.MethodPost, "/api/members", `{"name":"again","url":"`+host.URL+`","token":"tok"}`, true)
	var body struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if rec.Code != http.StatusBadRequest || body.Code != "identity_unverified" {
		t.Fatalf("add = %d %q, want 400 identity_unverified", rec.Code, body.Code)
	}
	if members, _ := store.ListMembers(ctx); len(members) != 1 {
		t.Errorf("members = %d, want 1", len(members))
	}
}

// A transaction that read the roster and then writes after another connection
// committed fails with SQLITE_BUSY_SNAPSHOT; the delete path maps that real
// error to ErrMembershipChanged and passes every other error through.
func TestMembershipChangedOnBusySnapshot(t *testing.T) {
	store := newTestStore(t)
	ctx := t.Context()
	if _, err := store.CreateMember(ctx, "a", "http://127.0.0.1:9/a", "tok"); err != nil {
		t.Fatalf("CreateMember: %v", err)
	}
	reader, err := store.db.Conn(ctx)
	if err != nil {
		t.Fatalf("Conn: %v", err)
	}
	defer func() { _ = reader.Close() }()
	if _, err := reader.ExecContext(ctx, `BEGIN`); err != nil {
		t.Fatalf("BEGIN: %v", err)
	}
	var n int
	if err := reader.QueryRowContext(ctx, `SELECT COUNT(*) FROM members`).Scan(&n); err != nil {
		t.Fatalf("read: %v", err)
	}
	if _, err := store.CreateMember(ctx, "b", "http://127.0.0.1:9/b", "tok"); err != nil {
		t.Fatalf("concurrent write: %v", err)
	}
	_, err = reader.ExecContext(ctx, `DELETE FROM members WHERE name = 'a'`)
	if got := membershipChangedOnBusySnapshot(err); !errors.Is(got, ErrMembershipChanged) {
		t.Fatalf("write after a concurrent commit: %v, want ErrMembershipChanged", got)
	}
	_, _ = reader.ExecContext(ctx, `ROLLBACK`)
	other := errors.New("other")
	if got := membershipChangedOnBusySnapshot(other); !errors.Is(got, other) || errors.Is(got, ErrMembershipChanged) {
		t.Errorf("other error mapped to %v, want it unchanged", got)
	}
	if membershipChangedOnBusySnapshot(nil) != nil {
		t.Error("nil error mapped to non-nil")
	}
}

// An unreadable auto-sync row is Front Desk's own failure: the quota proxy
// answers 500 rather than an empty "no quota" that would wipe device badges.
func TestHandleQuota_AutoSyncReadFailure(t *testing.T) {
	srv, store := newTestServer(t)
	if _, err := store.db.ExecContext(t.Context(), `ALTER TABLE settings DROP COLUMN auto_sync_enabled`); err != nil {
		t.Fatalf("drop column: %v", err)
	}
	rr := httptest.NewRecorder()
	srv.handleQuota(rr, httptest.NewRequest(http.MethodGet, "/api/quota", http.NoBody))
	if rr.Code != http.StatusInternalServerError {
		t.Errorf("handleQuota = %d, want 500", rr.Code)
	}
}

// A failed insert or an unusable store surfaces as an error and leaves no row.
func TestCreateVerifiedMember_StoreFailures(t *testing.T) {
	ctx := t.Context()
	store := newTestStore(t)
	if _, err := store.db.ExecContext(ctx, `CREATE TRIGGER no_insert BEFORE INSERT ON members BEGIN SELECT RAISE(ABORT, 'no insert'); END`); err != nil {
		t.Fatalf("trigger: %v", err)
	}
	if _, err := store.CreateVerifiedMember(ctx, "a", "http://127.0.0.1:9/a", "tok", "iid-a"); err == nil {
		t.Error("insert failure: CreateVerifiedMember succeeded")
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := store.CreateVerifiedMember(ctx, "a", "http://127.0.0.1:9/a", "tok", "iid-a"); err == nil {
		t.Error("closed store: CreateVerifiedMember succeeded")
	}
}

// A dormant designation B with the marker naming A: A is the effective primary
// and refused on any fleet above one member, while B is refused only from
// three members up. At two, removing B disbands, so the fleet can always be
// emptied.
func TestDeleteMember_DormantDesignationBesideTheMarkerPrimary(t *testing.T) {
	setup := func(t *testing.T, n int) (*Server, []string) {
		t.Helper()
		srv, store := newTestServer(t)
		ctx := t.Context()
		var ids []string
		for i := range n {
			name := "m" + strconv.Itoa(i)
			m, err := store.CreateMember(ctx, name, "http://127.0.0.1:9/"+name, "tok")
			if err != nil {
				t.Fatalf("CreateMember: %v", err)
			}
			ids = append(ids, m.ID)
		}
		if err := store.SetAutoSync(ctx, false, ids[1]); err != nil {
			t.Fatalf("SetAutoSync: %v", err)
		}
		if err := store.SetFleetPrimaryMarker(ctx, ids[0], "m0"); err != nil {
			t.Fatalf("SetFleetPrimaryMarker: %v", err)
		}
		return srv, ids
	}
	t.Run("two members", func(t *testing.T) {
		srv, ids := setup(t, 2)
		if rec := do(t, srv, http.MethodDelete, "/api/members/"+ids[0], "", true); rec.Code != http.StatusConflict {
			t.Fatalf("DELETE marker primary = %d, want 409", rec.Code)
		}
		if rec := do(t, srv, http.MethodDelete, "/api/members/"+ids[1], "", true); rec.Code != http.StatusNoContent {
			t.Fatalf("DELETE dormant designation = %d, want 204 (disband)", rec.Code)
		}
	})
	t.Run("three members", func(t *testing.T) {
		srv, ids := setup(t, 3)
		if rec := do(t, srv, http.MethodDelete, "/api/members/"+ids[1], "", true); rec.Code != http.StatusConflict {
			t.Fatalf("DELETE dormant designation = %d, want 409", rec.Code)
		}
	})
}

// A confirmed takeover of a host too old to report its desk id adds no empty
// taken_over_from key to member.added (the takeover warning is still logged).
func TestCreateMember_TakeoverWithoutADeskID(t *testing.T) {
	srv, store := newTestServer(t)
	host := fleetIdentityStub(t, `{"state":"warning","is_primary":true}`, "iid-old")
	body := `{"name":"old","url":"` + host.URL + `","token":"tok","confirm_token":"` + testFrontdeskToken + `"}`
	if rec := do(t, srv, http.MethodPost, "/api/members", body, true); rec.Code != http.StatusCreated {
		t.Fatalf("confirmed add = %d, want 201", rec.Code)
	}
	evs, _, err := store.ListEvents(t.Context(), EventFilter{Type: "member.added"})
	if err != nil || len(evs) != 1 {
		t.Fatalf("member.added events = %d (err %v), want 1", len(evs), err)
	}
	if _, ok := evs[0].Metadata["taken_over_from"]; ok {
		t.Errorf("taken_over_from present with no desk id: %v", evs[0].Metadata)
	}
}
