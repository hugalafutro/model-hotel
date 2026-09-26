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
	replicaSrv := newAnnounceRecorder(t, http.StatusNoContent)
	primary, err := store.CreateMember(ctx, "primary", primarySrv.srv.URL, "tok-primary")
	if err != nil {
		t.Fatalf("create primary: %v", err)
	}
	// A second member, so it is the marker that names the primary here and not
	// the lone-roster answer.
	if _, err := store.CreateMember(ctx, "replica", replicaSrv.srv.URL, "tok-replica"); err != nil {
		t.Fatalf("create replica: %v", err)
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
	if _, ann, _ := replicaSrv.snapshot(); ann.IsPrimary {
		t.Error("the replica was flagged primary too; only the marked member may be")
	}
}

// TestPollAnnounceOnce_AnnouncesThePrimarysCurrentName: the name is read from the
// live roster, so renaming the primary is reflected on the next announce rather
// than serving whatever name was recorded when the marker was written.
func TestPollAnnounceOnce_AnnouncesThePrimarysCurrentName(t *testing.T) {
	p, store, _ := newTestPoller(t, "")
	ctx := context.Background()

	primarySrv := newAnnounceRecorder(t, http.StatusNoContent)
	replicaSrv := newAnnounceRecorder(t, http.StatusNoContent)
	primary, err := store.CreateMember(ctx, "old-name", primarySrv.srv.URL, "tok-primary")
	if err != nil {
		t.Fatalf("create primary: %v", err)
	}
	// A second member, so the announced name comes from resolving the marker
	// against the roster rather than from the lone-roster answer.
	if _, err := store.CreateMember(ctx, "replica", replicaSrv.srv.URL, "tok-replica"); err != nil {
		t.Fatalf("create replica: %v", err)
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
	if _, ann, _ := replicaSrv.snapshot(); ann.IsPrimary {
		t.Error("the replica was flagged primary; the marker names the renamed member, not it")
	}
}

// TestPollAnnounceOnce_MarkerWinsWhenAutoSyncIsOff: the designation only outranks
// the last-sync marker while auto-sync is actually running. A primary_id survives
// switching auto-sync off (clearing it needs a confirmed token), so preferring it
// unconditionally meant that designating A, turning auto-sync off, then running the
// wizard from B left B, the instance the operator had just synced the fleet from,
// write-locked while A was still announced as primary.
func TestPollAnnounceOnce_MarkerWinsWhenAutoSyncIsOff(t *testing.T) {
	p, store, _ := newTestPoller(t, "")
	ctx := context.Background()

	aSrv := newAnnounceRecorder(t, http.StatusNoContent)
	bSrv := newAnnounceRecorder(t, http.StatusNoContent)
	a, err := store.CreateMember(ctx, "designated-a", aSrv.srv.URL, "tok-a")
	if err != nil {
		t.Fatalf("create a: %v", err)
	}
	b, err := store.CreateMember(ctx, "wizard-synced-b", bSrv.srv.URL, "tok-b")
	if err != nil {
		t.Fatalf("create b: %v", err)
	}
	// Designated, then auto-sync switched off with the designation left behind.
	if err := store.SetAutoSync(ctx, false, a.ID); err != nil {
		t.Fatalf("set auto-sync: %v", err)
	}
	// The operator then wizard-synced the fleet from B, which is the newer act.
	if err := store.SetFleetSyncState(ctx, b.ID, "wizard-synced-b", time.Now().UTC()); err != nil {
		t.Fatalf("set fleet sync state: %v", err)
	}

	p.PollAnnounceOnce(ctx)

	if _, ann, _ := bSrv.snapshot(); !ann.IsPrimary {
		t.Error("the member the operator just synced the fleet from was not flagged primary; it stays write-locked")
	}
	if _, ann, _ := aSrv.snapshot(); ann.IsPrimary {
		t.Error("a dormant designation outranked a fresher wizard sync")
	}
}

// TestPollAnnounceOnce_DormantDesignationStillBeatsNoMarker: the fallback within
// the fallback. With auto-sync off and no wizard run to point at, the designation
// is the only statement of a primary there is, and naming nobody would lock the
// whole fleet including the instance the operator chose.
func TestPollAnnounceOnce_DormantDesignationStillBeatsNoMarker(t *testing.T) {
	p, store, _ := newTestPoller(t, "")
	ctx := context.Background()

	srv := newAnnounceRecorder(t, http.StatusNoContent)
	otherSrv := newAnnounceRecorder(t, http.StatusNoContent)
	m, err := store.CreateMember(ctx, "chosen", srv.srv.URL, "tok")
	if err != nil {
		t.Fatalf("create member: %v", err)
	}
	// A second member, so the designation is what flags the chosen one and not
	// the lone-roster answer.
	if _, err := store.CreateMember(ctx, "other", otherSrv.srv.URL, "tok-other"); err != nil {
		t.Fatalf("create other member: %v", err)
	}
	if err := store.SetAutoSync(ctx, false, m.ID); err != nil {
		t.Fatalf("set auto-sync: %v", err)
	}

	p.PollAnnounceOnce(ctx)

	if _, ann, _ := srv.snapshot(); !ann.IsPrimary {
		t.Error("the designated member was not flagged, so nothing would be editable")
	}
	if _, ann, _ := otherSrv.snapshot(); ann.IsPrimary {
		t.Error("the undesignated member was flagged primary as well")
	}
}

// TestPollAnnounceOnce_StaleDesignationFallsBackToTheMarker: a designation left
// pointing at a member that has since been removed must not win. It resolves to
// nobody, so returning it would beat a marker that still names a real member and
// tell the whole fleet there is no primary.
func TestPollAnnounceOnce_StaleDesignationFallsBackToTheMarker(t *testing.T) {
	p, store, _ := newTestPoller(t, "")
	ctx := context.Background()

	goneSrv := newAnnounceRecorder(t, http.StatusNoContent)
	liveSrv := newAnnounceRecorder(t, http.StatusNoContent)
	gone, err := store.CreateMember(ctx, "gone", goneSrv.srv.URL, "tok-gone")
	if err != nil {
		t.Fatalf("create gone: %v", err)
	}
	live, err := store.CreateMember(ctx, "live", liveSrv.srv.URL, "tok-live")
	if err != nil {
		t.Fatalf("create live: %v", err)
	}
	// A third member, so removing the designated one still leaves a roster the
	// two sources have to resolve against rather than a lone member.
	restSrv := newAnnounceRecorder(t, http.StatusNoContent)
	if _, err := store.CreateMember(ctx, "rest", restSrv.srv.URL, "tok-rest"); err != nil {
		t.Fatalf("create rest: %v", err)
	}
	if err := store.SetAutoSync(ctx, true, gone.ID); err != nil {
		t.Fatalf("set auto-sync: %v", err)
	}
	if err := store.SetFleetSyncState(ctx, live.ID, "live", time.Now().UTC()); err != nil {
		t.Fatalf("set fleet sync state: %v", err)
	}
	if err := store.DeleteMember(ctx, gone.ID); err != nil {
		t.Fatalf("delete gone: %v", err)
	}

	p.PollAnnounceOnce(ctx)

	if _, ann, _ := liveSrv.snapshot(); !ann.IsPrimary {
		t.Error("a designation pointing at a removed member left the fleet with no primary at all")
	}
	if _, ann, _ := restSrv.snapshot(); ann.IsPrimary {
		t.Error("a member neither source names was flagged primary")
	}
}

// TestPollAnnounceOnce_FlagsTheLoneMemberAsPrimary: a fleet of one has no
// designated primary and can never get one (the wizard and SetAutoSyncGuarded
// both refuse a designation below two members). Announcing is_primary=false there
// told the sole instance it was a managed member, which locked providers, virtual
// keys, users and synced settings behind a 403 naming a primary that does not
// exist. The only member is the config source of truth, so it is flagged.
func TestPollAnnounceOnce_FlagsTheLoneMemberAsPrimary(t *testing.T) {
	p, store, _ := newTestPoller(t, "")
	ctx := context.Background()

	srv := newAnnounceRecorder(t, http.StatusNoContent)
	// No designation and no wizard run: the state a freshly added first member is
	// in, and the only state a one-member fleet can be in.
	if _, err := store.CreateMember(ctx, "solo", srv.srv.URL, "tok"); err != nil {
		t.Fatalf("create member: %v", err)
	}

	p.PollAnnounceOnce(ctx)

	_, ann, _ := srv.snapshot()
	if !ann.IsPrimary {
		t.Error("the only member of the fleet was not flagged primary, so everything on it stays read-only")
	}
	if ann.PrimaryName != "solo" {
		t.Errorf("announced primary name = %q, want %q", ann.PrimaryName, "solo")
	}
}

// TestPollAnnounceOnce_SecondMemberEndsTheLonePrimary: the lone-roster answer is
// recomputed per poll from the roster alone and is not itself stored. The rows
// here come from CreateMember, which writes the row alone (the verified add
// also records the lone member as the marker, TestCreateMemberKeepsTheLonePrimary),
// so once a second row exists nothing names a primary and the flag drops.
func TestPollAnnounceOnce_SecondMemberEndsTheLonePrimary(t *testing.T) {
	p, store, _ := newTestPoller(t, "")
	ctx := context.Background()

	firstSrv := newAnnounceRecorder(t, http.StatusNoContent)
	secondSrv := newAnnounceRecorder(t, http.StatusNoContent)
	if _, err := store.CreateMember(ctx, "first", firstSrv.srv.URL, "tok-first"); err != nil {
		t.Fatalf("create first: %v", err)
	}

	// Lone fleet: the one member is the primary.
	p.PollAnnounceOnce(ctx)
	if hits, ann, _ := firstSrv.snapshot(); hits != 1 || !ann.IsPrimary {
		t.Fatalf("lone member: hits=%d is_primary=%v, want 1/true", hits, ann.IsPrimary)
	}

	if _, err := store.CreateMember(ctx, "second", secondSrv.srv.URL, "tok-second"); err != nil {
		t.Fatalf("create second: %v", err)
	}
	p.PollAnnounceOnce(ctx)

	// Both announces still land (hit counts prove it): the flag dropped because
	// the roster grew, not because announcing stopped.
	if hits, ann, _ := firstSrv.snapshot(); hits != 2 || ann.IsPrimary {
		t.Errorf("first member after the second joined: hits=%d is_primary=%v, want 2/false", hits, ann.IsPrimary)
	}
	if hits, ann, _ := secondSrv.snapshot(); hits != 1 || ann.IsPrimary {
		t.Errorf("second member: hits=%d is_primary=%v, want 1/false", hits, ann.IsPrimary)
	}
}

// TestFleetPrimaryFailsOpenOnReadErrors: both primary sources are reads that can
// fail. Neither may abort the announce: the membership signal is still worth
// sending, and a transient database error must not tell a healthy fleet that some
// other member is now the primary.
func TestFleetPrimaryFailsOpenOnReadErrors(t *testing.T) {
	p, store, _ := newTestPoller(t, "")
	ctx := context.Background()
	// Two members: a one-member roster resolves to its member whatever the
	// sources say, so it could not show what a failed read does.
	m, err := store.CreateMember(ctx, "member", "http://127.0.0.1:9", "tok")
	if err != nil {
		t.Fatalf("create member: %v", err)
	}
	other, err := store.CreateMember(ctx, "other", "http://127.0.0.1:10", "tok-other")
	if err != nil {
		t.Fatalf("create other member: %v", err)
	}
	members := []*Member{m, other}
	if err := store.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	id, name, ok := p.fleetPrimary(ctx, members)

	if ok || id != "" || name != "" {
		t.Errorf("fleetPrimary() = (%q, %q, %v), want no primary when neither source can be read", id, name, ok)
	}
}

// TestCreateMemberKeepsTheLonePrimary: adding a second member to a one-member
// fleet records the lone member as the sync-state marker, so the announce keeps
// naming it primary instead of demoting it to a managed member whose config
// the wizard could then overwrite unannounced. A later add to the two-member
// fleet leaves the marker alone.
func TestCreateMemberKeepsTheLonePrimary(t *testing.T) {
	srv, store := newTestServer(t)
	ctx := t.Context()
	add := func(name string) {
		t.Helper()
		host := systemMemberServer(t, false)
		if rec := do(t, srv, http.MethodPost, "/api/members", `{"name":"`+name+`","url":"`+host.URL+`","token":"tok"}`, true); rec.Code != http.StatusCreated {
			t.Fatalf("add %s = %d (%s), want 201", name, rec.Code, rec.Body.String())
		}
	}
	add("original")
	add("newcomer")

	members, err := store.ListMembers(ctx)
	if err != nil || len(members) != 2 {
		t.Fatalf("members = %d (err %v), want 2", len(members), err)
	}
	_, name, ok := srv.poller.fleetPrimary(ctx, members)
	if !ok || name != "original" {
		t.Fatalf("fleet primary after the second add = (%q, %v), want original", name, ok)
	}
	// The marker names a primary without claiming a sync ran.
	state, found, err := store.GetFleetSyncState(ctx)
	if err != nil || found || state.PrimaryName != "original" {
		t.Fatalf("sync state = (%+v, found %v, err %v), want original with no run", state, found, err)
	}

	add("third")
	members, _ = store.ListMembers(ctx)
	if _, name, _ := srv.poller.fleetPrimary(ctx, members); name != "original" {
		t.Fatalf("fleet primary after a third add = %q, want original", name)
	}
}
