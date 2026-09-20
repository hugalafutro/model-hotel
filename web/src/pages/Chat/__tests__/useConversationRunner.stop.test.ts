import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { mockChatStream } from "../../../test/helpers";
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

	it("does not report a run stopped during the countdown as completed", async () => {
		// The loop exits on the abort exactly as it does after the last turn;
		// the stop already settled the state as paused.
		server.use(...mockChatStream([{ choices: [{ delta: { content: "A" } }] }]));
		const params = createMockParams({ maxTurns: 2, turnDelayMs: 60_000 });
		const { result } = renderHook(() => useConversationRunner(params), {
			wrapper: createWrapper(),
		});
		act(() => {
			void result.current.runConversation(false);
		});
		await waitFor(() =>
			expect(params.setTurnCountdown).toHaveBeenCalledWith(60),
		);

		act(() => {
			result.current.handleStopConversation();
		});
		await waitFor(() =>
			expect(params.setIsStreaming).toHaveBeenLastCalledWith(false),
		);
		await new Promise((r) => setTimeout(r, 20));

		expect(params.setConversationState).toHaveBeenLastCalledWith("paused");
		expect(params.setConversationState).not.toHaveBeenCalledWith("completed");
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
});
