package frontdesk

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// A manual config sync detaches from the request context on purpose: a client
// with a short HTTP timeout must not be able to abort a fleet-wide run half-way.
// Detached is not the same as unowned, and these tests hold the difference. The
// run reads and writes the store all the way to its last sync stamp, so
// Shutdown, which closes that store, has to know about it.

// TestConfigSyncInFlightAtShutdownNeverStampsAClosedStore is the ordering
// Shutdown owes a manual sync. With the run registered on the server's
// background group, Shutdown ends its lifetime and the drain waits for it, so by
// the time the store closes the run has stopped and recorded nothing. Left
// unregistered, the drain has nothing to wait for: the store closes under a run
// that is still pushing, and the member that answers next is reported as
// "applied but could not record the sync stamp" — a fleet Front Desk changed and
// then forgot.
func TestConfigSyncInFlightAtShutdownNeverStampsAClosedStore(t *testing.T) {
	srv, store := newTestServer(t)
	primary := newStubConfigMember(t, "ptoken")
	replica := newStubConfigMember(t, "rtoken")
	importing, releaseImport := replica.holdRealImport()
	// Released on every exit, failing ones included, so the stub's handler and the
	// sync goroutine are never left parked on a test that has gone.
	defer releaseImport()

	pm, err := store.CreateMember(t.Context(), "primary", primary.srv.URL, "ptoken")
	if err != nil {
		t.Fatalf("create primary: %v", err)
	}
	if _, err := store.CreateMember(t.Context(), "replica", replica.srv.URL, "rtoken"); err != nil {
		t.Fatalf("create replica: %v", err)
	}
	enableAutoSync(t, store, pm.ID)
	alignFleetVersions(t, srv, store, "dev")

	syncDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		syncDone <- do(t, srv, http.MethodPost, "/api/config/sync", `{"primary_id":"`+pm.ID+`"}`, true)
	}()
	<-importing // the run is detached from the request and mid-import

	// t.Context() outlives the test body, so this drain has no deadline to escape
	// through: only the run ending can release it.
	if err := srv.Shutdown(t.Context()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	var rec *httptest.ResponseRecorder
	select {
	case rec = <-syncDone:
	case <-time.After(2 * time.Second):
		t.Fatal("the manual sync was still running after Shutdown closed the store; the drain did not cover it")
	}
	if _, err := store.AutoSyncGen(context.Background()); err == nil {
		t.Error("store still usable after Shutdown; want it closed")
	}

	resp := decodeSyncResponse(t, rec)
	for _, item := range resp.Results {
		if strings.Contains(item.Error, "could not record the sync stamp") {
			t.Errorf("the run wrote a member's config and then failed to stamp it: %+v; it persisted against a store Shutdown had closed", item)
		}
	}
}

// TestConfigSyncCutShortReportsThePartialRun: a run shutdown ended is not a run
// that succeeded. It answers 503 carrying the members it did reach plus a
// message naming how many it never attempted, because the wizard reads a 200 as
// the whole fleet and would toast "1 of 1 synced" over a member Front Desk never
// touched.
func TestConfigSyncCutShortReportsThePartialRun(t *testing.T) {
	srv, store := newTestServer(t)
	primary := newStubConfigMember(t, "ptoken")
	// Both replicas hold their real import, so whichever the run reaches first is
	// the one it is parked on when shutdown lands: the assertion does not depend on
	// the order the store lists them in.
	first := newStubConfigMember(t, "rtoken1")
	second := newStubConfigMember(t, "rtoken2")
	enteredFirst, releaseFirst := first.holdRealImport()
	enteredSecond, releaseSecond := second.holdRealImport()
	defer releaseFirst()
	defer releaseSecond()

	pm, err := store.CreateMember(t.Context(), "primary", primary.srv.URL, "ptoken")
	if err != nil {
		t.Fatalf("create primary: %v", err)
	}
	for i, m := range []*stubConfigMember{first, second} {
		if _, err := store.CreateMember(t.Context(), "replica"+strconv.Itoa(i), m.srv.URL, m.token); err != nil {
			t.Fatalf("create replica: %v", err)
		}
	}
	enableAutoSync(t, store, pm.ID)
	alignFleetVersions(t, srv, store, "dev")

	syncDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		syncDone <- do(t, srv, http.MethodPost, "/api/config/sync", `{"primary_id":"`+pm.ID+`"}`, true)
	}()
	select { // one replica is mid-import; the other has not been reached
	case <-enteredFirst:
	case <-enteredSecond:
	}

	if err := srv.Shutdown(t.Context()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	var rec *httptest.ResponseRecorder
	select {
	case rec = <-syncDone:
	case <-time.After(2 * time.Second):
		t.Fatal("the manual sync was still running after Shutdown closed the store")
	}

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("interrupted sync = %d, want 503; body=%s", rec.Code, rec.Body.String())
	}
	resp := decodeSyncResponse(t, rec)
	if len(resp.Results) != 1 {
		t.Errorf("results = %d (%+v), want the one member the run reached", len(resp.Results), resp.Results)
	}
	if resp.Code != "sync_interrupted" {
		t.Errorf("code = %q, want sync_interrupted", resp.Code)
	}
	if !strings.Contains(resp.Error, "1 member(s) were not attempted") {
		t.Errorf("error = %q, want it to name the one member the run never reached", resp.Error)
	}
}

// syncResponse is the manual sync answer in both its shapes: the results, plus
// the code/error an interrupted run carries alongside them.
type syncResponse struct {
	Results []syncResultItem `json:"results"`
	Code    string           `json:"code"`
	Error   string           `json:"error"`
}

func decodeSyncResponse(t *testing.T, rec *httptest.ResponseRecorder) syncResponse {
	t.Helper()
	var resp syncResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode sync response (%d %s): %v", rec.Code, rec.Body.String(), err)
	}
	return resp
}

// TestConfigSyncRefusedOnceShutdownBegins: an http.Server drain returns on its
// own deadline without stopping the handlers still in flight, so this handler
// can be reached after Shutdown has parked its waiter. Starting a fleet-wide
// write then is refused rather than run unowned: nothing would record what it
// did, and it would be cancelled a moment later anyway. The operator is told, so
// they can run the sync again after the restart instead of reading a 200 for a
// run that never happened.
func TestConfigSyncRefusedOnceShutdownBegins(t *testing.T) {
	srv, store := newTestServer(t)
	primary := newStubConfigMember(t, "ptoken")
	replica := newStubConfigMember(t, "rtoken")

	pm, err := store.CreateMember(t.Context(), "primary", primary.srv.URL, "ptoken")
	if err != nil {
		t.Fatalf("create primary: %v", err)
	}
	if _, err := store.CreateMember(t.Context(), "replica", replica.srv.URL, "rtoken"); err != nil {
		t.Fatalf("create replica: %v", err)
	}
	alignFleetVersions(t, srv, store, "dev")

	if err := srv.Shutdown(t.Context()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	rec := do(t, srv, http.MethodPost, "/api/config/sync", `{"primary_id":"`+pm.ID+`"}`, true)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("sync during shutdown = %d (%s), want 503", rec.Code, strings.TrimSpace(rec.Body.String()))
	}
	if primary.gotImport || replica.gotImport {
		t.Error("a refused sync still pushed to a member")
	}
}
