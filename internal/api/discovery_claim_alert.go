package api

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/events"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// EventTypeClaimsOutstanding is published when the oldest counted discovery
// claim has been sitting in the Models badge for longer than the operator's
// configured threshold. It is the nudge that replaces staring at the badge:
// the badge itself is a passive count, and an operator who does not open the
// dashboard never learns that models stopped working.
const EventTypeClaimsOutstanding = "discovery.claims_outstanding"

// ClaimWindowDays is ClaimWindow in whole days. Derived, never written as a
// literal, so the operator-facing ceiling below and the settings UI move
// together with the constant.
const ClaimWindowDays = int(ClaimWindow / (24 * time.Hour))

// MaxClaimAlertDays is the highest age threshold the outstanding-claims alert
// can be given, and it is deliberately one day BELOW the claim window rather
// than equal to it.
//
// A gone model stops counting the moment its age exceeds ClaimWindow: the
// stale predicate in buildProviderClaims is strictly greater, so at that
// instant the claim leaves the badge count entirely. The alert fires when the
// oldest still-COUNTED claim exceeds the threshold. Set the threshold to
// exactly ClaimWindow and those two conditions have no overlap at all: the
// claim ages out of the count at precisely the instant the alert would
// trigger. The firing window would be measure-zero, no real scan interval
// would ever land inside it, and the alert would look configured while being
// silently dead forever. One whole day of margin means a crossed claim is
// still counted across at least one full discovery cycle.
const MaxClaimAlertDays = ClaimWindowDays - 1

// DefaultClaimAlertDays is the shipped threshold: a week of a discrepancy
// going unlooked-at is long enough to be a real oversight and short enough
// that the alert still arrives well inside the claim window.
const DefaultClaimAlertDays = 7

// SettingKeyClaimAlertDays is the operator-tunable threshold, in whole days.
// It is in BOTH settings allowlists (api.allowedSettings and
// settings.AllowedSettings); TestAllowedSettingsSync does not catch a key
// present in only one of them, so both were verified by hand.
const SettingKeyClaimAlertDays = "discovery_claim_alert_days"

// settingKeyClaimAlertFired is the edge latch that stops the alert re-firing
// on every discovery scan while the threshold stays crossed. It reproduces the
// circuit breaker's discipline (internal/failover/circuitbreaker.go publishes
// only on a state TRANSITION, never per failure), with one difference forced
// by where the evaluation runs: the breaker keeps its state machine in memory
// because it is consulted on every proxied request, whereas this is evaluated
// once per discovery cycle by a process that may restart between cycles. An
// in-memory latch would re-fire the same alert on every restart, so the edge
// is persisted. It is an internal `_`-prefixed key like
// _discovery_last_reviewed_at and the _fleet_* keys, deliberately NOT in
// either settings allowlist: it is bookkeeping, not an operator setting.
const settingKeyClaimAlertFired = "_discovery_claim_alert_fired_at"

// claimAlertWorstProviders caps how many providers the payload names. The
// point is to aim the operator at the worst offender, not to reproduce the
// modal: an instance with real history carries dozens of claims and a full
// dump would be unreadable in a push notification.
const claimAlertWorstProviders = 3

// clampClaimAlertDays resolves a configured threshold into one that can
// actually fire, reporting whether it had to be clamped.
//
// A value at or above ClaimWindowDays is rejected rather than honoured for the
// reason spelled out on MaxClaimAlertDays: the claim would age out of the
// count before the alert could ever trigger. A non-positive or absent value is
// not a clamp, it is a missing setting, and falls back to the default.
func clampClaimAlertDays(days int) (int, bool) {
	if days < 1 {
		return DefaultClaimAlertDays, false
	}
	if days > MaxClaimAlertDays {
		return MaxClaimAlertDays, true
	}
	return days, false
}

// claimAgeSummary is the derived, content-free picture of what is currently
// counted. Nothing here is stored: it is rebuilt from live model and failover
// state on every evaluation, exactly like GET /api/discovery/status.
type claimAgeSummary struct {
	models   int
	groups   int
	oldestAt time.Time
	// worstNames and worstCounts are parallel, ordered worst-first.
	worstNames  []string
	worstCounts []int
}

func (s claimAgeSummary) total() int { return s.models + s.groups }

// summarizeCountedClaims derives the counted claim set and finds the oldest
// member of it. Only COUNTED claims are considered: a stale (aged out) or
// suspect model is shown in the modal but never holds the badge open, so it
// must not hold this alert open either.
func summarizeCountedClaims(ctx context.Context, pool *pgxpool.Pool, now time.Time) (claimAgeSummary, error) {
	var s claimAgeSummary

	rows, err := listClaimRows(ctx, pool)
	if err != nil {
		return s, fmt.Errorf("list claim rows: %w", err)
	}
	window, err := flapCounts(ctx, pool, now.Add(-ClaimWindow))
	if err != nil {
		return s, fmt.Errorf("flap counts: %w", err)
	}
	// A nil since-review map is correct here rather than lazy: the alert never
	// reports "since your last visit", and buildProviderClaims only ever reads
	// the map, so every lookup yields the zero value.
	claims, count := buildProviderClaims(rows, window, nil, now)

	groups, err := listGroupClaims(ctx, pool)
	if err != nil {
		return s, fmt.Errorf("list group claims: %w", err)
	}

	s.models = count
	s.groups = len(groups)

	// claims is already ordered by counted-claim count descending, so the worst
	// offenders are simply the first few providers that still have any.
	for _, p := range claims {
		if len(p.Gone) == 0 || len(s.worstNames) >= claimAlertWorstProviders {
			continue
		}
		s.worstNames = append(s.worstNames, p.ProviderName)
		s.worstCounts = append(s.worstCounts, len(p.Gone))
	}

	for _, p := range claims {
		for _, c := range p.Gone {
			if s.oldestAt.IsZero() || c.LastSeenAt.Before(s.oldestAt) {
				s.oldestAt = c.LastSeenAt
			}
		}
	}
	for _, g := range groups {
		if s.oldestAt.IsZero() || g.DisabledAt.Before(s.oldestAt) {
			s.oldestAt = g.DisabledAt
		}
	}
	return s, nil
}

