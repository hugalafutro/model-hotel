import type { GenerationParams } from "../api/types";

// ---------------------------------------------------------------------------
// models.dev types
// ---------------------------------------------------------------------------

interface ModelsDevCost {
	input?: number;
	output?: number;
	cache_read?: number;
}

interface ModelsDevLimit {
	context?: number;
	input?: number;
	output?: number;
}

interface ModelsDevModalities {
	input?: string[];
	output?: string[];
}

interface ModelsDevModel {
	id: string;
	name?: string;
	family?: string;
	attachment?: boolean;
	reasoning?: boolean;
	tool_call?: boolean;
	temperature?: boolean;
	structured_output?: boolean;
	knowledge?: string;
	release_date?: string;
	last_updated?: string;
	modalities?: ModelsDevModalities;
	open_weights?: boolean;
	cost?: ModelsDevCost;
	limit?: ModelsDevLimit;
}

interface ModelsDevProvider {
	id: string;
	name?: string;
	env?: string[];
	npm?: string;
	api?: string;
	doc?: string;
	models?: Record<string, ModelsDevModel>;
}

interface ModelsDevApi {
	[providerId: string]: ModelsDevProvider;
}

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

// ---------------------------------------------------------------------------
// Provider name normalisation for models.dev matching
// ---------------------------------------------------------------------------

const PROVIDER_ALIASES: Record<string, string | false> = {
	openai: "openai",
	anthropic: "anthropic",
	google: "google",
	"google-ai-studio": "google",
	gemini: "google",
	aistudio: "google",
	mistral: "mistral",
	deepseek: "deepseek",
	meta: "meta",
	cohere: "cohere",
	openrouter: false, // aggregator - skip direct matching
	together: "together",
	fireworks: "fireworks",
	groq: "groq",
};

function normalizeForMatch(s: string): string {
	return s.toLowerCase().replace(/[\s._-]+/g, "");
}

/**
 * Drop the provider prefix from a proxy model id, returning the bare model id.
 * proxyModelID builds "<providerName-with-spaces-as-dashes>/<model_id>", so the
 * prefix to remove is exactly that provider segment, and ONLY when present:
 * fetchRecommendedSettings is exported and callers may pass a bare model id
 * directly, including slashful models.dev-style ids like "deepseek-ai/DeepSeek-R1".
 * Stripping every leading slash would mangle those into "DeepSeek-R1" and make
 * the exact catalog entry unreachable. So strip only when modelId actually starts
 * with "<provider>/" (case-insensitively); otherwise leave the id untouched.
 */
function stripProviderPrefix(modelId: string, providerName: string): string {
	const prefix = `${providerName.replace(/ /g, "-")}/`;
	return modelId.toLowerCase().startsWith(prefix.toLowerCase())
		? modelId.slice(prefix.length)
		: modelId;
}

/**
 * The model family identifier (gpt-4o, llama-3, deepseek-r1) always lives in the
 * final path segment, so curated-pattern and family-bonus matching use just that
 * segment. For "openai/gpt-4o" this is "gpt-4o"; for a bare id it is the id.
 */
function modelFamilySegment(modelId: string): string {
	return modelId.slice(modelId.lastIndexOf("/") + 1);
}

// ---------------------------------------------------------------------------
// models.dev API fetch (no module-level cache - TanStack Query handles caching)
// ---------------------------------------------------------------------------

const MODELS_DEV_URL = "https://models.dev/api.json";

async function fetchModelsDevApi(): Promise<ModelsDevApi | null> {
	try {
		const controller = new AbortController();
		const timeoutId = setTimeout(() => controller.abort(), 10_000);
		const res = await fetch(MODELS_DEV_URL, { signal: controller.signal });
		clearTimeout(timeoutId);
		if (!res.ok) return null;
		return (await res.json()) as ModelsDevApi;
	} catch {
		return null;
	}
}

// ---------------------------------------------------------------------------
// Fuzzy model matching
// ---------------------------------------------------------------------------

interface ModelsDevMatch {
	model: ModelsDevModel;
	providerId: string;
	score: number;
}

/**
 * Try to find the best matching models.dev entry for a local model.
 * Returns the match and a score (higher = better).
 *
 * `modelId` must be the bare model id (no "<provider>/" prefix), though it may
 * keep an inner vendor segment (e.g. "openai/gpt-4o") so an exact catalog entry
 * for that full id can still match. Callers strip the provider via
 * stripProviderPrefix before calling.
 */
