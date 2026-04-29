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
	label: string;
	style: string;
	muted: string;
	disabled: string;
}

export const CAP_META: CapMeta[] = [
	{
		key: "vision",
		label: "Vision",
		style:
			"bg-purple-900/40 text-purple-300 border-purple-700/50 shadow-[0_0_6px_1px_rgba(147,51,234,0.35)]",
		muted:
			"bg-purple-900/15 text-purple-500/60 border-purple-700/25 hover:bg-purple-900/25 hover:text-purple-400",
		disabled:
			"bg-gray-800/30 text-gray-600/40 border-gray-700/20 cursor-not-allowed opacity-50",
	},
	{
		key: "reasoning",
		label: "Reasoning",
		style:
			"bg-amber-900/40 text-amber-300 border-amber-700/50 shadow-[0_0_6px_1px_rgba(245,158,11,0.35)]",
		muted:
			"bg-amber-900/15 text-amber-500/60 border-amber-700/25 hover:bg-amber-900/25 hover:text-amber-400",
		disabled:
			"bg-gray-800/30 text-gray-600/40 border-gray-700/20 cursor-not-allowed opacity-50",
	},
	{
		key: "tool_calling",
		label: "Tools",
		style:
			"bg-cyan-900/40 text-cyan-300 border-cyan-700/50 shadow-[0_0_6px_1px_rgba(6,182,212,0.35)]",
		muted:
			"bg-cyan-900/15 text-cyan-500/60 border-cyan-700/25 hover:bg-cyan-900/25 hover:text-cyan-400",
		disabled:
			"bg-gray-800/30 text-gray-600/40 border-gray-700/20 cursor-not-allowed opacity-50",
	},
	{
		key: "structured_output",
		label: "Structured",
		style:
			"bg-emerald-900/40 text-emerald-300 border-emerald-700/50 shadow-[0_0_6px_1px_rgba(16,185,129,0.35)]",
		muted:
			"bg-emerald-900/15 text-emerald-500/60 border-emerald-700/25 hover:bg-emerald-900/25 hover:text-emerald-400",
		disabled:
			"bg-gray-800/30 text-gray-600/40 border-gray-700/20 cursor-not-allowed opacity-50",
	},
	{
		key: "pdf_upload",
		label: "PDF",
		style:
			"bg-red-900/40 text-red-300 border-red-700/50 shadow-[0_0_6px_1px_rgba(239,68,68,0.35)]",
		muted:
			"bg-red-900/15 text-red-500/60 border-red-700/25 hover:bg-red-900/25 hover:text-red-400",
		disabled:
			"bg-gray-800/30 text-gray-600/40 border-gray-700/20 cursor-not-allowed opacity-50",
	},
	{
		key: "video_input",
		label: "Video",
		style:
			"bg-pink-900/40 text-pink-300 border-pink-700/50 shadow-[0_0_6px_1px_rgba(236,72,153,0.35)]",
		muted:
			"bg-pink-900/15 text-pink-500/60 border-pink-700/25 hover:bg-pink-900/25 hover:text-pink-400",
		disabled:
			"bg-gray-800/30 text-gray-600/40 border-gray-700/20 cursor-not-allowed opacity-50",
	},
	{
		key: "audio_input",
		label: "Audio",
		style:
			"bg-orange-900/40 text-orange-300 border-orange-700/50 shadow-[0_0_6px_1px_rgba(249,115,22,0.35)]",
		muted:
			"bg-orange-900/15 text-orange-500/60 border-orange-700/25 hover:bg-orange-900/25 hover:text-orange-400",
		disabled:
			"bg-gray-800/30 text-gray-600/40 border-gray-700/20 cursor-not-allowed opacity-50",
	},
	{
		key: "parallel_tool_calls",
		label: "Parallel",
		style:
			"bg-teal-900/40 text-teal-300 border-teal-700/50 shadow-[0_0_6px_1px_rgba(20,184,166,0.35)]",
		muted:
			"bg-teal-900/15 text-teal-500/60 border-teal-700/25 hover:bg-teal-900/25 hover:text-teal-400",
		disabled:
			"bg-gray-800/30 text-gray-600/40 border-gray-700/20 cursor-not-allowed opacity-50",
	},
];

export function hasCap(caps: ModelCapabilities | null, key: CapKey): boolean {
	if (!caps) return false;
	return !!caps[key];
}
