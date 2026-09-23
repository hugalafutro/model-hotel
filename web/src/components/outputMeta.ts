import type { Icon } from "@phosphor-icons/react";
import {
	FileText,
	Hash,
	Image,
	ListOrdered,
	TextAa,
	VideoCamera,
	Volume2,
} from "@/lib/icons";

/**
 * Metadata for the output modalities ("what this model produces"), text
 * included. Complements CAP_META, whose vision/audio/video pills describe
 * *input* capabilities. Kept in sync with the closed output vocabulary derived
 * by internal/provider/model_class.go.
 */
export interface OutputMeta {
	key: string;
	icon: Icon;
	/** i18n key for the pill label (models.outputPills.<key>). */
	labelKey: string;
	style: string;
	muted: string;
}

export const OUTPUT_META: OutputMeta[] = [
	{
		key: "text",
		icon: TextAa,
		labelKey: "models.outputPills.text",
		style:
			"bg-slate-700/40 text-slate-200 border-slate-500/50 shadow-[0_0_6px_1px_rgba(148,163,184,0.35)]",
		muted:
			"bg-slate-800/15 text-slate-400/60 border-slate-600/25 hover:bg-slate-800/25 hover:text-slate-300",
	},
	{
		key: "image",
		icon: Image,
		labelKey: "models.outputPills.image",
		style:
			"bg-fuchsia-900/40 text-fuchsia-300 border-fuchsia-700/50 shadow-[0_0_6px_1px_rgba(217,70,239,0.35)]",
		muted:
			"bg-fuchsia-900/15 text-fuchsia-500/60 border-fuchsia-700/25 hover:bg-fuchsia-900/25 hover:text-fuchsia-400",
	},
	{
		key: "audio",
		icon: Volume2,
		labelKey: "models.outputPills.audio",
		style:
			"bg-rose-900/40 text-rose-300 border-rose-700/50 shadow-[0_0_6px_1px_rgba(244,63,94,0.35)]",
		muted:
			"bg-rose-900/15 text-rose-500/60 border-rose-700/25 hover:bg-rose-900/25 hover:text-rose-400",
	},
	{
		key: "video",
		icon: VideoCamera,
		labelKey: "models.outputPills.video",
		style:
			"bg-indigo-900/40 text-indigo-300 border-indigo-700/50 shadow-[0_0_6px_1px_rgba(99,102,241,0.35)]",
		muted:
			"bg-indigo-900/15 text-indigo-500/60 border-indigo-700/25 hover:bg-indigo-900/25 hover:text-indigo-400",
	},
	{
		key: "pdf",
		icon: FileText,
		labelKey: "models.outputPills.pdf",
		style:
			"bg-red-900/40 text-red-300 border-red-700/50 shadow-[0_0_6px_1px_rgba(239,68,68,0.35)]",
		muted:
			"bg-red-900/15 text-red-500/60 border-red-700/25 hover:bg-red-900/25 hover:text-red-400",
	},
	{
		key: "embedding",
		icon: Hash,
		labelKey: "models.outputPills.embedding",
		style:
			"bg-sky-900/40 text-sky-300 border-sky-700/50 shadow-[0_0_6px_1px_rgba(14,165,233,0.35)]",
		muted:
			"bg-sky-900/15 text-sky-500/60 border-sky-700/25 hover:bg-sky-900/25 hover:text-sky-400",
	},
	{
		key: "rerank",
		icon: ListOrdered,
		labelKey: "models.outputPills.rerank",
		style:
			"bg-lime-900/40 text-lime-300 border-lime-700/50 shadow-[0_0_6px_1px_rgba(132,204,22,0.35)]",
		muted:
			"bg-lime-900/15 text-lime-500/60 border-lime-700/25 hover:bg-lime-900/25 hover:text-lime-400",
	},
];
