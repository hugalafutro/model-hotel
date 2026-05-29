import { act, renderHook } from "@testing-library/react";
import { HttpResponse, http } from "msw";
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
		const messages = [
			{ role: "user" as const, content: "Hello", timestamp: 1 },
			{
				role: "assistant" as const,
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
});
