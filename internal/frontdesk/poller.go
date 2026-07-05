package frontdesk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/events"
)

// This file holds the background pollers: a per-member /health probe (status +
// latency, with up/down transition events), a poller for Traefik's own
// serverStatus view (so the UI can show both "Front Desk sees up/down" and
// "Traefik sees up/down" for split-brain diagnostics), a member version
// fetcher, and the "Traefik hasn't polled config for > N seconds" watchdog,
// which is the one silent failure mode of the HTTP-provider design.
//
// All control-plane facts are persisted to the event log AND published on the
// SSE bus. No request or prompt content is ever read or logged.

const (
	memberHealthPath   = "/health"
	memberSettingsPath = "/api/settings"
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
	// AutoSyncVerifiedAt is the last time the auto-syncer confirmed this member
	// matches the primary (a real write, a self-converged empty diff, or a quiet
	// verify tick on an already-converged fleet). It is the live "auto-sync is
	// running" heartbeat that advances ~every tick while the member is reachable,
	// distinct from last_config_sync_at, which moves only on a real config write.
	// A pointer so a never-verified member serializes as absent, not a zero time.
	AutoSyncVerifiedAt *time.Time `json:"auto_sync_verified_at,omitempty"`
}

// Poller probes members and Traefik on intervals taken from settings.
type Poller struct {
	store      *Store
	bus        *events.Bus
	client     *http.Client
	traefikAPI string
	now        func() time.Time

	mu               sync.RWMutex
	statuses         map[string]MemberStatus // keyed by member ID
	lastConfigPollAt time.Time
	staleNotified    bool
	versionFailures  map[string]int // consecutive version-fetch failures, keyed by member ID
	healthFailures   map[string]int // consecutive failed health polls, keyed by member ID
	traefikNonUp     map[string]int // consecutive non-UP Traefik observations, keyed by member ID
}

// NewPoller builds a Poller. traefikAPI is the base URL of the Traefik API
// (e.g. http://traefik:8080) reachable only on the compose-internal network; an
// empty value disables Traefik status polling.
func NewPoller(store *Store, bus *events.Bus, traefikAPI string) *Poller {
	if bus == nil {
		bus = events.DefaultBus
	}
	return &Poller{
		store:           store,
		bus:             bus,
		client:          newProbeClient(httpProbeTimeout),
		traefikAPI:      strings.TrimRight(traefikAPI, "/"),
		now:             time.Now,
		statuses:        make(map[string]MemberStatus),
		versionFailures: make(map[string]int),
		healthFailures:  make(map[string]int),
		traefikNonUp:    make(map[string]int),
	}
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
	for k, v := range p.statuses {
		out[k] = v
	}
	return out
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
		{func(s Settings) time.Duration { return secs(s.TraefikPollSecs, 5) }, p.checkConfigStaleness},
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
		hs.Error = err.Error()
		return hs
	}
	resp, err := p.client.Do(req)
	hs.LatencyMs = p.now().Sub(start).Milliseconds()
	if err != nil {
		hs.Error = err.Error()
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
// it is reported down (an error event plus, by default, an Apprise alert). This
// tolerates the brief unreachability of a routine container rebuild without
// flapping. Recovery is immediate: the first healthy poll clears the count and,
// if the member had been reported down, announces it back up.
//
// The reported badge follows the same rule as the version poller: during the
// grace window (below threshold) the last known-good status is kept, so the
// dashboard does not flicker red on every rebuild. A first observation that is
// healthy is recorded silently (baseline).
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
		debuglog.Warn("frontdesk: member health failing",
			"member", m.Name, "consecutive_failures", fails, "error", hs.Error)
		p.recordEvent(ctx, Event{
			Type: "health.down", Severity: "error", Source: "frontdesk-poller",
			Message: fmt.Sprintf("%s is unreachable after %d checks", m.Name, fails), MemberID: m.ID,
			Metadata: map[string]any{"error": hs.Error, "consecutive_failures": fails},
		})
	}
}

// memberAnnounce is the heartbeat body Front Desk POSTs to each member's
// /api/fleet/announce. It carries only routing metadata: whether the member is
// the fleet primary, and the primary's display name for the member's tooltip.
type memberAnnounce struct {
	IsPrimary   bool   `json:"is_primary"`
	PrimaryName string `json:"primary_name,omitempty"`
}

