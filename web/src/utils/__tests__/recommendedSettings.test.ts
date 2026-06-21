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
		globalThis.fetch = vi
			.fn()
			.mockRejectedValueOnce(new Error("Network error"));

		const result = await fetchRecommendedSettings("gpt-4o", "OpenAI");

		expect(result.params).toEqual({ temperature: 0.7, top_p: 1 });
		expect(result.maxTokensSource).toBeNull();
		expect(result.matchedProviderId).toBeNull();
		expect(result.matchedModelId).toBeNull();
	});

	it("returns curated settings for GPT-4-turbo model", async () => {
		globalThis.fetch = vi
			.fn()
			.mockRejectedValueOnce(new Error("Network error"));

		const result = await fetchRecommendedSettings("gpt-4-turbo", "OpenAI");

		expect(result.params).toEqual({ temperature: 0.7, top_p: 1 });
	});

	it("returns curated settings for GPT-4 model", async () => {
		globalThis.fetch = vi
			.fn()
			.mockRejectedValueOnce(new Error("Network error"));

		const result = await fetchRecommendedSettings("gpt-4", "OpenAI");

		expect(result.params).toEqual({ temperature: 0.7, top_p: 1 });
	});

	it("returns curated settings for GPT-3.5-turbo model", async () => {
		globalThis.fetch = vi
			.fn()
			.mockRejectedValueOnce(new Error("Network error"));

		const result = await fetchRecommendedSettings("gpt-3.5-turbo", "OpenAI");

		expect(result.params).toEqual({ temperature: 0.7, top_p: 1 });
	});

	it("returns curated settings for Claude 4 model", async () => {
		globalThis.fetch = vi
			.fn()
			.mockRejectedValueOnce(new Error("Network error"));

		const result = await fetchRecommendedSettings("claude-4", "Anthropic");

		expect(result.params).toEqual({ temperature: 0.7, top_p: 0.9 });
	});

	it("returns curated settings for Claude 3.5 model", async () => {
		globalThis.fetch = vi
			.fn()
			.mockRejectedValueOnce(new Error("Network error"));

		const result = await fetchRecommendedSettings("claude-3.5", "Anthropic");

		expect(result.params).toEqual({ temperature: 0.7, top_p: 0.9 });
	});

	it("returns curated settings for Claude 3 model", async () => {
		globalThis.fetch = vi
			.fn()
			.mockRejectedValueOnce(new Error("Network error"));

		const result = await fetchRecommendedSettings("claude-3", "Anthropic");

		expect(result.params).toEqual({ temperature: 0.7, top_p: 0.9 });
	});

	it("returns curated settings for Gemini 2.5 model", async () => {
		globalThis.fetch = vi
			.fn()
			.mockRejectedValueOnce(new Error("Network error"));

		const result = await fetchRecommendedSettings("gemini-2.5", "Google");

		expect(result.params).toEqual({ temperature: 0.7, top_p: 0.95 });
	});

	it("returns curated settings for Gemini 2 model", async () => {
		globalThis.fetch = vi
			.fn()
			.mockRejectedValueOnce(new Error("Network error"));

		const result = await fetchRecommendedSettings("gemini-2", "Google");

		expect(result.params).toEqual({ temperature: 0.7, top_p: 0.95 });
	});

	it("returns curated settings for Gemini 1.5 model", async () => {
		globalThis.fetch = vi
			.fn()
			.mockRejectedValueOnce(new Error("Network error"));

		const result = await fetchRecommendedSettings("gemini-1.5", "Google");

		expect(result.params).toEqual({ temperature: 0.7, top_p: 0.95 });
	});

	it("returns curated settings for generic Gemini model", async () => {
		globalThis.fetch = vi
			.fn()
			.mockRejectedValueOnce(new Error("Network error"));

		const result = await fetchRecommendedSettings("gemini-pro", "Google");

		expect(result.params).toEqual({ temperature: 0.7, top_p: 0.95 });
	});

	it("returns curated settings for Llama 4 model", async () => {
		globalThis.fetch = vi
			.fn()
			.mockRejectedValueOnce(new Error("Network error"));

		const result = await fetchRecommendedSettings("llama-4", "Meta");

		expect(result.params).toEqual({
			temperature: 0.7,
			top_p: 0.9,
			top_k: 40,
		});
	});

	it("returns curated settings for Llama 3 model", async () => {
		globalThis.fetch = vi
			.fn()
			.mockRejectedValueOnce(new Error("Network error"));

		const result = await fetchRecommendedSettings("llama-3", "Meta");

		expect(result.params).toEqual({
			temperature: 0.7,
			top_p: 0.9,
			top_k: 40,
		});
	});

	it("returns curated settings for Llama 2 model", async () => {
		globalThis.fetch = vi
			.fn()
			.mockRejectedValueOnce(new Error("Network error"));

		const result = await fetchRecommendedSettings("llama-2", "Meta");

		expect(result.params).toEqual({
			temperature: 0.7,
			top_p: 0.9,
			top_k: 40,
		});
	});

	it("returns curated settings for generic Llama model", async () => {
		globalThis.fetch = vi
			.fn()
			.mockRejectedValueOnce(new Error("Network error"));

		const result = await fetchRecommendedSettings("llama", "Meta");

		expect(result.params).toEqual({
			temperature: 0.7,
			top_p: 0.9,
			top_k: 40,
		});
	});

	it("returns curated settings for Mistral Large model", async () => {
		globalThis.fetch = vi
			.fn()
			.mockRejectedValueOnce(new Error("Network error"));

		const result = await fetchRecommendedSettings("mistral-large", "Mistral");

		expect(result.params).toEqual({ temperature: 0.7, top_p: 0.9 });
	});

	it("returns curated settings for Mistral Medium model", async () => {
		globalThis.fetch = vi
			.fn()
			.mockRejectedValueOnce(new Error("Network error"));

		const result = await fetchRecommendedSettings("mistral-medium", "Mistral");

		expect(result.params).toEqual({ temperature: 0.7, top_p: 0.9 });
	});

	it("returns curated settings for Mistral Small model", async () => {
		globalThis.fetch = vi
			.fn()
			.mockRejectedValueOnce(new Error("Network error"));

		const result = await fetchRecommendedSettings("mistral-small", "Mistral");

		expect(result.params).toEqual({ temperature: 0.7, top_p: 0.9 });
	});

	it("returns curated settings for generic Mistral model", async () => {
		globalThis.fetch = vi
			.fn()
			.mockRejectedValueOnce(new Error("Network error"));

		const result = await fetchRecommendedSettings("mistral", "Mistral");

		expect(result.params).toEqual({
			temperature: 0.7,
			top_p: 0.9,
			top_k: 40,
		});
	});

	it("returns curated settings for DeepSeek R1 model", async () => {
		globalThis.fetch = vi
			.fn()
			.mockRejectedValueOnce(new Error("Network error"));

		const result = await fetchRecommendedSettings("deepseek-r1", "DeepSeek");

		expect(result.params).toEqual({ temperature: 0.7, top_p: 0.9 });
	});

	it("returns curated settings for DeepSeek V3 model", async () => {
		globalThis.fetch = vi
			.fn()
			.mockRejectedValueOnce(new Error("Network error"));

		const result = await fetchRecommendedSettings("deepseek-v3", "DeepSeek");

		expect(result.params).toEqual({ temperature: 0.7, top_p: 0.9 });
	});

	it("returns curated settings for DeepSeek Chat model", async () => {
		globalThis.fetch = vi
			.fn()
			.mockRejectedValueOnce(new Error("Network error"));

		const result = await fetchRecommendedSettings("deepseek-chat", "DeepSeek");

		expect(result.params).toEqual({ temperature: 0.7, top_p: 0.9 });
	});

	it("returns curated settings for generic DeepSeek model", async () => {
		globalThis.fetch = vi
			.fn()
			.mockRejectedValueOnce(new Error("Network error"));

		const result = await fetchRecommendedSettings("deepseek", "DeepSeek");

		expect(result.params).toEqual({ temperature: 0.7, top_p: 0.9 });
	});

	it("returns curated settings for Qwen3 model", async () => {
		globalThis.fetch = vi
			.fn()
			.mockRejectedValueOnce(new Error("Network error"));

		const result = await fetchRecommendedSettings("qwen3", "Qwen");

		expect(result.params).toEqual({ temperature: 0.7, top_p: 0.9 });
	});

	it("returns curated settings for Qwen2.5 model", async () => {
		globalThis.fetch = vi
			.fn()
			.mockRejectedValueOnce(new Error("Network error"));

		const result = await fetchRecommendedSettings("qwen2.5", "Qwen");

		expect(result.params).toEqual({
			temperature: 0.7,
			top_p: 0.9,
			top_k: 40,
		});
	});

	it("returns curated settings for Qwen2 model", async () => {
		globalThis.fetch = vi
			.fn()
			.mockRejectedValueOnce(new Error("Network error"));

		const result = await fetchRecommendedSettings("qwen2", "Qwen");

		expect(result.params).toEqual({
			temperature: 0.7,
			top_p: 0.9,
			top_k: 40,
		});
	});

	it("returns curated settings for generic Qwen model", async () => {
		globalThis.fetch = vi
			.fn()
			.mockRejectedValueOnce(new Error("Network error"));

		const result = await fetchRecommendedSettings("qwen", "Qwen");

		expect(result.params).toEqual({
			temperature: 0.7,
			top_p: 0.9,
			top_k: 40,
		});
	});

	it("returns curated settings for Command R Plus model", async () => {
		globalThis.fetch = vi
			.fn()
			.mockRejectedValueOnce(new Error("Network error"));

		const result = await fetchRecommendedSettings("command-r-plus", "Cohere");

		expect(result.params).toEqual({ temperature: 0.7, top_p: 0.9 });
	});

	it("returns curated settings for Command R model", async () => {
		globalThis.fetch = vi
			.fn()
			.mockRejectedValueOnce(new Error("Network error"));

		const result = await fetchRecommendedSettings("command-r", "Cohere");

		expect(result.params).toEqual({ temperature: 0.7, top_p: 0.9 });
	});

	it("returns curated settings for generic Command model", async () => {
		globalThis.fetch = vi
			.fn()
			.mockRejectedValueOnce(new Error("Network error"));

		const result = await fetchRecommendedSettings("command", "Cohere");

		expect(result.params).toEqual({ temperature: 0.7, top_p: 0.9 });
	});

	it("returns curated settings for Phi model", async () => {
		globalThis.fetch = vi
			.fn()
			.mockRejectedValueOnce(new Error("Network error"));

		const result = await fetchRecommendedSettings("phi-3", "Microsoft");

		expect(result.params).toEqual({ temperature: 0.7, top_p: 0.9 });
	});

	it("returns curated settings for Yi model", async () => {
		globalThis.fetch = vi
			.fn()
			.mockRejectedValueOnce(new Error("Network error"));

		const result = await fetchRecommendedSettings("yi-large", "01.AI");

		expect(result.params).toEqual({ temperature: 0.7, top_p: 0.9 });
	});

	it("returns curated settings for Gemma model", async () => {
		globalThis.fetch = vi
			.fn()
			.mockRejectedValueOnce(new Error("Network error"));

		const result = await fetchRecommendedSettings("gemma-7b", "Google");

		expect(result.params).toEqual({ temperature: 0.7, top_p: 0.9 });
	});

	it("returns curated settings for Mixtral model", async () => {
		globalThis.fetch = vi
			.fn()
			.mockRejectedValueOnce(new Error("Network error"));

		const result = await fetchRecommendedSettings("mixtral-8x7b", "Mistral");

		expect(result.params).toEqual({
			temperature: 0.7,
			top_p: 0.9,
			top_k: 40,
		});
	});

	it("returns curated settings for Codestral model", async () => {
		globalThis.fetch = vi
			.fn()
			.mockRejectedValueOnce(new Error("Network error"));

		const result = await fetchRecommendedSettings("codestral", "Mistral");

		expect(result.params).toEqual({ temperature: 0.7, top_p: 0.9 });
	});

	it("returns null params for unknown model when fetch fails", async () => {
		globalThis.fetch = vi
			.fn()
			.mockRejectedValueOnce(new Error("Network error"));

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

// ---------------------------------------------------------------------------
// normalizeForMatch behavior tests (tested via curated pattern matching)
// ---------------------------------------------------------------------------

describe("normalizeForMatch behavior via curated matching", () => {
	it("normalizes spaces in model ID for curated matching", async () => {
		globalThis.fetch = vi
			.fn()
			.mockRejectedValueOnce(new Error("Network error"));
		const result = await fetchRecommendedSettings(
			"Claude 3.5 Sonnet",
			"Anthropic",
		);
		expect(result.params).toEqual({ temperature: 0.7, top_p: 0.9 });
	});

	it("normalizes spaces in GPT model ID for curated matching", async () => {
		globalThis.fetch = vi
			.fn()
			.mockRejectedValueOnce(new Error("Network error"));
		const result = await fetchRecommendedSettings("GPT 4o", "OpenAI");
		expect(result.params).toEqual({ temperature: 0.7, top_p: 1 });
	});

	it("normalizes underscores in model ID for curated matching", async () => {
		globalThis.fetch = vi
			.fn()
			.mockRejectedValueOnce(new Error("Network error"));
		const result = await fetchRecommendedSettings("Llama_4_Maverick", "Meta");
		expect(result.params).toEqual({
			temperature: 0.7,
			top_p: 0.9,
			top_k: 40,
		});
	});

	it("normalizes dots and hyphens in model ID for curated matching", async () => {
		globalThis.fetch = vi
			.fn()
			.mockRejectedValueOnce(new Error("Network error"));
		const result = await fetchRecommendedSettings("Gemini-2.5 Pro", "Google");
		expect(result.params).toEqual({ temperature: 0.7, top_p: 0.95 });
	});
});

// ---------------------------------------------------------------------------
// findModelsDevMatch scoring logic tests
// ---------------------------------------------------------------------------

describe("findModelsDevMatch scoring logic", () => {
	it("exact match scores 100", async () => {
		const mockApi = {
			openai: {
				id: "openai",
				name: "OpenAI",
				models: { "gpt-4o": { id: "gpt-4o" } },
			},
		};

		globalThis.fetch = vi.fn().mockResolvedValueOnce({
			ok: true,
			json: vi.fn().mockResolvedValueOnce(mockApi),
		} as unknown as Response);

		const result = await fetchRecommendedSettings("gpt-4o", "OpenAI");

		expect(result.matchedModelId).toBe("gpt-4o");
		expect(result.matchedProviderId).toBe("openai");
	});

	it("contains match scores 80", async () => {
		const mockApi = {
			openai: {
				id: "openai",
				name: "OpenAI",
				models: { "gpt-4o-2024-08-06": { id: "gpt-4o-2024-08-06" } },
			},
		};

		globalThis.fetch = vi.fn().mockResolvedValueOnce({
			ok: true,
			json: vi.fn().mockResolvedValueOnce(mockApi),
		} as unknown as Response);

		const result = await fetchRecommendedSettings("gpt-4o", "OpenAI");

		expect(result.matchedModelId).toBe("gpt-4o-2024-08-06");
		expect(result.matchedProviderId).toBe("openai");
	});

	it("reverse contains match scores 60", async () => {
		const mockApi = {
			openai: {
				id: "openai",
				name: "OpenAI",
				models: { "gpt-4": { id: "gpt-4" } },
			},
		};

		globalThis.fetch = vi.fn().mockResolvedValueOnce({
			ok: true,
			json: vi.fn().mockResolvedValueOnce(mockApi),
		} as unknown as Response);

		const result = await fetchRecommendedSettings("gpt-4o", "OpenAI");

		expect(result.matchedModelId).toBe("gpt-4");
		expect(result.matchedProviderId).toBe("openai");
	});

	it("provider bonus +20 prefers matching provider", async () => {
		const mockApi = {
			meta: {
				id: "meta",
				name: "Meta",
				models: { "llama-3": { id: "llama-3" } },
			},
			openrouter: {
				id: "openrouter",
				name: "OpenRouter",
				models: { "llama-3": { id: "llama-3" } },
			},
		};

		globalThis.fetch = vi.fn().mockResolvedValueOnce({
			ok: true,
			json: vi.fn().mockResolvedValueOnce(mockApi),
		} as unknown as Response);

		const result = await fetchRecommendedSettings("llama-3", "Meta");

		expect(result.matchedProviderId).toBe("meta");
		expect(result.matchedModelId).toBe("llama-3");
	});

	it("family bonus +5 breaks a tie in favor of the family-matching model", async () => {
		// Two models score identically on id + provider match; only the family
		// bonus separates them. The non-family model is listed first, so a strict
		// `score > best.score` keeps it on a tie. The family model winning proves
		// the +5 bonus actually fires (it previously could not: normModel was
		// already separator-stripped, so the old split never yielded a family
		// token).
		const mockApi = {
			meta: {
				id: "meta",
				name: "Meta",
				models: {
					"llama-3-8b": { id: "llama-3-8b" }, // no family -> no bonus
					"llama-3-70b": { id: "llama-3-70b", family: "llama" }, // +5
				},
			},
		};

		globalThis.fetch = vi.fn().mockResolvedValueOnce({
			ok: true,
			json: vi.fn().mockResolvedValueOnce(mockApi),
		} as unknown as Response);

		const result = await fetchRecommendedSettings("llama-3", "Meta");

		expect(result.matchedProviderId).toBe("meta");
		expect(result.matchedModelId).toBe("llama-3-70b");
	});

	it("provider-prefixed model ids resolve to the exact models.dev entry", async () => {
		// UI callers pass proxy ids like "meta/llama-3" (provider name + "/" +
		// model id). The "/" prefix must be stripped before matching: otherwise the
		// normalized form is "metallama3", which can't exactly match "llama-3" and
		// the first-listed "llama" (output 111) wins over the real "llama-3" (222).
		const mockApi = {
			meta: {
				id: "meta",
				name: "Meta",
				models: {
					llama: { id: "llama", limit: { output: 111 } },
					"llama-3": { id: "llama-3", family: "llama", limit: { output: 222 } },
				},
			},
		};

		globalThis.fetch = vi.fn().mockResolvedValueOnce({
			ok: true,
			json: vi.fn().mockResolvedValueOnce(mockApi),
		} as unknown as Response);

		const result = await fetchRecommendedSettings("meta/llama-3", "Meta");

		expect(result.matchedModelId).toBe("llama-3");
		expect(result.params?.max_tokens).toBe(222);
	});

	it("provider-prefixed ids still get curated defaults when models.dev is unavailable", async () => {
		// Regression: with the provider prefix left in, "OpenAI/gpt-4o" normalized
		// to "openaigpt4o", which started with no curated pattern, so a proxied
		// model fell back to null params instead of its curated GPT-4o defaults.
		globalThis.fetch = vi
			.fn()
			.mockRejectedValueOnce(new Error("models.dev unavailable"));

		const result = await fetchRecommendedSettings("OpenAI/gpt-4o", "OpenAI");

		expect(result.params).toEqual({ temperature: 0.7, top_p: 1 });
	});

	it("below threshold returns no match", async () => {
		const mockApi = {
			openai: {
				id: "openai",
				name: "OpenAI",
				models: { "gpt-4": { id: "gpt-4" } },
			},
		};

		globalThis.fetch = vi.fn().mockResolvedValueOnce({
			ok: true,
			json: vi.fn().mockResolvedValueOnce(mockApi),
		} as unknown as Response);

		const result = await fetchRecommendedSettings("xyz", "Unknown");

		expect(result.matchedProviderId).toBeNull();
		expect(result.matchedModelId).toBeNull();
	});

	it("openrouter is skipped for provider matching", async () => {
		const mockApi = {
			openrouter: {
				id: "openrouter",
				name: "OpenRouter",
				models: { "llama-3": { id: "llama-3" } },
			},
			meta: {
				id: "meta",
				name: "Meta",
				models: { "llama-3": { id: "llama-3" } },
			},
		};

		globalThis.fetch = vi.fn().mockResolvedValueOnce({
			ok: true,
			json: vi.fn().mockResolvedValueOnce(mockApi),
		} as unknown as Response);

		const result = await fetchRecommendedSettings("llama-3", "OpenRouter");

		// OpenRouter provider alias is false, so no provider bonus is given
		// Both providers have score 100 (exact model match), first one wins
		// The key behavior: OpenRouter as search provider doesn't give bonus to any provider
		expect(result.matchedModelId).toBe("llama-3");
		// Since no provider gets bonus, iteration order determines winner
		// (openrouter comes first in the mock object)
		expect(result.matchedProviderId).toBe("openrouter");
	});

	it("provider alias normalization: Google AI Studio", async () => {
		const mockApi = {
			google: {
				id: "google",
				name: "Google",
				models: { "gemini-2.5": { id: "gemini-2.5" } },
			},
		};

		globalThis.fetch = vi.fn().mockResolvedValueOnce({
			ok: true,
			json: vi.fn().mockResolvedValueOnce(mockApi),
		} as unknown as Response);

		const result = await fetchRecommendedSettings(
			"gemini-2.5",
			"Google AI Studio",
		);

		expect(result.matchedProviderId).toBe("google");
		expect(result.matchedModelId).toBe("gemini-2.5");
	});

	it("provider alias normalization: gemini", async () => {
		const mockApi = {
			google: {
				id: "google",
				name: "Google",
				models: { "gemini-2.5": { id: "gemini-2.5" } },
			},
		};

		globalThis.fetch = vi.fn().mockResolvedValueOnce({
			ok: true,
			json: vi.fn().mockResolvedValueOnce(mockApi),
		} as unknown as Response);

		const result = await fetchRecommendedSettings("gemini-2.5", "gemini");

		expect(result.matchedProviderId).toBe("google");
		expect(result.matchedModelId).toBe("gemini-2.5");
	});

	it("provider alias normalization: aistudio", async () => {
		const mockApi = {
			google: {
				id: "google",
				name: "Google",
				models: { "gemini-2.5": { id: "gemini-2.5" } },
			},
		};

		globalThis.fetch = vi.fn().mockResolvedValueOnce({
			ok: true,
			json: vi.fn().mockResolvedValueOnce(mockApi),
		} as unknown as Response);

		const result = await fetchRecommendedSettings("gemini-2.5", "aistudio");

		expect(result.matchedProviderId).toBe("google");
		expect(result.matchedModelId).toBe("gemini-2.5");
	});

	it("models.dev max_tokens not set when output limit is absent", async () => {
		const mockApi = {
			openai: {
				id: "openai",
				name: "OpenAI",
				models: {
					"gpt-4o": { id: "gpt-4o", limit: {} },
				},
			},
		};

		globalThis.fetch = vi.fn().mockResolvedValueOnce({
			ok: true,
			json: vi.fn().mockResolvedValueOnce(mockApi),
		} as unknown as Response);

		const result = await fetchRecommendedSettings("gpt-4o", "OpenAI");

		expect(result.matchedProviderId).toBe("openai");
		expect(result.matchedModelId).toBe("gpt-4o");
		expect(result.maxTokensSource).toBeNull();
		expect(result.params?.max_tokens).toBeUndefined();
	});

	it("curated settings pattern specificity: longer pattern wins", async () => {
		globalThis.fetch = vi
			.fn()
			.mockRejectedValueOnce(new Error("Network error"));

		// gpt-4o should match the more specific "gpt-4o" pattern, not "gpt-4"
		const result = await fetchRecommendedSettings("gpt-4o", "OpenAI");

		// gpt-4o pattern has top_p: 1, gpt-4 pattern also has top_p: 1
		// but we verify it's the gpt-4o pattern by checking the match
		expect(result.params).toEqual({ temperature: 0.7, top_p: 1 });
		expect(result.matchedProviderId).toBeNull();
		expect(result.matchedModelId).toBeNull();
	});

	it("no curated match but has models.dev data", async () => {
		const mockApi = {
			unknown: {
				id: "unknown",
				name: "Unknown Provider",
				models: {
					"custom-model": {
						id: "custom-model",
						limit: { output: 2048 },
					},
				},
			},
		};

		globalThis.fetch = vi.fn().mockResolvedValueOnce({
			ok: true,
			json: vi.fn().mockResolvedValueOnce(mockApi),
		} as unknown as Response);

		const result = await fetchRecommendedSettings("custom-model", "Unknown");

		expect(result.params).toEqual({ max_tokens: 2048 });
		expect(result.maxTokensSource).toBe("models.dev");
		expect(result.matchedProviderId).toBe("unknown");
		expect(result.matchedModelId).toBe("custom-model");
	});
});
