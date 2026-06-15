import { useVirtualizer } from "@tanstack/react-virtual";
import { useCallback, useLayoutEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import type { LogEntry } from "../api/types";
import { formatMs, formatTPS } from "../pages/Logs/utils";
import { formatNumber } from "../utils/format";
import { isCancelled } from "../utils/logHelpers";
import { Badge } from "./Badge";
import { EndpointTypeBadge } from "./logs";
import { LOG_COL_WIDTHS } from "./logTableWidths";

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
	sortDir: string;
	onSortToggle: () => void;
}

const getStatusBadgeVariant = (
	statusCode: number,
	log?: { error_kind?: string; error_message?: string },
): "error" | "warning" | "success" | "orange" | "muted" => {
	if (isCancelled(log)) return "warning";
	if (statusCode === 0) return "error";
	if (statusCode >= 200 && statusCode < 300) return "success";
	if (statusCode >= 400 && statusCode < 500) return "orange";
	if (statusCode >= 500) return "error";
	return "muted";
};

const HEADER_BASE =
	"px-2 py-2 text-left text-xs font-medium uppercase tracking-wider whitespace-nowrap ui-table-header-text";

const EDGE_THRESHOLD_PX = 500;

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
		sortDir,
		onSortToggle,
	} = props;

	const scrollRef = useRef<HTMLDivElement>(null);

	// eslint-disable-next-line react-hooks/incompatible-library -- TanStack Virtual returns mutable functions; compiler skips memoization
	const virtualizer = useVirtualizer({
		count: entries.length,
		getScrollElement: () => scrollRef.current,
		estimateSize: () => 29,
		overscan: 20,
		getItemKey: (index) => entries[index].id,
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
			<div className="flex flex-col">
				<div
					ref={scrollRef}
					className="ui-card overflow-y-auto"
					style={{
						overflowAnchor: "none",
						height: "calc(100dvh - 242px)",
						minHeight: "200px",
					}}
				>
					<table className="w-full table-fixed ui-table ui-table-virtual min-w-250">
						<colgroup>
							{LOG_COL_WIDTHS.map((col) => (
								<col key={col.key} className={col.width} />
							))}
						</colgroup>
						<tbody>
							<tr>
								<td
									colSpan={11}
									className="px-4 py-8 text-center text-gray-500 text-sm"
								>
									{t("components.virtualLogTable.noLogsFound")}
								</td>
							</tr>
						</tbody>
					</table>
				</div>
				<div className="flex items-center justify-between px-3 py-2 text-xs text-gray-500 border-t border-gray-800">
					<span>0 {t("components.virtualLogTable.entries")}</span>
					<span className="flex items-center gap-2">
						{isLoadingBefore && (
							<span className="text-(--accent)">
								{t("components.virtualLogTable.loadingNewer")}
							</span>
						)}
						{isLoadingAfter && (
							<span className="text-(--accent)">
								{t("components.virtualLogTable.loadingOlder")}
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
				className="ui-card overflow-y-auto"
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
						{LOG_COL_WIDTHS.map((col) => (
							<col key={col.key} className={col.width} />
						))}
					</colgroup>
					<thead className="sticky top-0 z-10">
						<tr>
							<th
								className={`${HEADER_BASE} cursor-pointer`}
								onClick={onSortToggle}
								title={t("components.virtualLogTable.timeDate")}
							>
								{t("components.virtualLogTable.timeDate")}{" "}
								{sortDir === "desc" ? "↓" : "↑"}
							</th>
							<th
								className={HEADER_BASE}
								title={t("components.virtualLogTable.model")}
							>
								{t("components.virtualLogTable.model")}
							</th>
							<th
								className={HEADER_BASE}
								title={t("components.virtualLogTable.provider")}
							>
								{t("components.virtualLogTable.provider")}
							</th>
							<th
								className={HEADER_BASE}
								title={t("components.virtualLogTable.status")}
							>
								{t("components.virtualLogTable.status")}
							</th>
							<th
								className={HEADER_BASE}
								title={t("components.virtualLogTable.tokens")}
							>
								{t("components.virtualLogTable.tokens")}
							</th>
							<th
								className={HEADER_BASE}
								title={t("components.virtualLogTable.tps")}
							>
								{t("components.virtualLogTable.tps")}
							</th>
							<th
								className={HEADER_BASE}
								title={t("components.virtualLogTable.headers")}
							>
								{t("components.virtualLogTable.headers")}
							</th>
							<th
								className={HEADER_BASE}
								title={t("components.virtualLogTable.ttft")}
							>
								{t("components.virtualLogTable.ttft")}
							</th>
							<th
								className={HEADER_BASE}
								title={t("components.virtualLogTable.duration")}
							>
								{t("components.virtualLogTable.duration")}
							</th>
							<th
								className={HEADER_BASE}
								title={t("components.virtualLogTable.overhead")}
							>
								{t("components.virtualLogTable.overhead")}
							</th>
							<th
								className={HEADER_BASE}
								title={t("components.virtualLogTable.key")}
							>
								{t("components.virtualLogTable.key")}
							</th>
						</tr>
					</thead>
					<tbody>
						{virtualItems.map((vItem) => {
							const log = entries[vItem.index];
							return (
								<tr
									key={vItem.key}
									data-index={vItem.index}
									ref={virtualizer.measureElement}
									className={`hover:bg-(--surface-hover) ${vItem.index % 2 === 1 ? "ui-row-even" : ""} cursor-pointer`}
									onClick={() => onRowClick(log)}
								>
									<td className="px-2 py-1 whitespace-nowrap text-xs text-gray-400 font-mono">
										{log.created_at
											? new Date(log.created_at).toLocaleString()
											: "-"}
									</td>
									<td
										className="px-2 py-1 whitespace-nowrap text-xs text-gray-200 truncate"
										title={
											log.model_id?.startsWith("hotel/") &&
											log.resolved_model_id
												? `${log.model_id} (${log.resolved_model_id})`
												: log.model_id
										}
									>
										<EndpointTypeBadge endpointType={log.endpoint_type} />
										{log.model_id ? (
											log.model_id.startsWith("hotel/") ? (
												<>
													<span className="text-(--accent)">
														{log.model_id}
													</span>
													{log.resolved_model_id && (
														<span className="text-gray-500">
															{" "}
															({log.resolved_model_id})
														</span>
													)}
												</>
											) : log.model_id.includes("/") ? (
												log.model_id.slice(log.model_id.indexOf("/") + 1)
											) : (
												log.model_id
											)
										) : (
											"-"
										)}
									</td>
									<td
										className="px-2 py-1 whitespace-nowrap text-xs text-gray-300 truncate"
										title={log.provider_name || undefined}
									>
										{log.provider_name === "Deleted" ? (
											<span
												className="text-red-400 italic"
												title={t("components.virtualLogTable.deleted")}
											>
												{t("components.virtualLogTable.deleted")}
											</span>
										) : (
											log.provider_name || "-"
										)}
									</td>
									<td className="px-2 py-1 whitespace-nowrap">
										<Badge
											variant={getStatusBadgeVariant(log.status_code, log)}
											className="gap-1 whitespace-nowrap"
										>
											{log.status_code}
										</Badge>
									</td>
									<td className="px-2 py-1 whitespace-nowrap text-xs text-gray-400 font-mono">
										{isCancelled(log) ? (
											t("components.virtualLogTable.interrupted")
										) : log.tokens_prompt + log.tokens_completion > 0 ? (
											<>
												{formatNumber(log.tokens_prompt)}
												<span className="text-gray-600">+</span>
												{formatNumber(log.tokens_completion)}
											</>
										) : (
											"-"
										)}
									</td>
									<td className="px-2 py-1 whitespace-nowrap text-xs font-mono">
										{isCancelled(log) ? (
											"-"
										) : (
											<span
												className={
													log.tokens_prompt_cache_hit > 0
														? "opacity-50"
														: undefined
												}
												title={
													log.tokens_prompt_cache_hit > 0
														? t("components.virtualLogTable.cacheInflated")
														: undefined
												}
											>
												{formatTPS(log.tokens_per_second)}
											</span>
										)}
									</td>
									<td className="px-2 py-1 whitespace-nowrap text-xs text-gray-400 font-mono">
										{isCancelled(log)
											? "-"
											: log.response_header_ms > 0
												? formatMs(log.response_header_ms, 1)
												: "-"}
									</td>
									<td className="px-2 py-1 whitespace-nowrap text-xs text-gray-400 font-mono">
										{isCancelled(log)
											? "-"
											: log.ttft_ms > 0
												? formatMs(log.ttft_ms, 1)
												: "-"}
									</td>
									<td className="px-2 py-1 whitespace-nowrap text-xs text-gray-400 font-mono">
										{log.duration_ms > 0
											? log.duration_ms >= 1000
												? `${(log.duration_ms / 1000).toFixed(1)}s`
												: `${log.duration_ms.toFixed(0)}ms`
											: "-"}
									</td>
									<td className="px-2 py-1 whitespace-nowrap text-xs font-mono">
										{log.proxy_overhead_ms != null &&
										log.proxy_overhead_ms > 0 ? (
											<span className="text-(--accent)">
												{formatMs(log.proxy_overhead_ms)}
											</span>
										) : (
											<span className="text-gray-400">-</span>
										)}
									</td>
									<td
										className="px-2 py-1 text-xs text-gray-400 max-w-[7rem] truncate"
										title={
											log.virtual_key_deleted
												? undefined
												: log.virtual_key_name ||
													log.virtual_key_id ||
													undefined
										}
									>
										{log.virtual_key_deleted ? (
											<span className="text-red-400 italic">
												{t("components.virtualLogTable.deleted")}
											</span>
										) : log.virtual_key_name &&
											log.virtual_key_name.toLowerCase() === "internal" ? (
											<span className="text-gray-400 italic">
												{t("components.virtualLogTable.internal")}
											</span>
										) : (
											log.virtual_key_name || log.virtual_key_id || "-"
										)}
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
						? `${formatNumber(Math.max(1, Math.min((virtualItems[0]?.index ?? 0) + 1, entries.length)))}–${formatNumber(Math.min((virtualItems[virtualItems.length - 1]?.index ?? 0) + 1, entries.length))} / ${formatNumber(total)}`
						: t("components.virtualLogTable.zeroEntries")}
				</span>
				<span className="flex items-center gap-2">
					{isLoadingBefore && (
						<span className="text-(--accent)">
							{t("components.virtualLogTable.loadingNewer")}
						</span>
					)}
					{isLoadingAfter && (
						<span className="text-(--accent)">
							{t("components.virtualLogTable.loadingOlder")}
						</span>
					)}
				</span>
			</div>
		</div>
	);
}
