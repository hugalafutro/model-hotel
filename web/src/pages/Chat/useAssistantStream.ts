import type { TFunction } from "i18next";
import {
	type Dispatch,
	type SetStateAction,
	useCallback,
	useEffect,
	useRef,
} from "react";
import type {
	ChatMessage,
	GenerationParams,
	MessageContent,
} from "../../api/types";
import type { useToast } from "../../context/ToastContext";
import { hasAnyParam } from "../../utils/params";
import { getApiMessagesForModel, streamModelResponse } from "./chatStreaming";
import type { useMultimodalAttachments } from "./useMultimodalAttachments";

type Attachments = ReturnType<typeof useMultimodalAttachments>;

/**
 * Chat mode's single-model streaming: the shared assistant-reply streamer and
 * the send / stop / regenerate handlers built on it. Owns the abort controller
 * for the in-flight request and aborts it on unmount.
 */
export function useAssistantStream({
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
}: {
	messages: ChatMessage[];
	setMessages: Dispatch<SetStateAction<ChatMessage[]>>;
	input: string;
	setInput: (v: string) => void;
	selectedModel: string;
	systemPrompt: string;
	messageParams: GenerationParams;
	/** False while the chat model list is doing its first load. */
	modelsReady: boolean;
	isStreaming: boolean;
	setIsStreaming: (v: boolean) => void;
	pendingImage: Attachments["pendingImage"];
	setPendingImage: Attachments["setPendingImage"];
	pendingAudio: Attachments["pendingAudio"];
	setPendingAudio: Attachments["setPendingAudio"];
	toast: ReturnType<typeof useToast>["toast"];
	t: TFunction;
}) {
	const abortRef = useRef<AbortController | null>(null);
	const sendingRef = useRef(false);
	// A separate cleanup ref so the React Compiler doesn't mark abortRef as
	// "effect-only" and forbid mutation in event handlers - which is perfectly
	// valid React.
	const cleanupAbortRef = useRef<AbortController | null>(null);

	// Cleanup on unmount only: abort the in-flight request.
	useEffect(() => {
		const abortCtrl = cleanupAbortRef;
		return () => {
			abortCtrl.current?.abort();
		};
	}, []);

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
				undefined,
				t,
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
					aborted: result.aborted || undefined,
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
		[messageParams, setMessages, t],
	);

	const handleSend = useCallback(async () => {
		const hasAttachment = pendingImage || pendingAudio;
		// Wait for the model list to settle so a persisted stale selection is
		// reconciled away before it could be sent to a non-chat endpoint.
		if (
			!modelsReady ||
			(!input.trim() && !hasAttachment) ||
			!selectedModel ||
			isStreaming
		)
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
		modelsReady,
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
		setMessages,
		setInput,
		setIsStreaming,
	]);

	const handleStop = useCallback(() => {
		abortRef.current?.abort();
		abortRef.current = null;
		cleanupAbortRef.current = null;
		setIsStreaming(false);
	}, [setIsStreaming]);

	const handleRegenerate = useCallback(async () => {
		if (isStreaming) return;
		// Wait for the model list to settle so a persisted stale selection is
		// reconciled away first; same guard as handleSend: without a selected model
		// regenerate would stream with an empty model id.
		if (!modelsReady || !selectedModel) return;
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
		modelsReady,
		messages,
		selectedModel,
		systemPrompt,
		toast,
		streamAssistantReply,
		setMessages,
		setInput,
		setIsStreaming,
	]);

	return {
		sendingRef,
		streamAssistantReply,
		handleSend,
		handleStop,
		handleRegenerate,
	};
}
