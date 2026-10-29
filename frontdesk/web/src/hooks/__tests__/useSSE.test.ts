import { act, renderHook, waitFor } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { notifyUnauthorized, onUnauthorized } from "../../api/client";
import type { FdEvent } from "../../api/types";
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

// The loop below drives fetch directly so each test controls exactly when a
// connection opens, emits, ends or fails. The mocked stream errors when its
// request's signal aborts, as a real fetch body does, so unmount and an
// unauthorized notice are observed the way the browser would deliver them.
type Stream = { push: (chunk: string) => void; close: () => void };
type Plan = "stream" | "reject" | (() => Response);

function mockFetch(plans: Plan[]) {
	const streams: Stream[] = [];
	const signals: AbortSignal[] = [];
	const enc = new TextEncoder();
	const spy = vi.spyOn(globalThis, "fetch").mockImplementation((_url, init) => {
		const signal = init?.signal as AbortSignal;
		signals.push(signal);
		const plan = plans.shift() ?? "reject";
		if (plan === "reject") return Promise.reject(new TypeError("network"));
		if (plan !== "stream") return Promise.resolve(plan());
		let ctrl!: ReadableStreamDefaultController<Uint8Array>;
		const body = new ReadableStream<Uint8Array>({
			start(c) {
				ctrl = c;
			},
		});
		signal.addEventListener("abort", () =>
			ctrl.error(new DOMException("aborted", "AbortError")),
		);
		streams.push({
			push: (chunk) => ctrl.enqueue(enc.encode(chunk)),
			close: () => ctrl.close(),
		});
		return Promise.resolve(new Response(body, { status: 200 }));
	});
	return { spy, streams, signals };
}

const ev = (id: string): FdEvent => ({
	id,
	type: "health.up",
	severity: "info",
	source: "frontdesk",
	message: `m${id}`,
	created_at: "",
});
const frame = (e: FdEvent) => `data: ${JSON.stringify(e)}\n\n`;

// Settle the promise chain (fetch -> reader.read -> dispatch -> finally)
// without moving the fake clock.
const flush = () =>
	act(async () => {
		for (let i = 0; i < 20; i++) await Promise.resolve();
	});
const advance = async (ms: number) => {
	await act(() => vi.advanceTimersByTimeAsync(ms));
	await flush();
};

