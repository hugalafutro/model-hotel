import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { ChatMessage } from "../../../api/types";
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
