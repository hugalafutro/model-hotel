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

	it("alternates between Model A and Model B across turns", async () => {
		const calledModels: string[] = [];
		let nextIdx = 0;
		server.use(
			http.post("/api/chat/chat", async ({ request }) => {
				const idx = nextIdx++;
				const body = await request.json();
				Object.defineProperty(calledModels, idx, {
					value: (body as { model: string }).model,
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

		expect(calledModels.length).toBeGreaterThanOrEqual(4);
		expect(calledModels[0]).toBe("provider-a/model-a");
		expect(calledModels[1]).toBe("provider-b/model-b");
		expect(calledModels[2]).toBe("provider-a/model-a");
		expect(calledModels[3]).toBe("provider-b/model-b");
	});
});
