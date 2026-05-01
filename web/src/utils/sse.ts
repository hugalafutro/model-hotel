/**
 * OpenAI-compatible streaming chunk shape.
 * Used by chat/arena SSE endpoints that return choices with deltas.
 */
export interface StreamChunk {
	choices?: Array<{
		delta?: {
			content?: string;
			reasoning_content?: string;
			reasoning?: string;
		};
	}>;
	usage?: {
		prompt_tokens?: number;
		completion_tokens?: number;
	};
}

/**
 * Read an SSE stream from a ReadableStream, buffering partial chunks,
 * splitting lines, stripping the `data: ` prefix, and JSON-parsing each
 * payload before passing it to `onChunk`.
 *
 * Handles:
 * - Partial line buffering across chunk boundaries
 * - `[DONE]` sentinel detection (configurable)
 * - AbortSignal support
 * - Silent JSON parse errors (matching existing behavior)
 */
export async function readSSEStream<T = unknown>(opts: {
	reader: ReadableStreamDefaultReader<Uint8Array>;
	signal?: AbortSignal;
	onChunk: (parsed: T) => void;
	/** Set to null to skip sentinel check. Defaults to "[DONE]". */
	doneSentinel?: string | null;
}): Promise<void> {
	const { reader, signal, onChunk, doneSentinel = "[DONE]" } = opts;
	const decoder = new TextDecoder();
	let buffer = "";

	while (true) {
		const { done, value } = await reader.read();
		if (done || signal?.aborted) break;

		buffer += decoder.decode(value, { stream: true });
		const lines = buffer.split("\n");
		buffer = lines.pop() || "";

		let streamDone = false;
		for (const line of lines) {
			if (!line.startsWith("data: ")) continue;
			const data = line.slice(6);
			if (doneSentinel !== null && data === doneSentinel) {
				streamDone = true;
				break;
			}
			try {
				onChunk(JSON.parse(data));
			} catch {
				// ignore malformed JSON
			}
		}
		if (streamDone) break;
	}
}
