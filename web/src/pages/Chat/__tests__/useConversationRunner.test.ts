import { act, renderHook } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import type { Mock } from "vitest";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { ChatMessage } from "../../../api/types";
import { server } from "../../../test/mocks/server";
import type { ConversationState } from "../chatStreaming";
import { useConversationRunner } from "../useConversationRunner";

const createWrapper = () => {
	return function Wrapper({ children }: { children: React.ReactNode }) {
		return children;
	};
};

const createMockParams = (
	overrides?: Partial<Parameters<typeof useConversationRunner>[0]>,
) => {
	const messagesState: ChatMessage[] = [];
	const baseParams: Parameters<typeof useConversationRunner>[0] = {
		selectedModel: "provider-a/model-a",
		selectedModelB: "provider-b/model-b",
		input: "Test prompt",
		get messages() {
			return [...messagesState];
		},
		currentTurn: 0,
		maxTurns: 2,
		turnDelayMs: 100,
		systemPrompt: "System prompt A",
		systemPromptB: "System prompt B",
		messageParams: {},
		messageParamsB: {},
		conversationState: "idle" as ConversationState,
		toast: vi.fn(),
		conversationAbortRef: { current: null },
		cleanupConvAbortRef: { current: null },
		conversationRunningRef: { current: false },
		capturedModelARef: { current: "" },
		capturedModelBRef: { current: "" },
		lastPromptRef: { current: "" },
		setMessages: vi.fn((updater) => {
			if (typeof updater === "function") {
				const fn = updater as (prev: ChatMessage[]) => ChatMessage[];
				messagesState.splice(0, messagesState.length, ...fn(messagesState));
			} else {
				messagesState.splice(0, messagesState.length, ...updater);
			}
			return messagesState;
		}),
		setInput: vi.fn(),
		setIsStreaming: vi.fn(),
		setConversationState: vi.fn(),
		setCurrentTurn: vi.fn(),
		setTurnCountdown: vi.fn(),
		...overrides,
	};
	return baseParams;
};

