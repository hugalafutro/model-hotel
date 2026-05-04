import {
	Bot,
	CircleStop,
	Eraser,
	Gauge,
	MessageSquare,
	MessagesSquare,
	RefreshCw,
	RotateCcw,
	Send,
	Settings,
	Timer,
	Trash2,
	Users,
	X,
} from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { API_BASE, getAuthHeaders } from "../api/client";
import type { ChatMessage, GenerationParams } from "../api/types";
import { ActionIconButton } from "../components/ActionIconButton";
import { CollapsibleToggle } from "../components/CollapsibleToggle";
import { ConfirmDialog } from "../components/ConfirmDialog";
import { ConversationConfig } from "../components/ConversationConfig";
import { CopyButton } from "../components/CopyButton";
import { MarkdownContent } from "../components/MarkdownContent";
import { ModelDetailPanel } from "../components/ModelDetailPanel";
import { ModelPicker } from "../components/ModelPicker";
import { ModelReplyCard } from "../components/ModelReplyCard";
import { PersonaPicker } from "../components/PersonaPicker";
import { SubModeToggle } from "../components/SubModeToggle";
import { useSidebarMode } from "../context/SidebarModeContext";
import { useStorage } from "../context/StorageContext";
import { useToast } from "../context/ToastContext";
import { CHAT_PERSONAS } from "../data/presets";
import { useLocalStorage } from "../hooks/useLocalStorage";
import { useEnabledModels } from "../hooks/useModels";
import { proxyModelID } from "../utils/model";
import { hasAnyParam } from "../utils/params";
import { readSSEStream, type StreamChunk } from "../utils/sse";
import { fetchWithRetry } from "../utils/stagger";
import { extractThinking, sanitizeDelta } from "../utils/thinking";

function formatTime(ts: number): string {
	const d = new Date(ts);
	return d.toLocaleTimeString(undefined, {
		hour: "2-digit",
		minute: "2-digit",
	});
}

type ConversationState = "idle" | "running" | "paused" | "completed" | "error";

function getApiMessagesForModel(
	allMessages: ChatMessage[],
	targetModelId: string,
	persona: string,
): Array<{ role: string; content: string }> {
	const apiMessages: Array<{ role: string; content: string }> = [];
	if (persona.trim()) {
		apiMessages.push({ role: "system", content: persona.trim() });
	}
	for (const msg of allMessages) {
		if (msg.role === "user") {
			apiMessages.push({
				role: "user",
				content: msg.content,
			});
		} else if (msg.role === "assistant") {
			if (msg.model === targetModelId) {
				apiMessages.push({
					role: "assistant",
					content: msg.content,
				});
			} else {
				apiMessages.push({
					role: "user",
					content: msg.content,
				});
			}
		}
	}
	return apiMessages;
}

interface StreamResult {
	rawContent: string;
	content: string;
	thinkingContent: string;
	error: string | null;
	durationMs: number;
	charsPerSecond: number | null;
	promptTokens: number;
	completionTokens: number;
}

