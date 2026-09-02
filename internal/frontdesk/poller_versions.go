package frontdesk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

// PollVersionsOnce fetches the running version of each member that has a stored
// admin token, so the UI can flag version mismatches across the group.
func (p *Poller) PollVersionsOnce(ctx context.Context) {
	members, err := p.store.ListMembers(ctx)
	if err != nil {
		return
	}
	for _, m := range members {
		if !m.HasToken {
			// A member whose token was removed loses its build with it: the
			// config-sync gates read an empty version as "cannot confirm", and
			// a version kept from before the removal would vouch for a build
			// nothing can read any more.
			if p.clearBuild(m.ID) {
				p.publishMemberStatus(m.ID)
			}
			continue
		}
		token, ok, err := p.store.MemberToken(ctx, m.ID)
		if err != nil || !ok {
			if p.clearBuild(m.ID) {
				p.publishMemberStatus(m.ID)
			}
			continue
		}
		build, err := p.fetchMemberBuild(ctx, m.URL, token)
		if err != nil {
			p.noteVersionFetchFailure(ctx, m, err)
			// A build that can no longer be read is unknown, and the config-sync
			// gates treat unknown as skewed (fail closed). Keeping the last good
			// value would let a sync proceed on stale data while the member is
			// mid-upgrade, the window the gate exists for. The commit is cleared
			// with the version: kept beside a blank version it would outlive the
			// read that vouched for it.
			if p.clearBuild(m.ID) {
				p.publishMemberStatus(m.ID)
			}
			continue
		}
		p.mu.Lock()
		cur := p.statuses[m.ID]
		versionChanged := cur.Version != build.Version || cur.Commit != build.Commit
		cur.Version = build.Version
		cur.Commit = build.Commit
		p.statuses[m.ID] = cur
		wasAlerting := p.versionFailures[m.ID] >= versionFetchFailThreshold
		delete(p.versionFailures, m.ID)
		p.mu.Unlock()
		if versionChanged {
			// First successful read (or a build change) for this member: refresh
			// the UI so the Version column populates without a manual reload. A
			// commit-only move counts: on a "dev" fleet it is the whole of what
			// a rebuild changes.
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

// clearBuild drops a member's version and commit together and reports whether
// there was a version to drop, so the caller refreshes the UI only on a
// change. The commit is cleared with the version: kept on its own it would
// outlive the read that vouched for it.
func (p *Poller) clearBuild(memberID string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	cur := p.statuses[memberID]
	had := cur.Version != ""
	cur.Version = ""
	cur.Commit = ""
	p.statuses[memberID] = cur
	return had
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
		// Logged separately from the event (which logEvent mirrors) because
		// the error itself must stay out of the persisted payload.
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

// fetchMemberBuild reads app_version and app_commit from the member's admin
// settings API. Both ride in one response, so the commit costs no extra call.
func (p *Poller) fetchMemberBuild(ctx context.Context, baseURL, token string) (memberBuild, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+memberSettingsPath, http.NoBody)
	if err != nil {
		return memberBuild{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := p.client.Do(req)
	if err != nil {
		return memberBuild{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return memberBuild{}, fmt.Errorf("settings api returned %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return memberBuild{}, err
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		// Don't wrap the decoder error: it can echo a fragment of the response.
		return memberBuild{}, errors.New("frontdesk: parse settings response")
	}
	var out memberBuild
	if v, ok := payload["app_version"].(string); ok {
		out.Version = v
	}
	// A member too old to report app_commit leaves it empty, which buildSkew
	// reads as "cannot vouch" and falls back to the version comparison.
	if c, ok := payload["app_commit"].(string); ok {
		out.Commit = c
	}
	return out, nil
}

// The three Traefik staleness checks below subtract raw, without the
// util.TrustedAge guard the settings- and database-backed staleness checks use.
// Both operands are time.Time values taken by time.Now() in THIS process (p.now
// is time.Now; lastConfigPollAt is assigned in RecordConfigPoll;
// Server.startedAt in NewServer), so each carries a monotonic reading and Sub
// uses the monotonic clock: no wall-clock step can make those differences
// negative, which is the failure the guard exists for. A time that crosses a
// boundary, parsed from RFC3339, read back from a row, or passed through
// .UTC()/.Round()/.Truncate(), loses the reading and does need the guard.

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

// ConfigPollStale reports whether Traefik has stopped fetching the dynamic
// config past the configured threshold: the same rule checkConfigStaleness
// alerts on, exposed side-effect-free for the fleet state machine
// (fleetstate.go). False while unarmed (nothing has ever polled), matching the
// watchdog's fresh-start grace.
func (p *Poller) ConfigPollStale(ctx context.Context) bool {
	threshold := secs(p.settings(ctx).TraefikStaleSecs, 30)
	p.mu.RLock()
	last := p.lastConfigPollAt
	p.mu.RUnlock()
	return !last.IsZero() && p.now().Sub(last) > threshold
}

// ConfigPollWarm reports whether the Traefik staleness input has produced a
// real observation this process: either Traefik has fetched the config at least
// once, or a full staleness window has elapsed since `since` (process start)
// without a fetch, at which point the silence is itself the steady-state
// observation ConfigPollStale keeps reporting. Used by fleetInputsWarm to keep
// a cold start from reading as a recovery.
func (p *Poller) ConfigPollWarm(ctx context.Context, since time.Time) bool {
	p.mu.RLock()
	armed := !p.lastConfigPollAt.IsZero()
	p.mu.RUnlock()
	return armed || p.now().Sub(since) > secs(p.settings(ctx).TraefikStaleSecs, 30)
}

// checkAutoSyncStale emits a single warning when auto-sync is off and the fleet
// has not been synced within autoSyncStaleThreshold (autoSyncStale holds the
// exact rule). Like checkConfigStaleness it de-dups on an in-memory flag so it
// fires once per stale episode, not every tick; the flag disarms silently when
// the condition clears (auto-sync re-enabled, or a fresh sync recorded), so a
// later stale episode alerts again. A restart resets the flag, so an
// already-stale fleet re-alerts once, as with the other staleness checks.
func (p *Poller) checkAutoSyncStale(ctx context.Context) {
	cfg, err := p.store.GetAutoSync(ctx)
	if err != nil {
		debuglog.Warn("frontdesk: auto-sync staleness: read config", "error", err)
		return
	}
	state, found, err := p.store.GetFleetSyncState(ctx)
	if err != nil {
		debuglog.Warn("frontdesk: auto-sync staleness: read fleet sync state", "error", err)
		return
	}
	stale := autoSyncStale(cfg, state.LastRunAt, found, p.now())

	p.mu.Lock()
	notified := p.autoSyncStaleNotified
	if stale != notified {
		p.autoSyncStaleNotified = stale
	}
	p.mu.Unlock()

	if stale && !notified {
		p.recordEvent(ctx, Event{
			Type: "config.autosync_stale", Severity: "warning", Source: "frontdesk-poller",
			Message: "Auto-sync is off and the fleet has not been synced in over a day; replicas may be drifting from the primary",
		})
	}
}
