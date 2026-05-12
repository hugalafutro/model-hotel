import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { fetchRecommendedSettings } from "../recommendedSettings";

describe("fetchRecommendedSettings", () => {
	beforeEach(() => {
		vi.spyOn(globalThis, "fetch").mockClear();
	});

	afterEach(() => {
		vi.restoreAllMocks();
	});

	it("returns curated settings for GPT-4o model", async () => {
		globalThis.fetch = vi.fn().mockRejectedValueOnce(new Error("Network error"));

		const result = await fetchRecommendedSettings("gpt-4o", "OpenAI");

		expect(result.params).toEqual({ temperature: 0.7, top_p: 1 });
		expect(result.maxTokensSource).toBeNull();
		expect(result.matchedProviderId).toBeNull();
		expect(result.matchedModelId).toBeNull();
	});

	it("returns curated settings for GPT-4-turbo model", async () => {
		globalThis.fetch = vi.fn().mockRejectedValueOnce(new Error("Network error"));

		const result = await fetchRecommendedSettings("gpt-4-turbo", "OpenAI");

		expect(result.params).toEqual({ temperature: 0.7, top_p: 1 });
	});

	it("returns curated settings for GPT-4 model", async () => {
		globalThis.fetch = vi.fn().mockRejectedValueOnce(new Error("Network error"));

		const result = await fetchRecommendedSettings("gpt-4", "OpenAI");

		expect(result.params).toEqual({ temperature: 0.7, top_p: 1 });
	});

	it("returns curated settings for GPT-3.5-turbo model", async () => {
		globalThis.fetch = vi.fn().mockRejectedValueOnce(new Error("Network error"));

		const result = await fetchRecommendedSettings("gpt-3.5-turbo", "OpenAI");

		expect(result.params).toEqual({ temperature: 0.7, top_p: 1 });
	});

	it("returns curated settings for Claude 4 model", async () => {
		globalThis.fetch = vi.fn().mockRejectedValueOnce(new Error("Network error"));

		const result = await fetchRecommendedSettings("claude-4", "Anthropic");

		expect(result.params).toEqual({ temperature: 0.7, top_p: 0.9 });
	});

	it("returns curated settings for Claude 3.5 model", async () => {
		globalThis.fetch = vi.fn().mockRejectedValueOnce(new Error("Network error"));

		const result = await fetchRecommendedSettings("claude-3.5", "Anthropic");

		expect(result.params).toEqual({ temperature: 0.7, top_p: 0.9 });
	});

	it("returns curated settings for Claude 3 model", async () => {
		globalThis.fetch = vi.fn().mockRejectedValueOnce(new Error("Network error"));

		const result = await fetchRecommendedSettings("claude-3", "Anthropic");

		expect(result.params).toEqual({ temperature: 0.7, top_p: 0.9 });
	});

	it("returns curated settings for Gemini 2.5 model", async () => {
		globalThis.fetch = vi.fn().mockRejectedValueOnce(new Error("Network error"));

		const result = await fetchRecommendedSettings("gemini-2.5", "Google");

		expect(result.params).toEqual({ temperature: 0.7, top_p: 0.95 });
	});

	it("returns curated settings for Gemini 2 model", async () => {
		globalThis.fetch = vi.fn().mockRejectedValueOnce(new Error("Network error"));

		const result = await fetchRecommendedSettings("gemini-2", "Google");

		expect(result.params).toEqual({ temperature: 0.7, top_p: 0.95 });
	});

	it("returns curated settings for Gemini 1.5 model", async () => {
		globalThis.fetch = vi.fn().mockRejectedValueOnce(new Error("Network error"));

		const result = await fetchRecommendedSettings("gemini-1.5", "Google");

		expect(result.params).toEqual({ temperature: 0.7, top_p: 0.95 });
	});

	it("returns curated settings for generic Gemini model", async () => {
		globalThis.fetch = vi.fn().mockRejectedValueOnce(new Error("Network error"));

		const result = await fetchRecommendedSettings("gemini-pro", "Google");

		expect(result.params).toEqual({ temperature: 0.7, top_p: 0.95 });
	});

	it("returns curated settings for Llama 4 model", async () => {
		globalThis.fetch = vi.fn().mockRejectedValueOnce(new Error("Network error"));

		const result = await fetchRecommendedSettings("llama-4", "Meta");

		expect(result.params).toEqual({
			temperature: 0.7,
			top_p: 0.9,
			top_k: 40,
		});
	});

	it("returns curated settings for Llama 3 model", async () => {
		globalThis.fetch = vi.fn().mockRejectedValueOnce(new Error("Network error"));

		const result = await fetchRecommendedSettings("llama-3", "Meta");

		expect(result.params).toEqual({
			temperature: 0.7,
			top_p: 0.9,
			top_k: 40,
		});
	});

	it("returns curated settings for Llama 2 model", async () => {
		globalThis.fetch = vi.fn().mockRejectedValueOnce(new Error("Network error"));

		const result = await fetchRecommendedSettings("llama-2", "Meta");

		expect(result.params).toEqual({
			temperature: 0.7,
			top_p: 0.9,
			top_k: 40,
		});
	});

	it("returns curated settings for generic Llama model", async () => {
		globalThis.fetch = vi.fn().mockRejectedValueOnce(new Error("Network error"));

		const result = await fetchRecommendedSettings("llama", "Meta");

		expect(result.params).toEqual({
			temperature: 0.7,
			top_p: 0.9,
			top_k: 40,
		});
	});

	it("returns curated settings for Mistral Large model", async () => {
		globalThis.fetch = vi.fn().mockRejectedValueOnce(new Error("Network error"));

		const result = await fetchRecommendedSettings("mistral-large", "Mistral");

		expect(result.params).toEqual({ temperature: 0.7, top_p: 0.9 });
	});

	it("returns curated settings for Mistral Medium model", async () => {
		globalThis.fetch = vi.fn().mockRejectedValueOnce(new Error("Network error"));

		const result = await fetchRecommendedSettings("mistral-medium", "Mistral");

		expect(result.params).toEqual({ temperature: 0.7, top_p: 0.9 });
	});

	it("returns curated settings for Mistral Small model", async () => {
		globalThis.fetch = vi.fn().mockRejectedValueOnce(new Error("Network error"));

		const result = await fetchRecommendedSettings("mistral-small", "Mistral");

		expect(result.params).toEqual({ temperature: 0.7, top_p: 0.9 });
	});

	it("returns curated settings for generic Mistral model", async () => {
		globalThis.fetch = vi.fn().mockRejectedValueOnce(new Error("Network error"));

		const result = await fetchRecommendedSettings("mistral", "Mistral");

		expect(result.params).toEqual({
			temperature: 0.7,
			top_p: 0.9,
			top_k: 40,
		});
	});

	it("returns curated settings for DeepSeek R1 model", async () => {
		globalThis.fetch = vi.fn().mockRejectedValueOnce(new Error("Network error"));

		const result = await fetchRecommendedSettings("deepseek-r1", "DeepSeek");

		expect(result.params).toEqual({ temperature: 0.7, top_p: 0.9 });
	});

	it("returns curated settings for DeepSeek V3 model", async () => {
		globalThis.fetch = vi.fn().mockRejectedValueOnce(new Error("Network error"));

		const result = await fetchRecommendedSettings("deepseek-v3", "DeepSeek");

		expect(result.params).toEqual({ temperature: 0.7, top_p: 0.9 });
	});

	it("returns curated settings for DeepSeek Chat model", async () => {
		globalThis.fetch = vi.fn().mockRejectedValueOnce(new Error("Network error"));

		const result = await fetchRecommendedSettings("deepseek-chat", "DeepSeek");

		expect(result.params).toEqual({ temperature: 0.7, top_p: 0.9 });
	});

	it("returns curated settings for generic DeepSeek model", async () => {
		globalThis.fetch = vi.fn().mockRejectedValueOnce(new Error("Network error"));

		const result = await fetchRecommendedSettings("deepseek", "DeepSeek");

		expect(result.params).toEqual({ temperature: 0.7, top_p: 0.9 });
	});

	it("returns curated settings for Qwen3 model", async () => {
		globalThis.fetch = vi.fn().mockRejectedValueOnce(new Error("Network error"));

		const result = await fetchRecommendedSettings("qwen3", "Qwen");

		expect(result.params).toEqual({ temperature: 0.7, top_p: 0.9 });
	});

	it("returns curated settings for Qwen2.5 model", async () => {
		globalThis.fetch = vi.fn().mockRejectedValueOnce(new Error("Network error"));

		const result = await fetchRecommendedSettings("qwen2.5", "Qwen");

		expect(result.params).toEqual({
			temperature: 0.7,
			top_p: 0.9,
			top_k: 40,
		});
	});

	it("returns curated settings for Qwen2 model", async () => {
		globalThis.fetch = vi.fn().mockRejectedValueOnce(new Error("Network error"));

		const result = await fetchRecommendedSettings("qwen2", "Qwen");

		expect(result.params).toEqual({
			temperature: 0.7,
			top_p: 0.9,
			top_k: 40,
		});
	});

	it("returns curated settings for generic Qwen model", async () => {
		globalThis.fetch = vi.fn().mockRejectedValueOnce(new Error("Network error"));

		const result = await fetchRecommendedSettings("qwen", "Qwen");

		expect(result.params).toEqual({
			temperature: 0.7,
			top_p: 0.9,
			top_k: 40,
		});
	});

	it("returns curated settings for Command R Plus model", async () => {
		globalThis.fetch = vi.fn().mockRejectedValueOnce(new Error("Network error"));

		const result = await fetchRecommendedSettings("command-r-plus", "Cohere");

		expect(result.params).toEqual({ temperature: 0.7, top_p: 0.9 });
	});

	it("returns curated settings for Command R model", async () => {
		globalThis.fetch = vi.fn().mockRejectedValueOnce(new Error("Network error"));

		const result = await fetchRecommendedSettings("command-r", "Cohere");

		expect(result.params).toEqual({ temperature: 0.7, top_p: 0.9 });
	});

	it("returns curated settings for generic Command model", async () => {
		globalThis.fetch = vi.fn().mockRejectedValueOnce(new Error("Network error"));

		const result = await fetchRecommendedSettings("command", "Cohere");

		expect(result.params).toEqual({ temperature: 0.7, top_p: 0.9 });
	});

	it("returns curated settings for Phi model", async () => {
		globalThis.fetch = vi.fn().mockRejectedValueOnce(new Error("Network error"));

		const result = await fetchRecommendedSettings("phi-3", "Microsoft");

		expect(result.params).toEqual({ temperature: 0.7, top_p: 0.9 });
	});

	it("returns curated settings for Yi model", async () => {
		globalThis.fetch = vi.fn().mockRejectedValueOnce(new Error("Network error"));

		const result = await fetchRecommendedSettings("yi-large", "01.AI");

		expect(result.params).toEqual({ temperature: 0.7, top_p: 0.9 });
	});

	it("returns curated settings for Gemma model", async () => {
		globalThis.fetch = vi.fn().mockRejectedValueOnce(new Error("Network error"));

		const result = await fetchRecommendedSettings("gemma-7b", "Google");

		expect(result.params).toEqual({ temperature: 0.7, top_p: 0.9 });
	});

	it("returns curated settings for Mixtral model", async () => {
		globalThis.fetch = vi.fn().mockRejectedValueOnce(new Error("Network error"));

		const result = await fetchRecommendedSettings("mixtral-8x7b", "Mistral");

		expect(result.params).toEqual({
			temperature: 0.7,
			top_p: 0.9,
			top_k: 40,
		});
	});

	it("returns curated settings for Codestral model", async () => {
		globalThis.fetch = vi.fn().mockRejectedValueOnce(new Error("Network error"));

		const result = await fetchRecommendedSettings("codestral", "Mistral");

		expect(result.params).toEqual({ temperature: 0.7, top_p: 0.9 });
	});

	it("returns null params for unknown model when fetch fails", async () => {
		globalThis.fetch = vi.fn().mockRejectedValueOnce(new Error("Network error"));

		const result = await fetchRecommendedSettings("unknown-model", "Unknown");

		expect(result.params).toBeNull();
		expect(result.maxTokensSource).toBeNull();
		expect(result.matchedProviderId).toBeNull();
		expect(result.matchedModelId).toBeNull();
	});

	it("merges models.dev max_tokens with curated settings", async () => {
		const mockApiResponse = {
			openai: {
				id: "openai",
				name: "OpenAI",
				models: {
					"gpt-4o": {
						id: "gpt-4o",
						limit: { output: 8192 },
					},
				},
			},
		};

		globalThis.fetch = vi.fn().mockResolvedValueOnce({
			ok: true,
			json: vi.fn().mockResolvedValueOnce(mockApiResponse),
		} as unknown as Response);

		const result = await fetchRecommendedSettings("gpt-4o", "OpenAI");

		expect(result.params).toEqual({
			temperature: 0.7,
			top_p: 1,
			max_tokens: 4096,
		});
		expect(result.maxTokensSource).toBe("models.dev");
		expect(result.matchedProviderId).toBe("openai");
		expect(result.matchedModelId).toBe("gpt-4o");
	});

	it("caps models.dev max_tokens at 4096", async () => {
		const mockApiResponse = {
			openai: {
				id: "openai",
				name: "OpenAI",
				models: {
					"gpt-4o": {
						id: "gpt-4o",
						limit: { output: 100000 },
					},
				},
			},
		};

		globalThis.fetch = vi.fn().mockResolvedValueOnce({
			ok: true,
			json: vi.fn().mockResolvedValueOnce(mockApiResponse),
		} as unknown as Response);

		const result = await fetchRecommendedSettings("gpt-4o", "OpenAI");

		expect(result.params?.max_tokens).toBe(4096);
	});

	it("uses models.dev max_tokens even without curated settings", async () => {
		const mockApiResponse = {
			unknown: {
				id: "unknown",
				name: "Unknown Provider",
				models: {
					"unknown-model": {
						id: "unknown-model",
						limit: { output: 2048 },
					},
				},
			},
		};

		globalThis.fetch = vi.fn().mockResolvedValueOnce({
			ok: true,
			json: vi.fn().mockResolvedValueOnce(mockApiResponse),
		} as unknown as Response);

		const result = await fetchRecommendedSettings("unknown-model", "Unknown");

		expect(result.params).toEqual({ max_tokens: 2048 });
		expect(result.maxTokensSource).toBe("models.dev");
		expect(result.matchedProviderId).toBe("unknown");
		expect(result.matchedModelId).toBe("unknown-model");
	});

	it("handles models.dev API timeout gracefully", async () => {
		const controller = new AbortController();
		globalThis.fetch = vi.fn().mockImplementation(() => {
			controller.abort();
			return Promise.reject(new Error("Timeout"));
		});

		const result = await fetchRecommendedSettings("gpt-4o", "OpenAI");

		expect(result.params).toEqual({ temperature: 0.7, top_p: 1 });
		expect(result.maxTokensSource).toBeNull();
	});

	it("handles models.dev API non-200 response gracefully", async () => {
		globalThis.fetch = vi.fn().mockResolvedValueOnce({
			ok: false,
			status: 500,
		} as unknown as Response);

		const result = await fetchRecommendedSettings("gpt-4o", "OpenAI");

		expect(result.params).toEqual({ temperature: 0.7, top_p: 1 });
		expect(result.maxTokensSource).toBeNull();
	});

	it("normalizes provider names for models.dev matching", async () => {
		const mockApiResponse = {
			google: {
				id: "google",
				name: "Google",
				models: {
					"gemini-2.5": {
						id: "gemini-2.5",
						limit: { output: 4096 },
					},
				},
			},
		};

		globalThis.fetch = vi.fn().mockResolvedValueOnce({
			ok: true,
			json: vi.fn().mockResolvedValueOnce(mockApiResponse),
		} as unknown as Response);

		// Test with different provider name variations
		const result = await fetchRecommendedSettings(
			"gemini-2.5",
			"Google AI Studio",
		);

		expect(result.matchedProviderId).toBe("google");
		expect(result.matchedModelId).toBe("gemini-2.5");
	});

	it("matches OpenRouter provider when it has models.dev data", async () => {
		const mockApiResponse = {
			openrouter: {
				id: "openrouter",
				name: "OpenRouter",
				models: {
					"gpt-4o": {
						id: "gpt-4o",
						limit: { output: 8192 },
					},
				},
			},
		};

		globalThis.fetch = vi.fn().mockResolvedValueOnce({
			ok: true,
			json: vi.fn().mockResolvedValueOnce(mockApiResponse),
		} as unknown as Response);

		const result = await fetchRecommendedSettings("gpt-4o", "OpenRouter");

		// OpenRouter provider should match when it has the model
		expect(result.matchedProviderId).toBe("openrouter");
		expect(result.matchedModelId).toBe("gpt-4o");
		expect(result.params?.max_tokens).toBe(4096);
	});

	it("handles models without limit.output gracefully", async () => {
		const mockApiResponse = {
			openai: {
				id: "openai",
				name: "OpenAI",
				models: {
					"gpt-4o": {
						id: "gpt-4o",
						limit: {},
					},
				},
			},
		};

		globalThis.fetch = vi.fn().mockResolvedValueOnce({
			ok: true,
			json: vi.fn().mockResolvedValueOnce(mockApiResponse),
		} as unknown as Response);

		const result = await fetchRecommendedSettings("gpt-4o", "OpenAI");

		expect(result.params).toEqual({ temperature: 0.7, top_p: 1 });
		expect(result.maxTokensSource).toBeNull();
		expect(result.matchedProviderId).toBe("openai");
		expect(result.matchedModelId).toBe("gpt-4o");
	});

	it("handles models without limit property gracefully", async () => {
		const mockApiResponse = {
			openai: {
				id: "openai",
				name: "OpenAI",
				models: {
					"gpt-4o": {
						id: "gpt-4o",
					},
				},
			},
		};

		globalThis.fetch = vi.fn().mockResolvedValueOnce({
			ok: true,
			json: vi.fn().mockResolvedValueOnce(mockApiResponse),
		} as unknown as Response);

		const result = await fetchRecommendedSettings("gpt-4o", "OpenAI");

		expect(result.params).toEqual({ temperature: 0.7, top_p: 1 });
		expect(result.maxTokensSource).toBeNull();
		expect(result.matchedProviderId).toBe("openai");
		expect(result.matchedModelId).toBe("gpt-4o");
	});
});
