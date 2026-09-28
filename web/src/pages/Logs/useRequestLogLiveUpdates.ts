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

const inFlight = (log: LogEntry) =>
	log.state === "pending" || log.state === "streaming";

/**
 * keepFinishedRow refuses an in-flight snapshot for a row the list already
 * holds finished. The request.streaming and request.completed events each
 * fetch the row; when the first fetch lands second it would put the streaming
 * snapshot (no tokens, no duration, a live pulse) back over the finished one,
 * and nothing later repairs that: fetchNewer only prepends ids not in the
 * list.
 */
export const keepFinishedRow = (current: LogEntry, next: LogEntry) =>
	!inFlight(current) && inFlight(next);

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
	mergeEntries: (
		entries: LogEntry[],
		keep?: (current: LogEntry, next: LogEntry) => boolean,
	) => void;
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
					mergeEntries([await api.logs.get(requestId)], keepFinishedRow);
				} catch {
					// The row may have been purged between the event and the fetch;
					// the fetchNewer below is the fallback.
				}
			}
		}
		// mergeEntries only updates rows already in the list, so this covers the
		// race where the pending row has not landed yet. fetchNewer is guarded
		// against concurrent calls.
		fetchNewer();
	});

	return { isVisible };
}