async function streamModelResponse(
	modelId: string,
	apiMessages: Array<{ role: string; content: string }>,
	params: GenerationParams,
	abortCtrl: AbortController,
	onDelta: (raw: string, content: string, thinking: string) => void,
): Promise<StreamResult> {
	const startTime = performance.now();
	let charCount = 0;
	let promptTokens = 0;
	let completionTokens = 0;
	let rawContent = "";
	let content = "";
	let thinkingContent = "";

	try {
		const resp = await fetchWithRetry(
			`${API_BASE}/api/chat/chat`,
			{
				method: "POST",
				headers: getAuthHeaders(),
				body: JSON.stringify({
					model: modelId,
					stream: true,
					messages: apiMessages,
					...(hasAnyParam(params) ? params : {}),
				}),
				signal: abortCtrl.signal,
			},
			{
				maxRetries: 2,
			},
		);

		if (!resp.ok) {
			const text = await resp.text();
			throw new Error(`Chat failed: ${resp.status} ${text}`);
		}

		const reader = resp.body?.getReader();
		if (!reader) throw new Error("No readable stream");

		await readSSEStream<StreamChunk>({
			reader,
			signal: abortCtrl.signal,
			onChunk: (chunk) => {
				const delta = chunk.choices?.[0]?.delta?.content;
				if (delta) {
					const clean = sanitizeDelta(delta);
					charCount += clean.length;
					rawContent += clean;
					const extracted = extractThinking(rawContent);
					content = extracted.content;
					thinkingContent = extracted.thinking || thinkingContent;
					onDelta(rawContent, content, thinkingContent);
				}
				const thinkingDelta =
					chunk.choices?.[0]?.delta?.reasoning_content ??
					chunk.choices?.[0]?.delta?.reasoning;
				if (thinkingDelta) {
					thinkingContent += thinkingDelta;
					onDelta(rawContent, content, thinkingContent);
				}
				if (chunk.usage) {
					promptTokens = chunk.usage.prompt_tokens ?? 0;
					completionTokens = chunk.usage.completion_tokens ?? 0;
				}
			},
		});
	} catch (err) {
		const errorMsg = err instanceof Error ? err.message : "Unknown error";
		return {
			rawContent,
			content,
			thinkingContent,
			error: errorMsg,
			durationMs: Math.round(performance.now() - startTime),
			charsPerSecond:
				performance.now() - startTime > 0
					? charCount / ((performance.now() - startTime) / 1000)
					: null,
			promptTokens,
			completionTokens,
		};
	}

	const durationMs = performance.now() - startTime;
	const charsPerSecond =
		durationMs > 0 ? charCount / (durationMs / 1000) : null;

	return {
		rawContent,
		content,
		thinkingContent,
		error: null,
		durationMs: Math.round(durationMs),
		charsPerSecond,
		promptTokens,
		completionTokens,
	};
}

