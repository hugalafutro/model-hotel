import { useVirtualizer } from "@tanstack/react-virtual";
import { useCallback, useLayoutEffect, useRef, useState } from "react";
import type { AppLogEntry } from "../api/types";
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
	sortDir: string; // "asc" or "desc"
	onSortToggle: () => void;
}

const getLevelBadgeVariant = (level: string) => {
	switch (level) {
		case "error":
			return "error" as const;
		case "warning":
			return "warning" as const;
		default:
			return "info" as const;
	}
};

const getSourceBadgeClasses = (source: string) => {
	switch (source) {
		case "auth":
			return "bg-purple-900/30 text-purple-400";
		case "proxy":
			return "bg-cyan-900/30 text-cyan-400";
		case "resolve":
			return "bg-teal-900/30 text-teal-400";
		case "discovery":
			return "bg-emerald-900/30 text-emerald-400";
		case "failover":
			return "bg-slate-700/50 text-slate-300";
		case "ratelimit":
			return "bg-amber-900/30 text-amber-400";
		case "vkey":
		case "admin":
			return "bg-pink-900/30 text-pink-400";
		case "settings":
			return "bg-indigo-900/30 text-indigo-400";
		case "events":
			return "bg-violet-900/30 text-violet-400";
		case "docker":
			return "bg-sky-900/30 text-sky-400";
		case "keycache":
		case "model":
		case "provider":
		case "cache":
		case "db":
			return "bg-lime-900/30 text-lime-400";
		case "access":
			return "bg-fuchsia-900/30 text-fuchsia-400";
		case "server":
		case "startup":
		case "retention":
			return "bg-blue-900/30 text-(--accent)";
		case "circuit-breaker":
			return "bg-orange-900/30 text-orange-400";
		case "modelsdev":
			return "bg-rose-900/30 text-rose-400";
		case "applogs":
			return "bg-gray-700/30 text-gray-400";
		default:
			return "bg-gray-800/30 text-gray-400";
	}
};

const formatTimestamp = (ts: string) => {
	try {
		const d = new Date(ts);
		return d.toLocaleString(undefined, {
			year: "numeric",
			month: "2-digit",
			day: "2-digit",
			hour: "2-digit",
			minute: "2-digit",
			second: "2-digit",
			hour12: false,
		});
	} catch {
		return ts;
	}
};

const HEADER_BASE =
	"px-2 py-2 text-left text-xs font-medium uppercase tracking-wider whitespace-nowrap ui-table-header-text";

const EDGE_THRESHOLD_PX = 500; // pixels from edge to trigger fetch

export function VirtualAppLogTable(props: VirtualAppLogTableProps) {
	"use no memo";

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
		estimateSize: () => 48, // AppLog rows are taller due to min-h-[2lh] and flex layout
		overscan: 20,
		getItemKey: (index) =>
			entries[index].id ??
			`${entries[index].timestamp}-${entries[index].source}-${entries[index].message.slice(0, 20)}`,
	});

	const virtualItems = virtualizer.getVirtualItems();

	const prevEntriesRef = useRef(entries);
	// State counter to force synchronous re-render after scrollTop adjustment.
	// React guarantees setState inside useLayoutEffect is flushed before paint.
	const [, forceRerender] = useState(0);

	// When items are prepended (fetchNewer), all item indices shift but
	// scrollTop stays the same, so the virtualizer maps the old scroll
	// position to different items. Adjust scrollTop by the average of
	// the virtualizer's measured row sizes (from measureElement /
	// ResizeObserver), falling back to estimateSize when no measurements
	// exist yet. Then force a synchronous re-render so the virtualizer
	// recomputes before the browser paints.
	useLayoutEffect(() => {
		const prev = prevEntriesRef.current;
		if (entries.length > prev.length && prev.length > 0) {
			const newItemCount = entries.length - prev.length;
			if (entries[newItemCount]?.id === prev[0]?.id && scrollRef.current) {
				const cache = virtualizer.measurementsCache;
				const avgSize =
					cache.length > 0
						? cache.reduce((sum, m) => sum + m.size, 0) / cache.length
						: 48;
				scrollRef.current.scrollTop += newItemCount * avgSize;
				prevEntriesRef.current = entries;
				forceRerender((c) => c + 1);
				return;
			}
		}
		prevEntriesRef.current = entries;
	}, [entries, virtualizer.measurementsCache]);

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
									No log entries found
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
					<thead className="sticky top-0 z-10 bg-(--surface)">
						<tr>
							<th
								className={`${HEADER_BASE} cursor-pointer`}
								onClick={onSortToggle}
							>
								Time/Date {sortDir === "desc" ? "↓" : "↑"}
							</th>
							<th className={HEADER_BASE}>Level</th>
							<th className={HEADER_BASE}>Source</th>
							<th className={HEADER_BASE}>Message</th>
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
