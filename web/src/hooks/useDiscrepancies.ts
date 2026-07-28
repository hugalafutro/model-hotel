import { useQuery } from "@tanstack/react-query";
import { useCallback, useState } from "react";
import { api } from "../api/client";
import type {
	DiscoveryChangeEntry,
	ModelClaim,
	ProviderClaims,
} from "../api/types";

/**
 * `pending` still wrong, `resolved` cleared during this session, `new` appeared
 * since the modal opened.
 */
export type ClaimStatus = "pending" | "resolved" | "new";

export interface MergedClaim extends ModelClaim {
	status: ClaimStatus;
}

export interface MergedProvider {
	provider_id: string;
	provider_name: string;
	gone: MergedClaim[];
	stale: MergedClaim[];
	suspect: MergedClaim[];
}

type GroupName = "gone" | "stale" | "suspect";
const GROUPS: GroupName[] = ["gone", "stale", "suspect"];

/** Seeds a fresh snapshot; everything the server reports is pending. */
export function toSnapshot(claims: ProviderClaims[]): MergedProvider[] {
	return claims.map((p) => ({
		provider_id: p.provider_id,
		provider_name: p.provider_name,
		gone: p.gone.map((c) => ({ ...c, status: "pending" as const })),
		stale: p.stale.map((c) => ({ ...c, status: "pending" as const })),
		suspect: p.suspect.map((c) => ({ ...c, status: "pending" as const })),
	}));
}

function mergeGroup(
	existing: MergedClaim[],
	fresh: ModelClaim[],
): MergedClaim[] {
	const byID = new Map(fresh.map((c) => [c.model_id, c]));
	// Existing entries keep their slot so a claim that clears is struck through
	// where it already was, rather than vanishing out from under the cursor.
	const merged: MergedClaim[] = existing.map((prev) => {
		const now = byID.get(prev.model_id);
		byID.delete(prev.model_id);
		return now
			? { ...now, status: "pending" as const }
			: { ...prev, status: "resolved" as const };
	});
	for (const added of byID.values()) {
		merged.push({ ...added, status: "new" });
	}
	return merged;
}

/**
 * Reconciles a refetch into the open modal's snapshot.
 *
 * The retest response's own diff is deliberately ignored. It describes what that
 * run changed, which is empty when the model is STILL missing, and reading that
 * emptiness as "resolved" was the original bug.
 */
export function mergeClaims(
	snapshot: MergedProvider[],
	fresh: ProviderClaims[],
): MergedProvider[] {
	const byID = new Map(fresh.map((p) => [p.provider_id, p]));
	const out: MergedProvider[] = snapshot.map((prev) => {
		const now = byID.get(prev.provider_id);
		byID.delete(prev.provider_id);
		const empty: ProviderClaims = {
			provider_id: prev.provider_id,
			provider_name: prev.provider_name,
			gone: [],
			stale: [],
			suspect: [],
		};
		const source = now ?? empty;
		const next: MergedProvider = { ...prev };
		for (const g of GROUPS) {
			next[g] = mergeGroup(prev[g], source[g]);
		}
		return next;
	});
	for (const added of byID.values()) {
		out.push({
			provider_id: added.provider_id,
			provider_name: added.provider_name,
			gone: added.gone.map((c) => ({ ...c, status: "new" as const })),
			stale: added.stale.map((c) => ({ ...c, status: "new" as const })),
			suspect: added.suspect.map((c) => ({ ...c, status: "new" as const })),
		});
	}
	return out;
}

/** True when nothing in any group is still pending or new. */
export function providerIsResolved(p: MergedProvider): boolean {
	return GROUPS.every((g) => p[g].every((c) => c.status === "resolved"));
}

/**
 * Owns the modal's snapshot. `refresh` refetches live status and merges it in;
 * it never replaces the snapshot, so resolved rows stay visible for the session.
 */
export function useDiscrepancies(open: boolean) {
	const [snapshot, setSnapshot] = useState<MergedProvider[]>([]);
	const [informational, setInformational] = useState<DiscoveryChangeEntry[]>(
		[],
	);
	const [refreshError, setRefreshError] = useState<Error | null>(null);
	const [seeded, setSeeded] = useState(false);

	// Each genuine modal-open gets its own query key. `?review=1` rebaselines the
	// server's flap_since_review stamp as a side effect of the fetch, and
	// staleTime: Infinity below deliberately suppresses focus/reconnect
	// refetches so that write fires exactly once per open, not once per render
	// or once per 60s of the modal staying open. Keying on the session instead
	// of invalidating on close means a reopen is a brand-new query with no
	// cached data, so it fetches (and re-stamps) unconditionally rather than
	// replaying a stale response from up to gcTime ago.
	const [session, setSession] = useState(0);
	const [prevOpen, setPrevOpen] = useState(open);
	if (open !== prevOpen) {
		setPrevOpen(open);
		if (open) {
			setSession((s) => s + 1);
		} else {
			setSeeded(false);
			setRefreshError(null);
		}
	}

	const query = useQuery({
		queryKey: ["discovery-status", "modal", session],
		queryFn: () => api.discovery.status(true),
		enabled: open,
		staleTime: Number.POSITIVE_INFINITY,
	});

	if (open && !seeded && query.data) {
		// React's documented "adjust state during render" bail-out: seeding in an
		// effect would paint one frame of an empty modal first.
		setSnapshot(toSnapshot(query.data.claims));
		setInformational(query.data.informational);
		setSeeded(true);
	}

	const refresh = useCallback(async () => {
		try {
			const fresh = await api.discovery.status(false);
			setSnapshot((prev) => mergeClaims(prev, fresh.claims));
			setInformational(fresh.informational);
			setRefreshError(null);
			return fresh;
		} catch (err) {
			// Surfaced via state rather than rethrown: callers fire this from a
			// button handler (typically `void refresh()`), so rethrowing would only
			// turn into an unhandled rejection instead of a renderable error.
			setRefreshError(err instanceof Error ? err : new Error(String(err)));
			return undefined;
		}
	}, []);

	return {
		snapshot,
		informational,
		loading: query.isLoading,
		isError: query.isError,
		error: query.error,
		refresh,
		refreshError,
	};
}