export function Chat() {
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
	const quotaWarnedRef = useRef(false);

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
	// mutation in event handlers — which is perfectly valid React.
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

	// ── Chat mode persistence effects ──
	useEffect(() => {
		if (!persistChat) return;
		try {
			localStorage.setItem("chatMessages", JSON.stringify(messages));
		} catch {
			/* quota exceeded */
			if (!quotaWarnedRef.current) {
				quotaWarnedRef.current = true;
				toast("Storage full — chat history not saved", "warning");
			}
		}
	}, [messages, persistChat, toast]);

	// ── Conversation messages persistence effect ──
	useEffect(() => {
		if (!persistConversation) return;
		if (chatSubMode !== "conversation") return;
		try {
			localStorage.setItem("conversationMessages", JSON.stringify(messages));
		} catch {
			/* quota exceeded */
			if (!quotaWarnedRef.current) {
				quotaWarnedRef.current = true;
				toast("Storage full — chat history not saved", "warning");
			}
		}
	}, [messages, persistConversation, chatSubMode, toast]);

	// Shared streaming helper: creates abort controller, assistant placeholder,
	// streams the response, applies progressive + final updates.
	const streamAssistantReply = useCallback(
		async (
			model: string,
			chatMessages: Array<{ role: string; content: string }>,
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
		if (!input.trim() || !selectedModel || isStreaming) return;
		if (sendingRef.current) return;

		const userMessage: ChatMessage = {
			role: "user",
			content: input.trim(),
			timestamp: Date.now(),
		};
		const updatedMessages = [...messages, userMessage];
		setMessages(updatedMessages);
		setInput("");
		setIsStreaming(true);
		sendingRef.current = true;

		const chatMessages: Array<{ role: string; content: string }> = [];
		if (systemPrompt.trim()) {
			chatMessages.push({
				role: "system",
				content: systemPrompt.trim(),
			});
		}
		for (const m of updatedMessages) {
			chatMessages.push({ role: m.role, content: m.content });
		}

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
			// First turn failed — the prompt is already restored in `input`.
			// Reset to idle so runConversation(false) runs as a fresh start.
			setConversationState("idle");
			setCurrentTurn(0);
			// Small delay to let state settle before re-triggering
			requestAnimationFrame(() => {
				runConversation(false);
			});
		} else {
			// Later turn failed — decrement turn counter to re-do the failed turn.
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
						// Deleted everything — back to idle, restore the prompt
						setConversationState("idle");
						setCurrentTurn(0);
						if (lastPromptRef.current) {
							setInput(lastPromptRef.current);
						}
						return [];
					}

					if (remaining.length === 1 && remaining[0]?.role === "user") {
						// Only the initial user prompt remains — back to idle
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

	const conversationDisabledReason = useMemo(() => {
		if (chatSubMode !== "conversation") return "";
		if (conversationState === "running") return "";
		if (!selectedModel) return "Select Model A";
		if (!selectedModelB) return "Select Model B";
		if (selectedModel === selectedModelB) return "Models must be different";
		if (!input.trim()) return "Enter a prompt";
		return "";
	}, [chatSubMode, selectedModel, selectedModelB, input, conversationState]);

	return (
		<div
			className={`flex flex-col gap-6 min-h-[calc(100vh-64px)] ${chatSubMode === "conversation" ? "" : "lg:h-[calc(100vh-64px)] lg:overflow-hidden"}`}
		>
			{/* Header */}
			<div className="flex justify-between items-center shrink-0">
				<div>
					<div className="flex items-center gap-3">
						{chatSubMode === "chat" ? (
							<MessageSquare
								size={28}
								strokeWidth={2}
								className="text-(--accent)"
							/>
						) : (
							<MessagesSquare
								size={28}
								strokeWidth={2}
								className="text-(--accent)"
							/>
						)}
						<h1 className="text-2xl font-bold text-(--text-primary)">
							{chatSubMode === "chat" ? "Chat" : "Conversation"}
						</h1>
					</div>
					<p className="text-gray-400">
						{chatSubMode === "chat"
							? "Test enabled models in temporary chat"
							: "Watch two models converse with each other"}
					</p>
				</div>
			</div>

			{/* Controls */}
			<div className="ui-card p-4 shrink-0">
				<div className="flex items-center justify-between">
					<div className="flex items-center gap-3">
						<span className="text-sm font-semibold text-(--text-primary)">
							Controls
						</span>
						<SubModeToggle
							options={[
								{
									value: "chat" as const,
									label: "Chat with AI",
									icon: MessageSquare,
								},
								{
									value: "conversation" as const,
									label: "AI Conversation",
									icon: Users,
								},
							]}
							value={chatSubMode}
							onChange={setChatSubMode}
						/>
					</div>
					<div className="flex items-center gap-1">
						{(messages.length > 0 ||
							(chatSubMode === "conversation" &&
								(conversationState === "completed" ||
									conversationState === "paused" ||
									conversationState === "error")) ||
							selectedModel ||
							(chatSubMode === "conversation" && selectedModelB) ||
							!!activePersonaId ||
							!!systemPrompt.trim() ||
							(chatSubMode === "conversation" &&
								(!!activePersonaIdB || !!systemPromptB.trim()))) && (
							<>
								{/* Light reset: clear messages/results only, keep model/persona/params */}
								{messages.length > 0 && (
									<ActionIconButton
										icon={Eraser}
										onClick={() => {
											if (chatSubMode === "conversation") {
												conversationAbortRef.current?.abort();
												conversationAbortRef.current = null;
												cleanupConvAbortRef.current = null;
												conversationRunningRef.current = false;
											}
											setMessages([]);
											setInput(lastPromptRef.current);
											setConversationState("idle");
											setCurrentTurn(0);
											setTurnCountdown(0);
											setIsStreaming(false);
											toast(
												chatSubMode === "chat"
													? "Chat cleared"
													: "Conversation cleared",
												"info",
											);
										}}
										title="Clear messages (keep model & settings)"
										color="amber"
										pulse={
											chatSubMode === "conversation" &&
											(conversationState === "completed" ||
												conversationState === "paused" ||
												conversationState === "error")
										}
									/>
								)}
								<ActionIconButton
									icon={RotateCcw}
									onClick={() => setPendingFullReset(true)}
									title="Reset all (clear model & settings)"
									color="red"
								/>
							</>
						)}
						<CollapsibleToggle
							collapsed={controlsCollapsed}
							onToggle={() => setControlsCollapsed((c) => !c)}
						/>
					</div>
				</div>
				<div
					className={`grid transition-[grid-template-rows] duration-300 ease-in-out ${
						controlsCollapsed ? "grid-rows-[0fr]" : "grid-rows-[1fr]"
					}`}
				>
					<div className="overflow-hidden">
						<div className="space-y-4 pt-4">
							{chatSubMode === "chat" ? (
								<>
									<ModelPicker
										models={enabledModels}
										selected={selectedModel}
										onChange={setSelectedModel}
										multi={false}
										onRandom={handleRandomModel}
									/>
									<PersonaPicker
										personas={CHAT_PERSONAS}
										activePersonaId={activePersonaId}
										systemPrompt={systemPrompt}
										onActivePersonaChange={setActivePersonaId}
										onSystemPromptChange={setSystemPrompt}
										onRandom={handleRandomPersona}
									/>
								</>
							) : (
								<div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
									<div>
										<label
											htmlFor="model-a-picker"
											className="text-sm font-semibold text-(--accent) mb-2 block"
										>
											Model A
										</label>
										<ModelPicker
											id="model-a-picker"
											models={enabledModels}
											selected={selectedModel}
											onChange={setSelectedModel}
											multi={false}
											onRandom={handleRandomModel}
											disabled={conversationState === "running"}
										/>
										<div className="mt-3">
											<PersonaPicker
												personas={CHAT_PERSONAS}
												activePersonaId={activePersonaId}
												systemPrompt={systemPrompt}
												onActivePersonaChange={setActivePersonaId}
												onSystemPromptChange={setSystemPrompt}
												onRandom={handleRandomPersona}
												label="Persona A"
												disabled={conversationState === "running"}
											/>
										</div>
									</div>
									<div>
										<label
											htmlFor="model-b-picker"
											className="text-sm font-semibold text-(--accent) mb-2 block"
										>
											Model B
										</label>
										<ModelPicker
											id="model-b-picker"
											models={enabledModels}
											selected={selectedModelB}
											onChange={setSelectedModelB}
											multi={false}
											onRandom={handleRandomModelB}
											disabled={conversationState === "running"}
										/>
										<div className="mt-3">
											<PersonaPicker
												personas={CHAT_PERSONAS}
												activePersonaId={activePersonaIdB}
												systemPrompt={systemPromptB}
												onActivePersonaChange={setActivePersonaIdB}
												onSystemPromptChange={setSystemPromptB}
												onRandom={handleRandomPersonaB}
												label="Persona B"
												disabled={conversationState === "running"}
											/>
										</div>
									</div>
								</div>
							)}
						</div>
					</div>
				</div>
			</div>

			{/* Conversation Config */}
			{chatSubMode === "conversation" && (
				<ConversationConfig
					maxTurns={maxTurns}
					onMaxTurnsChange={setMaxTurns}
					turnDelayMs={turnDelayMs}
					onTurnDelayMsChange={setTurnDelayMs}
					conversationState={conversationState}
					currentTurn={currentTurn}
					turnCountdown={turnCountdown}
					configCollapsed={configCollapsed}
					onToggleCollapsed={() => setConfigCollapsed((c) => !c)}
					input={input}
					onInputChange={setInput}
					onStart={() => runConversation(false)}
					onContinue={() => runConversation(true)}
					onRetry={handleRetryConversation}
					onStop={handleStopConversation}
					canStart={canStartConversation}
					disabledReason={conversationDisabledReason}
					selectedModel={selectedModel}
					selectedModelB={selectedModelB}
				/>
			)}

			{/* Chat Area: Model Details + Messages */}
			<div
				className={`flex gap-4 flex-1 ${chatSubMode === "conversation" ? "overflow-visible" : "min-h-0 lg:overflow-hidden"}`}
			>
				{/* Sidebar */}
				<div
					className={`shrink-0 flex flex-col ${
						chatSubMode === "conversation"
							? "w-1/3 gap-3 overflow-visible"
							: "min-h-0 lg:overflow-y-auto w-1/4"
					}`}
				>
					{chatSubMode === "chat" ? (
						selectedModelObj ? (
							<ModelDetailPanel
								model={selectedModelObj}
								params={messageParams}
								onParamsChange={setMessageParams}
								pulseBorder={
									isStreaming &&
									chatSubMode === "chat" &&
									messages.length > 0 &&
									messages[messages.length - 1].role === "assistant" &&
									messages[messages.length - 1].model === chatSelectedModel
								}
							/>
						) : (
							<div className="ui-card p-4 flex flex-col items-center justify-center text-(--text-tertiary) text-xs">
								<Bot size={32} strokeWidth={1} className="mb-2 opacity-40" />
								<p>Select a model</p>
							</div>
						)
					) : (
						<>
							{selectedModelObj ? (
								<ModelDetailPanel
									model={selectedModelObj}
									params={messageParams}
									onParamsChange={setMessageParams}
									collapsible
									tint="default"
									pulseBorder={
										isStreaming &&
										messages.length > 0 &&
										messages[messages.length - 1].role === "assistant" &&
										messages[messages.length - 1].model === selectedModel
									}
								/>
							) : (
								<div className="ui-card p-3 flex items-center justify-center text-(--text-tertiary) text-xs">
									<Bot size={20} className="mr-2 opacity-40" />
									Select Model A
								</div>
							)}
							{selectedModelObjB ? (
								<ModelDetailPanel
									model={selectedModelObjB}
									params={messageParamsB}
									onParamsChange={setMessageParamsB}
									collapsible
									tint="blue"
									pulseBorder={
										isStreaming &&
										messages.length > 0 &&
										messages[messages.length - 1].role === "assistant" &&
										messages[messages.length - 1].model === selectedModelB
									}
								/>
							) : (
								<div className="ui-card p-3 flex items-center justify-center text-(--text-tertiary) text-xs">
									<Bot size={20} className="mr-2 opacity-40" />
									Select Model B
								</div>
							)}
						</>
					)}
				</div>

				{/* Messages */}
				<div
					ref={messagesContainerRef}
					className={`flex-1 pr-1 space-y-4 ${
						chatSubMode === "conversation"
							? "overflow-visible"
							: "min-h-0 overflow-y-auto"
					}`}
				>
					{messages.length === 0 && (
						<div className="flex flex-col items-center justify-center py-20 text-(--text-tertiary)">
							{chatSubMode === "chat" ? (
								<Bot size={48} strokeWidth={1} className="mb-4 opacity-40" />
							) : (
								<div className="relative mb-4 w-20 h-12 flex items-center justify-center">
									<Bot
										size={48}
										strokeWidth={1}
										className="opacity-40 absolute left-0"
									/>
									<Bot
										size={48}
										strokeWidth={1}
										className="opacity-40 absolute right-0 scale-x-[-1]"
									/>
								</div>
							)}
							<p>
								{chatSubMode === "chat"
									? "Chat will appear here"
									: "Conversation will appear here"}
							</p>
						</div>
					)}

					{(() => {
						const lastAssistantIdx = messages.findLastIndex(
							(m) => m.role === "assistant",
						);
						return messages.map((msg, i) => {
							if (msg.role === "system") return null;
							const isUser = msg.role === "user";
							const isStreamingThis = isStreaming && i === messages.length - 1;
							const isModelB =
								msg.role === "assistant" && msg.model === selectedModelB;
							const isLastAssistant = i === lastAssistantIdx;
							// In conversation mode, only show delete on last assistant (or currently streaming)
							const canDelete =
								chatSubMode === "chat" ||
								(isLastAssistant && !isStreaming) ||
								(isStreamingThis && isLastAssistant);

							// Turn number: only in conversation mode — counts assistant messages up to and including this one
							const turnNumber =
								chatSubMode === "conversation" && msg.role === "assistant"
									? messages.filter(
											(m, mi) => m.role === "assistant" && mi <= i,
										).length
									: undefined;

							// Persona lookup for conversation mode
							const personaForModel = isModelB
								? CHAT_PERSONAS.find((p) => p.id === activePersonaIdB)
								: chatSubMode === "conversation"
									? CHAT_PERSONAS.find(
											(p) => p.id === conversationActivePersonaIdA,
										)
									: CHAT_PERSONAS.find((p) => p.id === chatActivePersonaId);
							const personaName =
								chatSubMode === "conversation" &&
								msg.role === "assistant" &&
								personaForModel
									? `${personaForModel.icon} ${personaForModel.label}`
									: chatSubMode === "chat" &&
											msg.role === "assistant" &&
											personaForModel
										? `${personaForModel.icon} ${personaForModel.label}`
										: undefined;
							const personaTooltip = personaForModel?.systemPrompt || undefined;

							/* ── User message ── */
							if (isUser) {
								// In conversation mode, user message is centered and gray
								const isConversationMode = chatSubMode === "conversation";
								return (
									<div
										key={`user-${msg.timestamp}`}
										className={`flex ${isConversationMode ? "justify-center" : "justify-end"}`}
									>
										<div
											className={`max-w-[80%] p-2.5 ${isConversationMode ? "bg-gray-500/20 text-(--text-primary) border border-gray-500/30" : "bg-(--accent) text-white"}`}
											style={{
												borderRadius: "var(--radius-card)",
											}}
										>
											<MarkdownContent
												className={`${isConversationMode ? "" : "[&_strong]:text-white [&_em]:text-white/80"}`}
											>
												{msg.content}
											</MarkdownContent>
											<div
												className={`flex items-center gap-3 text-[11px] mt-0.5 ${isConversationMode ? "text-(--text-secondary)" : "text-white/60"}`}
											>
												<span>{formatTime(msg.timestamp)}</span>
												<CopyButton
													text={msg.content}
													size={10}
													className={`inline-flex items-center cursor-pointer transition-all ${isConversationMode ? "text-(--text-secondary) hover:text-(--text-primary)" : "text-white hover:drop-shadow-[0_0_4px_white]"}`}
												/>
											</div>
										</div>
									</div>
								);
							}

							/* ── Model B message (conversation mode, right side) ── */
							if (chatSubMode === "conversation" && isModelB) {
								return (
									<div
										key={`modelb-${msg.timestamp}`}
										className="flex justify-end"
									>
										<div className="max-w-[80%]">
											<ModelReplyCard
												model={msg.model || ""}
												content={msg.content}
												thinkingContent={msg.thinkingContent}
												error={msg.error}
												metrics={msg.metrics}
												isStreaming={isStreamingThis}
												shortenModelName={false}
												tint="blue"
												personaName={personaName}
												personaTooltip={personaTooltip}
												turnNumber={turnNumber}
												headerEnd={
													isStreamingThis ? (
														<button
															type="button"
															onClick={handleStopConversation}
															className="text-red-400/60 hover:text-red-400 transition-colors cursor-pointer ml-1"
															title="Cancel"
														>
															<CircleStop size={14} />
														</button>
													) : null
												}
												footerStart={<span>{formatTime(msg.timestamp)}</span>}
												footerEnd={
													<div className="flex items-center gap-2">
														<CopyButton text={msg.content} size={10} />
														{canDelete && (
															<button
																type="button"
																className="inline-flex items-center cursor-pointer hover:drop-shadow-[0_0_4px_var(--color-red-500,red)] text-red-500 transition-all"
																onClick={() => handleDeleteMessage(i)}
																title="Delete message"
															>
																<Trash2 size={10} />
															</button>
														)}
													</div>
												}
												className="rounded-xl rounded-br-sm p-4"
												headerClassName="mb-2"
												footerClassName="mt-2"
											/>
										</div>
									</div>
								);
							}

							/* ── Assistant message (Model A or chat mode) ── */
							return (
								<div
									key={`assistant-${msg.timestamp}`}
									className="flex justify-start"
								>
									<div className="max-w-[80%]">
										<ModelReplyCard
											model={msg.model || ""}
											content={msg.content}
											thinkingContent={msg.thinkingContent}
											error={msg.error}
											metrics={msg.metrics}
											isStreaming={isStreamingThis}
											shortenModelName={false}
											personaName={personaName}
											personaTooltip={personaTooltip}
											turnNumber={turnNumber}
											headerEnd={
												isStreamingThis ? (
													<button
														type="button"
														onClick={
															chatSubMode === "conversation"
																? handleStopConversation
																: handleStop
														}
														className="text-red-400/60 hover:text-red-400 transition-colors cursor-pointer ml-1"
														title="Cancel"
													>
														<CircleStop size={14} />
													</button>
												) : (
													i === lastAssistantIdx &&
													chatSubMode === "chat" && (
														<button
															type="button"
															onClick={handleRegenerate}
															className="text-(--text-tertiary) hover:text-(--accent) hover:drop-shadow-[0_0_6px_var(--accent)] transition-all cursor-pointer ml-1"
															title="Regenerate"
														>
															<RefreshCw size={14} />
														</button>
													)
												)
											}
											footerStart={<span>{formatTime(msg.timestamp)}</span>}
											footerEnd={
												<div className="flex items-center gap-2">
													<CopyButton text={msg.content} size={10} />
													{canDelete && (
														<button
															type="button"
															className="inline-flex items-center cursor-pointer hover:drop-shadow-[0_0_4px_var(--color-red-500,red)] text-red-500 transition-all"
															onClick={() => handleDeleteMessage(i)}
															title="Delete message"
														>
															<Trash2 size={10} />
														</button>
													)}
													{msg.params && (
														<span
															className="inline-flex items-center text-(--accent) cursor-pointer hover:drop-shadow-[0_0_4px_var(--accent)] transition-all"
															title={`Settings: ${Object.entries(msg.params)
																.filter(([, v]) => v !== undefined)
																.map(([k, v]) => `${k.replace(/_/g, " ")}=${v}`)
																.join(", ")}`}
														>
															<Settings size={10} />
														</span>
													)}
												</div>
											}
											className="rounded-xl rounded-bl-sm p-4"
											headerClassName="mb-2"
											footerClassName="mt-2"
										/>
									</div>
								</div>
							);
						});
					})()}
				</div>
			</div>

			{/* Input / Stats Area — chat mode input bar + conversation stats when active */}
			{chatSubMode === "chat" && (
				<div className="ui-card p-4 shrink-0">
					<div className="space-y-2">
						<div className="flex items-center gap-3">
							<textarea
								value={input}
								onChange={(e) => {
									setInput(e.target.value);
									e.target.style.height = "auto";
									const el = e.target;
									requestAnimationFrame(() => {
										el.style.height = `${el.scrollHeight}px`;
									});
								}}
								onKeyDown={handleKeyDown}
								placeholder={
									selectedModel ? "Type a message…" : "Select a model first"
								}
								disabled={!selectedModel || isStreaming}
								rows={1}
								maxLength={32000}
								className="flex-1 ui-input resize-none max-h-32 min-h-11 overflow-y-auto"
								style={{ height: "auto" }}
							/>
							<button
								type="button"
								onClick={isStreaming ? handleStop : handleSend}
								disabled={!selectedModel}
								className={`ui-btn flex items-center gap-2 shrink-0 ${
									isStreaming ? "ui-btn-danger" : "ui-btn-primary"
								}`}
							>
								{isStreaming ? (
									<>
										<X size={16} />
										Stop
									</>
								) : (
									<>
										<Send size={16} />
										Send
									</>
								)}
							</button>
						</div>
						{!selectedModel && !isStreaming ? (
							<p className="text-xs text-amber-400">Select a model first</p>
						) : (
							<p className="text-xs text-(--text-muted)">
								Press Enter to send, Shift+Enter for newline
							</p>
						)}
					</div>
				</div>
			)}
			{chatSubMode === "conversation" &&
				(conversationState === "running" ||
					conversationState === "paused" ||
					conversationState === "completed" ||
					conversationState === "error") && (
					<div className="ui-card p-4 shrink-0">
						<div className="space-y-3">
							<div className="flex items-center justify-between flex-wrap gap-2">
								<div className="flex items-center gap-4 text-sm text-(--text-secondary)">
									<span className="flex items-center gap-1.5">
										<Gauge size={14} />
										Turn {Math.ceil(currentTurn / 2)} / {maxTurns}
									</span>
									<span className="flex items-center gap-1.5">
										<Timer size={14} />
										{(totalDuration / 1000).toFixed(1)}s
									</span>
									<span className="flex items-center gap-1.5">
										<Bot size={14} />
										{totalTokens} tokens
									</span>
								</div>
								<div className="flex items-center gap-2">
									{messages.length > 0 && (
										<ActionIconButton
											icon={Eraser}
											onClick={() => {
												conversationAbortRef.current?.abort();
												conversationAbortRef.current = null;
												cleanupConvAbortRef.current = null;
												conversationRunningRef.current = false;
												setMessages([]);
												setInput(lastPromptRef.current);
												setConversationState("idle");
												setCurrentTurn(0);
												setTurnCountdown(0);
												setIsStreaming(false);
												toast("Conversation cleared", "info");
											}}
											title="Clear"
											color="amber"
											size={16}
											label="Clear"
											withLabel
										/>
									)}
									<ActionIconButton
										icon={RotateCcw}
										onClick={() => setPendingFullReset(true)}
										title="Reset All"
										color="red"
										size={16}
										label="Reset All"
										withLabel
									/>
								</div>
							</div>
							{conversationState === "running" && (
								<div className="flex items-center gap-2 text-xs text-(--text-muted)">
									<span className="w-1.5 h-1.5 rounded-full bg-(--accent) animate-pulse" />
									{isStreaming
										? "Model is generating…"
										: "Waiting for next turn…"}
								</div>
							)}
							{conversationState === "error" && (
								<div className="flex items-center gap-2 text-xs text-red-400">
									<span className="w-1.5 h-1.5 rounded-full bg-red-400 shrink-0" />
									Generation failed — use Retry in config above, or Clear/Reset
									below
								</div>
							)}
						</div>
					</div>
				)}

			{pendingFullReset && (
				<ConfirmDialog
					title={chatSubMode === "chat" ? "Reset Chat" : "Reset Conversation"}
					message={
						chatSubMode === "chat"
							? "This will clear all messages, reset model selection, persona, and parameters. Continue?"
							: "This will clear the conversation and reset both models, personas, and parameters. Continue?"
					}
					fields={[]}
					confirmLabel="Reset All"
					onConfirm={() => {
						// Abort any running conversation
						conversationAbortRef.current?.abort();
						conversationAbortRef.current = null;
						cleanupConvAbortRef.current = null;
						conversationRunningRef.current = false;
						setMessages([]);
						setInput("");
						setConversationState("idle");
						setCurrentTurn(0);
						setTurnCountdown(0);
						setIsStreaming(false);
						if (chatSubMode === "chat") {
							setChatSelectedModel("");
							setChatSystemPrompt("");
							setChatActivePersonaId(null);
							setChatMessageParams({});
						} else {
							// conversation mode: also clear both models, personas, and params
							setConversationModelA("");
							setSelectedModelB("");
							setConversationSystemPromptA("");
							setSystemPromptB("");
							setConversationActivePersonaIdA(null);
							setActivePersonaIdB(null);
							setConversationParamsA({});
							setMessageParamsB({});
						}
						setPendingFullReset(false);
						toast(
							chatSubMode === "chat" ? "Chat reset" : "Conversation reset",
							"info",
						);
					}}
					onCancel={() => setPendingFullReset(false)}
				/>
			)}
		</div>
	);
}
