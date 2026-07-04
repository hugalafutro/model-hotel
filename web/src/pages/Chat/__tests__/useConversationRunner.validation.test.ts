import { act, renderHook } from "@testing-library/react";
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

	it("does not start if conversation is already running", () => {
		const params = createMockParams({
			conversationRunningRef: { current: true },
		});
		const { result } = renderHook(() => useConversationRunner(params), {
			wrapper: createWrapper(),
		});

		act(() => {
			void result.current.runConversation();
		});

		expect(params.setConversationState).not.toHaveBeenCalled();
	});

	it("does not start if in running state", () => {
		const params = createMockParams({
			conversationState: "running" as Parameters<
				typeof useConversationRunner
			>[0]["conversationState"],
		});
		const { result } = renderHook(() => useConversationRunner(params), {
			wrapper: createWrapper(),
		});

		act(() => {
			void result.current.runConversation();
		});

		expect(params.setConversationState).not.toHaveBeenCalled();
	});

	it("does not start while the model list is still loading", () => {
		// Persisted A/B selections can't be validated yet, so a conversation waits
		// rather than risk routing to a now-non-chat model.
		const params = createMockParams({ modelsReady: false });
		const { result } = renderHook(() => useConversationRunner(params), {
			wrapper: createWrapper(),
		});

		act(() => {
			void result.current.runConversation();
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
			void result.current.runConversation();
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
			void result.current.runConversation();
		});

		expect(params.setConversationState).not.toHaveBeenCalled();
	});

	it("starts conversation and sets state to running", () => {
		const params = createMockParams();
		const { result } = renderHook(() => useConversationRunner(params), {
			wrapper: createWrapper(),
		});

		act(() => {
			void result.current.runConversation();
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
			void result.current.runConversation();
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
			void result.current.runConversation();
		});

		expect(params.lastPromptRef.current).toBe("Test prompt");
	});
});
