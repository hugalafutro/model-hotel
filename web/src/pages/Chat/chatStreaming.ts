import { API_BASE, getAuthHeaders } from "../../api/client";
import type {
	ChatMessage,
	ContentPart,
	GenerationParams,
	MessageContent,
} from "../../api/types";
import { hasAnyParam } from "../../utils/params";
import { readSSEStream, type StreamChunk } from "../../utils/sse";
import { fetchWithRetry } from "../../utils/stagger";
import { extractThinking, sanitizeDelta } from "../../utils/thinking";

export function formatTime(ts: number): string {
	const d = new Date(ts);
	return d.toLocaleTimeString(undefined, {
		hour: "2-digit",
		minute: "2-digit",
	});
}

export type ConversationState =
	| "idle"
	| "running"
	| "paused"
	| "completed"
	| "error";

/**
 * Build the messages array for the API. When a message has attachments
 * (image or audio), produces an OpenAI-compatible content-parts array;
 * otherwise uses a plain string for backward compatibility.
 */
export function buildMessageContent(msg: ChatMessage): MessageContent {
	if (msg.imageUrl || msg.audioAttachment) {
		const parts: ContentPart[] = [];
		if (msg.imageUrl) {
			parts.push({ type: "image_url", image_url: { url: msg.imageUrl } });
		}
		if (msg.audioAttachment) {
			parts.push({
				type: "input_audio",
				input_audio: {
					data: msg.audioAttachment.data,
					format: msg.audioAttachment.format,
				},
			});
		}
		// Always include the text part last (most providers expect text after media)
		if (msg.content) {
			parts.push({ type: "text", text: msg.content });
		}
		return parts;
	}
	return msg.content;
}

export function getApiMessagesForModel(
	allMessages: ChatMessage[],
	targetModelId: string,
	persona: string,
): Array<{ role: string; content: MessageContent }> {
	const apiMessages: Array<{ role: string; content: MessageContent }> = [];
	if (persona.trim()) {
		apiMessages.push({ role: "system", content: persona.trim() });
	}
	for (const msg of allMessages) {
		if (msg.role === "user") {
			apiMessages.push({
				role: "user",
				content: buildMessageContent(msg),
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

export interface StreamResult {
	rawContent: string;
	content: string;
	thinkingContent: string;
	error: string | null;
	durationMs: number;
	charsPerSecond: number | null;
	promptTokens: number;
	completionTokens: number;
}

export async function streamModelResponse(
	modelId: string,
	apiMessages: Array<{ role: string; content: MessageContent }>,
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

		const completion = await readSSEStream<StreamChunk>({
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
		if (!completion.sawDone && !completion.aborted) {
			const durationMs = Math.round(performance.now() - startTime);
			const charsPerSecond =
				durationMs > 0 ? charCount / (durationMs / 1000) : null;
			return {
				rawContent,
				content,
				thinkingContent,
				error: completion.idleTimeout
					? "Stream stalled - no data received within the timeout period."
					: content
						? "Stream was cut off - the response may be incomplete."
						: "Stream ended unexpectedly with no content.",
				durationMs,
				charsPerSecond,
				promptTokens,
				completionTokens,
			};
		}
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
