import { ChevronDown, ChevronUp } from "lucide-react";
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { Spinner } from "../../components/Spinner";
import { formatLatency } from "../../utils/format";
import { RangeToggle } from "./ToggleGroup";
import type { Range } from "./types";

type SortField = "response" | "overhead";
type SortDir = "asc" | "desc";

// Color gradient: index 0 = worst (orange, hue 30) → index total-1 = best (green, hue 120)
function getColorForRank(index: number, total: number): string {
	if (total === 1) {
		return "hsl(60, 70%, 50%)";
	}
	// hue 30 = orange (worst) → hue 120 = green (best)
	const hue = 30 + (index / (total - 1)) * 90;
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
	const [sortField, setSortField] = useState<SortField>("response");
	const [sortDir, setSortDir] = useState<SortDir>("desc");

	// Build independent color maps based on ranking (worst→best)
	const { responseColorMap, overheadColorMap } = useMemo(() => {
		const byResponse = [...entries].sort((a, b) => b.totalMs - a.totalMs);
		const byOverhead = [...entries].sort((a, b) => b.overheadMs - a.overheadMs);

		const rMap = new Map<string, string>();
		const oMap = new Map<string, string>();

		byResponse.forEach((entry, i) => {
			rMap.set(entry.label, getColorForRank(i, byResponse.length));
		});
		byOverhead.forEach((entry, i) => {
			oMap.set(entry.label, getColorForRank(i, byOverhead.length));
		});

		return { responseColorMap: rMap, overheadColorMap: oMap };
	}, [entries]);

	// Sort entries for display based on user selection
	const sortedEntries = useMemo(() => {
		const sorted = [...entries];
		if (sortField === "response") {
			sorted.sort((a, b) =>
				sortDir === "desc" ? b.totalMs - a.totalMs : a.totalMs - b.totalMs,
			);
		} else {
			sorted.sort((a, b) =>
				sortDir === "desc"
					? b.overheadMs - a.overheadMs
					: a.overheadMs - b.overheadMs,
			);
		}
		return sorted;
	}, [entries, sortField, sortDir]);

	const handleSort = (field: SortField) => {
		if (sortField === field) {
			setSortDir((d) => (d === "desc" ? "asc" : "desc"));
		} else {
			setSortField(field);
			setSortDir("desc");
		}
	};

	const SortArrow = ({ field }: { field: SortField }) => {
		if (sortField !== field) return null;
		const Arrow = sortDir === "desc" ? ChevronDown : ChevronUp;
		return <Arrow size={12} className="inline ml-0.5" />;
	};

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
					{/* Column headers — clickable to sort */}
					<div className="grid grid-cols-3 gap-2 text-xs font-semibold text-(--text-muted) uppercase tracking-wide">
						<div></div>
						<button
							type="button"
							onClick={() => handleSort("response")}
							className="text-right cursor-pointer hover:text-(--text-primary) transition-colors flex items-center justify-end gap-0.5 ml-auto"
						>
							{t("dashboard.providerLatency.response")}
							<SortArrow field="response" />
						</button>
						<button
							type="button"
							onClick={() => handleSort("overhead")}
							className="text-right cursor-pointer hover:text-(--text-primary) transition-colors flex items-center justify-end gap-0.5 ml-auto"
						>
							{t("dashboard.providerLatency.overhead")}
							<SortArrow field="overhead" />
						</button>
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
										count: entry.requestCount,
									})}
								>
									{formatLatency(entry.totalMs)}
								</div>
								<div
									className="text-right font-semibold"
									style={{ color: overheadColor }}
									title={t("dashboard.providerLatency.overheadTooltip", {
										value: formatLatency(entry.overheadMs),
										count: entry.requestCount,
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
