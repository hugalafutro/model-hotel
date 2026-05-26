import { Spinner } from "../../components/Spinner";
import { MetricToggle, RangeToggle } from "./ToggleGroup";
import type { MetricType, Range, UsageEntry } from "./types";

export function UsageBarPanel({
	title,
	icon: Icon,
	entries,
	range,
	onRangeChange,
	metric,
	onMetricChange,
	loading,
	onEntryClick,
}: {
	title: string;
	icon: React.ElementType;
	entries: UsageEntry[];
	range: Range;
	onRangeChange: (r: Range) => void;
	metric?: MetricType;
	onMetricChange?: (m: MetricType) => void;
	loading?: boolean;
	/** When provided, entry labels become clickable buttons that invoke this callback */
	onEntryClick?: (label: string) => void;
}) {
	const max = entries.length > 0 ? Math.max(...entries.map((e) => e.value)) : 0;

	return (
		<div className="ui-card p-6">
			<div className="flex items-center justify-between mb-5">
				<div className="flex items-center gap-2 min-w-0">
					<Icon size={18} className="text-(--accent)" />
					<h3 className="text-lg font-semibold text-(--text-primary) whitespace-nowrap">
						{title}
					</h3>
					{loading && <Spinner className="ml-1" />}
				</div>
				<div className="flex items-center gap-1 shrink-0">
					{metric !== undefined && onMetricChange !== undefined && (
						<MetricToggle value={metric} onChange={onMetricChange} />
					)}
					<RangeToggle value={range} onChange={onRangeChange} />
				</div>
			</div>
			{entries.length === 0 ? (
				<p className="text-sm text-(--text-muted) text-center py-8">
					No usage data yet. Usage breakdown will appear here once traffic
					flows.
				</p>
			) : (
				<div className="space-y-3.5">
					{entries.map((entry) => {
						const pct = max > 0 ? (entry.value / max) * 100 : 0;
						return (
							<div key={entry.label} className="space-y-1.5">
								<div className="flex justify-between items-center text-sm">
									{onEntryClick && !entry.failoverGroup ? (
										<button
											type="button"
											onClick={() => onEntryClick(entry.label)}
											className={`truncate max-w-[70%] text-left cursor-pointer transition-colors hover:text-(--accent) hover:drop-shadow-[var(--glow-accent)] ${entry.deleted ? "text-red-400 italic pr-1" : "text-(--text-secondary)"}`}
											title={`View details for ${entry.label}`}
											aria-label={`View details for ${entry.label}`}
										>
											{entry.label}
										</button>
									) : (
										<span
											className={`truncate max-w-[70%] ${entry.deleted ? "text-red-400 italic pr-1" : "text-(--text-secondary)"}`}
											title={entry.label}
										>
											{entry.label}
										</span>
									)}
									<span className="font-semibold text-(--text-primary) ml-2 shrink-0">
										{entry.value.toLocaleString()}
										{entry.suffix
											? entry.value === 1
												? entry.suffix.replace(/s$/, "")
												: entry.suffix
											: ""}
									</span>
								</div>
								<div className="h-[4px] rounded-full overflow-hidden bg-(--border-subtle)">
									<div
										className="h-full rounded-full transition-all duration-700 [transform:translateZ(0)]"
										style={{
											width: `${pct}%`,
											backgroundColor: "var(--accent)",
										}}
									/>
								</div>
							</div>
						);
					})}
				</div>
			)}
		</div>
	);
}
