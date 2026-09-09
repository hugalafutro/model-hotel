import { useTranslation } from "react-i18next";
import type { AppLogEntry } from "../api/types";
import { useVirtualRows } from "../hooks/useVirtualRows";
import {
	formatLogTimestamp,
	getLevelBadgeVariant,
	getSourceBadgeClasses,
} from "../utils/logBadgeUtils";
import { appLogKey, displayLogMessage } from "../utils/logText";
import { Badge } from "./Badge";
import { VirtualTableFooter } from "./VirtualTableFooter";

interface VirtualAppLogTableProps {
	entries: AppLogEntry[];
	total: number;
	hasBefore: boolean;
	hasAfter: boolean;
	isLoadingBefore: boolean;
	isLoadingAfter: boolean;
	onFetchNewer: () => void;
	onFetchOlder: () => void;
	onRowClick: (entry: AppLogEntry) => void;
	sortDir: string;
	onSortToggle: () => void;
}

const HEADER_BASE =
	"px-2 py-2 text-left text-xs font-medium uppercase tracking-wider whitespace-nowrap ui-table-header-text";

export function VirtualAppLogTable(props: VirtualAppLogTableProps) {
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
		estimateSize: 48,
		pinTop: true,
		getItemKey: appLogKey,
	});

	return (
		<div className="flex flex-col min-h-0">
			<div
				ref={scrollRef}
				className="ui-card overflow-y-auto overflow-x-auto"
				style={{
					overflowAnchor: "none",
					height: "calc(100dvh - 242px)",
					minHeight: "200px",
				}}
				onScroll={handleScroll}
			>
				<table
					className="w-full table-fixed ui-table ui-table-virtual min-w-250"
					style={{
						marginTop: paddingTop,
						marginBottom: paddingBottom + 8,
					}}
				>
					<colgroup>
						<col className="w-44" />
						<col className="w-20" />
						<col className="w-24" />
						<col />
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
									title={t("components.virtualAppLogTable.timeDate")}
								>
									{t("components.virtualAppLogTable.timeDate")}{" "}
									{sortDir === "desc" ? "↓" : "↑"}
								</th>
								<th
									className={HEADER_BASE}
									title={t("components.virtualAppLogTable.level")}
								>
									{t("components.virtualAppLogTable.level")}
								</th>
								<th
									className={HEADER_BASE}
									title={t("components.virtualAppLogTable.source")}
								>
									{t("components.virtualAppLogTable.source")}
								</th>
								<th
									className={HEADER_BASE}
									title={t("components.virtualAppLogTable.message")}
								>
									{t("components.virtualAppLogTable.message")}
								</th>
							</tr>
						</thead>
					)}
					<tbody>
						{entries.length === 0 && (
							<tr>
								<td
									colSpan={4}
									className="px-4 py-8 text-center text-gray-500 text-sm"
								>
									{t("components.virtualAppLogTable.noEntriesFound")}
								</td>
							</tr>
						)}
						{virtualItems.map((vItem) => {
							const entry = entries[vItem.index];
							return (
								<tr
									key={vItem.key}
									data-index={vItem.index}
									ref={virtualizer.measureElement}
									className={`hover:bg-(--surface-hover) ${vItem.index % 2 === 1 ? "ui-row-even" : ""} cursor-pointer`}
									onClick={() => onRowClick(entry)}
								>
									<td className="px-2 py-1 align-middle whitespace-nowrap text-xs text-gray-400">
										{formatLogTimestamp(entry.timestamp)}
									</td>
									<td className="px-2 py-1 align-middle">
										<Badge variant={getLevelBadgeVariant(entry.level)}>
											{entry.level.toUpperCase()}
										</Badge>
									</td>
									<td className="px-2 py-1 align-middle">
										{entry.source ? (
											<Badge
												variant="custom"
												className={getSourceBadgeClasses(entry.source)}
											>
												{entry.source}
											</Badge>
										) : (
											<span className="text-gray-600">-</span>
										)}
									</td>
									<td className="px-2 py-1 align-middle">
										<div className="min-h-[2lh] flex items-center">
											<div className="text-xs font-mono line-clamp-2 text-gray-400">
												{displayLogMessage(
													entry.message,
													entry.escaped,
													entry.attrs_at,
												)}
											</div>
										</div>
									</td>
								</tr>
							);
						})}
					</tbody>
				</table>
			</div>
			<VirtualTableFooter
				range={
					entries.length > 0
						? `${startIndex}–${endIndex} / ${total.toLocaleString()}`
						: t("components.virtualAppLogTable.zeroEntries")
				}
				isLoadingBefore={isLoadingBefore}
				isLoadingAfter={isLoadingAfter}
			/>
		</div>
	);
}
