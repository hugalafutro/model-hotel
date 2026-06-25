package frontdesk

import (
	"context"
	"net/http"
	"net/http/httptest"
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

	p, store, _ := newTestPoller(t, "")
	ctx := context.Background()
	m, err := store.CreateMember(ctx, "m1", srv.URL, "admin-token")
	if err != nil {
		t.Fatalf("CreateMember: %v", err)
	}

	p.PollVersionsOnce(ctx)

	if got := p.Snapshot()[m.ID].Version; got != "1.2.3" {
		t.Errorf("recorded version = %q, want 1.2.3", got)
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

	p := NewPoller(store, events.NewBus(), traefik.URL)
	p.PollTraefikOnce(ctx)

	if got := p.Snapshot()[m.ID].TraefikStatus; got != "UP" {
		t.Errorf("traefik status = %q, want UP", got)
	}
}
