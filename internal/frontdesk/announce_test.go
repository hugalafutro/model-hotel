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
