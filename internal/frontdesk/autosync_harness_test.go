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

	"github.com/hugalafutro/model-hotel/internal/events"
)

// The auto-sync suite's shared harness: the stub member every test drives the
// loop against, the fleet builders, and the small state readers.

// stubAutoMember plays a member for the auto-sync loop: it answers the config
// version GET (the drift signal), the export GET, and the dry-run and real config
// imports. Each is independently configurable so a single stub can be a primary or
// a replica in any disposition.
//
// It also answers POST /api/backups and counts the calls. No sync path may call
// that endpoint: members back themselves up on their own schedule. Serving it (and
// asserting the count is zero) catches a reintroduced pre-sync backup as a counted
// call rather than as a 404 that some other assertion happens to notice.
type stubAutoMember struct {
	mu          sync.Mutex
	srv         *httptest.Server
	token       string
	instanceID  string
	versionHash string
	// appliedHash, when set, is the config-version hash this member starts
	// reporting once it accepts a real import: a member that applied the primary's
	// config hashes identically to it, which is what lets the next pass skip it. An
	// incomplete import never adopts it, since the member did not build everything.
	appliedHash string
	versionCode int    // status for the version GET (default 200)
	versionRaw  string // raw version body; overrides the {"version":...} JSON when set
	// versionSections, when set, rides in the version response as the per-section
	// hash map a current member serves; nil models an older member whose response
	// carries only the overall hash.
	versionSections map[string]string
	// versionDelay, when set, holds the version GET response for that long before
	// answering. It stands in for the envelope-build-and-hash cost the real
	// endpoint pays, to prove which client (probe vs. read) the caller used.
	versionDelay time.Duration
	exportBody   string
	exportCode   int    // status for the export GET (default 200)
	dryDiff      string // diff object returned on a dry-run import
	importCode   int    // status for the dry-run import (default 200)
	importBody   string // full dry-run import body; overrides dryDiff when set
	// realImportBody is the full body the real (non-dry-run) import answers with,
	// overriding the default success response. It is how a test pins exactly what
	// the member claims about its own apply: an explicit "incomplete":false, or an
	// older member's response that omits the field entirely.
	realImportBody string
	// realImportCode, when set, is the status the real import answers with even
	// though the member applies the config anyway: what a reverse proxy answering
	// 502/504 mid-import looks like from Front Desk, with the member finishing
	// the apply behind it.
	realImportCode int
	gotBackup      bool
	backups        int // how many backups this member was asked to take; must stay 0
	dryRuns        int // how many dry-run imports this member was asked for
	// versionReads counts the config-version GETs this member answered. It is the
	// direct evidence of whether a pass measured this member at all, which is what
	// separates a convergence pass from a tick that skipped the fleet entirely.
	versionReads int
	// exports counts the config-export GETs this member answered. On the primary
	// it is the cost a settled fleet does not pay: the export is read only once a
	// member is found that needs it.
	exports      int
	gotRealSync  bool
	realSyncs    int    // how many real (non-dry-run) imports this member accepted
	gotSourceGen string // X-Fleet-Source-Gen seen on the last real (non-dry-run) import
	staleImport  bool   // when true, the real import answers with the commit-fence "stale" response
	// incompleteImport makes the real import answer applied-but-incomplete: the
	// core config committed, one custom failover group could not be built.
	incompleteImport bool
	// onDryRun fires inside the dry-run import handler, to simulate a rearm landing
	// in the window between the (slow) dry-run and the final staleness gate.
	onDryRun func()
	// onImport fires inside the real (non-dry-run) import handler. It receives the
	// request context and returns whether the import should be recorded as applied;
	// returning false models the import being cancelled in flight before it commits.
	onImport func(reqCtx context.Context) (commit bool)
}

