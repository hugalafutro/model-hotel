import { useCallback, useEffect, useRef, useState } from "react";
import { api, getAuthToken } from "../api/client";
import type { QuotaSnapshot } from "../api/types";

// Read cadence. Quota moves slowly and this reads snapshots the primary has
// already stored, so a minute is plenty; the member list next door polls at 5s
// because health has to feel live, which quota does not.
const POLL_MS = 60_000;

// Matches the main dashboard's manual-refresh cooldown. A refresh forces the
// primary to re-poll every provider upstream, so it is not free.
const REFRESH_COOLDOWN_MS = 10_000;

/**
 * Shared head of every persisted-snapshot key. The full key carries a per-token
 * suffix (see `quotaCacheKey`); this prefix is what makes "drop every operator's
 * entry" expressible without knowing which tokens ever existed.
 */
export const QUOTA_CACHE_PREFIX = "fdQuotaSnapshots";

/**
 * Namespace suffix for a session token: FNV-1a run twice with different offset
 * bases, so the suffix is ~64 bits and two live sessions cannot collide in
 * practice.
 *
 * Deliberately NOT a cryptographic digest. This only has to keep one operator's
 * localStorage entry out of another's reach on a shared browser, and every
 * digest the platform offers (crypto.subtle) is async, which would push the seed
 * past first paint and defeat the entire point of having a cache. The token
 * itself is never written into the key.
 */
function namespaceFor(token: string): string {
	let a = 0x811c9dc5;
	let b = 0x01000193;
	for (let i = 0; i < token.length; i++) {
		const c = token.charCodeAt(i);
		a = Math.imul(a ^ c, 0x01000193) >>> 0;
		b = Math.imul(b ^ c, 0x85ebca6b) >>> 0;
	}
	return a.toString(36) + b.toString(36);
}

/**
 * Cache key for the session that is signed in right now, or null when none is.
 *
 * Front Desk is a shared control plane, so a reload on a shared browser mounts
 * the authenticated shell purely because a token exists in localStorage, well
 * before anything has established that the token is still valid. A single
 * unnamespaced key therefore paints the previous operator's quota data at first
 * paint, and if the first read fails with anything other than a 401 (a dead
 * network, a 502 from the primary) the 401 cleanup never runs and it stays on
 * screen. Keying by token closes that: operator B's key is not operator A's, so
 * there is nothing to read, while the same operator reloading still gets their
 * own snapshots and keeps the instant repaint this cache exists for.
 */
export function quotaCacheKey(): string | null {
	const token = getAuthToken();
	if (!token) return null;
	return `${QUOTA_CACHE_PREFIX}:${namespaceFor(token)}`;
}

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
//
// With no token there is no session to seed for, and nothing that could be
// written back either, so it returns empty rather than falling back to some
// shared entry.
function readCache(): CachedQuota {
	try {
		const key = quotaCacheKey();
		if (!key) return emptyQuota();
		const raw = localStorage.getItem(key);
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
		const key = quotaCacheKey();
		if (!key) return;
		localStorage.setItem(key, JSON.stringify(v));
	} catch {
		/* private mode: the cache is an optimisation, not a requirement */
	}
}

/**
 * Drops EVERY persisted snapshot entry, not just the current session's. Call
 * this wherever a session ends.
 *
 * Namespacing by token means entries accumulate, one per token this browser has
 * ever signed in with, so a clear that only removed `quotaCacheKey()` would
 * narrow what this used to do and leave a growing pile behind. Sweeping the
 * prefix also makes the call order-independent: App's logout clears the auth
 * token first, at which point `quotaCacheKey()` is already null and a
 * single-key removal would silently do nothing.
 *
 * There is no in-memory counterpart to reset: `cached` is per-hook state and
 * the strip lives inside the authenticated subtree, so a session end unmounts
 * it and the next mount re-seeds from this (now cleared) cache. The unmount
 * also bumps the request sequence, so a read still in flight cannot write the
 * cache back after this ran.
 */
export function clearQuotaCache() {
	try {
		// Collected first: removing while iterating localStorage re-indexes it and
		// would skip every other match.
		const doomed: string[] = [];
		for (let i = 0; i < localStorage.length; i++) {
			const k = localStorage.key(i);
			if (k?.startsWith(QUOTA_CACHE_PREFIX)) doomed.push(k);
		}
		for (const k of doomed) localStorage.removeItem(k);
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

	// Resolves true when the GET itself came back 200, false otherwise. The
	// boolean is about the request, not about which read won the seq race: a
	// superseded read still demonstrably reached the primary, and `refresh` only
	// asks whether it could read its own result back.
	const read = useCallback((): Promise<boolean> => {
		const seq = ++seqRef.current;
		return api
			.getQuota()
			.then(({ quota }) => {
				if (seq !== seqRef.current) return true;
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
				return true;
			})
			.catch(() => {
				// Any non-2xx (or a dead network) means we could not ask the primary,
				// which is NOT the same as "this fleet has no quota providers". Keep the
				// last-good snapshots and let the caller mark them stale. See
				// internal/frontdesk/quota.go for the contract this mirrors.
				if (seq === seqRef.current) setError(true);
				return false;
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
		//
		// The read-back is part of the outcome. If it fails, the badges (and any
		// open modal) still show pre-refresh numbers and are flagged stale, so a
		// success toast over them would be a plain lie: nothing on screen came from
		// the sweep we just triggered. Same "failed" outcome and same string as a
		// failed POST, because the operator's next action is identical, and because
		// a new outcome would cost a new message in all eleven locales.
		//
		// `read` keeps its last-good preservation untouched: this changes only what
		// refresh REPORTS, never what it throws away.
		if (!(await read())) outcome = "failed";
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
