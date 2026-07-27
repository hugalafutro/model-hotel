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

/**
 * What one hook instance knows: the snapshots it is showing and when they were
 * read. Purely in-memory and purely per-mount.
 *
 * Snapshots were once ALSO persisted to localStorage, so that a reload repainted
 * the badges instead of flashing empty for one round trip. That is gone on
 * purpose. Front Desk is a shared control plane whose authenticated shell mounts
 * on the mere presence of a stored token, well before anything has established
 * that the token is still valid, so persisted snapshots meant one operator's
 * quota figures sat readable in the next operator's browser. Every attempt to
 * keep the seed and gate it (namespacing per token, clearing on logout, clearing
 * on 401) only moved the exposure, because the exposure is the STORAGE, not the
 * paint: whatever is written is readable with devtools whether or not the app
 * draws it. Do NOT reintroduce persistence here in any form.
 */
interface QuotaState {
	snapshots: QuotaSnapshot[];
	lastUpdatedAt: string | null;
}

// A fresh empty result per call: this value is handed straight out as
// `snapshots`, so a shared module-level array would let one consumer's in-place
// sort corrupt every other mount in the session.
function emptyQuota(): QuotaState {
	return { snapshots: [], lastUpdatedAt: null };
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
	// Always starts empty: nothing is persisted, so a fresh mount knows nothing
	// until its first read resolves and the strip renders its empty state until
	// then. Last-good preservation below is in-memory and lives for the mount.
	const [data, setData] = useState<QuotaState>(emptyQuota);
	const [loading, setLoading] = useState(true);
	const [error, setError] = useState(false);
	const [refreshing, setRefreshing] = useState(false);

	// SSE-free, but the poll and a manual refresh can still overlap, so only the
	// newest in-flight read is allowed to apply.
	const seqRef = useRef(0);
	const lastRefreshRef = useRef(0);

	// Resolves true only when this read APPLIED its own result. A 200 is not
	// enough: if a newer read overtook this one, the sequence guard below throws
	// the response away, so nothing on screen came from it and it has demonstrated
	// nothing about what the operator is looking at. `refresh` asks whether it
	// could read its own result back, and a discarded read could not.
	//
	// The trade-off, deliberately accepted: when a concurrent poll supersedes the
	// read-back AND that poll succeeds, the data on screen IS fresh, yet the
	// refresh reports failure anyway. That false negative is the right direction
	// to err. The operator re-checks and sees good numbers, which beats trusting a
	// success toast sitting over stale ones. Telling the two cases apart would
	// cost more state than the rare wrong toast is worth, so do NOT "fix" this
	// back to returning true on any 200.
	const read = useCallback((): Promise<boolean> => {
		const seq = ++seqRef.current;
		return api
			.getQuota()
			.then(({ quota }) => {
				if (seq !== seqRef.current) return false;
				// A 200 is authoritative in both directions. An empty list means no
				// primary is designated, which is a real steady state, so it CLEARS the
				// badges; anything less would leave stale ones on screen forever after a
				// fleet is torn down.
				setData({ snapshots: quota, lastUpdatedAt: new Date().toISOString() });
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
	// setState on a dead tree. Pulled out of the effect body itself (rather
	// than `seqRef.current++` inline in the cleanup closure) because eslint's
	// ref-in-cleanup heuristic can't tell this apart from a DOM-node ref.
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
		snapshots: data.snapshots,
		loading,
		error,
		stale: error && data.snapshots.length > 0,
		lastUpdatedAt: data.lastUpdatedAt,
		refreshing,
		refresh,
	};
}
