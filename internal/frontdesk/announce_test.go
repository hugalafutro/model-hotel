package frontdesk

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// announceRecorder is a stub member that captures the announce calls it receives.
type announceRecorder struct {
	mu   sync.Mutex
	hits int
	last memberAnnounce
	auth string
	srv  *httptest.Server
}

func newAnnounceRecorder(t *testing.T, status int) *announceRecorder {
	t.Helper()
	rec := &announceRecorder{}
	rec.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != memberAnnouncePath || r.Method != http.MethodPost {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		rec.mu.Lock()
		defer rec.mu.Unlock()
		rec.hits++
		rec.auth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&rec.last)
		w.WriteHeader(status)
	}))
	t.Cleanup(rec.srv.Close)
	return rec
}

func (r *announceRecorder) snapshot() (int, memberAnnounce, string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.hits, r.last, r.auth
}

func TestPollAnnounceOnce_FlagsPrimaryAndReplica(t *testing.T) {
	p, store, _ := newTestPoller(t, "")
	ctx := context.Background()

	primarySrv := newAnnounceRecorder(t, http.StatusNoContent)
	replicaSrv := newAnnounceRecorder(t, http.StatusNoContent)

	primary, err := store.CreateMember(ctx, "primary", primarySrv.srv.URL, "tok-primary")
	if err != nil {
		t.Fatalf("create primary: %v", err)
	}
	if _, err := store.CreateMember(ctx, "replica", replicaSrv.srv.URL, "tok-replica"); err != nil {
		t.Fatalf("create replica: %v", err)
	}
	if err := store.SetFleetSyncState(ctx, primary.ID, "primary", time.Now().UTC()); err != nil {
		t.Fatalf("set fleet sync state: %v", err)
	}

	p.PollAnnounceOnce(ctx)

	hits, ann, auth := primarySrv.snapshot()
	if hits != 1 || !ann.IsPrimary {
		t.Errorf("primary: hits=%d is_primary=%v, want 1/true", hits, ann.IsPrimary)
	}
	if ann.PrimaryName != "primary" {
		t.Errorf("primary name = %q, want %q", ann.PrimaryName, "primary")
	}
	if auth != "Bearer tok-primary" {
		t.Errorf("primary auth = %q, want Bearer tok-primary", auth)
	}

	hits, ann, auth = replicaSrv.snapshot()
	if hits != 1 || ann.IsPrimary {
		t.Errorf("replica: hits=%d is_primary=%v, want 1/false", hits, ann.IsPrimary)
	}
	if auth != "Bearer tok-replica" {
		t.Errorf("replica auth = %q, want Bearer tok-replica", auth)
	}
}

func TestPollAnnounceOnce_SkipsTokenlessAndToleratesErrors(t *testing.T) {
	p, store, _ := newTestPoller(t, "")
	ctx := context.Background()

	// A member with no stored token: the announce endpoint needs admin auth, so
	// it must be skipped without a call.
	tokenlessSrv := newAnnounceRecorder(t, http.StatusNoContent)
	if _, err := store.CreateMember(ctx, "tokenless", tokenlessSrv.srv.URL, ""); err != nil {
		t.Fatalf("create tokenless: %v", err)
	}
	// A member that errors on announce must not abort the sweep.
	erroringSrv := newAnnounceRecorder(t, http.StatusInternalServerError)
	if _, err := store.CreateMember(ctx, "erroring", erroringSrv.srv.URL, "tok"); err != nil {
		t.Fatalf("create erroring: %v", err)
	}
	okSrv := newAnnounceRecorder(t, http.StatusNoContent)
	if _, err := store.CreateMember(ctx, "ok", okSrv.srv.URL, "tok"); err != nil {
		t.Fatalf("create ok: %v", err)
	}

	// No fleet sync state recorded: no primary is flagged, but the sweep still runs.
	p.PollAnnounceOnce(ctx)

	if hits, _, _ := tokenlessSrv.snapshot(); hits != 0 {
		t.Errorf("tokenless member was called %d times, want 0", hits)
	}
	if hits, ann, _ := erroringSrv.snapshot(); hits != 1 || ann.IsPrimary {
		t.Errorf("erroring member: hits=%d is_primary=%v, want 1/false", hits, ann.IsPrimary)
	}
	// The member after the erroring one still got its announce: errors don't abort.
	if hits, ann, _ := okSrv.snapshot(); hits != 1 || ann.IsPrimary {
		t.Errorf("ok member: hits=%d is_primary=%v, want 1/false", hits, ann.IsPrimary)
	}
}

