import { useVirtualizer } from "@tanstack/react-virtual";
import { useCallback, useRef } from "react";
import type { LogEntry } from "../api/types";
import { formatMs, formatTPS } from "../pages/Logs/utils";
import { formatNumber } from "../utils/format";
import { Badge } from "./Badge";

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
	sortDir: string; // "asc" or "desc"
	onSortToggle: () => void; // toggle sort direction
}

const isCancelled = (errorMessage?: string): boolean => {
	if (!errorMessage) return false;
	const msg = errorMessage.toLowerCase();
	return (
		msg.includes("cancel") ||
		msg.includes("disconnect") ||
		msg.includes("context canceled")
	);
};

const getStatusBadgeVariant = (
	statusCode: number,
	errorMessage?: string,
): "error" | "warning" | "success" | "orange" | "muted" => {
	if (isCancelled(errorMessage)) return "warning";
	if (statusCode === 0) return "error";
	if (statusCode >= 200 && statusCode < 300) return "success";
	if (statusCode >= 400 && statusCode < 500) return "orange";
	if (statusCode >= 500) return "error";
	return "muted";
};

const EDGE_THRESHOLD_PX = 500; // pixels from edge to trigger fetch

export function VirtualLogTable(props: VirtualLogTableProps) {
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

	const virtualizer = useVirtualizer({
		count: entries.length,
		getScrollElement: () => scrollRef.current,
		estimateSize: () => 34, // approximate row height
		overscan: 20,
	});

	const virtualItems = virtualizer.getVirtualItems();

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
					className="ui-card overflow-y-auto overflow-x-auto"
					style={{
						overflowAnchor: "none",
						height: "calc(100dvh - 210px)",
						minHeight: "200px",
					}}
				>
					<table className="w-full table-fixed ui-table min-w-250">
						<colgroup>
							<col className="w-30" />
							<col className="w-28" />
							<col className="w-50" />
							<col className="w-25" />
							<col className="w-14" />
							<col className="w-21" />
							<col className="w-16.25" />
							<col className="w-16.25" />
							<col className="w-16.25" />
							<col className="w-17.5" />
							<col className="w-25" />
						</colgroup>
						<tbody>
							<tr>
								<td
									colSpan={11}
									className="px-4 py-8 text-center text-gray-500 text-sm"
								>
									No logs found
								</td>
							</tr>
						</tbody>
					</table>
				</div>
				<div className="flex items-center justify-between px-3 py-2 text-xs text-gray-500 border-t border-gray-800">
					<span>0 entries</span>
					<span className="flex items-center gap-2">
						{isLoadingBefore && (
							<span className="text-(--accent)">↻ Loading newer…</span>
						)}
						{isLoadingAfter && (
							<span className="text-(--accent)">↻ Loading older…</span>
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
					className="w-full table-fixed ui-table min-w-250"
					style={{ marginBottom: "8px" }}
				>
					<colgroup>
						<col className="w-30" />
						<col className="w-28" />
						<col className="w-50" />
						<col className="w-25" />
						<col className="w-14" />
						<col className="w-21" />
						<col className="w-16.25" />
						<col className="w-16.25" />
						<col className="w-16.25" />
						<col className="w-17.5" />
						<col className="w-25" />
					</colgroup>
					<thead className="sticky top-0 z-10 bg-(--surface)">
						<tr>
							<th
								className="px-2 py-2 text-left text-xs font-semibold text-gray-400 uppercase tracking-wider cursor-pointer"
								onClick={onSortToggle}
							>
								Time/Date {sortDir === "desc" ? "↓" : "↑"}
							</th>
							<th className="px-2 py-2 text-left text-xs font-semibold text-gray-400 uppercase tracking-wider">
								Hash
							</th>
							<th className="px-2 py-2 text-left text-xs font-semibold text-gray-400 uppercase tracking-wider">
								Model
							</th>
							<th className="px-2 py-2 text-left text-xs font-semibold text-gray-400 uppercase tracking-wider">
								Provider
							</th>
							<th className="px-2 py-2 text-left text-xs font-semibold text-gray-400 uppercase tracking-wider">
								Status
							</th>
							<th className="px-2 py-2 text-left text-xs font-semibold text-gray-400 uppercase tracking-wider">
								Tokens
							</th>
							<th className="px-2 py-2 text-left text-xs font-semibold text-gray-400 uppercase tracking-wider">
								T/s
							</th>
							<th className="px-2 py-2 text-left text-xs font-semibold text-gray-400 uppercase tracking-wider">
								TTFT
							</th>
							<th className="px-2 py-2 text-left text-xs font-semibold text-gray-400 uppercase tracking-wider">
								Duration
							</th>
							<th className="px-2 py-2 text-left text-xs font-semibold text-gray-400 uppercase tracking-wider">
								Overhead
							</th>
							<th className="px-2 py-2 text-left text-xs font-semibold text-gray-400 uppercase tracking-wider">
								Key
							</th>
						</tr>
					</thead>
					<tbody>
						{paddingTop > 0 && (
							<tr>
								<td
									colSpan={11}
									style={{ height: paddingTop, padding: 0, border: "none" }}
								/>
							</tr>
						)}
						{virtualItems.map((vItem) => {
							const log = entries[vItem.index];
							return (
								<tr
									key={log.id}
									data-index={vItem.index}
									className="hover:bg-(--surface-hover) transition-colors cursor-pointer"
									onClick={() => onRowClick(log)}
								>
									<td className="px-2 py-1 whitespace-nowrap text-xs text-gray-400">
										{log.created_at
											? new Date(log.created_at).toLocaleString()
											: "-"}
									</td>
									<td
										className="px-2 py-1 text-xs font-mono text-gray-400 truncate"
										title={log.request_hash}
									>
										{log.request_hash ? log.request_hash.slice(0, 16) : "-"}
									</td>
									<td
										className="px-2 py-1 whitespace-nowrap text-xs text-gray-200 truncate"
										title={log.model_id}
									>
										{log.model_id
											? log.model_id.startsWith("hotel/")
												? log.model_id
												: log.model_id.includes("/")
													? log.model_id.slice(log.model_id.indexOf("/") + 1)
													: log.model_id
											: "-"}
									</td>
									<td className="px-2 py-1 whitespace-nowrap text-xs text-gray-300 truncate">
										{log.provider_name === "Deleted" ? (
											<span
												className="text-red-400 italic"
												title="Provider was deleted"
											>
												Deleted
											</span>
										) : (
											log.provider_name || "-"
										)}
									</td>
									<td className="px-2 py-1 whitespace-nowrap">
										<Badge
											variant={getStatusBadgeVariant(
												log.status_code,
												log.error_message,
											)}
											className="gap-1 whitespace-nowrap"
										>
											{log.status_code}
										</Badge>
									</td>
									<td className="px-2 py-1 whitespace-nowrap text-xs text-gray-400 font-mono">
										{isCancelled(log.error_message) ? (
											"Interrupted"
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
									<td className="px-2 py-1 whitespace-nowrap text-xs text-gray-400 font-mono">
										{isCancelled(log.error_message)
											? "-"
											: formatTPS(log.tokens_per_second)}
									</td>
									<td className="px-2 py-1 whitespace-nowrap text-xs text-gray-400 font-mono">
										{isCancelled(log.error_message)
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
									<td className="px-2 py-1 whitespace-nowrap text-xs text-gray-400">
										{log.virtual_key_deleted ? (
											<span className="text-red-400 italic">Deleted</span>
										) : log.virtual_key_name &&
											log.virtual_key_name.toLowerCase() === "internal" ? (
											<span className="text-gray-400 italic">internal</span>
										) : (
											log.virtual_key_name || log.virtual_key_id || "-"
										)}
									</td>
								</tr>
							);
						})}
						{paddingBottom > 0 && (
							<tr>
								<td
									colSpan={11}
									style={{ height: paddingBottom, padding: 0, border: "none" }}
								/>
							</tr>
						)}
					</tbody>
				</table>
			</div>
			<div className="flex items-center justify-between px-3 py-2 text-xs text-gray-500 border-t border-gray-800">
				<span>
					{entries.length > 0
						? `${formatNumber(Math.max(1, virtualItems[0]?.index + 1 || 1))}–${formatNumber(virtualItems[virtualItems.length - 1]?.index + 1 || 0)} / ${formatNumber(total)}`
						: "0 entries"}
				</span>
				<span className="flex items-center gap-2">
					{isLoadingBefore && (
						<span className="text-(--accent)">↻ Loading newer…</span>
					)}
					{isLoadingAfter && (
						<span className="text-(--accent)">↻ Loading older…</span>
					)}
				</span>
			</div>
		</div>
	);
}
