import { useCallback, useEffect, useRef, useState } from "react";
import { api } from "../api/client";
import type { FdEvent, MemberView } from "../api/types";
import { useSSE } from "./useSSE";

interface UseMembers {
	members: MemberView[];
	loading: boolean;
	error: boolean;
	refetch: () => void;
}

// useMembers loads the member list (with live poller status) and keeps it fresh:
// it refetches whenever a membership or health/version event arrives on the SSE
// stream, so badges update within a poll interval without manual reloads.
//
// It owns the page's single SSE subscription. A consumer that also needs to react
// to events (e.g. the Events page refetching its log) passes `onEvent` instead of
// opening a second stream, so a page never holds two connections to /api/sse.
export function useMembers(onEvent?: (e: FdEvent) => void): UseMembers {
	const [members, setMembers] = useState<MemberView[]>([]);
	const [loading, setLoading] = useState(true);
	const [error, setError] = useState(false);
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
			})
			.catch(() => {
				if (seq === seqRef.current) setError(true);
			})
			.finally(() => {
				if (seq === seqRef.current) setLoading(false);
			});
	}, []);

	useEffect(refetch, [refetch]);

	useSSE(
		useCallback(
			(e) => {
				if (
					e.type.startsWith("member.") ||
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

	return { members, loading, error, refetch };
}
