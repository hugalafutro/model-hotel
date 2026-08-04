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

// stubBackupMember is a fake Model Hotel member exposing the two backup routes
// Front Desk uses: GET /api/backups (the listing, each entry carrying the origin
// the member itself derived) and DELETE /api/backups/{filename}. Origin is set
// per entry by the test, deliberately independent of the filename, so a test can
// model a manual backup whose name happens to contain the word frontdesk.
type stubBackupMember struct {
	token string

	mu         sync.Mutex
	files      []memberBackupEntry
	deleted    []string
	listStatus int // 0 means 200 with the listing
	listBody   string
	delStatus  int // 0 means the real delete (204, or 404 for an unknown file)

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
		switch {
		case r.Method == http.MethodGet && strings.TrimSuffix(r.URL.Path, "/") == "/api/backups":
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
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/api/backups/"):
			name := strings.TrimPrefix(r.URL.Path, "/api/backups/")
			if sm.delStatus != 0 {
				w.WriteHeader(sm.delStatus)
				return
			}
			for i, f := range sm.files {
				if f.Filename == name {
					sm.files = append(sm.files[:i:i], sm.files[i+1:]...)
					sm.deleted = append(sm.deleted, name)
					w.WriteHeader(http.StatusNoContent)
					return
				}
			}
			w.WriteHeader(http.StatusNotFound)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(sm.srv.Close)
	return sm
}

// remaining lists the filenames the member still holds.
func (sm *stubBackupMember) remaining() []string {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	out := make([]string, 0, len(sm.files))
	for _, f := range sm.files {
		out = append(out, f.Filename)
	}
	return out
}

func (sm *stubBackupMember) deletedFiles() []string {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	return append([]string{}, sm.deleted...)
}

// backupEntryAt builds a listing entry with an explicit origin and age.
func backupEntryAt(name, origin string, age time.Duration) memberBackupEntry {
	return memberBackupEntry{
		Filename:  name,
		Origin:    origin,
		CreatedAt: time.Now().Add(-age).UTC().Format(time.RFC3339),
	}
}

// pruneResponse is the decoded body of the fleet prune endpoint.
type pruneResponse struct {
	Deleted int                 `json:"deleted"`
	Failed  int                 `json:"failed"`
	Results []backupPruneResult `json:"results"`
}

func doPrune(t *testing.T, srv *Server, path string) pruneResponse {
	t.Helper()
	rec := do(t, srv, http.MethodPost, path, "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST %s = %d (%s)", path, rec.Code, rec.Body.String())
	}
	var resp pruneResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode prune response: %v (%s)", err, rec.Body.String())
	}
	return resp
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

// countType counts how many of the recorded events carry a given type.
func countType(t *testing.T, store *Store, typ string) int {
	t.Helper()
	n := 0
	for _, got := range eventTypes(t, store, "") {
		if got == typ {
			n++
		}
	}
	return n
}

