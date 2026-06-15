import { useVirtualizer } from "@tanstack/react-virtual";
import { useCallback, useLayoutEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import type { AppLogEntry } from "../api/types";
import {
	formatTimestamp,
	getLevelBadgeVariant,
	getSourceBadgeClasses,
} from "../utils/logBadgeUtils";
import { Badge } from "./Badge";

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

const EDGE_THRESHOLD_PX = 500;

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

	const scrollRef = useRef<HTMLDivElement>(null);

	// eslint-disable-next-line react-hooks/incompatible-library -- TanStack Virtual returns mutable functions; compiler skips memoization
	const virtualizer = useVirtualizer({
		count: entries.length,
		getScrollElement: () => scrollRef.current,
		estimateSize: () => 48,
		overscan: 20,
		getItemKey: (index) =>
			entries[index].id ??
			`${entries[index].timestamp}-${entries[index].source}-${entries[index].message.slice(0, 20)}`,
	});

	const virtualItems = virtualizer.getVirtualItems();

	const prevEntriesRef = useRef(entries);
	const prevTotalSizeRef = useRef(0);
	const [, forceRerender] = useState(0);

	// biome-ignore lint/correctness/useExhaustiveDependencies: virtualizer.getTotalSize is a stable reference
	useLayoutEffect(() => {
		const prev = prevEntriesRef.current;
		if (entries.length > prev.length && prev.length > 0) {
			const newItemCount = entries.length - prev.length;
			if (entries[newItemCount]?.id === prev[0]?.id && scrollRef.current) {
				if (scrollRef.current.scrollTop > 1) {
					const newTotalSize = virtualizer.getTotalSize();
					scrollRef.current.scrollTop +=
						newTotalSize - prevTotalSizeRef.current;
				}
				prevEntriesRef.current = entries;
				prevTotalSizeRef.current = virtualizer.getTotalSize();
				forceRerender((c) => c + 1);
				return;
			}
		}
		prevEntriesRef.current = entries;
		prevTotalSizeRef.current = virtualizer.getTotalSize();
		// eslint-disable-next-line react-hooks/exhaustive-deps -- virtualizer is stable ref; adding it would cause infinite re-renders
	}, [entries]);

	useLayoutEffect(() => {
		prevTotalSizeRef.current = virtualizer.getTotalSize();
	});

	const [paddingTop, paddingBottom] =
		virtualItems.length > 0
			? [
					Math.max(0, virtualItems[0].start),
					Math.max(
						0,
						virtualizer.getTotalSize() -
							virtualItems[virtualItems.length - 1].end,
					),
				]
			: [0, 0];

	const handleScroll = useCallback(() => {
		const el = scrollRef.current;
		if (!el) return;

		const nearTop = el.scrollTop < EDGE_THRESHOLD_PX;
		const nearBottom =
			el.scrollHeight - el.scrollTop - el.clientHeight < EDGE_THRESHOLD_PX;

		if (nearTop && hasBefore && !isLoadingBefore) {
			onFetchNewer();
		}
		if (nearBottom && hasAfter && !isLoadingAfter) {
			onFetchOlder();
		}
	}, [
		hasBefore,
		hasAfter,
		isLoadingBefore,
		isLoadingAfter,
		onFetchNewer,
		onFetchOlder,
	]);

	if (entries.length === 0) {
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
				>
					<table className="w-full table-fixed ui-table ui-table-virtual min-w-250">
						<colgroup>
							<col className="w-44" />
							<col className="w-20" />
							<col className="w-24" />
							<col />
						</colgroup>
						<tbody>
							<tr>
								<td
									colSpan={4}
									className="px-4 py-8 text-center text-gray-500 text-sm"
								>
									{t("components.virtualAppLogTable.noEntriesFound")}
								</td>
							</tr>
						</tbody>
					</table>
				</div>
				<div className="flex items-center justify-between px-3 py-2 text-xs text-gray-500 border-t border-gray-800">
					<span>0 {t("components.virtualAppLogTable.entries")}</span>
					<span className="flex items-center gap-2">
						{isLoadingBefore && (
							<span className="text-(--accent)">
								{t("components.virtualAppLogTable.loadingNewer")}
							</span>
						)}
						{isLoadingAfter && (
							<span className="text-(--accent)">
								{t("components.virtualAppLogTable.loadingOlder")}
							</span>
						)}
					</span>
				</div>
			</div>
		);
	}

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
					<tbody>
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
										{formatTimestamp(entry.timestamp)}
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
												{entry.message}
											</div>
										</div>
									</td>
								</tr>
							);
						})}
					</tbody>
				</table>
			</div>
			<div className="flex items-center justify-between px-3 py-2 text-xs text-gray-500 border-t border-gray-800">
				<span>
					{entries.length > 0
						? `${Math.max(1, virtualItems[0]?.index + 1 || 1)}–${Math.max(0, virtualItems[virtualItems.length - 1]?.index + 1 || 0)} / ${total.toLocaleString()}`
						: t("components.virtualAppLogTable.zeroEntries")}
				</span>
				<span className="flex items-center gap-2">
					{isLoadingBefore && (
						<span className="text-(--accent)">
							{t("components.virtualAppLogTable.loadingNewer")}
						</span>
					)}
					{isLoadingAfter && (
						<span className="text-(--accent)">
							{t("components.virtualAppLogTable.loadingOlder")}
						</span>
					)}
				</span>
			</div>
		</div>
	);
}
