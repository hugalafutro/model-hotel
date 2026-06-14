import { useTranslation } from "react-i18next";
import { Loader2, Sparkles } from "@/lib/icons";
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
	const { t } = useTranslation();
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
			className={`w-full flex items-center justify-center gap-1.5 px-3 py-1.5 rounded-(--radius-button) text-xs font-medium transition-colors border ${
				hasRecommended && !loading
					? "border-(--accent)/30 bg-(--accent)/10 text-(--accent) hover:bg-(--accent)/20 hover:border-(--accent)/50"
					: "border-(--border-subtle) bg-(--surface-hover)/50 text-(--text-muted) cursor-not-allowed"
			}`}
			title={
				hasRecommended
					? t("components.applyRecommendedButton.tooltip")
					: undefined
			}
		>
			{loading ? (
				<>
					<Loader2 size={12} className="animate-spin" />
					{t("common.loadingDots")}
				</>
			) : hasRecommended ? (
				<>
					<Sparkles size={12} />
					{t("components.applyRecommendedButton.applyRecommended")}
					<span className="text-[10px] opacity-70 whitespace-nowrap">
						{t("components.applyRecommendedButton.params", {
							count: paramCount,
						})}
					</span>
					{matchedModel && matchedModel !== modelId && (
						<span
							className="text-[10px] opacity-60 whitespace-nowrap shrink-0 inline-flex items-center gap-0.5 rounded bg-(--surface-hover) px-1 py-px"
							title={t("components.applyRecommendedButton.modelsDevMatched", {
								model: matchedModel,
							})}
						>
							↗&#x200A;{matchedModel}
						</span>
					)}
				</>
			) : (
				<>
					<Sparkles size={12} className="opacity-50" />
					{t("components.applyRecommendedButton.noRecommendationsAvailable")}
				</>
			)}
		</button>
	);
}