// TestPruneFrontDeskBackupsDeletesOnlyFrontDeskOrigin: the fleet prune removes
// the dumps Front Desk asked members to take and leaves every other backup in
// place. Manual and scheduled files are the operator's and the member's own
// safety net; the prune must never touch them.
func TestPruneFrontDeskBackupsDeletesOnlyFrontDeskOrigin(t *testing.T) {
	srv, store := newTestServer(t)
	member := newStubBackupMember(t, "tok",
		backupEntryAt("backup_20260101_000000_1_frontdesk.dump", "frontdesk", time.Hour),
		backupEntryAt("backup_20260101_010000_1_manual.dump", "manual", time.Hour),
		backupEntryAt("backup_20260101_020000_1_auto.dump", "scheduled", time.Hour),
		backupEntryAt("backup_20260101_030000_1_frontdesk.dump", "frontdesk", time.Hour),
		backupEntryAt("legacy.dump", "somethingelse", time.Hour),
	)
	if _, err := store.CreateMember(t.Context(), "m1", member.srv.URL, "tok"); err != nil {
		t.Fatalf("CreateMember: %v", err)
	}

	resp := doPrune(t, srv, "/api/fleet/backups/prune-frontdesk")
	if resp.Deleted != 2 || resp.Failed != 0 {
		t.Errorf("fleet totals: deleted=%d failed=%d, want 2/0", resp.Deleted, resp.Failed)
	}
	if len(resp.Results) != 1 || resp.Results[0].Name != "m1" || resp.Results[0].Deleted != 2 {
		t.Fatalf("results = %+v", resp.Results)
	}

	left := member.remaining()
	if len(left) != 3 {
		t.Fatalf("member kept %v, want the manual, scheduled and unrecognised entries", left)
	}
	for _, name := range left {
		if strings.HasSuffix(strings.TrimSuffix(name, ".dump"), "_frontdesk") {
			t.Errorf("a frontdesk-origin backup survived: %s", name)
		}
	}

	// An audit event names what the run did.
	if n := countType(t, store, "backup.pruned"); n != 1 {
		t.Errorf("backup.pruned events = %d, want exactly 1 for the run", n)
	}
}

// TestPruneFrontDeskBackupsMatchesOriginNotFilename is the destructive-mistake
// guard: selection is on the origin the member reports, never on the filename.
// A manual backup an operator named with the word frontdesk in it must survive.
// The fixture is one a real member can actually produce: the member classifies
// by the trailing "_frontdesk" marker, so a name that merely contains the word
// is reported as manual.
func TestPruneFrontDeskBackupsMatchesOriginNotFilename(t *testing.T) {
	srv, store := newTestServer(t)
	member := newStubBackupMember(t, "tok",
		backupEntryAt("before-frontdesk-migration.dump", "manual", time.Hour),
		backupEntryAt("backup_20260101_000000_1_frontdesk.dump", "frontdesk", time.Hour),
	)
	if _, err := store.CreateMember(t.Context(), "m1", member.srv.URL, "tok"); err != nil {
		t.Fatalf("CreateMember: %v", err)
	}

	resp := doPrune(t, srv, "/api/fleet/backups/prune-frontdesk")
	if resp.Deleted != 1 {
		t.Errorf("deleted = %d, want 1", resp.Deleted)
	}
	got := member.remaining()
	if len(got) != 1 || got[0] != "before-frontdesk-migration.dump" {
		t.Errorf("member holds %v; a manual backup named with the word frontdesk was destroyed", got)
	}
}

// TestPruneFrontDeskBackupsReportsTokenlessMember: a member Front Desk holds no
// admin token for cannot be pruned, and the operator is told so rather than
// left to assume the whole fleet was covered.
func TestPruneFrontDeskBackupsReportsTokenlessMember(t *testing.T) {
	srv, store := newTestServer(t)
	member := newStubBackupMember(t, "tok",
		backupEntryAt("backup_20260101_000000_1_frontdesk.dump", "frontdesk", time.Hour),
	)
	if _, err := store.CreateMember(t.Context(), "tokenless", member.srv.URL, ""); err != nil {
		t.Fatalf("CreateMember: %v", err)
	}

	resp := doPrune(t, srv, "/api/fleet/backups/prune-frontdesk")
	if len(resp.Results) != 1 {
		t.Fatalf("results = %+v, want the token-less member reported", resp.Results)
	}
	if resp.Results[0].Name != "tokenless" || resp.Results[0].Error == "" {
		t.Errorf("result = %+v, want a stated reason", resp.Results[0])
	}
	if resp.Deleted != 0 {
		t.Errorf("deleted = %d, want 0: the member was never authenticated to", resp.Deleted)
	}
	if len(member.remaining()) != 1 {
		t.Errorf("member holds %v; it was pruned without a stored token", member.remaining())
	}
}

