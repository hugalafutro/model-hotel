import { Monitor, Sparkles, Terminal } from "@/lib/icons";

// The three UI styles the Appearance card offers. i18nKey is the stem under
// settings.appearance.uiStyles.* holding the name and its description.
export const UI_STYLES = [
	{
		id: "clean-saas" as const,
		i18nKey: "cleanSaas" as const,
		icon: Monitor,
	},
	{
		id: "cyber-terminal" as const,
		i18nKey: "cyberTerminal" as const,
		icon: Terminal,
	},
	{
		id: "glassmorphism-lite" as const,
		i18nKey: "glassmorphism" as const,
		icon: Sparkles,
	},
];

const PROVIDER_CACHE_KEYS = [
	"model-hotel:nanogpt-usage",
	"model-hotel:zai-coding-usage",
	"model-hotel:kimi-code-usage",
	"model-hotel:minimax-usage",
	"model-hotel:deepseek-balance",
	"model-hotel:ollama-cloud-account",
] as const;

function hasCacheKey(key: string): boolean {
	try {
		return localStorage.getItem(key) !== null;
	} catch {
		return false;
	}
}

export function getProviderCacheCount(): number {
	return PROVIDER_CACHE_KEYS.filter(hasCacheKey).length;
}

export function clearProviderCache() {
	for (const key of PROVIDER_CACHE_KEYS) {
		try {
			localStorage.removeItem(key);
		} catch {
			/* ignore */
		}
	}
}
