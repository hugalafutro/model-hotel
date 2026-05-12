import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { fetchWithRetry, staggerByProvider } from "../stagger";

describe("staggerByProvider", () => {
	it("returns empty array for empty input", () => {
		const result = staggerByProvider([], (item) => item);
		expect(result).toEqual([]);
	});

	it("returns single item with zero delay", () => {
		const items = [{ provider: "OpenAI", model: "gpt-4" }];
		const result = staggerByProvider(items, (i) => i.provider, 300);
		expect(result).toHaveLength(1);
		expect(result[0].item).toBe(items[0]);
		expect(result[0].delayMs).toBe(0);
	});

	it("groups items by provider with correct delays", () => {
		const items = [
			{ provider: "OpenAI", model: "gpt-4" },
			{ provider: "OpenAI", model: "gpt-4-mini" },
			{ provider: "Anthropic", model: "claude-3" },
			{ provider: "OpenAI", model: "o1" },
		];

		const result = staggerByProvider(items, (i) => i.provider, 300);

		// First OpenAI item: delay 0
		expect(result[0].delayMs).toBe(0);
		// Anthropic item: delay 0 (first in its group)
		expect(result[2].delayMs).toBe(0);
		// Second OpenAI item: delay 300
		expect(result[1].delayMs).toBe(300);
		// Third OpenAI item: delay 600
		expect(result[3].delayMs).toBe(600);
	});

	it("handles mixed providers correctly", () => {
		const items = [
			{ provider: "A", id: 1 },
			{ provider: "B", id: 2 },
			{ provider: "A", id: 3 },
			{ provider: "B", id: 4 },
			{ provider: "A", id: 5 },
		];

		const result = staggerByProvider(items, (i) => i.provider, 100);

		// A group: 0, 100, 200
		expect(result[0].delayMs).toBe(0);
		expect(result[2].delayMs).toBe(100);
		expect(result[4].delayMs).toBe(200);

		// B group: 0, 100
		expect(result[1].delayMs).toBe(0);
		expect(result[3].delayMs).toBe(100);
	});

	it("respects custom delayMs parameter", () => {
		const items = [
			{ provider: "OpenAI", model: "gpt-4" },
			{ provider: "OpenAI", model: "gpt-4-mini" },
		];

		const result = staggerByProvider(items, (i) => i.provider, 500);
		expect(result[0].delayMs).toBe(0);
		expect(result[1].delayMs).toBe(500);
	});

	it("returns zero delays when delayMs is 0 or negative", () => {
		const items = [
			{ provider: "OpenAI", model: "gpt-4" },
			{ provider: "OpenAI", model: "gpt-4-mini" },
		];

		const resultZero = staggerByProvider(items, (i) => i.provider, 0);
		expect(resultZero[0].delayMs).toBe(0);
		expect(resultZero[1].delayMs).toBe(0);

		const resultNegative = staggerByProvider(items, (i) => i.provider, -100);
		expect(resultNegative[0].delayMs).toBe(0);
		expect(resultNegative[1].delayMs).toBe(0);
	});

	it("handles all items from same provider", () => {
		const items = [
			{ provider: "OpenAI", model: "gpt-4" },
			{ provider: "OpenAI", model: "gpt-4-mini" },
			{ provider: "OpenAI", model: "o1" },
		];

		const result = staggerByProvider(items, (i) => i.provider, 200);
		expect(result[0].delayMs).toBe(0);
		expect(result[1].delayMs).toBe(200);
		expect(result[2].delayMs).toBe(400);
	});

	it("handles all items from different providers", () => {
		const items = [
			{ provider: "OpenAI", model: "gpt-4" },
			{ provider: "Anthropic", model: "claude-3" },
			{ provider: "Google", model: "gemini" },
		];

		const result = staggerByProvider(items, (i) => i.provider, 300);
		expect(result[0].delayMs).toBe(0);
		expect(result[1].delayMs).toBe(0);
		expect(result[2].delayMs).toBe(0);
	});
});

