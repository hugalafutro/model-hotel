import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { ChatMessage } from "../../../api/types";
import type { ChatSubMode } from "../../../context/SidebarModeContext";
import type { ConversationState } from "../chatStreaming";
import { useChat } from "../useChat";

// ── Mock functions ──
const mockToast = vi.fn();
const mockSetConversationState = vi.fn();
const mockSetCurrentTurn = vi.fn();
const mockSetPendingImage = vi.fn();
const mockSetPendingAudio = vi.fn();
const mockStreamModelResponse = vi.fn();
const mockGetApiMessagesForModel = vi.fn();

// ── Mock all dependencies ──
vi.mock("../../../context/SidebarModeContext", () => ({
	useSidebarMode: vi.fn(() => ({
		chatSubMode: "chat" as ChatSubMode,
		setChatSubMode: vi.fn(),
		arenaSubMode: "competition",
		setArenaSubMode: vi.fn(),
		logsSubMode: "request",
		setLogsSubMode: vi.fn(),
	})),
}));

vi.mock("../../../context/StorageContext", () => ({
	useStorage: vi.fn(() => ({
		persistChat: false,
		setPersistChat: vi.fn(),
		persistArena: false,
		setPersistArena: vi.fn(),
		persistConversation: false,
		setPersistConversation: vi.fn(),
		arenaHistoryEnabled: false,
		setArenaHistoryEnabled: vi.fn(),
		arenaHistoryLimit: 25,
		setArenaHistoryLimit: vi.fn(),
	})),
}));

vi.mock("../../../context/ToastContext", () => ({
	useToast: vi.fn(() => ({
		toast: mockToast,
		position: "bottom-right" as const,
		setPosition: vi.fn(),
		timeout: 3000,
		setTimeout: vi.fn(),
	})),
}));

vi.mock("../../../hooks/useModels", () => ({
	useEnabledModels: vi.fn(() => ({
		data: [],
		isLoading: false,
		isError: false,
		error: null,
		isSuccess: true,
		status: "success" as const,
		fetchStatus: "idle" as const,
		isFetching: false,
		isPending: false,
		isLoadingError: false,
		isRefetchError: false,
		isStale: false,
		refetch: vi.fn(),
	})),
}));

vi.mock("../useChatConversationState", () => ({
	useChatConversationState: vi.fn(() => ({
		conversationModelA: "",
		setConversationModelA: vi.fn(),
		conversationSystemPromptA: "",
		setConversationSystemPromptA: vi.fn(),
		conversationActivePersonaIdA: null,
		setConversationActivePersonaIdA: vi.fn(),
		conversationParamsA: {},
		setConversationParamsA: vi.fn(),
		selectedModelB: "",
		setSelectedModelB: vi.fn(),
		systemPromptB: "",
		setSystemPromptB: vi.fn(),
		activePersonaIdB: null,
		setActivePersonaIdB: vi.fn(),
		messageParamsB: {},
		setMessageParamsB: vi.fn(),
		conversationState: "idle" as ConversationState,
		setConversationState: mockSetConversationState,
		currentTurn: 0,
		setCurrentTurn: mockSetCurrentTurn,
		turnCountdown: 0,
		setTurnCountdown: vi.fn(),
		maxTurns: 10,
		setMaxTurns: vi.fn(),
		turnDelayMs: 500,
		setTurnDelayMs: vi.fn(),
		configCollapsed: false,
		setConfigCollapsed: vi.fn(),
		conversationAbortRef: { current: null },
		conversationRunningRef: { current: false },
		capturedModelARef: { current: "" },
		capturedModelBRef: { current: "" },
	})),
}));

vi.mock("../useChatPersistence", () => ({
	useChatPersistence: vi.fn(() => undefined),
}));

vi.mock("../useChatRandom", () => ({
	useChatRandomActions: vi.fn(() => ({
		handleRandomPersona: vi.fn(),
		handleRandomPersonaB: vi.fn(),
		handleRandomModel: vi.fn(),
		handleRandomModelB: vi.fn(),
	})),
}));

vi.mock("../useConversationRunner", () => ({
	useConversationRunner: vi.fn(() => ({
		runConversation: vi.fn(),
		handleStopConversation: vi.fn(),
		handleRetryConversation: vi.fn(),
		clearConversationAbort: vi.fn(),
	})),
}));

vi.mock("../useMultimodalAttachments", () => ({
	useMultimodalAttachments: vi.fn(() => ({
		hasVision: false,
		pendingImage: null,
		setPendingImage: mockSetPendingImage,
		pendingAudio: null,
		setPendingAudio: mockSetPendingAudio,
		imageInputRef: { current: null },
		audioInputRef: { current: null },
		handlePaste: vi.fn(),
		handleImageSelect: vi.fn(),
		handleAudioSelect: vi.fn(),
	})),
}));

vi.mock("../../../utils/model", () => ({
	parseCapabilities: vi.fn(() => ({})),
	proxyModelID: (providerName: string, modelId: string) =>
		`${providerName}/${modelId}`,
}));

vi.mock("../../../utils/params", () => ({
	hasAnyParam: vi.fn(() => false),
}));

vi.mock("../chatStreaming", () => ({
	getApiMessagesForModel: vi.fn(() => mockGetApiMessagesForModel()),
	streamModelResponse: vi.fn(() => mockStreamModelResponse()),
}));

// Import mocked modules for reconfiguration
const SidebarModeContext = await import("../../../context/SidebarModeContext");
const StorageContext = await import("../../../context/StorageContext");
const ChatConversationState = await import("../useChatConversationState");
const MultimodalAttachments = await import("../useMultimodalAttachments");

