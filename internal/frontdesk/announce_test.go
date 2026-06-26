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
