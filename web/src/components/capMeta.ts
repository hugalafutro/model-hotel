import type { ModelCapabilities } from "../api/types";

export type CapKey =
	| "vision"
	| "reasoning"
	| "tool_calling"
	| "structured_output"
	| "pdf_upload"
	| "video_input"
	| "audio_input"
	| "parallel_tool_calls";

export interface CapMeta {
	key: CapKey;
	/** i18n key for the pill label (models.capPills.<key>). */
	labelKey: string;
	style: string;
	muted: string;
}

/** Shared "this capability is not reachable under the current filters" look. */
export const CAP_DISABLED =
	"bg-gray-800/30 text-gray-600/40 border-gray-700/20 cursor-not-allowed opacity-50";

export const CAP_META: CapMeta[] = [
	{
		key: "vision",
		labelKey: "models.capPills.vision",
		style:
			"bg-purple-900/40 text-purple-300 border-purple-700/50 shadow-[0_0_6px_1px_rgba(147,51,234,0.35)]",
		muted:
			"bg-purple-900/15 text-purple-500/60 border-purple-700/25 hover:bg-purple-900/25 hover:text-purple-400",
	},
	{
		key: "reasoning",
		labelKey: "models.capPills.reasoning",
		style:
			"bg-amber-900/40 text-amber-300 border-amber-700/50 shadow-[0_0_6px_1px_rgba(245,158,11,0.35)]",
		muted:
			"bg-amber-900/15 text-amber-500/60 border-amber-700/25 hover:bg-amber-900/25 hover:text-amber-400",
	},
	{
		key: "tool_calling",
		labelKey: "models.capPills.tool_calling",
		style:
			"bg-cyan-900/40 text-cyan-300 border-cyan-700/50 shadow-[0_0_6px_1px_rgba(6,182,212,0.35)]",
		muted:
			"bg-cyan-900/15 text-cyan-500/60 border-cyan-700/25 hover:bg-cyan-900/25 hover:text-cyan-400",
	},
	{
		key: "structured_output",
		labelKey: "models.capPills.structured_output",
		style:
			"bg-emerald-900/40 text-emerald-300 border-emerald-700/50 shadow-[0_0_6px_1px_rgba(16,185,129,0.35)]",
		muted:
			"bg-emerald-900/15 text-emerald-500/60 border-emerald-700/25 hover:bg-emerald-900/25 hover:text-emerald-400",
	},
	{
		key: "pdf_upload",
		labelKey: "models.capPills.pdf_upload",
		style:
			"bg-red-900/40 text-red-300 border-red-700/50 shadow-[0_0_6px_1px_rgba(239,68,68,0.35)]",
		muted:
			"bg-red-900/15 text-red-500/60 border-red-700/25 hover:bg-red-900/25 hover:text-red-400",
	},
	{
		key: "video_input",
		labelKey: "models.capPills.video_input",
		style:
			"bg-pink-900/40 text-pink-300 border-pink-700/50 shadow-[0_0_6px_1px_rgba(236,72,153,0.35)]",
		muted:
			"bg-pink-900/15 text-pink-500/60 border-pink-700/25 hover:bg-pink-900/25 hover:text-pink-400",
	},
	{
		key: "audio_input",
		labelKey: "models.capPills.audio_input",
		style:
			"bg-orange-900/40 text-orange-300 border-orange-700/50 shadow-[0_0_6px_1px_rgba(249,115,22,0.35)]",
		muted:
			"bg-orange-900/15 text-orange-500/60 border-orange-700/25 hover:bg-orange-900/25 hover:text-orange-400",
	},
	{
		key: "parallel_tool_calls",
		labelKey: "models.capPills.parallel_tool_calls",
		style:
			"bg-teal-900/40 text-teal-300 border-teal-700/50 shadow-[0_0_6px_1px_rgba(20,184,166,0.35)]",
		muted:
			"bg-teal-900/15 text-teal-500/60 border-teal-700/25 hover:bg-teal-900/25 hover:text-teal-400",
	},
];

export function hasCap(caps: ModelCapabilities | null, key: CapKey): boolean {
	if (!caps) return false;
	return !!caps[key];
}

export function matchesAllCaps(
	caps: ModelCapabilities | null,
	keys: Set<CapKey>,
): boolean {
	if (keys.size === 0) return true;
	for (const k of keys) {
		if (!hasCap(caps, k)) return false;
	}
	return true;
}