describe("useConversationRunner", () => {
	beforeEach(() => {
		server.resetHandlers();
		vi.clearAllMocks();
	});
	it("does not start if conversation is already running", () => {
		const params = createMockParams({
			conversationRunningRef: { current: true },
		});
		const { result } = renderHook(() => useConversationRunner(params), {
			wrapper: createWrapper(),
		});

		act(() => {
			result.current.runConversation();
		});

		expect(params.setConversationState).not.toHaveBeenCalled();
	});

	it("does not start if in running state", () => {
		const params = createMockParams({
			conversationState: "running" as ConversationState,
		});
		const { result } = renderHook(() => useConversationRunner(params), {
			wrapper: createWrapper(),
		});

		act(() => {
			result.current.runConversation();
		});

		expect(params.setConversationState).not.toHaveBeenCalled();
	});

	it("does not start if no selected models", () => {
		const params = createMockParams({
			selectedModel: "",
			selectedModelB: "provider-b/model-b",
		});
		const { result } = renderHook(() => useConversationRunner(params), {
			wrapper: createWrapper(),
		});

		act(() => {
			result.current.runConversation();
		});

		expect(params.setConversationState).not.toHaveBeenCalled();
	});

	it("does not start if no input and not resuming", () => {
		const params = createMockParams({
			input: "",
		});
		const { result } = renderHook(() => useConversationRunner(params), {
			wrapper: createWrapper(),
		});

		act(() => {
			result.current.runConversation();
		});

		expect(params.setConversationState).not.toHaveBeenCalled();
	});

	it("starts conversation and sets state to running", () => {
		const params = createMockParams();
		const { result } = renderHook(() => useConversationRunner(params), {
			wrapper: createWrapper(),
		});

		act(() => {
			result.current.runConversation();
		});

		expect(params.setConversationState).toHaveBeenCalledWith("running");
		expect(params.setIsStreaming).toHaveBeenCalledWith(true);
		expect(params.setCurrentTurn).toHaveBeenCalledWith(0);
	});

	it("captures models and creates user message on start", () => {
		const params = createMockParams();
		const setMessagesMock = vi.fn();
		const setInputMock = vi.fn();
		params.setMessages = setMessagesMock;
		params.setInput = setInputMock;

		const { result } = renderHook(() => useConversationRunner(params), {
			wrapper: createWrapper(),
		});

		act(() => {
			result.current.runConversation();
		});

		expect(params.capturedModelARef.current).toBe("provider-a/model-a");
		expect(params.capturedModelBRef.current).toBe("provider-b/model-b");
		expect(setMessagesMock).toHaveBeenCalled();
		expect(setInputMock).toHaveBeenCalledWith("");
	});

	it("saves prompt to lastPromptRef on start", () => {
		const params = createMockParams();
		const { result } = renderHook(() => useConversationRunner(params), {
			wrapper: createWrapper(),
		});

		act(() => {
			result.current.runConversation();
		});

		expect(params.lastPromptRef.current).toBe("Test prompt");
	});

	it("handles stop conversation", () => {
		const abortController = new AbortController();
		const params = createMockParams({
			conversationAbortRef: { current: abortController },
			cleanupConvAbortRef: { current: abortController },
			conversationRunningRef: { current: true },
		});
		const { result } = renderHook(() => useConversationRunner(params), {
			wrapper: createWrapper(),
		});

		act(() => {
			result.current.handleStopConversation();
		});

		expect(abortController.signal.aborted).toBe(true);
		expect(params.conversationAbortRef.current).toBe(null);
		expect(params.cleanupConvAbortRef.current).toBe(null);
		expect(params.setTurnCountdown).toHaveBeenCalledWith(0);
		expect(params.setIsStreaming).toHaveBeenCalledWith(false);
		expect(params.setConversationState).toHaveBeenCalledWith("paused");
		expect(params.conversationRunningRef.current).toBe(false);
	});

	it("handles stop when no abort controller", () => {
		const params = createMockParams({
			conversationAbortRef: { current: null },
			cleanupConvAbortRef: { current: null },
		});
		const { result } = renderHook(() => useConversationRunner(params), {
			wrapper: createWrapper(),
		});

		act(() => {
			result.current.handleStopConversation();
		});

		expect(params.setConversationState).toHaveBeenCalledWith("paused");
	});

	it("clears abort controller", () => {
		const abortController = new AbortController();
		const params = createMockParams({
			conversationAbortRef: { current: abortController },
			cleanupConvAbortRef: { current: abortController },
			conversationRunningRef: { current: true },
		});
		const { result } = renderHook(() => useConversationRunner(params), {
			wrapper: createWrapper(),
		});

		act(() => {
			result.current.clearConversationAbort();
		});

		expect(abortController.signal.aborted).toBe(true);
		expect(params.conversationAbortRef.current).toBe(null);
		expect(params.conversationRunningRef.current).toBe(false);
	});

	it("retries from error state - first turn", () => {
		const messages: ChatMessage[] = [
			{ role: "user", content: "Hello", timestamp: 1 },
			{ role: "assistant", content: "", model: "model-a", timestamp: 2 },
		];
		const params = createMockParams({
			conversationState: "error" as ConversationState,
			messages,
			currentTurn: 0,
		});
		const setMessagesMock = vi.fn();
		setMessagesMock.mockImplementation((fn) => {
			if (typeof fn === "function") {
				fn(messages);
			}
		});
		params.setMessages = setMessagesMock;

		vi.useFakeTimers();
		const { result } = renderHook(() => useConversationRunner(params), {
			wrapper: createWrapper(),
		});

		act(() => {
			result.current.handleRetryConversation();
		});

		act(() => {
			vi.advanceTimersByTime(100);
		});

		expect(params.setConversationState).toHaveBeenCalledWith("idle");
		expect(params.setCurrentTurn).toHaveBeenCalledWith(0);
		expect(setMessagesMock).toHaveBeenCalled();

		vi.useRealTimers();
	});

	it("retries from error state - later turn", () => {
		const messages: ChatMessage[] = [
			{ role: "user", content: "Hello", timestamp: 1 },
			{
				role: "assistant",
				content: "Response A",
				model: "model-a",
				timestamp: 2,
			},
			{ role: "assistant", content: "", model: "model-b", timestamp: 3 },
		];
		const params = createMockParams({
			conversationState: "error" as ConversationState,
			messages,
			currentTurn: 2,
		});
		const setMessagesMock = vi.fn();
		setMessagesMock.mockImplementation((fn) => {
			if (typeof fn === "function") {
				fn(messages);
			}
		});
		params.setMessages = setMessagesMock;

		vi.useFakeTimers();
		const { result } = renderHook(() => useConversationRunner(params), {
			wrapper: createWrapper(),
		});

		act(() => {
			result.current.handleRetryConversation();
		});

		act(() => {
			vi.advanceTimersByTime(100);
		});

		expect(params.setConversationState).toHaveBeenCalledWith("paused");
		expect(params.setCurrentTurn).toHaveBeenCalledWith(1);
		expect(setMessagesMock).toHaveBeenCalled();

		vi.useRealTimers();
	});

	it("does not retry if not in error state", () => {
		const params = createMockParams({
			conversationState: "idle" as ConversationState,
		});
		const { result } = renderHook(() => useConversationRunner(params), {
			wrapper: createWrapper(),
		});

		act(() => {
			result.current.handleRetryConversation();
		});

		expect(params.setConversationState).not.toHaveBeenCalled();
	});

	it("streams model response successfully", async () => {
		server.use(
			http.post("/api/chat/chat", async () => {
				const encoder = new TextEncoder();
				const stream = new ReadableStream({
					start(controller) {
						controller.enqueue(
							encoder.encode(
								'data: {"choices":[{"delta":{"content":"Hello"}}]}\n\n',
							),
						);
						controller.enqueue(
							encoder.encode(
								'data: {"choices":[{"delta":{"content":" world"}}]}\n\n',
							),
						);
						controller.enqueue(
							encoder.encode(
								'data: {"choices":[{"delta":{}}],"usage":{"prompt_tokens":10,"completion_tokens":5}}\n\n',
							),
						);
						controller.enqueue(encoder.encode("data: [DONE]\n\n"));
						controller.close();
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

		await act(async () => {
			await result.current.runConversation();
		});

		expect(params.setConversationState).toHaveBeenCalledWith("completed");
		expect(params.setIsStreaming).toHaveBeenCalledWith(false);
	});

	it("handles stream error and shows toast", async () => {
		server.use(
			http.post("/api/chat/chat", () => {
				return HttpResponse.json({ error: "Model not found" }, { status: 404 });
			}),
		);

		const params = createMockParams();
		const toastMock = vi.fn();
		params.toast = toastMock;

		const { result } = renderHook(() => useConversationRunner(params), {
			wrapper: createWrapper(),
		});

		await act(async () => {
			await result.current.runConversation();
		});

		expect(toastMock).toHaveBeenCalled();
		expect(params.setConversationState).toHaveBeenCalledWith("error");
	});

	it("resumes from paused state with correct model turn", () => {
		const messages: ChatMessage[] = [
			{ role: "user", content: "Hello", timestamp: 1 },
			{
				role: "assistant",
				content: "Response A",
				model: "provider-a/model-a",
				timestamp: 2,
			},
		];
		const params = createMockParams({
			messages,
			capturedModelARef: { current: "provider-a/model-a" },
			capturedModelBRef: { current: "provider-b/model-b" },
		});

		const { result } = renderHook(() => useConversationRunner(params), {
			wrapper: createWrapper(),
		});

		act(() => {
			result.current.runConversation(true);
		});

		expect(params.setConversationState).toHaveBeenCalledWith("running");
		expect(params.setCurrentTurn).not.toHaveBeenCalledWith(0);
	});

	it("respects turn delay between responses", async () => {
		server.use(
			http.post("/api/chat/chat", async () => {
				const encoder = new TextEncoder();
				const stream = new ReadableStream({
					start(controller) {
						controller.enqueue(
							encoder.encode(
								'data: {"choices":[{"delta":{"content":"OK"}}]}\n\n',
							),
						);
						controller.enqueue(encoder.encode("data: [DONE]\n\n"));
						controller.close();
					},
				});
				return new Response(stream, {
					headers: { "Content-Type": "text/event-stream" },
				});
			}),
		);

		const params = createMockParams({
			maxTurns: 2,
			turnDelayMs: 1,
		});
		const setTurnCountdownMock = vi.fn();
		params.setTurnCountdown = setTurnCountdownMock;

		const { result } = renderHook(() => useConversationRunner(params), {
			wrapper: createWrapper(),
		});

		await act(async () => {
			await result.current.runConversation();
		});

		expect(setTurnCountdownMock).toHaveBeenCalled();
	});

	it("restores prompt to input on first turn error", async () => {
		server.use(
			http.post("/api/chat/chat", () => {
				return HttpResponse.json({ error: "Failed" }, { status: 500 });
			}),
		);

		const setInputMock = vi.fn();
		const params = createMockParams({
			setInput: setInputMock,
		});

		const { result } = renderHook(() => useConversationRunner(params), {
			wrapper: createWrapper(),
		});

		await act(async () => {
			await result.current.runConversation();
		});

		expect(setInputMock).toHaveBeenCalledWith("Test prompt");
	});

	it("does not restore prompt on later turn error", async () => {
		let callCount = 0;
		server.use(
			http.post("/api/chat/chat", async () => {
				callCount++;
				if (callCount === 1) {
					const encoder = new TextEncoder();
					const stream = new ReadableStream({
						start(controller) {
							controller.enqueue(
								encoder.encode(
									'data: {"choices":[{"delta":{"content":"First"}}]}\n\n',
								),
							);
							controller.enqueue(encoder.encode("data: [DONE]\n\n"));
							controller.close();
						},
					});
					return new Response(stream, {
						headers: { "Content-Type": "text/event-stream" },
					});
				}
				return HttpResponse.json({ error: "Failed" }, { status: 500 });
			}),
		);

		const setInputMock = vi.fn();
		const params = createMockParams({
			setInput: setInputMock,
			maxTurns: 2,
		});

		const { result } = renderHook(() => useConversationRunner(params), {
			wrapper: createWrapper(),
		});

		await act(async () => {
			await result.current.runConversation();
		});

		expect(setInputMock).not.toHaveBeenCalledWith("Test prompt");
	});

	it("alternates between Model A and Model B across turns", async () => {
		const calledModels: string[] = [];
		let nextIdx = 0;
		server.use(
			http.post("/api/chat/chat", async ({ request }) => {
				const idx = nextIdx++;
				const body = await request.json();
				calledModels[idx] = (body as { model: string }).model; // nosemgrep: typescript.lang.security.audit.detect-object-injection
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

		const params = createMockParams({ maxTurns: 2, turnDelayMs: 1 });
		const { result } = renderHook(() => useConversationRunner(params), {
			wrapper: createWrapper(),
		});

		await act(async () => {
			await result.current.runConversation();
		});

		expect(calledModels.length).toBeGreaterThanOrEqual(4);
		expect(calledModels[0]).toBe("provider-a/model-a");
		expect(calledModels[1]).toBe("provider-b/model-b");
		expect(calledModels[2]).toBe("provider-a/model-a");
		expect(calledModels[3]).toBe("provider-b/model-b");
	});

	it("passes correct system prompt for each model", async () => {
		const calledSystemPrompts: string[] = [];
		let nextIdx = 0;
		server.use(
			http.post("/api/chat/chat", async ({ request }) => {
				const idx = nextIdx++;
				const body = await request.json();
				const messages = (
					body as { messages: Array<{ role: string; content: string }> }
				).messages;
				const systemMsg = messages.find((m) => m.role === "system");
				calledSystemPrompts[idx] = systemMsg?.content ?? ""; // nosemgrep: typescript.lang.security.audit.detect-object-injection
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

		const params = createMockParams({ maxTurns: 2, turnDelayMs: 1 });
		const { result } = renderHook(() => useConversationRunner(params), {
			wrapper: createWrapper(),
		});

		await act(async () => {
			await result.current.runConversation();
		});

		// Minimum 4 calls (2 turns × 2 models), but can be higher under React strict mode
		expect(calledSystemPrompts.length).toBeGreaterThanOrEqual(4);
		// Each model should be called at least twice (once per turn)
		const promptACount = calledSystemPrompts.filter(
			(p) => p === "System prompt A",
		).length;
		const promptBCount = calledSystemPrompts.filter(
			(p) => p === "System prompt B",
		).length;
		expect(promptACount).toBeGreaterThanOrEqual(2);
		expect(promptBCount).toBeGreaterThanOrEqual(2);
		// Verify ABAB interleaving: consecutive pairs should contain one A and one B
		for (let i = 0; i < calledSystemPrompts.length - 1; i += 2) {
			const pair = [calledSystemPrompts[i], calledSystemPrompts[i + 1]];
			const hasA = pair.some((p) => p === "System prompt A");
			const hasB = pair.some((p) => p === "System prompt B");
			expect(hasA && hasB).toBe(true);
		}
	});

	it("passes message history to each turn", async () => {
		const callMessages: Array<{ role: string; content: string }[]> = [];
		let nextIdx = 0;
		server.use(
			http.post("/api/chat/chat", async ({ request }) => {
				const idx = nextIdx++;
				const body = await request.json();
				const messages = (
					body as { messages: Array<{ role: string; content: string }> }
				).messages;
				callMessages[idx] = messages.filter((m) => m.role !== "system"); // nosemgrep: typescript.lang.security.audit.detect-object-injection
				return new HttpResponse(
					new ReadableStream({
						start(controller) {
							const encoder = new TextEncoder();
							controller.enqueue(
								encoder.encode(
									'data: {"choices":[{"delta":{"content":"Response"}}]}\n\n',
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

		const params = createMockParams({ maxTurns: 2, turnDelayMs: 1 });
		const { result } = renderHook(() => useConversationRunner(params), {
			wrapper: createWrapper(),
		});

		await act(async () => {
			await result.current.runConversation();
		});

		// First turn (Model A): receives only user message
		expect(callMessages[0]).toHaveLength(1);
		expect(callMessages[0][0].role).toBe("user");

		// Second turn (Model B): receives user message + Model A's response
		// Note: getApiMessagesForModel converts other model's assistant messages to user role
		expect(callMessages[1]).toHaveLength(2);
		expect(callMessages[1][0].role).toBe("user");
		expect(callMessages[1][1].role).toBe("user"); // Model A's response appears as user message to Model B
		expect(callMessages[1][1].content).toBe("Response");

		// Verify message history grows
		expect(callMessages[2]).toHaveLength(3);
		expect(callMessages[3]).toHaveLength(4);
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

	it("clearConversationAbort aborts and cleans up", () => {
		const abortController = new AbortController();
		const params = createMockParams({
			conversationAbortRef: { current: abortController },
			cleanupConvAbortRef: { current: abortController },
			conversationRunningRef: { current: true },
		});
		const { result } = renderHook(() => useConversationRunner(params), {
			wrapper: createWrapper(),
		});

		act(() => {
			result.current.clearConversationAbort();
		});

		expect(abortController.signal.aborted).toBe(true);
		expect(params.conversationAbortRef.current).toBe(null);
		expect(params.cleanupConvAbortRef.current).toBe(null);
		expect(params.conversationRunningRef.current).toBe(false);
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
