// Reserved for future dashboard customization feature.
// This panel will be available as a card option when users can
// customize their dashboard layout. Do not delete.
import { useTranslation } from "react-i18next";
import { Spinner } from "../../components/Spinner";
import { formatLatency } from "../../utils/format";
import { RangeToggle } from "./ToggleGroup";
import type { Range } from "./types";

export type LatencyEntry = {
	label: string;
	totalMs: number;
	overheadMs: number;
	providerMs: number;
	requestCount: number;
};

export function LatencyBarPanel({
	title,
	icon: Icon,
	entries,
	range,
	onRangeChange,
	loading,
	overheadColor,
}: {
	title: string;
	icon: React.ElementType;
	entries: LatencyEntry[];
	range: Range;
	onRangeChange: (r: Range) => void;
	loading?: boolean;
	/** Color for the proxy overhead portion of the split bar (CSS color) */
	overheadColor: string;
}) {
	const { t } = useTranslation();
	const max =
		entries.length > 0 ? Math.max(...entries.map((e) => e.totalMs)) : 0;

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
					{t("dashboard.latency.emptyState")}
				</p>
			) : (
				<div className="space-y-3.5">
					{entries.map((entry) => {
						const totalPct = max > 0 ? (entry.totalMs / max) * 100 : 0;
						const overheadPct =
							entry.totalMs > 0
								? Math.min(
										(entry.overheadMs / entry.totalMs) * totalPct,
										totalPct,
									)
								: 0;
						const providerPct = totalPct - overheadPct;
						const overheadRatio =
							entry.totalMs > 0
								? ((entry.overheadMs / entry.totalMs) * 100).toFixed(1)
								: "0.0";

						return (
							<div key={entry.label} className="space-y-1.5">
								<div className="flex justify-between items-center text-sm">
									<span
										className="truncate max-w-[70%] text-(--text-secondary)"
										title={entry.label}
									>
										{entry.label}
									</span>
									<span
										className="font-semibold text-(--text-primary) ml-2 shrink-0"
										title={t("dashboard.latency.tooltip", {
											overhead: Math.round(entry.overheadMs),
											provider: Math.round(entry.providerMs),
											ratio: overheadRatio,
										})}
									>
										{formatLatency(entry.totalMs)}
									</span>
								</div>
								<div className="h-[4px] rounded-full overflow-hidden bg-(--border-subtle) flex">
									<div
										className="h-full transition-all duration-700 [transform:translateZ(0)] rounded-l-full"
										style={{
											width: `${providerPct}%`,
											backgroundColor: "var(--accent)",
										}}
										title={t("dashboard.latency.providerTip", {
											value: formatLatency(entry.providerMs),
										})}
									/>
									{overheadPct > 0 && (
										<div
											className="h-full transition-all duration-700 [transform:translateZ(0)] rounded-r-full"
											style={{
												width: `${overheadPct}%`,
												backgroundColor: overheadColor,
											}}
											title={t("dashboard.latency.overheadTip", {
												value: formatLatency(entry.overheadMs),
												ratio: overheadRatio,
											})}
										/>
									)}
								</div>
							</div>
						);
					})}
				</div>
			)}
		</div>
	);
}
