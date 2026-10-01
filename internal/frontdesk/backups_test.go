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

// stubBackupMember is a fake Model Hotel member exposing the two routes the
// backup watchdog reads: GET /api/backups (the listing, each entry carrying the
// origin the member itself derived) and GET /api/settings (backup_interval).
// Origin is set per entry by the test, deliberately independent of the
// filename, so a test can model a manual backup whose name happens to contain
// the word frontdesk.
type stubBackupMember struct {
	token string

	mu             sync.Mutex
	files          []memberBackupEntry
	listStatus     int // 0 means 200 with the listing
	listBody       string
	interval       string // backup_interval in the settings; "" leaves it unset
	settingsStatus int    // 0 means 200 with the settings
	settingsBody   string // when set, served verbatim instead of the settings

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
		if r.Method == http.MethodGet && strings.TrimSuffix(r.URL.Path, "/") == "/api/settings" {
			if sm.settingsStatus != 0 {
				w.WriteHeader(sm.settingsStatus)
				return
			}
			if sm.settingsBody != "" {
				_, _ = w.Write([]byte(sm.settingsBody))
				return
			}
			settings := map[string]string{"backup_enabled": "true"}
			if sm.interval != "" {
				settings["backup_interval"] = sm.interval
			}
			_ = json.NewEncoder(w).Encode(settings)
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

// TestBackupStaleThresholdBoundary pins the threshold's VALUE, not merely its
// direction. The ages are written as literals on purpose: an age expressed
// relative to the threshold would follow it wherever it moved and prove only
// that older is staler. A member's newest dump is routinely a little past its
// interval while the next one is being written, and must stay quiet (the false
// alert the grace exists for); one past the interval by more than the hour of
// grace must alert. The interval is the member's own, never judged tighter than
// a day, so a weekly member is not flagged on day two, nor looser than a week,
// so a member reporting an absurd interval cannot silence its own alert.
func TestBackupStaleThresholdBoundary(t *testing.T) {
	for _, tc := range []struct {
		name      string
		interval  string
		age       time.Duration
		wantStale bool
	}{
		{"daily: the next dump still being written", "", 24*time.Hour + time.Minute, false},
		{"daily: a day and thirty minutes", "", 24*time.Hour + 30*time.Minute, false},
		{"daily: an hour past the grace", "", 26 * time.Hour, true},
		{"daily in the member's day form", "1d", 26 * time.Hour, true},
		{"hourly is still judged by a day", "1h", 24*time.Hour + 30*time.Minute, false},
		{"weekly: six days in", "168h", 6 * 24 * time.Hour, false},
		{"weekly: in the day form, six days in", "7d", 6 * 24 * time.Hour, false},
		{"weekly: a week and an hour past the grace", "168h", 7*24*time.Hour + 2*time.Hour, true},
		{"unparseable interval reads as a day", "fortnightly", 26 * time.Hour, true},
		{"past the weekly ceiling: six days in", "1000d", 6 * 24 * time.Hour, false},
		{"past the weekly ceiling is judged weekly", "1000d", 7*24*time.Hour + 2*time.Hour, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, store := newTestServer(t)
			member := newStubBackupMember(t, "tok",
				backupEntryAt("backup_auto.dump", "scheduled", tc.age),
			)
			member.interval = tc.interval
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

// TestBackupStaleMessageNamesTheWindow: the alert text states the window the
// member was actually judged against (its interval, floored at a day, plus the
// grace), not a fixed day that a weekly member never had.
func TestBackupStaleMessageNamesTheWindow(t *testing.T) {
	for _, tc := range []struct {
		interval string
		age      time.Duration
		want     string
	}{
		{"", 26 * time.Hour, "m1 has no database backup from the last 25 hours"},
		{"7d", 8 * 24 * time.Hour, "m1 has no database backup from the last 169 hours"},
	} {
		t.Run(tc.want, func(t *testing.T) {
			srv, store := newTestServer(t)
			member := newStubBackupMember(t, "tok",
				backupEntryAt("backup_auto.dump", "scheduled", tc.age),
			)
			member.interval = tc.interval
			if _, err := store.CreateMember(t.Context(), "m1", member.srv.URL, "tok"); err != nil {
				t.Fatalf("CreateMember: %v", err)
			}

			srv.checkMemberBackups(t.Context())

			evs, _, err := store.ListEvents(t.Context(), EventFilter{})
			if err != nil {
				t.Fatalf("list events: %v", err)
			}
			if len(evs) != 1 || evs[0].Message != tc.want {
				t.Fatalf("events = %+v, want one with message %q", evs, tc.want)
			}
		})
	}
}

// A member whose settings cannot be read is not judged on a guessed interval:
// with a weekly schedule, a guess of a day would raise a false alert. Its
// listing alone proves nothing about how often it is meant to back up.
func TestBackupWatchSkipsMemberWhoseSettingsFailToRead(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{"settings error", http.StatusInternalServerError, ""},
		{"settings not JSON", 0, "<html>proxy error</html>"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, store := newTestServer(t)
			member := newStubBackupMember(t, "tok",
				backupEntryAt("backup_auto.dump", "scheduled", 40*time.Hour),
			)
			member.settingsStatus = tc.status
			member.settingsBody = tc.body
			m, err := store.CreateMember(t.Context(), "m1", member.srv.URL, "tok")
			if err != nil {
				t.Fatalf("CreateMember: %v", err)
			}

			srv.checkMemberBackups(t.Context())

			if got := eventTypes(t, store, m.ID); len(got) != 0 {
				t.Fatalf("events = %v, want none: the member was judged without its interval", got)
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
