import { useTranslation } from "react-i18next";
import { Spinner } from "../../components/Spinner";
import { RangeToggle } from "./ToggleGroup";
import type { Range } from "./types";

function formatLatency(ms: number): string {
	if (ms >= 1000) {
		const sec = ms / 1000;
		return sec >= 10 ? `${Math.round(sec)}s` : `${sec.toFixed(1)}s`;
	}
	return `${Math.round(ms)}ms`;
}

function getColorForRank(index: number, total: number): string {
	if (total === 1) {
		// Single entry: use a neutral color (middle of the gradient)
		return "hsl(60, 70%, 50%)";
	}
	// index 0 = slowest/worst (red, hue 0)
	// index total-1 = fastest/best (green, hue 120)
	const hue = (index / (total - 1)) * 120;
	return `hsl(${hue}, 70%, 50%)`;
}

export type ProviderLatencyEntry = {
	label: string;
	totalMs: number;
	overheadMs: number;
	providerMs: number;
	requestCount: number;
};

export function ProviderLatencyPanel({
	title,
	icon: Icon,
	entries,
	range,
	onRangeChange,
	loading,
}: {
	title: string;
	icon: React.ElementType;
	entries: ProviderLatencyEntry[];
	range: Range;
	onRangeChange: (r: Range) => void;
	loading?: boolean;
}) {
	const { t } = useTranslation();

	// Sort entries by totalMs descending (slowest first) for consistent color assignment
	const sortedEntries = [...entries].sort((a, b) => b.totalMs - a.totalMs);
	// Also sort by overheadMs descending for independent overhead coloring
	const sortedByOverhead = [...entries].sort(
		(a, b) => b.overheadMs - a.overheadMs,
	);

	// Create maps to lookup color by label
	const responseColorMap = new Map<string, string>();
	const overheadColorMap = new Map<string, string>();

	sortedEntries.forEach((entry, index) => {
		responseColorMap.set(
			entry.label,
			getColorForRank(index, sortedEntries.length),
		);
	});

	sortedByOverhead.forEach((entry, index) => {
		overheadColorMap.set(
			entry.label,
			getColorForRank(index, sortedByOverhead.length),
		);
	});

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
					<RangeToggle value={range} onChange={onRangeChange} />
				</div>
			</div>
			{!loading && entries.length === 0 ? (
				<p className="text-sm text-(--text-muted) text-center py-8">
					{t("dashboard.providerLatency.emptyState")}
				</p>
			) : (
				<div className="space-y-3">
					{/* Column headers */}
					<div className="grid grid-cols-3 gap-2 text-xs font-semibold text-(--text-muted) uppercase tracking-wide">
						<div></div>
						<div className="text-right">
							{t("dashboard.providerLatency.response")}
						</div>
						<div className="text-right">
							{t("dashboard.providerLatency.overhead")}
						</div>
					</div>
					{/* Provider rows */}
					{sortedEntries.map((entry) => {
						const responseColor =
							responseColorMap.get(entry.label) || "hsl(60, 70%, 50%)";
						const overheadColor =
							overheadColorMap.get(entry.label) || "hsl(60, 70%, 50%)";

						return (
							<div
								key={entry.label}
								className="grid grid-cols-3 gap-2 items-center text-sm"
							>
								<div
									className="truncate text-(--text-secondary)"
									title={entry.label}
								>
									{entry.label}
								</div>
								<div
									className="text-right font-semibold"
									style={{ color: responseColor }}
									title={t("dashboard.providerLatency.responseTooltip", {
										value: formatLatency(entry.totalMs),
									})}
								>
									{formatLatency(entry.totalMs)}
								</div>
								<div
									className="text-right font-semibold"
									style={{ color: overheadColor }}
									title={t("dashboard.providerLatency.overheadTooltip", {
										value: formatLatency(entry.overheadMs),
									})}
								>
									{formatLatency(entry.overheadMs)}
								</div>
							</div>
						);
					})}
				</div>
			)}
		</div>
	);
}
