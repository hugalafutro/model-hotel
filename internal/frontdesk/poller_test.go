package frontdesk

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hugalafutro/model-hotel/internal/events"
)

func newTestPoller(t *testing.T, traefikAPI string) (*Poller, *Store, *events.Bus) {
	t.Helper()
	s := newTestStore(t)
	bus := events.NewBus()
	return NewPoller(s, bus, traefikAPI), s, bus
}

func TestCheckHealth(t *testing.T) {
	p, _, _ := newTestPoller(t, "")
	ctx := context.Background()

	okSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == memberHealthPath {
			_, _ = w.Write([]byte("OK"))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer okSrv.Close()

	hs := p.checkHealth(ctx, okSrv.URL)
	if !hs.Healthy || !hs.Known || hs.Error != "" {
		t.Errorf("healthy server: %+v", hs)
	}
	if hs.LatencyMs < 0 {
		t.Errorf("latency negative: %d", hs.LatencyMs)
	}

	degraded := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer degraded.Close()
	hs = p.checkHealth(ctx, degraded.URL)
	if hs.Healthy || hs.Error == "" {
		t.Errorf("degraded server should be unhealthy: %+v", hs)
	}

	// Unreachable host.
	hs = p.checkHealth(ctx, "http://127.0.0.1:1")
	if hs.Healthy || hs.Error == "" {
		t.Errorf("unreachable should be unhealthy: %+v", hs)
	}
}

func TestApplyHealthTransitions(t *testing.T) {
	p, store, bus := newTestPoller(t, "")
	ctx := context.Background()
	m, _ := store.CreateMember(ctx, "h", "http://h:8081", "")

	ch := bus.Subscribe()
	defer bus.Unsubscribe(ch)

	// nextTransition returns the next event on the bus, skipping the bus-only
	// "member.status" UI-refresh nudges (which are not persisted and carry no
	// control-plane meaning) so the assertions can focus on the transition events.
	nextTransition := func() events.Event {
		t.Helper()
		for {
			select {
			case ev := <-ch:
				if ev.Type == "member.status" {
					continue
				}
				return ev
			case <-time.After(time.Second):
				t.Fatal("timed out waiting for a transition event")
			}
		}
	}

	thr := p.healthFailThreshold(ctx)
	if thr < 2 {
		t.Fatalf("test assumes a grace window; threshold = %d", thr)
	}

	// First observation healthy: silent in the event log, but still nudges the UI
	// so a freshly added healthy member populates without a manual reload.
	p.applyHealth(ctx, m, HealthStatus{Known: true, Healthy: true})
	_, total, _ := store.ListEvents(ctx, EventFilter{})
	if total != 0 {
		t.Fatalf("first healthy should be silent in the log, got %d events", total)
	}
	if nudge := <-ch; nudge.Type != "member.status" {
		t.Errorf("first healthy should emit a member.status nudge, got %+v", nudge)
	}

	// Below-threshold failures are tolerated: no event, no nudge, and the badge
	// stays healthy (a rebuild blip must not flip the dashboard red).
	for i := 1; i < thr; i++ {
		p.applyHealth(ctx, m, HealthStatus{Known: true, Healthy: false, Error: "boom"})
		select {
		case ev := <-ch:
			t.Errorf("failure %d below threshold should be silent, got %+v", i, ev)
		default:
		}
	}
	if snap := p.Snapshot(); !snap[m.ID].Health.Healthy {
		t.Errorf("badge should stay healthy during the grace window: %+v", snap[m.ID])
	}

	// The threshold-th consecutive failure confirms down: one health.down
	// (preceded by a member.status nudge).
	p.applyHealth(ctx, m, HealthStatus{Known: true, Healthy: false, Error: "boom"})
	ev := nextTransition()
	if ev.Type != "health.down" || ev.Severity != "error" {
		t.Errorf("down event: %+v", ev)
	}

	// Recovery is immediate: the first healthy poll emits health.up.
	p.applyHealth(ctx, m, HealthStatus{Known: true, Healthy: true, LatencyMs: 12})
	ev = nextTransition()
	if ev.Type != "health.up" || ev.Severity != "success" {
		t.Errorf("up event: %+v", ev)
	}

	// No change: no further event of any kind (no transition, no nudge).
	p.applyHealth(ctx, m, HealthStatus{Known: true, Healthy: true})
	select {
	case ev := <-ch:
		t.Errorf("unchanged state should not emit, got %+v", ev)
	default:
	}

	// Two transitions persisted to the event log.
	_, total, _ = store.ListEvents(ctx, EventFilter{})
	if total != 2 {
		t.Errorf("expected 2 persisted transition events, got %d", total)
	}

	// Snapshot reflects last status.
	snap := p.Snapshot()
	if !snap[m.ID].Health.Healthy {
		t.Errorf("snapshot should show healthy: %+v", snap[m.ID])
	}
}

func TestApplyHealthFirstObservationDownDebounced(t *testing.T) {
	p, store, bus := newTestPoller(t, "")
	ctx := context.Background()
	m, _ := store.CreateMember(ctx, "h", "http://h:8081", "")
	thr := p.healthFailThreshold(ctx)

	ch := bus.Subscribe()
	defer bus.Unsubscribe(ch)

	// A member down from its very first observation is not reported until it has
	// missed `thr` polls in a row (a rebuild started while Front Desk was down).
	// The grace-window polls emit no event (the first observation nudges the
	// badge to "unknown"; below-threshold failures are otherwise silent).
	for i := 1; i < thr; i++ {
		p.applyHealth(ctx, m, HealthStatus{Known: true, Healthy: false, Error: "down at start"})
		if _, total, _ := store.ListEvents(ctx, EventFilter{}); total != 0 {
			t.Fatalf("down before threshold (poll %d) should be silent, got %d events", i, total)
		}
	}
	// Drain the baseline "unknown" nudge from the first observation so the check
	// below sees only what the confirming poll emits.
	sawMemberStatus(ch)

	p.applyHealth(ctx, m, HealthStatus{Known: true, Healthy: false, Error: "down at start"})
	evs, total, _ := store.ListEvents(ctx, EventFilter{})
	if total != 1 || evs[0].Type != "health.down" {
		t.Errorf("threshold-th down should emit health.down, got %d events", total)
	}
	// The badge crosses unknown -> down (Known flips, Healthy does not), so the
	// confirming poll must still nudge connected UIs to refetch.
	if !sawMemberStatus(ch) {
		t.Error("confirmed-down should nudge the badge even from an unknown start")
	}
}

func TestApplyHealthBlipBelowThresholdIsSilent(t *testing.T) {
	p, store, _ := newTestPoller(t, "")
	ctx := context.Background()
	m, _ := store.CreateMember(ctx, "h", "http://h:8081", "")
	// Configure a grace window (threshold >= 2) so a sub-threshold blip is silent.
	set, err := store.GetSettings(ctx)
	if err != nil {
		t.Fatalf("get settings: %v", err)
	}
	set.HealthFailThreshold = 3
	if err := store.UpdateSettings(ctx, set); err != nil {
		t.Fatalf("update settings: %v", err)
	}
	thr := p.healthFailThreshold(ctx)
	if thr < 2 {
		t.Fatalf("expected grace window (threshold >= 2) after configuring, got %d", thr)
	}

	p.applyHealth(ctx, m, HealthStatus{Known: true, Healthy: true})
	for i := 1; i < thr; i++ { // a rebuild blip, one poll short of the threshold
		p.applyHealth(ctx, m, HealthStatus{Known: true, Healthy: false, Error: "rebuild"})
	}
	p.applyHealth(ctx, m, HealthStatus{Known: true, Healthy: true}) // back before it counts

	if _, total, _ := store.ListEvents(ctx, EventFilter{}); total != 0 {
		t.Errorf("a sub-threshold blip should persist no events, got %d", total)
	}
}

func TestApplyHealthThresholdConfigurable(t *testing.T) {
	p, store, _ := newTestPoller(t, "")
	ctx := context.Background()
	m, _ := store.CreateMember(ctx, "h", "http://h:8081", "")

	set, err := store.GetSettings(ctx)
	if err != nil {
		t.Fatalf("get settings: %v", err)
	}
	set.HealthFailThreshold = 1
	if err := store.UpdateSettings(ctx, set); err != nil {
		t.Fatalf("update settings: %v", err)
	}

	// Threshold 1 restores immediate reporting: the first down emits.
	p.applyHealth(ctx, m, HealthStatus{Known: true, Healthy: false, Error: "boom"})
	if _, total, _ := store.ListEvents(ctx, EventFilter{}); total != 1 {
		t.Errorf("threshold=1 should emit on first down, got %d events", total)
	}
}

func TestParseTraefikServerStatus(t *testing.T) {
	body := []byte(`[
		{"name":"other@docker","serverStatus":{"http://x":"UP"}},
		{"name":"hotel@http","serverStatus":{"http://a:8081":"UP","http://b:8081":"DOWN"}}
	]`)
	got, err := parseTraefikServerStatus(body)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got["http://a:8081"] != "UP" || got["http://b:8081"] != "DOWN" {
		t.Errorf("server status map: %+v", got)
	}

	// No hotel service -> empty map, no error.
	got, err = parseTraefikServerStatus([]byte(`[{"name":"other@docker","serverStatus":{}}]`))
	if err != nil || len(got) != 0 {
		t.Errorf("missing hotel service: got=%+v err=%v", got, err)
	}

	if _, err := parseTraefikServerStatus([]byte(`not json`)); err == nil {
		t.Error("invalid json should error")
	}
}

func TestPollTraefikOnceMapsByURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == traefikServicesAPI {
			_, _ = w.Write([]byte(`[{"name":"hotel@http","serverStatus":{"http://a:8081":"UP","http://b:8081":"DOWN"}}]`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	p, store, _ := newTestPoller(t, srv.URL)
	ctx := context.Background()
	a, _ := store.CreateMember(ctx, "a", "http://a:8081", "")
	b, _ := store.CreateMember(ctx, "b", "http://b:8081", "")

	// Threshold 1 so a single poll commits DOWN; this test covers URL mapping,
	// not the down-flip damping (which TestPollTraefikOnceDampsDownFlip covers).
	set, _ := store.GetSettings(ctx)
	set.HealthFailThreshold = 1
	if err := store.UpdateSettings(ctx, set); err != nil {
		t.Fatalf("UpdateSettings: %v", err)
	}

	p.PollTraefikOnce(ctx)
	snap := p.Snapshot()
	if snap[a.ID].TraefikStatus != "UP" {
		t.Errorf("a traefik status = %q, want UP", snap[a.ID].TraefikStatus)
	}
	if snap[b.ID].TraefikStatus != "DOWN" {
		t.Errorf("b traefik status = %q, want DOWN", snap[b.ID].TraefikStatus)
	}
}

func TestFetchMemberVersion(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if r.URL.Path == memberSettingsPath {
			_, _ = w.Write([]byte(`{"app_version":"0.9.80","other":"x"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	p, _, _ := newTestPoller(t, "")
	v, err := p.fetchMemberVersion(context.Background(), srv.URL, "tok123")
	if err != nil {
		t.Fatalf("fetchMemberVersion: %v", err)
	}
	if v != "0.9.80" {
		t.Errorf("version = %q, want 0.9.80", v)
	}
	if gotAuth != "Bearer tok123" {
		t.Errorf("auth header = %q", gotAuth)
	}
}

func TestPollVersionsOnce(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"app_version":"1.2.3"}`))
	}))
	defer srv.Close()

	p, store, _ := newTestPoller(t, "")
	ctx := context.Background()
	withTok, _ := store.CreateMember(ctx, "wt", srv.URL, "tok")
	noTok, _ := store.CreateMember(ctx, "nt", "http://nt:8081", "")

	p.PollVersionsOnce(ctx)
	snap := p.Snapshot()
	if snap[withTok.ID].Version != "1.2.3" {
		t.Errorf("tokened member version = %q, want 1.2.3", snap[withTok.ID].Version)
	}
	if snap[noTok.ID].Version != "" {
		t.Errorf("tokenless member should have no version, got %q", snap[noTok.ID].Version)
	}
}

func TestConfigStalenessWatchdog(t *testing.T) {
	p, store, bus := newTestPoller(t, "")
	ctx := context.Background()
	_ = store

	// Controllable clock.
	now := time.Now()
	p.now = func() time.Time { return now }

	ch := bus.Subscribe()
	defer bus.Unsubscribe(ch)

	// First call with never-polled: arms baseline, no warning.
	p.checkConfigStaleness(ctx)
	select {
	case ev := <-ch:
		t.Fatalf("first check should arm silently, got %+v", ev)
	default:
	}

	// Advance beyond the stale threshold (default 30s): one warning.
	now = now.Add(31 * time.Second)
	p.checkConfigStaleness(ctx)
	ev := <-ch
	if ev.Type != "traefik.stale" || ev.Severity != "warning" {
		t.Errorf("stale event: %+v", ev)
	}

	// Still stale: no duplicate warning.
	now = now.Add(31 * time.Second)
	p.checkConfigStaleness(ctx)
	select {
	case ev := <-ch:
		t.Errorf("should not warn twice, got %+v", ev)
	default:
	}

	// Traefik polls again: re-arms.
	p.RecordConfigPoll()
	now = now.Add(31 * time.Second)
	p.checkConfigStaleness(ctx)
	ev = <-ch
	if ev.Type != "traefik.stale" {
		t.Errorf("after re-arm should warn again: %+v", ev)
	}
}

func TestAutoSyncStalenessWatchdog(t *testing.T) {
	p, store, bus := newTestPoller(t, "")
	ctx := context.Background()

	ch := bus.Subscribe()
	defer bus.Unsubscribe(ch)

	// Off with a designated primary and no sync ever recorded: one warning.
	if err := store.SetAutoSync(ctx, false, "m1"); err != nil {
		t.Fatal(err)
	}
	p.checkAutoSyncStale(ctx)
	ev := <-ch
	if ev.Type != "config.autosync_stale" || ev.Severity != "warning" {
		t.Fatalf("stale event: %+v", ev)
	}

	// Still stale: no duplicate warning.
	p.checkAutoSyncStale(ctx)
	select {
	case ev := <-ch:
		t.Errorf("should not warn twice, got %+v", ev)
	default:
	}

	// Enabling auto-sync clears the condition and disarms silently.
	if err := store.SetAutoSync(ctx, true, "m1"); err != nil {
		t.Fatal(err)
	}
	p.checkAutoSyncStale(ctx)
	select {
	case ev := <-ch:
		t.Errorf("enabling should not emit, got %+v", ev)
	default:
	}

	// Disabling again re-arms and re-alerts on the next stale episode.
	if err := store.SetAutoSync(ctx, false, "m1"); err != nil {
		t.Fatal(err)
	}
	p.checkAutoSyncStale(ctx)
	ev = <-ch
	if ev.Type != "config.autosync_stale" {
		t.Errorf("after re-arm should warn again: %+v", ev)
	}
}

// TestPollVersionsOnceClearsVersionOnFailedFetch: a version we can no longer
// read is unknown, and the sync gates treat unknown as skewed (fail closed).
// Keeping the last good value would let a sync proceed on stale data while a
// member is mid-upgrade, so a failed fetch clears the cached version.
func TestPollVersionsOnceClearsVersionOnFailedFetch(t *testing.T) {
	p, store, _ := newTestPoller(t, "")
	ctx := context.Background()

	var fail atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if fail.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(`{"app_version":"v1.2.3"}`))
	}))
	defer srv.Close()

	m, _ := store.CreateMember(ctx, "m", srv.URL, "tok")
	p.PollVersionsOnce(ctx)
	if v := p.MemberVersion(m.ID); v != "v1.2.3" {
		t.Fatalf("seed version = %q, want v1.2.3", v)
	}

	fail.Store(true)
	p.PollVersionsOnce(ctx)
	if v := p.MemberVersion(m.ID); v != "" {
		t.Errorf("version after a failed fetch = %q, want empty (fail closed)", v)
	}
}

