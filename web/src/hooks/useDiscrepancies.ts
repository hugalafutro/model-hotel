import { useQuery } from "@tanstack/react-query";
import { useCallback, useRef, useState } from "react";
import { api } from "../api/client";
import type {
	DiscoveryChangeEntry,
	GroupClaim,
	ModelClaim,
	ProviderClaims,
} from "../api/types";

/**
 * `pending` still wrong, `resolved` cleared during this session, `new` appeared
 * since the modal opened, `dismissed` acknowledged by the operator.
 *
 * `dismissed` is NOT a flavour of `resolved`. A resolved row is absent from the
 * refetch because the provider listed the model again; a dismissed row is absent
 * because `listClaimRows` filters on `discovery_dismissed_at IS NULL`. Collapsing
 * the two made the modal report every dismissal as "the model is listed again".
 */
export type ClaimStatus = "pending" | "resolved" | "new" | "dismissed";

export interface MergedClaim extends ModelClaim {
	status: ClaimStatus;
}

export interface MergedProvider {
	provider_id: string;
	provider_name: string;
	gone: MergedClaim[];
	stale: MergedClaim[];
	suspect: MergedClaim[];
	retired: MergedClaim[];
	pinned: MergedClaim[];
}

type GroupName = "gone" | "stale" | "suspect" | "retired" | "pinned";
const GROUPS: GroupName[] = ["gone", "stale", "suspect", "retired", "pinned"];

/**
 * One bucket off a payload, tolerating a server that predates it.
 *
 * `pinned` is absent rather than empty on older servers, which a rolling deploy
 * puts behind this dashboard, and every loop below indexes the buckets by name.
 */
const bucketOf = (p: ProviderClaims, g: GroupName): ModelClaim[] => p[g] ?? [];

/**
 * The same guard on the snapshot side, and NOT redundant with `bucketOf`.
 *
 * A snapshot outlives the payload it was seeded from: it is held across
 * refreshes, and the modal takes `providers` from whoever renders it. So a
 * missing bucket reaches these helpers too, and it reaches them at their worst
 * moment. Every reader below short-circuits (`every`, `filter` over a bucket
 * that is usually non-empty first), which means an unguarded read does not fail
 * on the common path at all: it waits for the provider whose earlier buckets
 * are ALL cleared, and only then throws, taking the whole modal with it.
 */
const snapshotBucketOf = (p: MergedProvider, g: GroupName): MergedClaim[] =>
	p[g] ?? [];

type Buckets = Record<GroupName, MergedClaim[]>;

const emptyBuckets = (): Buckets => ({
	gone: [],
	stale: [],
	suspect: [],
	retired: [],
	pinned: [],
});

/** Every bucket of one payload, stamped with the same status. */
function seedBuckets(p: ProviderClaims, status: ClaimStatus): Buckets {
	const out = emptyBuckets();
	for (const g of GROUPS)
		out[g] = bucketOf(p, g).map((c) => ({ ...c, status }));
	return out;
}

/** Seeds a fresh snapshot; everything the server reports is pending. */
export function toSnapshot(claims: ProviderClaims[]): MergedProvider[] {
	return claims.map((p) => ({
		provider_id: p.provider_id,
		provider_name: p.provider_name,
		...seedBuckets(p, "pending"),
	}));
}

/**
 * A row the refetch no longer reports: cleared, kept in place.
 *
 * A dismissed row is absent BECAUSE it was dismissed, so absence CONFIRMS the
 * dismissal rather than contradicting it. Every other absence means the provider
 * listed the model again. The two cannot share a status: the cleared summary
 * reports one as "is listed again", which is false for the other.
 */
function clearedRow(before: MergedClaim): MergedClaim {
	return {
		...before,
		status: before.status === "dismissed" ? "dismissed" : "resolved",
	};
}

/**
 * Reconciles ONE provider's buckets against a refetch, keyed on
 * `model_id` ACROSS the buckets rather than within each of them.
 *
 * Per-bucket reconciliation is wrong because the buckets are states of the same
 * claim, not independent lists. A model that degrades suspect -> gone
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
		for (const c of bucketOf(fresh, g))
			freshByID.set(c.model_id, { claim: c, group: g });
	}

	const kept = emptyBuckets();
	const moved = emptyBuckets();
	const reconciled = new Set<string>();

	for (const g of GROUPS) {
		for (const before of snapshotBucketOf(prev, g)) {
			const now = freshByID.get(before.model_id);
			if (!now) {
				kept[g].push(clearedRow(before));
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
			retired: [],
			pinned: [],
		};
		return { ...prev, ...mergeProviderBuckets(prev, now ?? empty) };
	});
	for (const added of byID.values()) {
		out.push({
			provider_id: added.provider_id,
			provider_name: added.provider_name,
			...seedBuckets(added, "new"),
		});
	}
	return out;
}

/**
 * Rewrites the named claims in place, leaving every other row untouched.
 *
 * Every bucket is scanned even though only some of them offer a per-row control:
 * `model_id` is unique within a provider, the per-provider Dismiss all also
 * covers stale, and a refresh landing during the request can legitimately have
 * moved a row to another bucket. Identity is therefore
 * `(provider_id, model_id)`, never a bucket position.
 */