// TestPruneFrontDeskBackupsReportsPerMemberFailure: a member whose listing
// cannot be read is reported, not silently skipped, and does not stop the
// remaining members being pruned.
func TestPruneFrontDeskBackupsReportsPerMemberFailure(t *testing.T) {
	srv, store := newTestServer(t)
	broken := newStubBackupMember(t, "tok1")
	broken.listStatus = http.StatusInternalServerError
	good := newStubBackupMember(t, "tok2",
		backupEntryAt("backup_20260101_000000_1_frontdesk.dump", "frontdesk", time.Hour),
	)
	if _, err := store.CreateMember(t.Context(), "broken", broken.srv.URL, "tok1"); err != nil {
		t.Fatalf("CreateMember: %v", err)
	}
	if _, err := store.CreateMember(t.Context(), "good", good.srv.URL, "tok2"); err != nil {
		t.Fatalf("CreateMember: %v", err)
	}

	resp := doPrune(t, srv, "/api/fleet/backups/prune-frontdesk")
	if len(resp.Results) != 2 {
		t.Fatalf("results = %+v, want both members reported", resp.Results)
	}
	byName := map[string]backupPruneResult{}
	for _, r := range resp.Results {
		byName[r.Name] = r
	}
	if byName["broken"].Error == "" {
		t.Error("the unreadable member was reported without an error")
	}
	if byName["good"].Deleted != 1 || byName["good"].Error != "" {
		t.Errorf("good member result = %+v; one member's failure aborted the run", byName["good"])
	}
	if resp.Deleted != 1 {
		t.Errorf("fleet deleted = %d, want 1", resp.Deleted)
	}
	if len(good.deletedFiles()) != 1 {
		t.Errorf("good member deletions = %v", good.deletedFiles())
	}
}

// TestPruneFrontDeskBackupsHandlesAHugeListing: the prune is needed most on the
// members that accumulated the most dumps, so the listing read must reach well
// past the limit an ordinary member response gets. A listing far larger than
// maxMemberRespBody is read and pruned in full rather than failing to parse.
func TestPruneFrontDeskBackupsHandlesAHugeListing(t *testing.T) {
	srv, store := newTestServer(t)
	// Comfortably past maxMemberRespBody (1 MiB) at roughly 135 bytes an entry, and
	// past the ~7,600 entries that limit allows.
	const files = 20000
	entries := make([]memberBackupEntry, 0, files)
	for i := range files {
		entries = append(entries, backupEntryAt(
			fmt.Sprintf("backup_20260101_%06d_1_frontdesk.dump", i), "frontdesk", time.Hour))
	}
	member := newStubBackupMember(t, "tok", entries...)
	if _, err := store.CreateMember(t.Context(), "packed", member.srv.URL, "tok"); err != nil {
		t.Fatalf("CreateMember: %v", err)
	}

	resp := doPrune(t, srv, "/api/fleet/backups/prune-frontdesk?dryRun=1")
	if resp.Deleted != files {
		t.Errorf("dry-run counted %d of %d entries; the listing read stopped short", resp.Deleted, files)
	}
	if len(resp.Results) == 1 && resp.Results[0].Error != "" {
		t.Errorf("member reported an error on a large listing: %q", resp.Results[0].Error)
	}
}

// TestPruneFrontDeskBackupsReportsAnUnreadablyLargeListing: past even the
// listing's own limit the body is refused rather than truncated, and the
// operator is told that specifically. A truncated prefix would fail to parse and
// report as a malformed listing, pointing at the wrong problem.
func TestPruneFrontDeskBackupsReportsAnUnreadablyLargeListing(t *testing.T) {
	srv, store := newTestServer(t)
	member := newStubBackupMember(t, "tok")
	// Valid JSON, just past the read limit: the failure must come from the limit,
	// not from the shape of the body.
	member.listBody = "[" + strings.Repeat(`{"filename":"x","created_at":"","origin":"manual"},`,
		(maxMemberBackupListBody/50)+1) + `{"filename":"y","created_at":"","origin":"manual"}]`
	if _, err := store.CreateMember(t.Context(), "huge", member.srv.URL, "tok"); err != nil {
		t.Fatalf("CreateMember: %v", err)
	}

	resp := doPrune(t, srv, "/api/fleet/backups/prune-frontdesk?dryRun=1")
	if len(resp.Results) != 1 {
		t.Fatalf("results = %+v, want the one member reported", resp.Results)
	}
	if got := resp.Results[0].Error; got != "this member holds more backups than Front Desk can list at once" {
		t.Errorf("error = %q, want the too-large reason rather than a generic read failure", got)
	}
}

