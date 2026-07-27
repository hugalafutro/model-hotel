import { useCallback, useEffect, useRef, useState } from "react";
import { api } from "../api/client";
import type { QuotaSnapshot } from "../api/types";

// Read cadence. Quota moves slowly and this reads snapshots the primary has
// already stored, so a minute is plenty; the member list next door polls at 5s
// because health has to feel live, which quota does not.
const POLL_MS = 60_000;

// Matches the main dashboard's manual-refresh cooldown. A refresh forces the
// primary to re-poll every provider upstream, so it is not free.
const REFRESH_COOLDOWN_MS = 10_000;

export const QUOTA_CACHE_KEY = "fdQuotaSnapshots";

interface CachedQuota {
	snapshots: QuotaSnapshot[];
	lastUpdatedAt: string | null;
}

// A fresh empty result per call: readCache's fallback value is handed straight
// out as `snapshots`, so a shared module-level array would let one consumer's
// in-place sort corrupt every other mount in the session.
function emptyQuota(): CachedQuota {
	return { snapshots: [], lastUpdatedAt: null };
}

// Seeding from localStorage means a reload paints the badges immediately instead
// of flashing empty for a round trip, and it is what lets a failed first read
// still show something. Mirrors the main dashboard's initialData seeding.
function readCache(): CachedQuota {
	try {
		const raw = localStorage.getItem(QUOTA_CACHE_KEY);
		if (!raw) return emptyQuota();
		const parsed = JSON.parse(raw) as CachedQuota;
		if (!Array.isArray(parsed?.snapshots)) return emptyQuota();
		return {
			snapshots: parsed.snapshots,
			lastUpdatedAt: parsed.lastUpdatedAt ?? null,
		};
	} catch {
		return emptyQuota();
	}
}

function writeCache(v: CachedQuota) {
	try {
		localStorage.setItem(QUOTA_CACHE_KEY, JSON.stringify(v));
	} catch {
		/* private mode: the cache is an optimisation, not a requirement */
	}
}

/**
 * Drops the persisted snapshots. Call this wherever a session ends.
 *
 * The cache is not scoped to an operator, so on a shared browser it would
 * otherwise seed the next operator's first paint with the previous one's quota
 * data, and a failed first read leaves that stale data on screen indefinitely
 * (a non-2xx deliberately keeps the last-good snapshots, see `read`).
 *
 * There is no in-memory counterpart to reset: `cached` is per-hook state and
 * the strip lives inside the authenticated subtree, so a session end unmounts
 * it and the next mount re-seeds from this (now cleared) cache. The unmount
 * also bumps the request sequence, so a read still in flight cannot write the
 * cache back after this ran.
 */
export function clearQuotaCache() {
	try {
		localStorage.removeItem(QUOTA_CACHE_KEY);
	} catch {
		/* private mode: nothing was ever persisted to remove */
	}
}

export type QuotaRefreshOutcome = "ok" | "cooldown" | "failed";

export interface UseQuota {
	snapshots: QuotaSnapshot[];
	loading: boolean;
	/** The most recent read attempt failed. */
	error: boolean;
	/** We are showing data we could not confirm on the last attempt. */
	stale: boolean;
	/** ISO stamp of the last successful read, null until one lands. */
	lastUpdatedAt: string | null;
	refreshing: boolean;
	refresh: () => Promise<QuotaRefreshOutcome>;
}

/**
 * Reads the fleet primary's quota snapshots through Front Desk.
 *
 * Written in the useMembers.ts shape (plain state plus a monotonic request id)
 * rather than with a query library, because Front Desk has none.
 *
 * It deliberately opens no SSE stream: useMembers owns the page's single
 * /api/sse connection and quota has no event type, so subscribing here would put
 * a second connection on the control plane for nothing.
 *
 * @param collapsed pauses the poll while the strip is collapsed.
 */
export function useQuota(collapsed: boolean): UseQuota {
	const [cached, setCached] = useState<CachedQuota>(readCache);
	const [loading, setLoading] = useState(true);
	const [error, setError] = useState(false);
	const [refreshing, setRefreshing] = useState(false);

	// SSE-free, but the poll and a manual refresh can still overlap, so only the
	// newest in-flight read is allowed to apply.
	const seqRef = useRef(0);
	const lastRefreshRef = useRef(0);

	const read = useCallback(() => {
		const seq = ++seqRef.current;
		return api
			.getQuota()
			.then(({ quota }) => {
				if (seq !== seqRef.current) return;
				// A 200 is authoritative in both directions. An empty list means no
				// primary is designated, which is a real steady state, so it clears the
				// cache as well; anything less would leave stale badges on screen forever
				// after a fleet is torn down.
				const next: CachedQuota = {
					snapshots: quota,
					lastUpdatedAt: new Date().toISOString(),
				};
				setCached(next);
				writeCache(next);
				setError(false);
			})
			.catch(() => {
				// Any non-2xx (or a dead network) means we could not ask the primary,
				// which is NOT the same as "this fleet has no quota providers". Keep the
				// last-good snapshots and let the caller mark them stale. See
				// internal/frontdesk/quota.go for the contract this mirrors.
				if (seq === seqRef.current) setError(true);
			})
			.finally(() => {
				if (seq === seqRef.current) setLoading(false);
			});
	}, []);

	// Discard an in-flight response that lands after unmount: it must not
	// setState on a dead tree, and it must not write the cache after a test's
	// teardown (or a real navigation away) has already cleared it. Pulled out
	// of the effect body itself (rather than `seqRef.current++` inline in the
	// cleanup closure) because eslint's ref-in-cleanup heuristic can't tell
	// this apart from a DOM-node ref.
	const cancelInFlightRead = useCallback(() => {
		seqRef.current++;
	}, []);

	useEffect(() => {
		void read();
		return cancelInFlightRead;
	}, [read, cancelInFlightRead]);

	useEffect(() => {
		if (collapsed) return;
		const id = setInterval(() => void read(), POLL_MS);
		return () => clearInterval(id);
	}, [collapsed, read]);

	const refresh = useCallback(async (): Promise<QuotaRefreshOutcome> => {
		const now = Date.now();
		if (now - lastRefreshRef.current < REFRESH_COOLDOWN_MS) return "cooldown";
		lastRefreshRef.current = now;
		setRefreshing(true);
		let outcome: QuotaRefreshOutcome = "ok";
		try {
			// A 2xx only means the primary accepted and ran the sweep; the body's
			// counters say whether every provider actually answered. Without this
			// check a 200 carrying `failed: 1` reported success and the strip
			// toasted "refreshed" over providers that never responded.
			//
			// A partial failure is deliberately surfaced as the plain "failed"
			// outcome rather than a third, half-success one: the operator's next
			// action is the same either way (go look at the primary), so a separate
			// message would buy nothing and cost a new string in every locale.
			const { failed } = await api.refreshQuota();
			if (failed > 0) outcome = "failed";
		} catch {
			outcome = "failed";
		}
		setRefreshing(false);
		// Re-read either way: a failed force-poll still leaves the primary's last
		// stored snapshot readable, so the UI should reflect it rather than nothing.
		await read();
		return outcome;
	}, [read]);

	return {
		snapshots: cached.snapshots,
		loading,
		error,
		stale: error && cached.snapshots.length > 0,
		lastUpdatedAt: cached.lastUpdatedAt,
		refreshing,
		refresh,
	};
}
