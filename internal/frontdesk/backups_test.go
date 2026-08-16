package frontdesk

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// stubBackupMember is a fake Model Hotel member exposing the one backup route
// Front Desk uses: GET /api/backups (the listing, each entry carrying the origin
// the member itself derived). Origin is set per entry by the test, deliberately
// independent of the filename, so a test can model a manual backup whose name
// happens to contain the word frontdesk.
type stubBackupMember struct {
	token string

	mu         sync.Mutex
	files      []memberBackupEntry
	listStatus int // 0 means 200 with the listing
	listBody   string

	srv *httptest.Server
}

func newStubBackupMember(t *testing.T, token string, files ...memberBackupEntry) *stubBackupMember {
	t.Helper()
	sm := &stubBackupMember{token: token, files: files}
	sm.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+sm.token {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		sm.mu.Lock()
		defer sm.mu.Unlock()
		if r.Method == http.MethodGet && strings.TrimSuffix(r.URL.Path, "/") == "/api/backups" {
			if sm.listStatus != 0 {
				w.WriteHeader(sm.listStatus)
				return
			}
			if sm.listBody != "" {
				_, _ = w.Write([]byte(sm.listBody))
				return
			}
			out := sm.files
			if out == nil {
				out = []memberBackupEntry{}
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(out)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(sm.srv.Close)
	return sm
}

// backupEntryAt builds a listing entry with an explicit origin and age.
func backupEntryAt(name, origin string, age time.Duration) memberBackupEntry {
	return memberBackupEntry{
		Filename:  name,
		Origin:    origin,
		CreatedAt: time.Now().Add(-age).UTC().Format(time.RFC3339),
	}
}

// eventTypes lists the recorded event types for a member, oldest first.
func eventTypes(t *testing.T, store *Store, memberID string) []string {
	t.Helper()
	evs, _, err := store.ListEvents(t.Context(), EventFilter{})
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	out := make([]string, 0, len(evs))
	for i := len(evs) - 1; i >= 0; i-- {
		if memberID == "" || evs[i].MemberID == memberID {
			out = append(out, evs[i].Type)
		}
	}
	return out
}

// TestBackupStaleEmitsOnceAcrossPolls: a member whose newest scheduled backup is
// older than a day is unprotected, and it is said once, not on every pass.
func TestBackupStaleEmitsOnceAcrossPolls(t *testing.T) {
	srv, store := newTestServer(t)
	member := newStubBackupMember(t, "tok",
		backupEntryAt("backup_old_auto.dump", "scheduled", 30*time.Hour),
	)
	m, err := store.CreateMember(t.Context(), "m1", member.srv.URL, "tok")
	if err != nil {
		t.Fatalf("CreateMember: %v", err)
	}

	for range 3 {
		srv.checkMemberBackups(t.Context())
	}

	got := eventTypes(t, store, m.ID)
	if len(got) != 1 || got[0] != "backup.stale" {
		t.Fatalf("events = %v, want exactly one backup.stale", got)
	}
}

// TestBackupStaleThresholdBoundary pins the VALUE of memberBackupStaleAfter,
// not merely the direction. The ages are written as literals on purpose: an age
// expressed relative to the constant would follow it wherever it moved and
// prove only that older is staler. 23 hours must stay quiet and 25 hours must
// alert, so any threshold other than a day fails here.
func TestBackupStaleThresholdBoundary(t *testing.T) {
	if memberBackupStaleAfter != 24*time.Hour {
		t.Fatalf("memberBackupStaleAfter = %s; the cases below are written for 24h", memberBackupStaleAfter)
	}
	for _, tc := range []struct {
		name      string
		age       time.Duration
		wantStale bool
	}{
		{"an hour inside the window", 23 * time.Hour, false},
		{"an hour outside the window", 25 * time.Hour, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, store := newTestServer(t)
			member := newStubBackupMember(t, "tok",
				backupEntryAt("backup_auto.dump", "scheduled", tc.age),
			)
			m, err := store.CreateMember(t.Context(), "m1", member.srv.URL, "tok")
			if err != nil {
				t.Fatalf("CreateMember: %v", err)
			}

			srv.checkMemberBackups(t.Context())

			got := eventTypes(t, store, m.ID)
			if tc.wantStale {
				if len(got) != 1 || got[0] != "backup.stale" {
					t.Fatalf("events = %v at age %s, want one backup.stale", got, tc.age)
				}
				return
			}
			if len(got) != 0 {
				t.Fatalf("events = %v at age %s, want none", got, tc.age)
			}
		})
	}
}

// TestBackupStaleWithNoScheduledBackup: a member that has never run a scheduled
// backup is unprotected too. Manual and frontdesk-origin files do not count:
// nothing on that member is producing them on a schedule.
func TestBackupStaleWithNoScheduledBackup(t *testing.T) {
	srv, store := newTestServer(t)
	member := newStubBackupMember(t, "tok",
		backupEntryAt("backup_fresh_manual.dump", "manual", time.Minute),
		backupEntryAt("backup_fresh_frontdesk.dump", "frontdesk", time.Minute),
	)
	m, err := store.CreateMember(t.Context(), "m1", member.srv.URL, "tok")
	if err != nil {
		t.Fatalf("CreateMember: %v", err)
	}

	srv.checkMemberBackups(t.Context())

	if got := eventTypes(t, store, m.ID); len(got) != 1 || got[0] != "backup.stale" {
		t.Fatalf("events = %v, want one backup.stale", got)
	}
}

// TestBackupFreshEmitsNothing: a member backing itself up on schedule is quiet.
func TestBackupFreshEmitsNothing(t *testing.T) {
	srv, store := newTestServer(t)
	member := newStubBackupMember(t, "tok",
		backupEntryAt("backup_old_auto.dump", "scheduled", 40*time.Hour),
		backupEntryAt("backup_new_auto.dump", "scheduled", time.Hour),
	)
	m, err := store.CreateMember(t.Context(), "m1", member.srv.URL, "tok")
	if err != nil {
		t.Fatalf("CreateMember: %v", err)
	}

	srv.checkMemberBackups(t.Context())
	srv.checkMemberBackups(t.Context())

	if got := eventTypes(t, store, m.ID); len(got) != 0 {
		t.Fatalf("events = %v, want none for a member with a fresh backup", got)
	}
}

// TestBackupRecoveredEmitsOnce: the recovery edge fires once when a fresh
// scheduled backup appears, and a member that was never flagged stays quiet.
func TestBackupRecoveredEmitsOnce(t *testing.T) {
	srv, store := newTestServer(t)
	member := newStubBackupMember(t, "tok",
		backupEntryAt("backup_old_auto.dump", "scheduled", 30*time.Hour),
	)
	m, err := store.CreateMember(t.Context(), "m1", member.srv.URL, "tok")
	if err != nil {
		t.Fatalf("CreateMember: %v", err)
	}

	srv.checkMemberBackups(t.Context())

	member.mu.Lock()
	member.files = append(member.files, backupEntryAt("backup_new_auto.dump", "scheduled", time.Minute))
	member.mu.Unlock()

	for range 3 {
		srv.checkMemberBackups(t.Context())
	}

	got := eventTypes(t, store, m.ID)
	want := []string{"backup.stale", "backup.recovered"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("events = %v, want %v", got, want)
	}
}

// TestBackupUnreadableMemberIsNotJudged: a member whose backup listing could not
// be read has not been measured. Reporting it unprotected would duplicate
// health.down and, worse, claim a fact about a member Front Desk never saw.
func TestBackupUnreadableMemberIsNotJudged(t *testing.T) {
	srv, store := newTestServer(t)
	member := newStubBackupMember(t, "tok")
	member.listStatus = http.StatusInternalServerError
	m, err := store.CreateMember(t.Context(), "m1", member.srv.URL, "tok")
	if err != nil {
		t.Fatalf("CreateMember: %v", err)
	}

	srv.checkMemberBackups(t.Context())
	srv.checkMemberBackups(t.Context())

	if got := eventTypes(t, store, m.ID); len(got) != 0 {
		t.Fatalf("events = %v, want none: an unread listing is not a measurement", got)
	}
}

// TestBackupWatchReadsPastTheSharedMemberLimit proves the watchdog reads a
// member's listing under maxMemberBackupListBody, not the shared 1 MiB
// maxMemberRespBody. A member is first flagged stale on a short listing, then
// its listing balloons past 1 MiB (comfortably under the 16 MiB backup limit)
// with a fresh scheduled entry appended; backup.recovered only fires if the
// larger body was read in full, so a silent fall-back to the shared limit would
// leave the member stuck stale instead.
func TestBackupWatchReadsPastTheSharedMemberLimit(t *testing.T) {
	srv, store := newTestServer(t)
	member := newStubBackupMember(t, "tok",
		backupEntryAt("backup_old_auto.dump", "scheduled", 30*time.Hour),
	)
	m, err := store.CreateMember(t.Context(), "m1", member.srv.URL, "tok")
	if err != nil {
		t.Fatalf("CreateMember: %v", err)
	}

	srv.checkMemberBackups(t.Context())
	if got := eventTypes(t, store, m.ID); len(got) != 1 || got[0] != "backup.stale" {
		t.Fatalf("events after first pass = %v, want one backup.stale", got)
	}

	// Comfortably past maxMemberRespBody (1 MiB) at roughly 135 bytes an entry,
	// and comfortably short of maxMemberBackupListBody (16 MiB).
	const files = 20000
	entries := make([]memberBackupEntry, 0, files+1)
	for i := range files {
		entries = append(entries, backupEntryAt(
			fmt.Sprintf("backup_20260101_%06d_manual.dump", i), "manual", 30*time.Hour))
	}
	entries = append(entries, backupEntryAt("backup_new_auto.dump", "scheduled", time.Minute))
	member.mu.Lock()
	member.files = entries
	member.mu.Unlock()

	srv.checkMemberBackups(t.Context())

	got := eventTypes(t, store, m.ID)
	want := []string{"backup.stale", "backup.recovered"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("events = %v, want %v: the larger listing must have been read in full", got, want)
	}
}

// TestBackupWatchTreatsAnUnreadablyLargeListingAsUnread: past even the 16 MiB
// backup limit the read is refused rather than truncated, and the watchdog
// treats that exactly like any other unreadable listing: not judged, not
// flagged stale.
func TestBackupWatchTreatsAnUnreadablyLargeListingAsUnread(t *testing.T) {
	srv, store := newTestServer(t)
	member := newStubBackupMember(t, "tok")
	// Valid JSON, just past maxMemberBackupListBody: the failure must come from
	// the size limit, not from the shape of the body.
	member.listBody = "[" + strings.Repeat(`{"filename":"x","created_at":"","origin":"manual"},`,
		(maxMemberBackupListBody/50)+1) + `{"filename":"y","created_at":"","origin":"manual"}]`
	m, err := store.CreateMember(t.Context(), "m1", member.srv.URL, "tok")
	if err != nil {
		t.Fatalf("CreateMember: %v", err)
	}

	srv.checkMemberBackups(t.Context())
	srv.checkMemberBackups(t.Context())

	if got := eventTypes(t, store, m.ID); len(got) != 0 {
		t.Fatalf("events = %v, want none: an oversized listing is not a measurement", got)
	}
}

// TestBackupUnreachableMemberIsNotJudged is the same invariant for a member that
// does not answer at all.
func TestBackupUnreachableMemberIsNotJudged(t *testing.T) {
	srv, store := newTestServer(t)
	member := newStubBackupMember(t, "tok")
	deadURL := member.srv.URL
	member.srv.Close()
	m, err := store.CreateMember(t.Context(), "m1", deadURL, "tok")
	if err != nil {
		t.Fatalf("CreateMember: %v", err)
	}

	srv.checkMemberBackups(t.Context())

	if got := eventTypes(t, store, m.ID); len(got) != 0 {
		t.Fatalf("events = %v, want none for an unreachable member", got)
	}
}

// TestBackupWatchSkipsTokenlessMember: without a stored admin token the listing
// cannot be read at all, which is not the same as being unprotected.
func TestBackupWatchSkipsTokenlessMember(t *testing.T) {
	srv, store := newTestServer(t)
	member := newStubBackupMember(t, "tok")
	m, err := store.CreateMember(t.Context(), "m1", member.srv.URL, "")
	if err != nil {
		t.Fatalf("CreateMember: %v", err)
	}

	srv.checkMemberBackups(t.Context())

	if got := eventTypes(t, store, m.ID); len(got) != 0 {
		t.Fatalf("events = %v, want none for a member with no stored token", got)
	}
}

// TestRunBackupWatchStopsOnContextCancel: the loop returns promptly when its
// context is cancelled, so shutdown is not held up.
func TestRunBackupWatchStopsOnContextCancel(t *testing.T) {
	srv, _ := newTestServer(t)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { srv.RunBackupWatch(ctx); close(done) }()
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("RunBackupWatch did not return after context cancel")
	}
}