function mapClaims(
	snapshot: MergedProvider[],
	providerID: string,
	modelIDs: Set<string>,
	fn: (c: MergedClaim) => MergedClaim,
): MergedProvider[] {
	return snapshot.map((p) => {
		if (p.provider_id !== providerID) return p;
		const next: MergedProvider = { ...p };
		for (const g of GROUPS) {
			next[g] = snapshotBucketOf(p, g).map((c) =>
				modelIDs.has(c.model_id) ? fn(c) : c,
			);
		}
		return next;
	});
}

/**
 * Marks claims as dismissed where they sit.
 *
 * Callers apply this only once the server has CONFIRMED the write, never before.
 * Marking up front raced any refresh that landed while the request was out: the
 * model was still being reported, so the merge rebuilt the row as `pending` and
 * dropped the status, and the next refresh then read its absence as `resolved` -
 * "is listed again" - for a model that had just been dismissed by hand.
 *
 * Deliberately not a removal — a row that vanishes on a click is the exact
 * complaint this rework exists to fix.
 *
 * `dismissed`, never `resolved`: see ClaimStatus. Writing `resolved` here is what
 * made the cleared summary announce a dismissal as "the model is listed again".
 *
 * Pure, so the caller can apply it FUNCTIONALLY against whatever the snapshot
 * happens to be when the write settles, rather than replaying a captured array
 * over anything a concurrent refresh established in the meantime.
 */
export function markDismissed(
	snapshot: MergedProvider[],
	providerID: string,
	modelIDs: Set<string>,
): MergedProvider[] {
	return mapClaims(snapshot, providerID, modelIDs, (c) => ({
		...c,
		status: "dismissed",
	}));
}

/**
 * True when no row in any group is still ACTIONABLE, i.e. none is `pending` or
 * `new`.
 *
 * `dismissed` counts as dealt with alongside `resolved`: the operator asked for
 * the row to stop being tracked, so it needs neither a retest nor another
 * dismissal. This is the single predicate behind the provider pill's either-or
 * controls (Retest all + Dismiss all, or Clean) and behind which providers the
 * Retest-all walk visits.
 */
export function providerHasNoPending(p: MergedProvider): boolean {
	return GROUPS.every((g) =>
		snapshotBucketOf(p, g).every(
			(c) => c.status === "resolved" || c.status === "dismissed",
		),
	);
}

/**
 * True when a retest of this provider could only confirm what is already known.
 *
 * A retest re-runs discovery, which asks the provider what it lists. A provider
 * whose only outstanding claims are retirements has models that ARE listed —
 * that is precisely the problem, listed and refused — so the answer changes
 * nothing and costs a slow upstream call.
 *
 * It lives here rather than in the modal because BOTH the control and the walk
 * have to honour it, and they are in different files. Gating only the modal's
 * button hides the walk's Retest-all control when every provider is
 * retired-only, which looks like agreement but is not: on a mixed fleet the
 * button renders for the provider that does need retesting, and the walk then
 * visits the retired-only ones too, each with its own pill sitting disabled
 * saying a retest proves nothing.
 */
export function retestProvesNothing(p: MergedProvider): boolean {
	const pending = (g: GroupName) =>
		snapshotBucketOf(p, g).filter(
			(c) => c.status === "pending" || c.status === "new",
		).length;
	return (
		!providerHasNoPending(p) &&
		pending("retired") > 0 &&
		pending("gone") === 0 &&
		pending("stale") === 0 &&
		pending("suspect") === 0
	);
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

	/**
	 * Monotonic sequence counter: refreshes overlap, so only the NEWEST in-flight
	 * response is applied.
	 *
	 * Retest, dismiss and the modal's own reads all call `refresh`, and a status
	 * read issued before a dismissal can land after it. Its payload predates the
	 * write, so it still reports the dismissed model, and `mergeProviderBuckets`
	 * would rebuild that row from the stale claim as `pending` — erasing a
	 * confirmed dismissal and putting the controls back for a model that is gone.
	 *
	 * The same seqRef pattern MembersPage and useMembers use for their own
	 * concurrent refetches.
	 */
	const refreshSeq = useRef(0);

	const refresh = useCallback(async () => {
		const seq = ++refreshSeq.current;
		try {
			const fresh = await api.discovery.status(false);
			// A newer refresh has already landed; this payload is history.
			if (seq !== refreshSeq.current) return fresh;
			setSnapshot((prev) => mergeClaims(prev, fresh.claims));
			setGroupClaims(fresh.group_claims);
			setInformational(fresh.informational);
			setRefreshError(null);
			return fresh;
		} catch (err) {
			if (seq !== refreshSeq.current) return undefined;
			// Surfaced via state rather than rethrown: callers fire this from a
			// button handler (typically `void refresh()`), so rethrowing would only
			// turn into an unhandled rejection instead of a renderable error.
			setRefreshError(err instanceof Error ? err : new Error(String(err)));
			return undefined;
		}
	}, []);

	/**
	 * Marks confirmed dismissals; see `markDismissed`.
	 *
	 * The unpin path shares it. An unpinned model leaves the claims payload
	 * outright (the pin is gone and the miss streak is reset, so nothing is left
	 * to claim), which is the same absence a dismissal produces: caused by the
	 * operator, and therefore never "the provider is listing it again".
	 */
	const dismissClaim = useCallback(
		(providerID: string, modelIDs: Set<string>) => {
			setSnapshot((prev) => markDismissed(prev, providerID, modelIDs));
		},
		[],
	);

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
	};
}
