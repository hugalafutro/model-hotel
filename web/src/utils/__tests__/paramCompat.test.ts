import { describe, expect, it } from "vitest";
import {
	getParamIncompatibility,
	isParamDisabled,
	normalizeToProviderType,
} from "../paramCompat";

describe("normalizeToProviderType", () => {
	it("returns exact key match", () => {
		expect(normalizeToProviderType("openai")).toBe("openai");
	});

	it("is case-insensitive", () => {
		expect(normalizeToProviderType("OpenAI")).toBe("openai");
	});

	it("uses substring heuristic for Anthropic Pro", () => {
		expect(normalizeToProviderType("Anthropic Pro")).toBe("anthropic");
	});

	it("uses substring heuristic for Google AI Studio (Gemini)", () => {
		expect(normalizeToProviderType("Google AI Studio (Gemini)")).toBe("google");
	});

	it("returns openai fallback for empty string", () => {
		expect(normalizeToProviderType("")).toBe("openai");
	});

	it("returns lowercased name for unknown provider", () => {
		expect(normalizeToProviderType("UnknownProvider")).toBe("unknownprovider");
	});

	it("handles case variants correctly", () => {
		expect(normalizeToProviderType("ANTHROPIC")).toBe("anthropic");
		expect(normalizeToProviderType("google")).toBe("google");
		expect(normalizeToProviderType("DeepSeek")).toBe("deepseek");
	});

	it("handles providers with spaces", () => {
		expect(normalizeToProviderType("LM Studio")).toBe("lmstudio");
	});

	it("handles z.ai variants", () => {
		expect(normalizeToProviderType("z.ai Coding")).toBe("zai-coding");
	});

	it("handles Ollama Cloud (falls back to ollama)", () => {
		// "Ollama Cloud" contains "ollama" substring, so it matches ollama first
		expect(normalizeToProviderType("Ollama Cloud")).toBe("ollama");
	});
});

describe("getParamIncompatibility", () => {
	it("returns incompatibility reason for OpenAI + min_p", () => {
		const result = getParamIncompatibility("openai", "min_p");
		expect(result).toBe("Not part of the OpenAI API");
	});

	it("returns null for compatible OpenAI + temperature", () => {
		const result = getParamIncompatibility("openai", "temperature");
		expect(result).toBeNull();
	});

	it("returns deprecated message for Anthropic + top_p", () => {
		const result = getParamIncompatibility("anthropic", "top_p");
		expect(result).toBe(
			"top_p is deprecated on current Anthropic models; use top_k instead",
		);
	});

	it("returns incompatibility for Google + frequency_penalty", () => {
		const result = getParamIncompatibility("google", "frequency_penalty");
		expect(result).toBe("Gemini does not support frequency/presence penalties");
	});

	it("returns null for unknown provider", () => {
		const result = getParamIncompatibility("unknown", "min_p");
		expect(result).toBeNull();
	});

	it("returns null for nanogpt (empty rules)", () => {
		const result = getParamIncompatibility("nanogpt", "min_p");
		expect(result).toBeNull();
	});

	it("normalizes case before checking incompatibility", () => {
		const result = getParamIncompatibility("OpenAI", "min_p");
		expect(result).toBe("Not part of the OpenAI API");
	});

	it("returns null for compatible params", () => {
		expect(getParamIncompatibility("openai", "top_p")).toBeNull();
		expect(getParamIncompatibility("anthropic", "temperature")).toBeNull();
		expect(getParamIncompatibility("google", "temperature")).toBeNull();
	});

	it("handles deepseek incompatibilities", () => {
		expect(getParamIncompatibility("deepseek", "min_p")).toBe(
			"Not supported by the DeepSeek API",
		);
		expect(getParamIncompatibility("deepseek", "top_k")).toBe(
			"Not supported by the DeepSeek API",
		);
	});

	it("handles xai incompatibilities", () => {
		expect(getParamIncompatibility("xai", "min_p")).toBe(
			"Not supported by the xAI API",
		);
		expect(getParamIncompatibility("xai", "top_k")).toBe(
			"Not supported by the xAI API",
		);
	});
});

describe("normalizeToProviderType - substring heuristic", () => {
	it("matches Ollama Cloud to ollama", () => {
		expect(normalizeToProviderType("Ollama Cloud")).toBe("ollama");
	});

	it("matches LM Studio Local to lmstudio", () => {
		expect(normalizeToProviderType("LM Studio Local")).toBe("lmstudio");
	});

	it("matches Nano-GPT Free to nanogpt", () => {
		expect(normalizeToProviderType("Nano-GPT Free")).toBe("nanogpt");
	});

	it("matches KoboldCpp Local to koboldcpp", () => {
		expect(normalizeToProviderType("KoboldCpp Local")).toBe("koboldcpp");
	});

	it("matches z.ai Coding Pro to zai-coding", () => {
		expect(normalizeToProviderType("z.ai Coding Pro")).toBe("zai-coding");
	});

	it("matches OpenCode Zen v1 to opencode-zen", () => {
		expect(normalizeToProviderType("OpenCode Zen v1")).toBe("opencode-zen");
	});

	it("matches OpenCode Go v1 to opencode-go", () => {
		expect(normalizeToProviderType("OpenCode Go v1")).toBe("opencode-go");
	});

	it("matches Grok 3 to xai", () => {
		expect(normalizeToProviderType("Grok 3")).toBe("xai");
	});

	it("matches xAI API to xai", () => {
		expect(normalizeToProviderType("xAI API")).toBe("xai");
	});

	it("matches x.ai Official to xai", () => {
		expect(normalizeToProviderType("x.ai Official")).toBe("xai");
	});
});

