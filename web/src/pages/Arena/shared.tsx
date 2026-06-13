import { Settings, ThumbsDown, ThumbsUp } from "lucide-react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import type { GenerationParams } from "../../api/types";

export function VoteThumb({
	size,
	isWinner,
	animating,
}: {
	size: number;
	isWinner: boolean;
	animating: boolean;
}) {
	const [showUp, setShowUp] = useState(false);

	useEffect(() => {
		if (!animating) return;
		const id = setInterval(() => setShowUp((v) => !v), 1200);
		return () => clearInterval(id);
	}, [animating]);

	if (isWinner) return <ThumbsUp size={size} />;
	if (!animating) return <ThumbsDown size={size} />;

	return (
		<span
			className="relative inline-flex"
			style={{ width: size, height: size }}
		>
			<ThumbsDown
				size={size}
				className={`absolute inset-0 transition-opacity duration-500 ${showUp ? "opacity-0" : "opacity-100"}`}
			/>
			<ThumbsUp
				size={size}
				className={`absolute inset-0 transition-opacity duration-500 ${showUp ? "opacity-100" : "opacity-0"}`}
			/>
		</span>
	);
}

export function SlotParamsTooltip({ params }: { params?: GenerationParams }) {
	if (!params) return null;
	const entries = Object.entries(params).filter(([, v]) => v !== undefined);
	if (entries.length === 0) return null;
	const lines = entries
		.map(([k, v]) => {
			const label = k.replace(/_/g, " ").replace(/^\w/, (c) => c.toUpperCase());
			return `${label}: ${v}`;
		})
		.join("\n");
	return (
		<span className="shrink-0 text-(--accent) cursor-help" title={lines}>
			<Settings size={10} />
		</span>
	);
}

export function BracketPreviewPill({
	modelId,
	displayName,
	isTbd = false,
}: {
	modelId: string;
	displayName?: string;
	isTbd?: boolean;
}) {
	const { t } = useTranslation();
	if (isTbd || !modelId) {
		return (
			<div className="px-3 py-2 rounded-lg border border-dashed border-(--border-subtle) bg-(--surface) text-xs text-(--text-tertiary) min-w-24 text-center">
				{t("arena.tbd")}
			</div>
		);
	}
	return (
		<div
			className="px-3 py-2 rounded-lg border bg-(--accent)/15 border-(--accent)/40 text-(--accent) text-xs font-medium truncate max-w-40"
			title={displayName || modelId}
		>
			{displayName || modelId.split("/").pop()}
		</div>
	);
}
