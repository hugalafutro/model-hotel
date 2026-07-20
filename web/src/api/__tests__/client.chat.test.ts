import { beforeEach, describe, expect, it, vi } from "vitest";
import { api } from "../client";

describe("api.chat", () => {
	beforeEach(() => {
		document.cookie = "mh_csrf=test-csrf; path=/";
		vi.restoreAllMocks();
	});

	const chatBody = {
		model: "gpt-4",
		stream: false,
		messages: [{ role: "user", content: "Hello" }],
		temperature: 0.7,
	};

	describe("completions", () => {
		it("sends chat completions request", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify({ choices: [] }), { status: 200 }),
			);

			const result = await api.chat.completions(chatBody);

			expect(result).toBeInstanceOf(Response);
			expect(result.ok).toBe(true);
			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/chat/completions",
				expect.objectContaining({
					method: "POST",
					headers: expect.objectContaining({
						"Content-Type": "application/json",
					}),
					body: JSON.stringify(chatBody),
				}),
			);
		});

		it("throws on error response", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response("model not found", { status: 404 }),
			);

			await expect(api.chat.completions(chatBody)).rejects.toThrow(
				"Chat failed: 404 model not found",
			);
		});
	});

	describe("chat", () => {
		it("sends chat request", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify({ id: "chat-123" }), { status: 200 }),
			);

			const result = await api.chat.chat(chatBody);

			expect(result).toBeInstanceOf(Response);
			expect(result.ok).toBe(true);
			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/chat/chat",
				expect.objectContaining({
					method: "POST",
					headers: expect.objectContaining({}),
					body: JSON.stringify(chatBody),
				}),
			);
		});

		it("passes signal to fetch when provided", async () => {
			const abortController = new AbortController();
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(null, { status: 200 }),
			);

			await api.chat.chat({ ...chatBody, signal: abortController.signal });

			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/chat/chat",
				expect.objectContaining({
					signal: abortController.signal,
				}),
			);
		});

		it("throws on error response", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response("rate limited", { status: 429 }),
			);

			await expect(api.chat.chat(chatBody)).rejects.toThrow(
				"Chat failed: 429 rate limited",
			);
		});
	});

	describe("arena", () => {
		it("sends arena request", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify({ winner: "model-a" }), { status: 200 }),
			);

			const result = await api.chat.arena(chatBody);

			expect(result).toBeInstanceOf(Response);
			expect(result.ok).toBe(true);
			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/chat/arena",
				expect.objectContaining({
					method: "POST",
					headers: expect.objectContaining({}),
					body: JSON.stringify(chatBody),
				}),
			);
		});

		it("passes signal to fetch when provided", async () => {
			const abortController = new AbortController();
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(null, { status: 200 }),
			);

			await api.chat.arena({ ...chatBody, signal: abortController.signal });

			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/chat/arena",
				expect.objectContaining({
					signal: abortController.signal,
				}),
			);
		});

		it("throws on error response", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response("arena unavailable", { status: 503 }),
			);

			await expect(api.chat.arena(chatBody)).rejects.toThrow(
				"Arena failed: 503 arena unavailable",
			);
		});
	});
});