func TestPollAnnounceOnce_SendsFrontdeskID(t *testing.T) {
	p, store, _ := newTestPoller(t, "")
	p.SetFrontdeskID("fd-abc-123")
	ctx := context.Background()

	srv := newAnnounceRecorder(t, http.StatusNoContent)
	if _, err := store.CreateMember(ctx, "m", srv.srv.URL, "tok"); err != nil {
		t.Fatalf("create member: %v", err)
	}

	p.PollAnnounceOnce(ctx)

	if _, ann, _ := srv.snapshot(); ann.FrontdeskID != "fd-abc-123" {
		t.Errorf("announce frontdesk_id = %q, want %q", ann.FrontdeskID, "fd-abc-123")
	}
}

func TestPollAnnounceOnce_ConflictWarnsOnceDoesNotAbort(t *testing.T) {
	p, store, _ := newTestPoller(t, "")
	p.SetFrontdeskID("fd-second")
	ctx := context.Background()

	// A member owned by another Front Desk replies 409 to every announce.
	conflictSrv := newAnnounceRecorder(t, http.StatusConflict)
	if _, err := store.CreateMember(ctx, "conflict", conflictSrv.srv.URL, "tok"); err != nil {
		t.Fatalf("create conflict member: %v", err)
	}
	okSrv := newAnnounceRecorder(t, http.StatusNoContent)
	if _, err := store.CreateMember(ctx, "ok", okSrv.srv.URL, "tok"); err != nil {
		t.Fatalf("create ok member: %v", err)
	}

	// Two sweeps: the 409 must not abort the sweep (the ok member is still
	// announced) and the conflict latch must be recorded after the first hit.
	p.PollAnnounceOnce(ctx)
	if hits, _, _ := conflictSrv.snapshot(); hits != 1 {
		t.Errorf("conflict member hits after first sweep = %d, want 1", hits)
	}
	if hits, _, _ := okSrv.snapshot(); hits != 1 {
		t.Errorf("ok member hits after first sweep = %d, want 1 (409 must not abort)", hits)
	}

	p.mu.RLock()
	latched := p.conflictNotified[memberIDByName(ctx, t, store, "conflict")]
	p.mu.RUnlock()
	if !latched {
		t.Error("conflict was not latched after a 409 announce")
	}

	// Second sweep still announces (retried every poll) without crashing.
	p.PollAnnounceOnce(ctx)
	if hits, _, _ := conflictSrv.snapshot(); hits != 2 {
		t.Errorf("conflict member hits after second sweep = %d, want 2", hits)
	}
}

func TestPollAnnounceOnce_SendsActiveMembers(t *testing.T) {
	p, store, _ := newTestPoller(t, "")
	ctx := context.Background()

	// Two members, both StateActive by default (CreateMember inserts StateActive),
	// so every announce must carry active_members=2.
	srvA := newAnnounceRecorder(t, http.StatusNoContent)
	srvB := newAnnounceRecorder(t, http.StatusNoContent)
	if _, err := store.CreateMember(ctx, "a", srvA.srv.URL, "tok-a"); err != nil {
		t.Fatalf("create a: %v", err)
	}
	if _, err := store.CreateMember(ctx, "b", srvB.srv.URL, "tok-b"); err != nil {
		t.Fatalf("create b: %v", err)
	}

	p.PollAnnounceOnce(ctx)

	if _, ann, _ := srvA.snapshot(); ann.ActiveMembers != 2 {
		t.Errorf("member a: active_members = %d, want 2", ann.ActiveMembers)
	}
	if _, ann, _ := srvB.snapshot(); ann.ActiveMembers != 2 {
		t.Errorf("member b: active_members = %d, want 2", ann.ActiveMembers)
	}
}

