package ratelimit

import (
	"context"
	"time"
)

// settingsKeyFleetActiveMembers is the instance-local count of StateActive fleet
// members, delivered by Front Desk's announce heartbeat (internal/api/fleet.go).
// Unset (standalone or pre-upgrade) reads back as the default divisor 1: no
// fair-share division, i.e. exactly today's single-process behavior.
const settingsKeyFleetActiveMembers = "_fleet_active_members"

// settingsKeyFleetActiveMembersAt is the member-local Unix-seconds timestamp of
// the last divisor-aware announce (written by internal/api/fleet.go only when a
// real active_members count arrives). It is what lets the divisor self-expire:
// without it, a persisted count would keep throttling forever after the control
// plane stops managing this member.
const settingsKeyFleetActiveMembersAt = "_fleet_active_members_at"

// fleetDivisorTTL bounds how long a persisted divisor stays valid without a
// refresh. Past it the divisor reverts to 1 (no division — the accepted
// pre-feature Nx behavior) rather than throttle valid traffic to a stale
// fraction indefinitely.
//
// It matches the fleet's existing standalone horizon (internal/api's
// fleetForgetTTL, 24h — "the window after which a member that has not heard from
// Front Desk is treated as standalone again"). That is deliberately long: any
// realistic control-plane outage (a Front Desk restart, rebuild, or even a
// multi-hour blip) stays within it, so every member keeps dividing by N and the
// fleet's aggregate rate never exceeds the configured cap. Only a member truly
// abandoned for a full day reverts to standalone — the same moment the rest of
// the fleet logic already forgets it. Cross-package constant (ratelimit must not
// import api); keep the two in step if either changes.
const fleetDivisorTTL = 24 * time.Hour

// fleetDivisor is the number of active members sharing each configured limit.
// Always >= 1: an absent, zero, or negative setting means "no division", so it
// is safe to divide by unconditionally.
//
// The count is honored only while a divisor-aware announce has refreshed it
// within fleetDivisorTTL. If that refresh timestamp is missing or stale, this
// member is standalone, has been pulled from the fleet, or is under a control
// plane too old to send the count — so the divisor reverts to 1 rather than
// under-serve on a frozen fraction of the configured capacity.
func fleetDivisor(ctx context.Context, s SettingsReader) int {
	n := s.GetInt(ctx, settingsKeyFleetActiveMembers, 1)
	if n <= 1 {
		return 1
	}
	at := s.GetInt(ctx, settingsKeyFleetActiveMembersAt, 0)
	if at == 0 || time.Since(time.Unix(int64(at), 0)) > fleetDivisorTTL {
		return 1
	}
	return n
}

// fleetShareTPM returns tpm split into this member's 1/N fair share. Unlimited
// caps (tpm <= 0) pass through untouched; a finite share is floored to >= 1 so a
// small cap on a large fleet never rounds to 0 — which the TPM limiter treats as
// "no cap", the wrong and unsafe direction.
//
// Accepted edge: when tpm < N the floor makes every member allow 1, so the
// aggregate (N) exceeds the configured cap. This is a deliberate, bounded
// (aggregate <= N) lesser-evil versus flooring to 0 (which reads as
// "unlimited"). It cannot be fixed without giving some members a share of 0,
// which requires per-member ordinals — the cross-member coordination this
// stateless divisor exists to avoid. In practice it only bites at
// non-physical caps: a TPM below the active-member count (a single request
// spends far more than a handful of tokens), never at realistic limits.
func fleetShareTPM(ctx context.Context, s SettingsReader, tpm int) int {
	if tpm <= 0 {
		return tpm
	}
	n := fleetDivisor(ctx, s)
	if n <= 1 {
		return tpm
	}
	return max(1, tpm/n)
}