func newStubAutoMember(t *testing.T, token string) *stubAutoMember {
	t.Helper()
	sm := &stubAutoMember{
		token:       token,
		instanceID:  fmt.Sprintf("iid-auto-%d", memberServerSeq.Add(1)),
		versionHash: "hash-A",
		exportBody:  fleetExportWithKey,
		dryDiff:     `{"providers":{},"virtual_keys":{},"settings":{}}`, // converged
		versionCode: http.StatusOK,
		exportCode:  http.StatusOK,
		importCode:  http.StatusOK,
	}
	sm.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+sm.token {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		sm.mu.Lock()
		defer sm.mu.Unlock()
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/config/version":
			sm.versionReads++
			if sm.versionDelay > 0 {
				time.Sleep(sm.versionDelay)
			}
			w.WriteHeader(sm.versionCode)
			if sm.versionRaw != "" {
				_, _ = w.Write([]byte(sm.versionRaw))
				return
			}
			if sm.versionSections != nil {
				_ = json.NewEncoder(w).Encode(map[string]any{"version": sm.versionHash, "sections": sm.versionSections})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"version": sm.versionHash})
		case r.Method == http.MethodGet && r.URL.Path == "/api/config/export":
			sm.exports++
			w.WriteHeader(sm.exportCode)
			_, _ = w.Write([]byte(sm.exportBody))
		case r.Method == http.MethodPost && r.URL.Path == "/api/config/import":
			if r.URL.Query().Get("dryRun") != "" {
				sm.dryRuns++
				if sm.onDryRun != nil {
					sm.onDryRun() // simulate a rearm/repoint landing after the dry-run, before the import
				}
				w.WriteHeader(sm.importCode)
				if sm.importBody != "" {
					_, _ = w.Write([]byte(sm.importBody))
					return
				}
				_, _ = w.Write([]byte(`{"schema_version_ok":true,"master_key_ok":true,"applied":false,"diff":` + sm.dryDiff + `}`))
				return
			}
			sm.gotSourceGen = r.Header.Get(fleetSourceGenHeader)
			if sm.staleImport {
				// Simulate the member's commit fence refusing a stale, out-of-order push.
				_, _ = w.Write([]byte(`{"schema_version_ok":true,"master_key_ok":true,"applied":false,"stale":true,"diff":` + sm.dryDiff + `}`))
				return
			}
			if sm.onImport != nil && !sm.onImport(r.Context()) {
				return // import cancelled in flight before commit: record nothing
			}
			sm.gotRealSync = true
			sm.realSyncs++
			if sm.incompleteImport {
				_, _ = w.Write([]byte(`{"schema_version_ok":true,"master_key_ok":true,"applied":true,` +
					`"incomplete":true,"unapplied":["ds4flash"],"diff":` + sm.dryDiff + `}`))
				return
			}
			if sm.appliedHash != "" {
				sm.versionHash = sm.appliedHash // the member now holds the primary's config
			}
			if sm.realImportCode != 0 {
				// The apply above still happened; only the answer is lost.
				w.WriteHeader(sm.realImportCode)
				return
			}
			if sm.realImportBody != "" {
				_, _ = w.Write([]byte(sm.realImportBody))
				return
			}
			_, _ = w.Write([]byte(`{"schema_version_ok":true,"master_key_ok":true,"applied":true,"diff":` + sm.dryDiff + `}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/settings":
			// The token probe (createMember/patchMember) hits this; 200 = accepted.
			_, _ = w.Write([]byte(`{}`))
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/system"):
			// The fleet-identity self-report the add path reads: a faithful member
			// stub answers a non-primary box with a unique instance_id.
			_, _ = w.Write([]byte(`{"fleet":{"is_primary":false},"instance_id":"` + sm.instanceID + `"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/backups":
			// No sync path may reach here; the counters exist to prove it.
			sm.gotBackup = true
			sm.backups++
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"filename":"backup_x_frontdesk.dump"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(sm.srv.Close)
	return sm
}

func (sm *stubAutoMember) didBackup() bool { sm.mu.Lock(); defer sm.mu.Unlock(); return sm.gotBackup }
func (sm *stubAutoMember) didRealSync() bool {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	return sm.gotRealSync
}
func (sm *stubAutoMember) backupCount() int {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	return sm.backups
}

func (sm *stubAutoMember) dryRunCount() int {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	return sm.dryRuns
}

func (sm *stubAutoMember) exportCount() int {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	return sm.exports
}

func (sm *stubAutoMember) versionReadCount() int {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	return sm.versionReads
}

func (sm *stubAutoMember) realSyncCount() int {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	return sm.realSyncs
}

func (sm *stubAutoMember) sourceGen() string {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	return sm.gotSourceGen
}

// setVersionHash changes the hash this member serves, standing in for the member
// finally holding the primary's config (or drifting away from it) between passes.
func (sm *stubAutoMember) setVersionHash(h string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.versionHash = h
}

// setAppliedHash changes what the member ends up holding after an import. Clearing
// it between passes turns a member that adopted the primary's config into one that
// commits every import and never matches.
func (sm *stubAutoMember) setAppliedHash(h string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.appliedHash = h
}

const driftDiff = `{"providers":{"added":["anthropic"]},"virtual_keys":{},"settings":{}}`

// enableAutoSync turns auto-sync on and points it at primaryID.
func enableAutoSync(t *testing.T, store *Store, primaryID string) {
	t.Helper()
	if err := store.SetAutoSync(t.Context(), true, primaryID); err != nil {
		t.Fatalf("SetAutoSync: %v", err)
	}
}

// memberVerified reports whether a pass has measured this member holding the
// primary's config. It reads the verified-in-sync heartbeat, which only a hash
// match moves, so it is the observable that says "this member was measured as
// converged" rather than "this member was written to".
func memberVerified(s *Server, memberID string) bool {
	return s.poller.Snapshot()[memberID].AutoSyncVerifiedAt != nil
}

// memberDiverged reports whether the member is currently flagged as not holding
// the primary's config: the state the amber fleet badge and the
// config.sync_incomplete alert read.
func memberDiverged(s *Server, memberID string) bool {
	s.syncIncompleteMu.Lock()
	defer s.syncIncompleteMu.Unlock()
	return s.syncIncomplete[memberID].diverged
}

// setMemberVersion seeds the poller's last-polled app version for a member, so
// tests can align (or skew) the fleet against the auto-sync version gate
// without a live settings probe.
func setMemberVersion(srv *Server, memberID, version string) {
	srv.poller.mu.Lock()
	st := srv.poller.statuses[memberID]
	st.Version = version
	srv.poller.statuses[memberID] = st
	srv.poller.mu.Unlock()
}

// setMemberBuild seeds both halves of a member's polled build, for the fleet the
// version alone cannot describe: every self-built image reports "dev", so the
// commit is the only field that distinguishes one build from another.
func setMemberBuild(srv *Server, memberID, version, commit string) {
	srv.poller.mu.Lock()
	st := srv.poller.statuses[memberID]
	st.Version = version
	st.Commit = commit
	srv.poller.statuses[memberID] = st
	srv.poller.mu.Unlock()
}

// alignFleetVersions stamps every current member's polled app version to ver,
// so the auto-sync version gate sees an aligned fleet and the push paths under
// test are actually reached (the gate fails closed on unknown versions).
func alignFleetVersions(t *testing.T, srv *Server, store *Store, ver string) {
	t.Helper()
	members, err := store.ListMembers(t.Context())
	if err != nil {
		t.Fatalf("ListMembers: %v", err)
	}
	for _, m := range members {
		setMemberVersion(srv, m.ID, ver)
	}
}

// sawSyncFailed drains the bus channel and reports whether a config.sync_failed
// event was published.
func sawSyncFailed(ch chan events.Event) bool {
	for {
		select {
		case ev := <-ch:
			if ev.Type == "config.sync_failed" {
				return true
			}
		default:
			return false
		}
	}
}

// countEventsOfType returns how many stored events of the given type the fleet
// has, so an edge-triggered emission can be asserted across repeated passes.
func countEventsOfType(t *testing.T, store *Store, typ string) int {
	t.Helper()
	evs, _, err := store.ListEvents(t.Context(), EventFilter{Type: typ})
	if err != nil {
		t.Fatalf("ListEvents(%s): %v", typ, err)
	}
	return len(evs)
}

// incompleteFleet is a two-member fleet whose replica commits every import but
// never builds its custom failover group, so the fleet can never converge. The
// stubs count the real imports each pass costs.
type incompleteFleet struct {
	srv      *Server
	store    *Store
	primary  *stubAutoMember
	replica  *stubAutoMember
	replicaM *Member
}

// verified reports whether a pass has measured the replica holding the primary's
// config, the per-member record of convergence.
func (f *incompleteFleet) verified() bool {
	return memberVerified(f.srv, f.replicaM.ID)
}

func newIncompleteFleet(t *testing.T) *incompleteFleet {
	t.Helper()
	srv, store := newTestServer(t)
	primary := newStubAutoMember(t, "ptoken")
	primary.versionHash = "hash-B" // changed vs the recorded last hash
	replica := newStubAutoMember(t, "rtoken")
	replica.dryDiff = driftDiff // presence-based diffs are never zero for a real member
	replica.incompleteImport = true

	pm, _ := store.CreateMember(t.Context(), "primary", primary.srv.URL, "ptoken")
	rm, _ := store.CreateMember(t.Context(), "replica", replica.srv.URL, "rtoken")
	enableAutoSync(t, store, pm.ID)
	alignFleetVersions(t, srv, store, "dev")
	return &incompleteFleet{srv: srv, store: store, primary: primary, replica: replica, replicaM: rm}
}

// settledTick runs one convergence pass the way the loop does once the primary's
// hash has settled (prev equals the hash the primary reports), so the pass takes
// the converge path rather than the coalescing branch that clears retry timers.
func (f *incompleteFleet) settledTick(t *testing.T) {
	t.Helper()
	f.srv.autoSyncOnce(t.Context(), f.primary.versionHash)
}

// backdateIncompleteRetry moves a member's last incomplete retry attempt d into
// the past, so the next pass sees the rate-limit window as expired.
func backdateIncompleteRetry(t *testing.T, s *Server, memberID string, d time.Duration) {
	t.Helper()
	s.syncIncompleteMu.Lock()
	defer s.syncIncompleteMu.Unlock()
	st, ok := s.syncIncomplete[memberID]
	if !ok {
		t.Fatalf("member %s is not marked incomplete", memberID)
	}
	st.lastAttempt = st.lastAttempt.Add(-d)
	s.syncIncomplete[memberID] = st
}

// hashFleet is a two-member fleet for the convergence-by-hash tests: a primary
// reporting hash-B, one replica the caller configures, and auto-sync enabled at a
// stale hash-A so every settled tick runs a convergence pass.
type hashFleet struct {
	srv      *Server
	store    *Store
	primary  *stubAutoMember
	replica  *stubAutoMember
	replicaM *Member
}

// newHashFleet builds that fleet, running setup on the replica stub before it is
// registered so the member is in its intended disposition from the first request.
func newHashFleet(t *testing.T, setup func(replica *stubAutoMember)) *hashFleet {
	t.Helper()
	srv, store := newTestServer(t)
	primary := newStubAutoMember(t, "ptoken")
	primary.versionHash = "hash-B"
	replica := newStubAutoMember(t, "rtoken")
	setup(replica)

	pm, err := store.CreateMember(t.Context(), "primary", primary.srv.URL, "ptoken")
	if err != nil {
		t.Fatalf("create primary: %v", err)
	}
	rm, err := store.CreateMember(t.Context(), "replica", replica.srv.URL, "rtoken")
	if err != nil {
		t.Fatalf("create replica: %v", err)
	}
	enableAutoSync(t, store, pm.ID)
	alignFleetVersions(t, srv, store, "dev")
	return &hashFleet{srv: srv, store: store, primary: primary, replica: replica, replicaM: rm}
}

// tick runs one settled convergence pass, the way the loop does once the primary's
// hash has stopped moving.
func (f *hashFleet) tick(t *testing.T) {
	t.Helper()
	f.srv.autoSyncOnce(t.Context(), "hash-B")
}

// verified reports whether a pass has measured the replica holding the primary's
// config. It reads the verified-in-sync heartbeat, which only a hash match moves,
// and is the per-member record that replaced the fleet-wide applied-hash marker.
func (f *hashFleet) verified() bool {
	return memberVerified(f.srv, f.replicaM.ID)
}

// partialImportBody is a member's answer to a real import that built every group
// it was sent but built one of them short: it resolved fewer entries for
// testgroup than the primary has. incomplete stays absent because nothing failed
// here; the member simply holds fewer models.
const partialImportBody = `{"schema_version_ok":true,"master_key_ok":true,"applied":true,` +
	`"partial":["testgroup"],"diff":{}}`

// divergenceCapNames returns n distinct group names built from prefix, numbered
// from 1, for exercising divergenceMessage's per-clause name cap. The numbering is
// deterministic so the expected message strings in the cap tests can be written as
// literals.
func divergenceCapNames(prefix string, n int) []string {
	names := make([]string, n)
	for i := range names {
		names[i] = fmt.Sprintf("%s%d", prefix, i+1)
	}
	return names
}

// seedUnreadableSince backdates a member's unreadable-hash clock, so a test can
// reach the far side of unreadableHashThreshold without waiting it out. It seeds
// the same field the production path sets on its first failed read.
func seedUnreadableSince(srv *Server, memberID string, since time.Time) {
	srv.syncIncompleteMu.Lock()
	defer srv.syncIncompleteMu.Unlock()
	st := srv.syncIncomplete[memberID]
	st.unreadableSince = since
	srv.syncIncomplete[memberID] = st
}

// unreadableSince reads a member's unreadable-hash clock.
func unreadableSince(srv *Server, memberID string) time.Time {
	srv.syncIncompleteMu.Lock()
	defer srv.syncIncompleteMu.Unlock()
	return srv.syncIncomplete[memberID].unreadableSince
}
