import { describe, expect, it } from "vitest";
import type { GenerationParams } from "../../api/types";
import { recommendedSettings } from "../recommendedSettings";

const OPENAI = { temperature: 0.7, top_p: 1 };
const NUCLEUS = { temperature: 0.7, top_p: 0.9 };
const NUCLEUS_TOPK = { temperature: 0.7, top_p: 0.9, top_k: 40 };
const GEMINI = { temperature: 0.7, top_p: 0.95 };

describe("recommendedSettings curated families", () => {
	const cases: [modelId: string, expected: GenerationParams][] = [
		["gpt-4o", OPENAI],
		["gpt-4-turbo", OPENAI],
		["gpt-4", OPENAI],
		["gpt-3.5-turbo", OPENAI],
		["claude-4", NUCLEUS],
		["claude-3.5", NUCLEUS],
		["claude-3", NUCLEUS],
		["gemini-2.5", GEMINI],
		["gemini-2", GEMINI],
		["gemini-1.5", GEMINI],
		["gemini-pro", GEMINI],
		["llama-4", NUCLEUS_TOPK],
		["llama-3", NUCLEUS_TOPK],
		["llama-2", NUCLEUS_TOPK],
		["llama", NUCLEUS_TOPK],
		["mistral-large", NUCLEUS],
		["mistral-medium", NUCLEUS],
		["mistral-small", NUCLEUS],
		["mistral", NUCLEUS_TOPK],
		["deepseek-r1", NUCLEUS],
		["deepseek-v3", NUCLEUS],
		["deepseek-chat", NUCLEUS],
		["deepseek", NUCLEUS],
		["qwen3", NUCLEUS],
		["qwen2.5", NUCLEUS_TOPK],
		["qwen2", NUCLEUS_TOPK],
		["qwen", NUCLEUS_TOPK],
		["command-r-plus", NUCLEUS],
		["command-r", NUCLEUS],
		["command", NUCLEUS],
		["phi-3", NUCLEUS],
		["yi-large", NUCLEUS],
		["gemma-7b", NUCLEUS],
		["mixtral-8x7b", NUCLEUS_TOPK],
		["codestral", NUCLEUS],
		// Spaces, underscores, dots and case are normalised away.
		["Claude 3.5 Sonnet", NUCLEUS],
		["GPT 4o", OPENAI],
		["Llama_4_Maverick", NUCLEUS_TOPK],
		["Gemini-2.5 Pro", GEMINI],
		// Only the final path segment names the family.
		["OpenAI/gpt-4o", OPENAI],
		["OpenRouter/openai/gpt-4o", OPENAI],
	];

	it.each(cases)("%s", (modelId, expected) => {
		expect(recommendedSettings(modelId)).toEqual(expected);
	});

	it("does not share the curated object between calls", () => {
		const first = recommendedSettings("gpt-4o");
		if (first) first.temperature = 2;
		expect(recommendedSettings("gpt-4o")).toEqual(OPENAI);
	});
});

describe("recommendedSettings max_tokens from the stored max output", () => {
	it("caps a large stored max output at 4096", () => {
		expect(recommendedSettings("Test/unknown-model", 128000)).toEqual({
			max_tokens: 4096,
		});
	});

	it("uses a stored max output below the cap as is", () => {
		expect(recommendedSettings("Test/unknown-model", 2000)).toEqual({
			max_tokens: 2000,
		});
	});

	it("merges the max_tokens default into curated params", () => {
		expect(recommendedSettings("Anthropic/claude-3.5-sonnet", 8192)).toEqual({
			...NUCLEUS,
			max_tokens: 4096,
		});
	});

	it.each([undefined, null, 0])(
		"returns null with no curated family and stored max output %s",
		(max) => {
			expect(recommendedSettings("Test/unknown-model", max)).toBeNull();
		},
	);

	it("keeps curated params alone when there is no stored max output", () => {
		expect(recommendedSettings("deepseek-r1", null)).toEqual(NUCLEUS);
	});
});
