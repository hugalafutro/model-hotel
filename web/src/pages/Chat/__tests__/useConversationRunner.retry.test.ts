import { act, renderHook, waitFor } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { ChatMessage } from "../../../api/types";
import { createSSEStream } from "../../../test/helpers";
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

	it("retries from error state - first turn", () => {
		const messages: ChatMessage[] = [
			{ role: "user", content: "Hello", timestamp: 1 },
			{ role: "assistant", content: "", model: "model-a", timestamp: 2 },
		];
		const params = createMockParams({
			conversationState: "error" as Parameters<
				typeof useConversationRunner
			>[0]["conversationState"],
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
			conversationState: "error" as Parameters<
				typeof useConversationRunner
			>[0]["conversationState"],
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

	it("retries the model that failed, without the errored reply", async () => {
		// Model B's turn errored. The retry must run from the transcript with
		// that reply removed: B is asked again (not A), and the empty errored
		// assistant message is not sent upstream as context.
		const messages: ChatMessage[] = [
			{ role: "user", content: "Hello", timestamp: 1 },
			{
				role: "assistant",
				content: "Response A",
				model: "provider-a/model-a",
				timestamp: 2,
			},
			{
				role: "assistant",
				content: "",
				model: "provider-b/model-b",
				timestamp: 3,
			},
		];
		const bodies: Array<{
			model: string;
			messages: Array<{ role: string; content: unknown }>;
		}> = [];
		server.use(
			http.post("/api/chat/chat", async ({ request }) => {
				bodies.push((await request.json()) as (typeof bodies)[number]);
				return new HttpResponse(
					createSSEStream([{ choices: [{ delta: { content: "B again" } }] }]),
					{ headers: { "Content-Type": "text/event-stream" } },
				);
			}),
		);
		const params = createMockParams({
			conversationState: "error" as Parameters<
				typeof useConversationRunner
			>[0]["conversationState"],
			messages,
			currentTurn: 2,
			maxTurns: 2,
			capturedModelARef: { current: "provider-a/model-a" },
			capturedModelBRef: { current: "provider-b/model-b" },
		});
		const { result } = renderHook(() => useConversationRunner(params), {
			wrapper: createWrapper(),
		});

		act(() => {
			result.current.handleRetryConversation();
		});
		await waitFor(() => expect(bodies.length).toBeGreaterThan(0));
		params.conversationAbortRef.current?.abort();

		expect(bodies[0].model).toBe("provider-b/model-b");
		// A's reply reaches B as a user turn; the errored empty reply is gone.
		expect(bodies[0].messages).toEqual([
			{ role: "system", content: "System prompt B" },
			{ role: "user", content: "Hello" },
			{ role: "user", content: "Response A" },
		]);
	});

	it("retries a failed first turn with the prompt once", async () => {
		// The error handler put the prompt back into `input`; the fresh start
		// appends it from there, so the transcript must not carry it too.
		const messages: ChatMessage[] = [
			{ role: "user", content: "Hello", timestamp: 1 },
			{
				role: "assistant",
				content: "",
				model: "provider-a/model-a",
				timestamp: 2,
			},
		];
		const bodies: Array<{
			model: string;
			messages: Array<{ role: string; content: unknown }>;
		}> = [];
		server.use(
			http.post("/api/chat/chat", async ({ request }) => {
				bodies.push((await request.json()) as (typeof bodies)[number]);
				return new HttpResponse(
					createSSEStream([{ choices: [{ delta: { content: "A" } }] }]),
					{ headers: { "Content-Type": "text/event-stream" } },
				);
			}),
		);
		const params = createMockParams({
			conversationState: "error" as Parameters<
				typeof useConversationRunner
			>[0]["conversationState"],
			messages,
			currentTurn: 0,
			input: "Hello",
		});
		const { result } = renderHook(() => useConversationRunner(params), {
			wrapper: createWrapper(),
		});

		act(() => {
			result.current.handleRetryConversation();
		});
		await waitFor(() => expect(bodies.length).toBeGreaterThan(0));
		params.conversationAbortRef.current?.abort();

		expect(params.setMessages).toHaveBeenCalledWith([]);
		expect(bodies[0].model).toBe("provider-a/model-a");
		expect(bodies[0].messages).toEqual([
			{ role: "system", content: "System prompt A" },
			{ role: "user", content: "Hello" },
		]);
	});

	it("does not retry if not in error state", () => {
		const params = createMockParams({
			conversationState: "idle" as Parameters<
				typeof useConversationRunner
			>[0]["conversationState"],
		});
		const { result } = renderHook(() => useConversationRunner(params), {
			wrapper: createWrapper(),
		});

		act(() => {
			result.current.handleRetryConversation();
		});

		expect(params.setConversationState).not.toHaveBeenCalled();
	});
});
