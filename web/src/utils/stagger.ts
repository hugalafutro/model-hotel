import { asError } from "./errors";

/**
 * Stagger utility for provider-aware request spacing and retry-with-backoff.
 *
 * When multiple LLM requests target the same provider, firing them all
 * simultaneously can trigger rate limits. This module provides:
 *
 * 1. `staggerByProvider` - groups items by provider and returns them with
 *    staggered delays so same-provider requests are spaced apart while
 *    different providers start immediately.
 *
 * 2. `fetchWithRetry` - wraps a fetch call with exponential backoff retry
 *    logic for transient failures (429, 502, 503, 504).
 */

// ────────────────────────────────────────────────────────────────────────────
// Provider-aware staggering
// ────────────────────────────────────────────────────────────────────────────

export interface StaggeredItem<T> {
	/** The original item. */
	item: T;
	/** Milliseconds to wait before starting this item's request. */
	delayMs: number;
}

/**
 * Arrange items so that requests to the same provider are spaced apart
 * by `delayMs`, while requests to different providers start immediately.
 *
 * @example
 * ```ts
 * const items = [
 *   { model: "OpenAI/gpt-4o", provider: "OpenAI" },
 *   { model: "OpenAI/gpt-4o-mini", provider: "OpenAI" },
 *   { model: "Anthropic/claude-3-opus", provider: "Anthropic" },
 *   { model: "OpenAI/o1", provider: "OpenAI" },
 * ];
 *
 * const staggered = staggerByProvider(items, (i) => i.provider, 300);
 * // Result:
 * //  { item: "OpenAI/gpt-4o",           delayMs: 0   }
 * //  { item: "Anthropic/claude-3-opus",  delayMs: 0   }
 * //  { item: "OpenAI/gpt-4o-mini",       delayMs: 300 }
 * //  { item: "OpenAI/o1",                delayMs: 600 }
 * ```
 */
export function staggerByProvider<T>(
	items: T[],
	getProvider: (item: T) => string,
	delayMs: number = 300,
): StaggeredItem<T>[] {
	// One pass with a per-provider counter: the nth item of a provider waits n
	// slots, and a provider seen for the first time starts immediately.
	const seen = new Map<string, number>();
	return items.map((item) => {
		const provider = getProvider(item);
		const slot = seen.get(provider) ?? 0;
		seen.set(provider, slot + 1);
		return { item, delayMs: Math.max(0, slot * delayMs) };
	});
}

/**
 * Exponential backoff with +/-25% jitter, capped at `maxDelayMs` and floored at
 * `floorMs` (a Retry-After the server asked for). Never negative.
 */
function backoff(
	attempt: number,
	baseDelayMs: number,
	maxDelayMs: number,
	floorMs = 0,
): number {
	const delayMs = Math.max(
		Math.min(baseDelayMs * 2 ** attempt, maxDelayMs),
		floorMs,
	);
	const jitter = delayMs * 0.25 * (Math.random() * 2 - 1);
	return Math.max(0, Math.round(delayMs + jitter));
}

// ────────────────────────────────────────────────────────────────────────────
// Retry-able fetch with exponential backoff
// ────────────────────────────────────────────────────────────────────────────

/** HTTP status codes that are retry-able. */
const RETRYABLE_STATUS_CODES = new Set([429, 502, 503, 504]);

export interface RetryOptions {
	/** Maximum number of retry attempts (not counting the initial request). Default: 2 */
	maxRetries?: number;
	/** Base delay in ms for exponential backoff. Default: 1000 */
	baseDelayMs?: number;
	/** Maximum delay in ms for any single backoff. Default: 10000 */
	maxDelayMs?: number;
	/** Called before each retry with the attempt number (1-based) and delay in ms. */
	onRetry?: (attempt: number, delayMs: number, status: number) => void;
}

/**
 * Perform a fetch with automatic retry on transient errors (429, 502, 503, 504).
 * Uses exponential backoff with jitter: `baseDelay * 2^attempt + random jitter`.
 *
 * For 429 specifically, if the response includes a `Retry-After` header
 * (in seconds), that value is used as the minimum delay.
 *
 * @returns The successful `Response`, or throws the last error after all retries exhausted.
 */
export async function fetchWithRetry(
	url: string,
	init: RequestInit,
	options: RetryOptions = {},
): Promise<Response> {
	const {
		maxRetries = 2,
		baseDelayMs = 1000,
		maxDelayMs = 10000,
		onRetry,
	} = options;

	for (let attempt = 0; attempt <= maxRetries; attempt++) {
		try {
			const response = await fetch(url, init);

			if (!RETRYABLE_STATUS_CODES.has(response.status)) {
				return response;
			}

			// If we've exhausted retries, return the response as-is
			// so the caller can handle the error status.
			if (attempt >= maxRetries) {
				return response;
			}

			// Respect Retry-After header for 429 responses
			let floorMs = 0;
			if (response.status === 429) {
				const retryAfter = response.headers.get("Retry-After");
				if (retryAfter) {
					const retryAfterMs = Number.parseFloat(retryAfter) * 1000;
					if (!Number.isNaN(retryAfterMs) && retryAfterMs > 0) {
						floorMs = retryAfterMs;
					}
				}
			}

			const totalDelay = backoff(attempt, baseDelayMs, maxDelayMs, floorMs);

			onRetry?.(attempt + 1, totalDelay, response.status);

			// Drain the response body to avoid leaking the connection
			try {
				await response.text();
			} catch {
				// Ignore body drain errors
			}

			await sleep(totalDelay);
		} catch (err) {
			// Network-level errors (AbortError should NOT be retried)
			if (err instanceof DOMException && err.name === "AbortError") {
				throw err;
			}

			// Network errors are retryable, but only if we have attempts left
			if (attempt >= maxRetries) {
				throw asError(err);
			}

			const totalDelay = backoff(attempt, baseDelayMs, maxDelayMs);

			onRetry?.(attempt + 1, totalDelay, 0);

			await sleep(totalDelay);
		}
	}

	// Every iteration returns or throws, so the loop cannot fall through; this
	// statement is what tells the type checker so.
	throw new Error("All retries exhausted");
}

function sleep(ms: number): Promise<void> {
	return new Promise((resolve) => setTimeout(resolve, ms));
}
