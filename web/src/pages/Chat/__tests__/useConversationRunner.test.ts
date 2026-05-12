import { act, renderHook } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";
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
	const baseParams: Parameters<typeof useConversationRunner>[0] = {
		selectedModel: "provider-a/model-a",
		selectedModelB: "provider-b/model-b",
		input: "Test prompt",
		messages: [],
		currentTurn: 0,
		maxTurns: 2,
		turnDelayMs: 1000,
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
		setMessages: vi.fn(),
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
	it("returns all expected methods", () => {
		const params = createMockParams();
		const { result } = renderHook(() => useConversationRunner(params), {
			wrapper: createWrapper(),
		});

		expect(result.current.runConversation).toBeDefined();
		expect(result.current.handleStopConversation).toBeDefined();
		expect(result.current.handleRetryConversation).toBeDefined();
		expect(result.current.clearConversationAbort).toBeDefined();
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
			turnDelayMs: 100,
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
});
