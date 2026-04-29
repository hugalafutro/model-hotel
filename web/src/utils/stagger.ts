/**
 * Stagger utility for provider-aware request spacing and retry-with-backoff.
 *
 * When multiple LLM requests target the same provider, firing them all
 * simultaneously can trigger rate limits. This module provides:
 *
 * 1. `staggerByProvider` — groups items by provider and returns them with
 *    staggered delays so same-provider requests are spaced apart while
 *    different providers start immediately.
 *
 * 2. `fetchWithRetry` — wraps a fetch call with exponential backoff retry
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
	if (items.length === 0) return [];
	if (delayMs <= 0) return items.map((item) => ({ item, delayMs: 0 }));

	// Group indices by provider
	const providerGroups = new Map<string, number[]>();
	for (let i = 0; i < items.length; i++) {
		const provider = getProvider(items[i]);
		const group = providerGroups.get(provider);
		if (group) {
			group.push(i);
		} else {
			providerGroups.set(provider, [i]);
		}
	}

	// Assign delays: first item per provider = 0, second = delayMs, etc.
	const result: StaggeredItem<T>[] = items.map((item) => ({
		item,
		delayMs: 0,
	}));

	for (const indices of providerGroups.values()) {
		for (let slotIndex = 0; slotIndex < indices.length; slotIndex++) {
			const originalIndex = indices[slotIndex];
			result[originalIndex].delayMs = slotIndex * delayMs;
		}
	}

	return result;
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

	let lastError: Error | null = null;

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

			// Compute backoff delay
			let delayMs = Math.min(baseDelayMs * 2 ** attempt, maxDelayMs);

			// Respect Retry-After header for 429 responses
			if (response.status === 429) {
				const retryAfter = response.headers.get("Retry-After");
				if (retryAfter) {
					const retryAfterMs = parseFloat(retryAfter) * 1000;
					if (!Number.isNaN(retryAfterMs) && retryAfterMs > 0) {
						delayMs = Math.max(delayMs, retryAfterMs);
					}
				}
			}

			// Add jitter (±25%)
			const jitter = delayMs * 0.25 * (Math.random() * 2 - 1);
			const totalDelay = Math.max(0, Math.round(delayMs + jitter));

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

			lastError = err instanceof Error ? err : new Error(String(err));

			// Network errors are retryable, but only if we have attempts left
			if (attempt >= maxRetries) {
				throw lastError;
			}

			const delayMs = Math.min(baseDelayMs * 2 ** attempt, maxDelayMs);
			const jitter = delayMs * 0.25 * (Math.random() * 2 - 1);
			const totalDelay = Math.max(0, Math.round(delayMs + jitter));

			onRetry?.(attempt + 1, totalDelay, 0);

			await sleep(totalDelay);
		}
	}

	// Should be unreachable, but just in case
	throw lastError ?? new Error("All retries exhausted");
}

function sleep(ms: number): Promise<void> {
	return new Promise((resolve) => setTimeout(resolve, ms));
}
