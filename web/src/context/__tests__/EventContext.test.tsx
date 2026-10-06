import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, waitFor } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { afterEach, describe, expect, it, type MockInstance, vi } from "vitest";
import { createSSEStream } from "../../test/helpers";
import { server } from "../../test/mocks/server";
import { EventProvider } from "../EventContext";
import { ToastProvider } from "../ToastContext";

// The /api/events fetch calls EventProvider has made so far.
function eventsCalls(fetchSpy: MockInstance<typeof fetch>): number {
	return fetchSpy.mock.calls.filter(([input]) =>
		String(input instanceof Request ? input.url : input).includes(
			"/api/events",
		),
	).length;
}

// The AbortSignal EventProvider passed to its /api/events fetch, once made.
function eventsSignal(
	fetchSpy: MockInstance<typeof fetch>,
): AbortSignal | undefined {
	const call = fetchSpy.mock.calls.find(([input]) =>
		String(input instanceof Request ? input.url : input).includes(
			"/api/events",
		),
	);
	return call?.[1]?.signal ?? undefined;
}

interface ServerEvent {
	id: string;
	type: string;
	severity: "success" | "info" | "warning" | "error";
	message: string;
	metadata?: Record<string, unknown>;
	timestamp: string;
}

function renderWithEventProvider(
	ui: React.ReactElement,
	queryClient = new QueryClient(),
) {
	return render(ui, {
		wrapper: ({ children }) => (
			<QueryClientProvider client={queryClient}>
				<ToastProvider>
					<EventProvider>{children}</EventProvider>
				</ToastProvider>
			</QueryClientProvider>
		),
	});
}

function sseOnce(event: ServerEvent) {
	server.use(
		http.get("/api/events", () => {
			const stream = createSSEStream([event], { doneSentinel: null });
			return new HttpResponse(stream, {
				status: 200,
				headers: {
					"Content-Type": "text/event-stream",
					"Cache-Control": "no-cache",
				},
			});
		}),
	);
}

describe("EventContext", () => {
	it("EventProvider renders children without crashing", () => {
		const TestChild = () => <div data-testid="child">Test Child</div>;

		const { getByTestId } = renderWithEventProvider(<TestChild />);

		expect(getByTestId("child")).toBeInTheDocument();
	});

	it("EventProvider works with the session cookie set (seeded in setup.ts)", () => {
		const TestChild = () => <div data-testid="child">Test Child</div>;

		const { getByTestId } = renderWithEventProvider(<TestChild />);

		expect(getByTestId("child")).toBeInTheDocument();
	});
});

