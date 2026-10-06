import { HttpResponse, http } from "msw";
import type { FdEvent } from "../api/types";

// sseHandler mocks GET /api/sse with a stream that stays open and emits nothing,
// so components using useSSE connect cleanly without triggering the reconnect
// loop during a test. The stream is torn down when the test aborts the fetch
// (on unmount/cleanup).
export function sseHandler() {
	return http.get("/api/sse", () => {
		const stream = new ReadableStream({
			start() {
				/* never enqueue, never close: an idle keep-alive */
			},
		});
		return new HttpResponse(stream, {
			headers: { "Content-Type": "text/event-stream" },
		});
	});
}

// sseEmitting mocks GET /api/sse and pushes the given events as SSE frames,
// then stays open. Use it to exercise live-refetch paths driven by the event
// stream. With `after`, the frames wait for that promise: a test whose handler
// answers differently before and after the event resolves it from the first
// read, so the event cannot race the mount read. Independent requests are not
// served in issue order, and without the gate the event-triggered refetch can
// reach its handler before the mount read does, handing the "after" answer to
// the stale read the page drops and the "before" answer to the one it keeps.
export function sseEmitting(events: FdEvent[], after?: Promise<unknown>) {
	return http.get("/api/sse", () => {
		const enc = new TextEncoder();
		const stream = new ReadableStream({
			async start(controller) {
				await after;
				for (const e of events) {
					controller.enqueue(enc.encode(`data: ${JSON.stringify(e)}\n\n`));
				}
				/* stay open so useSSE doesn't reconnect */
			},
		});
		return new HttpResponse(stream, {
			headers: { "Content-Type": "text/event-stream" },
		});
	});
}
