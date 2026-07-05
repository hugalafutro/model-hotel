import { useCallback, useEffect, useRef, useState } from "react";
import { api } from "../api/client";
import type { FdEvent, MemberView } from "../api/types";
import { useSSE } from "./useSSE";

interface UseMembers {
	members: MemberView[];
	loading: boolean;
	error: boolean;
	refetch: () => void;
	// ISO timestamp of the last successful list refresh, for a "last updated"
	// footer. null until the first response applies.
	lastUpdatedAt: string | null;
}

// Live refresh cadence (ms). SSE events still refresh the list immediately; this
// short interval keeps the dashboard "live" so health badges, the verified-in-sync
// heartbeat, and relative times advance on their own, and it doubles as the
// safety net for a missed SSE event (e.g. a dropped/reconnected stream). A member
// list read is a single cheap GET, so a 5s cadence is fine for a control plane.
const FALLBACK_REFRESH_MS = 5_000;

// useMembers loads the member list (with live poller status) and keeps it fresh:
// it refetches whenever a membership, config, or health/version event arrives on
// the SSE stream, and on a slow fallback interval, so badges and last-sync times
// update without manual reloads even if an event is missed.
//
// It owns the page's single SSE subscription. A consumer that also needs to react
// to events (e.g. the Events page refetching its log) passes `onEvent` instead of
// opening a second stream, so a page never holds two connections to /api/sse.
export function useMembers(onEvent?: (e: FdEvent) => void): UseMembers {
	const [members, setMembers] = useState<MemberView[]>([]);
	const [loading, setLoading] = useState(true);
	const [error, setError] = useState(false);
	const [lastUpdatedAt, setLastUpdatedAt] = useState<string | null>(null);
	// Monotonic request id: SSE events can fire refetch faster than the network
	// responds, so only the newest in-flight request is allowed to apply, keeping
	// the list from flipping back to a stale snapshot.
	const seqRef = useRef(0);
	// Hold the latest consumer handler in a ref so the SSE callback stays stable
	// (keyed only on refetch) and never reconnects when the handler identity changes.
	const onEventRef = useRef(onEvent);
	useEffect(() => {
		onEventRef.current = onEvent;
	}, [onEvent]);

	const refetch = useCallback(() => {
		const seq = ++seqRef.current;
		api
			.listMembers()
			.then((m) => {
				if (seq !== seqRef.current) return;
				setMembers(m);
				setError(false);
				setLastUpdatedAt(new Date().toISOString());
			})
			.catch(() => {
				if (seq === seqRef.current) setError(true);
			})
			.finally(() => {
				if (seq === seqRef.current) setLoading(false);
			});
	}, []);

	useEffect(refetch, [refetch]);

	// Fallback poll: refresh from the stored timestamps even when no SSE event
	// arrives, so a missed config.auto_synced can't leave the last-sync column
	// stuck. Cheap (a single member-list read) and keeps relative times ticking.
	useEffect(() => {
		const id = setInterval(refetch, FALLBACK_REFRESH_MS);
		return () => clearInterval(id);
	}, [refetch]);

	useSSE(
		useCallback(
			(e) => {
				if (
					e.type.startsWith("member.") ||
					e.type.startsWith("config.") ||
					e.type.startsWith("health.") ||
					e.type.startsWith("version.")
				) {
					refetch();
				}
				onEventRef.current?.(e);
			},
			[refetch],
		),
		true,
	);

	return { members, loading, error, refetch, lastUpdatedAt };
}