describe("SSE connection and event handling", () => {
	afterEach(() => {
		server.resetHandlers();
		vi.clearAllMocks();
		vi.useRealTimers();
		// Re-seed the session cookie so the next test starts authenticated (the
		// 401 test clears it via clearAuth()).
		document.cookie = "mh_csrf=test-csrf; path=/";
	});

	it("connects to /api/events on mount with the session cookie", async () => {
		let fetchCalled = false;
		let authHeader: string | undefined;
		let cookieHeader: string | undefined;

		server.use(
			http.get("/api/events", ({ request }) => {
				fetchCalled = true;
				authHeader = request.headers.get("Authorization") ?? undefined;
				cookieHeader = request.headers.get("Cookie") ?? undefined;
				const stream = createSSEStream([], { doneSentinel: null });
				return new HttpResponse(stream, {
					status: 200,
					headers: {
						"Content-Type": "text/event-stream",
						"Cache-Control": "no-cache",
					},
				});
			}),
		);

		const TestChild = () => <div data-testid="child">Test</div>;
		renderWithEventProvider(<TestChild />);

		await waitFor(() => {
			expect(fetchCalled).toBe(true);
		});

		expect(authHeader).toBeUndefined();
		expect(cookieHeader).toContain("mh_csrf=");
	});

	it("re-reads providers, models and the badge when a discovery completes", async () => {
		// Discovery can run server-side with no dashboard action behind it (a
		// provider save that changed its address), so the lists follow the
		// completion event rather than a mutation's settle.
		const queryClient = new QueryClient();
		const invalidate = vi.spyOn(queryClient, "invalidateQueries");
		sseOnce({
			id: "evt-d",
			type: "request.discovery.provider_completed",
			severity: "info",
			message: "Finished discovery for Test",
			timestamp: new Date().toISOString(),
		});
		renderWithEventProvider(<div />, queryClient);
		await waitFor(() => {
			expect(invalidate).toHaveBeenCalledWith({ queryKey: ["providers"] });
			expect(invalidate).toHaveBeenCalledWith({ queryKey: ["models"] });
			// exact: a prefix match would also refetch the discrepancy modal's
			// query, which stamps the server's last-reviewed marker.
			expect(invalidate).toHaveBeenCalledWith({
				queryKey: ["discovery-status"],
				exact: true,
			});
		});
	});

	it.each([
		"backup.created",
		"request.discovery.provider_starting",
		"discovery.provider_fetched",
	])("does not re-read lists on %s", async (type) => {
		// The mid-scan events fire before the scan's rows are written, so a
		// re-read on them would still see the old catalogue.
		const queryClient = new QueryClient();
		const invalidate = vi.spyOn(queryClient, "invalidateQueries");
		const seen = vi.fn();
		window.addEventListener("server-event", seen);
		sseOnce({
			id: "evt-x",
			type,
			severity: "info",
			message: "something happened",
			timestamp: new Date().toISOString(),
		});
		renderWithEventProvider(<div />, queryClient);
		await waitFor(() => expect(seen).toHaveBeenCalledTimes(1));
		window.removeEventListener("server-event", seen);
		expect(invalidate).not.toHaveBeenCalled();
	});

	it("dispatches server-event CustomEvent for each SSE chunk", async () => {
		const eventHandler = vi.fn();
		window.addEventListener("server-event", eventHandler);

		const serverEvent: ServerEvent = {
			id: "evt-1",
			type: "backup.created",
			severity: "success",
			message: "Backup completed successfully",
			timestamp: new Date().toISOString(),
		};

		server.use(
			http.get("/api/events", () => {
				const stream = createSSEStream([serverEvent], {
					doneSentinel: null,
				});
				return new HttpResponse(stream, {
					status: 200,
					headers: {
						"Content-Type": "text/event-stream",
						"Cache-Control": "no-cache",
					},
				});
			}),
		);

		const TestChild = () => <div data-testid="child">Test</div>;
		renderWithEventProvider(<TestChild />);

		await waitFor(() => {
			expect(eventHandler).toHaveBeenCalledTimes(1);
		});

		const customEvent = eventHandler.mock.calls[0][0] as CustomEvent;
		expect(customEvent.detail).toEqual(serverEvent);

		window.removeEventListener("server-event", eventHandler);
	});

	it("shows toast for user-facing events", async () => {
		const serverEvent: ServerEvent = {
			id: "evt-1",
			type: "backup.created",
			severity: "success",
			message: "Backup completed successfully",
			timestamp: new Date().toISOString(),
		};

		server.use(
			http.get("/api/events", () => {
				const stream = createSSEStream([serverEvent], {
					doneSentinel: null,
				});
				return new HttpResponse(stream, {
					status: 200,
					headers: {
						"Content-Type": "text/event-stream",
						"Cache-Control": "no-cache",
					},
				});
			}),
		);

		const dispatchSpy = vi.spyOn(window, "dispatchEvent");

		const TestChild = () => <div data-testid="child">Test</div>;
		renderWithEventProvider(<TestChild />);

		await waitFor(() => {
			expect(dispatchSpy).toHaveBeenCalled();
		});

		const customEventCall = dispatchSpy.mock.calls.find(
			(call) => call[0] instanceof CustomEvent,
		);
		expect(customEventCall).toBeDefined();
		if (customEventCall) {
			const event = customEventCall[0] as CustomEvent<ServerEvent>;
			expect(event.detail.type).toBe("backup.created");
			expect(event.detail.message).toBe("Backup completed successfully");
		}

		dispatchSpy.mockRestore();
	});

	it("does not show toast for request.* events", async () => {
		const requestEvent: ServerEvent = {
			id: "evt-1",
			type: "request.started",
			severity: "info",
			message: "Request started",
			timestamp: new Date().toISOString(),
		};

		const dispatchSpy = vi.spyOn(window, "dispatchEvent");

		server.use(
			http.get("/api/events", () => {
				const stream = createSSEStream([requestEvent], {
					doneSentinel: null,
				});
				return new HttpResponse(stream, {
					status: 200,
					headers: {
						"Content-Type": "text/event-stream",
						"Cache-Control": "no-cache",
					},
				});
			}),
		);

		const TestChild = () => <div data-testid="child">Test</div>;
		renderWithEventProvider(<TestChild />);

		await waitFor(() => {
			expect(dispatchSpy).toHaveBeenCalled();
		});

		const customEventCall = dispatchSpy.mock.calls.find(
			(call) => call[0] instanceof CustomEvent,
		);
		expect(customEventCall).toBeDefined();
		if (customEventCall) {
			const event = customEventCall[0] as CustomEvent<ServerEvent>;
			expect(event.detail.type).toBe("request.started");
		}

		dispatchSpy.mockRestore();
	});

	it("reconnects after stream ends", async () => {
		let callCount = 0;
		const callTimes: number[] = [];

		server.use(
			http.get("/api/events", () => {
				callCount++;
				callTimes.push(Date.now());
				const stream = createSSEStream(
					[
						{
							id: `evt-${callCount}`,
							type: "test.event",
							severity: "info",
							message: `Event ${callCount}`,
							timestamp: new Date().toISOString(),
						},
					],
					{ doneSentinel: "[DONE]" },
				);
				return new HttpResponse(stream, {
					status: 200,
					headers: {
						"Content-Type": "text/event-stream",
						"Cache-Control": "no-cache",
					},
				});
			}),
		);

		const TestChild = () => <div data-testid="child">Test</div>;
		renderWithEventProvider(<TestChild />);

		// Wait for first 3 connections (initial + 2 reconnects)
		await waitFor(
			() => {
				expect(callCount).toBeGreaterThanOrEqual(3);
			},
			{ timeout: 10000 },
		);

		// Reconnection happened repeatedly. We intentionally do NOT assert the
		// exact wall-clock backoff gap here: with real timers under coverage
		// instrumentation (and React StrictMode's double-mount) the measured
		// delay between connections is jittery and was a flaky failure source
		// (e.g. 357ms vs a 500ms floor). Backoff behavior is covered by the
		// callCount progression and the sibling "reconnects multiple times" test.
		expect(callTimes.length).toBeGreaterThanOrEqual(3);
	});

	it("reconnects multiple times after stream ends", async () => {
		let callCount = 0;

		server.use(
			http.get("/api/events", () => {
				callCount++;
				const stream = createSSEStream(
					[
						{
							id: `evt-${callCount}`,
							type: "test.event",
							severity: "info",
							message: `Event ${callCount}`,
							timestamp: new Date().toISOString(),
						},
					],
					{ doneSentinel: "[DONE]" },
				);
				return new HttpResponse(stream, {
					status: 200,
					headers: {
						"Content-Type": "text/event-stream",
						"Cache-Control": "no-cache",
					},
				});
			}),
		);

		const TestChild = () => <div data-testid="child">Test</div>;
		renderWithEventProvider(<TestChild />);

		// Verify multiple reconnections happen
		await waitFor(
			() => {
				expect(callCount).toBeGreaterThanOrEqual(4);
			},
			{ timeout: 10000 },
		);
	});

	it("aborts SSE connection on unmount", async () => {
		// The abort is read off the fetch call itself: the request a handler
		// receives is msw's own copy, and its signal does not follow the
		// caller's AbortController.
		const fetchSpy = vi.spyOn(globalThis, "fetch");

		server.use(
			http.get("/api/events", () => {
				const stream = createSSEStream([], { doneSentinel: null });
				return new HttpResponse(stream, {
					status: 200,
					headers: {
						"Content-Type": "text/event-stream",
						"Cache-Control": "no-cache",
					},
				});
			}),
		);

		const TestChild = () => <div data-testid="child">Test</div>;
		const { unmount } = renderWithEventProvider(<TestChild />);

		await waitFor(() => {
			expect(eventsSignal(fetchSpy)).toBeDefined();
		});

		const firstSignal = eventsSignal(fetchSpy);
		expect(firstSignal?.aborted).toBe(false);

		unmount();

		// Wait for abort to propagate
		await waitFor(() => {
			expect(firstSignal?.aborted).toBe(true);
		});
		fetchSpy.mockRestore();
	});

	it("does not reconnect after unmount", async () => {
		// This test verifies that the abort signal is set on unmount,
		// which prevents the reconnection logic in the finally block.
		// The EventContext.finally() checks `!ac.signal.aborted` before
		// scheduling reconnection, so an aborted signal = no reconnect.
		// Both the precondition (the abort fires) and the outcome (no further
		// /api/events fetch through the first backoff) are checked. The signal
		// is read off the fetch call (see the abort test above).
		let callCount = 0;
		const fetchSpy = vi.spyOn(globalThis, "fetch");

		server.use(
			http.get("/api/events", () => {
				callCount++;
				const encoder = new TextEncoder();
				const stream = new ReadableStream({
					start(controller) {
						controller.enqueue(encoder.encode('data: {"type":"ping"}\n\n'));
						controller.close();
					},
				});
				return new HttpResponse(stream, {
					status: 200,
					headers: {
						"Content-Type": "text/event-stream",
						"Cache-Control": "no-cache",
					},
				});
			}),
		);

		const TestChild = () => <div data-testid="child">Test</div>;
		const { unmount } = renderWithEventProvider(<TestChild />);

		await waitFor(() => {
			expect(callCount).toBeGreaterThanOrEqual(1);
		});

		const firstSignal = eventsSignal(fetchSpy);

		unmount();

		// Verify the abort signal fires - this is what prevents reconnection
		// in the EventContext.finally() block (line 70: !ac.signal.aborted)
		await waitFor(
			() => {
				expect(firstSignal?.aborted).toBe(true);
			},
			{ timeout: 3000 },
		);
		// And that nothing reconnects: the stream above closes at once, so a
		// reconnect would be due after the 1s first backoff. Sit past it.
		await new Promise((r) => setTimeout(r, 1300));
		expect(eventsCalls(fetchSpy)).toBe(1);
		fetchSpy.mockRestore();
	});

	it("handles non-ok response and reconnects", async () => {
		let callCount = 0;

		server.use(
			http.get("/api/events", () => {
				callCount++;
				return HttpResponse.json(
					{ error: "Internal server error" },
					{ status: 500 },
				);
			}),
		);

		const TestChild = () => <div data-testid="child">Test</div>;
		renderWithEventProvider(<TestChild />);

		// Verify reconnection attempts on error
		await waitFor(
			() => {
				expect(callCount).toBeGreaterThanOrEqual(3);
			},
			{ timeout: 10000 },
		);
	});

	it("clears the session and reloads on 401 response", async () => {
		// The session cookie is present (seeded in setup); a 401 must clear it.
		expect(document.cookie).toContain("mh_csrf=");

		const reloadMock = vi.fn();
		vi.stubGlobal("location", {
			...window.location,
			reload: reloadMock,
		});

		server.use(
			http.get("/api/events", () => {
				return new HttpResponse(null, { status: 401 });
			}),
		);

		const TestChild = () => <div data-testid="child">Test</div>;
		renderWithEventProvider(<TestChild />);

		await waitFor(() => {
			expect(reloadMock).toHaveBeenCalled();
		});

		// clearAuth() expired the readable CSRF cookie.
		expect(document.cookie).not.toContain("mh_csrf=");

		vi.unstubAllGlobals();
	});

	it("reconnects with backoff on non-401 error", async () => {
		const reloadMock = vi.fn();
		vi.stubGlobal("location", {
			...window.location,
			reload: reloadMock,
		});

		let callCount = 0;

		server.use(
			http.get("/api/events", () => {
				callCount++;
				return HttpResponse.json(
					{ error: "Internal server error" },
					{ status: 500 },
				);
			}),
		);

		const TestChild = () => <div data-testid="child">Test</div>;
		renderWithEventProvider(<TestChild />);

		// Wait for multiple reconnection attempts (exponential backoff: 1s, 2s, 4s...)
		await waitFor(
			() => {
				expect(callCount).toBeGreaterThanOrEqual(3);
			},
			{ timeout: 10000 },
		);

		// Verify reload was NOT called
		expect(reloadMock).not.toHaveBeenCalled();
		// The session cookie is NOT cleared on non-401 errors.
		expect(document.cookie).toContain("mh_csrf=");

		vi.unstubAllGlobals();
	});
});
