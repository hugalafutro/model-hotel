import { useQueryClient } from "@tanstack/react-query";
import { api } from "../../api/client";
import type { LogEntry } from "../../api/types";
import { useServerEvent } from "../../context/EventContext";
import { useDocumentVisible } from "../../hooks/useDocumentVisible";
import { useScrollLivePoll } from "../../hooks/useScrollLivePoll";

const REQUEST_EVENTS = new Set([
	"request.started",
	"request.streaming",
	"request.completed",
]);

// A row only moves forward: pending, then streaming, then finished.
const rank = (log: LogEntry) =>
	log.state === "pending" ? 0 : log.state === "streaming" ? 1 : 2;

/**
 * keepFresherRow refuses a snapshot from earlier in a row's life than the one
 * the list holds. Fetches for one row settle out of order: the
 * request.streaming and request.completed events each fetch it, and a page
 * fetch started before a merge lands after it. Taking the older copy put a
 * live pulse with no tokens back over a finished row, and nothing later
 * repaired it. Same rank replaces, so a fresher copy of the same state lands.
 */
export const keepFresherRow = (current: LogEntry, next: LogEntry) =>
	rank(current) > rank(next);

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
	const isVisible = useDocumentVisible();

	useScrollLivePoll({
		enabled: viewMode === "scroll" && liveEnabled,
		fetchNewer,
		intervalMs: 60_000,
	});

	useServerEvent(async (event) => {
		if (!liveEnabled || !REQUEST_EVENTS.has(event.type)) return;
		if (viewMode === "paginate") {
			queryClient.invalidateQueries({ queryKey: ["logs"] });
			return;
		}
		if (event.type !== "request.started") {
			// The row carries its real provider, tokens and duration once the
			// provider commits mid-stream or the request finishes; merging by id
			// swaps the placeholder values in place.
			const requestId = event.metadata?.request_id;
			if (typeof requestId === "string") {
				try {
					mergeEntries([await api.logs.get(requestId)]);
				} catch {
					// The row may have been purged between the event and the fetch;
					// the fetchNewer below is the fallback.
				}
			}
		}
		// A row not in the list yet is held by mergeEntries for the page that
		// first lists it, and this fetchNewer is that page: it also covers the
		// race where the pending row has not landed yet. fetchNewer is guarded
		// against concurrent calls.
		fetchNewer();
	});

	return { isVisible };
}
