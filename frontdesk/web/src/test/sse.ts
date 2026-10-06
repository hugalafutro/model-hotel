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

// sseEmitting mocks GET /api/sse and pushes the given events as SSE frames on
// connect, then stays open. Use it to exercise live-refetch paths driven by the
// event stream. onOpen fires as the frames are enqueued: a handler that must
// answer differently once the event is out reads a flag set here, rather than
// counting calls. Independent requests are not served in issue order (the
// event-triggered refetch can reach its handler before the mount read does), so
// a call counter can hand the "after" answer to the stale read and the "before"
// answer to the one the page keeps.
export function sseEmitting(events: FdEvent[], onOpen?: () => void) {
	return http.get("/api/sse", () => {
		const enc = new TextEncoder();
		const stream = new ReadableStream({
			start(controller) {
				onOpen?.();
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
