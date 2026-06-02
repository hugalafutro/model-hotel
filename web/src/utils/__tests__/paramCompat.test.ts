import { describe, expect, it } from "vitest";
import type { GenerationParams } from "../../api/types";
import {
	getParamIncompatibility,
	isParamDisabled,
	isParamHidden,
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

	it("handles Ollama Cloud (matches ollama-cloud directly)", () => {
		expect(normalizeToProviderType("Ollama Cloud")).toBe("ollama-cloud");
	});
});

describe("getParamIncompatibility", () => {
	it("returns incompatibility reason for OpenAI + min_p", () => {
		const result = getParamIncompatibility("openai", "min_p");
		expect(result).toBe("paramCompat.openai.minP");
	});

	it("returns null for compatible OpenAI + temperature", () => {
		const result = getParamIncompatibility("openai", "temperature");
		expect(result).toBeNull();
	});

	it("returns deprecated message for Anthropic + top_p", () => {
		const result = getParamIncompatibility("anthropic", "top_p");
		expect(result).toBe("paramCompat.anthropic.topP");
	});

	it("returns incompatibility for Google + frequency_penalty", () => {
		const result = getParamIncompatibility("google", "frequency_penalty");
		expect(result).toBe("paramCompat.google.frequencyPenalty");
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
		expect(result).toBe("paramCompat.openai.minP");
	});

	it("returns null for compatible params", () => {
		expect(getParamIncompatibility("openai", "top_p")).toBeNull();
		expect(getParamIncompatibility("anthropic", "temperature")).toBeNull();
		expect(getParamIncompatibility("google", "temperature")).toBeNull();
	});

	it("handles deepseek incompatibilities", () => {
		expect(getParamIncompatibility("deepseek", "min_p")).toBe(
			"paramCompat.deepseek.minP",
		);
		expect(getParamIncompatibility("deepseek", "top_k")).toBe(
			"paramCompat.deepseek.topK",
		);
	});

	it("handles xai incompatibilities", () => {
		expect(getParamIncompatibility("xai", "min_p")).toBe(
			"paramCompat.xai.minP",
		);
		expect(getParamIncompatibility("xai", "top_k")).toBe(
			"paramCompat.xai.topK",
		);
	});
});

describe("normalizeToProviderType - substring heuristic", () => {
	it("matches Ollama Cloud to ollama-cloud", () => {
		expect(normalizeToProviderType("Ollama Cloud")).toBe("ollama-cloud");
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
				"paramCompat.cohere.topK",
			);
		});

		it("disables min_p", () => {
			expect(isParamDisabled("cohere", "min_p")).toBe(true);
			expect(getParamIncompatibility("cohere", "min_p")).toBe(
				"paramCompat.cohere.minP",
			);
		});
	});

	describe("ollama", () => {
		it("disables min_p", () => {
			expect(isParamDisabled("ollama", "min_p")).toBe(true);
			expect(getParamIncompatibility("ollama", "min_p")).toBe(
				"paramCompat.ollama.minP",
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
				"paramCompat.zaiCoding.minP",
			);
		});

		it("disables top_k", () => {
			expect(isParamDisabled("zai-coding", "top_k")).toBe(true);
			expect(getParamIncompatibility("zai-coding", "top_k")).toBe(
				"paramCompat.zaiCoding.topK",
			);
		});
	});
});

describe("isParamDisabled - empty rules providers", () => {
	// Only custom has truly empty rules (no incompatibilities)
	// nanogpt has reasoning_effort incompatibility
	const emptyRuleProviders = ["custom"];

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
			"paramCompat.anthropic.topP",
		);
	});

	it("handles GOOGLE case-insensitively for frequency_penalty", () => {
		expect(getParamIncompatibility("GOOGLE", "frequency_penalty")).toBe(
			"paramCompat.google.frequencyPenalty",
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

	it("returns false for nanogpt common params (but not reasoning_effort)", () => {
		expect(isParamDisabled("nanogpt", "min_p")).toBe(false);
		expect(isParamDisabled("nanogpt", "reasoning_effort")).toBe(true);
	});

	it("handles case-insensitive provider names", () => {
		expect(isParamDisabled("OpenAI", "min_p")).toBe(true);
		expect(isParamDisabled("GOOGLE", "frequency_penalty")).toBe(true);
	});
});

describe("isParamHidden", () => {
	it("returns true when isParamDisabled returns true", () => {
		expect(isParamHidden("openai", "min_p")).toBe(true);
		expect(isParamHidden("anthropic", "top_p")).toBe(true);
		expect(isParamHidden("google", "frequency_penalty")).toBe(true);
	});

	it("returns false when isParamDisabled returns false", () => {
		expect(isParamHidden("openai", "temperature")).toBe(false);
		expect(isParamHidden("openai", "top_p")).toBe(false);
		expect(isParamHidden("anthropic", "temperature")).toBe(false);
	});

	it("returns false for unknown providers", () => {
		expect(isParamHidden("unknown", "min_p")).toBe(false);
	});
});

describe("reasoning_effort incompatibility", () => {
	const providersWithReasoningEffortIncompatible = [
		"anthropic",
		"google",
		"cohere",
		"deepseek",
		"ollama",
		"ollama-cloud",
		"zai-coding",
		"koboldcpp",
		"lmstudio",
		"nanogpt",
		"openrouter",
		"opencode-zen",
		"opencode-go",
	];

	const providersWithReasoningEffortCompatible = ["openai", "xai"];

	it.each(
		providersWithReasoningEffortIncompatible,
	)("reasoning_effort is incompatible for %s", (provider) => {
		expect(isParamDisabled(provider, "reasoning_effort")).toBe(true);
		expect(isParamHidden(provider, "reasoning_effort")).toBe(true);
		expect(getParamIncompatibility(provider, "reasoning_effort")).toMatch(
			/^paramCompat\./,
		);
	});

	it.each(
		providersWithReasoningEffortCompatible,
	)("reasoning_effort is compatible for %s", (provider) => {
		expect(isParamDisabled(provider, "reasoning_effort")).toBe(false);
		expect(isParamHidden(provider, "reasoning_effort")).toBe(false);
		expect(getParamIncompatibility(provider, "reasoning_effort")).toBeNull();
	});
});
