import { Monitor, Sparkles, Terminal } from "lucide-react";

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
	{ key: "model-hotel:nanogpt-usage", name: "NanoGPT" },
	{ key: "model-hotel:zai-coding-usage", name: "Z.ai Coding Plan" },
	{ key: "model-hotel:deepseek-balance", name: "DeepSeek" },
	{ key: "model-hotel:ollama-cloud-account", name: "Ollama Cloud" },
] as const;

export function getProviderCacheCount(): number {
	let count = 0;
	for (const entry of PROVIDER_CACHE_KEYS) {
		try {
			if (localStorage.getItem(entry.key) !== null) count++;
		} catch {
			/* ignore */
		}
	}
	return count;
}

export function getProviderCacheNames(): string[] {
	const names: string[] = [];
	for (const entry of PROVIDER_CACHE_KEYS) {
		try {
			if (localStorage.getItem(entry.key) !== null) names.push(entry.name);
		} catch {
			/* ignore */
		}
	}
	return names;
}

export function clearProviderCache() {
	for (const entry of PROVIDER_CACHE_KEYS) {
		try {
			localStorage.removeItem(entry.key);
		} catch {
			/* ignore */
		}
	}
}
