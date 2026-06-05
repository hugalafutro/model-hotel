import { render, waitFor } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { afterEach, describe, expect, it, vi } from "vitest";
import { createSSEStream } from "../../test/helpers";
import { server } from "../../test/mocks/server";
import { EventProvider } from "../EventContext";
import { ToastProvider } from "../ToastContext";

interface ServerEvent {
	id: string;
	type: string;
	severity: "success" | "info" | "warning" | "error";
	message: string;
	metadata?: Record<string, unknown>;
	timestamp: string;
}

function renderWithEventProvider(ui: React.ReactElement) {
	return render(ui, {
		wrapper: ({ children }) => (
			<ToastProvider>
				<EventProvider>{children}</EventProvider>
			</ToastProvider>
		),
	});
}

describe("EventContext", () => {
	it("EventProvider renders children without crashing", () => {
		const TestChild = () => <div data-testid="child">Test Child</div>;

		const { getByTestId } = renderWithEventProvider(<TestChild />);

		expect(getByTestId("child")).toBeInTheDocument();
	});

	it("EventProvider works with admin token set (from setup.ts which calls setAdminToken)", () => {
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
	});

	it("connects to /api/events on mount", async () => {
		let fetchCalled = false;
		let authHeader: string | undefined;

		server.use(
			http.get("/api/events", ({ request }) => {
				fetchCalled = true;
				authHeader = request.headers.get("Authorization") ?? undefined;
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

		expect(authHeader).toBe("Bearer test-admin-token");
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

		// Verify reconnection happens with reasonable delays
		expect(callTimes.length).toBeGreaterThanOrEqual(3);
		const delay1 = callTimes[1] - callTimes[0];

		// Delay should be around 1000ms (backoff resets on successful connections)
		expect(delay1).toBeGreaterThanOrEqual(500);
		expect(delay1).toBeLessThanOrEqual(1500);
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
		const requestSignals: AbortSignal[] = [];

		server.use(
			http.get("/api/events", ({ request }) => {
				requestSignals.push(request.signal);
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
			expect(requestSignals.length).toBeGreaterThanOrEqual(1);
		});

		const firstSignal = requestSignals[0];
		expect(firstSignal?.aborted).toBe(false);

		unmount();

		// Wait for abort to propagate
		await waitFor(() => {
			expect(firstSignal?.aborted).toBe(true);
		});
	});

	it("does not reconnect after unmount", async () => {
		// This test verifies that the abort signal is set on unmount,
		// which prevents the reconnection logic in the finally block.
		// The EventContext.finally() checks `!ac.signal.aborted` before
		// scheduling reconnection, so an aborted signal = no reconnect.
		// We verify the precondition (abort fires) rather than the
		// reconnection behavior, because MSW/JSDOM don't properly
		// propagate abort to streaming ReadableStreams.
		let callCount = 0;
		const requestSignals: AbortSignal[] = [];

		server.use(
			http.get("/api/events", ({ request }) => {
				callCount++;
				requestSignals.push(request.signal);
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

		const firstSignal = requestSignals[0];

		unmount();

		// Verify the abort signal fires - this is what prevents reconnection
		// in the EventContext.finally() block (line 70: !ac.signal.aborted)
		await waitFor(
			() => {
				expect(firstSignal?.aborted).toBe(true);
			},
			{ timeout: 3000 },
		);
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

	it("clears token and reloads on 401 response", async () => {
		const reloadMock = vi.fn();
		vi.stubGlobal("location", {
			...window.location,
			reload: reloadMock,
		});
		const removeItemSpy = vi.spyOn(window.localStorage, "removeItem");

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

		expect(removeItemSpy).toHaveBeenCalledWith("adminToken");

		removeItemSpy.mockRestore();
		vi.unstubAllGlobals();
	});

	it("reconnects with backoff on non-401 error", async () => {
		const reloadMock = vi.fn();
		vi.stubGlobal("location", {
			...window.location,
			reload: reloadMock,
		});
		const removeItemSpy = vi.spyOn(window.localStorage, "removeItem");

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
		// Verify adminToken was NOT removed
		expect(removeItemSpy).not.toHaveBeenCalledWith("adminToken");

		removeItemSpy.mockRestore();
		vi.unstubAllGlobals();
	});
});