describe("useSSE stream loop", () => {
	beforeEach(() => {
		vi.useFakeTimers();
	});
	afterEach(() => {
		vi.useRealTimers();
	});

	it("parses and dispatches frames in order, across and within chunks, skipping non-data and malformed lines", async () => {
		const { spy, streams } = mockFetch(["stream"]);
		const onEvent = vi.fn();
		renderHook(() => useSSE(onEvent, true));
		await flush();
		const [s] = streams;

		s.push(frame(ev("1")) + frame(ev("2")));
		await flush();
		expect(onEvent.mock.calls).toEqual([[ev("1")], [ev("2")]]);

		const split = frame(ev("3"));
		s.push(split.slice(0, 12));
		await flush();
		expect(onEvent).toHaveBeenCalledTimes(2);
		s.push(split.slice(12));
		await flush();
		expect(onEvent.mock.calls).toEqual([[ev("1")], [ev("2")], [ev("3")]]);

		s.push("event: ping\nid: 7\n: comment\n\ndata:\n\ndata:   \n\n");
		await flush();
		expect(onEvent).toHaveBeenCalledTimes(3);

		s.push(`data: {not json\n\n${frame(ev("4"))}`);
		await flush();
		expect(onEvent.mock.calls).toEqual([
			[ev("1")],
			[ev("2")],
			[ev("3")],
			[ev("4")],
		]);
		// The malformed frame neither ended the stream nor forced a reconnect.
		expect(spy).toHaveBeenCalledTimes(1);
	});

	it("delivers later frames to the newest handler after a rerender without reconnecting", async () => {
		const { spy, streams } = mockFetch(["stream"]);
		const first = vi.fn();
		const second = vi.fn();
		const { rerender } = renderHook(({ h }) => useSSE(h, true), {
			initialProps: { h: first },
		});
		await flush();
		streams[0].push(frame(ev("1")));
		await flush();

		rerender({ h: second });
		streams[0].push(frame(ev("2")));
		await flush();

		expect(first.mock.calls).toEqual([[ev("1")]]);
		expect(second.mock.calls).toEqual([[ev("2")]]);
		expect(spy).toHaveBeenCalledTimes(1);
	});

	it("reconnects after a failed fetch with a delay that doubles per failure and caps at 30s", async () => {
		const { spy } = mockFetch([]); // every attempt rejects
		renderHook(() => useSSE(() => {}, true));
		await flush();
		expect(spy).toHaveBeenCalledTimes(1);

		let calls = 1;
		for (const d of [1000, 2000, 4000, 8000, 16000, 30000, 30000]) {
			await advance(d - 1);
			expect(spy).toHaveBeenCalledTimes(calls);
			await advance(1);
			calls++;
			expect(spy).toHaveBeenCalledTimes(calls);
		}
	});

	it("resets the backoff to 1s once a stream connects, and reconnects when that stream ends", async () => {
		const { spy, streams } = mockFetch(["reject", "reject", "stream"]);
		renderHook(() => useSSE(() => {}, true));
		await flush();
		await advance(1000);
		await advance(2000);
		expect(spy).toHaveBeenCalledTimes(3);

		// Without the reset the next delay would be 4000.
		streams[0].close();
		await flush();
		await advance(999);
		expect(spy).toHaveBeenCalledTimes(3);
		await advance(1);
		expect(spy).toHaveBeenCalledTimes(4);
		// And it keeps doubling from there.
		await advance(1999);
		expect(spy).toHaveBeenCalledTimes(4);
		await advance(1);
		expect(spy).toHaveBeenCalledTimes(5);
	});

	it.each([
		["a non-ok status", () => new Response("boom", { status: 500 })],
		["a missing body", () => new Response(null, { status: 200 })],
	])(
		"treats %s as a failure: no dispatch, reconnect without resetting the backoff",
		async (_name, response) => {
			const { spy } = mockFetch(["reject", response]);
			const onEvent = vi.fn();
			renderHook(() => useSSE(onEvent, true));
			await flush();
			await advance(1000);
			expect(spy).toHaveBeenCalledTimes(2);

			// The failure did not count as a connection, so the backoff is 2000.
			await advance(1999);
			expect(spy).toHaveBeenCalledTimes(2);
			await advance(1);
			expect(spy).toHaveBeenCalledTimes(3);
			expect(onEvent).not.toHaveBeenCalled();
		},
	);

	it("on 401 notifies unauthorized and stops reconnecting", async () => {
		const { spy } = mockFetch([() => new Response(null, { status: 401 })]);
		const listener = vi.fn();
		const stop = onUnauthorized(listener);
		try {
			renderHook(() => useSSE(() => {}, true));
			await flush();
			expect(listener).toHaveBeenCalledTimes(1);
			await advance(60_000);
			expect(spy).toHaveBeenCalledTimes(1);
			expect(vi.getTimerCount()).toBe(0);
		} finally {
			stop();
		}
	});

	it("aborts the open stream and stops reconnecting when another request reports unauthorized", async () => {
		const { spy, signals } = mockFetch(["stream"]);
		renderHook(() => useSSE(() => {}, true));
		await flush();

		notifyUnauthorized();
		await flush();
		expect(signals[0].aborted).toBe(true);
		await advance(60_000);
		expect(spy).toHaveBeenCalledTimes(1);
	});

	it("an unauthorized notice during the backoff wait cancels the pending reconnect", async () => {
		const { spy } = mockFetch([]);
		renderHook(() => useSSE(() => {}, true));
		await flush();
		expect(vi.getTimerCount()).toBe(1);

		notifyUnauthorized();
		await advance(60_000);
		expect(spy).toHaveBeenCalledTimes(1);
	});

	it("on unmount aborts the in-flight stream and delivers nothing further", async () => {
		const { spy, streams, signals } = mockFetch(["stream"]);
		const onEvent = vi.fn();
		const { unmount } = renderHook(() => useSSE(onEvent, true));
		await flush();

		unmount();
		expect(signals[0].aborted).toBe(true);
		// The aborted body refuses further chunks, so nothing reaches the handler.
		expect(() => streams[0].push(frame(ev("1")))).toThrow();
		await advance(60_000);
		expect(onEvent).not.toHaveBeenCalled();
		expect(spy).toHaveBeenCalledTimes(1);
	});

	it("on unmount clears the pending reconnect timer", async () => {
		const { spy } = mockFetch([]);
		const { unmount } = renderHook(() => useSSE(() => {}, true));
		await flush();
		expect(vi.getTimerCount()).toBe(1);

		unmount();
		expect(vi.getTimerCount()).toBe(0);
		await advance(60_000);
		expect(spy).toHaveBeenCalledTimes(1);
	});
});
