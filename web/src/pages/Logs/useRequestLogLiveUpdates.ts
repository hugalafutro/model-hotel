import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { api } from "../../api/client";
import type { LogEntry } from "../../api/types";

/**
 * Keeps the request list current while the live toggle is on, in whichever
 * view mode is active.
 *
 * Paginate mode simply invalidates the page query on request events. Scroll
 * mode merges the finished row by id on request.streaming / request.completed
 * (so it swaps its placeholder values for the real provider, tokens and
 * duration without waiting for a refetch), and always follows up with
 * fetchNewer to cover the race where the pending row has not landed in the
 * list yet. A 60s poll and a visibility/focus refresh cover SSE disconnects.
 *
 * Also reports whether the document is visible, which the paginate query uses
 * to pause its own refetch interval.
 */
export function useRequestLogLiveUpdates({
	viewMode,
	liveEnabled,
	fetchNewer,
	mergeEntries,
}: {
	viewMode: "paginate" | "scroll";
	liveEnabled: boolean;
	fetchNewer: () => void;
	mergeEntries: (entries: LogEntry[]) => void;
}) {
	const queryClient = useQueryClient();
	const [isVisible, setIsVisible] = useState(!document.hidden);
	useEffect(() => {
		const handler = () => setIsVisible(!document.hidden);
		document.addEventListener("visibilitychange", handler);
		return () => document.removeEventListener("visibilitychange", handler);
	}, []);

	useEffect(() => {
		if (!liveEnabled) return;
		const handler = (e: Event) => {
			const event = (e as CustomEvent).detail;
			if (
				event.type === "request.started" ||
				event.type === "request.streaming" ||
				event.type === "request.completed"
			) {
				if (viewMode === "paginate") {
					queryClient.invalidateQueries({ queryKey: ["logs"] });
				}
			}
		};
		window.addEventListener("server-event", handler);
		return () => window.removeEventListener("server-event", handler);
	}, [liveEnabled, queryClient, viewMode]);

	// Slow poll fallback for scroll mode (catches SSE disconnects)
	useEffect(() => {
		if (viewMode !== "scroll" || !liveEnabled) return;
		const interval = setInterval(() => {
			if (!document.hidden) {
				fetchNewer();
			}
		}, 60000);
		return () => clearInterval(interval);
	}, [viewMode, liveEnabled, fetchNewer]);

	// Visibility/focus refresh for scroll mode
	useEffect(() => {
		if (viewMode !== "scroll" || !liveEnabled) return;
		const handler = () => {
			if (!document.hidden) {
				fetchNewer();
			}
		};
		document.addEventListener("visibilitychange", handler);
		return () => document.removeEventListener("visibilitychange", handler);
	}, [viewMode, liveEnabled, fetchNewer]);

	// SSE-driven live updates for scroll mode
	useEffect(() => {
		if (viewMode !== "scroll" || !liveEnabled) return;
		const handler = async (e: Event) => {
			const event = (e as CustomEvent).detail;
			if (event.type === "request.completed") {
				// Fetch the completed entry by ID and merge it into
				// the existing entries so the row shows final metrics
				// (provider, tokens, duration, etc.) instead of the
				// placeholder values from the initial INSERT.
				const requestId: string | undefined = event.metadata?.request_id;
				if (requestId) {
					try {
						const entry = await api.logs.get(requestId);
						mergeEntries([entry]);
					} catch {
						// Fall back to fetchNewer on error (e.g. row
						// was purged between event and fetch)
						fetchNewer();
					}
				} else {
					// Fallback when request_id is missing from the
					// event payload (e.g. schema change, old server)
					fetchNewer();
				}
				// Always fetchNewer after request.completed to cover
				// the race where the pending row hasn't been added to
				// the list yet (request.started fetch still in-flight).
				// fetchNewer is guarded against concurrent calls.
				fetchNewer();
			} else if (event.type === "request.streaming") {
				// The provider just committed mid-stream: fetch the row by
				// ID and merge so it swaps "Resolving" for the real
				// provider/model (and the "Streaming" state) without waiting
				// for request.completed. mergeEntries only updates rows that
				// are already in the list, so the trailing fetchNewer covers
				// the race where the pending row hasn't landed yet.
				const requestId: string | undefined = event.metadata?.request_id;
				if (requestId) {
					try {
						const entry = await api.logs.get(requestId);
						mergeEntries([entry]);
					} catch {
						// Fall back to fetchNewer on error (e.g. row
						// was purged between event and fetch)
						fetchNewer();
						return;
					}
				} else {
					// Fallback when request_id is missing from the
					// event payload (e.g. schema change, old server)
					fetchNewer();
					return;
				}
				// Always fetchNewer after request.streaming to cover
				// the race where the pending row hasn't been added to
				// the list yet (request.started fetch still in-flight).
				// fetchNewer is guarded against concurrent calls.
				fetchNewer();
			} else if (event.type === "request.started") {
				fetchNewer();
			}
		};
		window.addEventListener("server-event", handler);
		return () => window.removeEventListener("server-event", handler);
	}, [viewMode, liveEnabled, fetchNewer, mergeEntries]);

	return { isVisible };
}