// Factory functions to eliminate duplicate mock return values
function createMockConversationState(overrides?: Record<string, unknown>) {
	return {
		conversationModelA: "",
		setConversationModelA: vi.fn(),
		conversationSystemPromptA: "",
		setConversationSystemPromptA: vi.fn(),
		conversationActivePersonaIdA: null,
		setConversationActivePersonaIdA: vi.fn(),
		conversationParamsA: {},
		setConversationParamsA: vi.fn(),
		selectedModelB: "",
		setSelectedModelB: vi.fn(),
		systemPromptB: "",
		setSystemPromptB: vi.fn(),
		activePersonaIdB: null,
		setActivePersonaIdB: vi.fn(),
		messageParamsB: {},
		setMessageParamsB: vi.fn(),
		conversationState: "idle" as ConversationState,
		setConversationState: mockSetConversationState,
		currentTurn: 0,
		setCurrentTurn: mockSetCurrentTurn,
		turnCountdown: 0,
		setTurnCountdown: vi.fn(),
		maxTurns: 10,
		setMaxTurns: vi.fn(),
		turnDelayMs: 500,
		setTurnDelayMs: vi.fn(),
		configCollapsed: false,
		setConfigCollapsed: vi.fn(),
		conversationAbortRef: { current: null },
		conversationRunningRef: { current: false },
		capturedModelARef: { current: "" },
		capturedModelBRef: { current: "" },
		...overrides,
	};
}