// claimAlertMessage renders the notification body. Routing metadata only:
// counts, ages and provider names. No model IDs, no request or response
// content, and nothing that could identify a prompt, matching the rule that
// holds everywhere else in this codebase.
func claimAlertMessage(s claimAgeSummary, thresholdDays, oldestDays int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s unaddressed for more than %s. The oldest has been outstanding for %s.",
		util.Count(s.total(), "model discrepancy", "model discrepancies"),
		util.Count(thresholdDays, "day", "days"),
		util.Count(oldestDays, "day", "days"))
	if len(s.worstNames) > 0 {
		parts := make([]string, 0, len(s.worstNames))
		for i, name := range s.worstNames {
			parts = append(parts, fmt.Sprintf("%s (%d)", name, s.worstCounts[i]))
		}
		fmt.Fprintf(&b, " Worst: %s.", strings.Join(parts, ", "))
	}
	if s.groups > 0 {
		fmt.Fprintf(&b, " Includes %s with dead hotel/ routing.",
			util.Count(s.groups, "failover group", "failover groups"))
	}
	return b.String()
}

// EvaluateClaimAgeAlert publishes EventTypeClaimsOutstanding when the oldest
// currently-counted claim has been outstanding for longer than the operator's
// threshold, and re-arms once nothing is crossed any more.
//
// The oldest counted claim is a sound proxy for "the badge has been non-zero
// continuously for at least this long": a claim that is still counted has been
// counted for its whole life, so its age is a lower bound on how long the
// badge has been asking for attention.
//
// Errors are returned rather than swallowed so the caller can log them, but
// the caller must treat this as housekeeping: a failure here says nothing
// about whether the discovery run itself succeeded.
func EvaluateClaimAgeAlert(ctx context.Context, pool *pgxpool.Pool, store SettingsStore, now time.Time) error {
	configured := store.GetInt(ctx, SettingKeyClaimAlertDays, DefaultClaimAlertDays)
	days, clamped := clampClaimAlertDays(configured)
	if clamped {
		debuglog.Warn("discovery: outstanding-claims alert threshold clamped below the claim window",
			"configured_days", configured, "effective_days", days, "claim_window_days", ClaimWindowDays)
	}
	threshold := time.Duration(days) * 24 * time.Hour

	summary, err := summarizeCountedClaims(ctx, pool, now)
	if err != nil {
		return err
	}

	oldestAge := time.Duration(0)
	if !summary.oldestAt.IsZero() {
		oldestAge = now.Sub(summary.oldestAt)
	}
	crossed := summary.total() > 0 && oldestAge > threshold
	fired := store.GetWithDefault(ctx, settingKeyClaimAlertFired, "") != ""

	switch {
	case crossed && !fired:
		oldestDays := int(oldestAge / (24 * time.Hour))
		// Metadata deliberately carries no "provider", "provider_id" or
		// "model_id" scalar: those are the dispatcher's debounce keys
		// (internal/alert/dispatcher.go), and this is one instance-wide alert
		// rather than a per-entity one, so it must debounce on its Type alone.
		events.Publish(events.Event{
			Type:     EventTypeClaimsOutstanding,
			Severity: "warning",
			Source:   "discovery",
			Message:  claimAlertMessage(summary, days, oldestDays),
			Metadata: map[string]any{
				"claim_count":     summary.total(),
				"model_claims":    summary.models,
				"group_claims":    summary.groups,
				"oldest_age_days": oldestDays,
				"oldest_at":       summary.oldestAt.UTC().Format(time.RFC3339),
				"threshold_days":  days,
				"worst_providers": worstProviderMetadata(summary),
			},
		})
		// Published BEFORE the latch is written on purpose. If the write fails
		// the next cycle re-fires, which is a duplicate notification; latching
		// first and failing would instead lose the alert entirely, and a missed
		// warning is worse than a repeated one.
		if err := store.Set(ctx, settingKeyClaimAlertFired, now.Format(time.RFC3339Nano)); err != nil {
			return fmt.Errorf("latch outstanding-claims alert: %w", err)
		}
	case !crossed && fired:
		if err := store.DeleteKey(ctx, settingKeyClaimAlertFired); err != nil {
			return fmt.Errorf("re-arm outstanding-claims alert: %w", err)
		}
	}
	return nil
}

// worstProviderMetadata renders the worst-offender list as structured metadata
// for SSE consumers.
func worstProviderMetadata(s claimAgeSummary) []map[string]any {
	out := make([]map[string]any, 0, len(s.worstNames))
	for i, name := range s.worstNames {
		out = append(out, map[string]any{"provider": name, "gone": s.worstCounts[i]})
	}
	return out
}
