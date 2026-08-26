import type { ChatMessage } from "../../api/types";

/**
 * The last failed assistant reply in chat mode, but only if it came from the
 * currently selected model: after switching models the error is stale and
 * misleading. Aborted replies are not errors.
 */
export function lastChatError(
	messages: ChatMessage[],
	chatSubMode: string,
	selectedModel: string,
): { error: ChatMessage["error"]; model: string } | null {
	if (chatSubMode !== "chat") return null;
	for (let i = messages.length - 1; i >= 0; i--) {
		if (
			messages[i].role === "assistant" &&
			messages[i].error &&
			!messages[i].aborted
		) {
			const errModel = messages[i].model || "";
			if (errModel !== selectedModel) return null;
			return { error: messages[i].error, model: errModel };
		}
	}
	return null;
}

/** The short name of the model whose reply put a conversation into `error`. */
export function failedConversationModel(
	messages: ChatMessage[],
	chatSubMode: string,
	conversationState: string,
): string | undefined {
	if (chatSubMode !== "conversation" || conversationState !== "error")
		return undefined;
	const lastErr = [...messages].reverse().find((m) => m.error);
	return lastErr?.model ? lastErr.model.split("/").pop() : undefined;
}

/** Token and wall-clock totals across every reply that reported metrics. */
export function messageTotals(messages: ChatMessage[]): {
	totalTokens: number;
	totalDuration: number;
} {
	return {
		totalTokens: messages.reduce(
			(acc, m) =>
				acc +
				(m.metrics?.promptTokens ?? 0) +
				(m.metrics?.completionTokens ?? 0),
			0,
		),
		totalDuration: messages.reduce(
			(acc, m) => acc + (m.metrics?.durationMs ?? 0),
			0,
		),
	};
}
