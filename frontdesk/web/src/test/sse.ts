import { HttpResponse, http } from "msw";

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
