package api

import (
	"context"
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/events"
	"github.com/hugalafutro/model-hotel/internal/provider"
)

// Confirmation-probe schedule for ConfirmMissingModels: a model absent from
// the initial listing is only counted missing after these many extra listings,
// each preceded by the given delay (plus up to confirmProbeJitter of random
// jitter so fleet members and parallel providers don't probe in lockstep).
// Injectable so tests can zero the delays.
var confirmProbeDelays = []time.Duration{15 * time.Second, 45 * time.Second}

const confirmProbeJitter = 5 * time.Second

// suspectMissingFloor and suspectMissingRatio form the mass-vanish guard: a
// scan whose confirmed-missing set exceeds the floor AND the ratio of the
// provider's enabled models is treated as a broken listing, not a real
// removal, and records no misses. False-disabling dozens of models (which HA
// then propagates fleet-wide) is far worse than keeping a stale model enabled
// until an operator looks at the warning event.
const (
	suspectMissingFloor = 5
	suspectMissingRatio = 0.5
)

// suspectStreakThreshold is how many consecutive mass-vanish scans a provider
// must trip before discovery escalates from the quiet per-scan warning to a
// distinct, actionable "this looks like a real bulk removal" alert. Kept well
// above MissingScanThreshold so a brief upstream outage never reaches it: a
// transient broken listing recovers within a scan or two and resets the streak,
// whereas a genuine catalog sunset stays missing every scan. We never
// auto-disable on this signal (disabling half a provider's catalog propagates
// fleet-wide through HA); the alert asks an operator to disable by hand.
const suspectStreakThreshold = 3

// shouldEscalateSuspect reports whether a just-incremented consecutive
// mass-vanish count should raise the escalation alert. It fires on the crossing
// scan and then re-fires once every suspectStreakThreshold scans, so a
// persistent condition re-pings periodically without alerting on every scan.
func shouldEscalateSuspect(streak int) bool {
	return streak >= suspectStreakThreshold && streak%suspectStreakThreshold == 0
}

// SuspectStreak persists the per-provider consecutive-mass-vanish counter. It is
// nil on paths that must not touch the counter (unit tests, and any future
// caller without a pool); ConfirmMissingModels guards every use.
type SuspectStreak struct {
	exec func(pool *pgxpool.Pool, ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	pool *pgxpool.Pool
}

// NewSuspectStreak builds a pool-backed SuspectStreak for the discovery sweep.
func NewSuspectStreak(pool *pgxpool.Pool) *SuspectStreak {
	return &SuspectStreak{exec: dbExec, pool: pool}
}

// bump increments the provider's consecutive mass-vanish counter and, every
// suspectStreakThreshold consecutive scans, emits the louder escalation event.
// Counter errors are logged, not fatal: a missed escalation must never abort a
// scan.
func (s *SuspectStreak) bump(ctx context.Context, prov *provider.Provider, missing, enabled int) {
	if s == nil {
		return
	}
	var streak int
	if err := s.pool.QueryRow(ctx,
		`UPDATE providers SET suspect_scans = suspect_scans + 1 WHERE id = $1 RETURNING suspect_scans`,
		prov.ID,
	).Scan(&streak); err != nil {
		debuglog.Warn("discovery: failed to bump provider suspect streak", "provider", prov.Name, "error", err)
		return
	}
	if shouldEscalateSuspect(streak) {
		debuglog.Warn("discovery: provider suspected of bulk-removing models",
			"provider", prov.Name, "provider_id", prov.ID, "missing", missing, "enabled", enabled, "consecutive_scans", streak)
		events.Publish(events.Event{
			Type:     "discovery.bulk_removal_suspected",
			Severity: "error",
			Source:   "discovery",
			Message:  fmt.Sprintf("Provider '%s' has been missing %d of %d enabled models for %d consecutive scans. This looks like a real bulk removal rather than a broken listing; discovery will not auto-disable them. Review and disable the retired models by hand.", prov.Name, missing, enabled, streak),
			Metadata: map[string]any{"provider": prov.Name, "provider_id": prov.ID, "missing": missing, "enabled": enabled, "consecutive_scans": streak},
		})
	}
}

// reset clears the counter after a healthy scan (listing recovered). It is a
// no-op write when the counter is already zero, so the common healthy scan does
// not churn the row.
func (s *SuspectStreak) reset(ctx context.Context, prov *provider.Provider) {
	if s == nil {
		return
	}
	if _, err := s.exec(s.pool, ctx,
		`UPDATE providers SET suspect_scans = 0 WHERE id = $1 AND suspect_scans <> 0`, prov.ID); err != nil {
		debuglog.Warn("discovery: failed to reset provider suspect streak", "provider", prov.Name, "error", err)
	}
}

// confirmProbeSleep waits for the probe backoff, honouring ctx cancellation.
// Injectable for tests (which also exercise sleepWithContext directly).
var confirmProbeSleep = sleepWithContext

func sleepWithContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}

