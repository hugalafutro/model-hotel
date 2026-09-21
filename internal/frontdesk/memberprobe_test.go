package frontdesk

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// These tests cover the add/edit-time admin-token probe: a token the member
// positively refuses (401) blocks the save, an unreachable member saves with a
// warning, and a good token saves cleanly. They reuse the fleet stub member,
// which answers /api/settings (the probe endpoint) only when the Bearer matches.

func createMemberJSON(t *testing.T, srv *Server, name, url, token string) (int, memberResponse) {
	t.Helper()
	body := `{"name":"` + name + `","url":"` + url + `","token":"` + token + `"}`
	rec := do(t, srv, http.MethodPost, "/api/members", body, true)
	var resp memberResponse
	if rec.Body.Len() > 0 && strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	}
	return rec.Code, resp
}

func TestCreateMemberAcceptsGoodToken(t *testing.T) {
	srv, store := newTestServer(t)
	stub := newStubFleetMember(t, "good")

	code, resp := createMemberJSON(t, srv, "m1", stub.srv.URL, "good")
	if code != http.StatusCreated {
		t.Fatalf("create = %d, want 201", code)
	}
	if resp.TokenWarning != "" {
		t.Errorf("unexpected token_warning: %q", resp.TokenWarning)
	}
	members, _ := store.ListMembers(t.Context())
	if len(members) != 1 {
		t.Fatalf("members = %d, want 1", len(members))
	}
}

func TestCreateMemberRejectsRefusedToken(t *testing.T) {
	srv, store := newTestServer(t)
	stub := newStubFleetMember(t, "right")

	code, _ := createMemberJSON(t, srv, "m1", stub.srv.URL, "wrong")
	if code != http.StatusBadRequest {
		t.Fatalf("create with wrong token = %d, want 400", code)
	}
	// The bad add must be rolled back, not left half-created.
	members, _ := store.ListMembers(t.Context())
	if len(members) != 0 {
		t.Fatalf("members = %d after rejected add, want 0 (rollback)", len(members))
	}
}

// The host is verified before any row exists: while the probes run, the roster
// does not count the candidate (a row that exists that long would let a
// concurrent removal of another member pass the fleet-size floor), and a
// rejected add never had anything to roll back.
func TestCreateMemberInsertsOnlyAfterVerification(t *testing.T) {
	srv, store := newTestServer(t)
	seenDuringProbe := -1
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		members, _ := store.ListMembers(r.Context())
		seenDuringProbe = len(members)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer stub.Close()

	code, _ := createMemberJSON(t, srv, "m1", stub.URL, "wrong")
	if code != http.StatusBadRequest {
		t.Fatalf("create with wrong token = %d, want 400", code)
	}
	if seenDuringProbe != 0 {
		t.Errorf("members during the probe = %d, want 0 (nothing inserted before verification)", seenDuringProbe)
	}
	if members, _ := store.ListMembers(t.Context()); len(members) != 0 {
		t.Errorf("members after the rejected add = %d, want 0", len(members))
	}
}

