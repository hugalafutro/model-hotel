import { useQueryClient } from "@tanstack/react-query";
import { useCallback } from "react";

/**
 * Re-reads the Models nav badge (`claim_count` + `informational_unseen`).
 *
 * Every path that changes what /api/discovery/status would report has to call
 * this, wherever it lives. The badge is a 60s poll on its own query key, and
 * nothing else feeds it: the discrepancy modal's `refresh()` writes to the
 * modal's snapshot alone, and the Providers page's discover/delete mutations
 * invalidate models and providers. A dismissal, a retest or a rediscover
 * therefore leaves the badge showing a count the server no longer agrees with,
 * for up to another 60s. Dismissing everything and closing the modal onto a
 * badge that still reads 7 is the symptom.
 *
 * `exact: true`, and it is not optional. TanStack Query matches query keys by
 * PREFIX, and the modal's key is ["discovery-status", "modal", n] — a prefix
 * child of the poll's ["discovery-status"]. A non-exact invalidation therefore
 * also refetches the modal's query whenever the modal is open, and that query
 * calls api.discovery.status(TRUE), which stamps the server's last-reviewed
 * marker. The refetch itself is inert (`seeded` is already true, so the
 * snapshot does not change) but the stamp is not: it moves the "since your last
 * visit" baseline to now, zeroing every flap_since_review for the next visit.
 *
 * Deliberately an invalidation rather than a `setQueryData` seeded from the
 * modal's own refresh, even though that payload is byte-identical and would
 * save a round trip: `refresh()` returns its response even when a newer refresh
 * has superseded it (it drops the stale one instead of applying it), so seeding
 * from the return value can write a payload already known to be out of date
 * into the badge. One small GET per operator click is the cheaper mistake.
 */
export function useRefreshDiscoveryBadge() {
	const queryClient = useQueryClient();
	return useCallback(() => {
		queryClient.invalidateQueries({
			queryKey: ["discovery-status"],
			exact: true,
		});
	}, [queryClient]);
}