// ConfirmMissingModels gives absent models a second opinion before any miss is
// recorded. presentIDs is the initial listing's membership; every snapshot
// model that is enabled but unlisted triggers up to len(confirmProbeDelays)
// fresh listings with backoff, and the union of all probes' model IDs is
// returned as the confirmed membership. suspect=true means this scan's
// membership cannot be trusted (a confirmation probe failed, ctx was
// cancelled, or the mass-vanish guard tripped) and the caller must skip miss
// recording entirely. Probes affect membership only; metadata still comes
// exclusively from the initial listing's upserts.
// streak, when non-nil, tracks consecutive mass-vanish scans per provider: it is
// reset on any healthy scan and bumped (with escalation) when the mass-vanish
// guard trips, so a genuine bulk removal eventually raises a loud alert while a
// one-off broken listing does not. It is nil on paths that must not touch the
// counter (unit tests).
func ConfirmMissingModels(ctx context.Context, svc *provider.DiscoveryService, prov *provider.Provider, masterKey string, presentIDs []string, snapshot map[string]ModelSnapshot, streak *SuspectStreak) (confirmedPresent []string, suspect bool) {
	present := make(map[string]bool, len(presentIDs))
	for _, id := range presentIDs {
		present[id] = true
	}
	countMissing := func() int {
		n := 0
		for id, snap := range snapshot {
			if snap.enabled && !present[id] {
				n++
			}
		}
		return n
	}

	confirmedPresent = presentIDs
	missing := countMissing()
	if missing == 0 {
		streak.reset(ctx, prov)
		return confirmedPresent, false
	}

	for probe, delay := range confirmProbeDelays {
		//nolint:gosec // jitter, not crypto
		wait := delay + time.Duration(rand.Int64N(int64(confirmProbeJitter)))
		debuglog.Info("discovery: models absent from listing, running confirmation probe",
			"provider", prov.Name, "provider_id", prov.ID, "missing", missing,
			"probe", probe+1, "max_probes", len(confirmProbeDelays), "delay", wait)
		if err := confirmProbeSleep(ctx, wait); err != nil {
			return confirmedPresent, true
		}
		models, err := discoverModelsForConfirm(ctx, svc, prov, masterKey)
		if err != nil {
			// The provider answered the initial listing but not the probe: the
			// upstream is flapping, so this scan's membership proves nothing.
			debuglog.Warn("discovery: confirmation probe failed, treating scan as suspect",
				"provider", prov.Name, "provider_id", prov.ID, "probe", probe+1, "error", err)
			return confirmedPresent, true
		}
		for _, m := range models {
			if !present[m.ModelID] {
				present[m.ModelID] = true
				confirmedPresent = append(confirmedPresent, m.ModelID)
			}
		}
		missing = countMissing()
		if missing == 0 {
			debuglog.Info("discovery: confirmation probe found all absent models, no misses recorded",
				"provider", prov.Name, "provider_id", prov.ID, "probe", probe+1)
			streak.reset(ctx, prov)
			return confirmedPresent, false
		}
	}

	enabledCount := 0
	for _, snap := range snapshot {
		if snap.enabled {
			enabledCount++
		}
	}
	// A total blackout - the initial listing and every confirmation probe
	// returned no models at all while the provider still has enabled models - is
	// the strongest broken-listing signal there is, so treat it as suspect
	// regardless of the mass-vanish floor. A small provider (enabled <=
	// suspectMissingFloor) otherwise slips past the floor+ratio guard below with
	// suspect=false; RecordMissingModels's empty-list no-op then records nothing,
	// leaving every stale model enabled indefinitely with no operator signal. An
	// empty listing is a common provider quirk (see the discovery_*.go
	// empty-listing guards), so we still disable nothing here; the suspect event
	// and its escalation surface the condition for an operator instead.
	blackout := len(confirmedPresent) == 0 && enabledCount > 0
	massVanish := missing > suspectMissingFloor && float64(missing) > suspectMissingRatio*float64(enabledCount)
	if blackout || massVanish {
		debuglog.Warn("discovery: mass-vanish guard tripped, treating scan as suspect",
			"provider", prov.Name, "provider_id", prov.ID, "missing", missing, "enabled", enabledCount, "blackout", blackout)
		events.Publish(events.Event{
			Type:     "discovery.suspect_scan",
			Severity: "warning",
			Source:   "discovery",
			Message:  fmt.Sprintf("Discovery scan for %s is missing %d of %d enabled models even after confirmation probes; treating the listing as broken and disabling nothing", prov.Name, missing, enabledCount),
			Metadata: map[string]any{"provider": prov.Name, "provider_id": prov.ID, "missing": missing, "enabled": enabledCount},
		})
		// A recovered listing resets the streak; a persistent one crosses the
		// escalation threshold and raises the louder bulk-removal alert.
		streak.bump(ctx, prov, missing, enabledCount)
		return confirmedPresent, true
	}

	streak.reset(ctx, prov)
	return confirmedPresent, false
}
