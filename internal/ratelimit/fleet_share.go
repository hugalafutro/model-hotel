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
