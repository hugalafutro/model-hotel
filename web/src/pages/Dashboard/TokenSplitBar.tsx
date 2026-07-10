import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { Target } from "@/lib/icons";
import { Spinner } from "../../components/Spinner";
import {
	formatPercent,
	formatTokens,
	formatWithCommas,
} from "../../utils/format";
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

	// Measured bar width for integer-pixel tile layout. Fractional flex-1
	// tile widths make the 2px gaps land on sub-pixels and anti-alias
	// unevenly; the provider waffle avoids this with absolute integer
	// positioning, and so does this bar once measured. Before the first
	// measurement (and in jsdom, where ResizeObserver is a no-op) the
	// tiles fall back to the flex layout.
	const barRef = useRef<HTMLDivElement>(null);
	const [barWidth, setBarWidth] = useState(0);
	useEffect(() => {
		const el = barRef.current;
		if (!el) return;
		const ro = new ResizeObserver((entries) => {
			setBarWidth(Math.floor(entries[0].contentRect.width));
		});
		ro.observe(el);
		return () => ro.disconnect();
	}, []);

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

	// Integer-pixel tile layout: every gap is exactly GAP px; the leftover
	// pixels widen the first `rem` tiles by 1px each (imperceptible),
	// instead of letting the browser smear fractions into the gaps.
	const GAP = 2; // keep in sync with the flex fallback's gap-0.5
	const n = tiles.length;
	const cellW = n > 0 ? Math.floor((barWidth - (n - 1) * GAP) / n) : 0;
	const useIntegerLayout = barWidth > 0 && cellW >= 2;
	const rem = useIntegerLayout ? barWidth - (cellW * n + (n - 1) * GAP) : 0;
	const lefts: number[] = [];
	if (useIntegerLayout) {
		let x = 0;
		for (let i = 0; i < n; i++) {
			lefts.push(x);
			x += cellW + (i < rem ? 1 : 0) + GAP;
		}
	}

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
				title={formatWithCommas(total)}
			>
				{formatTokens(total)}{" "}
				<span className="text-sm font-normal text-(--text-muted)">
					{t("dashboard.tokens.tokens")}
				</span>
			</p>
			<div
				ref={barRef}
				className="relative flex gap-0.5 h-6"
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
						className={`ui-waffle-cell rounded-sm animate-waffle-pop ${
							useIntegerLayout ? "absolute inset-y-0" : "flex-1"
						}`}
						data-tile-type={tile.type}
						style={{
							backgroundColor: tileColor(tile.type),
							opacity: tile.opacity,
							animationDelay: `${i * 20}ms`,
							...(useIntegerLayout
								? { left: lefts[i], width: cellW + (i < rem ? 1 : 0) }
								: {}),
						}}
						title={
							tile.type === "cache_hit"
								? t("dashboard.tokens.cacheHitTooltip", {
										pct: cacheHitPct.toFixed(1),
										count: formatWithCommas(cacheHit),
									})
								: tile.type === "prompt"
									? t("dashboard.tokens.promptTooltip", {
											pct: uncachedPct.toFixed(1),
											count: formatWithCommas(uncachedPrompt),
										})
									: t("dashboard.tokens.completionTooltip", {
											pct: completionPct.toFixed(1),
											count: formatWithCommas(completion),
										})
						}
					/>
				))}
			</div>
			<div className="flex justify-between mt-3 text-sm" data-testid="legend">
				{/* Cache hit + Prompt stack: token values share a left-aligned
				    column and percents a right-aligned column via the grid. */}
				<div className="grid grid-cols-[auto_auto_auto_auto] items-center gap-x-1.5 gap-y-1.5">
					<div className="contents">
						<span
							className="w-2 h-2 rounded-full"
							style={{ backgroundColor: CACHE_HIT_COLOR }}
						/>
						<span className="text-(--text-tertiary)">
							{t("dashboard.tokens.cacheHit")}
						</span>
						<span
							className="font-medium text-(--text-primary)"
							title={formatWithCommas(cacheHit)}
						>
							{formatTokens(cacheHit)}
						</span>
						<span className="text-(--text-muted) text-xs tabular-nums justify-self-end">
							{formatPercent(cacheHitPct)}
						</span>
					</div>
					<div className="contents">
						<span
							className="w-2 h-2 rounded-full"
							style={{ backgroundColor: PROMPT_COLOR }}
						/>
						<span className="text-(--text-tertiary)">
							{t("dashboard.tokens.prompt")}
						</span>
						<span
							className="font-medium text-(--text-primary)"
							title={formatWithCommas(uncachedPrompt)}
						>
							{formatTokens(uncachedPrompt)}
						</span>
						<span className="text-(--text-muted) text-xs tabular-nums justify-self-end">
							{formatPercent(uncachedPct)}
						</span>
					</div>
				</div>
				<div className="flex items-center gap-1.5 self-end">
					<span
						className="w-2 h-2 rounded-full"
						style={{ backgroundColor: COMPLETION_COLOR }}
					/>
					<span className="text-(--text-tertiary)">
						{t("dashboard.tokens.completion")}
					</span>
					<span
						className="font-medium text-(--text-primary) ml-1"
						title={formatWithCommas(completion)}
					>
						{formatTokens(completion)}
					</span>
					<span className="text-(--text-muted) text-xs ml-1 tabular-nums">
						{formatPercent(completionPct)}
					</span>
				</div>
			</div>
		</div>
	);
}