// PollAnnounceOnce tells every reachable, tokened member that Front Desk is in
// contact, and which member is the fleet primary. It is the producing half of
// HA Phase 6: a member uses these announces to light up the HA line on its own
// dashboard and to self-clear it when they stop. Best-effort, exactly like the
// health poll: a member that is down, has no stored token, or runs an older
// build without the endpoint is silently skipped, never retried or surfaced.
func (p *Poller) PollAnnounceOnce(ctx context.Context) {
	members, err := p.store.ListMembers(ctx)
	if err != nil {
		debuglog.Warn("frontdesk: poll announce: list members", "error", err)
		return
	}
	// The recorded last-sync marker names the fleet primary. Absent (no sync has
	// ever run) means no member is flagged primary yet; the membership signal is
	// still worth sending, so continue without a primary rather than abort.
	state, hasPrimary, err := p.store.GetFleetSyncState(ctx)
	if err != nil {
		debuglog.Warn("frontdesk: poll announce: fleet sync state", "error", err)
		hasPrimary = false
	}
	for _, m := range members {
		token, ok, err := p.store.MemberToken(ctx, m.ID)
		if err != nil || !ok {
			continue // no stored token: the announce endpoint needs admin auth
		}
		ann := memberAnnounce{IsPrimary: hasPrimary && m.ID == state.PrimaryID}
		if hasPrimary {
			ann.PrimaryName = state.PrimaryName
		}
		if err := p.announceToMember(ctx, m.URL, token, ann); err != nil {
			debuglog.Debug("frontdesk: announce to member failed", "member", m.ID, "error", err)
		}
	}
}

// announceToMember POSTs one heartbeat through the guarded probe client (the
// same SSRF-protected client the health poll uses), carrying the member's admin
// Bearer token. A non-204 reply is an error so the caller can log-and-continue.
func (p *Poller) announceToMember(ctx context.Context, baseURL, token string, ann memberAnnounce) error {
	body, err := json.Marshal(ann)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+memberAnnouncePath, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<10))
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("announce returned %d", resp.StatusCode)
	}
	return nil
}

// PollTraefikOnce fetches Traefik's serverStatus and maps it onto members by URL.
func (p *Poller) PollTraefikOnce(ctx context.Context) {
	if p.traefikAPI == "" {
		return
	}
	statusByURL, err := p.fetchTraefikServerStatus(ctx)
	if err != nil {
		debuglog.Debug("frontdesk: poll traefik status", "error", err)
		return
	}
	members, err := p.store.ListMembers(ctx)
	if err != nil {
		return
	}
	// Damp the UP->non-UP flip with the same consecutive-miss threshold as health:
	// Traefik briefly stops listing a member (or marks it DOWN) during a rebuild,
	// and committing that immediately flaps the badge. "UP" is applied at once
	// (recovery); a non-UP status is held back until it has been observed
	// `threshold` polls in a row.
	threshold := p.healthFailThreshold(ctx)
	p.mu.Lock()
	var changed []string
	for _, m := range members {
		cur := p.statuses[m.ID]
		next := statusByURL[m.URL] // "" when Traefik does not list it
		if next == "UP" {
			delete(p.traefikNonUp, m.ID)
		} else {
			p.traefikNonUp[m.ID]++
			if p.traefikNonUp[m.ID] < threshold {
				continue // tolerate a rebuild blink; keep the last reported status
			}
		}
		if cur.TraefikStatus != next {
			cur.TraefikStatus = next
			p.statuses[m.ID] = cur
			changed = append(changed, m.ID)
		}
	}
	p.mu.Unlock()
	// Traefik's view caught up to a new/changed member (it needs to re-poll the
	// config before it lists one), so refresh the UI without a manual reload.
	for _, id := range changed {
		p.publishMemberStatus(id)
	}
}

