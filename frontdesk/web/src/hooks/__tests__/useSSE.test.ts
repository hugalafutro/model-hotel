import { renderHook, waitFor } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { afterEach, describe, expect, it, vi } from "vitest";
import { onUnauthorized } from "../../api/client";
import { server } from "../../test/server";
import { sseHandler } from "../../test/sse";
import { useSSE } from "../useSSE";

// No session state is seeded anywhere here: the stream is authenticated by the
// HttpOnly cookie the browser attaches, so the hook has no token precondition
// left to satisfy and connects on `enabled` alone.

afterEach(() => {
	vi.restoreAllMocks();
});

describe("useSSE", () => {
	it("streams with same-origin credentials and no Authorization header", async () => {
		// Spy without an implementation so MSW still answers the request.
		const fetchSpy = vi.spyOn(globalThis, "fetch");
		server.use(sseHandler());
		renderHook(() => useSSE(() => {}, true));

		await waitFor(() => expect(fetchSpy).toHaveBeenCalled());
		const [url, init] = fetchSpy.mock.calls[0];
		expect(String(url)).toContain("/api/sse");
		expect(init?.credentials).toBe("same-origin");
		expect(new Headers(init?.headers).get("Authorization")).toBeNull();
	});

	it("does not connect while disabled", () => {
		const fetchSpy = vi.spyOn(globalThis, "fetch");
		server.use(sseHandler());
		renderHook(() => useSSE(() => {}, false));

		expect(fetchSpy).not.toHaveBeenCalled();
	});

	it("drops to login (notifies unauthorized) when the stream returns 401", async () => {
		server.use(
			http.get("/api/sse", () => new HttpResponse(null, { status: 401 })),
		);
		const spy = vi.fn();
		const stop = onUnauthorized(spy);
		try {
			renderHook(() => useSSE(() => {}, true));
			await waitFor(() => expect(spy).toHaveBeenCalled());
		} finally {
			stop();
		}
	});

	it("delivers parsed events to the handler", async () => {
		server.use(
			http.get("/api/sse", () => {
				const enc = new TextEncoder();
				const stream = new ReadableStream({
					start(controller) {
						controller.enqueue(
							enc.encode(
								`data: ${JSON.stringify({ id: "1", type: "health.up", severity: "info", source: "frontdesk", message: "up", created_at: "" })}\n\n`,
							),
						);
					},
				});
				return new HttpResponse(stream, {
					headers: { "Content-Type": "text/event-stream" },
				});
			}),
		);
		const onEvent = vi.fn();
		renderHook(() => useSSE(onEvent, true));
		await waitFor(() =>
			expect(onEvent).toHaveBeenCalledWith(
				expect.objectContaining({ type: "health.up" }),
			),
		);
	});
});
