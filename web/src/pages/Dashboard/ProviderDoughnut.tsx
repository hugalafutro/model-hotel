import { TrendingUp } from "lucide-react";
import { Cell, Pie, PieChart, ResponsiveContainer } from "recharts";
import { Spinner } from "../../components/Spinner";
import { MetricToggle, RangeToggle } from "./ToggleGroup";
import type { MetricType, ProviderDistItem, Range } from "./types";
import { formatCompact } from "./utils";

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
	const colors = ["#818cf8", "#059669", "#fbbf24", "#f87171", "#a78bfa"];

	return (
		<div className="ui-card p-6">
			<div className="flex items-center justify-between mb-4">
				<h3 className="text-lg font-semibold text-(--text-primary) flex items-center gap-2">
					<TrendingUp size={18} className="text-(--accent)" />
					Provider Breakdown
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
					<div className="w-35 h-35">
						<ResponsiveContainer width="100%" height="100%">
							<PieChart>
								<Pie
									data={items}
									cx="50%"
									cy="50%"
									innerRadius={50}
									outerRadius={65}
									paddingAngle={2}
									dataKey="share"
									stroke="none"
								>
									{items.map((item, i) => (
										<Cell key={item.name} fill={colors[i % colors.length]} />
									))}
								</Pie>
							</PieChart>
						</ResponsiveContainer>
					</div>
					<div className="flex-1 space-y-2">
						{items.map((it, i) => (
							<div
								key={it.name}
								className="flex items-center justify-between gap-3"
							>
								<div className="flex items-center gap-2 min-w-0">
									<span
										className="w-2.5 h-2.5 rounded-full shrink-0"
										style={{
											backgroundColor: colors[i % colors.length],
										}}
									/>
									<span className="text-sm text-(--text-secondary) truncate">
										{it.name}
									</span>
								</div>
								<div className="text-right shrink-0 flex items-baseline justify-end tabular-nums">
									<span className="text-sm font-medium text-(--text-primary) w-14 text-right">
										{it.share.toFixed(1)}%
									</span>
									<span className="text-xs text-(--text-muted) ml-1 min-w-20 text-left">
										(
										{metric === "tokens"
											? `${formatCompact(it.tokens)} Token${it.tokens !== 1 ? "s" : ""}`
											: `${it.count} Request${it.count !== 1 ? "s" : ""}`}
										)
									</span>
								</div>
							</div>
						))}
					</div>
				</div>
			)}
		</div>
	);
}