func TestMemberVersion(t *testing.T) {
	p := NewPoller(nil, nil, "")
	if v := p.MemberVersion("m1"); v != "" {
		t.Errorf("unpolled member version = %q, want empty", v)
	}
	p.mu.Lock()
	p.statuses["m1"] = MemberStatus{Version: "v1.2.3"}
	p.mu.Unlock()
	if v := p.MemberVersion("m1"); v != "v1.2.3" {
		t.Errorf("MemberVersion = %q, want v1.2.3", v)
	}
}

func TestConfigPollStaleAccessor(t *testing.T) {
	store := newTestStore(t)       // reuse this file's existing store fixture helper
	p := NewPoller(store, nil, "") // nil bus falls back to events.DefaultBus
	base := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	now := base
	p.now = func() time.Time { return now }

	// Never polled: not stale (unarmed), matching checkConfigStaleness.
	if p.ConfigPollStale(context.Background()) {
		t.Fatal("unarmed poller reported stale")
	}
	p.RecordConfigPoll()
	if p.ConfigPollStale(context.Background()) {
		t.Fatal("fresh poll reported stale")
	}
	now = base.Add(10 * time.Minute) // default threshold is 30s
	if !p.ConfigPollStale(context.Background()) {
		t.Fatal("10-minute-old poll not reported stale")
	}
}