func TestPollAnnounceOnce_ActiveMembersCountsOnlyActive(t *testing.T) {
	p, store, _ := newTestPoller(t, "")
	ctx := context.Background()

	// One active member and one drained member: the drained one is not a Traefik
	// backend, so the announced divisor must be 1, not 2.
	activeSrv := newAnnounceRecorder(t, http.StatusNoContent)
	drainedSrv := newAnnounceRecorder(t, http.StatusNoContent)
	if _, err := store.CreateMember(ctx, "active", activeSrv.srv.URL, "tok-a"); err != nil {
		t.Fatalf("create active: %v", err)
	}
	drained, err := store.CreateMember(ctx, "drained", drainedSrv.srv.URL, "tok-d")
	if err != nil {
		t.Fatalf("create drained: %v", err)
	}
	if err := store.SetMemberState(ctx, drained.ID, StateDrained); err != nil {
		t.Fatalf("drain member: %v", err)
	}

	p.PollAnnounceOnce(ctx)

	if _, ann, _ := activeSrv.snapshot(); ann.ActiveMembers != 1 {
		t.Errorf("active member: active_members = %d, want 1 (drained excluded)", ann.ActiveMembers)
	}
}

// memberIDByName resolves a member's generated ID from its name for assertions.
func memberIDByName(ctx context.Context, t *testing.T, store *Store, name string) string {
	t.Helper()
	members, err := store.ListMembers(ctx)
	if err != nil {
		t.Fatalf("list members: %v", err)
	}
	for _, m := range members {
		if m.Name == name {
			return m.ID
		}
	}
	t.Fatalf("member %q not found", name)
	return ""
}

// TestPollAnnounceOnce_FlagsTheDesignatedPrimaryWithoutASyncRun: the fleet's
// primary is whichever member the operator designated, from the moment they
// designated it. Nothing has to have been written yet.
//
// This is the bug the announce carried: it read the primary from the last-sync
// marker, which only the wizard writes. A fleet driven by automatic sync alone
// therefore never had one, so every member was told is_primary=false, including
// the primary. Each then read itself as a managed member and refused provider,
// virtual-key, user and synced-settings edits with a 403, on the one instance
// those edits are supposed to be made.
func TestPollAnnounceOnce_FlagsTheDesignatedPrimaryWithoutASyncRun(t *testing.T) {
	p, store, _ := newTestPoller(t, "")
	ctx := context.Background()

	primarySrv := newAnnounceRecorder(t, http.StatusNoContent)
	replicaSrv := newAnnounceRecorder(t, http.StatusNoContent)
	primary, err := store.CreateMember(ctx, "alpha", primarySrv.srv.URL, "tok-primary")
	if err != nil {
		t.Fatalf("create primary: %v", err)
	}
	if _, err := store.CreateMember(ctx, "beta", replicaSrv.srv.URL, "tok-replica"); err != nil {
		t.Fatalf("create replica: %v", err)
	}
	// Auto-sync designates the primary. No wizard run, so no last-sync marker.
	if err := store.SetAutoSync(ctx, true, primary.ID); err != nil {
		t.Fatalf("set auto-sync: %v", err)
	}

	p.PollAnnounceOnce(ctx)

	if _, ann, _ := primarySrv.snapshot(); !ann.IsPrimary {
		t.Error("the designated primary was told is_primary=false; it would lock itself out of its own config")
	}
	if _, ann, _ := primarySrv.snapshot(); ann.PrimaryName != "alpha" {
		t.Errorf("primary name = %q, want %q", ann.PrimaryName, "alpha")
	}
	if _, ann, _ := replicaSrv.snapshot(); ann.IsPrimary {
		t.Error("a replica was told it is the primary")
	}
	if _, ann, _ := replicaSrv.snapshot(); ann.PrimaryName != "alpha" {
		t.Errorf("replica was told the primary is %q, want %q", ann.PrimaryName, "alpha")
	}
}

