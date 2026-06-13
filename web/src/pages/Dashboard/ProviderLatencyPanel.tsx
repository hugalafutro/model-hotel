import { ChevronDown, ChevronUp } from "lucide-react";
import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { Spinner } from "../../components/Spinner";
import { useLocalStorage } from "../../hooks/useLocalStorage";
import { formatLatency } from "../../utils/format";
import { RangeToggle } from "./ToggleGroup";
import type { Range } from "./types";

type SortField = "response" | "overhead";
type SortDir = "asc" | "desc";

const VALID_SORT_FIELDS: SortField[] = ["response", "overhead"];
const VALID_SORT_DIRS: SortDir[] = ["asc", "desc"];

function deserializeSortField(stored: string, fallback: SortField): SortField {
	return VALID_SORT_FIELDS.includes(stored as SortField)
		? (stored as SortField)
		: fallback;
}

function deserializeSortDir(stored: string, fallback: SortDir): SortDir {
	return VALID_SORT_DIRS.includes(stored as SortDir)
		? (stored as SortDir)
		: fallback;
}

const FALLBACK_COLOR = "hsl(60, 70%, 50%)";

// Color gradient: index 0 = worst (orange, hue 30) → index total-1 = best (green, hue 120)
function getColorForRank(index: number, total: number): string {
	if (total === 1) {
		return FALLBACK_COLOR;
	}
	// hue 30 = orange (worst) → hue 120 = green (best)
	const hue = 30 + (index / (total - 1)) * 90;
	return `hsl(${hue}, 70%, 50%)`;
}

function SortArrow({
	field,
	sortField,
	sortDir,
}: {
	field: SortField;
	sortField: SortField;
	sortDir: SortDir;
}) {
	if (sortField !== field) return null;
	const Arrow = sortDir === "desc" ? ChevronDown : ChevronUp;
	return <Arrow size={12} className="inline ml-0.5" />;
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
	const [sortField, setSortField] = useLocalStorage<SortField>(
		"dashboard.latencySortField",
		"response",
		{ deserialize: deserializeSortField },
	);
	const [sortDir, setSortDir] = useLocalStorage<SortDir>(
		"dashboard.latencySortDir",
		"desc",
		{ deserialize: deserializeSortDir },
	);

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
				// One grid owns the column tracks; the header and each provider row
				// are subgrids spanning all three columns, so the two value columns
				// auto-size to the widest content across every row and stay aligned.
				// (Independent per-row grids each sized their own columns, which is
				// what broke the alignment.) gap-x is tight; the label column (1fr)
				// takes the remaining room.
				<div className="grid grid-cols-[1fr_auto_auto] gap-x-4 gap-y-3">
					{/* Column headers — clickable to sort */}
					<div className="col-span-3 grid grid-cols-subgrid items-center text-xs font-semibold text-(--text-muted) uppercase tracking-wide">
						<div></div>
						<button
							type="button"
							onClick={() => handleSort("response")}
							className="text-right hover:text-(--text-primary) transition-colors flex items-center justify-end gap-0.5"
						>
							{t("dashboard.providerLatency.response")}
							<SortArrow
								field="response"
								sortField={sortField}
								sortDir={sortDir}
							/>
						</button>
						<button
							type="button"
							onClick={() => handleSort("overhead")}
							className="text-right hover:text-(--text-primary) transition-colors flex items-center justify-end gap-0.5"
						>
							{t("dashboard.providerLatency.overhead")}
							<SortArrow
								field="overhead"
								sortField={sortField}
								sortDir={sortDir}
							/>
						</button>
					</div>
					{/* Provider rows */}
					{sortedEntries.map((entry) => {
						const responseColor =
							responseColorMap.get(entry.label) || FALLBACK_COLOR;
						const overheadColor =
							overheadColorMap.get(entry.label) || FALLBACK_COLOR;

						return (
							<div
								key={entry.label}
								className="col-span-3 grid grid-cols-subgrid items-center text-sm"
							>
								<div
									className="min-w-0 truncate text-(--text-secondary)"
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
