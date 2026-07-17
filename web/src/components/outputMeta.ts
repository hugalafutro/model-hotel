/**
 * Pill metadata for non-text output modalities ("what this model produces").
 * Complements CAP_META, whose vision/audio/video pills describe *input*
 * capabilities. Kept in sync with the closed output vocabulary derived by
 * internal/provider/model_class.go.
 */
export interface OutputMeta {
	key: string;
	label: string;
	style: string;
}

export const OUTPUT_META: OutputMeta[] = [
	{
		key: "image",
		label: "Image out",
		style:
			"bg-fuchsia-900/40 text-fuchsia-300 border-fuchsia-700/50 shadow-[0_0_6px_1px_rgba(217,70,239,0.35)]",
	},
	{
		key: "audio",
		label: "Audio out",
		style:
			"bg-rose-900/40 text-rose-300 border-rose-700/50 shadow-[0_0_6px_1px_rgba(244,63,94,0.35)]",
	},
	{
		key: "video",
		label: "Video out",
		style:
			"bg-indigo-900/40 text-indigo-300 border-indigo-700/50 shadow-[0_0_6px_1px_rgba(99,102,241,0.35)]",
	},
	{
		key: "embedding",
		label: "Embedding",
		style:
			"bg-sky-900/40 text-sky-300 border-sky-700/50 shadow-[0_0_6px_1px_rgba(14,165,233,0.35)]",
	},
	{
		key: "rerank",
		label: "Rerank",
		style:
			"bg-lime-900/40 text-lime-300 border-lime-700/50 shadow-[0_0_6px_1px_rgba(132,204,22,0.35)]",
	},
];