describe("useChat", () => {
	beforeEach(() => {
		vi.clearAllMocks();
		localStorage.clear();
		mockToast.mockClear();
		mockSetConversationState.mockClear();
		mockSetCurrentTurn.mockClear();
		mockSetPendingImage.mockClear();
		mockSetPendingAudio.mockClear();
		mockStreamModelResponse.mockClear();
		mockGetApiMessagesForModel.mockClear();
		// Reset to defaults
		vi.mocked(SidebarModeContext.useSidebarMode).mockReturnValue({
			chatSubMode: "chat",
			setChatSubMode: vi.fn(),
			arenaSubMode: "competition",
			setArenaSubMode: vi.fn(),
			logsSubMode: "request",
			setLogsSubMode: vi.fn(),
		});
		vi.mocked(StorageContext.useStorage).mockReturnValue({
			persistChat: false,
			setPersistChat: vi.fn(),
			persistArena: false,
			setPersistArena: vi.fn(),
			persistConversation: false,
			setPersistConversation: vi.fn(),
			arenaHistoryEnabled: false,
			setArenaHistoryEnabled: vi.fn(),
			arenaHistoryLimit: 25,
			setArenaHistoryLimit: vi.fn(),
		});
		vi.mocked(ChatConversationState.useChatConversationState).mockReturnValue(
			createMockConversationState(),
		);
	});

	describe("handleKeyDown", () => {
		it("calls handleStop and sets controlsCollapsed when Enter pressed during streaming in chat mode", () => {
			const { result } = renderHook(() => useChat());
			act(() => {
				result.current.setIsStreaming(true);
			});
			const preventDefault = vi.fn();
			act(() => {
				result.current.handleKeyDown({
					key: "Enter",
					shiftKey: false,
					preventDefault,
				} as unknown as React.KeyboardEvent);
			});
			expect(preventDefault).toHaveBeenCalled();
			expect(result.current.controlsCollapsed).toBe(false);
			expect(result.current.isStreaming).toBe(false);
		});

		it("calls handleSend when Enter pressed without shift in chat mode and not streaming", () => {
			mockStreamModelResponse.mockResolvedValue({
				rawContent: "",
				content: "Response",
				thinkingContent: "",
				tokensPerSecond: 10,
				durationMs: 1000,
				promptTokens: 10,
				completionTokens: 20,
				error: null,
				aborted: false,
			});
			mockGetApiMessagesForModel.mockReturnValue([
				{ role: "user", content: "Hello" },
			]);
			const { result } = renderHook(() => useChat());
			act(() => {
				result.current.setInput("Hello");
				result.current.setSelectedModel("Ollama/llama3");
			});
			const preventDefault = vi.fn();
			act(() => {
				result.current.handleKeyDown({
					key: "Enter",
					shiftKey: false,
					preventDefault,
				} as unknown as React.KeyboardEvent);
			});
			expect(preventDefault).toHaveBeenCalled();
			expect(result.current.isStreaming).toBe(true);
			expect(result.current.messages.length).toBeGreaterThanOrEqual(1);
			expect(result.current.messages[0].content).toBe("Hello");
		});

		it("does nothing when Shift+Enter is pressed (no-op branch)", () => {
			const { result } = renderHook(() => useChat());
			const preventDefault = vi.fn();
			act(() => {
				result.current.handleKeyDown({
					key: "Enter",
					shiftKey: true,
					preventDefault,
				} as unknown as React.KeyboardEvent);
			});
			expect(preventDefault).not.toHaveBeenCalled();
		});

		it("does nothing when Enter pressed in conversation mode (no-op branch)", () => {
			vi.mocked(SidebarModeContext.useSidebarMode).mockReturnValue({
				chatSubMode: "conversation",
				setChatSubMode: vi.fn(),
				arenaSubMode: "competition",
				setArenaSubMode: vi.fn(),
				logsSubMode: "request",
				setLogsSubMode: vi.fn(),
			});
			const { result } = renderHook(() => useChat());
			const handleSendSpy = vi.spyOn(result.current, "handleSend");
			const handleStopSpy = vi.spyOn(result.current, "handleStop");
			act(() => {
				result.current.handleKeyDown({
					key: "Enter",
					shiftKey: false,
					preventDefault: vi.fn(),
				} as unknown as React.KeyboardEvent);
			});
			expect(handleSendSpy).not.toHaveBeenCalled();
			expect(handleStopSpy).not.toHaveBeenCalled();
		});
	});

	describe("failedConversationModel", () => {
		it("returns model name without provider prefix when error exists in conversation mode", () => {
			vi.mocked(SidebarModeContext.useSidebarMode).mockReturnValue({
				chatSubMode: "conversation",
				setChatSubMode: vi.fn(),
				arenaSubMode: "competition",
				setArenaSubMode: vi.fn(),
				logsSubMode: "request",
				setLogsSubMode: vi.fn(),
			});
			vi.mocked(ChatConversationState.useChatConversationState).mockReturnValue(
				createMockConversationState({ conversationState: "error" }),
			);
			vi.mocked(StorageContext.useStorage).mockReturnValue({
				persistChat: false,
				setPersistChat: vi.fn(),
				persistArena: false,
				setPersistArena: vi.fn(),
				persistConversation: true,
				setPersistConversation: vi.fn(),
				arenaHistoryEnabled: false,
				setArenaHistoryEnabled: vi.fn(),
				arenaHistoryLimit: 25,
				setArenaHistoryLimit: vi.fn(),
			});
			const errorMessages: ChatMessage[] = [
				{ role: "user", content: "Hello", timestamp: 1 },
				{
					role: "assistant",
					content: "",
					timestamp: 2,
					model: "Provider-A/gpt-4",
					error: "Rate limit exceeded",
				},
			];
			localStorage.setItem(
				"conversationMessages",
				JSON.stringify(errorMessages),
			);
			localStorage.setItem("persistConversation", "true");
			const { result } = renderHook(() => useChat());
			expect(result.current.failedConversationModel).toBe("gpt-4");
		});

		it("returns undefined when no error messages exist", () => {
			vi.mocked(SidebarModeContext.useSidebarMode).mockReturnValue({
				chatSubMode: "conversation",
				setChatSubMode: vi.fn(),
				arenaSubMode: "competition",
				setArenaSubMode: vi.fn(),
				logsSubMode: "request",
				setLogsSubMode: vi.fn(),
			});
			vi.mocked(ChatConversationState.useChatConversationState).mockReturnValue(
				createMockConversationState({
					conversationState: "error",
					setConversationState: vi.fn(),
					currentTurn: 0,
					setCurrentTurn: vi.fn(),
				}),
			);
			vi.mocked(StorageContext.useStorage).mockReturnValue({
				persistChat: false,
				setPersistChat: vi.fn(),
				persistArena: false,
				setPersistArena: vi.fn(),
				persistConversation: true,
				setPersistConversation: vi.fn(),
				arenaHistoryEnabled: false,
				setArenaHistoryEnabled: vi.fn(),
				arenaHistoryLimit: 25,
				setArenaHistoryLimit: vi.fn(),
			});
			const messages: ChatMessage[] = [
				{ role: "user", content: "Hello", timestamp: 1 },
				{
					role: "assistant",
					content: "Hi",
					timestamp: 2,
					model: "Provider/model",
				},
			];
			localStorage.setItem("conversationMessages", JSON.stringify(messages));
			localStorage.setItem("persistConversation", "true");
			const { result } = renderHook(() => useChat());
			expect(result.current.failedConversationModel).toBeUndefined();
		});

		it("returns undefined when not in conversation mode", () => {
			const { result } = renderHook(() => useChat());
			expect(result.current.failedConversationModel).toBeUndefined();
		});

		it("returns undefined when conversationState is not error", () => {
			vi.mocked(SidebarModeContext.useSidebarMode).mockReturnValue({
				chatSubMode: "conversation",
				setChatSubMode: vi.fn(),
				arenaSubMode: "competition",
				setArenaSubMode: vi.fn(),
				logsSubMode: "request",
				setLogsSubMode: vi.fn(),
			});
			const { result } = renderHook(() => useChat());
			expect(result.current.failedConversationModel).toBeUndefined();
		});

		it("returns full model name when model has no '/' separator", () => {
			vi.mocked(SidebarModeContext.useSidebarMode).mockReturnValue({
				chatSubMode: "conversation",
				setChatSubMode: vi.fn(),
				arenaSubMode: "competition",
				setArenaSubMode: vi.fn(),
				logsSubMode: "request",
				setLogsSubMode: vi.fn(),
			});
			vi.mocked(ChatConversationState.useChatConversationState).mockReturnValue(
				createMockConversationState({
					conversationState: "error",
					setConversationState: vi.fn(),
					currentTurn: 0,
					setCurrentTurn: vi.fn(),
				}),
			);
			vi.mocked(StorageContext.useStorage).mockReturnValue({
				persistChat: false,
				setPersistChat: vi.fn(),
				persistArena: false,
				setPersistArena: vi.fn(),
				persistConversation: true,
				setPersistConversation: vi.fn(),
				arenaHistoryEnabled: false,
				setArenaHistoryEnabled: vi.fn(),
				arenaHistoryLimit: 25,
				setArenaHistoryLimit: vi.fn(),
			});
			const errorMessages: ChatMessage[] = [
				{
					role: "assistant",
					content: "",
					timestamp: 1,
					model: "noSlashModel",
					error: "Failed",
				},
			];
			localStorage.setItem(
				"conversationMessages",
				JSON.stringify(errorMessages),
			);
			localStorage.setItem("persistConversation", "true");
			const { result } = renderHook(() => useChat());
			expect(result.current.failedConversationModel).toBe("noSlashModel");
		});

		it("returns undefined when error message has no model property", () => {
			vi.mocked(SidebarModeContext.useSidebarMode).mockReturnValue({
				chatSubMode: "conversation",
				setChatSubMode: vi.fn(),
				arenaSubMode: "competition",
				setArenaSubMode: vi.fn(),
				logsSubMode: "request",
				setLogsSubMode: vi.fn(),
			});
			vi.mocked(ChatConversationState.useChatConversationState).mockReturnValue(
				createMockConversationState({
					conversationState: "error",
					setConversationState: vi.fn(),
					currentTurn: 0,
					setCurrentTurn: vi.fn(),
				}),
			);
			vi.mocked(StorageContext.useStorage).mockReturnValue({
				persistChat: false,
				setPersistChat: vi.fn(),
				persistArena: false,
				setPersistArena: vi.fn(),
				persistConversation: true,
				setPersistConversation: vi.fn(),
				arenaHistoryEnabled: false,
				setArenaHistoryEnabled: vi.fn(),
				arenaHistoryLimit: 25,
				setArenaHistoryLimit: vi.fn(),
			});
			const errorMessages: ChatMessage[] = [
				{ role: "assistant", content: "", timestamp: 1, error: "Failed" },
			];
			localStorage.setItem(
				"conversationMessages",
				JSON.stringify(errorMessages),
			);
			localStorage.setItem("persistConversation", "true");
			const { result } = renderHook(() => useChat());
			expect(result.current.failedConversationModel).toBeUndefined();
		});
	});

	describe("lastChatError", () => {
		it("returns null when error model differs from selectedModel", () => {
			const errorMessages: ChatMessage[] = [
				{
					role: "assistant",
					content: "",
					timestamp: 1,
					model: "Provider/different-model",
					error: "Old error",
				},
			];
			localStorage.setItem("chatMessages", JSON.stringify(errorMessages));
			localStorage.setItem("persistChat", "true");
			vi.mocked(StorageContext.useStorage).mockReturnValue({
				persistChat: true,
				setPersistChat: vi.fn(),
				persistArena: false,
				setPersistArena: vi.fn(),
				persistConversation: false,
				setPersistConversation: vi.fn(),
				arenaHistoryEnabled: false,
				setArenaHistoryEnabled: vi.fn(),
				arenaHistoryLimit: 25,
				setArenaHistoryLimit: vi.fn(),
			});
			const { result } = renderHook(() => useChat());
			act(() => {
				result.current.setChatSelectedModel("Provider/current-model");
			});
			expect(result.current.lastChatError).toBeNull();
		});

		it("returns error when error model matches selectedModel", () => {
			const errorMessages: ChatMessage[] = [
				{
					role: "assistant",
					content: "",
					timestamp: 1,
					model: "Provider/same-model",
					error: "Current error",
					aborted: false,
				},
			];
			localStorage.setItem("chatMessages", JSON.stringify(errorMessages));
			localStorage.setItem("persistChat", "true");
			vi.mocked(StorageContext.useStorage).mockReturnValue({
				persistChat: true,
				setPersistChat: vi.fn(),
				persistArena: false,
				setPersistArena: vi.fn(),
				persistConversation: false,
				setPersistConversation: vi.fn(),
				arenaHistoryEnabled: false,
				setArenaHistoryEnabled: vi.fn(),
				arenaHistoryLimit: 25,
				setArenaHistoryLimit: vi.fn(),
			});
			const { result } = renderHook(() => useChat());
			act(() => {
				result.current.setChatSelectedModel("Provider/same-model");
			});
			expect(result.current.lastChatError).toEqual({
				error: "Current error",
				model: "Provider/same-model",
			});
		});

		it("returns null when message was aborted", () => {
			const errorMessages: ChatMessage[] = [
				{
					role: "assistant",
					content: "",
					timestamp: 1,
					model: "Provider/model",
					error: "Aborted",
					aborted: true,
				},
			];
			localStorage.setItem("chatMessages", JSON.stringify(errorMessages));
			localStorage.setItem("persistChat", "true");
			vi.mocked(StorageContext.useStorage).mockReturnValue({
				persistChat: true,
				setPersistChat: vi.fn(),
				persistArena: false,
				setPersistArena: vi.fn(),
				persistConversation: false,
				setPersistConversation: vi.fn(),
				arenaHistoryEnabled: false,
				setArenaHistoryEnabled: vi.fn(),
				arenaHistoryLimit: 25,
				setArenaHistoryLimit: vi.fn(),
			});
			const { result } = renderHook(() => useChat());
			act(() => {
				result.current.setChatSelectedModel("Provider/model");
			});
			expect(result.current.lastChatError).toBeNull();
		});

		it("returns null in conversation mode", () => {
			vi.mocked(SidebarModeContext.useSidebarMode).mockReturnValue({
				chatSubMode: "conversation",
				setChatSubMode: vi.fn(),
				arenaSubMode: "competition",
				setArenaSubMode: vi.fn(),
				logsSubMode: "request",
				setLogsSubMode: vi.fn(),
			});
			const { result } = renderHook(() => useChat());
			expect(result.current.lastChatError).toBeNull();
		});
	});

	describe("conversationDisabledReason", () => {
		it("returns empty string when not in conversation mode", () => {
			const { result } = renderHook(() => useChat());
			expect(result.current.conversationDisabledReason).toBe("");
		});

		it("returns empty string when conversation is running", () => {
			vi.mocked(SidebarModeContext.useSidebarMode).mockReturnValue({
				chatSubMode: "conversation",
				setChatSubMode: vi.fn(),
				arenaSubMode: "competition",
				setArenaSubMode: vi.fn(),
				logsSubMode: "request",
				setLogsSubMode: vi.fn(),
			});
			vi.mocked(ChatConversationState.useChatConversationState).mockReturnValue(
				createMockConversationState({
					conversationState: "running",
					setConversationState: vi.fn(),
					currentTurn: 0,
					setCurrentTurn: vi.fn(),
				}),
			);
			const { result } = renderHook(() => useChat());
			expect(result.current.conversationDisabledReason).toBe("");
		});

		it("returns 'Select Model A' when model A not selected", () => {
			vi.mocked(SidebarModeContext.useSidebarMode).mockReturnValue({
				chatSubMode: "conversation",
				setChatSubMode: vi.fn(),
				arenaSubMode: "competition",
				setArenaSubMode: vi.fn(),
				logsSubMode: "request",
				setLogsSubMode: vi.fn(),
			});
			vi.mocked(ChatConversationState.useChatConversationState).mockReturnValue(
				createMockConversationState({
					conversationModelA: "",
					selectedModelB: "Provider/model-b",
					conversationState: "idle",
					setConversationState: vi.fn(),
					currentTurn: 0,
					setCurrentTurn: vi.fn(),
				}),
			);
			const { result } = renderHook(() => useChat());
			act(() => {
				result.current.setInput("Test prompt");
			});
			expect(result.current.conversationDisabledReason).toBe("Select Model A");
		});

		it("returns 'Select Model B' when model B not selected", () => {
			vi.mocked(SidebarModeContext.useSidebarMode).mockReturnValue({
				chatSubMode: "conversation",
				setChatSubMode: vi.fn(),
				arenaSubMode: "competition",
				setArenaSubMode: vi.fn(),
				logsSubMode: "request",
				setLogsSubMode: vi.fn(),
			});
			vi.mocked(ChatConversationState.useChatConversationState).mockReturnValue(
				createMockConversationState({
					conversationModelA: "Provider/model-a",
					selectedModelB: "",
					conversationState: "idle",
					setConversationState: vi.fn(),
					currentTurn: 0,
					setCurrentTurn: vi.fn(),
				}),
			);
			const { result } = renderHook(() => useChat());
			act(() => {
				result.current.setInput("Test prompt");
			});
			expect(result.current.conversationDisabledReason).toBe("Select Model B");
		});

		it("returns 'Models must be different' when both models are the same", () => {
			vi.mocked(SidebarModeContext.useSidebarMode).mockReturnValue({
				chatSubMode: "conversation",
				setChatSubMode: vi.fn(),
				arenaSubMode: "competition",
				setArenaSubMode: vi.fn(),
				logsSubMode: "request",
				setLogsSubMode: vi.fn(),
			});
			vi.mocked(ChatConversationState.useChatConversationState).mockReturnValue(
				createMockConversationState({
					conversationModelA: "Provider/same-model",
					selectedModelB: "Provider/same-model",
					conversationState: "idle",
					setConversationState: vi.fn(),
					currentTurn: 0,
					setCurrentTurn: vi.fn(),
				}),
			);
			const { result } = renderHook(() => useChat());
			act(() => {
				result.current.setInput("Test prompt");
			});
			expect(result.current.conversationDisabledReason).toBe(
				"Models must be different",
			);
		});

		it("returns 'Enter a prompt' when input is empty", () => {
			vi.mocked(SidebarModeContext.useSidebarMode).mockReturnValue({
				chatSubMode: "conversation",
				setChatSubMode: vi.fn(),
				arenaSubMode: "competition",
				setArenaSubMode: vi.fn(),
				logsSubMode: "request",
				setLogsSubMode: vi.fn(),
			});
			vi.mocked(ChatConversationState.useChatConversationState).mockReturnValue(
				createMockConversationState({
					conversationModelA: "Provider/model-a",
					selectedModelB: "Provider/model-b",
					conversationState: "idle",
					setConversationState: vi.fn(),
					currentTurn: 0,
					setCurrentTurn: vi.fn(),
				}),
			);
			const { result } = renderHook(() => useChat());
			act(() => {
				result.current.setInput("");
			});
			expect(result.current.conversationDisabledReason).toBe("Enter a prompt");
		});

		it("returns empty string when all conditions are met", () => {
			vi.mocked(SidebarModeContext.useSidebarMode).mockReturnValue({
				chatSubMode: "conversation",
				setChatSubMode: vi.fn(),
				arenaSubMode: "competition",
				setArenaSubMode: vi.fn(),
				logsSubMode: "request",
				setLogsSubMode: vi.fn(),
			});
			vi.mocked(ChatConversationState.useChatConversationState).mockReturnValue(
				createMockConversationState({
					conversationModelA: "Provider/model-a",
					selectedModelB: "Provider/model-b",
					conversationState: "idle",
					setConversationState: vi.fn(),
					currentTurn: 0,
					setCurrentTurn: vi.fn(),
				}),
			);
			const { result } = renderHook(() => useChat());
			act(() => {
				result.current.setInput("Test prompt");
			});
			expect(result.current.conversationDisabledReason).toBe("");
		});
	});

	describe("canStartConversation", () => {
		it("is true when all conditions are met", () => {
			vi.mocked(SidebarModeContext.useSidebarMode).mockReturnValue({
				chatSubMode: "conversation",
				setChatSubMode: vi.fn(),
				arenaSubMode: "competition",
				setArenaSubMode: vi.fn(),
				logsSubMode: "request",
				setLogsSubMode: vi.fn(),
			});
			vi.mocked(ChatConversationState.useChatConversationState).mockReturnValue(
				createMockConversationState({
					conversationModelA: "Provider/model-a",
					selectedModelB: "Provider/model-b",
					conversationState: "idle",
					setConversationState: vi.fn(),
					currentTurn: 0,
					setCurrentTurn: vi.fn(),
				}),
			);
			const { result } = renderHook(() => useChat());
			act(() => {
				result.current.setInput("Test prompt");
			});
			expect(result.current.canStartConversation).toBe(true);
		});

		it("is false when not in conversation mode", () => {
			const { result } = renderHook(() => useChat());
			expect(result.current.canStartConversation).toBe(false);
		});

		it("is false when model A not selected", () => {
			vi.mocked(SidebarModeContext.useSidebarMode).mockReturnValue({
				chatSubMode: "conversation",
				setChatSubMode: vi.fn(),
				arenaSubMode: "competition",
				setArenaSubMode: vi.fn(),
				logsSubMode: "request",
				setLogsSubMode: vi.fn(),
			});
			vi.mocked(ChatConversationState.useChatConversationState).mockReturnValue(
				createMockConversationState({
					conversationModelA: "",
					selectedModelB: "Provider/model-b",
					conversationState: "idle",
					setConversationState: vi.fn(),
					currentTurn: 0,
					setCurrentTurn: vi.fn(),
				}),
			);
			const { result } = renderHook(() => useChat());
			expect(result.current.canStartConversation).toBe(false);
		});

		it("is false when model B not selected", () => {
			vi.mocked(SidebarModeContext.useSidebarMode).mockReturnValue({
				chatSubMode: "conversation",
				setChatSubMode: vi.fn(),
				arenaSubMode: "competition",
				setArenaSubMode: vi.fn(),
				logsSubMode: "request",
				setLogsSubMode: vi.fn(),
			});
			vi.mocked(ChatConversationState.useChatConversationState).mockReturnValue(
				createMockConversationState({
					conversationModelA: "Provider/model-a",
					selectedModelB: "",
					conversationState: "idle",
					setConversationState: vi.fn(),
					currentTurn: 0,
					setCurrentTurn: vi.fn(),
				}),
			);
			const { result } = renderHook(() => useChat());
			expect(result.current.canStartConversation).toBe(false);
		});

		it("is false when models are the same", () => {
			vi.mocked(SidebarModeContext.useSidebarMode).mockReturnValue({
				chatSubMode: "conversation",
				setChatSubMode: vi.fn(),
				arenaSubMode: "competition",
				setArenaSubMode: vi.fn(),
				logsSubMode: "request",
				setLogsSubMode: vi.fn(),
			});
			vi.mocked(ChatConversationState.useChatConversationState).mockReturnValue(
				createMockConversationState({
					conversationModelA: "Provider/same",
					selectedModelB: "Provider/same",
					conversationState: "idle",
					setConversationState: vi.fn(),
					currentTurn: 0,
					setCurrentTurn: vi.fn(),
				}),
			);
			const { result } = renderHook(() => useChat());
			expect(result.current.canStartConversation).toBe(false);
		});

		it("is false when input is empty", () => {
			vi.mocked(SidebarModeContext.useSidebarMode).mockReturnValue({
				chatSubMode: "conversation",
				setChatSubMode: vi.fn(),
				arenaSubMode: "competition",
				setArenaSubMode: vi.fn(),
				logsSubMode: "request",
				setLogsSubMode: vi.fn(),
			});
			vi.mocked(ChatConversationState.useChatConversationState).mockReturnValue(
				createMockConversationState({
					conversationModelA: "Provider/model-a",
					selectedModelB: "Provider/model-b",
					conversationState: "idle",
					setConversationState: vi.fn(),
					currentTurn: 0,
					setCurrentTurn: vi.fn(),
				}),
			);
			const { result } = renderHook(() => useChat());
			expect(result.current.canStartConversation).toBe(false);
		});

		it("is false when conversation is running", () => {
			vi.mocked(SidebarModeContext.useSidebarMode).mockReturnValue({
				chatSubMode: "conversation",
				setChatSubMode: vi.fn(),
				arenaSubMode: "competition",
				setArenaSubMode: vi.fn(),
				logsSubMode: "request",
				setLogsSubMode: vi.fn(),
			});
			vi.mocked(ChatConversationState.useChatConversationState).mockReturnValue(
				createMockConversationState({
					conversationModelA: "Provider/model-a",
					selectedModelB: "Provider/model-b",
					conversationState: "running",
					setConversationState: vi.fn(),
					currentTurn: 0,
					setCurrentTurn: vi.fn(),
				}),
			);
			const { result } = renderHook(() => useChat());
			act(() => {
				result.current.setInput("Test prompt");
			});
			expect(result.current.canStartConversation).toBe(false);
		});
	});

	describe("chatSubMode change effect", () => {
		it("clears messages, conversationState, and input when mode changes", () => {
			const { result, rerender } = renderHook(() => useChat());
			act(() => {
				result.current.setMessages([
					{ role: "user", content: "Hello", timestamp: 1 },
				]);
				result.current.setInput("Test");
			});
			vi.mocked(SidebarModeContext.useSidebarMode).mockReturnValue({
				chatSubMode: "conversation",
				setChatSubMode: vi.fn(),
				arenaSubMode: "competition",
				setArenaSubMode: vi.fn(),
				logsSubMode: "request",
				setLogsSubMode: vi.fn(),
			});
			rerender();
			expect(result.current.messages).toEqual([]);
			expect(result.current.input).toBe("");
		});
	});

	describe("handleDeleteMessage", () => {
		it("shows toast error when deleting non-last assistant message in conversation mode", () => {
			vi.mocked(SidebarModeContext.useSidebarMode).mockReturnValue({
				chatSubMode: "conversation",
				setChatSubMode: vi.fn(),
				arenaSubMode: "competition",
				setArenaSubMode: vi.fn(),
				logsSubMode: "request",
				setLogsSubMode: vi.fn(),
			});
			const { result } = renderHook(() => useChat());
			act(() => {
				result.current.setMessages([
					{ role: "user", content: "First", timestamp: 1 },
					{ role: "assistant", content: "First response", timestamp: 2 },
					{ role: "user", content: "Second", timestamp: 3 },
					{ role: "assistant", content: "Second response", timestamp: 4 },
				]);
			});
			act(() => {
				result.current.handleDeleteMessage(1);
			});
			expect(mockToast).toHaveBeenCalledWith(
				"Can only delete the most recent response",
				"error",
			);
		});

		it("deletes the last streaming message successfully", () => {
			vi.mocked(SidebarModeContext.useSidebarMode).mockReturnValue({
				chatSubMode: "conversation",
				setChatSubMode: vi.fn(),
				arenaSubMode: "competition",
				setArenaSubMode: vi.fn(),
				logsSubMode: "request",
				setLogsSubMode: vi.fn(),
			});
			const { result } = renderHook(() => useChat());
			act(() => {
				result.current.setIsStreaming(true);
				result.current.setMessages([
					{ role: "user", content: "Hello", timestamp: 1 },
					{ role: "assistant", content: "", timestamp: 2 },
				]);
			});
			act(() => {
				result.current.handleDeleteMessage(1);
			});
			expect(result.current.messages.length).toBe(0);
			expect(mockToast).toHaveBeenCalledWith("Message deleted", "info");
		});

		it("in chat mode, deletes assistant message and preceding user message", () => {
			const { result } = renderHook(() => useChat());
			act(() => {
				result.current.setMessages([
					{ role: "user", content: "Hello", timestamp: 1 },
					{ role: "assistant", content: "Hi", timestamp: 2 },
				]);
			});
			act(() => {
				result.current.handleDeleteMessage(1);
			});
			expect(result.current.messages.length).toBe(0);
			expect(mockToast).toHaveBeenCalledWith("Message deleted", "info");
		});

		it("in chat mode, deletes only the message if preceding message is not user", () => {
			const { result } = renderHook(() => useChat());
			act(() => {
				result.current.setMessages([
					{ role: "assistant", content: "First", timestamp: 1 },
					{ role: "assistant", content: "Second", timestamp: 2 },
				]);
			});
			act(() => {
				result.current.handleDeleteMessage(1);
			});
			expect(result.current.messages).toHaveLength(1);
			expect(result.current.messages[0].role).toBe("assistant");
			expect(mockToast).toHaveBeenCalledWith("Message deleted", "info");
		});

		it("in conversation mode, when remaining is empty, restores to idle", () => {
			vi.mocked(SidebarModeContext.useSidebarMode).mockReturnValue({
				chatSubMode: "conversation",
				setChatSubMode: vi.fn(),
				arenaSubMode: "competition",
				setArenaSubMode: vi.fn(),
				logsSubMode: "request",
				setLogsSubMode: vi.fn(),
			});
			const { result } = renderHook(() => useChat());
			act(() => {
				result.current.setIsStreaming(false);
				result.current.setMessages([
					{ role: "user", content: "Hello", timestamp: 1 },
					{ role: "assistant", content: "Hi", timestamp: 2 },
				]);
			});
			act(() => {
				result.current.handleDeleteMessage(1);
			});
			expect(result.current.messages).toEqual([]);
			expect(mockSetConversationState).toHaveBeenCalledWith("idle");
			expect(mockSetCurrentTurn).toHaveBeenCalledWith(0);
			expect(mockToast).toHaveBeenCalledWith("Message deleted", "info");
		});

		it("in conversation mode, when only one user message remains, restores to idle", () => {
			vi.mocked(SidebarModeContext.useSidebarMode).mockReturnValue({
				chatSubMode: "conversation",
				setChatSubMode: vi.fn(),
				arenaSubMode: "competition",
				setArenaSubMode: vi.fn(),
				logsSubMode: "request",
				setLogsSubMode: vi.fn(),
			});
			const { result } = renderHook(() => useChat());
			act(() => {
				result.current.setIsStreaming(false);
				result.current.setMessages([
					{ role: "user", content: "First", timestamp: 1 },
					{ role: "assistant", content: "First response", timestamp: 2 },
					{ role: "assistant", content: "Second response", timestamp: 3 },
				]);
			});
			act(() => {
				result.current.handleDeleteMessage(2);
			});
			expect(result.current.messages).toEqual([]);
			expect(mockSetConversationState).toHaveBeenCalledWith("idle");
			expect(mockSetCurrentTurn).toHaveBeenCalledWith(0);
			expect(mockToast).toHaveBeenCalledWith("Message deleted", "info");
		});

		it("in conversation mode, when prevState is error, transitions to paused", () => {
			vi.mocked(SidebarModeContext.useSidebarMode).mockReturnValue({
				chatSubMode: "conversation",
				setChatSubMode: vi.fn(),
				arenaSubMode: "competition",
				setArenaSubMode: vi.fn(),
				logsSubMode: "request",
				setLogsSubMode: vi.fn(),
			});
			vi.mocked(ChatConversationState.useChatConversationState).mockReturnValue(
				createMockConversationState({
					conversationState: "error",
					setConversationState: mockSetConversationState,
				}),
			);
			const { result } = renderHook(() => useChat());
			act(() => {
				result.current.setIsStreaming(false);
				result.current.setMessages([
					{ role: "user", content: "First", timestamp: 1 },
					{ role: "assistant", content: "First response", timestamp: 2 },
					{ role: "user", content: "Second", timestamp: 3 },
					{ role: "assistant", content: "Second response", timestamp: 4 },
				]);
			});
			act(() => {
				result.current.handleDeleteMessage(3);
			});
			expect(mockSetConversationState).toHaveBeenCalledWith("paused");
			expect(mockSetCurrentTurn).toHaveBeenCalledWith(1);
			expect(mockToast).toHaveBeenCalledWith("Message deleted", "info");
		});
	});

	describe("handleSend", () => {
		it("includes pendingImage in the message when present", () => {
			const pendingImageData = {
				dataUrl: "data:image/png;base64,test",
				format: "png",
				name: "test.png",
			};
			vi.mocked(MultimodalAttachments.useMultimodalAttachments).mockReturnValue(
				{
					hasVision: false,
					pendingImage: pendingImageData,
					setPendingImage: mockSetPendingImage,
					pendingAudio: null,
					setPendingAudio: mockSetPendingAudio,
					imageInputRef: { current: null },
					audioInputRef: { current: null },
					handlePaste: vi.fn(),
					handleImageSelect: vi.fn(),
					handleAudioSelect: vi.fn(),
				},
			);
			mockGetApiMessagesForModel.mockReturnValue([
				{ role: "user", content: "Hello with image" },
			]);
			mockStreamModelResponse.mockResolvedValue({
				rawContent: "",
				content: "Response",
				thinkingContent: "",
				tokensPerSecond: 10,
				durationMs: 1000,
				promptTokens: 10,
				completionTokens: 20,
				error: null,
				aborted: false,
			});
			const { result } = renderHook(() => useChat());
			act(() => {
				result.current.setInput("Hello with image");
				result.current.setChatSelectedModel("Provider/model");
			});
			act(() => {
				result.current.handleSend();
			});
			expect(result.current.messages.length).toBeGreaterThanOrEqual(1);
			const userMsg = result.current.messages.find((m) => m.role === "user");
			expect(userMsg?.imageUrl).toBe("data:image/png;base64,test");
			expect(mockSetPendingImage).toHaveBeenCalledWith(null);
		});

		it("returns early when already sending (sendingRef)", () => {
			const { result } = renderHook(() => useChat());
			act(() => {
				result.current.setInput("Hello");
				result.current.setChatSelectedModel("Provider/model");
				result.current.sendingRef.current = true;
			});
			act(() => {
				result.current.handleSend();
			});
			expect(result.current.messages.length).toBe(0);
		});

		it("shows toast error for non-AbortError errors", async () => {
			mockGetApiMessagesForModel.mockReturnValue([
				{ role: "user", content: "Hello" },
			]);
			mockStreamModelResponse.mockRejectedValue(new Error("Network error"));
			const { result } = renderHook(() => useChat());
			act(() => {
				result.current.setInput("Hello");
				result.current.setChatSelectedModel("Provider/model");
			});
			await act(async () => {
				await result.current.handleSend();
			});
			expect(mockToast).toHaveBeenCalledWith("Network error", "error");
		});

		it("does not show toast for AbortError", async () => {
			mockGetApiMessagesForModel.mockReturnValue([
				{ role: "user", content: "Hello" },
			]);
			const abortError = new Error("Aborted");
			abortError.name = "AbortError";
			mockStreamModelResponse.mockRejectedValue(abortError);
			const { result } = renderHook(() => useChat());
			act(() => {
				result.current.setInput("Hello");
				result.current.setChatSelectedModel("Provider/model");
			});
			await act(async () => {
				await result.current.handleSend();
			});
			expect(mockToast).not.toHaveBeenCalled();
		});

		it("does nothing when no model is selected", () => {
			vi.mocked(MultimodalAttachments.useMultimodalAttachments).mockReturnValue(
				{
					hasVision: false,
					pendingImage: null,
					setPendingImage: mockSetPendingImage,
					pendingAudio: null,
					setPendingAudio: mockSetPendingAudio,
					imageInputRef: { current: null },
					audioInputRef: { current: null },
					handlePaste: vi.fn(),
					handleImageSelect: vi.fn(),
					handleAudioSelect: vi.fn(),
				},
			);
			const { result } = renderHook(() => useChat());
			act(() => {
				result.current.setInput("Hello");
				result.current.setChatSelectedModel("");
			});
			act(() => {
				result.current.handleSend();
			});
			expect(result.current.messages).toEqual([]);
		});

		it("does nothing when input is empty and no attachments", () => {
			vi.mocked(MultimodalAttachments.useMultimodalAttachments).mockReturnValue(
				{
					hasVision: false,
					pendingImage: null,
					setPendingImage: mockSetPendingImage,
					pendingAudio: null,
					setPendingAudio: mockSetPendingAudio,
					imageInputRef: { current: null },
					audioInputRef: { current: null },
					handlePaste: vi.fn(),
					handleImageSelect: vi.fn(),
					handleAudioSelect: vi.fn(),
				},
			);
			const { result } = renderHook(() => useChat());
			act(() => {
				result.current.setInput("");
				result.current.setChatSelectedModel("Provider/model");
			});
			act(() => {
				result.current.handleSend();
			});
			expect(result.current.messages).toEqual([]);
		});
	});

	describe("handleRegenerate", () => {
		it("returns early when there are no messages", () => {
			const { result } = renderHook(() => useChat());
			act(() => {
				result.current.setMessages([]);
			});
			act(() => {
				result.current.handleRegenerate();
			});
			expect(result.current.messages.length).toBe(0);
		});

		it("returns early when no user messages exist", () => {
			const { result } = renderHook(() => useChat());
			act(() => {
				result.current.setMessages([
					{ role: "assistant", content: "Orphan response", timestamp: 1 },
				]);
			});
			act(() => {
				result.current.handleRegenerate();
			});
			expect(result.current.messages.length).toBe(1);
		});

		it("does nothing when already streaming", () => {
			const { result } = renderHook(() => useChat());
			act(() => {
				result.current.setIsStreaming(true);
				result.current.setMessages([
					{ role: "user", content: "Hello", timestamp: 1 },
					{ role: "assistant", content: "Hi", timestamp: 2 },
				]);
			});
			act(() => {
				result.current.handleRegenerate();
			});
			expect(result.current.isStreaming).toBe(true);
		});

		it("regenerates by removing last user+assistant pair and re-streaming", async () => {
			mockGetApiMessagesForModel.mockReturnValue([
				{ role: "user", content: "Second" },
			]);
			mockStreamModelResponse.mockResolvedValue({
				rawContent: "",
				content: "New response",
				thinkingContent: "",
				tokensPerSecond: 10,
				durationMs: 1000,
				promptTokens: 10,
				completionTokens: 20,
				error: null,
				aborted: false,
			});
			const { result } = renderHook(() => useChat());
			act(() => {
				result.current.setMessages([
					{ role: "user", content: "First", timestamp: 1 },
					{ role: "assistant", content: "First response", timestamp: 2 },
					{ role: "user", content: "Second", timestamp: 3 },
					{ role: "assistant", content: "Second response", timestamp: 4 },
				]);
				result.current.setChatSelectedModel("test/model");
			});
			await act(async () => {
				await result.current.handleRegenerate();
			});
			expect(result.current.isStreaming).toBe(false);
			expect(mockStreamModelResponse).toHaveBeenCalled();
			// Original "Second" user and "Second response" assistant removed;
			// new user message + streaming assistant placeholder added
			const userMessages = result.current.messages.filter(
				(m) => m.role === "user",
			);
			expect(userMessages).toHaveLength(2);
			expect(userMessages[0].content).toBe("First");
			expect(userMessages[1].content).toBe("Second");
		});
	});

	describe("messages initializer", () => {
		it("reads from localStorage 'chatMessages' when persistChat=true", () => {
			const storedMessages: ChatMessage[] = [
				{ role: "user", content: "Persisted", timestamp: 1 },
			];
			localStorage.setItem("persistChat", "true");
			localStorage.setItem("chatMessages", JSON.stringify(storedMessages));
			vi.mocked(StorageContext.useStorage).mockReturnValue({
				persistChat: true,
				setPersistChat: vi.fn(),
				persistArena: false,
				setPersistArena: vi.fn(),
				persistConversation: false,
				setPersistConversation: vi.fn(),
				arenaHistoryEnabled: false,
				setArenaHistoryEnabled: vi.fn(),
				arenaHistoryLimit: 25,
				setArenaHistoryLimit: vi.fn(),
			});
			const { result } = renderHook(() => useChat());
			expect(result.current.messages).toEqual(storedMessages);
		});

		it("reads from localStorage 'conversationMessages' when persistConversation=true", () => {
			const storedMessages: ChatMessage[] = [
				{ role: "user", content: "Persisted conversation", timestamp: 1 },
			];
			localStorage.setItem("persistConversation", "true");
			localStorage.setItem(
				"conversationMessages",
				JSON.stringify(storedMessages),
			);
			vi.mocked(StorageContext.useStorage).mockReturnValue({
				persistChat: false,
				setPersistChat: vi.fn(),
				persistArena: false,
				setPersistArena: vi.fn(),
				persistConversation: true,
				setPersistConversation: vi.fn(),
				arenaHistoryEnabled: false,
				setArenaHistoryEnabled: vi.fn(),
				arenaHistoryLimit: 25,
				setArenaHistoryLimit: vi.fn(),
			});
			const { result } = renderHook(() => useChat());
			expect(result.current.messages).toEqual(storedMessages);
		});

		it("returns empty array when no persistence is enabled", () => {
			const { result } = renderHook(() => useChat());
			expect(result.current.messages).toEqual([]);
		});

		it("handles localStorage parse errors gracefully", () => {
			localStorage.setItem("persistChat", "true");
			localStorage.setItem("chatMessages", "invalid json");
			vi.mocked(StorageContext.useStorage).mockReturnValue({
				persistChat: true,
				setPersistChat: vi.fn(),
				persistArena: false,
				setPersistArena: vi.fn(),
				persistConversation: false,
				setPersistConversation: vi.fn(),
				arenaHistoryEnabled: false,
				setArenaHistoryEnabled: vi.fn(),
				arenaHistoryLimit: 25,
				setArenaHistoryLimit: vi.fn(),
			});
			const { result } = renderHook(() => useChat());
			expect(result.current.messages).toEqual([]);
		});
	});
});
