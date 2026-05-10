import { Monitor, Sparkles, Terminal } from "lucide-react";

export const DISCOVERY_INTERVALS = [
	{ value: "30m", label: "30 minutes" },
	{ value: "1h", label: "1 hour" },
	{ value: "6h", label: "6 hours" },
	{ value: "12h", label: "12 hours" },
	{ value: "24h", label: "24 hours" },
	{ value: "0", label: "Disabled" },
];

export const UI_STYLES = [
	{
		id: "clean-saas" as const,
		label: "Clean SaaS",
		description: "Refined, professional, minimal",
		icon: Monitor,
	},
	{
		id: "cyber-terminal" as const,
		label: "Cyber Terminal",
		description: "Developer-centric, high-contrast",
		icon: Terminal,
	},
	{
		id: "glassmorphism-lite" as const,
		label: "Glassmorphism",
		description: "Slick, translucent surfaces",
		icon: Sparkles,
	},
];

const PROVIDER_CACHE_KEYS = [
	"model-hotel:nanogpt-usage",
	"model-hotel:zai-coding-usage",
	"model-hotel:deepseek-balance",
	"model-hotel:ollama-cloud-account",
];

export function getProviderCacheCount(): number {
	let count = 0;
	for (const key of PROVIDER_CACHE_KEYS) {
		try {
			if (localStorage.getItem(key) !== null) count++;
		} catch {
			/* ignore */
		}
	}
	return count;
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
