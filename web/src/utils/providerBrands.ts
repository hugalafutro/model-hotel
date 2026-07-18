/** Provider brand keys - union type for compile-time safety. */
export type ProviderBrand =
	| "anthropic"
	| "openai"
	| "google"
	| "deepseek"
	| "xai"
	| "ollama"
	| "ollama-cloud"
	| "openrouter"
	| "cohere"
	| "zai-coding"
	| "nanogpt"
	| "lmstudio"
	| "koboldcpp"
	| "opencode"
	| "neuralwatt"
	| "bedrock";

/**
 * Provider brand colors - single source of truth for consistent provider styling.
 *
 * Each key maps to a provider type used throughout the app
 * (matches `baseUrls` keys in Providers.tsx and `QuotaProviderType`).
 *
 * Hex values are the primary brand color; consumers compute
 * alpha variants (bg opacity, border opacity, hover opacity) as needed.
 */
export const PROVIDER_BRAND_COLORS: Record<ProviderBrand, string> = {
	anthropic: "#D97757",
	openai: "#000000",
	google: "#4285F4",
	deepseek: "#4D6BFE",
	xai: "#1A1A1A",
	ollama: "#3D3D3D",
	"ollama-cloud": "#3D3D3D",
	openrouter: "#6366F1",
	cohere: "#D4E7C5",
	"zai-coding": "#2D2D2D",
	nanogpt: "#0EA5B0",
	lmstudio: "#E879F9",
	koboldcpp: "#DC2626",
	opencode: "#2D2D2D",
	neuralwatt: "#ac4324",
	bedrock: "#FF9900",
} as const;

/** Short display prefixes for quota badges in the sidebar. */
export const PROVIDER_PREFIXES: Record<ProviderBrand, string> = {
	nanogpt: "NG",
	"zai-coding": "ZAI",
	deepseek: "DS",
	openrouter: "OR",
	anthropic: "AC",
	openai: "OA",
	google: "GEM",
	xai: "XAI",
	ollama: "OLL",
	"ollama-cloud": "OLC",
	cohere: "COH",
	lmstudio: "LM",
	koboldcpp: "KC",
	opencode: "OC",
	neuralwatt: "NW",
	bedrock: "AWS",
} as const;
