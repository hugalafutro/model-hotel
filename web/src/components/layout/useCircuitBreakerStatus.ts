import {
	keepPreviousData,
	useQuery,
	useQueryClient,
} from "@tanstack/react-query";
import { api } from "../../api/client";
import { useServerEvent } from "../../context/EventContext";

/**
 * The aggregate circuit-breaker status behind the Failover nav badge, polled
 * every 15s and invalidated on circuit_breaker SSE events for real-time
 * updates. The same listener also handles the scheduled-disable sweep, which
 * flips providers off outside any user action, so every open tab refetches
 * what denormalizes provider enabled state: the providers list (cards +
 * quota badges hide via the enabled filter) and the failover groups the
 * sweep re-synced.
 */
export function useCircuitBreakerStatus() {
	const queryClient = useQueryClient();
	const { data: cbStatus } = useQuery({
		queryKey: ["circuit-breaker-status"],
		queryFn: () => api.failoverGroups.circuitBreakerStatus(true),
		refetchInterval: 15_000,
		placeholderData: keepPreviousData,
	});

	useServerEvent((event) => {
		if (event?.type?.startsWith("circuit_breaker.")) {
			queryClient.invalidateQueries({ queryKey: ["circuit-breaker-status"] });
		}
		if (event?.type === "provider.scheduled_disable") {
			queryClient.invalidateQueries({ queryKey: ["providers"] });
			queryClient.invalidateQueries({ queryKey: ["failover-groups"] });
		}
	});

	return cbStatus;
}
