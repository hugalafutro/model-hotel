import type { GenerationParams } from "../api/types";

/**
 * Maps provider type → param key → human-readable reason the param is incompatible.
 * Provider type keys must match the output of DetectProviderType() (backend) and ProviderBrand keys.
 */
export const PROVIDER_PARAM_INCOMPATIBILITY: Record<
	string,
	Partial<Record<keyof GenerationParams, string>>
> = {
	anthropic: {
		top_p: "paramCompat.anthropic.topP",
		frequency_penalty: "paramCompat.anthropic.frequencyPenalty",
		presence_penalty: "paramCompat.anthropic.presencePenalty",
		min_p: "paramCompat.anthropic.minP",
		reasoning_effort: "paramCompat.anthropic.reasoningEffort",
	},
	google: {
		frequency_penalty: "paramCompat.google.frequencyPenalty",
		presence_penalty: "paramCompat.google.presencePenalty",
		top_k: "paramCompat.google.topK",
		min_p: "paramCompat.google.minP",
		reasoning_effort: "paramCompat.google.reasoningEffort",
	},
	cohere: {
		top_k: "paramCompat.cohere.topK",
		min_p: "paramCompat.cohere.minP",
		reasoning_effort: "paramCompat.cohere.reasoningEffort",
	},
	openai: {
		min_p: "paramCompat.openai.minP",
		top_k: "paramCompat.openai.topK",
	},
	deepseek: {
		min_p: "paramCompat.deepseek.minP",
		top_k: "paramCompat.deepseek.topK",
		reasoning_effort: "paramCompat.deepseek.reasoningEffort",
	},
	xai: {
		min_p: "paramCompat.xai.minP",
		top_k: "paramCompat.xai.topK",
	},
	ollama: {
		min_p: "paramCompat.ollama.minP",
		reasoning_effort: "paramCompat.ollama.reasoningEffort",
	},
	"ollama-cloud": {
		min_p: "paramCompat.ollamaCloud.minP",
		reasoning_effort: "paramCompat.ollamaCloud.reasoningEffort",
	},
	"zai-coding": {
		min_p: "paramCompat.zaiCoding.minP",
		top_k: "paramCompat.zaiCoding.topK",
		reasoning_effort: "paramCompat.zaiCoding.reasoningEffort",
	},
	nanogpt: {
		reasoning_effort: "paramCompat.nanogpt.reasoningEffort",
	},
	openrouter: {
		reasoning_effort: "paramCompat.openrouter.reasoningEffort",
	},
	"opencode-zen": {
		reasoning_effort: "paramCompat.opencodeZen.reasoningEffort",
	},
	"opencode-go": {
		reasoning_effort: "paramCompat.opencodeGo.reasoningEffort",
	},
	koboldcpp: {
		reasoning_effort: "paramCompat.koboldcpp.reasoningEffort",
	},
	lmstudio: {
		reasoning_effort: "paramCompat.lmstudio.reasoningEffort",
	},
	custom: {
		reasoning_effort: "paramCompat.custom.reasoningEffort",
	},
};

/**
 * Normalizes a user-facing provider name to the canonical provider type key
 * used by PROVIDER_PARAM_INCOMPATIBILITY and backend DetectProviderType().
 *
 * Handles common cases like "OpenAI" → "openai", "Anthropic Pro" → "anthropic",
 * "Google AI Studio (Gemini)" → "google", etc.
 */
export function normalizeToProviderType(providerName: string): string {
	if (!providerName) return "openai"; // fallback

	// Direct match (already a type key)
	if (providerName in PROVIDER_PARAM_INCOMPATIBILITY) {
		return providerName;
	}

	// Case-insensitive match against known type keys
	const lower = providerName.toLowerCase().replace(/\s+/g, "-");
	for (const key of Object.keys(PROVIDER_PARAM_INCOMPATIBILITY)) {
		if (key === lower) return key;
	}

	// Substring heuristic: check if the provider name contains a known type
	const typePatterns: Record<string, string[]> = {
		anthropic: ["anthropic"],
		openai: ["openai"],
		google: ["google", "gemini", "generativelanguage"],
		deepseek: ["deepseek"],
		xai: ["xai", "x.ai", "grok"],
		ollama: ["ollama"],
		"ollama-cloud": ["ollama-cloud", "ollama cloud"],
		openrouter: ["openrouter"],
		cohere: ["cohere"],
		"zai-coding": ["z.ai", "zai", "z-ai"],
		nanogpt: ["nanogpt", "nano-gpt", "nano-gpt"],
		lmstudio: ["lmstudio", "lm-studio", "lm studio"],
		koboldcpp: ["koboldcpp", "kobold"],
		"opencode-zen": ["opencode-zen", "opencode zen"],
		"opencode-go": ["opencode-go", "opencode go"],
	};

	for (const [typeKey, patterns] of Object.entries(typePatterns)) {
		for (const pattern of patterns) {
			if (lower.includes(pattern)) return typeKey;
		}
	}

	// Unknown provider - return the lowercased name as-is (will match no incompatibility rules)
	return lower;
}

/**
 * Returns the incompatibility reason for a param on a given provider,
 * or null if the param is compatible.
 */
export function getParamIncompatibility(
	providerName: string,
	paramKey: keyof GenerationParams,
): string | null {
	const providerType = normalizeToProviderType(providerName);
	const rules = PROVIDER_PARAM_INCOMPATIBILITY[providerType];
	if (!rules || !(paramKey in rules)) return null;
	const reason = rules[paramKey];
	return reason || null; // empty string means no incompatibility
}

/**
 * Returns true if the param should be disabled (greyed out) for the given provider.
 */
export function isParamDisabled(
	providerName: string,
	paramKey: keyof GenerationParams,
): boolean {
	return getParamIncompatibility(providerName, paramKey) !== null;
}

/**
 * Returns true if the param should be hidden (not shown at all) for the given provider.
 * Currently, all incompatible params are hidden instead of disabled.
 */
export function isParamHidden(
	providerName: string,
	paramKey: keyof GenerationParams,
): boolean {
	return isParamDisabled(providerName, paramKey);
}
