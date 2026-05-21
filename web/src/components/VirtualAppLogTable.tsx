import { useVirtualizer } from "@tanstack/react-virtual";
import { useCallback, useRef } from "react";
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

const EDGE_THRESHOLD_PX = 500; // pixels from edge to trigger fetch

export function VirtualAppLogTable(props: VirtualAppLogTableProps) {
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
		estimateSize: () => 48, // AppLog rows are taller due to min-h-[2lh] and flex layout
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
					<table
						className="w-full table-fixed ui-table min-w-250"
						style={{ marginBottom: "8px" }}
					>
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
					className="w-full table-fixed ui-table min-w-250"
					style={{ marginBottom: "8px" }}
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
								className="px-2 py-2 text-left text-xs font-semibold text-gray-400 uppercase tracking-wider cursor-pointer"
								onClick={onSortToggle}
							>
								Time/Date {sortDir === "desc" ? "↓" : "↑"}
							</th>
							<th className="px-2 py-2 text-left text-xs font-semibold text-gray-400 uppercase tracking-wider">
								Level
							</th>
							<th className="px-2 py-2 text-left text-xs font-semibold text-gray-400 uppercase tracking-wider">
								Source
							</th>
							<th className="px-2 py-2 text-left text-xs font-semibold text-gray-400 uppercase tracking-wider">
								Message
							</th>
						</tr>
					</thead>
					<tbody>
						{paddingTop > 0 && (
							<tr>
								<td
									colSpan={4}
									style={{ height: paddingTop, padding: 0, border: "none" }}
								/>
							</tr>
						)}
						{virtualItems.map((vItem) => {
							const entry = entries[vItem.index];
							return (
								<tr
									key={entry.id ?? `${vItem.index}`}
									data-index={vItem.index}
									className="hover:bg-(--surface-hover) transition-colors cursor-pointer"
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
						{paddingBottom > 0 && (
							<tr>
								<td
									colSpan={4}
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
