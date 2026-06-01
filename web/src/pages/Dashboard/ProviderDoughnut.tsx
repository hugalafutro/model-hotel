import { TrendingUp } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Spinner } from "../../components/Spinner";
import { formatCompact, formatPercent } from "../../utils/format";
import { MetricToggle, RangeToggle } from "./ToggleGroup";
import type { MetricType, ProviderDistItem, Range } from "./types";

const COLORS = [
	"#818cf8",
	"#34d399",
	"#fbbf24",
	"#f87171",
	"#38bdf8",
	"#c084fc",
	"#fb923c",
	"#f472b6",
] as const;
const GRID = 10;
const CELL = 12;
const GAP = 2;
const GRID_SIZE = GRID * CELL + (GRID - 1) * GAP;

function buildCells(items: ProviderDistItem[]) {
	const total = GRID * GRID;
	const counts = items.map((it) => Math.round(it.share));

	// Guarantee 1 cell for any provider present in the data (share may be 0
	// after backend rounding of <0.1% values, but count/tokens prove existence)
	for (let i = 0; i < counts.length; i++) {
		if (counts[i] === 0 && (items[i].count > 0 || items[i].tokens > 0)) {
			counts[i] = 1;
		}
	}

	// Adjust total to exactly 100
	const sum = counts.reduce((s, v) => s + v, 0);
	if (sum > total) {
		// Over-allocated: subtract excess from providers with largest counts
		const excess = sum - total;
		for (let e = 0; e < excess; e++) {
			const maxIdx = counts.indexOf(Math.max(...counts));
			counts[maxIdx]--;
		}
	} else if (sum < total) {
		// Under-allocated: give leftover to providers with largest fractional part
		const shortfall = total - sum;
		const fractional = items
			.map((it, i) => ({ index: i, frac: it.share - Math.floor(it.share) }))
			.sort((a, b) => b.frac - a.frac);
		for (let l = 0; l < shortfall; l++) {
			counts[fractional[l % fractional.length].index]++;
		}
	}

	const cells: {
		color: string;
		providerIndex: number;
		providerName: string;
	}[] = [];

	for (let i = 0; i < items.length; i++) {
		for (let s = 0; s < counts[i]; s++) {
			cells.push({
				color: COLORS[i % COLORS.length],
				providerIndex: i,
				providerName: items[i].name,
			});
		}
	}

	return cells;
}

export function ProviderDoughnut({
	items,
	range,
	onRangeChange,
	metric,
	onMetricChange,
	loading,
}: {
	items: ProviderDistItem[];
	range: Range;
	onRangeChange: (r: Range) => void;
	metric: MetricType;
	onMetricChange: (m: MetricType) => void;
	loading?: boolean;
}) {
	const { t } = useTranslation();
	const [hoveredProvider, setHoveredProvider] = useState<string | null>(null);
	const cells = items.length > 0 ? buildCells(items) : [];

	return (
		<div className="ui-card p-6">
			<div className="flex items-center justify-between mb-4">
				<h3 className="text-lg font-semibold text-(--text-primary) flex items-center gap-2">
					<TrendingUp size={18} className="text-(--accent)" />
					{items.length > 0
						? t("dashboard.providers.top", {
								count: items.length,
							})
						: t("dashboard.providers.providers")}
					{loading && <Spinner className="ml-1" />}
				</h3>
				<div className="flex items-center gap-1.5">
					<MetricToggle value={metric} onChange={onMetricChange} />
					<RangeToggle value={range} onChange={onRangeChange} />
				</div>
			</div>
			{items.length === 0 ? (
				<p className="text-sm text-(--text-muted) text-center py-12">
					{t("dashboard.providers.emptyState")}
				</p>
			) : (
				<div className="flex items-center gap-6">
					<div
						className="relative shrink-0"
						style={{ width: GRID_SIZE, height: GRID_SIZE }}
						role="img"
						aria-label={t("dashboard.providers.chartAriaLabel")}
					>
						{cells.map((cell, i) => {
							const isDimmed =
								hoveredProvider !== null &&
								cell.providerName !== hoveredProvider;
							const isGlowing =
								hoveredProvider !== null &&
								cell.providerName === hoveredProvider;
							const col = i % GRID;
							const row = Math.floor(i / GRID);

							return (
								<div
									// biome-ignore lint/suspicious/noArrayIndexKey: fixed 100-cell grid, order never changes
									key={i}
									className="absolute rounded-[2px] animate-waffle-pop"
									style={{
										width: CELL,
										height: CELL,
										left: col * (CELL + GAP),
										top: row * (CELL + GAP),
										backgroundColor: cell.color,
										animationDelay: `${i * 6}ms`,
										filter: isDimmed ? "grayscale(1)" : undefined,
										opacity: isDimmed ? 0.2 : 1,
										boxShadow: isGlowing
											? `0 0 6px 1px ${cell.color}90`
											: undefined,
										transition: "filter 0.2s, opacity 0.2s, box-shadow 0.2s",
									}}
								/>
							);
						})}
					</div>
					<ul className="flex-1 space-y-2 list-none m-0 p-0">
						{items.map((it, i) => {
							const isHighlighted = hoveredProvider === it.name;
							return (
								<li
									key={it.name}
									className="grid grid-cols-[1fr_minmax(0,5.5rem_auto)] gap-3 items-baseline"
									onMouseEnter={() => setHoveredProvider(it.name)}
									onMouseLeave={() => setHoveredProvider(null)}
								>
									<div className="flex items-center gap-2 min-w-0">
										<span
											className="w-2.5 h-2.5 rounded-full shrink-0"
											style={{
												backgroundColor: COLORS[i % COLORS.length],
												boxShadow: isHighlighted
													? `0 0 8px 2px ${COLORS[i % COLORS.length]}80`
													: undefined,
												transition: "box-shadow 0.2s",
											}}
										/>
										<span
											className="text-sm text-(--text-secondary) truncate"
											style={{
												color: isHighlighted
													? COLORS[i % COLORS.length]
													: undefined,
												transition: "color 0.2s",
											}}
										>
											{it.name}
										</span>
										<span className="text-xs text-(--text-muted)">
											{metric === "tokens"
												? `${formatCompact(it.tokens)} ${t(it.tokens !== 1 ? "dashboard.providers.tokensUnit" : "dashboard.providers.tokenUnit")}`
												: `${it.count} ${t(it.count !== 1 ? "dashboard.providers.requestsUnit" : "dashboard.providers.requestUnit")}`}
										</span>
									</div>
									<span className="text-sm font-medium text-(--text-primary) tabular-nums text-right">
										{formatPercent(it.share)}
									</span>
								</li>
							);
						})}
					</ul>
				</div>
			)}
		</div>
	);
}
