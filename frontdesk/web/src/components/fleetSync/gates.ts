import type { FleetMemberStatus, FleetStatus } from "../../api/types";

// Step gates shared by the wizard, its step screens, and the resting screen.
// Every predicate reads the one fleet-status probe (GET /api/fleet/status) so
// the gates can never disagree with each other.

export type Step = 1 | 2 | 3;
export const STEPS: Step[] = [1, 2, 3];

// Reachable members other than the primary: the ones a step can actually act on.
export function reachablePeers(s: FleetStatus): FleetMemberStatus[] {
	return s.members.filter((m) => m.member_id !== s.primary_id && m.reachable);
}
// MASTER_KEY blockers: reachable members that provably cannot decrypt the
// primary's keys. null (keyless / not evaluated) never blocks.
export function masterKeyBlockers(s: FleetStatus): FleetMemberStatus[] {
	return s.members.filter((m) => m.reachable && m.master_key_matches === false);
}
// Schema blockers: reachable members whose app is too old to receive the
// primary's config. The member checks its schema before the MASTER_KEY canary,
// so a skewed member reports master_key_matches=null and zero diff counts and
// would otherwise slip through every gate, where config sync then fails for it.
// They can only be fixed by upgrading the member, so they hard-block.
export function schemaBlockers(s: FleetStatus): FleetMemberStatus[] {
	return reachablePeers(s).filter((m) => !m.schema_ok);
}
export function configChanges(s: FleetStatus): FleetMemberStatus[] {
	return reachablePeers(s).filter((m) => m.added + m.updated + m.removed > 0);
}
export function offlinePeers(s: FleetStatus): FleetMemberStatus[] {
	return s.members.filter((m) => m.member_id !== s.primary_id && !m.reachable);
}
