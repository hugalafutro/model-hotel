import type { QuotaProviderType } from "./types";

// The two presentation values both frontends agree on: how a window percentage
// reads as text, and the short prefix a pill puts in front of it. Everything
// else about a pill (brand colour, shell, translation namespace) is the
// rendering app's own.

/**
 * One window percentage as a label, or "-" when the window is not reported.
 * `pct` is always the percent USED; remaining mode inverts it.
 */
export function windowPct(
	pct: number | undefined | null,
	mode: "used" | "remaining",
): string {
	if (pct == null) return "-";
	// Bounded: a window consumed past its cap is 100% used, 0% remaining, not
	// "105%" and "-5%".
	const used = Math.min(Math.max(pct, 0), 100);
	return `${(mode === "remaining" ? 100 - used : used).toFixed(0)}%`;
}

/** Short pill prefixes for the quota providers, identical in both apps. */
export const QUOTA_PREFIXES: Record<QuotaProviderType, string> = {
	nanogpt: "NG",
	"zai-coding": "ZAI",
	"kimi-code": "KIMI",
	minimax: "MMX",
	deepseek: "DS",
	openrouter: "OR",
	"ollama-cloud": "OLC",
	neuralwatt: "NW",
	"opencode-go": "OCG",
};