function findModelsDevMatch(
	api: ModelsDevApi,
	providerName: string,
	modelId: string,
): ModelsDevMatch | null {
	const normProvider = normalizeForMatch(providerName);
	const alias = PROVIDER_ALIASES[normProvider];
	const mappedProvider = alias === false ? null : (alias ?? normProvider);
	const normModel = normalizeForMatch(modelId);
	// Leading family token (e.g. "llama" from "llama-3"), taken from the final
	// path segment so an inner vendor prefix like "openai/" does not skew it.
	// Computed once: it does not vary across models.
	const searchFamilyToken = normalizeForMatch(
		modelFamilySegment(modelId).split(/[\s._-]/)[0],
	);

	let best: ModelsDevMatch | null = null;

	for (const [providerId, provider] of Object.entries(api)) {
		if (!provider.models) continue;
		const providerMatch =
			mappedProvider !== null &&
			normalizeForMatch(providerId) === mappedProvider;

		for (const [modelKey, model] of Object.entries(provider.models)) {
			const normKey = normalizeForMatch(modelKey);
			const normModelId = normalizeForMatch(model.id);

			let score: number;

			// Exact model ID match
			if (normKey === normModel || normModelId === normModel) {
				score = 100;
			}
			// Model ID contains our search term (e.g., "gpt-4o" in "gpt-4o-2024-08-06")
			else if (normKey.includes(normModel) || normModelId.includes(normModel)) {
				score = 80;
			}
			// Our search term contains the model ID (e.g., searching "gpt-4o" matches "gpt-4")
			else if (normModel.includes(normKey) || normModel.includes(normModelId)) {
				score = 60;
			}
			// No model match - skip
			else {
				continue;
			}

			// Bonus for matching provider
			if (providerMatch) score += 20;

			// Bonus for family match against the searched id's leading segment.
			// (normModel is separator-stripped, so the previous normModel.split("-")
			// never yielded a family token and this bonus could never fire.)
			if (
				model.family &&
				normalizeForMatch(model.family) === searchFamilyToken
			) {
				score += 5;
			}

			if (!best || score > best.score) {
				best = { model, providerId, score };
			}
		}
	}

	// Require at least 60 points (partial model match)
	return best && best.score >= 60 ? best : null;
}

// ---------------------------------------------------------------------------
// Public API
// ---------------------------------------------------------------------------

export interface RecommendedSettingsResult {
	/** Recommended generation params (null if no recommendation available) */
	params: GenerationParams | null;
	/** Source of the max_tokens value, if any */
	maxTokensSource: "models.dev" | "curated" | null;
	/** The matched models.dev provider ID */
	matchedProviderId: string | null;
	/** The matched models.dev model ID */
	matchedModelId: string | null;
}

/**
 * Fetch recommended settings for a model. Combines:
 * 1. Curated settings from the RECOMMENDED_SETTINGS table (by model family)
 * 2. models.dev metadata (max_tokens from output limits, capability flags)
 */
export async function fetchRecommendedSettings(
	modelId: string,
	providerName: string,
): Promise<RecommendedSettingsResult> {
	const result: RecommendedSettingsResult = {
		params: null,
		maxTokensSource: null,
		matchedProviderId: null,
		matchedModelId: null,
	};

	// Callers pass proxy ids like "OpenAI/gpt-4o" (provider arrives separately as
	// providerName). Strip the provider for models.dev matching, keeping any inner
	// vendor segment so an exact catalog entry (e.g. "deepseek-ai/DeepSeek-R1")
	// still matches. Curated patterns are written against the family name, which
	// is the final segment, so match those against it: "OpenRouter/openai/gpt-4o"
	// -> "gpt-4o" keeps its curated GPT-4o defaults.
	const bareModelId = stripProviderPrefix(modelId, providerName);

	// 1. Match curated settings by model family (final segment of the id)
	const normFamily = normalizeForMatch(modelFamilySegment(bareModelId));
	let curatedParams: GenerationParams | null = null;

	for (const [pattern, params] of RECOMMENDED_SETTINGS) {
		if (normFamily.startsWith(normalizeForMatch(pattern))) {
			curatedParams = { ...params };
			break;
		}
	}

	// 2. Fetch models.dev for limits
	const api = await fetchModelsDevApi();
	let modelsDevMaxTokens: number | undefined;
	let matchedProviderId: string | null = null;
	let matchedModelId: string | null = null;

	if (api) {
		const match = findModelsDevMatch(api, providerName, bareModelId);
		if (match) {
			matchedProviderId = match.providerId;
			matchedModelId = match.model.id;
			if (match.model.limit?.output) {
				// Cap at a sensible default, not the model's absolute ceiling
				modelsDevMaxTokens = Math.min(match.model.limit.output, 4096);
			}
		}
	}

	// 3. Merge results
	if (curatedParams || modelsDevMaxTokens !== undefined) {
		result.params = { ...curatedParams };
		result.matchedProviderId = matchedProviderId;
		result.matchedModelId = matchedModelId;

		// Use models.dev output limit (capped) for max_tokens if available
		if (modelsDevMaxTokens !== undefined) {
			result.params.max_tokens = modelsDevMaxTokens;
			result.maxTokensSource = "models.dev";
		}

		return result;
	}

	// No curated match and no models.dev limit: the first branch already handles
	// the models.dev-only case (curatedParams null -> params starts as {} and
	// max_tokens is filled in there), so nothing is left to set here.
	return result;
}
