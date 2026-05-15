import { MessageSquare, MessagesSquare } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type {
	ChatMessage,
	GenerationParams,
	MessageContent,
} from "../../api/types";
import { useSidebarMode } from "../../context/SidebarModeContext";
import { useStorage } from "../../context/StorageContext";
import { useToast } from "../../context/ToastContext";
import { useLocalStorage } from "../../hooks/useLocalStorage";
import { useEnabledModels } from "../../hooks/useModels";
import { parseCapabilities, proxyModelID } from "../../utils/model";
import { hasAnyParam } from "../../utils/params";
import { getApiMessagesForModel, streamModelResponse } from "./chatStreaming";
import { useChatConversationState } from "./useChatConversationState";
import { useChatPersistence } from "./useChatPersistence";
import { useChatRandomActions } from "./useChatRandom";
import { useConversationRunner } from "./useConversationRunner";
import { useMultimodalAttachments } from "./useMultimodalAttachments";

export function useChat() {
	const { data: enabledModels } = useEnabledModels();
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
	const abortRef = useRef<AbortController | null>(null);
	const sendingRef = useRef(false);
	/** Saves the conversation prompt before it's cleared, so it can be restored on error */
	const lastPromptRef = useRef<string>("");
	const messagesContainerRef = useRef<HTMLDivElement>(null);
	const { toast } = useToast();

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

	// Cleanup: abort streams on unmount
	// We store the abort controllers in separate cleanup refs so the React
	// Compiler doesn't mark conversationAbortRef as "effect-only" and forbid
	// mutation in event handlers - which is perfectly valid React.
	const cleanupAbortRef = useRef<AbortController | null>(null);
	const cleanupConvAbortRef = useRef<AbortController | null>(null);

	// Cleanup on unmount only: abort in-flight requests.
	useEffect(() => {
		const abortCtrl = cleanupAbortRef;
		const convAbortCtrl = cleanupConvAbortRef;
		return () => {
			abortCtrl.current?.abort();
			convAbortCtrl.current?.abort();
		};
	}, []);

	const selectedModelObj = enabledModels.find(
		(m) => proxyModelID(m.provider_name, m.model_id) === selectedModel,
	);
	const selectedModelObjB = enabledModels.find(
		(m) => proxyModelID(m.provider_name, m.model_id) === selectedModelB,
	);

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

	const scrollToBottom = useCallback((smooth = false) => {
		requestAnimationFrame(() => {
			const el = messagesContainerRef.current;
			if (!el) return;
			if (smooth) {
				el.scrollTo({ top: el.scrollHeight, behavior: "smooth" });
			} else {
				el.scrollTop = el.scrollHeight;
			}
		});
	}, []);

	// Scroll on mount
	useEffect(() => {
		scrollToBottom();
	}, [scrollToBottom]);

	// Scroll after layout settles (e.g. when message bubbles render)
	useEffect(() => {
		scrollToBottom();
		const timer = setTimeout(scrollToBottom, 320);
		return () => clearTimeout(timer);
	}, [scrollToBottom]);

	// Auto-scroll during streaming - keeps latest content visible
	// without being jarring if user is reading earlier messages.
	// Uses instant scroll (not smooth) because Firefox cancels in-progress
	// smooth scrolls when scrollTo is called again rapidly during streaming.
	const streamingContentLen = messages.reduce(
		(sum, m) => sum + m.content.length,
		0,
	);
	// biome-ignore lint/correctness/useExhaustiveDependencies: streamingContentLen triggers re-scroll on streaming updates
	useEffect(() => {
		if (!isStreaming) return;
		const el = messagesContainerRef.current;
		if (!el) return;
		// Only auto-scroll if user is already near the bottom (within 150px)
		const nearBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 150;
		if (nearBottom) {
			scrollToBottom(false);
		}
	}, [streamingContentLen, isStreaming, scrollToBottom]);

	// Shared streaming helper: creates abort controller, assistant placeholder,
	// streams the response, applies progressive + final updates.
	const streamAssistantReply = useCallback(
		async (
			model: string,
			chatMessages: Array<{ role: string; content: MessageContent }>,
		) => {
			const abortCtrl = new AbortController();
			abortRef.current = abortCtrl;
			cleanupAbortRef.current = abortCtrl;

			const createdAt = Date.now();
			const assistantMessage: ChatMessage = {
				role: "assistant",
				content: "",
				rawContent: "",
				thinkingContent: "",
				model,
				timestamp: createdAt,
				params: hasAnyParam(messageParams) ? messageParams : undefined,
			};
			setMessages((prev) => [...prev, assistantMessage]);

			const result = await streamModelResponse(
				model,
				chatMessages,
				messageParams,
				abortCtrl,
				(raw, content, thinking) => {
					setMessages((prev) => {
						const idx = prev.findIndex(
							(m) => m.timestamp === createdAt && m.role === "assistant",
						);
						if (idx === -1) return prev;
						const next = [...prev];
						next[idx] = {
							...next[idx],
							rawContent: raw,
							content,
							thinkingContent: thinking,
						};
						return next;
					});
				},
			);

			setMessages((prev) => {
				const idx = prev.findIndex(
					(m) => m.timestamp === createdAt && m.role === "assistant",
				);
				if (idx === -1) return prev;
				const next = [...prev];
				next[idx] = {
					...next[idx],
					rawContent: result.rawContent,
					content: result.content,
					thinkingContent: result.thinkingContent,
					error: result.error,
					metrics: {
						tokensPerSecond: result.tokensPerSecond,
						durationMs: result.durationMs,
						promptTokens: result.promptTokens,
						completionTokens: result.completionTokens,
					},
				};
				return next;
			});

			return result;
		},
		[messageParams],
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

	const handleSend = useCallback(async () => {
		const hasAttachment = pendingImage || pendingAudio;
		if ((!input.trim() && !hasAttachment) || !selectedModel || isStreaming)
			return;
		if (sendingRef.current) return;

		const userMessage: ChatMessage = {
			role: "user",
			content: input.trim(),
			timestamp: Date.now(),
			...(pendingImage ? { imageUrl: pendingImage.dataUrl } : {}),
			...(pendingAudio
				? {
						audioAttachment: {
							data: pendingAudio.dataUrl.split(",")[1] || pendingAudio.dataUrl,
							format: pendingAudio.format,
						},
					}
				: {}),
		};
		// Clear attachments
		setPendingImage(null);
		setPendingAudio(null);

		const updatedMessages = [...messages, userMessage];
		setMessages(updatedMessages);
		setInput("");
		setIsStreaming(true);
		sendingRef.current = true;

		const chatMessages = getApiMessagesForModel(
			updatedMessages,
			selectedModel,
			systemPrompt,
		);

		try {
			const result = await streamAssistantReply(selectedModel, chatMessages);

			if (result.error && !result.aborted) toast(result.error, "error");
		} catch (err) {
			if (err instanceof Error && err.name === "AbortError") {
				// User-initiated abort, no toast needed
			} else {
				const msg = err instanceof Error ? err.message : "Unknown error";
				toast(msg, "error");
			}
		} finally {
			setIsStreaming(false);
			abortRef.current = null;
			cleanupAbortRef.current = null;
			sendingRef.current = false;
		}
	}, [
		input,
		selectedModel,
		isStreaming,
		messages,
		systemPrompt,
		toast,
		streamAssistantReply,
		pendingImage,
		pendingAudio,
		setPendingImage,
		setPendingAudio,
	]);

	const handleStop = useCallback(() => {
		abortRef.current?.abort();
		abortRef.current = null;
		cleanupAbortRef.current = null;
		setIsStreaming(false);
	}, []);

	const handleRegenerate = useCallback(async () => {
		if (isStreaming) return;
		let lastUserIdx = -1;
		for (let i = messages.length - 1; i >= 0; i--) {
			if (messages[i].role === "user") {
				lastUserIdx = i;
				break;
			}
		}
		if (lastUserIdx === -1) return;
		const userContent = messages[lastUserIdx].content;
		const baseMessages = messages.slice(0, lastUserIdx);
		setMessages(baseMessages);
		setInput(userContent);

		const chatMessages: Array<{ role: string; content: string }> = [];
		if (systemPrompt.trim()) {
			chatMessages.push({
				role: "system",
				content: systemPrompt.trim(),
			});
		}
		for (const m of baseMessages) {
			chatMessages.push({ role: m.role, content: m.content });
		}
		chatMessages.push({ role: "user", content: userContent });

		const userMessage: ChatMessage = {
			role: "user",
			content: userContent,
			timestamp: Date.now(),
		};
		const updatedMessages = [...baseMessages, userMessage];
		setMessages(updatedMessages);
		setInput("");
		setIsStreaming(true);

		try {
			const result = await streamAssistantReply(
				selectedModel || "",
				chatMessages,
			);

			if (result.error && !result.aborted) toast(result.error, "error");
		} catch (err) {
			if (err instanceof Error && err.name === "AbortError") {
				// User-initiated abort, no toast needed
			} else {
				const msg = err instanceof Error ? err.message : "Unknown error";
				toast(msg, "error");
			}
		} finally {
			setIsStreaming(false);
			abortRef.current = null;
			cleanupAbortRef.current = null;
		}
	}, [
		isStreaming,
		messages,
		selectedModel,
		systemPrompt,
		toast,
		streamAssistantReply,
	]);

	// ── Extracted conversation runner hook ──
	const {
		runConversation,
		handleStopConversation,
		handleRetryConversation,
		clearConversationAbort,
	} = useConversationRunner({
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

	// Helper to delete a message
	const handleDeleteMessage = useCallback(
		(msgIndex: number) => {
			// Capture conversation state before the setMessages callback
			// so we can make decisions based on it inside the updater.
			const prevState = conversationState;

			setMessages((prev) => {
				const msg = prev[msgIndex];
				if (!msg) return prev;

				const toRemove = new Set<number>();

				if (chatSubMode === "chat") {
					// In chat mode, delete the assistant and preceding user message
					toRemove.add(msgIndex);
					if (msgIndex > 0 && prev[msgIndex - 1].role === "user") {
						toRemove.add(msgIndex - 1);
					}
				} else {
					// In conversation mode:
					// - If streaming, can only delete the last (currently generating) message
					// - If not streaming, can only delete the last pair
					const lastAssistantIdx = prev.findLastIndex(
						(m) => m.role === "assistant",
					);
					const isLastAssistant = msgIndex === lastAssistantIdx;
					const isStreamingLast = isStreaming && msgIndex === prev.length - 1;

					if (!isLastAssistant && !isStreamingLast) {
						// Can't delete - not the last message
						toast("Can only delete the most recent response", "error");
						return prev;
					}

					// Delete this assistant message and the preceding message (either user or other assistant)
					toRemove.add(msgIndex);
					if (msgIndex > 0) {
						toRemove.add(msgIndex - 1);
					}

					// After deletion, determine the correct conversation state
					const remaining = prev.filter((_, i) => !toRemove.has(i));

					if (remaining.length === 0) {
						// Deleted everything - back to idle, restore the prompt
						setConversationState("idle");
						setCurrentTurn(0);
						if (lastPromptRef.current) {
							setInput(lastPromptRef.current);
						}
						return [];
					}

					if (remaining.length === 1 && remaining[0]?.role === "user") {
						// Only the initial user prompt remains - back to idle
						setConversationState("idle");
						setCurrentTurn(0);
						setInput(remaining[0].content);
						return [];
					}

					// There are earlier successful turns remaining
					if (prevState === "error" || prevState === "completed") {
						// Transition to "paused" so the user can continue
						setConversationState("paused");
						// Adjust turn counter: count remaining assistant messages
						const remainingAssistantCount = remaining.filter(
							(m) => m.role === "assistant",
						).length;
						setCurrentTurn(remainingAssistantCount);
					}
				}

				return prev.filter((_, i) => !toRemove.has(i));
			});
			toast("Message deleted", "info");
		},
		[
			chatSubMode,
			toast,
			isStreaming,
			conversationState,
			setCurrentTurn, // Transition to "paused" so the user can continue
			setConversationState,
		],
	);

	const handleKeyDown = (e: React.KeyboardEvent) => {
		if (e.key === "Enter" && !e.shiftKey) {
			e.preventDefault();
			if (chatSubMode === "chat") {
				if (isStreaming) handleStop();
				else handleSend();
			}
			// In conversation mode, Enter doesn't auto-submit
		}
	};

	const totalTokens = messages.reduce(
		(acc, m) =>
			acc + (m.metrics?.promptTokens ?? 0) + (m.metrics?.completionTokens ?? 0),
		0,
	);
	const totalDuration = messages.reduce(
		(acc, m) => acc + (m.metrics?.durationMs ?? 0),
		0,
	);

	// Can start if: both models selected, has input, and not currently running
	const canStartConversation =
		chatSubMode === "conversation" &&
		!!selectedModel &&
		!!selectedModelB &&
		selectedModel !== selectedModelB &&
		!!input.trim() &&
		conversationState !== "running";

	const lastChatError = (() => {
		if (chatSubMode !== "chat") return null;
		for (let i = messages.length - 1; i >= 0; i--) {
			if (messages[i].role === "assistant" && messages[i].error) {
				const errModel = messages[i].model || "";
				// Only show the error if it's from the currently selected model.
				// After switching models the error is stale and misleading.
				if (errModel !== selectedModel) return null;
				return { error: messages[i].error, model: errModel };
			}
		}
		return null;
	})();

	const failedConversationModel = (() => {
		if (chatSubMode !== "conversation" || conversationState !== "error")
			return undefined;
		const lastErr = [...messages].reverse().find((m) => m.error);
		return lastErr?.model ? lastErr.model.split("/").pop() : undefined;
	})();

	const conversationDisabledReason = useMemo(() => {
		if (chatSubMode !== "conversation") return "";
		if (conversationState === "running") return "";
		if (!selectedModel) return "Select Model A";
		if (!selectedModelB) return "Select Model B";
		if (selectedModel === selectedModelB) return "Models must be different";
		if (!input.trim()) return "Enter a prompt";
		return "";
	}, [chatSubMode, selectedModel, selectedModelB, input, conversationState]);

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
		lastChatError,
		failedConversationModel,
		conversationDisabledReason,
		chatIcon,
		// Refs
		abortRef,
		sendingRef,
		lastPromptRef,
		messagesContainerRef,
		imageInputRef,
		audioInputRef,
		cleanupAbortRef,
		cleanupConvAbortRef,
		conversationAbortRef,
		conversationRunningRef,
		capturedModelARef,
		capturedModelBRef,
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
