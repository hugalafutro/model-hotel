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
import { CHAT_PERSONAS } from "../../data/presets";
import { useLocalStorage } from "../../hooks/useLocalStorage";
import { useEnabledModels } from "../../hooks/useModels";
import { parseCapabilities, proxyModelID } from "../../utils/model";
import { hasAnyParam } from "../../utils/params";
import {
	type ConversationState,
	getApiMessagesForModel,
	streamModelResponse,
} from "./chatStreaming";
import { useChatPersistence } from "./useChatPersistence";

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

	// ── Conversation mode state (Model A) ──
	const [conversationModelA, setConversationModelA] = useLocalStorage<string>(
		"conversationModelA",
		"",
		{ enabled: persistConversation },
	);
	const [conversationSystemPromptA, setConversationSystemPromptA] =
		useLocalStorage<string>("conversationSystemPromptA", "", {
			enabled: persistConversation,
		});
	const [conversationActivePersonaIdA, setConversationActivePersonaIdA] =
		useLocalStorage<string | null>("conversationActivePersonaIdA", null, {
			enabled: persistConversation,
			serialize: (v) => v ?? "",
			deserialize: (v) => v || null,
		});
	const [conversationParamsA, setConversationParamsA] =
		useState<GenerationParams>({});

	// ── Conversation mode state (Model B) ──
	const [selectedModelB, setSelectedModelB] = useLocalStorage<string>(
		"conversationModelB",
		"",
		{ enabled: persistConversation },
	);
	const [systemPromptB, setSystemPromptB] = useLocalStorage<string>(
		"conversationSystemPromptB",
		"",
		{ enabled: persistConversation },
	);
	const [activePersonaIdB, setActivePersonaIdB] = useLocalStorage<
		string | null
	>("conversationActivePersonaIdB", null, {
		enabled: persistConversation,
		serialize: (v) => v ?? "",
		deserialize: (v) => v || null,
	});
	const [messageParamsB, setMessageParamsB] = useLocalStorage<GenerationParams>(
		"conversationParamsB",
		{},
		{
			enabled: persistConversation,
			serialize: JSON.stringify,
			deserialize: (v) => JSON.parse(v),
		},
	);
	const [conversationState, setConversationState] =
		useState<ConversationState>("idle");
	const [currentTurn, setCurrentTurn] = useState(0);
	const [turnCountdown, setTurnCountdown] = useState(0);

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

	// ── Multimodal attachment state (chat mode only) ──
	const [pendingImage, setPendingImage] = useState<{
		dataUrl: string;
		name: string;
	} | null>(null);
	const [pendingAudio, setPendingAudio] = useState<{
		dataUrl: string;
		name: string;
		format: string;
	} | null>(null);
	const imageInputRef = useRef<HTMLInputElement>(null);
	const audioInputRef = useRef<HTMLInputElement>(null);

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

	const handleRandomPersona = useCallback(() => {
		const currentId =
			chatSubMode === "chat"
				? chatActivePersonaId
				: conversationActivePersonaIdA;
		const available = CHAT_PERSONAS.filter((p) => p.id !== currentId);
		if (available.length === 0) return;
		const pick = available[Math.floor(Math.random() * available.length)];
		setActivePersonaId(pick.id);
		setSystemPrompt(pick.systemPrompt);
	}, [
		chatSubMode,
		chatActivePersonaId,
		conversationActivePersonaIdA,
		setActivePersonaId,
		setSystemPrompt,
	]);

	const handleRandomPersonaB = useCallback(() => {
		const available = CHAT_PERSONAS.filter((p) => p.id !== activePersonaIdB);
		if (available.length === 0) return;
		const pick = available[Math.floor(Math.random() * available.length)];
		setActivePersonaIdB(pick.id);
		setSystemPromptB(pick.systemPrompt);
	}, [activePersonaIdB, setActivePersonaIdB, setSystemPromptB]);

	const handleRandomModel = useCallback(() => {
		const available = enabledModels.filter((m) => {
			const val = proxyModelID(m.provider_name, m.model_id);
			return val !== selectedModel;
		});
		if (available.length === 0) return;
		const pick = available[Math.floor(Math.random() * available.length)];
		const val = proxyModelID(pick.provider_name, pick.model_id);
		setSelectedModel(val);
	}, [enabledModels, selectedModel, setSelectedModel]);

	const handleRandomModelB = useCallback(() => {
		const available = enabledModels.filter((m) => {
			const val = proxyModelID(m.provider_name, m.model_id);
			return val !== selectedModelB;
		});
		if (available.length === 0) return;
		const pick = available[Math.floor(Math.random() * available.length)];
		const val = proxyModelID(pick.provider_name, pick.model_id);
		setSelectedModelB(val);
	}, [enabledModels, selectedModelB, setSelectedModelB]);

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
	}, [chatSubMode]);

	// Cleanup: abort streams on unmount
	// We store the abort controllers in separate cleanup refs so the React
	// Compiler doesn't mark conversationAbortRef as "effect-only" and forbid
	// mutation in event handlers - which is perfectly valid React.
	const cleanupAbortRef = useRef<AbortController | null>(null);
	const cleanupConvAbortRef = useRef<AbortController | null>(null);

	useEffect(() => {
		return () => {
			cleanupAbortRef.current?.abort();
			cleanupConvAbortRef.current?.abort();
		};
	}, []);

	const [maxTurns, setMaxTurns] = useLocalStorage<number>(
		"conversationMaxTurns",
		10,
		{ serialize: String, deserialize: (v) => parseInt(v, 10) || 10 },
	);
	const [turnDelayMs, setTurnDelayMs] = useLocalStorage<number>(
		"conversationTurnDelayMs",
		500,
		{ serialize: String, deserialize: (v) => parseInt(v, 10) || 500 },
	);
	const [configCollapsed, setConfigCollapsed] = useState(false);
	const conversationAbortRef = useRef<AbortController | null>(null);
	const conversationRunningRef = useRef(false);
	const capturedModelARef = useRef<string>("");
	const capturedModelBRef = useRef<string>("");

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

	const scrollToBottom = useCallback(() => {
		requestAnimationFrame(() => {
			const el = messagesContainerRef.current;
			if (el) el.scrollTop = el.scrollHeight;
		});
	}, []);

	useEffect(() => {
		scrollToBottom();
	}, [scrollToBottom]);

	useEffect(() => {
		scrollToBottom();
		const timer = setTimeout(scrollToBottom, 320);
		return () => clearTimeout(timer);
	}, [scrollToBottom]);

	// Shared streaming helper: creates abort controller, assistant placeholder,
	// streams the response, applies progressive + final updates.
	const streamAssistantReply = useCallback(
		async (
			model: string,
			chatMessages: Array<{ role: string; content: MessageContent }>,
			messageIndex: number,
		) => {
			const abortCtrl = new AbortController();
			abortRef.current = abortCtrl;
			cleanupAbortRef.current = abortCtrl;

			const assistantMessage: ChatMessage = {
				role: "assistant",
				content: "",
				rawContent: "",
				thinkingContent: "",
				model,
				timestamp: Date.now(),
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
						if (prev.length <= messageIndex) return prev;
						const next = [...prev];
						next[messageIndex] = {
							...next[messageIndex],
							rawContent: raw,
							content,
							thinkingContent: thinking,
						};
						return next;
					});
				},
			);

			setMessages((prev) => {
				if (prev.length <= messageIndex) return prev;
				const next = [...prev];
				next[messageIndex] = {
					...next[messageIndex],
					rawContent: result.rawContent,
					content: result.content,
					thinkingContent: result.thinkingContent,
					error: result.error,
					metrics: {
						charsPerSecond: result.charsPerSecond,
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
			const result = await streamAssistantReply(
				selectedModel,
				chatMessages,
				updatedMessages.length,
			);

			if (result.error) toast(result.error, "error");
		} catch (err) {
			const msg = err instanceof Error ? err.message : "Unknown error";
			toast(msg, "error");
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
	]);

	// ── Multimodal attachment handlers ──
	const handleImageSelect = useCallback(
		(e: React.ChangeEvent<HTMLInputElement>) => {
			const file = e.target.files?.[0];
			if (!file) return;
			if (file.size > 20 * 1024 * 1024) {
				toast("Image must be under 20 MB", "error");
				return;
			}
			const reader = new FileReader();
			reader.onload = () => {
				setPendingImage({ dataUrl: reader.result as string, name: file.name });
				setPendingAudio(null); // only one attachment at a time
			};
			reader.readAsDataURL(file);
			// Reset so the same file can be re-selected
			e.target.value = "";
		},
		[toast],
	);

	const handleAudioSelect = useCallback(
		(e: React.ChangeEvent<HTMLInputElement>) => {
			const file = e.target.files?.[0];
			if (!file) return;
			if (file.size > 25 * 1024 * 1024) {
				toast("Audio must be under 25 MB", "error");
				return;
			}
			const ext = file.name.split(".").pop()?.toLowerCase() || "mp3";
			const formatMap: Record<string, string> = {
				mp3: "mp3",
				wav: "wav",
				ogg: "ogg",
				m4a: "m4a",
				flac: "flac",
				webm: "webm",
			};
			const format = formatMap[ext] || ext;
			const reader = new FileReader();
			reader.onload = () => {
				setPendingAudio({
					dataUrl: reader.result as string,
					name: file.name,
					format,
				});
				setPendingImage(null); // only one attachment at a time
			};
			reader.readAsDataURL(file);
			e.target.value = "";
		},
		[toast],
	);

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
				updatedMessages.length,
			);

			if (result.error) toast(result.error, "error");
		} catch (err) {
			const msg = err instanceof Error ? err.message : "Unknown error";
			toast(msg, "error");
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

	// ── Unified conversation orchestration ──
	const runConversation = useCallback(
		async (resume = false) => {
			if (conversationRunningRef.current) return;

			const canStart =
				selectedModel &&
				selectedModelB &&
				(resume || input.trim()) &&
				conversationState !== "running";

			if (!canStart) return;

			conversationRunningRef.current = true;

			const abortCtrl = new AbortController();
			conversationAbortRef.current = abortCtrl;
			cleanupConvAbortRef.current = abortCtrl;
			setConversationState("running");
			setIsStreaming(true);

			let currentMessages = messages;
			let turn = currentTurn;
			let modelTurn: "A" | "B";

			if (!resume) {
				capturedModelARef.current = selectedModel;
				capturedModelBRef.current = selectedModelB;
				setCurrentTurn(0);
				turn = 0;
				lastPromptRef.current = input.trim();
				const userMessage: ChatMessage = {
					role: "user",
					content: input.trim(),
					timestamp: Date.now(),
				};
				currentMessages = [...messages, userMessage];
				setMessages(currentMessages);
				setInput("");
				modelTurn = "A";
			} else {
				// Resume: figure out whose turn it is based on last assistant
				const lastAssistantIdx = currentMessages.findLastIndex(
					(m) => m.role === "assistant",
				);
				modelTurn =
					lastAssistantIdx >= 0 &&
					currentMessages[lastAssistantIdx].model === capturedModelARef.current
						? "B"
						: "A";
			}

			// maxTurns = number of conversation rounds; each round involves
			// 2 model responses (Model A then Model B), so the loop runs
			// maxTurns * 2 iterations total.
			while (turn < maxTurns * 2 && !abortCtrl.signal.aborted) {
				const isModelA = modelTurn === "A";
				const modelId = isModelA
					? capturedModelARef.current
					: capturedModelBRef.current;
				const persona = isModelA ? systemPrompt : systemPromptB;
				const params = isModelA ? messageParams : messageParamsB;

				const apiMessages = getApiMessagesForModel(
					currentMessages,
					modelId,
					persona,
				);

				const assistantMessage: ChatMessage = {
					role: "assistant",
					content: "",
					rawContent: "",
					thinkingContent: "",
					model: modelId,
					timestamp: Date.now(),
					params: hasAnyParam(params) ? params : undefined,
				};
				currentMessages = [...currentMessages, assistantMessage];
				setMessages(currentMessages);
				const messageIndex = currentMessages.length - 1;

				const result = await streamModelResponse(
					modelId,
					apiMessages,
					params,
					abortCtrl,
					(raw, content, thinking) => {
						setMessages((prev) => {
							if (prev.length <= messageIndex) return prev;
							const next = [...prev];
							next[messageIndex] = {
								...next[messageIndex],
								rawContent: raw,
								content,
								thinkingContent: thinking,
							};
							return next;
						});
					},
				);

				setMessages((prev) => {
					if (prev.length <= messageIndex) return prev;
					const next = [...prev];
					next[messageIndex] = {
						...next[messageIndex],
						rawContent: result.rawContent,
						content: result.content,
						thinkingContent: result.thinkingContent,
						error: result.error,
						metrics: {
							charsPerSecond: result.charsPerSecond,
							durationMs: result.durationMs,
							promptTokens: result.promptTokens,
							completionTokens: result.completionTokens,
						},
					};
					return next;
				});

				currentMessages = currentMessages.map((m, i) =>
					i === messageIndex
						? {
								...m,
								rawContent: result.rawContent,
								content: result.content,
								thinkingContent: result.thinkingContent,
								error: result.error,
								metrics: {
									charsPerSecond: result.charsPerSecond,
									durationMs: result.durationMs,
									promptTokens: result.promptTokens,
									completionTokens: result.completionTokens,
								},
							}
						: m,
				);

				if (result.error) {
					toast(`${modelId}: ${result.error}`, "error");
					// Transition to error state so user can retry
					// If this was the first turn, restore the prompt
					setConversationState("error");
					if (turn === 0 && lastPromptRef.current) {
						setInput(lastPromptRef.current);
					}
					setIsStreaming(false);
					setTurnCountdown(0);
					conversationAbortRef.current = null;
					cleanupConvAbortRef.current = null;
					conversationRunningRef.current = false;
					return;
				}

				turn++;
				modelTurn = modelTurn === "A" ? "B" : "A";
				setCurrentTurn(turn);

				// Same maxTurns * 2 semantics as the loop condition above.
				if (turn < maxTurns * 2 && !abortCtrl.signal.aborted) {
					const countdownSeconds = Math.ceil(turnDelayMs / 1000);
					setTurnCountdown(countdownSeconds);
					await new Promise<void>((resolve) => {
						let remaining = countdownSeconds;
						const interval = setInterval(() => {
							remaining--;
							if (remaining <= 0) {
								clearInterval(interval);
								setTurnCountdown(0);
								resolve();
							} else {
								setTurnCountdown(remaining);
							}
						}, 1000);
						// Resolve immediately on abort so the loop can exit cleanly
						abortCtrl.signal.addEventListener(
							"abort",
							() => {
								clearInterval(interval);
								setTurnCountdown(0);
								resolve();
							},
							{ once: true },
						);
					});
				}
			}

			setTurnCountdown(0);
			setIsStreaming(false);
			setConversationState("completed");
			conversationAbortRef.current = null;
			cleanupConvAbortRef.current = null;
			conversationRunningRef.current = false;
		},
		[
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
			toast,
			conversationState,
		],
	);

	const handleStopConversation = useCallback(() => {
		conversationAbortRef.current?.abort();
		conversationAbortRef.current = null;
		cleanupConvAbortRef.current = null;
		setTurnCountdown(0);
		setIsStreaming(false);
		setConversationState("paused");
		conversationRunningRef.current = false;
	}, []);

	/** Retry from error state: remove the failed assistant message and
	 *  re-run the conversation from the last successful turn.
	 *  If the first turn failed (currentTurn === 0), the user's prompt
	 *  has already been restored to `input` by the error handler. */
	const handleRetryConversation = useCallback(() => {
		if (conversationState !== "error") return;

		// Remove the last assistant message (the one that errored)
		const lastAssistantIdx = messages.findLastIndex(
			(m) => m.role === "assistant",
		);

		if (lastAssistantIdx >= 0) {
			setMessages((prev) => {
				const next = [...prev];
				next.splice(lastAssistantIdx, 1);
				return next;
			});
		}

		if (currentTurn === 0) {
			// First turn failed - the prompt is already restored in `input`.
			// Reset to idle so runConversation(false) runs as a fresh start.
			setConversationState("idle");
			setCurrentTurn(0);
			// Small delay to let state settle before re-triggering
			requestAnimationFrame(() => {
				runConversation(false);
			});
		} else {
			// Later turn failed - decrement turn counter to re-do the failed turn.
			// The prompt was not lost (it was never in `input` for later turns).
			const newTurn = currentTurn > 0 ? currentTurn - 1 : 0;
			setCurrentTurn(newTurn);
			setConversationState("paused");
			// Resume from the last successful turn
			requestAnimationFrame(() => {
				runConversation(true);
			});
		}
	}, [conversationState, messages, currentTurn, runConversation]);

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
		[chatSubMode, toast, isStreaming, conversationState],
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
				return { error: messages[i].error, model: messages[i].model || "" };
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

	// Mutation helpers for refs (to satisfy ESLint immutability rules)
	const clearConversationAbort = useCallback(() => {
		conversationAbortRef.current?.abort();
		conversationAbortRef.current = null;
		cleanupConvAbortRef.current = null;
		conversationRunningRef.current = false;
	}, []);

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
