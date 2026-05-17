import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { FileText, ScrollText } from "lucide-react";
import { useCallback, useMemo, useState } from "react";
import { api } from "../api/client";
import type { AppLogEntry } from "../api/types";
import { Badge } from "../components/Badge";
import type { SortState } from "../components/DataTable";
import {
	EmptyRow,
	PaginationBar,
	Row,
	SortableHeader,
} from "../components/DataTable";
import { FilterDropdown } from "../components/FilterDropdown";
import { FilterInput } from "../components/FilterInput";
import { LoadingSpinner } from "../components/LoadingSpinner";
import { LogDetailModal } from "../components/LogDetailModal";
import { PageHeader } from "../components/PageHeader";
import { useSidebarMode } from "../context/SidebarModeContext";
import { useToast } from "../context/ToastContext";
import { useDebounce } from "../hooks/useDebounce";

type AppLogSortField = "time" | "level" | "source" | "message";

export function AppLogs() {
	const { logsSubMode, setLogsSubMode } = useSidebarMode();
	const [liveEnabled, setLiveEnabled] = useState(true);
	const [searchFilter, setSearchFilter] = useState("");
	const [levelFilter, setLevelFilter] = useState<
		"all" | "info" | "warning" | "error"
	>("all");
	const [sourceFilter, setSourceFilter] = useState<string>("all");
	const [sort, setSort] = useState<SortState<AppLogSortField>>({
		field: "time",
		dir: "desc",
	});
	const [selectedLog, setSelectedLog] = useState<AppLogEntry | null>(null);
	const [page, setPage] = useState(1);
	const [pageSize, setPageSize] = useState(20);
	const { toast } = useToast();

	const debouncedSearch = useDebounce(searchFilter, 300);

	const handleSort = useCallback((field: AppLogSortField) => {
		setSort((prev) => ({
			field,
			dir: prev.field === field && prev.dir === "asc" ? "desc" : "asc",
		}));
		setPage(1);
	}, []);

	const {
		data: historyData,
		isLoading,
		error,
	} = useQuery({
		queryKey: [
			"appLogHistory",
			page,
			pageSize,
			levelFilter,
			sourceFilter,
			debouncedSearch,
			sort.field,
			sort.dir,
		],
		queryFn: () =>
			api.appLogs.history({
				page,
				per_page: pageSize,
				level: levelFilter !== "all" ? levelFilter : undefined,
				source: sourceFilter !== "all" ? sourceFilter : undefined,
				search: debouncedSearch || undefined,
				sort_by: sort.field,
				sort_dir: sort.dir,
			}),
		refetchInterval: liveEnabled ? 2000 : false,
		placeholderData: keepPreviousData,
	});

	const entries = useMemo(
		() => historyData?.entries ?? [],
		[historyData?.entries],
	);
	const totalItems = historyData?.total ?? 0;

	const levelCounts = useMemo(() => {
		const serverCounts = historyData?.level_counts;
		if (serverCounts) {
			return {
				info: serverCounts.info ?? 0,
				warning: serverCounts.warning ?? 0,
				error: serverCounts.error ?? 0,
			};
		}
		return { info: 0, warning: 0, error: 0 };
	}, [historyData?.level_counts]);

	const sources = useMemo(() => {
		const serverCounts = historyData?.source_counts;
		if (serverCounts) {
			return Object.keys(serverCounts).sort();
		}
		return [] as string[];
	}, [historyData?.source_counts]);

	const sourceCounts = useMemo(() => {
		return historyData?.source_counts ?? {};
	}, [historyData?.source_counts]);

	const totalPages = Math.max(1, Math.ceil(totalItems / pageSize));
	const safePage = Math.min(page, totalPages);

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
				return "bg-blue-900/30 text-blue-400";
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

	const getLevelColor = (level: string) => {
		switch (level) {
			case "error":
				return "text-red-400";
			case "warning":
				return "text-yellow-400";
			default:
				return "text-blue-400";
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

	return (
		<div className="space-y-4">
			{selectedLog && (
				<LogDetailModal
					log={selectedLog}
					type="app"
					onClose={() => setSelectedLog(null)}
				/>
			)}

			<PageHeader
				icon={FileText}
				title="Logs"
				description="Server application log output"
				badge={
					<button
						type="button"
						aria-label="Toggle live updates"
						onClick={() => {
							setLiveEnabled(!liveEnabled);
							toast(
								liveEnabled ? "Live updates paused" : "Live updates resumed",
								"info",
							);
						}}
						className={`flex items-center gap-1 px-1.5 py-0.5 rounded text-[10px] font-semibold transition-colors ${
							liveEnabled
								? "bg-green-500/20 text-green-400 hover:bg-green-500/30"
								: "bg-gray-700 text-gray-400 hover:bg-gray-600"
						}`}
					>
						<span
							className={`w-1.5 h-1.5 rounded-full transition-colors ${
								liveEnabled ? "bg-green-400" : "bg-gray-500"
							}`}
						/>
						Live
					</button>
				}
				actions={
					totalItems > 0 ? (
						<PaginationBar
							page={safePage}
							totalPages={totalPages}
							totalItems={totalItems}
							pageSize={pageSize}
							onPageChange={setPage}
							onPageSizeChange={(s) => {
								setPageSize(s);
								setPage(1);
							}}
							label="entries"
						/>
					) : undefined
				}
			/>

			<div className="ui-card p-4 shrink-0">
				<div className="flex items-center justify-between">
					<div className="flex items-center gap-1">
						<button
							type="button"
							onClick={() => {
								setLogsSubMode("request");
								setSourceFilter("all");
							}}
							className={`px-3 py-1 rounded-md text-xs font-medium transition-all ${
								logsSubMode === "request"
									? "bg-(--accent)/20 text-(--accent) border border-(--accent)/40 cursor-default"
									: "text-(--text-tertiary) hover:text-(--text-secondary) border border-transparent cursor-pointer"
							}`}
						>
							<ScrollText size={12} className="inline mr-1 -mt-0.5" />
							Requests
						</button>
						<button
							type="button"
							onClick={() => {
								setLogsSubMode("app");
								setSourceFilter("all");
							}}
							className={`px-3 py-1 rounded-md text-xs font-medium transition-all ${
								logsSubMode === "app"
									? "bg-(--accent)/20 text-(--accent) border border-(--accent)/40 cursor-default"
									: "text-(--text-tertiary) hover:text-(--text-secondary) border border-transparent cursor-pointer"
							}`}
						>
							<FileText size={12} className="inline mr-1 -mt-0.5" />
							Logs
						</button>
					</div>
					<div className="flex items-center gap-2">
						<FilterDropdown
							value={levelFilter === "all" ? "" : levelFilter}
							onChange={(v) => {
								setLevelFilter((v || "all") as typeof levelFilter);
								setPage(1);
							}}
							placeholder="Level"
							allLabel={`All (${totalItems})`}
							options={(["info", "warning", "error"] as const).map((lvl) => ({
								value: lvl,
								label: lvl.charAt(0).toUpperCase() + lvl.slice(1),
								count: levelCounts[lvl] ?? 0,
							}))}
							className="w-32"
						/>
						{sources.length > 1 && (
							<FilterDropdown
								value={sourceFilter === "all" ? "" : sourceFilter}
								onChange={(v) => {
									setSourceFilter(v || "all");
									setPage(1);
								}}
								placeholder="Source"
								options={sources.map((src) => ({
									value: src,
									label: src,
									count: sourceCounts[src] ?? 0,
								}))}
								className="w-36"
							/>
						)}
						<FilterInput
							value={searchFilter}
							onChange={(v) => {
								setSearchFilter(v);
								setPage(1);
							}}
							placeholder="Filter logs…"
							className="w-50"
							autoFocus
						/>
					</div>
				</div>
			</div>

			{isLoading && !historyData && <LoadingSpinner />}

			{error && !historyData && entries.length === 0 && (
				<div className="ui-card p-8 text-center">
					<p className="text-red-400 text-sm">
						Failed to load logs: {error?.message || "Unknown error"}
					</p>
				</div>
			)}

			{(!isLoading || historyData) && (
				<div className="ui-card overflow-x-auto">
					<table className="w-full table-fixed ui-table">
						<colgroup>
							<col className="w-44" />
							<col className="w-20" />
							<col className="w-24" />
							<col />
						</colgroup>
						<thead>
							<tr>
								<SortableHeader
									label="Time/Date"
									field="time"
									sort={sort}
									onSort={handleSort}
								/>
								<SortableHeader
									label="Level"
									field="level"
									sort={sort}
									onSort={handleSort}
								/>
								<SortableHeader
									label="Source"
									field="source"
									sort={sort}
									onSort={handleSort}
								/>
								<SortableHeader
									label="Message"
									field="message"
									sort={sort}
									onSort={handleSort}
								/>
							</tr>
						</thead>
						<tbody>
							{entries.length > 0 ? (
								entries.map((entry) => (
									<Row
										key={entry.timestamp}
										onClick={() => setSelectedLog(entry)}
									>
										<td className="px-4 py-2 whitespace-nowrap text-xs text-gray-400">
											{formatTimestamp(entry.timestamp)}
										</td>
										<td className="px-4 py-2">
											<Badge variant={getLevelBadgeVariant(entry.level)}>
												{entry.level.toUpperCase()}
											</Badge>
										</td>
										<td className="px-4 py-2">
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
										<td className="px-4 py-2">
											<div
												className={`text-xs font-mono line-clamp-2 ${getLevelColor(entry.level)}`}
											>
												{entry.message}
											</div>
										</td>
									</Row>
								))
							) : (
								<EmptyRow
									colSpan={4}
									message={
										totalItems === 0
											? "No log entries yet - logs will appear here as the server generates output"
											: "No entries match your filter"
									}
								/>
							)}
						</tbody>
					</table>
				</div>
			)}
		</div>
	);
}
