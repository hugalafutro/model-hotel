package ratelimit

import "context"

// settingsKeyFleetActiveMembers is the instance-local count of StateActive fleet
// members, delivered by Front Desk's announce heartbeat (internal/api/fleet.go).
// Unset (standalone or pre-upgrade) reads back as the default divisor 1: no
// fair-share division, i.e. exactly today's single-process behavior.
const settingsKeyFleetActiveMembers = "_fleet_active_members"

// fleetDivisor is the number of active members sharing each configured limit.
// Always >= 1: an absent, zero, or negative setting means "no division", so it is
// safe to divide by unconditionally.
func fleetDivisor(ctx context.Context, s SettingsReader) int {
	n := s.GetInt(ctx, settingsKeyFleetActiveMembers, 1)
	if n < 1 {
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
