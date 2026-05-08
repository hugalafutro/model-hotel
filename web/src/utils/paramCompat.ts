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
		top_p: "top_p is deprecated on current Anthropic models; use top_k instead",
		frequency_penalty:
			"Anthropic uses a single penalties parameter, not separate frequency/presence",
		presence_penalty:
			"Anthropic uses a single penalties parameter, not separate frequency/presence",
		min_p: "Not supported by the Anthropic API",
	},
	google: {
		frequency_penalty: "Gemini does not support frequency/presence penalties",
		presence_penalty: "Gemini does not support frequency/presence penalties",
		top_k:
			"Gemini top_k behaves differently from OpenAI top_k; use top_p instead",
		min_p: "Not supported by the Google Gemini API",
	},
	cohere: {
		top_k: "Cohere uses a different 'k' parameter; not recommended",
		min_p: "Not supported by the Cohere API",
	},
	openai: {
		min_p: "Not part of the OpenAI API",
		top_k: "Not part of the OpenAI API",
	},
	deepseek: {
		min_p: "Not supported by the DeepSeek API",
		top_k: "Not supported by the DeepSeek API",
	},
	xai: {
		min_p: "Not supported by the xAI API",
		top_k: "Not supported by the xAI API",
	},
	ollama: {
		min_p: "Support varies by underlying model; not universally available",
	},
	"zai-coding": {
		min_p: "Not supported by z.ai Coding",
		top_k: "Not supported by z.ai Coding",
	},
	nanogpt: {},
	openrouter: {},
	"opencode-zen": {},
	"opencode-go": {},
	koboldcpp: {},
	lmstudio: {},
	custom: {},
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