// TestPruneFrontDeskBackupsCountsFailedDeletes: a member that lists a
// frontdesk-origin file but refuses to delete it is reported as failed, and the
// fleet total does not claim it was removed.
func TestPruneFrontDeskBackupsCountsFailedDeletes(t *testing.T) {
	srv, store := newTestServer(t)
	member := newStubBackupMember(t, "tok",
		backupEntryAt("backup_20260101_000000_1_frontdesk.dump", "frontdesk", time.Hour),
	)
	member.delStatus = http.StatusInternalServerError
	if _, err := store.CreateMember(t.Context(), "m1", member.srv.URL, "tok"); err != nil {
		t.Fatalf("CreateMember: %v", err)
	}

	resp := doPrune(t, srv, "/api/fleet/backups/prune-frontdesk")
	if resp.Deleted != 0 || resp.Failed != 1 {
		t.Errorf("totals: deleted=%d failed=%d, want 0/1", resp.Deleted, resp.Failed)
	}
	if len(resp.Results) != 1 || resp.Results[0].Error == "" {
		t.Errorf("results = %+v, want the failure surfaced", resp.Results)
	}
}

// TestPruneFrontDeskBackupsDryRunDeletesNothing: the preview the confirmation
// step is built on counts what would go without removing anything.
func TestPruneFrontDeskBackupsDryRunDeletesNothing(t *testing.T) {
	srv, store := newTestServer(t)
	member := newStubBackupMember(t, "tok",
		backupEntryAt("backup_20260101_000000_1_frontdesk.dump", "frontdesk", time.Hour),
		backupEntryAt("backup_20260101_010000_1_manual.dump", "manual", time.Hour),
	)
	if _, err := store.CreateMember(t.Context(), "m1", member.srv.URL, "tok"); err != nil {
		t.Fatalf("CreateMember: %v", err)
	}

	resp := doPrune(t, srv, "/api/fleet/backups/prune-frontdesk?dryRun=1")
	if resp.Deleted != 1 {
		t.Errorf("preview count = %d, want 1", resp.Deleted)
	}
	if len(member.remaining()) != 2 {
		t.Errorf("a dry run deleted files: %v", member.remaining())
	}
	if n := countType(t, store, "backup.pruned"); n != 0 {
		t.Errorf("backup.pruned events = %d after a dry run, want 0", n)
	}
}

// TestPruneFrontDeskBackupsNeedsAuth: an unauthenticated caller is refused
// outright.
func TestPruneFrontDeskBackupsNeedsAuth(t *testing.T) {
	srv, _ := newTestServer(t)
	if rec := do(t, srv, http.MethodPost, "/api/fleet/backups/prune-frontdesk", "", false); rec.Code != http.StatusUnauthorized {
		t.Errorf("unauthenticated prune = %d, want 401", rec.Code)
	}
}

// TestPruneFrontDeskBackupsNeedsOperator: the prune is destructive, so a
// read-only (monitor) device token is refused by the role gate even though it
// authenticates fine for reads.
func TestPruneFrontDeskBackupsNeedsOperator(t *testing.T) {
	srv, _ := newTestServer(t)
	monitor, _ := pairDevice(t, srv, RoleMonitor, "watcher")

	rec := doDevice(t, srv, http.MethodPost, "/api/fleet/backups/prune-frontdesk", "", monitor)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "device_role_forbidden") {
		t.Errorf("monitor prune = %d: %s", rec.Code, rec.Body.String())
	}
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