// The token verifies but the host's identity (/api/system) does not answer:
// the add is refused rather than admitted without a dedup key, and nothing is
// stored. A name the validator rejects fails before any probe.
func TestCreateMemberRefusesUnverifiedIdentityAndBadName(t *testing.T) {
	srv, store := newTestServer(t)
	probes := 0
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		probes++
		if r.URL.Path == "/api/settings" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{}`))
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer stub.Close()

	rec := do(t, srv, http.MethodPost, "/api/members", `{"name":"m1","url":"`+stub.URL+`","token":"tok"}`, true)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "identity_unverified") {
		t.Fatalf("identity failure = %d %s, want 400 identity_unverified", rec.Code, rec.Body.String())
	}
	if members, _ := store.ListMembers(t.Context()); len(members) != 0 {
		t.Errorf("members = %d after a refused add, want 0", len(members))
	}

	probes = 0
	if code, _ := createMemberJSON(t, srv, "   ", stub.URL, "tok"); code != http.StatusBadRequest {
		t.Fatalf("blank name = %d, want 400", code)
	}
	if probes != 0 {
		t.Errorf("a rejected name still probed the host %d times", probes)
	}
}

// A URL that is already a member's is refused before the host is probed.
func TestCreateMemberDuplicateURLRefusedBeforeProbing(t *testing.T) {
	srv, store := newTestServer(t)
	hits := 0
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer stub.Close()
	if code, _ := createMemberJSON(t, srv, "m1", stub.URL, "good"); code != http.StatusCreated {
		t.Fatalf("first create = %d, want 201", code)
	}
	hits = 0
	code, _ := createMemberJSON(t, srv, "m2", stub.URL, "anything")
	if code != http.StatusBadRequest {
		t.Fatalf("duplicate create = %d, want 400", code)
	}
	if hits != 0 {
		t.Errorf("the duplicate add probed the host %d times, want 0", hits)
	}
	if members, _ := store.ListMembers(t.Context()); len(members) != 1 {
		t.Errorf("members = %d, want 1", len(members))
	}
}

// Two adds of the same instance under different URLs that race past the
// pre-insert scan: the unique index on instance_id refuses the second insert,
// so one row remains and the loser sees already_member.
func TestCreateMemberRaceOnTheSameInstanceKeepsOneRow(t *testing.T) {
	srv, store := newTestServer(t)
	// Both stubs report the same instance id; the first add's /api/system read
	// blocks until the second add has inserted, so both pass the pre-insert scan.
	firstIdentityRead := make(chan struct{})
	secondInserted := make(chan struct{})
	system := `{"is_primary":false,"instance_id":"inst-shared"}`
	mk := func(gate bool) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if strings.HasPrefix(r.URL.Path, "/api/system") {
				if gate {
					close(firstIdentityRead)
					<-secondInserted
				}
				_, _ = w.Write([]byte(system))
				return
			}
			_, _ = w.Write([]byte(`{}`))
		}))
	}
	first, second := mk(true), mk(false)
	defer first.Close()
	defer second.Close()

	firstCode := make(chan int, 1)
	go func() {
		code, _ := createMemberJSON(t, srv, "m1", first.URL, "tok")
		firstCode <- code
	}()
	<-firstIdentityRead
	secondStatus, _ := createMemberJSON(t, srv, "m2", second.URL, "tok")
	close(secondInserted)
	if secondStatus != http.StatusCreated {
		t.Fatalf("second add = %d, want 201 (it landed first)", secondStatus)
	}
	if code := <-firstCode; code != http.StatusConflict {
		t.Errorf("first add = %d, want 409 already_member", code)
	}
	members, _ := store.ListMembers(t.Context())
	if len(members) != 1 || members[0].Name != "m2" {
		t.Errorf("members = %v, want only m2", members)
	}
}

func TestCreateMemberRejectsUnreachable(t *testing.T) {
	srv, store := newTestServer(t)

	// A dead port: an add now requires a positive reply, so an unreachable host is
	// rejected outright (not saved with a warning) and nothing is persisted.
	code, _ := createMemberJSON(t, srv, "m1", "http://127.0.0.1:9", "tok")
	if code != http.StatusBadRequest {
		t.Fatalf("create unreachable = %d, want 400", code)
	}
	if members, _ := store.ListMembers(t.Context()); len(members) != 0 {
		t.Errorf("members = %d after rejected add, want 0", len(members))
	}
}

func TestCreateMemberRequiresToken(t *testing.T) {
	srv, store := newTestServer(t)
	// No token: Front Desk cannot verify the host's identity or fleet role, so the
	// add is refused before anything is persisted.
	code, _ := createMemberJSON(t, srv, "m1", "http://127.0.0.1:9", "")
	if code != http.StatusBadRequest {
		t.Fatalf("create without token = %d, want 400", code)
	}
	if members, _ := store.ListMembers(t.Context()); len(members) != 0 {
		t.Errorf("members = %d after tokenless add, want 0", len(members))
	}
}

func TestCreateMemberRejectsSelfReportedPrimary(t *testing.T) {
	srv, store := newTestServer(t)
	// The candidate answers the token probe AND self-reports is_primary=true, i.e.
	// it is the fleet primary reached under a different URL. Adding it as a member
	// is refused (409) so the source of truth is never duplicated into the pool.
	host := systemMemberServer(t, true)

	code, _ := createMemberJSON(t, srv, "hotel-1-lan", host.URL, "tok")
	if code != http.StatusConflict {
		t.Fatalf("create self-reported primary = %d, want 409", code)
	}
	if members, _ := store.ListMembers(t.Context()); len(members) != 0 {
		t.Errorf("members = %d after rejected primary add, want 0", len(members))
	}
}

func TestCreateMemberRejectsAlreadyMember(t *testing.T) {
	srv, store := newTestServer(t)
	// First host: a normal member, verified and stored with its instance_id.
	host := systemMemberServerID(t, false, "iid-shared")
	if code, _ := createMemberJSON(t, srv, "hotel-1", host.URL, "tok"); code != http.StatusCreated {
		t.Fatalf("first add = %d, want 201", code)
	}
	members, _ := store.ListMembers(t.Context())
	if len(members) != 1 || members[0].InstanceID != "iid-shared" {
		t.Fatalf("first member instance_id = %+v, want iid-shared stored", members)
	}

	// Second "host" at a different URL but the SAME instance_id: the same physical
	// instance reached under another address. The add is refused.
	sameHost := systemMemberServerID(t, false, "iid-shared")
	code, _ := createMemberJSON(t, srv, "hotel-1-lan", sameHost.URL, "tok")
	if code != http.StatusConflict {
		t.Fatalf("duplicate-instance add = %d, want 409", code)
	}
	if m, _ := store.ListMembers(t.Context()); len(m) != 1 {
		t.Errorf("members = %d after rejected duplicate, want 1", len(m))
	}
}

func TestCreateMemberBackfillsAndDedupsPreexisting(t *testing.T) {
	srv, store := newTestServer(t)
	// A member added before instance identity existed: stored with an empty
	// instance_id (simulated by creating it directly in the store).
	host := systemMemberServerID(t, false, "iid-old")
	old, err := store.CreateMember(t.Context(), "old", host.URL, "tok")
	if err != nil {
		t.Fatalf("seed old member: %v", err)
	}
	if old.InstanceID != "" {
		t.Fatalf("precondition: old.InstanceID = %q, want empty", old.InstanceID)
	}

	// Adding the same instance under a new URL must be caught by backfilling the
	// old member's identity during the dedup check.
	sameHost := systemMemberServerID(t, false, "iid-old")
	code, _ := createMemberJSON(t, srv, "old-lan", sameHost.URL, "tok")
	if code != http.StatusConflict {
		t.Fatalf("duplicate against pre-existing = %d, want 409", code)
	}
	// The dedup pass also backfilled the old member's instance_id.
	got, _ := store.GetMember(t.Context(), old.ID)
	if got.InstanceID != "iid-old" {
		t.Errorf("old member instance_id = %q after dedup, want backfilled iid-old", got.InstanceID)
	}
}

func TestPatchMemberRejectsRefusedToken(t *testing.T) {
	srv, store := newTestServer(t)
	stub := newStubFleetMember(t, "right")
	m, _ := store.CreateMember(t.Context(), "m1", stub.srv.URL, "right")

	rec := do(t, srv, http.MethodPatch, "/api/members/"+m.ID, `{"token":"wrong"}`, true)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("patch with wrong token = %d, want 400", rec.Code)
	}
	// The refused token must not have replaced the working one.
	tok, ok, _ := store.MemberToken(t.Context(), m.ID)
	if !ok || tok != "right" {
		t.Errorf("stored token = %q (ok=%v), want unchanged %q", tok, ok, "right")
	}
}

func TestPatchMemberClearingTokenSkipsProbe(t *testing.T) {
	srv, store := newTestServer(t)
	stub := newStubFleetMember(t, "right")
	m, _ := store.CreateMember(t.Context(), "m1", stub.srv.URL, "right")

	rec := do(t, srv, http.MethodPatch, "/api/members/"+m.ID, `{"token":""}`, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("clear token = %d, want 200", rec.Code)
	}
	if _, ok, _ := store.MemberToken(t.Context(), m.ID); ok {
		t.Error("token should be cleared")
	}
}

// A member from before the identity index whose verification learns an
// identity another member already holds is a duplicate row: the backfill
// reports it (no silent failure), the roster is left as configured, and the
// add that triggered the scan still goes through.
func TestCreateMemberBackfillNamesALegacyDuplicate(t *testing.T) {
	srv, store := newTestServer(t)
	ctx := t.Context()
	report := func(instance string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if strings.HasPrefix(r.URL.Path, "/api/system") {
				_, _ = w.Write([]byte(`{"is_primary":false,"instance_id":"` + instance + `"}`))
				return
			}
			_, _ = w.Write([]byte(`{}`))
		}))
	}
	held, legacy, fresh := report("inst-1"), report("inst-1"), report("inst-3")
	defer held.Close()
	defer legacy.Close()
	defer fresh.Close()
	if _, err := store.CreateVerifiedMember(ctx, "held", held.URL, "tok", "inst-1"); err != nil {
		t.Fatalf("create held: %v", err)
	}
	// The legacy row: same host, no identity recorded (the index migration
	// cleared it), token stored so the scan probes it.
	dup, err := store.CreateMember(ctx, "legacy", legacy.URL, "tok")
	if err != nil {
		t.Fatalf("create legacy: %v", err)
	}
	if code, _ := createMemberJSON(t, srv, "fresh", fresh.URL, "tok"); code != http.StatusCreated {
		t.Fatalf("add of an unrelated host = %d, want 201", code)
	}
	after, err := store.GetMember(ctx, dup.ID)
	if err != nil {
		t.Fatalf("legacy row: %v", err)
	}
	if after.InstanceID != "" {
		t.Errorf("legacy row's instance_id = %q, want empty (the identity is held by another row)", after.InstanceID)
	}
	if members, _ := store.ListMembers(ctx); len(members) != 3 {
		t.Errorf("members = %d, want 3 (nothing removed or drained)", len(members))
	}
}
