package frontdesk

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
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

func TestActiveMemberCount(t *testing.T) {
	members := []*Member{
		{State: StateActive},
		{State: StateDrained},
		{State: StateActive},
		{State: StateActive},
	}
	if got := activeMemberCount(members); got != 3 {
		t.Errorf("activeMemberCount = %d, want 3 (only StateActive counts)", got)
	}
	if got := activeMemberCount(nil); got != 0 {
		t.Errorf("activeMemberCount(nil) = %d, want 0", got)
	}
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
	p.applyHealth(ctx, m, HealthStatus{Known: true, Healthy: true}, thr)
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
		p.applyHealth(ctx, m, HealthStatus{Known: true, Healthy: false, Error: "boom"}, thr)
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
	p.applyHealth(ctx, m, HealthStatus{Known: true, Healthy: false, Error: "boom"}, thr)
	ev := nextTransition()
	if ev.Type != "health.down" || ev.Severity != "error" {
		t.Errorf("down event: %+v", ev)
	}

	// Recovery is immediate: the first healthy poll emits health.up.
	p.applyHealth(ctx, m, HealthStatus{Known: true, Healthy: true, LatencyMs: 12}, thr)
	ev = nextTransition()
	if ev.Type != "health.up" || ev.Severity != "success" {
		t.Errorf("up event: %+v", ev)
	}

	// No change: no further event of any kind (no transition, no nudge).
	p.applyHealth(ctx, m, HealthStatus{Known: true, Healthy: true}, thr)
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
		p.applyHealth(ctx, m, HealthStatus{Known: true, Healthy: false, Error: "down at start"}, thr)
		if _, total, _ := store.ListEvents(ctx, EventFilter{}); total != 0 {
			t.Fatalf("down before threshold (poll %d) should be silent, got %d events", i, total)
		}
	}
	// Drain the baseline "unknown" nudge from the first observation so the check
	// below sees only what the confirming poll emits.
	sawMemberStatus(ch)

	p.applyHealth(ctx, m, HealthStatus{Known: true, Healthy: false, Error: "down at start"}, thr)
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

	p.applyHealth(ctx, m, HealthStatus{Known: true, Healthy: true}, thr)
	for i := 1; i < thr; i++ { // a rebuild blip, one poll short of the threshold
		p.applyHealth(ctx, m, HealthStatus{Known: true, Healthy: false, Error: "rebuild"}, thr)
	}
	p.applyHealth(ctx, m, HealthStatus{Known: true, Healthy: true}, thr) // back before it counts

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
	thr := p.healthFailThreshold(ctx)
	p.applyHealth(ctx, m, HealthStatus{Known: true, Healthy: false, Error: "boom"}, thr)
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