describe("provider incompatibility coverage", () => {
	describe("cohere", () => {
		it("disables top_k", () => {
			expect(isParamDisabled("cohere", "top_k")).toBe(true);
			expect(getParamIncompatibility("cohere", "top_k")).toBe(
				"Cohere uses a different 'k' parameter; not recommended",
			);
		});

		it("disables min_p", () => {
			expect(isParamDisabled("cohere", "min_p")).toBe(true);
			expect(getParamIncompatibility("cohere", "min_p")).toBe(
				"Not supported by the Cohere API",
			);
		});
	});

	describe("ollama", () => {
		it("disables min_p", () => {
			expect(isParamDisabled("ollama", "min_p")).toBe(true);
			expect(getParamIncompatibility("ollama", "min_p")).toBe(
				"Support varies by underlying model; not universally available",
			);
		});

		it("does NOT disable top_k", () => {
			expect(isParamDisabled("ollama", "top_k")).toBe(false);
			expect(getParamIncompatibility("ollama", "top_k")).toBeNull();
		});
	});

	describe("zai-coding", () => {
		it("disables min_p", () => {
			expect(isParamDisabled("zai-coding", "min_p")).toBe(true);
			expect(getParamIncompatibility("zai-coding", "min_p")).toBe(
				"Not supported by z.ai Coding",
			);
		});

		it("disables top_k", () => {
			expect(isParamDisabled("zai-coding", "top_k")).toBe(true);
			expect(getParamIncompatibility("zai-coding", "top_k")).toBe(
				"Not supported by z.ai Coding",
			);
		});
	});
});

describe("isParamDisabled - empty rules providers", () => {
	const emptyRuleProviders = [
		"koboldcpp",
		"lmstudio",
		"custom",
		"openrouter",
		"opencode-zen",
		"opencode-go",
	];

	const commonParams = [
		"temperature",
		"max_tokens",
		"top_p",
		"top_k",
		"min_p",
		"frequency_penalty",
		"presence_penalty",
	];

	it.each(
		emptyRuleProviders,
	)("returns false for all common params on %s", (provider) => {
		commonParams.forEach((param) => {
			expect(isParamDisabled(provider, param as keyof GenerationParams)).toBe(
				false,
			);
			expect(
				getParamIncompatibility(provider, param as keyof GenerationParams),
			).toBeNull();
		});
	});
});

describe("getParamIncompatibility - additional providers", () => {
	it("handles Anthropic case-insensitively for top_p", () => {
		expect(getParamIncompatibility("Anthropic", "top_p")).toBe(
			"top_p is deprecated on current Anthropic models; use top_k instead",
		);
	});

	it("handles GOOGLE case-insensitively for frequency_penalty", () => {
		expect(getParamIncompatibility("GOOGLE", "frequency_penalty")).toBe(
			"Gemini does not support frequency/presence penalties",
		);
	});

	it("returns false for common compatible params on openai", () => {
		expect(isParamDisabled("openai", "temperature")).toBe(false);
		expect(isParamDisabled("openai", "max_tokens")).toBe(false);
		expect(isParamDisabled("openai", "top_p")).toBe(false);
		expect(isParamDisabled("openai", "frequency_penalty")).toBe(false);
		expect(isParamDisabled("openai", "presence_penalty")).toBe(false);
	});
});

describe("isParamDisabled", () => {
	it("returns true when param has incompatibility reason", () => {
		expect(isParamDisabled("openai", "min_p")).toBe(true);
		expect(isParamDisabled("anthropic", "top_p")).toBe(true);
	});

	it("returns false when param is compatible", () => {
		expect(isParamDisabled("openai", "temperature")).toBe(false);
		expect(isParamDisabled("openai", "top_p")).toBe(false);
	});

	it("returns false for unknown providers", () => {
		expect(isParamDisabled("unknown", "min_p")).toBe(false);
	});

	it("returns false for nanogpt (empty rules)", () => {
		expect(isParamDisabled("nanogpt", "min_p")).toBe(false);
	});

	it("handles case-insensitive provider names", () => {
		expect(isParamDisabled("OpenAI", "min_p")).toBe(true);
		expect(isParamDisabled("GOOGLE", "frequency_penalty")).toBe(true);
	});
});
