package frontdesk

import (
	"context"
	"fmt"
	"io"
	"maps"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/events"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// The background pollers: a per-member /health probe (status and latency, with
// up/down transition events), a poller for Traefik's own serverStatus view (so
// the UI can show both "Front Desk sees up/down" and "Traefik sees up/down" for
// split-brain diagnostics), a member version fetcher, and the "Traefik has not
// polled config for > N seconds" watchdog, the one silent failure mode of the
// HTTP-provider design.
//
// All control-plane facts are persisted to the event log AND published on the
// SSE bus. No request or prompt content is ever read or logged.

const (
	memberHealthPath   = "/health"
	memberSettingsPath = "/api/settings"
	memberSystemPath   = "/api/system/"
	memberAnnouncePath = "/api/fleet/announce"
	traefikServicesAPI = "/api/http/services"

	// httpProbeTimeout bounds a single member or Traefik HTTP probe.
	httpProbeTimeout = 4 * time.Second

	// versionFetchFailThreshold is the number of consecutive version-fetch
	// failures for a member before a single visible warning + event is raised.
	// The member's admin token is sent on every attempt, so a persistently
	// failing URL must be surfaced, not retried silently forever.
	versionFetchFailThreshold = 3
)

// HealthStatus is the Front Desk poller's view of a member's /health endpoint.
type HealthStatus struct {
	Known     bool      `json:"known"`
	Healthy   bool      `json:"healthy"`
	LatencyMs int64     `json:"latency_ms"`
	CheckedAt time.Time `json:"checked_at"`
	Error     string    `json:"error,omitempty"`
}

// MemberStatus is the live, in-memory status the Members tab renders. It is not
// persisted; only transitions are written to the event log.
type MemberStatus struct {
	Health        HealthStatus `json:"health"`
	TraefikStatus string       `json:"traefik_status,omitempty"` // "UP" / "DOWN" / "" (unknown)
	Version       string       `json:"version,omitempty"`
	// Commit is the source commit the member's binary was built from, read from
	// the same settings response as Version. It is what distinguishes two builds
	// on a fleet whose images all report the "dev" placeholder version, so it is
	// serialized as well as gated on.
	Commit string `json:"commit,omitempty"`
	// AutoSyncVerifiedAt is the last time the auto-syncer confirmed this member
	// matches the primary (a real write, a self-converged empty diff, or a quiet
	// verify tick on an already-converged fleet). It is the live "auto-sync is
	// running" heartbeat that advances ~every tick while the member is reachable,
	// distinct from last_config_sync_at, which moves only on a real config write.
	// A pointer so a never-verified member serializes as absent, not a zero time.
	AutoSyncVerifiedAt *time.Time `json:"auto_sync_verified_at,omitempty"`
	// Circuits is the member's own circuit-breaker ledger as last read from its
	// status API (poller_circuits.go): the circuits it holds open or owes a
	// probe, each with its cause. nil until the first successful read, and
	// cleared on a failed one, so the Members tab never shows a stale ledger
	// as current. Not persisted.
	Circuits *MemberCircuits `json:"circuits,omitempty"`
}

// Poller probes members and Traefik on intervals taken from settings.
type Poller struct {
	store      *Store
	bus        *events.Bus
	client     *http.Client
	traefikAPI string
	now        func() time.Time

	// frontdeskID is this Front Desk's persistent identity, stamped onto every
	// announce so a member can tell which Front Desk owns its fleet role. Set
	// once at startup via SetFrontdeskID; current members reject an announce
	// without it.
	frontdeskID string

	mu                    sync.RWMutex
	statuses              map[string]MemberStatus // keyed by member ID
	lastConfigPollAt      time.Time
	staleNotified         bool
	autoSyncStaleNotified bool
	versionFailures       map[string]int  // consecutive version-fetch failures, keyed by member ID
	healthFailures        map[string]int  // consecutive failed health polls, keyed by member ID
	traefikNonUp          map[string]int  // consecutive non-UP Traefik observations, keyed by member ID
	traefikAPIFails       int             // consecutive failed Traefik API polls (whole-API, not per member)
	traefikBlanked        bool            // true once a Traefik outage has blanked all badges; skips re-blank + member re-reads until recovery
	conflictNotified      map[string]bool // members that rejected our announce (409), keyed by member ID
}

// NewPoller builds a Poller. traefikAPI is the base URL of the Traefik API
// (e.g. http://traefik:8080) reachable only on the compose-internal network; an
// empty value disables Traefik status polling.
func NewPoller(store *Store, bus *events.Bus, traefikAPI string) *Poller {
	if bus == nil {
		bus = events.DefaultBus
	}
	return &Poller{
		store:            store,
		bus:              bus,
		client:           newProbeClient(httpProbeTimeout),
		traefikAPI:       strings.TrimRight(traefikAPI, "/"),
		now:              time.Now,
		statuses:         make(map[string]MemberStatus),
		versionFailures:  make(map[string]int),
		healthFailures:   make(map[string]int),
		traefikNonUp:     make(map[string]int),
		conflictNotified: make(map[string]bool),
	}
}

// SetFrontdeskID records this Front Desk's persistent identity, stamped onto
// every subsequent announce. Called once at startup after the ID is resolved
// from the store.
func (p *Poller) SetFrontdeskID(id string) {
	p.frontdeskID = id
}

// RecordConfigPoll marks that Traefik just fetched the dynamic config. The
// config handler calls this; the watchdog uses it to detect a stalled provider.
func (p *Poller) RecordConfigPoll() {
	p.mu.Lock()
	p.lastConfigPollAt = p.now()
	p.staleNotified = false
	p.mu.Unlock()
}

// Snapshot returns a copy of the current per-member status map.
func (p *Poller) Snapshot() map[string]MemberStatus {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make(map[string]MemberStatus, len(p.statuses))
	maps.Copy(out, p.statuses)
	return out
}

// memberBuildOf returns the last successfully polled build for a member: its
// app_version and the commit that version was built from. Both are zero when
// the member has never been polled or its last fetch failed. It is the read the
// config-sync gates consult; an empty version means "cannot confirm", which
// they treat as skewed (fail closed).
func (p *Poller) memberBuildOf(id string) memberBuild {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return buildOf(p.statuses[id])
}

// buildOf reads a member's build out of a polled status. One extraction, so a
// caller working from an already-taken snapshot cannot drift from
// memberBuildOf.
func buildOf(st MemberStatus) memberBuild {
	return memberBuild{Version: st.Version, Commit: st.Commit}
}

// SetAutoSyncVerified records that the auto-syncer just confirmed the member is
// in sync with the primary, advancing the live "auto-sync is running" heartbeat
// the Members tab renders. It read-modify-writes under the same lock the health
// loop uses, so a concurrent health probe (which copies the whole MemberStatus)
// cannot drop the marker. In-memory only: it resets on restart and repopulates
// within a tick, which is exactly what a liveness signal should do.
func (p *Poller) SetAutoSyncVerified(memberID string, at time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	st := p.statuses[memberID]
	st.AutoSyncVerifiedAt = &at
	p.statuses[memberID] = st
}

// Run starts the poll loops and blocks until ctx is cancelled. Each loop reads
// the current settings every tick so interval changes take effect live.
func (p *Poller) Run(ctx context.Context) {
	var wg sync.WaitGroup
	loops := []struct {
		interval func(Settings) time.Duration
		fn       func(context.Context)
	}{
		{func(s Settings) time.Duration { return secs(s.HealthPollSecs, 5) }, p.PollHealthOnce},
		{func(s Settings) time.Duration { return secs(s.TraefikPollSecs, 5) }, p.PollTraefikOnce},
		{func(s Settings) time.Duration { return secs(s.HealthPollSecs, 5) }, p.PollVersionsOnce},
		// Three health polls apart: the member caches this status for 5s, so
		// the health cadence would recompute it on every read (poller_circuits.go).
		{func(s Settings) time.Duration { return 3 * secs(s.HealthPollSecs, 5) }, p.PollCircuitsOnce},
		{func(s Settings) time.Duration { return secs(s.TraefikPollSecs, 5) }, p.checkConfigStaleness},
		{func(s Settings) time.Duration { return secs(s.TraefikPollSecs, 5) }, p.checkAutoSyncStale},
		{func(s Settings) time.Duration { return secs(s.HealthPollSecs, 5) }, p.PollAnnounceOnce},
	}
	for _, l := range loops {
		wg.Add(1)
		go func(interval func(Settings) time.Duration, fn func(context.Context)) {
			defer wg.Done()
			p.tickLoop(ctx, interval, fn)
		}(l.interval, l.fn)
	}
	wg.Wait()
}

func (p *Poller) tickLoop(ctx context.Context, interval func(Settings) time.Duration, fn func(context.Context)) {
	for {
		fn(ctx)
		d := interval(p.settings(ctx))
		select {
		case <-ctx.Done():
			return
		case <-time.After(d):
		}
	}
}

func (p *Poller) settings(ctx context.Context) Settings {
	set, err := p.store.GetSettings(ctx)
	if err != nil {
		debuglog.Warn("frontdesk: poller using default settings", "error", err)
		return Settings{HealthPollSecs: 5, TraefikPollSecs: 5, TraefikStaleSecs: 30, HealthFailThreshold: 3}
	}
	return set
}

// healthFailThreshold is the number of consecutive failed polls a member must
// accrue before it is reported down. It also damps the Traefik UP->DOWN flip.
// Defaults to 3 when unset or invalid so a bad/zero row never disables damping.
func (p *Poller) healthFailThreshold(ctx context.Context) int {
	if t := p.settings(ctx).HealthFailThreshold; t >= 1 {
		return t
	}
	return 3
}

// PollHealthOnce probes every member's /health and records up/down transitions.
func (p *Poller) PollHealthOnce(ctx context.Context) {
	start := p.now()
	defer func() { observePollDuration("health", p.now().Sub(start)) }()
	members, err := p.store.ListMembers(ctx)
	if err != nil {
		debuglog.Warn("frontdesk: poll health: list members", "error", err)
		return
	}
	for _, m := range members {
		hs := p.checkHealth(ctx, m.URL)
		p.applyHealth(ctx, m, hs)
	}
}

// checkHealth performs one /health GET and returns the observed status.
func (p *Poller) checkHealth(ctx context.Context, baseURL string) HealthStatus {
	start := p.now()
	hs := HealthStatus{Known: true, CheckedAt: start}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+memberHealthPath, http.NoBody)
	if err != nil {
		// redactErrURL: the status is monitor-readable and the dial error embeds
		// the member URL, which can carry userinfo on a pre-rejection row.
		hs.Error = redactErrURL(err)
		return hs
	}
	resp, err := p.client.Do(req)
	hs.LatencyMs = p.now().Sub(start).Milliseconds()
	if err != nil {
		// redactErrURL: the status is monitor-readable and the dial error embeds
		// the member URL, which can carry userinfo on a pre-rejection row.
		hs.Error = redactErrURL(err)
		return hs
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<10))

	if resp.StatusCode == http.StatusOK {
		hs.Healthy = true
	} else {
		hs.Error = fmt.Sprintf("health returned %d", resp.StatusCode)
	}
	return hs
}

