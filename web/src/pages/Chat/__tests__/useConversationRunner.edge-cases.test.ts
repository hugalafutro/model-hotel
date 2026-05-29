import { act, renderHook } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import type { Mock } from "vitest";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { server } from "../../../test/mocks/server";
import { useConversationRunner } from "../useConversationRunner";
import {
	createMockParams,
	createWrapper,
} from "./useConversationRunner.helpers";

describe("useConversationRunner", () => {
	beforeEach(() => {
		server.resetHandlers();
		vi.clearAllMocks();
	});

	it("handles abort during turn delay", async () => {
		vi.useFakeTimers();

		const params = createMockParams({ maxTurns: 2, turnDelayMs: 1000 });
		const { result } = renderHook(() => useConversationRunner(params), {
			wrapper: createWrapper(),
		});

		server.use(
			http.post("/api/chat/chat", () => {
				return new HttpResponse(
					new ReadableStream({
						start(controller) {
							const encoder = new TextEncoder();
							controller.enqueue(
								encoder.encode(
									'data: {"choices":[{"delta":{"content":"OK"}}]}\n\n',
								),
							);
							controller.enqueue(encoder.encode("data: [DONE]\n\n"));
							controller.close();
						},
					}),
					{ headers: { "Content-Type": "text/event-stream" } },
				);
			}),
		);

		let runPromise: Promise<void> | undefined;
		act(() => {
			runPromise = result.current.runConversation();
		});

		// Let first turn complete
		await act(async () => {
			vi.advanceTimersByTime(100);
			await Promise.resolve();
		});

		// Advance into turn delay
		await act(async () => {
			vi.advanceTimersByTime(500);
			await Promise.resolve();
		});

		// Stop during delay
		act(() => {
			result.current.handleStopConversation();
		});

		// Let remaining timers flush
		await act(async () => {
			vi.advanceTimersByTime(1000);
			await Promise.resolve();
		});

		await runPromise;

		expect(params.setConversationState).toHaveBeenCalledWith("paused");
		expect(params.setIsStreaming).toHaveBeenCalledWith(false);

		vi.useRealTimers();
	});

	it("skips duplicate state writes when handleStopConversation already cleaned up", async () => {
		// Simulate a stream that closes on abort
		server.use(
			http.post("/api/chat/chat", ({ request }) => {
				const encoder = new TextEncoder();
				const stream = new ReadableStream({
					start(ctrl) {
						ctrl.enqueue(
							encoder.encode(
								'data: {"choices":[{"delta":{"content":"Hel"}}]}\n\n',
							),
						);
						// Close when the abort signal fires so the stream resolves
						request.signal.addEventListener("abort", () => {
							ctrl.close();
						});
					},
				});
				return new Response(stream, {
					headers: { "Content-Type": "text/event-stream" },
				});
			}),
		);

		const params = createMockParams();
		const { result } = renderHook(() => useConversationRunner(params), {
			wrapper: createWrapper(),
		});

		let runPromise: Promise<void> | undefined;
		act(() => {
			runPromise = result.current.runConversation();
		});

		// Let the stream start
		await act(async () => {
			await Promise.resolve();
		});

		// Stop while streaming — handleStopConversation runs synchronously
		act(() => {
			result.current.handleStopConversation();
		});

		// Let the aborted stream resolve
		await act(async () => {
			await Promise.resolve();
			await Promise.resolve();
		});

		if (runPromise) await runPromise;

		// setConversationState should be called exactly once with "paused"
		// (from handleStopConversation), not a second time from the error handler
		const stateCalls = (params.setConversationState as Mock).mock.calls.filter(
			(call: unknown[]) => call[0] === "paused",
		);
		expect(stateCalls).toHaveLength(1);
		expect(params.conversationRunningRef.current).toBe(false);
	});

	it("does not start conversation without Model B", () => {
		const params = createMockParams({
			selectedModel: "provider-a/model-a",
			selectedModelB: "",
		});
		const { result } = renderHook(() => useConversationRunner(params), {
			wrapper: createWrapper(),
		});

		act(() => {
			result.current.runConversation();
		});

		expect(params.setConversationState).not.toHaveBeenCalled();
		expect(params.setIsStreaming).not.toHaveBeenCalled();
	});
});
