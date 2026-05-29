import { vi } from "vitest";
import type { ChatMessage } from "../../../api/types";
import type { ConversationState } from "../chatStreaming";

export const createWrapper = () => {
	return function Wrapper({ children }: { children: React.ReactNode }) {
		return children;
	};
};

export const createMockParams = (
	overrides?: Partial<
		Parameters<
			typeof import("../useConversationRunner").useConversationRunner
		>[0]
	>,
) => {
	const messagesState: ChatMessage[] = [];
	const baseParams: Parameters<
		typeof import("../useConversationRunner").useConversationRunner
	>[0] = {
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

export type MockParams = ReturnType<typeof createMockParams>;