// applyHealth records a health probe and emits an up/down transition event,
// debounced so a member must miss `health_fail_threshold` polls in a row before
// it is reported down (an error event plus, by default, an Apprise alert). That
// tolerates the brief unreachability of a routine container rebuild without
// flapping. Recovery is immediate: the first healthy poll clears the count and,
// if the member had been reported down, announces it back up.
//
// During the grace window (below threshold) the badge keeps the last known-good
// status, so the dashboard does not flicker red on every rebuild. A first
// observation that is healthy is recorded silently as the baseline.
func (p *Poller) applyHealth(ctx context.Context, m *Member, hs HealthStatus) {
	threshold := p.healthFailThreshold(ctx)

	p.mu.Lock()
	prev, had := p.statuses[m.ID]
	cur := prev
	priorFails := p.healthFailures[m.ID]

	var fails int
	switch {
	case hs.Healthy:
		delete(p.healthFailures, m.ID)
		cur.Health = hs
	default:
		p.healthFailures[m.ID]++
		fails = p.healthFailures[m.ID]
		// Only let a "down" reach the badge once it is confirmed; below the
		// threshold keep the last known status (zero-value "unknown" for a
		// never-seen member) so a rebuild blip does not render red.
		if fails >= threshold {
			cur.Health = hs
		}
	}
	p.statuses[m.ID] = cur
	p.mu.Unlock()

	// The rendered badge is a function of both Known and Healthy (unknown vs
	// up vs down), so compare both: a never-seen member that crosses straight
	// from "unknown" to confirmed-down flips Known without flipping Healthy, and
	// would otherwise be nudged only by the health.down event, not the badge.
	badgeChanged := !had ||
		prev.Health.Healthy != cur.Health.Healthy ||
		prev.Health.Known != cur.Health.Known
	if badgeChanged {
		// Nudge connected UIs to refetch the changed badge. Without this a
		// freshly added, healthy member shows no status until an unrelated event
		// fires or the operator reloads.
		p.publishMemberStatus(m.ID)
	}

	switch {
	case hs.Healthy && priorFails >= threshold:
		// Recovered from a state we had actually reported down.
		p.recordEvent(ctx, Event{
			Type: "health.up", Severity: "success", Source: "frontdesk-poller",
			Message: fmt.Sprintf("%s is healthy", m.Name), MemberID: m.ID,
			Metadata: map[string]any{"latency_ms": hs.LatencyMs},
		})
	case !hs.Healthy && fails == threshold:
		// Crossed into confirmed-down: emit exactly once, not on every later poll.
		p.recordEvent(ctx, Event{
			Type: "health.down", Severity: "error", Source: "frontdesk-poller",
			Message: fmt.Sprintf("%s is unreachable after %d %s", m.Name, fails, util.Plural(fails, "check", "checks")), MemberID: m.ID,
			Metadata: map[string]any{"error": hs.Error, "consecutive_failures": fails},
		})
	}
}

// recordEvent persists a control-plane event and publishes it on the SSE bus.
func (p *Poller) recordEvent(ctx context.Context, e Event) {
	stored, err := p.store.InsertEvent(ctx, e)
	if err != nil {
		debuglog.Warn("frontdesk: persist event", "type", e.Type, "error", err)
		stored = e
	}
	logEvent(stored)
	p.bus.Publish(busEvent(stored))
}

// publishMemberStatus emits a bus-only signal that a member's live status
// snapshot changed in a way the Members tab renders, so connected UIs refetch
// promptly instead of waiting for an unrelated event or a manual reload. It is
// deliberately NOT persisted to the event log: these are frequent UI nudges,
// not control-plane facts, and would otherwise clutter the Events tab. It only
// fires on an actual change, so a quiet fleet produces no traffic.
func (p *Poller) publishMemberStatus(memberID string) {
	p.bus.Publish(events.Event{
		Type:      "member.status",
		Severity:  "info",
		Source:    "frontdesk-poller",
		Metadata:  map[string]any{"member_id": memberID},
		Timestamp: p.now(),
	})
}

func secs(n, fallback int) time.Duration {
	if n < 1 {
		n = fallback
	}
	return time.Duration(n) * time.Second
}
