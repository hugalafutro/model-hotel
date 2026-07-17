package frontdesk

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/hugalafutro/model-hotel/internal/events"
)

// TestPollVersionsOnceRecordsVersion covers the success path of version polling:
// a member that serves its settings API has its app_version recorded on the
// status snapshot (the data behind the version-mismatch UI).
func TestPollVersionsOnceRecordsVersion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == memberSettingsPath && r.Header.Get("Authorization") != "" {
			_, _ = w.Write([]byte(`{"app_version":"1.2.3"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	p, store, bus := newTestPoller(t, "")
	ctx := context.Background()
	m, err := store.CreateMember(ctx, "m1", srv.URL, "admin-token")
	if err != nil {
		t.Fatalf("CreateMember: %v", err)
	}

	ch := bus.Subscribe()
	defer bus.Unsubscribe(ch)

	p.PollVersionsOnce(ctx)

	if got := p.Snapshot()[m.ID].Version; got != "1.2.3" {
		t.Errorf("recorded version = %q, want 1.2.3", got)
	}
	// A first-seen version nudges the UI to refetch without a manual reload.
	if !sawMemberStatus(ch) {
		t.Error("first version read should emit a member.status nudge")
	}
}

// sawMemberStatus drains the bus channel and reports whether a member.status
// UI-refresh nudge was published (the signal the Members tab refetches on).
func sawMemberStatus(ch chan events.Event) bool {
	for {
		select {
		case ev := <-ch:
			if ev.Type == "member.status" {
				return true
			}
		default:
			return false
		}
	}
}

// TestPollTraefikOnceUpdatesStatus covers the success path of Traefik-status
// polling: a member listed UP in Traefik's services API gets that status on its
// snapshot (the data behind the Members tab Traefik column).
func TestPollTraefikOnceUpdatesStatus(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	m, err := store.CreateMember(ctx, "m1", "http://m1:8081", "")
	if err != nil {
		t.Fatalf("CreateMember: %v", err)
	}

	// The Traefik services response references the member's stored URL so the
	// status maps back onto it.
	traefik := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == traefikServicesAPI {
			_, _ = w.Write([]byte(`[{"name":"hotel@http","serverStatus":{"` + m.URL + `":"UP"}}]`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer traefik.Close()

	bus := events.NewBus()
	p := NewPoller(store, bus, traefik.URL)
	ch := bus.Subscribe()
	defer bus.Unsubscribe(ch)

	p.PollTraefikOnce(ctx)

	if got := p.Snapshot()[m.ID].TraefikStatus; got != "UP" {
		t.Errorf("traefik status = %q, want UP", got)
	}
	// Traefik catching up to a member emits a member.status nudge so the column
	// fills without a manual reload.
	if !sawMemberStatus(ch) {
		t.Error("first Traefik status should emit a member.status nudge")
	}

	// A second identical poll must not re-nudge: the status did not change.
	p.PollTraefikOnce(ctx)
	if sawMemberStatus(ch) {
		t.Error("unchanged Traefik status should not emit a nudge")
	}
}

// TestPollTraefikOnceDampsDownFlip covers the rebuild-tolerance path: a member
// briefly marked DOWN (or unlisted) must not flip the badge until the non-UP
// status has been seen `health_fail_threshold` polls in a row; recovery to UP
// is immediate.
func TestPollTraefikOnceDampsDownFlip(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	m, err := store.CreateMember(ctx, "m1", "http://m1:8081", "")
	if err != nil {
		t.Fatalf("CreateMember: %v", err)
	}

	var status atomic.Value // current serverStatus string, "" = unlisted
	status.Store("UP")
	traefik := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != traefikServicesAPI {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		s, _ := status.Load().(string)
		if s == "" {
			_, _ = w.Write([]byte(`[]`)) // Traefik no longer lists the member
			return
		}
		_, _ = w.Write([]byte(`[{"name":"hotel@http","serverStatus":{"` + m.URL + `":"` + s + `"}}]`))
	}))
	defer traefik.Close()

	p := NewPoller(store, events.NewBus(), traefik.URL)
	thr := p.healthFailThreshold(ctx)
	if thr < 2 {
		t.Skip("no grace window at this threshold")
	}

	p.PollTraefikOnce(ctx)
	if got := p.Snapshot()[m.ID].TraefikStatus; got != "UP" {
		t.Fatalf("baseline traefik status = %q, want UP", got)
	}

	// Below threshold the DOWN status is held back: the badge stays UP.
	status.Store("DOWN")
	for i := 1; i < thr; i++ {
		p.PollTraefikOnce(ctx)
		if got := p.Snapshot()[m.ID].TraefikStatus; got != "UP" {
			t.Errorf("poll %d: badge should stay UP during grace, got %q", i, got)
		}
	}
	// The threshold-th consecutive non-UP observation commits the flip.
	p.PollTraefikOnce(ctx)
	if got := p.Snapshot()[m.ID].TraefikStatus; got != "DOWN" {
		t.Errorf("threshold-th DOWN should commit, got %q", got)
	}

	// Recovery is immediate.
	status.Store("UP")
	p.PollTraefikOnce(ctx)
	if got := p.Snapshot()[m.ID].TraefikStatus; got != "UP" {
		t.Errorf("recovery to UP should be immediate, got %q", got)
	}
}

// TestPollTraefikOnceBlanksStatusAfterAPIFailures covers the Traefik-API-down
// path: once the Traefik API itself stops answering for `health_fail_threshold`
// polls in a row, every member's TraefikStatus must blank (rendering as the
// faint "unknown") instead of freezing at its last live-looking value.
func TestPollTraefikOnceBlanksStatusAfterAPIFailures(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	m, err := store.CreateMember(ctx, "m1", "http://m1:8081", "")
	if err != nil {
		t.Fatalf("CreateMember: %v", err)
	}

	// apiDown toggles the Traefik API between answering (200 with the member's UP
	// status) and failing (500), so the outage and the recovery can both be driven
	// without tearing the server down. The services response references the
	// member's stored URL so the status maps back onto it.
	var apiDown atomic.Bool
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if apiDown.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if r.URL.Path == traefikServicesAPI {
			_, _ = w.Write([]byte(`[{"name":"hotel@http","serverStatus":{"` + m.URL + `":"UP"}}]`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer stub.Close()

	p := NewPoller(store, events.NewBus(), stub.URL)
	threshold := p.healthFailThreshold(ctx)

	p.PollTraefikOnce(ctx) // seeds "UP"
	if got := p.Snapshot()[m.ID].TraefikStatus; got != "UP" {
		t.Fatalf("seed status = %q, want UP", got)
	}

	// Traefik's API goes down. Below the threshold the last live value is held.
	apiDown.Store(true)
	for i := 0; i < threshold-1; i++ {
		p.PollTraefikOnce(ctx)
		if got := p.Snapshot()[m.ID].TraefikStatus; got != "UP" {
			t.Fatalf("blanked after %d failures, want hold until %d", i+1, threshold)
		}
	}
	// The threshold-th consecutive failure blanks the badge.
	p.PollTraefikOnce(ctx)
	if got := p.Snapshot()[m.ID].TraefikStatus; got != "" {
		t.Fatalf("status after %d failed polls = %q, want blank", threshold, got)
	}
	p.mu.Lock()
	blanked, fails := p.traefikBlanked, p.traefikAPIFails
	p.mu.Unlock()
	if !blanked || fails != threshold {
		t.Fatalf("after blanking: traefikBlanked=%v traefikAPIFails=%d, want true and %d", blanked, fails, threshold)
	}

	// Further failed polls while blanked neither re-blank nor advance the counter.
	p.PollTraefikOnce(ctx)
	p.mu.Lock()
	fails = p.traefikAPIFails
	p.mu.Unlock()
	if fails != threshold {
		t.Fatalf("counter advanced past threshold while blanked: got %d, want %d", fails, threshold)
	}

	// Recovery: the API answers again, so the next successful poll repopulates the
	// badge and resets the outage flags (a regression deleting either reset would
	// leave the badge blank or the flags stuck).
	apiDown.Store(false)
	p.PollTraefikOnce(ctx)
	if got := p.Snapshot()[m.ID].TraefikStatus; got != "UP" {
		t.Fatalf("recovery status = %q, want UP", got)
	}
	p.mu.Lock()
	blanked, fails = p.traefikBlanked, p.traefikAPIFails
	p.mu.Unlock()
	if blanked || fails != 0 {
		t.Fatalf("after recovery: traefikBlanked=%v traefikAPIFails=%d, want false and 0", blanked, fails)
	}
}
