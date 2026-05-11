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
 * - Per-read idle timeout (prevents hanging on stalled upstream connections)
 * - Silent JSON parse errors (matching existing behavior)
 */
/** Whether the stream ended via a `[DONE]` sentinel, was aborted, or ended without one. */
export type StreamCompletion = {
	/** True if the stream ended with a `[DONE]` sentinel (normal completion). */
	sawDone: boolean;
	/** True if the stream was aborted via AbortSignal. */
	aborted: boolean;
	/** True if the stream was aborted because no data arrived within the idle timeout. */
	idleTimeout?: boolean;
};

/** Default idle timeout for stream reads (90 seconds). */
const DEFAULT_IDLE_TIMEOUT_MS = 90_000;

/** Unique sentinel to distinguish idle timeout from genuine stream end. */
const IDLE_TIMEOUT = Symbol("idle-timeout");

export async function readSSEStream<T = unknown>(opts: {
	reader: ReadableStreamDefaultReader<Uint8Array>;
	signal?: AbortSignal;
	onChunk: (parsed: T) => void;
	/** Set to null to skip sentinel check. Defaults to "[DONE]". */
	doneSentinel?: string | null;
	/**
	 * Maximum time (ms) to wait for a single `reader.read()` call before
	 * considering the stream stalled. Resets on each successful read.
	 * Defaults to 90 000 (90 seconds). Set to 0 or Infinity to disable.
	 */
	idleTimeoutMs?: number;
}): Promise<StreamCompletion> {
	const {
		reader,
		signal,
		onChunk,
		doneSentinel = "[DONE]",
		idleTimeoutMs = DEFAULT_IDLE_TIMEOUT_MS,
	} = opts;
	const decoder = new TextDecoder();
	let buffer = "";
	let sawDone = false;
	let idleTimedOut = false;

	while (true) {
		let readResult: Awaited<
			ReturnType<ReadableStreamDefaultReader<Uint8Array>["read"]>
		>;
		if (idleTimeoutMs > 0 && idleTimeoutMs < Infinity) {
			// Race the read against an idle timeout so stalled streams don't hang forever.
			let idleTimerId: ReturnType<typeof setTimeout> | undefined;
			const idlePromise = new Promise<typeof IDLE_TIMEOUT>((resolve) => {
				idleTimerId = setTimeout(() => resolve(IDLE_TIMEOUT), idleTimeoutMs);
			});
			const result = await Promise.race([reader.read(), idlePromise]);
			if (idleTimerId !== undefined) clearTimeout(idleTimerId);
			if (result === IDLE_TIMEOUT) {
				idleTimedOut = true;
				reader.cancel();
				break;
			}
			readResult = result as Awaited<
				ReturnType<ReadableStreamDefaultReader<Uint8Array>["read"]>
			>;
		} else {
			readResult = await reader.read();
		}
		const { done, value } = readResult;
		if (signal?.aborted) break;
		if (done) break;

		buffer += decoder.decode(value, { stream: true });
		const lines = buffer.split("\n");
		buffer = lines.pop() || "";

		let streamDone = false;
		for (const line of lines) {
			// Match "data: " (standard SSE) or "data:" (LM Studio, some proxies).
			// Strip leading whitespace after the colon for both forms.
			let data: string | null = null;
			if (line.startsWith("data: ")) {
				data = line.slice(6);
			} else if (line.startsWith("data:") && line.length > 5) {
				data = line.slice(5).replace(/^[\t ]/, "");
			}
			if (data === null) continue;
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
		if (streamDone) {
			sawDone = !signal?.aborted;
			break;
		}
	}

	return {
		sawDone,
		aborted: !!signal?.aborted,
		idleTimeout: idleTimedOut || undefined,
	};
}
