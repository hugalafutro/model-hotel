package frontdesk

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// stubFleetMember plays a real member for the fleet-status probe: it answers the
// config export GET and the config import (dry-run) POST, so one stub can be a
// primary or a replica. Its export carries an encrypted_key by default so
// MASTER_KEY verification has something to check.
type stubFleetMember struct {
	mu         sync.Mutex
	srv        *httptest.Server
	token      string
	instanceID string
	exportBody string
	importCode int
	importBody string
	gotDryRun  bool
}

const fleetExportWithKey = `{"schema_version":1,"app_version":"v-test","config":{"providers":[{"name":"openai","base_url":"https://o","encrypted_key":"AAAA","key_nonce":"BBBB"}],"virtual_keys":[],"settings":{}}}`

const fleetExportKeyless = `{"schema_version":1,"app_version":"v-test","config":{"providers":[{"name":"openai","base_url":"https://o"}],"virtual_keys":[],"settings":{}}}`

// importOK is a clean dry-run response: schema + MASTER_KEY good, an empty diff.
const importOK = `{"schema_version_ok":true,"master_key_ok":true,"applied":false,"diff":{"providers":{},"virtual_keys":{},"settings":{}}}`

func newStubFleetMember(t *testing.T, token string) *stubFleetMember {
	t.Helper()
	sm := &stubFleetMember{
		token:      token,
		instanceID: fmt.Sprintf("iid-stub-%d", atomic.AddInt32(&memberServerSeq, 1)),
		exportBody: fleetExportWithKey,
		importCode: http.StatusOK,
		importBody: importOK,
	}
	sm.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+sm.token {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		sm.mu.Lock()
		defer sm.mu.Unlock()
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/settings":
			// The lightweight admin-authenticated endpoint the add/edit token probe
			// hits; app_version doubles for the version poller.
			_ = json.NewEncoder(w).Encode(map[string]string{"app_version": "v1"})
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/system"):
			// The fleet-identity self-report the add path reads to confirm the host
			// is not the primary and not an already-registered instance. A faithful
			// member stub answers it: a non-primary box with a unique instance_id.
			_, _ = fmt.Fprintf(w, `{"fleet":{"is_primary":false},"instance_id":%q}`, sm.instanceID)
		case r.Method == http.MethodGet && r.URL.Path == "/api/config/export":
			_, _ = w.Write([]byte(sm.exportBody))
		case r.Method == http.MethodPost && r.URL.Path == "/api/config/import":
			sm.gotDryRun = r.URL.Query().Get("dryRun") != ""
			w.WriteHeader(sm.importCode)
			_, _ = w.Write([]byte(sm.importBody))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(sm.srv.Close)
	return sm
}

func fleetStatusByID(t *testing.T, srv *Server, primaryID string) fleetStatusResponse {
	t.Helper()
	rec := do(t, srv, http.MethodGet, "/api/fleet/status?primary="+primaryID, "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("fleet status = %d (%s)", rec.Code, rec.Body.String())
	}
	var resp fleetStatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return resp
}

// TestFleetStatusClassifies exercises every per-member verdict in one fleet:
// the primary, a fully-converged replica, a config drift, a MASTER_KEY
// mismatch, a version (schema) mismatch, an unreachable box, and a token-less
// member. None of these may fail the request.
func TestFleetStatusClassifies(t *testing.T) {
	srv, store := newTestServer(t)

	primary := newStubFleetMember(t, "ptoken")

	matched := newStubFleetMember(t, "mtoken") // empty diff

	drift := newStubFleetMember(t, "dtoken") // config changes
	drift.importBody = `{"schema_version_ok":true,"master_key_ok":true,"applied":false,"diff":{"providers":{"added":["anthropic"]},"virtual_keys":{},"settings":{}}}`

	badKey := newStubFleetMember(t, "ktoken")
	badKey.importCode = http.StatusConflict
	badKey.importBody = `{"schema_version_ok":true,"master_key_ok":false}`

	badSchema := newStubFleetMember(t, "stoken")
	badSchema.importCode = http.StatusUnprocessableEntity
	badSchema.importBody = `{"schema_version_ok":false,"master_key_ok":false}`

	pm, _ := store.CreateMember(t.Context(), "primary", primary.srv.URL, "ptoken")
	mm, _ := store.CreateMember(t.Context(), "matched", matched.srv.URL, "mtoken")
	dm, _ := store.CreateMember(t.Context(), "drift", drift.srv.URL, "dtoken")
	km, _ := store.CreateMember(t.Context(), "badkey", badKey.srv.URL, "ktoken")
	bm, _ := store.CreateMember(t.Context(), "badschema", badSchema.srv.URL, "stoken")
	um, _ := store.CreateMember(t.Context(), "unreachable", "http://127.0.0.1:9", "utoken")
	nm, _ := store.CreateMember(t.Context(), "no-token", "http://127.0.0.1:10", "")

	resp := fleetStatusByID(t, srv, pm.ID)
	if !resp.PrimaryReachable {
		t.Fatalf("primary not reachable: %q", resp.PrimaryNote)
	}
	// The Done step needs the LB port to tell the operator where to send traffic;
	// an unconfigured server falls back to the documented default.
	if resp.LBPort != defaultLBPort {
		t.Errorf("lb_port = %q, want default %q", resp.LBPort, defaultLBPort)
	}
	byID := map[string]fleetMemberStatus{}
	for _, it := range resp.Members {
		byID[it.MemberID] = it
	}

	if p := byID[pm.ID]; !p.Reachable || p.MasterKeyMatches == nil || !*p.MasterKeyMatches {
		t.Errorf("primary = %+v (want reachable, key match)", p)
	}
	if m := byID[mm.ID]; !m.Reachable || m.MasterKeyMatches == nil || !*m.MasterKeyMatches ||
		m.Added != 0 || m.Updated != 0 || m.Removed != 0 {
		t.Errorf("matched = %+v (want converged, empty diff)", m)
	}
	if d := byID[dm.ID]; !d.Reachable || d.MasterKeyMatches == nil || !*d.MasterKeyMatches || d.Added != 1 {
		t.Errorf("drift = %+v (want reachable + key match + added=1)", d)
	}
	if k := byID[km.ID]; !k.Reachable || k.MasterKeyMatches == nil || *k.MasterKeyMatches || !strings.Contains(k.Note, "MASTER_KEY") {
		t.Errorf("badkey = %+v (want key mismatch false + MASTER_KEY note)", k)
	}
	if b := byID[bm.ID]; !b.Reachable || b.SchemaOK || b.MasterKeyMatches != nil || !strings.Contains(b.Note, "version") {
		t.Errorf("badschema = %+v (want schema false, key nil, version note)", b)
	}
	if u := byID[um.ID]; u.Reachable || u.Note == "" {
		t.Errorf("unreachable = %+v (want not reachable + note)", u)
	}
	if n := byID[nm.ID]; n.HasToken || n.Reachable || !strings.Contains(n.Note, "admin token") {
		t.Errorf("no-token = %+v (want has_token false + note)", n)
	}

	// The probe must never mutate a replica.
	if !matched.gotDryRun || !drift.gotDryRun {
		t.Error("fleet status must call import with dryRun")
	}
}

// TestFleetStatusProbeFailureNotSchemaBlocker: a member whose config import
// probe fails transiently (5xx) is reported as not-converged, not as a schema
// mismatch. The single dry-run probe is both the reachability check and the diff
// source, so an unexpected status reports the real HTTP code rather than a
// spurious "too old, upgrade it" remedy; such a member is excluded from the
// schema blockers and so never blocks the whole fleet on a transient blip.
func TestFleetStatusProbeFailureNotSchemaBlocker(t *testing.T) {
	srv, store := newTestServer(t)
	primary := newStubFleetMember(t, "ptoken")
	flaky := newStubFleetMember(t, "ftoken") // import 5xx
	flaky.importCode = http.StatusInternalServerError
	flaky.importBody = "boom"

	pm, _ := store.CreateMember(t.Context(), "primary", primary.srv.URL, "ptoken")
	fm, _ := store.CreateMember(t.Context(), "flaky", flaky.srv.URL, "ftoken")

	resp := fleetStatusByID(t, srv, pm.ID)
	byID := map[string]fleetMemberStatus{}
	for _, it := range resp.Members {
		byID[it.MemberID] = it
	}
	// Not flagged as a schema/version problem (which would be a false "too old").
	if f := byID[fm.ID]; f.Reachable || strings.Contains(f.Note, "version") {
		t.Errorf("flaky = %+v (want not reachable, no version note)", f)
	}
}

// TestFleetStatusEmptyPrimaryExport: a primary whose config has no providers,
// virtual keys, or settings is reported as a primary-level problem (200 +
// primary_reachable false + explanatory note), not by probing every peer with a
// config the member side refuses, which would paint the whole fleet "offline".
func TestFleetStatusEmptyPrimaryExport(t *testing.T) {
	srv, store := newTestServer(t)
	primary := newStubFleetMember(t, "ptoken")
	primary.exportBody = `{"schema_version":1,"app_version":"v-test","config":{"providers":[],"virtual_keys":[],"settings":{}}}`
	replica := newStubFleetMember(t, "rtoken")

	pm, _ := store.CreateMember(t.Context(), "primary", primary.srv.URL, "ptoken")
	store.CreateMember(t.Context(), "replica", replica.srv.URL, "rtoken") //nolint:errcheck // presence is the point

	resp := fleetStatusByID(t, srv, pm.ID)
	if resp.PrimaryReachable {
		t.Errorf("empty-export primary reported reachable: %+v", resp)
	}
	if !strings.Contains(resp.PrimaryNote, "providers") {
		t.Errorf("primary note = %q, want it to explain the empty config", resp.PrimaryNote)
	}
	if len(resp.Members) != 0 {
		t.Errorf("members = %+v, want none (no peer probed)", resp.Members)
	}
	// The replica must never have been probed with the empty config.
	if replica.gotDryRun {
		t.Error("replica was probed despite an empty primary export")
	}
}

// TestFleetStatusMemberRejectsToken: a reachable member whose import answers with
// 401 (a wrong stored token) is reported with the real cause, not the misleading
// "could not reach this member" reserved for a transport failure.
func TestFleetStatusMemberRejectsToken(t *testing.T) {
	srv, store := newTestServer(t)
	primary := newStubFleetMember(t, "ptoken")
	rejecting := newStubFleetMember(t, "rtoken")
	rejecting.importCode = http.StatusUnauthorized
	rejecting.importBody = ""

	pm, _ := store.CreateMember(t.Context(), "primary", primary.srv.URL, "ptoken")
	rm, _ := store.CreateMember(t.Context(), "rejecting", rejecting.srv.URL, "rtoken")

	resp := fleetStatusByID(t, srv, pm.ID)
	byID := map[string]fleetMemberStatus{}
	for _, it := range resp.Members {
		byID[it.MemberID] = it
	}
	r := byID[rm.ID]
	if r.Reachable {
		t.Errorf("rejecting member reported reachable: %+v", r)
	}
	if strings.Contains(r.Note, "could not reach") {
		t.Errorf("note = %q, want the real 401 cause, not a transport message", r.Note)
	}
	if !strings.Contains(r.Note, "401") || !strings.Contains(r.Note, "admin token") {
		t.Errorf("note = %q, want it to name the HTTP 401 token rejection", r.Note)
	}
}

// TestFleetStatusKeylessFleet: when the primary has no provider keys there is
// nothing to verify, so MASTER_KEY is reported as nil (not a false alarm).
func TestFleetStatusKeylessFleet(t *testing.T) {
	srv, store := newTestServer(t)
	primary := newStubFleetMember(t, "ptoken")
	primary.exportBody = fleetExportKeyless
	replica := newStubFleetMember(t, "rtoken")

	pm, _ := store.CreateMember(t.Context(), "primary", primary.srv.URL, "ptoken")
	rm, _ := store.CreateMember(t.Context(), "replica", replica.srv.URL, "rtoken")

	resp := fleetStatusByID(t, srv, pm.ID)
	byID := map[string]fleetMemberStatus{}
	for _, it := range resp.Members {
		byID[it.MemberID] = it
	}
	if r := byID[rm.ID]; r.MasterKeyMatches != nil || !strings.Contains(r.Note, "verify") {
		t.Errorf("keyless replica = %+v (want master_key nil + 'verify' note)", r)
	}
	if p := byID[pm.ID]; p.MasterKeyMatches != nil {
		t.Errorf("keyless primary master_key = %v, want nil", p.MasterKeyMatches)
	}
}

// TestFleetStatusPrimaryUnusable: a primary Front Desk can't read, or one with
// no stored token, is reported inline (200 + primary_reachable false), never a
// 502. An unknown id is still a real 404.
func TestFleetStatusPrimaryUnusable(t *testing.T) {
	srv, store := newTestServer(t)

	// Reachability failure: primary URL is dead.
	dead, _ := store.CreateMember(t.Context(), "dead", "http://127.0.0.1:9", "tok")
	resp := fleetStatusByID(t, srv, dead.ID)
	if resp.PrimaryReachable || resp.PrimaryNote == "" {
		t.Errorf("dead primary = %+v (want not reachable + note)", resp)
	}

	// No stored token: cannot be a source.
	noTok, _ := store.CreateMember(t.Context(), "notok", "http://127.0.0.1:10", "")
	resp = fleetStatusByID(t, srv, noTok.ID)
	if resp.PrimaryReachable || !strings.Contains(resp.PrimaryNote, "admin token") {
		t.Errorf("token-less primary = %+v (want note about admin token)", resp)
	}

	// Unknown id is a 404.
	rec := do(t, srv, http.MethodGet, "/api/fleet/status?primary=00000000-0000-0000-0000-000000000000", "", true)
	if rec.Code != http.StatusNotFound {
		t.Errorf("unknown primary = %d, want 404", rec.Code)
	}
}
