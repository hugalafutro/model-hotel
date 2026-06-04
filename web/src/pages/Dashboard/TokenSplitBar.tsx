import { Target } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Spinner } from "../../components/Spinner";
import { formatPercent } from "../../utils/format";
import { RangeToggle } from "./ToggleGroup";
import { computeTileSegments } from "./tokenTileUtils";
import type { Range } from "./types";

const PROMPT_COLOR = "#818cf8";
const COMPLETION_COLOR = "#059669";
const CACHE_HIT_COLOR = "var(--accent)";

export function TokenSplitBar({
	prompt,
	completion,
	cacheHit,
	total,
	range,
	onRangeChange,
	loading,
}: {
	/** Prompt token count. */
	prompt: number;
	/** Completion token count. */
	completion: number;
	/** Cache hit token count (subset of prompt). */
	cacheHit: number;
	/** Total tokens displayed in the header. Should equal prompt + completion for consistent display. */
	total: number;
	range: Range;
	onRangeChange: (r: Range) => void;
	loading?: boolean;
}) {
	const { t } = useTranslation();
	const totalPC = prompt + completion;
	if (totalPC === 0) {
		return (
			<div className="ui-card p-6">
				<div className="flex items-center justify-between mb-1">
					<h3 className="text-lg font-semibold text-(--text-primary) flex items-center gap-2">
						<Target size={18} className="text-(--accent)" />
						{t("dashboard.tokens.tokenMix")}
						{loading && <Spinner className="ml-1" />}
					</h3>
					<RangeToggle value={range} onChange={onRangeChange} />
				</div>
				<p className="text-sm text-(--text-muted) text-center py-12">
					{t("dashboard.tokens.noTokenData")}
				</p>
			</div>
		);
	}
	const promptPct = (prompt / totalPC) * 100;
	const completionPct = (completion / totalPC) * 100;
	const cacheHitPct = (cacheHit / totalPC) * 100;
	const tiles = computeTileSegments(promptPct, completionPct, cacheHitPct);

	const uncachedPrompt = Math.max(0, prompt - cacheHit);
	const uncachedPct = totalPC > 0 ? (uncachedPrompt / totalPC) * 100 : 0;

	const tileColor = (type: string) => {
		if (type === "cache_hit") return CACHE_HIT_COLOR;
		if (type === "prompt") return PROMPT_COLOR;
		return COMPLETION_COLOR;
	};

	return (
		<div className="ui-card p-6">
			<div className="flex items-center justify-between mb-1">
				<h3 className="text-lg font-semibold text-(--text-primary) flex items-center gap-2">
					<Target size={18} className="text-(--accent)" />
					{t("dashboard.tokens.tokenMix")}
					{loading && <Spinner className="ml-1" />}
				</h3>
				<RangeToggle value={range} onChange={onRangeChange} />
			</div>
			<p
				className="text-2xl font-bold text-(--text-primary) mb-4"
				style={{ textTransform: "none" }}
			>
				{total.toLocaleString()}{" "}
				<span className="text-sm font-normal text-(--text-muted)">
					{t("dashboard.tokens.tokens")}
				</span>
			</p>
			<div
				className="flex gap-0.5 h-6"
				role="img"
				aria-label={t("dashboard.tokens.mixAriaLabel", {
					promptPct: promptPct.toFixed(1),
					completionPct: completionPct.toFixed(1),
				})}
			>
				{tiles.map((tile, i) => (
					<div
						// biome-ignore lint/suspicious/noArrayIndexKey: static tile grid, never reordered
						key={`${tile.type}-${i}`}
						className="flex-1 rounded-sm animate-waffle-pop"
						data-tile-type={tile.type}
						style={{
							backgroundColor: tileColor(tile.type),
							opacity: tile.opacity,
							animationDelay: `${i * 20}ms`,
						}}
						title={
							tile.type === "cache_hit"
								? t("dashboard.tokens.cacheHitTooltip", {
										pct: cacheHitPct.toFixed(1),
										count: cacheHit.toLocaleString(),
									})
								: tile.type === "prompt"
									? t("dashboard.tokens.promptTooltip", {
											pct: uncachedPct.toFixed(1),
											count: uncachedPrompt.toLocaleString(),
										})
									: t("dashboard.tokens.completionTooltip", {
											pct: completionPct.toFixed(1),
											count: completion.toLocaleString(),
										})
						}
					/>
				))}
			</div>
			<div className="flex justify-between mt-3 text-sm" data-testid="legend">
				<div className="flex items-center gap-1.5">
					<span
						className="w-2 h-2 rounded-full"
						style={{ backgroundColor: CACHE_HIT_COLOR }}
					/>
					<span className="text-(--text-tertiary)">
						{t("dashboard.tokens.cacheHit")}
					</span>
					<span className="font-medium text-(--text-primary) ml-1">
						{cacheHit.toLocaleString()}
					</span>
					<span className="text-(--text-muted) text-xs ml-1">
						{formatPercent(cacheHitPct)}
					</span>
				</div>
				<div className="flex items-center gap-1.5">
					<span
						className="w-2 h-2 rounded-full"
						style={{ backgroundColor: PROMPT_COLOR }}
					/>
					<span className="text-(--text-tertiary)">
						{t("dashboard.tokens.prompt")}
					</span>
					<span className="font-medium text-(--text-primary) ml-1">
						{uncachedPrompt.toLocaleString()}
					</span>
					<span className="text-(--text-muted) text-xs ml-1">
						{formatPercent(uncachedPct)}
					</span>
				</div>
				<div className="flex items-center gap-1.5">
					<span
						className="w-2 h-2 rounded-full"
						style={{ backgroundColor: COMPLETION_COLOR }}
					/>
					<span className="text-(--text-tertiary)">
						{t("dashboard.tokens.completion")}
					</span>
					<span className="font-medium text-(--text-primary) ml-1">
						{completion.toLocaleString()}
					</span>
					<span className="text-(--text-muted) text-xs ml-1">
						{formatPercent(completionPct)}
					</span>
				</div>
			</div>
		</div>
	);
}
