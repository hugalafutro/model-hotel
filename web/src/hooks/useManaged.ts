import { useQuery } from "@tanstack/react-query";
import { api } from "../api/client";
import type { FleetStatus } from "../api/types";

// useManaged reports whether this instance is currently a managed fleet member:
// Front Desk is in contact AND this node is a non-primary member with a fresh
// heartbeat. In that state its synced entities (providers, virtual keys, custom
// failover groups, syncable settings) are declaratively replaced on the next
// config sync, so the UI makes them read-only and points the operator at the
// primary.
//
// Unlike useReadOnly (demo mode, fetched once with staleTime: Infinity), fleet
// liveness changes at runtime, so this rides the polling ["system"] query the
// sidebar already runs. The same queryKey means no extra request, and the value
// self-relaxes: when Front Desk stops announcing, computeFleetStatus flips the
// state to "warning" and this returns false again, re-enabling local edits.
//
// Only "member" locks. "primary" is fully editable; "warning" (stale heartbeat)
// relaxes to editable so an operator is never stranded if Front Desk goes away.
export function useManaged(): boolean {
	return useFleetState() === "member";
}

// useFleetState exposes the raw fleet state behind useManaged for the few
// surfaces that also react to being the primary (the Settings fleet banner).
// Undefined for a standalone instance Front Desk has never contacted, and
// while the system query is still loading.
export function useFleetState(): FleetStatus["state"] | undefined {
	const { data } = useQuery({
		queryKey: ["system"],
		queryFn: () => api.system.get(),
		refetchInterval: 10000,
		staleTime: 3000,
		retry: 1,
	});
	return data?.fleet?.state;
}