describe("fetchWithRetry", () => {
	const originalFetch = global.fetch;

	beforeEach(() => {
		global.fetch = vi.fn();
		vi.useFakeTimers();
	});

	afterEach(() => {
		vi.useRealTimers();
		global.fetch = originalFetch;
	});

	it("succeeds on first try", async () => {
		const mockResponse = new Response("OK", { status: 200 });
		vi.mocked(global.fetch).mockResolvedValue(mockResponse);

		const result = await fetchWithRetry("https://api.example.com", {}, {});
		expect(result.status).toBe(200);
		expect(global.fetch).toHaveBeenCalledTimes(1);
	});

	it("retries on 429 status", async () => {
		const errorResponse = new Response("Rate Limited", { status: 429 });
		const successResponse = new Response("OK", { status: 200 });

		vi.mocked(global.fetch)
			.mockResolvedValueOnce(errorResponse)
			.mockResolvedValueOnce(successResponse);

		const fetchPromise = fetchWithRetry(
			"https://api.example.com",
			{},
			{
				maxRetries: 2,
				baseDelayMs: 100,
			},
		);

		// Advance timers to allow retry
		await vi.advanceTimersByTimeAsync(200);

		const result = await fetchPromise;
		expect(result.status).toBe(200);
		expect(global.fetch).toHaveBeenCalledTimes(2);
	});

	it("retries on 502 status", async () => {
		const errorResponse = new Response("Bad Gateway", { status: 502 });
		const successResponse = new Response("OK", { status: 200 });

		vi.mocked(global.fetch)
			.mockResolvedValueOnce(errorResponse)
			.mockResolvedValueOnce(successResponse);

		const fetchPromise = fetchWithRetry(
			"https://api.example.com",
			{},
			{
				maxRetries: 2,
				baseDelayMs: 100,
			},
		);

		await vi.advanceTimersByTimeAsync(200);

		const result = await fetchPromise;
		expect(result.status).toBe(200);
		expect(global.fetch).toHaveBeenCalledTimes(2);
	});

	it("retries on 503 status", async () => {
		const errorResponse = new Response("Service Unavailable", { status: 503 });
		const successResponse = new Response("OK", { status: 200 });

		vi.mocked(global.fetch)
			.mockResolvedValueOnce(errorResponse)
			.mockResolvedValueOnce(successResponse);

		const fetchPromise = fetchWithRetry(
			"https://api.example.com",
			{},
			{
				maxRetries: 2,
				baseDelayMs: 100,
			},
		);

		await vi.advanceTimersByTimeAsync(200);

		const result = await fetchPromise;
		expect(result.status).toBe(200);
		expect(global.fetch).toHaveBeenCalledTimes(2);
	});

	it("retries on 504 status", async () => {
		const errorResponse = new Response("Gateway Timeout", { status: 504 });
		const successResponse = new Response("OK", { status: 200 });

		vi.mocked(global.fetch)
			.mockResolvedValueOnce(errorResponse)
			.mockResolvedValueOnce(successResponse);

		const fetchPromise = fetchWithRetry(
			"https://api.example.com",
			{},
			{
				maxRetries: 2,
				baseDelayMs: 100,
			},
		);

		await vi.advanceTimersByTimeAsync(200);

		const result = await fetchPromise;
		expect(result.status).toBe(200);
		expect(global.fetch).toHaveBeenCalledTimes(2);
	});

	it("gives up after max retries", async () => {
		const errorResponse = new Response("Rate Limited", { status: 429 });

		vi.mocked(global.fetch).mockResolvedValue(errorResponse);

		const fetchPromise = fetchWithRetry(
			"https://api.example.com",
			{},
			{
				maxRetries: 2,
				baseDelayMs: 100,
			},
		);

		// Advance timers for all retries (initial + 2 retries with backoff)
		await vi.advanceTimersByTimeAsync(10000);

		const result = await fetchPromise;

		// Should return the error response after exhausting retries
		expect(result.status).toBe(429);
		expect(global.fetch).toHaveBeenCalledTimes(3); // initial + 2 retries
	}, 15000);

	it("respects Retry-After header", async () => {
		const errorResponse = new Response("Rate Limited", {
			status: 429,
			headers: { "Retry-After": "1" }, // 1 second
		});
		const successResponse = new Response("OK", { status: 200 });

		vi.mocked(global.fetch)
			.mockResolvedValueOnce(errorResponse)
			.mockResolvedValueOnce(successResponse);

		const fetchPromise = fetchWithRetry(
			"https://api.example.com",
			{},
			{
				maxRetries: 2,
				baseDelayMs: 100,
			},
		);

		// Should wait at least 1000ms due to Retry-After header
		await vi.advanceTimersByTimeAsync(1500);

		const result = await fetchPromise;
		expect(result.status).toBe(200);
		expect(global.fetch).toHaveBeenCalledTimes(2);
	});

	it("does not retry on non-retryable status codes", async () => {
		const errorResponse = new Response("Not Found", { status: 404 });

		vi.mocked(global.fetch).mockResolvedValue(errorResponse);

		const result = await fetchWithRetry("https://api.example.com", {}, {});
		expect(result.status).toBe(404);
		expect(global.fetch).toHaveBeenCalledTimes(1);
	});

	it("retries on network error", async () => {
		vi.mocked(global.fetch)
			.mockRejectedValueOnce(new Error("Network error"))
			.mockResolvedValueOnce(new Response("OK", { status: 200 }));

		const fetchPromise = fetchWithRetry(
			"https://api.example.com",
			{},
			{
				maxRetries: 2,
				baseDelayMs: 100,
			},
		);

		await vi.advanceTimersByTimeAsync(200);

		const result = await fetchPromise;
		expect(result.status).toBe(200);
		expect(global.fetch).toHaveBeenCalledTimes(2);
	});

	it("does not retry on AbortError", async () => {
		const abortError = new DOMException("Aborted", "AbortError");
		vi.mocked(global.fetch).mockRejectedValue(abortError);

		await expect(
			fetchWithRetry("https://api.example.com", {}, { maxRetries: 2 }),
		).rejects.toThrow("Aborted");

		expect(global.fetch).toHaveBeenCalledTimes(1);
	});

	it("calls onRetry callback before each retry", async () => {
		const onRetry = vi.fn();
		const errorResponse = new Response("Rate Limited", { status: 429 });
		const successResponse = new Response("OK", { status: 200 });

		vi.mocked(global.fetch)
			.mockResolvedValueOnce(errorResponse)
			.mockResolvedValueOnce(successResponse);

		const fetchPromise = fetchWithRetry(
			"https://api.example.com",
			{},
			{
				maxRetries: 2,
				baseDelayMs: 100,
				onRetry,
			},
		);

		await vi.advanceTimersByTimeAsync(200);
		await fetchPromise;

		expect(onRetry).toHaveBeenCalledTimes(1);
		expect(onRetry).toHaveBeenCalledWith(
			expect.any(Number),
			expect.any(Number),
			429,
		);
	});
});
