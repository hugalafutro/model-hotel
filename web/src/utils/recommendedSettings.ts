import type { GenerationParams } from "../api/types";
// The model family identifier (gpt-4o, llama-3, deepseek-r1) always lives in the
// final path segment, so curated-pattern matching uses just that segment, which
// is what shortModelName returns.
import { shortModelName } from "./model";

// ---------------------------------------------------------------------------
// Curated recommended settings by model family
// ---------------------------------------------------------------------------

/**
 * Pattern → recommended GenerationParams.  Patterns are matched against the
 * normalised model ID (lowercased) using startsWith.  More specific (longer)
 * patterns should come first so they win over shorter generic ones.
 */
const RECOMMENDED_SETTINGS: [pattern: string, params: GenerationParams][] = [
	// OpenAI GPT-4 family
	["gpt-4o", { temperature: 0.7, top_p: 1 }],
	["gpt-4-turbo", { temperature: 0.7, top_p: 1 }],
	["gpt-4", { temperature: 0.7, top_p: 1 }],
	["gpt-3.5-turbo", { temperature: 0.7, top_p: 1 }],

	// Anthropic Claude 3.5 / 4 family
	["claude-4", { temperature: 0.7, top_p: 0.9 }],
	["claude-3.5", { temperature: 0.7, top_p: 0.9 }],
	["claude-3", { temperature: 0.7, top_p: 0.9 }],

	// Google Gemini
	["gemini-2.5", { temperature: 0.7, top_p: 0.95 }],
	["gemini-2", { temperature: 0.7, top_p: 0.95 }],
	["gemini-1.5", { temperature: 0.7, top_p: 0.95 }],
	["gemini", { temperature: 0.7, top_p: 0.95 }],

	// Meta Llama
	["llama-4", { temperature: 0.7, top_p: 0.9, top_k: 40 }],
	["llama-3", { temperature: 0.7, top_p: 0.9, top_k: 40 }],
	["llama-2", { temperature: 0.7, top_p: 0.9, top_k: 40 }],
	["llama", { temperature: 0.7, top_p: 0.9, top_k: 40 }],

	// Mistral
	["mistral-large", { temperature: 0.7, top_p: 0.9 }],
	["mistral-medium", { temperature: 0.7, top_p: 0.9 }],
	["mistral-small", { temperature: 0.7, top_p: 0.9 }],
	["mistral", { temperature: 0.7, top_p: 0.9, top_k: 40 }],

	// DeepSeek
	["deepseek-r1", { temperature: 0.7, top_p: 0.9 }],
	["deepseek-v3", { temperature: 0.7, top_p: 0.9 }],
	["deepseek-chat", { temperature: 0.7, top_p: 0.9 }],
	["deepseek", { temperature: 0.7, top_p: 0.9 }],

	// Qwen
	["qwen3", { temperature: 0.7, top_p: 0.9 }],
	["qwen2.5", { temperature: 0.7, top_p: 0.9, top_k: 40 }],
	["qwen2", { temperature: 0.7, top_p: 0.9, top_k: 40 }],
	["qwen", { temperature: 0.7, top_p: 0.9, top_k: 40 }],

	// Cohere
	["command-r-plus", { temperature: 0.7, top_p: 0.9 }],
	["command-r", { temperature: 0.7, top_p: 0.9 }],
	["command", { temperature: 0.7, top_p: 0.9 }],

	// Generic open-weights catch-all
	["phi-", { temperature: 0.7, top_p: 0.9 }],
	["yi-", { temperature: 0.7, top_p: 0.9 }],
	["gemma", { temperature: 0.7, top_p: 0.9 }],
	["mixtral", { temperature: 0.7, top_p: 0.9, top_k: 40 }],
	["codestral", { temperature: 0.7, top_p: 0.9 }],
];

function normalizeForMatch(s: string): string {
	return s.toLowerCase().replace(/[\s._-]+/g, "");
}

/** The max_tokens default: the model's own output ceiling, capped here. */
const MAX_TOKENS_CAP = 4096;

/**
 * Recommended settings for a model. Combines the curated RECOMMENDED_SETTINGS
 * entry for the model's family with a max_tokens default taken from the
 * model's stored max output (filled by discovery or pinned by an operator),
 * capped at MAX_TOKENS_CAP. Returns null when neither source has anything.
 *
 * @param modelId - The proxy model id (e.g. "OpenAI/gpt-4o"); only its final
 *   segment is matched against the curated patterns.
 * @param maxOutputTokens - The model's stored max output, if known.
 */
export function recommendedSettings(
	modelId: string,
	maxOutputTokens?: number | null,
): GenerationParams | null {
	const normFamily = normalizeForMatch(shortModelName(modelId));
	const curated = RECOMMENDED_SETTINGS.find(([pattern]) =>
		normFamily.startsWith(normalizeForMatch(pattern)),
	);
	const hasLimit = maxOutputTokens != null && maxOutputTokens > 0;
	if (!curated && !hasLimit) return null;

	const params: GenerationParams = { ...curated?.[1] };
	if (hasLimit) params.max_tokens = Math.min(maxOutputTokens, MAX_TOKENS_CAP);
	return params;
}
