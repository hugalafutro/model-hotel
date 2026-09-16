import {
	clearCachedData,
	hasCachedData,
	QUOTA_QUERY_KEYS,
} from "@/hooks/useQuotaData";
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

// Every provider payload useQuotaData mirrors into localStorage lives under
// one of its query keys, so "clear provider cache" leaves none behind and the
// count is the whole set.
export function getProviderCacheCount(): number {
	return QUOTA_QUERY_KEYS.filter((key) => hasCachedData(key)).length;
}

export function clearProviderCache() {
	for (const key of QUOTA_QUERY_KEYS) {
		clearCachedData(key);
	}
}
