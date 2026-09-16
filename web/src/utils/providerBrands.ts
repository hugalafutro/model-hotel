import { QUOTA_PREFIXES } from "@web-shared/quota";

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
	| "kimi-code"
	| "minimax"
	| "nanogpt"
	| "lmstudio"
	| "koboldcpp"
	| "opencode"
	| "opencode-go"
	| "neuralwatt"
	| "bedrock"
	| "azure"
	| "vertex-express";

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
	"kimi-code": "#2D2D2D",
	minimax: "#F23F5B",
	nanogpt: "#0EA5B0",
	lmstudio: "#E879F9",
	koboldcpp: "#DC2626",
	opencode: "#2D2D2D",
	"opencode-go": "#2D2D2D",
	neuralwatt: "#ac4324",
	bedrock: "#FF9900",
	azure: "#0078D4",
	"vertex-express": "#4285F4",
} as const;

/** Short display prefixes, one per brand. The quota providers take theirs from
 *  the shared map Front Desk renders its pills from, so the same provider keeps
 *  the same prefix in both apps; the rest are dashboard-only. */
export const PROVIDER_PREFIXES: Record<ProviderBrand, string> = {
	...QUOTA_PREFIXES,
	anthropic: "AC",
	openai: "OA",
	google: "GEM",
	xai: "XAI",
	ollama: "OLL",
	cohere: "COH",
	lmstudio: "LM",
	koboldcpp: "KC",
	opencode: "OC",
	bedrock: "AWS",
	azure: "AZ",
	"vertex-express": "VX",
} as const;
