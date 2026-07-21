package frontdesk

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// stubQuotaMember plays a member for the quota-distribution loop: it serves the
// fleet quota export (GET) and records the fleet quota pushes it receives (POST),
// both fleet-authed by a bearer token, mirroring the real member endpoints.
type stubQuotaMember struct {
	mu         sync.Mutex
	srv        *httptest.Server
	token      string
	exportBody string
	posted     [][]byte
}

func newStubQuotaMember(t *testing.T, token, exportBody string) *stubQuotaMember {
	t.Helper()
	sm := &stubQuotaMember{token: token, exportBody: exportBody}
	sm.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+sm.token {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		sm.mu.Lock()
		defer sm.mu.Unlock()
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/config/quota-snapshots":
			_, _ = w.Write([]byte(sm.exportBody))
		case r.Method == http.MethodPost && r.URL.Path == "/api/config/quota-snapshots":
			b, _ := io.ReadAll(r.Body)
			sm.posted = append(sm.posted, b)
			_, _ = w.Write([]byte(`{"applied":1,"skipped":0}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(sm.srv.Close)
	return sm
}

func (sm *stubQuotaMember) postedBodies() [][]byte {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	return append([][]byte(nil), sm.posted...)
}

// TestDistributeQuotaOnce: Front Desk fetches the designated primary's quota
// snapshots and posts them to every other member, never back to the primary.
func TestDistributeQuotaOnce(t *testing.T) {
	srv, store := newTestServer(t)
	exportBody := `{"snapshots":[{"provider_name":"nano","kind":"usage","payload":{"used":5},"http_status":200,"fetched_at":"2026-07-21T00:00:00Z"}]}`
	primary := newStubQuotaMember(t, "ptoken", exportBody)
	replica := newStubQuotaMember(t, "rtoken", "")

	pm, _ := store.CreateMember(t.Context(), "primary", primary.srv.URL, "ptoken")
	_, _ = store.CreateMember(t.Context(), "replica", replica.srv.URL, "rtoken")
	if err := store.SetAutoSync(t.Context(), true, pm.ID); err != nil {
		t.Fatalf("SetAutoSync: %v", err)
	}

	srv.DistributeQuotaOnce(t.Context())

	got := replica.postedBodies()
	if len(got) != 1 || !strings.Contains(string(got[0]), `"used":5`) {
		t.Fatalf("member should receive the primary snapshot, got %v", got)
	}
	if n := len(primary.postedBodies()); n != 0 {
		t.Fatalf("primary is the source, not a destination; got %d posts", n)
	}
}

// TestDistributeQuotaOnce_NoPrimaryIsNoop: with no designated primary the pass
// is a no-op and touches nothing.
func TestDistributeQuotaOnce_NoPrimaryIsNoop(t *testing.T) {
	srv, _ := newTestServer(t)
	srv.DistributeQuotaOnce(t.Context()) // must not panic or call anything
}

// TestRunQuotaDistributeDistributesOnTick: the loop distributes on its tick.
func TestRunQuotaDistributeDistributesOnTick(t *testing.T) {
	old := quotaDistributeInterval
	quotaDistributeInterval = 10 * time.Millisecond
	t.Cleanup(func() { quotaDistributeInterval = old })

	srv, store := newTestServer(t)
	exportBody := `{"snapshots":[{"provider_name":"nano","kind":"usage","payload":{"used":5},"http_status":200,"fetched_at":"2026-07-21T00:00:00Z"}]}`
	primary := newStubQuotaMember(t, "ptoken", exportBody)
	replica := newStubQuotaMember(t, "rtoken", "")
	pm, _ := store.CreateMember(t.Context(), "primary", primary.srv.URL, "ptoken")
	_, _ = store.CreateMember(t.Context(), "replica", replica.srv.URL, "rtoken")
	if err := store.SetAutoSync(t.Context(), true, pm.ID); err != nil {
		t.Fatalf("SetAutoSync: %v", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go srv.RunQuotaDistribute(ctx)

	deadline := time.After(2 * time.Second)
	for {
		if len(replica.postedBodies()) > 0 {
			return
		}
		select {
		case <-deadline:
			t.Fatal("no distribution within deadline")
		case <-time.After(5 * time.Millisecond):
		}
	}
}

// TestRunQuotaDistributeStopsOnContextCancel: the loop returns promptly when its
// context is cancelled.
func TestRunQuotaDistributeStopsOnContextCancel(t *testing.T) {
	srv, _ := newTestServer(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	done := make(chan struct{})
	go func() { srv.RunQuotaDistribute(ctx); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("RunQuotaDistribute did not return after context cancel")
	}
}
