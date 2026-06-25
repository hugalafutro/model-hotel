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
// event stream.
export function sseEmitting(events: FdEvent[]) {
	return http.get("/api/sse", () => {
		const enc = new TextEncoder();
		const stream = new ReadableStream({
			start(controller) {
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
