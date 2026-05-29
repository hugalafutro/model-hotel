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
				Object.defineProperty(calledSystemPrompts, idx, {
					value: systemMsg?.content ?? "",
					writable: true,
					enumerable: true,
					configurable: true,
				});
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
				Object.defineProperty(callMessages, idx, {
					value: messages.filter((m) => m.role !== "system"),
					writable: true,
					enumerable: true,
					configurable: true,
				});
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
});
