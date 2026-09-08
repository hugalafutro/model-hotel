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
import { errorMessage } from "../../utils/errors";
import {
	getApiMessagesForModel,
	newAssistantPlaceholder,
	patchAssistantAt,
	streamModelResponse,
	withStreamResult,
} from "./chatStreaming";
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

			const assistantMessage = newAssistantPlaceholder(model, messageParams);
			const createdAt = assistantMessage.timestamp;
			setMessages((prev) => [...prev, assistantMessage]);

			const result = await streamModelResponse(
				model,
				chatMessages,
				messageParams,
				abortCtrl,
				(raw, content, thinking) => {
					setMessages((prev) =>
						patchAssistantAt(prev, createdAt, {
							rawContent: raw,
							content,
							thinkingContent: thinking,
						}),
					);
				},
				t,
			);

			setMessages((prev) =>
				patchAssistantAt(prev, createdAt, (m) => withStreamResult(m, result)),
			);

			return result;
		},
		[messageParams, setMessages, t],
	);

	/**
	 * Streams one reply and reports its outcome: an error the stream captured
	 * is toasted, a user abort is not, and the streaming flag and abort refs
	 * are cleared however it ends.
	 */
	const runReply = useCallback(
		async (
			model: string,
			chatMessages: Array<{ role: string; content: MessageContent }>,
		) => {
			try {
				const result = await streamAssistantReply(model, chatMessages);
				if (result.error && !result.aborted) toast(result.error, "error");
			} catch (err) {
				if (!(err instanceof Error && err.name === "AbortError")) {
					toast(errorMessage(err, t("common.unknownError")), "error");
				}
			} finally {
				setIsStreaming(false);
				abortRef.current = null;
				cleanupAbortRef.current = null;
			}
		},
		[streamAssistantReply, toast, setIsStreaming, t],
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
			await runReply(selectedModel, chatMessages);
		} finally {
			sendingRef.current = false;
		}
	}, [
		input,
		modelsReady,
		selectedModel,
		isStreaming,
		messages,
		systemPrompt,
		runReply,
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
		const lastUserIdx = messages.findLastIndex((m) => m.role === "user");
		if (lastUserIdx === -1) return;
		const baseMessages = messages.slice(0, lastUserIdx);
		// The original message object is reused so its attachments are re-sent:
		// rebuilding it from the text alone drops the image or audio the reply
		// was about.
		const userMessage: ChatMessage = {
			...messages[lastUserIdx],
			timestamp: Date.now(),
		};
		const updatedMessages = [...baseMessages, userMessage];
		setMessages(updatedMessages);
		setInput("");
		setIsStreaming(true);

		const chatMessages = getApiMessagesForModel(
			updatedMessages,
			selectedModel,
			systemPrompt,
		);

		await runReply(selectedModel, chatMessages);
	}, [
		isStreaming,
		modelsReady,
		messages,
		selectedModel,
		systemPrompt,
		runReply,
		setMessages,
		setInput,
		setIsStreaming,
	]);

	return {
		handleSend,
		handleStop,
		handleRegenerate,
	};
}
