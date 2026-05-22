import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { CalendarDays, FileText, ScrollText, X } from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";
import { api } from "../api/client";
import type { AppLogEntry } from "../api/types";
import { AccentCalendar } from "../components/AccentCalendar";
import { formatDateRangeShort } from "../components/AccentCalendar.utils";
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
import { VirtualAppLogTable } from "../components/VirtualAppLogTable";
import { useSidebarMode } from "../context/SidebarModeContext";
import { useToast } from "../context/ToastContext";
import { useBidirectionalFetch } from "../hooks/useBidirectionalFetch";
import { useDateRangePicker } from "../hooks/useDateRangePicker";
import { useDebounce } from "../hooks/useDebounce";
import { useLocalStorage } from "../hooks/useLocalStorage";
import { encodeCursor } from "../utils/format";

type AppLogSortField = "time" | "level" | "source" | "message";

export function AppLogs() {
	const { logsSubMode, setLogsSubMode } = useSidebarMode();
	const [liveEnabled, setLiveEnabled] = useState(true);
	const [isVisible, setIsVisible] = useState(!document.hidden);
	useEffect(() => {
		const handler = () => setIsVisible(!document.hidden);
		document.addEventListener("visibilitychange", handler);
		return () => document.removeEventListener("visibilitychange", handler);
	}, []);
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
	const [pageSize, setPageSize] = useLocalStorage("appLogsPageSize", 20);
	const [viewMode, setViewMode] = useLocalStorage<"paginate" | "scroll">(
		"appLogsViewMode",
		"scroll",
	);
	const { toast } = useToast();

	const debouncedSearch = useDebounce(searchFilter, 300);

	const {
		dateFrom,
		dateTo,
		showDatePicker,
		pendingFrom,
		pendingTo,
		datePickerRef,
		hasDateFilter,
		pickerYear,
		pickerMonth,
		handleCalendarSelect,
		applyDateFilter,
		clearDateFilter,
		toggleDatePicker,
		closeDatePicker,
	} = useDateRangePicker(() => setPage(1));

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
			dateFrom,
			dateTo,
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
				from: dateFrom || undefined,
				to: dateTo || undefined,
				sort_by: sort.field,
				sort_dir: sort.dir,
			}),
		refetchInterval:
			viewMode === "paginate" && liveEnabled && isVisible ? 2000 : false,
		placeholderData: keepPreviousData,
	});

	// --- Virtual scroll mode data ---
	const scrollSortDir = sort.dir;

	const cursorFilters = useMemo(
		() => ({
			level: levelFilter !== "all" ? levelFilter : undefined,
			source: sourceFilter !== "all" ? sourceFilter : undefined,
			search: debouncedSearch || undefined,
			from: dateFrom || undefined,
			to: dateTo || undefined,
		}),
		[levelFilter, sourceFilter, debouncedSearch, dateFrom, dateTo],
	);

	const {
		entries: scrollEntries,
		total: scrollTotal,
		hasBefore,
		hasAfter,
		isLoadingInitial: isScrollLoading,
		isLoadingBefore,
		isLoadingAfter,
		fetchNewer: scrollFetchNewer,
		fetchOlder: scrollFetchOlder,
		error: scrollError,
	} = useBidirectionalFetch<AppLogEntry>({
		fetchFn: (params) =>
			api.appLogs.cursor({
				cursor: params.cursor,
				direction: params.direction,
				limit: params.limit,
				sort_dir: params.sort_dir,
				level: params.level as string | undefined,
				source: params.source as string | undefined,
				search: params.search as string | undefined,
				from: params.from as string | undefined,
				to: params.to as string | undefined,
			}),
		filters: cursorFilters,
		sortDir: scrollSortDir,
		getCursor: (entry) =>
			encodeCursor({
				created_at: entry.created_at ?? "",
				id: entry.id ?? "",
			}),
		getId: (entry) =>
			entry.id ??
			`${entry.timestamp}-${entry.source}-${entry.message.slice(0, 20)}`,
	});

	// Slow poll for scroll mode (no SSE events exist for app logs)
	useEffect(() => {
		if (viewMode !== "scroll" || !liveEnabled) return;
		const interval = setInterval(() => {
			if (!document.hidden) {
				scrollFetchNewer();
			}
		}, 5000);
		return () => clearInterval(interval);
	}, [viewMode, liveEnabled, scrollFetchNewer]);

	// Visibility/focus refresh for scroll mode
	useEffect(() => {
		if (viewMode !== "scroll" || !liveEnabled) return;
		const handler = () => {
			if (!document.hidden) {
				scrollFetchNewer();
			}
		};
		document.addEventListener("visibilitychange", handler);
		return () => document.removeEventListener("visibilitychange", handler);
	}, [viewMode, liveEnabled, scrollFetchNewer]);

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
		<>
			{selectedLog && (
				<LogDetailModal
					log={selectedLog}
					type="app"
					onClose={() => setSelectedLog(null)}
				/>
			)}

			<div
				className={`space-y-4 flex flex-col ${viewMode === "scroll" ? "overflow-hidden h-[calc(100dvh-1rem)]" : "flex-1 min-h-0"}`}
			>
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
						viewMode === "paginate" && totalItems > 0 ? (
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
							{/* View mode toggle */}
							<button
								type="button"
								onClick={() =>
									setViewMode(viewMode === "paginate" ? "scroll" : "paginate")
								}
								className={`flex items-center gap-1 px-2 py-1.5 rounded-md text-xs font-medium transition-all border cursor-pointer ${
									viewMode === "scroll"
										? "bg-(--accent)/20 text-(--accent) border-(--accent)/40"
										: "text-gray-400 border-gray-700 hover:text-white hover:border-gray-500"
								}`}
								title={
									viewMode === "paginate"
										? "Switch to scroll mode"
										: "Switch to pagination mode"
								}
								aria-label={
									viewMode === "paginate"
										? "Switch to scroll mode"
										: "Switch to pagination mode"
								}
							>
								{viewMode === "paginate" ? "⇊ Scroll" : "⬡ Pages"}
							</button>
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

							{/* Calendar picker */}
							<div className="relative" ref={datePickerRef}>
								<div className="flex items-center gap-1">
									<button
										type="button"
										onClick={toggleDatePicker}
										className={`flex items-center justify-center h-9 w-9 rounded-(--radius-button) text-sm border transition-colors cursor-pointer ${
											hasDateFilter
												? "bg-(--accent)/15 text-(--accent) border-(--accent)/40 hover:bg-(--accent)/25"
												: "bg-gray-900/40 text-gray-400 border-gray-700/50 hover:text-white hover:border-gray-500"
										}`}
										title={
											hasDateFilter
												? `Date filter: ${formatDateRangeShort(dateFrom, dateTo)} - click to change`
												: "Filter by date range"
										}
										aria-label={
											hasDateFilter
												? `Date filter: ${formatDateRangeShort(dateFrom, dateTo)} - click to change`
												: "Filter by date range"
										}
									>
										<CalendarDays size={16} />
									</button>
									{hasDateFilter && (
										<button
											type="button"
											className="inline-flex items-center justify-center h-9 w-6 rounded-(--radius-button) bg-(--accent)/30 text-(--accent) hover:text-white transition-all cursor-default hover:drop-shadow-[var(--glow-accent-lg)]"
											onClick={clearDateFilter}
											title={`Clear date filter (${formatDateRangeShort(dateFrom, dateTo)})`}
											aria-label={`Clear date filter (${formatDateRangeShort(dateFrom, dateTo)})`}
										>
											<X size={14} />
										</button>
									)}
								</div>

								{showDatePicker && (
									<div className="absolute right-0 mt-2 w-72 p-4 bg-gray-900 border border-gray-700 rounded-(--radius-card) shadow-2xl z-50">
										<div className="flex items-center justify-between mb-3">
											<span className="text-sm font-semibold text-white">
												Select date range
											</span>
											<button
												type="button"
												onClick={() => closeDatePicker()}
												className="text-gray-400 hover:text-white transition-colors leading-none p-1 hover:drop-shadow-[var(--glow-accent-lg)]"
												title="Close date picker"
												aria-label="Close date picker"
											>
												<X size={16} />
											</button>
										</div>

										<AccentCalendar
											initialYear={pickerYear}
											initialMonth={pickerMonth}
											from={pendingFrom}
											to={pendingTo}
											onSelect={handleCalendarSelect}
										/>

										<div className="mt-3 flex items-center justify-between text-xs text-gray-400 min-h-5">
											{pendingFrom && pendingTo ? (
												<span>
													{formatDateRangeShort(pendingFrom, pendingTo)}
												</span>
											) : pendingFrom ? (
												<span className="text-(--accent)">
													Select end date…
												</span>
											) : (
												<span>Select start date</span>
											)}
										</div>

										<div className="flex gap-2 mt-3">
											<button
												type="button"
												onClick={clearDateFilter}
												className="flex-1 px-3 py-1.5 text-xs rounded-lg border border-gray-700 text-gray-400 hover:text-white hover:bg-gray-700 transition-colors"
											>
												Clear
											</button>
											<button
												type="button"
												onClick={applyDateFilter}
												disabled={!pendingFrom}
												className="flex-1 px-3 py-1.5 text-xs rounded-lg border border-(--accent-light) bg-(--accent-light) text-(--accent) hover:brightness-125 transition-colors disabled:opacity-40 disabled:cursor-not-allowed"
											>
												Apply
											</button>
										</div>
									</div>
								)}
							</div>
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

				{viewMode === "paginate" && (!isLoading || historyData) && (
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

				{viewMode === "scroll" && (
					<div className="flex flex-col flex-1 min-h-0">
						{isScrollLoading && scrollEntries.length === 0 && (
							<LoadingSpinner />
						)}
						{scrollError && scrollEntries.length === 0 && (
							<div className="ui-card p-8 text-center">
								<p className="text-red-400 text-sm">
									Failed to load logs: {scrollError}
								</p>
							</div>
						)}
						{(!isScrollLoading || scrollEntries.length > 0) && (
							<VirtualAppLogTable
								entries={scrollEntries}
								total={scrollTotal}
								hasBefore={hasBefore}
								hasAfter={hasAfter}
								isLoadingBefore={isLoadingBefore}
								isLoadingAfter={isLoadingAfter}
								onFetchNewer={scrollFetchNewer}
								onFetchOlder={scrollFetchOlder}
								onRowClick={(entry) => setSelectedLog(entry)}
								sortDir={scrollSortDir}
								onSortToggle={() =>
									setSort((prev) => ({
										field: prev.field,
										dir: prev.dir === "asc" ? "desc" : "asc",
									}))
								}
							/>
						)}
					</div>
				)}
			</div>
		</>
	);
}
