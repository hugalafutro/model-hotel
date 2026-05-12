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
