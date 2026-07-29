import { useQuery } from "@tanstack/react-query";
import { useCallback, useState } from "react";
import { api } from "../api/client";
import type {
	DiscoveryChangeEntry,
	GroupClaim,
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
	/**
	 * Written ONLY by the optimistic half of a dismiss, holding the status the row
	 * carried when the operator clicked.
	 *
	 * Its PRESENCE is the compare half of the rollback's compare-and-swap: while
	 * it is set, the row still holds the value that dismissal wrote and nothing
	 * else has spoken since. Every merge strips it, because a refetch is newer
	 * authority than an in-flight guess. Never rendered.
	 */
	optimisticFrom?: ClaimStatus;
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

type Buckets = Record<GroupName, MergedClaim[]>;

const emptyBuckets = (): Buckets => ({ gone: [], stale: [], suspect: [] });

/**
 * A row the refetch no longer reports: resolved, kept in place, and no longer
 * owned by any in-flight dismiss.
 *
 * Dropping `optimisticFrom` is what makes the merge authoritative over a dismiss
 * that is still out. Every OTHER row a merge emits is rebuilt from the refetch's
 * own `ModelClaim`, which cannot carry the marker; this branch is the one that
 * spreads a snapshot row forward, so it is the one that has to strip it.
 */
function resolvedRow(before: MergedClaim): MergedClaim {
	const { optimisticFrom: _superseded, ...rest } = before;
	return { ...rest, status: "resolved" };
}

/**
 * Reconciles ONE provider's three buckets against a refetch, keyed on
 * `model_id` ACROSS the buckets rather than within each of them.
 *
 * Per-bucket reconciliation is wrong because the buckets are three states of the
 * same claim, not three independent lists. A model that degrades suspect -> gone
 * (or ages gone -> stale) leaves one bucket and enters another, so bucket-local
 * merging reads one fact as two: `resolved` in the bucket it left and `new` in
 * the bucket it entered. The operator then sees the same model twice, once
 * struck through as fixed, at the exact moment it got worse.
 *
 * Ordering, in three tiers per bucket:
 *
 *  1. rows the snapshot already had in THIS bucket, at their original indices,
 *     whether they stayed pending or resolved. Keeping a cleared claim exactly
 *     where it sat is the invariant this whole rework exists for.
 *  2. rows that moved in from another bucket, appended in snapshot order.
 *  3. rows seen for the first time in this refetch, appended last.
 *
 * The refetch's own ordering is deliberately not honoured for tiers 1 and 2: it
 * would shuffle untouched rows out from under the cursor. A mover appends rather
 * than jumping to the top for the same reason, and outranks a genuinely new row
 * because the operator has already seen it.
 */
function mergeProviderBuckets(prev: MergedProvider, fresh: ProviderClaims) {
	const freshByID = new Map<string, { claim: ModelClaim; group: GroupName }>();
	for (const g of GROUPS) {
		for (const c of fresh[g]) freshByID.set(c.model_id, { claim: c, group: g });
	}

	const kept = emptyBuckets();
	const moved = emptyBuckets();
	const reconciled = new Set<string>();

	for (const g of GROUPS) {
		for (const before of prev[g]) {
			const now = freshByID.get(before.model_id);
			if (!now) {
				kept[g].push(resolvedRow(before));
				continue;
			}
			reconciled.add(before.model_id);
			const row: MergedClaim = { ...now.claim, status: "pending" };
			if (now.group === g) kept[g].push(row);
			else moved[now.group].push(row);
		}
	}

	const out = emptyBuckets();
	for (const g of GROUPS) out[g] = [...kept[g], ...moved[g]];
	for (const [id, entry] of freshByID) {
		if (reconciled.has(id)) continue;
		out[entry.group].push({ ...entry.claim, status: "new" });
	}
	return out;
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
		return { ...prev, ...mergeProviderBuckets(prev, now ?? empty) };
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

/**
 * Rewrites ONE claim in place, leaving every other row untouched.
 *
 * All three buckets are scanned even though only `gone` rows offer a Dismiss
 * control: `model_id` is unique within a provider, and a refresh landing during
 * the request can legitimately have moved the row to another bucket. Identity is
 * therefore `(provider_id, model_id)`, never a bucket position.
 */
function mapClaim(
	snapshot: MergedProvider[],
	providerID: string,
	modelID: string,
	fn: (c: MergedClaim) => MergedClaim,
): MergedProvider[] {
	return snapshot.map((p) => {
		if (p.provider_id !== providerID) return p;
		const next: MergedProvider = { ...p };
		for (const g of GROUPS) {
			next[g] = p[g].map((c) => (c.model_id === modelID ? fn(c) : c));
		}
		return next;
	});
}

/**
 * Optimistic half of a dismiss: strikes the claim through where it sits and
 * remembers the status it is displacing.
 *
 * Deliberately not a removal — a row that vanishes on a click is the exact
 * complaint this rework exists to fix, and `resolved` is already the vocabulary
 * for "was wrong, is not any more".
 *
 * Pure and single-claim, so the caller can apply it FUNCTIONALLY against
 * whatever the snapshot happens to be when the write settles. Capturing and
 * replaying a whole array instead would silently discard anything a concurrent
 * refresh established in the meantime.
 */
export function dismissOptimistically(
	snapshot: MergedProvider[],
	providerID: string,
	modelID: string,
): MergedProvider[] {
	return mapClaim(snapshot, providerID, modelID, (c) => ({
		...c,
		status: "resolved",
		optimisticFrom: c.status,
	}));
}

/**
 * Rollback half, as a compare-and-swap: reverts the claim ONLY if it still holds
 * the value this dismissal wrote.
 *
 * `optimisticFrom` is the comparand. A merge strips it from every row it emits,
 * so a claim that a refresh has resolved (the model came back), moved to another
 * bucket, or simply re-reported since the click no longer matches and is left
 * exactly as the refetch left it. That newer state is derived from the server and
 * is more authoritative than a status read off the screen before the request was
 * even sent; replaying the captured one over it would show a resolved claim as
 * pending until the next fetch.
 *
 * Nothing is resurrected on a miss, which is what makes the moved-bucket case
 * safe: the row lives in its new bucket with a merge-assigned status, and the
 * rollback neither writes it there nor puts a copy back where it used to be.
 */
export function revertDismissal(
	snapshot: MergedProvider[],
	providerID: string,
	modelID: string,
): MergedProvider[] {
	return mapClaim(snapshot, providerID, modelID, (c) => {
		if (c.optimisticFrom === undefined) return c;
		const { optimisticFrom, ...rest } = c;
		return { ...rest, status: optimisticFrom };
	});
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
	// Failover groups discovery disabled. Held alongside the claims because
	// `claim_count` (the nav badge number) already counts them: if the modal
	// cannot render them, the badge points at rows that do not exist.
	// Replaced wholesale rather than merged: a group claim is derived live from
	// `group_enabled`, so it has no session-local status to preserve.
	const [groupClaims, setGroupClaims] = useState<GroupClaim[]>([]);
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
	// The bumped session has to reach THIS render's query key, not the next one.
	// `setSession` during render does not change `session` for the rest of the
	// pass, so querying the old key here would subscribe the reopening modal to
	// the previous open's cached response, seed the snapshot from it and latch
	// `seeded` before the fresh fetch could ever land: stale rows for the whole
	// second visit. Deriving the key instead means the reopen render already
	// misses the cache and fetches.
	const activeSession = open && !prevOpen ? session + 1 : session;
	if (open !== prevOpen) {
		setPrevOpen(open);
		if (open) {
			setSession(activeSession);
		} else {
			setSeeded(false);
			setRefreshError(null);
			// Everything the last visit collected goes with it. These arrays are
			// answers to a question the next open re-asks, and the next open renders
			// before its fetch lands: keeping them would paint the previous
			// session's rows over the new one, struck-through resolved rows and
			// already-dismissed models included, until the fresh response arrived.
			setSnapshot([]);
			setGroupClaims([]);
			setInformational([]);
		}
	}

	const query = useQuery({
		queryKey: ["discovery-status", "modal", activeSession],
		queryFn: () => api.discovery.status(true),
		enabled: open,
		staleTime: Number.POSITIVE_INFINITY,
	});

	if (open && !seeded && query.data) {
		// React's documented "adjust state during render" bail-out: seeding in an
		// effect would paint one frame of an empty modal first.
		setSnapshot(toSnapshot(query.data.claims));
		setGroupClaims(query.data.group_claims);
		setInformational(query.data.informational);
		setSeeded(true);
	}

	const refresh = useCallback(async () => {
		try {
			const fresh = await api.discovery.status(false);
			setSnapshot((prev) => mergeClaims(prev, fresh.claims));
			setGroupClaims(fresh.group_claims);
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

	/** Optimistic half of a dismiss; see `dismissOptimistically`. */
	const dismissClaim = useCallback((providerID: string, modelID: string) => {
		setSnapshot((prev) => dismissOptimistically(prev, providerID, modelID));
	}, []);

	/**
	 * Rollback half: undoes the ONE claim this dismiss optimistically changed, and
	 * only while that write is still the row's current value.
	 *
	 * Applied functionally against the live snapshot rather than by restoring an
	 * array — or a status — captured before the request. A refresh (a retest, an
	 * undo, another dismiss) can settle inside that window, and replaying
	 * click-time state over it would resurrect claims the refresh had just
	 * resolved and erase ones it had just discovered, for as long as it takes the
	 * next fetch to land. See `revertDismissal` for the compare-and-swap.
	 */
	const restoreClaim = useCallback((providerID: string, modelID: string) => {
		setSnapshot((prev) => revertDismissal(prev, providerID, modelID));
	}, []);

	return {
		snapshot,
		groupClaims,
		informational,
		// True only while the per-open fetch is still out, since the collections
		// above are cleared on close and the query key changes per open. The modal
		// MUST render this as its own state: with nothing collected yet and no
		// error, "no discrepancies" is a claim the hook cannot back.
		loading: query.isLoading,
		isError: query.isError,
		error: query.error,
		refresh,
		refreshError,
		dismissClaim,
		restoreClaim,
	};
}