// TestPollAnnounceOnce_DesignationBeatsTheLastSyncMarker: repointing the fleet
// takes effect at once. The marker still names whichever member drove the last
// sync, so reading it first would keep announcing the old primary until some
// later run happened to overwrite it, leaving the newly designated primary
// locked out and the old one editable.
func TestPollAnnounceOnce_DesignationBeatsTheLastSyncMarker(t *testing.T) {
	p, store, _ := newTestPoller(t, "")
	ctx := context.Background()

	oldSrv := newAnnounceRecorder(t, http.StatusNoContent)
	newSrv := newAnnounceRecorder(t, http.StatusNoContent)
	oldPrimary, err := store.CreateMember(ctx, "was-primary", oldSrv.srv.URL, "tok-old")
	if err != nil {
		t.Fatalf("create old primary: %v", err)
	}
	newPrimary, err := store.CreateMember(ctx, "now-primary", newSrv.srv.URL, "tok-new")
	if err != nil {
		t.Fatalf("create new primary: %v", err)
	}
	// The marker records the member that drove the last wizard run.
	if err := store.SetFleetSyncState(ctx, oldPrimary.ID, "was-primary", time.Now().UTC()); err != nil {
		t.Fatalf("set fleet sync state: %v", err)
	}
	// The operator has since repointed the fleet.
	if err := store.SetAutoSync(ctx, true, newPrimary.ID); err != nil {
		t.Fatalf("set auto-sync: %v", err)
	}

	p.PollAnnounceOnce(ctx)

	if _, ann, _ := newSrv.snapshot(); !ann.IsPrimary {
		t.Error("the newly designated primary was not flagged; the stale marker outvoted the operator")
	}
	if _, ann, _ := oldSrv.snapshot(); ann.IsPrimary {
		t.Error("the previous primary is still flagged, so two members would accept primary-only edits")
	}
}

// TestPollAnnounceOnce_FallsBackToTheMarkerWithoutADesignation: a fleet driven by
// the wizard alone designates no primary, and there the marker is the only
// statement of one. It must still be honoured.
func TestPollAnnounceOnce_FallsBackToTheMarkerWithoutADesignation(t *testing.T) {
	p, store, _ := newTestPoller(t, "")
	ctx := context.Background()

	primarySrv := newAnnounceRecorder(t, http.StatusNoContent)
	primary, err := store.CreateMember(ctx, "primary", primarySrv.srv.URL, "tok-primary")
	if err != nil {
		t.Fatalf("create primary: %v", err)
	}
	if err := store.SetFleetSyncState(ctx, primary.ID, "primary", time.Now().UTC()); err != nil {
		t.Fatalf("set fleet sync state: %v", err)
	}
	// Auto-sync off and no designation, the state after a wizard-only sync.
	if err := store.SetAutoSync(ctx, false, ""); err != nil {
		t.Fatalf("set auto-sync: %v", err)
	}

	p.PollAnnounceOnce(ctx)

	if _, ann, _ := primarySrv.snapshot(); !ann.IsPrimary {
		t.Error("a wizard-synced fleet lost its primary flag")
	}
}

// TestPollAnnounceOnce_AnnouncesThePrimarysCurrentName: the name is read from the
// live roster, so renaming the primary is reflected on the next announce rather
// than serving whatever name was recorded when the marker was written.
func TestPollAnnounceOnce_AnnouncesThePrimarysCurrentName(t *testing.T) {
	p, store, _ := newTestPoller(t, "")
	ctx := context.Background()

	primarySrv := newAnnounceRecorder(t, http.StatusNoContent)
	primary, err := store.CreateMember(ctx, "old-name", primarySrv.srv.URL, "tok-primary")
	if err != nil {
		t.Fatalf("create primary: %v", err)
	}
	if err := store.SetFleetSyncState(ctx, primary.ID, "old-name", time.Now().UTC()); err != nil {
		t.Fatalf("set fleet sync state: %v", err)
	}
	const newName = "new-name"
	if err := store.RenameMember(ctx, primary.ID, newName); err != nil {
		t.Fatalf("rename primary: %v", err)
	}

	p.PollAnnounceOnce(ctx)

	if _, ann, _ := primarySrv.snapshot(); ann.PrimaryName != newName {
		t.Errorf("announced primary name = %q, want the current %q", ann.PrimaryName, newName)
	}
}
