import { TrendingUp } from "lucide-react";
import { useState } from "react";
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

/**
 * Largest-remainder method with minimum-1 guarantee:
 * every provider with share > 0 gets at least 1 cell, total = exactly 100.
 */
function buildCells(items: ProviderDistItem[]) {
	const total = GRID * GRID;

	// Guarantee 1 cell per provider with non-zero share, then distribute remainder
	const guaranteed = items.map((it) => (it.share > 0 ? 1 : 0));
	const guaranteedSum = guaranteed.reduce((s: number, v) => s + v, 0);
	const remaining = total - guaranteedSum;

	// Allocate remaining cells by largest fractional part
	const rawShares = items.map((it) => (it.share / 100) * total);
	const adjusted = rawShares.map((raw, i) => raw - guaranteed[i]);
	const floored = adjusted.map((v) => Math.max(0, Math.floor(v)));
	const remainders = adjusted.map((v, i) => ({
		index: i,
		remainder: v - Math.floor(v),
	}));

	const sumFloor = floored.reduce((s, v) => s + v, 0);
	const leftover = remaining - sumFloor;

	remainders.sort((a, b) => b.remainder - a.remainder);
	if (leftover >= 0) {
		for (let l = 0; l < leftover; l++) {
			floored[remainders[l].index]++;
		}
	} else {
		// Over-allocated (clipping of negative adjusted): subtract from largest
		const deficit = -leftover;
		// Sort by floored count descending to take from the largest
		const bySize = floored
			.map((v, i) => ({ index: i, value: v }))
			.filter((e) => e.value > 0)
			.sort((a, b) => b.value - a.value);
		for (let d = 0; d < deficit && d < bySize.length; d++) {
			floored[bySize[d].index]--;
		}
	}

	const counts = items.map((_, i) => guaranteed[i] + floored[i]);

	// Build cell array
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
	const [hoveredProvider, setHoveredProvider] = useState<string | null>(null);
	const cells = items.length > 0 ? buildCells(items) : [];

	return (
		<div className="ui-card p-6">
			<div className="flex items-center justify-between mb-4">
				<h3 className="text-lg font-semibold text-(--text-primary) flex items-center gap-2">
					<TrendingUp size={18} className="text-(--accent)" />
					{items.length > 0
						? `Top ${items.length} Provider${items.length !== 1 ? "s" : ""}`
						: "Providers"}
					{loading && <Spinner className="ml-1" />}
				</h3>
				<div className="flex items-center gap-1.5">
					<MetricToggle value={metric} onChange={onMetricChange} />
					<RangeToggle value={range} onChange={onRangeChange} />
				</div>
			</div>
			{items.length === 0 ? (
				<p className="text-sm text-(--text-muted) text-center py-12">
					No provider data yet. Provider breakdown will appear here once traffic
					flows.
				</p>
			) : (
				<div className="flex items-center gap-6">
					<div
						className="relative shrink-0"
						style={{ width: GRID_SIZE, height: GRID_SIZE }}
						role="img"
						aria-label="Provider distribution chart"
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
									className="flex items-center justify-between gap-3"
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
									</div>
									<div className="text-right shrink-0 flex items-baseline justify-end tabular-nums">
										<span className="text-sm font-medium text-(--text-primary) w-14 text-right">
											{formatPercent(it.share)}
										</span>
										<span className="text-xs text-(--text-muted) ml-1 min-w-20 text-left">
											(
											{metric === "tokens"
												? `${formatCompact(it.tokens)} Token${it.tokens !== 1 ? "s" : ""}`
												: `${it.count} Request${it.count !== 1 ? "s" : ""}`}
											)
										</span>
									</div>
								</li>
							);
						})}
					</ul>
				</div>
			)}
		</div>
	);
}
