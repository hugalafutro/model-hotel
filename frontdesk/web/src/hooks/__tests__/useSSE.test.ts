import { renderHook, waitFor } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { onUnauthorized } from "../../api/client";
import { server } from "../../test/server";
import { useSSE } from "../useSSE";

beforeEach(() => {
	localStorage.setItem("fdAuthToken", "tok");
});

describe("useSSE", () => {
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