func TestFetchMemberBuild(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if r.URL.Path == memberSettingsPath {
			_, _ = w.Write([]byte(`{"app_version":"0.9.80","app_commit":"d18a96d1f84d","other":"x"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	p, _, _ := newTestPoller(t, "")
	b, err := p.fetchMemberBuild(context.Background(), srv.URL, "tok123")
	if err != nil {
		t.Fatalf("fetchMemberBuild: %v", err)
	}
	if b.Version != "0.9.80" {
		t.Errorf("version = %q, want 0.9.80", b.Version)
	}
	if b.Commit != "d18a96d1f84d" {
		t.Errorf("commit = %q, want d18a96d1f84d", b.Commit)
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
// member is mid-upgrade, so reads failing for versionFetchFailThreshold polls
// clear the cached version (one blip keeps it; see TestVersionFetchBlipKeepsBuild).
func TestPollVersionsOnceClearsVersionOnFailedFetch(t *testing.T) {
	p, store, _ := newTestPoller(t, "")
	ctx := context.Background()

	var fail atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if fail.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(`{"app_version":"v1.2.3","app_commit":"d18a96d1f84d"}`))
	}))
	defer srv.Close()

	m, _ := store.CreateMember(ctx, "m", srv.URL, "tok")
	p.PollVersionsOnce(ctx)
	if b := p.memberBuildOf(m.ID); b.Version != "v1.2.3" || b.Commit != "d18a96d1f84d" {
		t.Fatalf("seed build = %+v, want v1.2.3 / d18a96d1f84d", b)
	}

	fail.Store(true)
	for range versionFetchFailThreshold {
		p.PollVersionsOnce(ctx)
	}
	// The commit is cleared with the version: kept on its own it would outlive
	// the read that vouched for it, and read as a build we can still confirm.
	if b := p.memberBuildOf(m.ID); b.Version != "" || b.Commit != "" {
		t.Errorf("build after the reads failed to the threshold = %+v, want zero (fail closed)", b)
	}

	// A member whose token is removed loses its build the same way: nothing
	// can read it any more, so nothing may vouch for it.
	fail.Store(false)
	p.PollVersionsOnce(ctx)
	if b := p.memberBuildOf(m.ID); b.Version != "v1.2.3" {
		t.Fatalf("setup: build did not come back: %+v", b)
	}
	if err := store.SetMemberToken(ctx, m.ID, ""); err != nil {
		t.Fatalf("remove token: %v", err)
	}
	p.PollVersionsOnce(ctx)
	if b := p.memberBuildOf(m.ID); b.Version != "" || b.Commit != "" {
		t.Errorf("build after the token was removed = %+v, want zero", b)
	}

	// A token the store holds but cannot decrypt is no token either.
	if err := store.SetMemberToken(ctx, m.ID, "tok"); err != nil {
		t.Fatalf("restore token: %v", err)
	}
	p.PollVersionsOnce(ctx)
	if b := p.memberBuildOf(m.ID); b.Version != "v1.2.3" {
		t.Fatalf("setup: build did not come back: %+v", b)
	}
	corruptMemberToken(t, store, m.ID)
	p.PollVersionsOnce(ctx)
	if b := p.memberBuildOf(m.ID); b.Version != "" || b.Commit != "" {
		t.Errorf("build with an unreadable token = %+v, want zero", b)
	}
}

func TestMemberBuildOf(t *testing.T) {
	p := NewPoller(nil, nil, "")
	if b := p.memberBuildOf("m1"); b != (memberBuild{}) {
		t.Errorf("unpolled member build = %+v, want zero", b)
	}
	p.mu.Lock()
	p.statuses["m1"] = MemberStatus{Version: "v1.2.3", Commit: "d18a96d1f84d"}
	p.mu.Unlock()
	if b := p.memberBuildOf("m1"); b.Version != "v1.2.3" || b.Commit != "d18a96d1f84d" {
		t.Errorf("memberBuildOf = %+v, want v1.2.3 / d18a96d1f84d", b)
	}
}

// The warm gate and the staleness accessor measure from the same instant, the
// watchdog's arming, so a fleet can never read warm-and-healthy while Traefik
// has not fetched and the watchdog has not yet had its window. A watchdog that
// never armed keeps the process-start bound, so the grace is never a deadlock.
func TestConfigPollWarmFollowsTheWatchdogArming(t *testing.T) {
	store := newTestStore(t)
	p := NewPoller(store, nil, "")
	start := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	now := start
	p.now = func() time.Time { return now }
	ctx := context.Background()

	if p.ConfigPollWarm(ctx, start) {
		t.Fatal("just started, unarmed: must not be warm")
	}
	now = start.Add(10 * time.Minute) // default window is 30s
	if !p.ConfigPollWarm(ctx, start) {
		t.Fatal("unarmed a window past process start: the process-start bound must open the gate")
	}
	p.checkConfigStaleness(ctx) // arms now, long after process start
	if p.ConfigPollWarm(ctx, start) || p.ConfigPollStale(ctx) {
		t.Fatal("armed just now: not warm (and not stale) until a window has passed since the arming")
	}
	now = now.Add(10 * time.Minute)
	if !p.ConfigPollWarm(ctx, start) || !p.ConfigPollStale(ctx) {
		t.Fatal("a window past the arming: warm and stale together")
	}
	p.RecordConfigPoll()
	if !p.ConfigPollWarm(ctx, start) || p.ConfigPollStale(ctx) {
		t.Fatal("a fetch: warm and not stale")
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

// TestCheckHealthRedactsURLError guards the health-status leg of the
// credential-leak fix: a dial failure against a member URL that still carries
// userinfo (a row stored before the rejection) is reported without the
// credentials, since HealthStatus.Error is monitor-readable.
func TestCheckHealthRedactsURLError(t *testing.T) {
	p, _, _ := newTestPoller(t, "")
	hs := p.checkHealth(context.Background(), "http://leakuser:leakpass@127.0.0.1:1")
	if hs.Healthy || hs.Error == "" {
		t.Fatalf("unreachable member should report an error: %+v", hs)
	}
	if strings.Contains(hs.Error, "leakuser") || strings.Contains(hs.Error, "leakpass") {
		t.Errorf("health error still carries credentials: %q", hs.Error)
	}
}

// TestPollTraefikOnceMatchesStrippedLegacyURL guards the correlation key for
// legacy rows: BuildTraefikConfig publishes a stored URL without its userinfo,
// so Traefik reports status under the stripped URL, and the lookup must use
// the same key or the member's Traefik badge stays blank forever.
func TestPollTraefikOnceMatchesStrippedLegacyURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == traefikServicesAPI {
			_, _ = w.Write([]byte(`[{"name":"hotel@http","serverStatus":{"http://a:8081":"UP"}}]`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	p, store, _ := newTestPoller(t, srv.URL)
	ctx := context.Background()
	m, err := store.CreateMember(ctx, "a", "http://a:8081", "")
	if err != nil {
		t.Fatalf("CreateMember: %v", err)
	}
	// Plant a pre-fix row shape directly: CreateMember itself now rejects it.
	if _, err := store.db.ExecContext(ctx, "UPDATE members SET url = ? WHERE id = ?",
		"http://leakuser:leakpass@a:8081", m.ID); err != nil {
		t.Fatalf("plant legacy url: %v", err)
	}

	p.PollTraefikOnce(ctx)
	if got := p.Snapshot()[m.ID].TraefikStatus; got != "UP" {
		t.Errorf("legacy member traefik status = %q, want UP", got)
	}
}

// TestTraefikStalenessInputsKeepMonotonicReadings guards the exemption stated
// above checkConfigStaleness in poller_versions.go.
//
// The three Traefik staleness checks subtract raw, without the util.TrustedAge
// guard every settings- and database-backed staleness check in this codebase
// uses. That is only safe while both operands carry Go monotonic readings, so
// that Sub uses the monotonic clock and cannot go negative under a wall-clock
// step. The whole risk of the exemption is a later refactor writing
// `startedAt: time.Now().UTC()`, or wrapping p.now, or routing the stamp
// through a store - each of which silently strips the reading and reinstates
// the bug with no test failing. This is that test.
//
// Round(0) drops the monotonic reading, so a Time that differs from its own
// Round(0) as a STRUCT still has one. (Equal would compare wall clocks, which
// agree either way.)
func TestTraefikStalenessInputsKeepMonotonicReadings(t *testing.T) {
	p, _, _ := newTestPoller(t, "")

	p.RecordConfigPoll()
	p.mu.RLock()
	last := p.lastConfigPollAt
	p.mu.RUnlock()
	if last.IsZero() {
		t.Fatal("RecordConfigPoll left lastConfigPollAt zero")
	}
	if last == last.Round(0) { //nolint:staticcheck // QF1009: identity, not temporal equality
		t.Error("lastConfigPollAt lost its monotonic reading: ConfigPollStale and " +
			"checkConfigStaleness now compare wall clocks and need util.TrustedAge")
	}

	// p.now is the other operand of all three comparisons.
	if n := p.now(); n == n.Round(0) { //nolint:staticcheck // QF1009: identity, not temporal equality
		t.Error("p.now() lost its monotonic reading; the raw subtractions are no longer safe")
	}

	// Server.startedAt is ConfigPollWarm's second operand (fleetstate.go).
	srv, _ := newTestServer(t)
	if srv.startedAt == srv.startedAt.Round(0) { //nolint:staticcheck // QF1009: identity, not temporal equality
		t.Error("Server.startedAt lost its monotonic reading; ConfigPollWarm now compares wall clocks")
	}
}

// A drained member's flips are maintenance, not an outage: the rebuild tool
// drains before it recreates, so its down and up land as health.maintenance
// (info, off by default in the picker) rather than health.down / health.up.
// The same flips on an active member keep paging, which the test above pins.
func TestApplyHealthDrainedMemberFlipsAreMaintenance(t *testing.T) {
	p, store, bus := newTestPoller(t, "")
	ctx := context.Background()
	m, err := store.CreateMember(ctx, "h", "http://h:8081", "")
	if err != nil {
		t.Fatal(err)
	}
	// applyHealth reads the state off the member it is handed, as the poll pass
	// hands it the freshly listed row.
	m.State = StateDrained
	thr := p.healthFailThreshold(ctx)

	ch := bus.Subscribe()
	defer bus.Unsubscribe(ch)
	next := func() events.Event {
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

	p.applyHealth(ctx, m, HealthStatus{Known: true, Healthy: true}, thr)
	for i := 0; i < thr; i++ {
		p.applyHealth(ctx, m, HealthStatus{Known: true, Healthy: false, Error: "recreating"}, thr)
	}
	if ev := next(); ev.Type != "health.maintenance" || ev.Severity != "info" {
		t.Errorf("drained member down: got %+v, want health.maintenance at info", ev)
	}
	// The note closes at success: a different severity from the down note, so a
	// notifier that keys its rows on type and severity shows both ends.
	p.applyHealth(ctx, m, HealthStatus{Known: true, Healthy: true, LatencyMs: 9}, thr)
	if ev := next(); ev.Type != "health.maintenance" || ev.Severity != "success" {
		t.Errorf("drained member up: got %+v, want health.maintenance at success", ev)
	}
	evs, total, err := store.ListEvents(ctx, EventFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 {
		t.Fatalf("persisted events = %d, want the two maintenance notes", total)
	}
	for _, ev := range evs {
		if ev.Type != "health.maintenance" || ev.MemberID != m.ID {
			t.Errorf("a drained member must not page: %+v", ev)
		}
	}
}

// The type is an episode's, decided when the down is recorded: an outage that
// paged closes as health.up even if the member was drained meanwhile, and a
// member re-activated while still down turns its maintenance note into the
// page it now deserves, then closes as health.up.
func TestApplyHealthEpisodeKeepsItsType(t *testing.T) {
	p, store, bus := newTestPoller(t, "")
	ctx := context.Background()
	thr := p.healthFailThreshold(ctx)
	ch := bus.Subscribe()
	defer bus.Unsubscribe(ch)
	next := func() events.Event {
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
	nothing := func(what string) {
		t.Helper()
		for {
			select {
			case ev := <-ch:
				if ev.Type != "member.status" {
					t.Errorf("%s: got %+v, want no event", what, ev)
				}
			default:
				return
			}
		}
	}

	// Paged while active, drained during the outage: the all-clear still pages.
	a, err := store.CreateMember(ctx, "a", "http://a:8081", "")
	if err != nil {
		t.Fatal(err)
	}
	p.applyHealth(ctx, a, HealthStatus{Known: true, Healthy: true}, thr)
	for i := 0; i < thr; i++ {
		p.applyHealth(ctx, a, HealthStatus{Known: true, Healthy: false, Error: "gone"}, thr)
	}
	if ev := next(); ev.Type != "health.down" {
		t.Fatalf("active down: %+v", ev)
	}
	a.State = StateDrained
	p.applyHealth(ctx, a, HealthStatus{Known: true, Healthy: false, Error: "gone"}, thr)
	nothing("drained while paged")
	p.applyHealth(ctx, a, HealthStatus{Known: true, Healthy: true}, thr)
	if ev := next(); ev.Type != "health.up" {
		t.Errorf("recovery of a paged outage: %+v, want health.up", ev)
	}

	// Down while drained, re-activated still down: the note becomes a page.
	b, err := store.CreateMember(ctx, "b", "http://b:8081", "")
	if err != nil {
		t.Fatal(err)
	}
	b.State = StateDrained
	p.applyHealth(ctx, b, HealthStatus{Known: true, Healthy: true}, thr)
	for i := 0; i < thr; i++ {
		p.applyHealth(ctx, b, HealthStatus{Known: true, Healthy: false, Error: "recreating"}, thr)
	}
	if ev := next(); ev.Type != "health.maintenance" {
		t.Fatalf("drained down: %+v", ev)
	}
	p.applyHealth(ctx, b, HealthStatus{Known: true, Healthy: false, Error: "recreating"}, thr)
	nothing("still drained, still down")
	b.State = StateActive
	p.applyHealth(ctx, b, HealthStatus{Known: true, Healthy: false, Error: "still dead"}, thr)
	if ev := next(); ev.Type != "health.down" || ev.Severity != "error" {
		t.Errorf("re-activated while down: %+v, want health.down", ev)
	}
	p.applyHealth(ctx, b, HealthStatus{Known: true, Healthy: false, Error: "still dead"}, thr)
	nothing("paged once")
	// Drained again while still down: the page stands, nothing more is said.
	b.State = StateDrained
	p.applyHealth(ctx, b, HealthStatus{Known: true, Healthy: false, Error: "still dead"}, thr)
	nothing("re-drained after the page")
	b.State = StateActive
	p.applyHealth(ctx, b, HealthStatus{Known: true, Healthy: true}, thr)
	if ev := next(); ev.Type != "health.up" {
		t.Errorf("recovery after the page: %+v, want health.up", ev)
	}
}

// TestForgottenMemberStatusIsNotWrittenBack: a health poll that measured a
// member before its removal lands after forgetMember. Its write must not
// re-add the member to the status map, which the Traefik status endpoint serves
// whole.
func TestForgottenMemberStatusIsNotWrittenBack(t *testing.T) {
	p, _, _ := newTestPoller(t, "")
	m := &Member{ID: "gone", Name: "gone"}
	p.applyHealth(t.Context(), m, HealthStatus{Known: true, Healthy: true}, 3)
	p.forgetMember(m.ID)
	p.applyHealth(t.Context(), m, HealthStatus{Known: true, Healthy: true}, 3)
	if _, ok := p.Snapshot()[m.ID]; ok {
		t.Error("a poll landing after the member's removal re-added its status")
	}
}

// TestPollOfRemovedMemberCommitsNothing: health, version and announce polls
// whose requests are in flight when the member is removed land after
// forgetMember. With a health threshold of 1 the late failure would confirm the
// member down; none of them may write state back or emit an event for it.
func TestPollOfRemovedMemberCommitsNothing(t *testing.T) {
	p, store, bus := newTestPoller(t, "")
	ctx := t.Context()
	set, err := store.GetSettings(ctx)
	if err != nil {
		t.Fatalf("get settings: %v", err)
	}
	set.HealthFailThreshold = 1
	if err := store.UpdateSettings(ctx, set); err != nil {
		t.Fatalf("update settings: %v", err)
	}
	const polls = 3 // health, version, announce
	entered := make(chan struct{}, polls)
	release := make(chan struct{})
	member := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		entered <- struct{}{}
		<-release
		if r.URL.Path == memberAnnouncePath {
			w.WriteHeader(http.StatusConflict)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(member.Close)
	m, err := store.CreateMember(ctx, "gone", member.URL, "tok")
	if err != nil {
		t.Fatalf("create member: %v", err)
	}
	ch := bus.Subscribe()

	var wg sync.WaitGroup
	for _, poll := range []func(context.Context){p.PollHealthOnce, p.PollVersionsOnce, p.PollAnnounceOnce} {
		wg.Go(func() { poll(ctx) })
	}
	for range polls {
		<-entered
	}
	if err := store.DeleteMember(ctx, m.ID); err != nil {
		t.Fatalf("delete member: %v", err)
	}
	p.forgetMember(m.ID)
	close(release)
	wg.Wait()

	p.mu.RLock()
	_, status := p.statuses[m.ID]
	state := []bool{status, p.healthFailures[m.ID] != 0, p.versionFailures[m.ID] != 0, p.conflictNotified[m.ID]}
	p.mu.RUnlock()
	if slices.Contains(state, true) {
		t.Errorf("state written back for the removed member (status, health, version, conflict): %v", state)
	}
	if _, total, _ := store.ListEvents(ctx, EventFilter{}); total != 0 {
		t.Errorf("%d events recorded for the removed member, want none", total)
	}
	select {
	case ev := <-ch:
		t.Errorf("published %s for the removed member", ev.Type)
	default:
	}
}

// A stored threshold below 1 would report a member down on its first failed
// poll (or never, at zero), so a readable but invalid row falls back to 3.
func TestHealthFailThresholdDefaultsWhenInvalid(t *testing.T) {
	p, store, _ := newTestPoller(t, "")
	if _, err := store.db.Exec(`UPDATE settings SET health_fail_threshold = 0 WHERE id = 1`); err != nil {
		t.Fatalf("zero the threshold: %v", err)
	}
	if got := p.healthFailThreshold(context.Background()); got != 3 {
		t.Errorf("threshold with a stored 0 = %d, want the default 3", got)
	}
}

// secs turns a stored interval into a duration, substituting the fallback for an
// unset or invalid (below one second) value so a bad row cannot spin a loop.
func TestSecsFallsBackBelowOneSecond(t *testing.T) {
	for _, c := range []struct {
		n    int
		want time.Duration
	}{
		{0, 7 * time.Second},
		{-1, 7 * time.Second},
		{4, 4 * time.Second},
	} {
		if got := secs(c.n, 7); got != c.want {
			t.Errorf("secs(%d, 7) = %v, want %v", c.n, got, c.want)
		}
	}
}

// A member URL the request cannot even be built from is reported down with the
// parse error, and that error is redacted like a dial error: it quotes the URL,
// which can carry userinfo on a row stored before the rejection.
func TestCheckHealthRedactsUnbuildableURLError(t *testing.T) {
	p, _, _ := newTestPoller(t, "")
	hs := p.checkHealth(context.Background(), "http://leakuser:leakpass@127.0.0.1:1/%zz")
	if !hs.Known || hs.Healthy {
		t.Fatalf("an unbuildable URL should report known-down: %+v", hs)
	}
	if !strings.Contains(hs.Error, "invalid URL escape") {
		t.Errorf("error = %q, want the request-build failure", hs.Error)
	}
	if strings.Contains(hs.Error, "leakuser") || strings.Contains(hs.Error, "leakpass") {
		t.Errorf("health error still carries credentials: %q", hs.Error)
	}
}

// tickLoop runs its poll again once the interval elapses, and stops when the
// context ends. The poll cancels the context on its second run, so exactly two
// runs prove the wait both elapsed and ended the loop.
func TestTickLoopRerunsAfterInterval(t *testing.T) {
	p, _, _ := newTestPoller(t, "")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runs := 0
	done := make(chan struct{})
	go func() {
		defer close(done)
		p.tickLoop(ctx, func(Settings) time.Duration { return time.Millisecond }, func(context.Context) {
			runs++
			if runs == 2 {
				cancel()
			}
		})
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("tickLoop did not stop after the context was cancelled")
	}
	if runs != 2 {
		t.Errorf("poll ran %d times, want 2", runs)
	}
}

// TestPollerRunTicksAndStops exercises Run + tickLoop and one tick of every poll
// loop, then confirms Run returns once the context is cancelled.
func TestPollerRunTicksAndStops(t *testing.T) {
	traefik := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("{}"))
	}))
	defer traefik.Close()

	member := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == memberHealthPath {
			_, _ = w.Write([]byte("OK"))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer member.Close()

	p, store, _ := newTestPoller(t, traefik.URL)
	if _, err := store.CreateMember(context.Background(), "m1", member.URL, ""); err != nil {
		t.Fatalf("CreateMember: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	done := make(chan struct{})
	go func() {
		p.Run(ctx)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after the context was cancelled")
	}
	if snap := p.Snapshot(); len(snap) == 0 {
		t.Error("expected at least one member status after a health tick")
	}
}

// TestPollerPollsHandleStoreErrors confirms the poll loops degrade gracefully
// (log and return, never panic) when the store is unavailable, and that
// settings() falls back to defaults.
func TestPollerPollsHandleStoreErrors(t *testing.T) {
	p, store, _ := newTestPoller(t, "")
	if err := store.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	ctx := context.Background()
	p.PollHealthOnce(ctx)
	p.PollVersionsOnce(ctx)
	p.PollTraefikOnce(ctx)
	p.checkConfigStaleness(ctx)
	if s := p.settings(ctx); s.HealthPollSecs == 0 {
		t.Error("settings() should fall back to defaults when the store errors")
	}
}

// TestPollHealthOnceReportsDown covers the per-member health loop and the
// down-transition event path. The threshold is set to 1 so a single poll
// confirms down (the debounce itself is covered in poller_test.go).
func TestPollHealthOnceReportsDown(t *testing.T) {
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer down.Close()

	p, store, _ := newTestPoller(t, "")
	ctx := context.Background()
	m, err := store.CreateMember(ctx, "m1", down.URL, "")
	if err != nil {
		t.Fatalf("CreateMember: %v", err)
	}

	set, err := store.GetSettings(ctx)
	if err != nil {
		t.Fatalf("GetSettings: %v", err)
	}
	set.HealthFailThreshold = 1
	if err := store.UpdateSettings(ctx, set); err != nil {
		t.Fatalf("UpdateSettings: %v", err)
	}

	p.PollHealthOnce(ctx)

	st, ok := p.Snapshot()[m.ID]
	if !ok || !st.Health.Known || st.Health.Healthy {
		t.Fatalf("expected a known-unhealthy status, got %+v (ok=%v)", st, ok)
	}
	evs, _, err := store.ListEvents(ctx, EventFilter{Type: "health.down", Limit: 10})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(evs) == 0 {
		t.Error("expected a health.down event to be recorded")
	}
}

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
	// Configure a grace window (threshold >= 2) so the DOWN status is held back.
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

// TestPruneEventsOnceAppliesRetention covers the retention tick: events past the
// configured window are deleted, so the operator-set event_retention_days is
// actually applied instead of being a stored no-op.
func TestPruneEventsOnceAppliesRetention(t *testing.T) {
	p, store, _ := newTestPoller(t, "")
	ctx := context.Background()

	set, err := store.GetSettings(ctx)
	if err != nil {
		t.Fatalf("GetSettings: %v", err)
	}
	set.EventRetentionDays = 7
	if err := store.UpdateSettings(ctx, set); err != nil {
		t.Fatalf("UpdateSettings: %v", err)
	}

	if _, err := store.InsertEvent(ctx, Event{
		Type: "health.down", Severity: "error", Source: "x", Message: "ancient",
		CreatedAt: time.Now().Add(-30 * 24 * time.Hour),
	}); err != nil {
		t.Fatalf("InsertEvent old: %v", err)
	}
	if _, err := store.InsertEvent(ctx, Event{Type: "health.up", Severity: "success", Source: "x", Message: "fresh"}); err != nil {
		t.Fatalf("InsertEvent new: %v", err)
	}

	p.PruneEventsOnce(ctx)

	evs, total, err := store.ListEvents(ctx, EventFilter{})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if total != 1 || evs[0].Message != "fresh" {
		t.Errorf("after the retention tick events = %+v, want only the fresh one", evs)
	}
}
