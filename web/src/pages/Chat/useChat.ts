import { useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { MessageSquare, MessagesSquare } from "@/lib/icons";
import type { ChatMessage, GenerationParams } from "../../api/types";
import { useSidebarMode } from "../../context/SidebarModeContext";
import { useStorage } from "../../context/StorageContext";
import { useToast } from "../../context/ToastContext";
import { useLocalStorage } from "../../hooks/useLocalStorage";
import { useChatModels } from "../../hooks/useModels";
import { parseCapabilities, proxyModelID } from "../../utils/model";
import {
	failedConversationModel,
	lastChatError,
	messageTotals,
} from "./chatDerived";
import { useAssistantStream } from "./useAssistantStream";
import { useChatConversationState } from "./useChatConversationState";
import { useChatPersistence } from "./useChatPersistence";
import { useChatRandomActions } from "./useChatRandom";
import { useChatScroll } from "./useChatScroll";
import { useConversationRunner } from "./useConversationRunner";
import { useDeleteMessage } from "./useDeleteMessage";
import { useMultimodalAttachments } from "./useMultimodalAttachments";
export function useChat() {
	const { data: enabledModels, isLoading: modelsLoading } = useChatModels();
	// False while the chat model list is doing its first load. Actions that would
	// dispatch a selected model wait for this so a persisted stale (now non-chat)
	// selection can't slip through the pre-reconciliation window.
	const modelsReady = !modelsLoading;
	const { chatSubMode, setChatSubMode } = useSidebarMode();
	const { persistChat, persistConversation } = useStorage();

	const [messages, setMessages] = useState<ChatMessage[]>(() => {
		try {
			if (localStorage.getItem("persistChat") === "true") {
				const stored = localStorage.getItem("chatMessages");
				if (stored) return JSON.parse(stored);
			}
			if (localStorage.getItem("persistConversation") === "true") {
				const stored = localStorage.getItem("conversationMessages");
				if (stored) return JSON.parse(stored);
			}
		} catch {
			/* ignore */
		}
		return [];
	});

	useChatPersistence({
		messages,
		chatSubMode,
		persistChat,
		persistConversation,
	});
	// ── Chat mode state ──
	const [chatSelectedModel, setChatSelectedModel] = useLocalStorage<string>(
		"chatSelectedModel",
		"",
		{ enabled: persistChat },
	);
	const [chatSystemPrompt, setChatSystemPrompt] = useLocalStorage<string>(
		"chatSystemPrompt",
		"",
		{ enabled: persistChat },
	);
	const [chatActivePersonaId, setChatActivePersonaId] = useLocalStorage<
		string | null
	>("chatActivePersonaId", null, {
		enabled: persistChat,
		serialize: (v) => v ?? "",
		deserialize: (v) => v || null,
	});
	const [chatMessageParams, setChatMessageParams] = useState<GenerationParams>(
		{},
	);

	// ── Conversation mode state ──
	const {
		conversationModelA,
		setConversationModelA,
		conversationSystemPromptA,
		setConversationSystemPromptA,
		conversationActivePersonaIdA,
		setConversationActivePersonaIdA,
		conversationParamsA,
		setConversationParamsA,
		selectedModelB,
		setSelectedModelB,
		systemPromptB,
		setSystemPromptB,
		activePersonaIdB,
		setActivePersonaIdB,
		messageParamsB,
		setMessageParamsB,
		conversationState,
		setConversationState,
		currentTurn,
		setCurrentTurn,
		turnCountdown,
		setTurnCountdown,
		maxTurns,
		setMaxTurns,
		turnDelayMs,
		setTurnDelayMs,
		configCollapsed,
		setConfigCollapsed,
		conversationAbortRef,
		conversationRunningRef,
		capturedModelARef,
		capturedModelBRef,
	} = useChatConversationState({ persistConversation });

	// ── Shared state ──
	const [pendingFullReset, setPendingFullReset] = useState(false);
	const [input, setInput] = useState("");
	const [isStreaming, setIsStreaming] = useState(false);
	const [controlsCollapsed, setControlsCollapsed] = useState(false);
	/** Saves the conversation prompt before it's cleared, so it can be restored on error */
	const lastPromptRef = useRef<string>("");
	const { toast } = useToast();
	const { t } = useTranslation();

	// Derived state based on current mode
	const selectedModel =
		chatSubMode === "chat" ? chatSelectedModel : conversationModelA;
	const setSelectedModel =
		chatSubMode === "chat" ? setChatSelectedModel : setConversationModelA;
	const systemPrompt =
		chatSubMode === "chat" ? chatSystemPrompt : conversationSystemPromptA;
	const setSystemPrompt =
		chatSubMode === "chat" ? setChatSystemPrompt : setConversationSystemPromptA;
	const activePersonaId =
		chatSubMode === "chat" ? chatActivePersonaId : conversationActivePersonaIdA;
	const setActivePersonaId =
		chatSubMode === "chat"
			? setChatActivePersonaId
			: setConversationActivePersonaIdA;
	const messageParams =
		chatSubMode === "chat" ? chatMessageParams : conversationParamsA;
	const setMessageParams =
		chatSubMode === "chat" ? setChatMessageParams : setConversationParamsA;

	// Reset conversation state when chatSubMode changes (e.g. sidebar click),
	// but skip the initial mount so we don't wipe persisted messages.
	const prevChatSubModeRef = useRef(chatSubMode);
	useEffect(() => {
		if (prevChatSubModeRef.current !== chatSubMode) {
			prevChatSubModeRef.current = chatSubMode;
			setMessages([]);
			setConversationState("idle");
			setCurrentTurn(0);
			setInput("");
		}
	}, [chatSubMode, setCurrentTurn, setConversationState]);

	// Cleanup: abort the conversation stream on unmount. Stored in a separate
	// cleanup ref so the React Compiler doesn't mark conversationAbortRef as
	// "effect-only" and forbid mutation in event handlers - which is perfectly
	// valid React. Chat mode's own abort lives in useAssistantStream.
	const cleanupConvAbortRef = useRef<AbortController | null>(null);
	useEffect(() => {
		const convAbortCtrl = cleanupConvAbortRef;
		return () => {
			convAbortCtrl.current?.abort();
		};
	}, []);

	const selectedModelObj = enabledModels.find(
		(m) => proxyModelID(m.provider_name, m.model_id) === selectedModel,
	);
	const selectedModelObjB = enabledModels.find(
		(m) => proxyModelID(m.provider_name, m.model_id) === selectedModelB,
	);

	// Drop persisted selections that are no longer valid chat models (e.g. a
	// previously-picked model that became an embedding/rerank model, or one
	// that got disabled). Without this a stale localStorage id would stay
	// selected while hidden from the picker, and send/start would route a chat
	// completion to a model that can't serve it. Only runs once the list has
	// loaded so a transient empty fetch never wipes a valid selection.
	useEffect(() => {
		// Reconcile once the list has settled (loaded, success or error). An empty
		// or failed list clears every selection so a stale (now non-chat) id can't
		// be dispatched to a chat endpoint; the loading window itself is covered by
		// the modelsReady guards on send/regenerate/conversation start.
		if (!modelsReady) return;
		const valid = new Set(
			enabledModels.map((m) => proxyModelID(m.provider_name, m.model_id)),
		);
		if (chatSelectedModel && !valid.has(chatSelectedModel))
			setChatSelectedModel("");
		if (conversationModelA && !valid.has(conversationModelA))
			setConversationModelA("");
		if (selectedModelB && !valid.has(selectedModelB)) setSelectedModelB("");
	}, [
		modelsReady,
		enabledModels,
		chatSelectedModel,
		conversationModelA,
		selectedModelB,
		setChatSelectedModel,
		setConversationModelA,
		setSelectedModelB,
	]);

	// ── Model capabilities for attachment icon visibility ──
	const modelCaps = selectedModelObj
		? parseCapabilities(selectedModelObj.capabilities)
		: {};
	const hasVision = !!modelCaps.vision;
	const hasAudioInput = !!modelCaps.audio_input;

	// Extract multimodal attachment state and handlers
	const {
		pendingImage,
		setPendingImage,
		pendingAudio,
		setPendingAudio,
		imageInputRef,
		audioInputRef,
		handlePaste,
		handleImageSelect,
		handleAudioSelect,
	} = useMultimodalAttachments(hasVision, toast);

	const { messagesContainerRef, scrollToBottom } = useChatScroll(
		messages,
		isStreaming,
	);

	const {
		handleRandomPersona,
		handleRandomPersonaB,
		handleRandomModel,
		handleRandomModelB,
	} = useChatRandomActions({
		chatSubMode,
		chatActivePersonaId,
		conversationActivePersonaIdA,
		activePersonaIdB,
		selectedModel,
		selectedModelB,
		enabledModels: enabledModels ?? [],
		setActivePersonaId,
		setSystemPrompt,
		setActivePersonaIdB,
		setSystemPromptB,
		setSelectedModel,
		setSelectedModelB,
	});

	const {
		sendingRef,
		streamAssistantReply,
		handleSend,
		handleStop,
		handleRegenerate,
	} = useAssistantStream({
		messages,
		setMessages,
		input,
		setInput,
		selectedModel,
		systemPrompt,
		messageParams,
		modelsReady,
		isStreaming,
		setIsStreaming,
		pendingImage,
		setPendingImage,
		pendingAudio,
		setPendingAudio,
		toast,
		t,
	});

	// ── Extracted conversation runner hook ──
	const {
		runConversation,
		handleStopConversation,
		handleRetryConversation,
		clearConversationAbort,
	} = useConversationRunner({
		modelsReady,
		selectedModel,
		selectedModelB,
		input,
		messages,
		currentTurn,
		maxTurns,
		turnDelayMs,
		systemPrompt,
		systemPromptB,
		messageParams,
		messageParamsB,
		conversationState,
		toast,
		conversationAbortRef,
		cleanupConvAbortRef,
		conversationRunningRef,
		capturedModelARef,
		capturedModelBRef,
		lastPromptRef,
		setMessages,
		setInput,
		setIsStreaming,
		setConversationState,
		setCurrentTurn,
		setTurnCountdown,
	});

	const handleDeleteMessage = useDeleteMessage({
		messages,
		setMessages,
		chatSubMode,
		isStreaming,
		conversationState,
		setConversationState,
		setCurrentTurn,
		setInput,
		lastPromptRef,
		toast,
		t,
	});

	const handleKeyDown = (e: React.KeyboardEvent) => {
		if (e.key === "Enter" && !e.shiftKey) {
			e.preventDefault();
			if (chatSubMode === "chat") {
				if (isStreaming) {
					setControlsCollapsed(false);
					handleStop();
				} else {
					setControlsCollapsed(true);
					handleSend();
				}
			}
			// In conversation mode, Enter doesn't auto-submit
		}
	};

	const { totalTokens, totalDuration } = messageTotals(messages);

	// Can start if: both models selected, has input, and not currently running
	const canStartConversation =
		chatSubMode === "conversation" &&
		!!selectedModel &&
		!!selectedModelB &&
		selectedModel !== selectedModelB &&
		!!input.trim() &&
		conversationState !== "running";

	const chatError = lastChatError(messages, chatSubMode, selectedModel);
	const failedModel = failedConversationModel(
		messages,
		chatSubMode,
		conversationState,
	);

	const conversationDisabledReason = useMemo(() => {
		if (chatSubMode !== "conversation") return "";
		if (conversationState === "running") return "";
		if (!selectedModel) return t("chat.validation.selectModelA");
		if (!selectedModelB) return t("chat.validation.selectModelB");
		if (selectedModel === selectedModelB)
			return t("chat.validation.modelsMustDiffer");
		if (!input.trim()) return t("chat.validation.enterPrompt");
		return "";
	}, [chatSubMode, selectedModel, selectedModelB, input, conversationState, t]);

	const chatIcon = chatSubMode === "chat" ? MessageSquare : MessagesSquare;

	return {
		// External data
		enabledModels,
		// Context hooks
		toast,
		// Mode state
		chatSubMode,
		setChatSubMode,
		// Messages state
		messages,
		setMessages,
		// Chat mode state
		chatSelectedModel,
		setChatSelectedModel,
		chatSystemPrompt,
		setChatSystemPrompt,
		chatActivePersonaId,
		setChatActivePersonaId,
		chatMessageParams,
		setChatMessageParams,
		// Conversation mode state (Model A)
		conversationModelA,
		setConversationModelA,
		conversationSystemPromptA,
		setConversationSystemPromptA,
		conversationActivePersonaIdA,
		setConversationActivePersonaIdA,
		conversationParamsA,
		setConversationParamsA,
		// Conversation mode state (Model B)
		selectedModelB,
		setSelectedModelB,
		systemPromptB,
		setSystemPromptB,
		activePersonaIdB,
		setActivePersonaIdB,
		messageParamsB,
		setMessageParamsB,
		conversationState,
		setConversationState,
		currentTurn,
		setCurrentTurn,
		turnCountdown,
		setTurnCountdown,
		// Shared state
		pendingFullReset,
		setPendingFullReset,
		input,
		setInput,
		isStreaming,
		setIsStreaming,
		controlsCollapsed,
		setControlsCollapsed,
		pendingImage,
		setPendingImage,
		pendingAudio,
		setPendingAudio,
		maxTurns,
		setMaxTurns,
		turnDelayMs,
		setTurnDelayMs,
		configCollapsed,
		setConfigCollapsed,
		// Derived state
		selectedModel,
		setSelectedModel,
		systemPrompt,
		setSystemPrompt,
		activePersonaId,
		setActivePersonaId,
		messageParams,
		setMessageParams,
		modelCaps,
		hasVision,
		hasAudioInput,
		selectedModelObj,
		selectedModelObjB,
		totalTokens,
		totalDuration,
		canStartConversation,
		lastChatError: chatError,
		failedConversationModel: failedModel,
		conversationDisabledReason,
		chatIcon,
		// Refs are grouped in a sub-object so consumers can destructure them away
		// from render-time state (`const { refs, ...chat } = useChat()`), keeping
		// the react-hooks/refs lint from tainting every state access. Internal
		// refs (abort/cleanup/captured-model) are deliberately not exposed.
		refs: {
			sendingRef,
			lastPromptRef,
			messagesContainerRef,
			imageInputRef,
			audioInputRef,
		},
		// Handlers
		handleRandomPersona,
		handleRandomPersonaB,
		handleRandomModel,
		handleRandomModelB,
		scrollToBottom,
		streamAssistantReply,
		handleSend,
		handlePaste,
		handleImageSelect,
		handleAudioSelect,
		handleStop,
		handleRegenerate,
		runConversation,
		handleStopConversation,
		handleRetryConversation,
		handleDeleteMessage,
		handleKeyDown,
		clearConversationAbort,
	};
}

/** Everything the Chat page and its sections read, minus the refs. */
export type ChatView = Omit<ReturnType<typeof useChat>, "refs">;
export type ChatRefs = ReturnType<typeof useChat>["refs"];