func (p *Poller) fetchTraefikServerStatus(ctx context.Context) (map[string]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.traefikAPI+traefikServicesAPI, http.NoBody)
	if err != nil {
		return nil, err
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("traefik api returned %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	return parseTraefikServerStatus(body)
}

// traefikServiceInfo is the subset of a Traefik API service object we read.
type traefikServiceInfo struct {
	Name         string            `json:"name"`
	ServerStatus map[string]string `json:"serverStatus"`
}

// parseTraefikServerStatus extracts the server-URL -> status map for the hotel
// service from a Traefik /api/http/services response.
func parseTraefikServerStatus(body []byte) (map[string]string, error) {
	var services []traefikServiceInfo
	if err := json.Unmarshal(body, &services); err != nil {
		// Don't wrap the decoder error: it can echo a fragment of the response.
		return nil, errors.New("frontdesk: parse traefik services response")
	}
	for _, svc := range services {
		// Traefik names HTTP-provider services like "hotel@http".
		if svc.Name == traefikServiceName || strings.HasPrefix(svc.Name, traefikServiceName+"@") {
			return svc.ServerStatus, nil
		}
	}
	return map[string]string{}, nil
}

// PollVersionsOnce fetches the running version of each member that has a stored
// admin token, so the UI can flag version mismatches across the group.
func (p *Poller) PollVersionsOnce(ctx context.Context) {
	members, err := p.store.ListMembers(ctx)
	if err != nil {
		return
	}
	for _, m := range members {
		if !m.HasToken {
			continue
		}
		token, ok, err := p.store.MemberToken(ctx, m.ID)
		if err != nil || !ok {
			continue
		}
		version, err := p.fetchMemberVersion(ctx, m.URL, token)
		if err != nil {
			p.noteVersionFetchFailure(ctx, m, err)
			continue
		}
		p.mu.Lock()
		cur := p.statuses[m.ID]
		versionChanged := cur.Version != version
		cur.Version = version
		p.statuses[m.ID] = cur
		wasAlerting := p.versionFailures[m.ID] >= versionFetchFailThreshold
		delete(p.versionFailures, m.ID)
		p.mu.Unlock()
		if versionChanged {
			// First successful read (or a version bump) for this member: refresh
			// the UI so the Version column populates without a manual reload.
			p.publishMemberStatus(m.ID)
		}
		if wasAlerting {
			p.recordEvent(ctx, Event{
				Type:     "version.fetch_recovered",
				Severity: "success",
				Source:   "frontdesk-poller",
				Message:  fmt.Sprintf("Recovered version reads from %s", m.Name),
				MemberID: m.ID,
			})
		}
	}
}

// noteVersionFetchFailure tracks consecutive version-fetch failures for a member
// and raises a single visible warning + event when they cross the threshold. The
// member's admin token is sent on every attempt, so a persistently failing
// (possibly hostile or misconfigured) URL is surfaced for the operator rather
// than retried silently at Debug level forever. The fetch error is logged but
// never put in the event payload (it can embed a fragment of the member's HTTP
// response).
func (p *Poller) noteVersionFetchFailure(ctx context.Context, m *Member, fetchErr error) {
	p.mu.Lock()
	p.versionFailures[m.ID]++
	n := p.versionFailures[m.ID]
	p.mu.Unlock()

	if n == versionFetchFailThreshold {
		debuglog.Warn("frontdesk: member version fetch failing",
			"member", m.Name, "consecutive_failures", n, "error", fetchErr)
		p.recordEvent(ctx, Event{
			Type:     "version.fetch_failed",
			Severity: "warning",
			Source:   "frontdesk-poller",
			Message:  fmt.Sprintf("Cannot read version from %s after %d attempts; check the member URL", m.Name, n),
			MemberID: m.ID,
			Metadata: map[string]any{"consecutive_failures": n},
		})
		return
	}
	debuglog.Debug("frontdesk: fetch member version", "member", m.Name, "error", fetchErr)
}

// fetchMemberVersion reads app_version from the member's admin settings API.
func (p *Poller) fetchMemberVersion(ctx context.Context, baseURL, token string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+memberSettingsPath, http.NoBody)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := p.client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("settings api returned %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		// Don't wrap the decoder error: it can echo a fragment of the response.
		return "", errors.New("frontdesk: parse settings response")
	}
	if v, ok := payload["app_version"].(string); ok {
		return v, nil
	}
	return "", nil
}

// checkConfigStaleness emits a single warning when Traefik has not polled the
// dynamic config within the configured threshold. It resets on the next poll
// (RecordConfigPoll), so a recovered provider re-arms the warning.
func (p *Poller) checkConfigStaleness(ctx context.Context) {
	threshold := secs(p.settings(ctx).TraefikStaleSecs, 30)

	p.mu.Lock()
	last := p.lastConfigPollAt
	notified := p.staleNotified
	// Never polled yet: arm from "now" so a fresh start does not immediately warn.
	if last.IsZero() {
		p.lastConfigPollAt = p.now()
		p.mu.Unlock()
		return
	}
	stale := p.now().Sub(last) > threshold
	if stale && !notified {
		p.staleNotified = true
	}
	p.mu.Unlock()

	if stale && !notified {
		p.recordEvent(ctx, Event{
			Type: "traefik.stale", Severity: "warning", Source: "frontdesk-poller",
			Message: fmt.Sprintf("Traefik has not fetched the config for over %s", threshold),
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
