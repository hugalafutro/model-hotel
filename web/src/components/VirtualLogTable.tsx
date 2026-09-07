import { useTranslation } from "react-i18next";
import type { LogEntry } from "../api/types";
import { useVirtualRows } from "../hooks/useVirtualRows";
import { formatNumber } from "../utils/format";
import { isInProgress } from "../utils/logHelpers";
import { RequestLogCells } from "./logs/RequestLogCells";
import { LOG_COL_WIDTHS, LOG_TABLE_MIN_W } from "./logTableWidths";
import { VirtualTableFooter } from "./VirtualTableFooter";

interface VirtualLogTableProps {
	entries: LogEntry[];
	total: number;
	hasBefore: boolean;
	hasAfter: boolean;
	isLoadingBefore: boolean;
	isLoadingAfter: boolean;
	onFetchNewer: () => void;
	onFetchOlder: () => void;
	onRowClick: (entry: LogEntry) => void;
	/** Ticking "now" used to evaluate in-progress/stale state (60s interval). */
	nowMs: number;
	/** Configured stale-request timeout in ms. */
	staleThresholdMs: number;
	sortDir: string;
	onSortToggle: () => void;
}

const HEADER_BASE =
	"px-2 py-2 text-left text-xs font-medium uppercase tracking-wider whitespace-nowrap ui-table-header-text";

export function VirtualLogTable(props: VirtualLogTableProps) {
	"use no memo";
	const { t } = useTranslation();

	const {
		entries,
		total,
		hasBefore,
		hasAfter,
		isLoadingBefore,
		isLoadingAfter,
		onFetchNewer,
		onFetchOlder,
		onRowClick,
		nowMs,
		staleThresholdMs,
		sortDir,
		onSortToggle,
	} = props;

	const {
		scrollRef,
		virtualizer,
		virtualItems,
		paddingTop,
		paddingBottom,
		handleScroll,
		startIndex,
		endIndex,
	} = useVirtualRows({
		entries,
		hasBefore,
		hasAfter,
		isLoadingBefore,
		isLoadingAfter,
		fetchNewer: onFetchNewer,
		fetchOlder: onFetchOlder,
		estimateSize: 29,
		pinTop: true,
	});

	return (
		<div className="flex flex-col min-h-0">
			<div
				ref={scrollRef}
				className="ui-card overflow-y-auto"
				style={{
					overflowAnchor: "none",
					height: "calc(100dvh - 242px)",
					minHeight: "200px",
				}}
				onScroll={handleScroll}
			>
				<table
					className={`w-full table-fixed ui-table ui-table-virtual ${LOG_TABLE_MIN_W}`}
					style={{
						marginTop: paddingTop,
						marginBottom: paddingBottom + 8,
					}}
				>
					<colgroup>
						{LOG_COL_WIDTHS.map((col) => (
							<col key={col.key} className={col.width} />
						))}
					</colgroup>
					{/* No header over an empty table: with nothing to sort or line
					    up, the column strip reads as a broken load rather than an
					    empty result. */}
					{entries.length > 0 && (
						<thead className="sticky top-0 z-10">
							<tr>
								<th
									className={`${HEADER_BASE} cursor-pointer`}
									onClick={onSortToggle}
									title={t("logs.table.timeDate")}
								>
									{t("logs.table.timeDate")} {sortDir === "desc" ? "↓" : "↑"}
								</th>
								<th className={HEADER_BASE} title={t("logs.table.model")}>
									{t("logs.table.model")}
								</th>
								<th className={HEADER_BASE} title={t("logs.table.provider")}>
									{t("logs.table.provider")}
								</th>
								<th className={HEADER_BASE} title={t("logs.table.status")}>
									{t("logs.table.status")}
								</th>
								<th className={HEADER_BASE} title={t("logs.table.tokens")}>
									{t("logs.table.tokens")}
								</th>
								<th className={HEADER_BASE} title={t("logs.table.tps")}>
									{t("logs.table.tps")}
								</th>
								<th className={HEADER_BASE} title={t("logs.table.headers")}>
									{t("logs.table.headers")}
								</th>
								<th className={HEADER_BASE} title={t("logs.table.ttft")}>
									{t("logs.table.ttft")}
								</th>
								<th className={HEADER_BASE} title={t("logs.table.duration")}>
									{t("logs.table.duration")}
								</th>
								<th className={HEADER_BASE} title={t("logs.table.overhead")}>
									{t("logs.table.overhead")}
								</th>
								<th className={HEADER_BASE} title={t("logs.table.key")}>
									{t("logs.table.key")}
								</th>
								<th className={HEADER_BASE} title={t("logs.table.ip")}>
									{t("logs.table.ip")}
								</th>
							</tr>
						</thead>
					)}
					<tbody>
						{entries.length === 0 && (
							<tr>
								<td
									colSpan={LOG_COL_WIDTHS.length}
									className="px-4 py-8 text-center text-gray-500 text-sm"
								>
									{t("components.virtualLogTable.noLogsFound")}
								</td>
							</tr>
						)}
						{virtualItems.map((vItem) => {
							const log = entries[vItem.index];
							const inProgress = isInProgress(log, nowMs, staleThresholdMs);
							return (
								<tr
									key={vItem.key}
									data-index={vItem.index}
									ref={virtualizer.measureElement}
									className={`hover:bg-(--surface-hover) ${vItem.index % 2 === 1 ? "ui-row-even" : ""} ${inProgress ? "animate-pulse-subtle" : ""} cursor-pointer`}
									onClick={() => onRowClick(log)}
								>
									<RequestLogCells
										log={log}
										nowMs={nowMs}
										staleThresholdMs={staleThresholdMs}
									/>
								</tr>
							);
						})}
					</tbody>
				</table>
			</div>
			<VirtualTableFooter
				range={
					entries.length > 0
						? `${formatNumber(startIndex)}–${formatNumber(endIndex)} / ${formatNumber(total)}`
						: t("components.virtualLogTable.zeroEntries")
				}
				isLoadingBefore={isLoadingBefore}
				isLoadingAfter={isLoadingAfter}
			/>
		</div>
	);
}
