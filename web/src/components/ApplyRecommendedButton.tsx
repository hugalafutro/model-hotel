import { Loader2, Sparkles } from "lucide-react";
import type { GenerationParams } from "../api/types";
import { useRecommendedSettings } from "../hooks/useRecommendedSettings";

export function ApplyRecommendedButton({
	modelId,
	providerName,
	onApply,
}: {
	modelId: string;
	providerName: string;
	onApply: (recommended: GenerationParams) => void;
}) {
	const { recommended, loading, matchedModel } = useRecommendedSettings(
		modelId,
		providerName,
	);
	const hasRecommended = recommended !== null;
	const paramCount = recommended
		? Object.values(recommended).filter((v) => v !== undefined).length
		: 0;

	return (
		<button
			type="button"
			disabled={!hasRecommended || loading}
			onClick={() => {
				if (recommended) onApply(recommended);
			}}
			className={`w-full flex items-center justify-center gap-1.5 px-3 py-1.5 rounded text-xs font-medium transition-colors cursor-pointer border ${
				hasRecommended && !loading
					? "border-(--accent)/30 bg-(--accent)/10 text-(--accent) hover:bg-(--accent)/20 hover:border-(--accent)/50"
					: "border-(--border-subtle) bg-(--surface-hover)/50 text-(--text-muted) cursor-not-allowed"
			}`}
		>
			{loading ? (
				<>
					<Loader2 size={12} className="animate-spin" />
					Loading…
				</>
			) : hasRecommended ? (
				<>
					<Sparkles size={12} />
					Apply Recommended
					<span className="text-[10px] opacity-70">({paramCount} params)</span>
					{matchedModel && matchedModel !== modelId && (
						<span
							className="text-[10px] opacity-60"
							title={`Matched: ${matchedModel}`}
						>
							↗ {matchedModel}
						</span>
					)}
				</>
			) : (
				<>
					<Sparkles size={12} className="opacity-50" />
					No recommendations available
				</>
			)}
		</button>
	);
}
