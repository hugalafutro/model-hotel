import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { FileText, ScrollText } from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
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
import {
	DateFilterButton,
	DateRangePickerPopover,
	LiveToggleButton,
	LogsErrorState,
	ViewModeToggle,
} from "../components/logs";
import { PageHeader } from "../components/PageHeader";
import { VirtualAppLogTable } from "../components/VirtualAppLogTable";
import { useSidebarMode } from "../context/SidebarModeContext";
import { useBidirectionalFetch } from "../hooks/useBidirectionalFetch";
import { useDateRangePicker } from "../hooks/useDateRangePicker";
import { useDebounce } from "../hooks/useDebounce";
import { useLocalStorage } from "../hooks/useLocalStorage";
import { encodeCursor } from "../utils/format";
import {
	formatTimestamp,
	getLevelBadgeVariant,
	getSourceBadgeClasses,
} from "../utils/logBadgeUtils";

type AppLogSortField = "time" | "level" | "source" | "message";

export function AppLogs() {
	const { t } = useTranslation();
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
					title={t("applogs.title")}
					description={t("applogs.description")}
					badge={
						<LiveToggleButton enabled={liveEnabled} onToggle={setLiveEnabled} />
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
								label={t("applogs.pagination.label")}
							/>
						) : undefined
					}
				/>

				<div className="ui-card has-dropdown p-4 shrink-0">
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
								{t("applogs.tabs.requests")}
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
								{t("applogs.tabs.logs")}
							</button>
						</div>
						<div className="flex items-center gap-2">
							<ViewModeToggle viewMode={viewMode} onChange={setViewMode} />
							<FilterDropdown
								value={levelFilter === "all" ? "" : levelFilter}
								onChange={(v) => {
									setLevelFilter((v || "all") as typeof levelFilter);
									setPage(1);
								}}
								placeholder={t("applogs.filters.level")}
								allLabel={t("applogs.filters.all", { count: totalItems })}
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
									placeholder={t("applogs.filters.source")}
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
								placeholder={t("applogs.filters.search")}
								className="w-50"
								autoFocus
							/>

							<div className="relative" ref={datePickerRef}>
								<DateFilterButton
									hasDateFilter={hasDateFilter}
									dateFrom={dateFrom}
									dateTo={dateTo}
									onToggleDatePicker={toggleDatePicker}
									onClearDateFilter={clearDateFilter}
								/>
								{showDatePicker && (
									<DateRangePickerPopover
										pickerYear={pickerYear}
										pickerMonth={pickerMonth}
										pendingFrom={pendingFrom}
										pendingTo={pendingTo}
										onCalendarSelect={(dateStr) =>
											handleCalendarSelect(dateStr)
										}
										onApply={applyDateFilter}
										onClear={clearDateFilter}
										onClose={closeDatePicker}
										anchor="right"
									/>
								)}
							</div>
						</div>
					</div>
				</div>

				{isLoading && !historyData && <LoadingSpinner />}

				{error && !historyData && entries.length === 0 && (
					<LogsErrorState
						message={t("applogs.toast.loadFailed", {
							message: error?.message || t("common.unknownError"),
						})}
					/>
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
										label={t("applogs.table.timeDate")}
										field="time"
										sort={sort}
										onSort={handleSort}
									/>
									<SortableHeader
										label={t("applogs.table.level")}
										field="level"
										sort={sort}
										onSort={handleSort}
									/>
									<SortableHeader
										label={t("applogs.table.source")}
										field="source"
										sort={sort}
										onSort={handleSort}
									/>
									<SortableHeader
										label={t("applogs.table.message")}
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
												? t("applogs.emptyState.noEntries")
												: t("applogs.emptyState.noMatch")
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
							<LogsErrorState
								message={t("applogs.toast.scrollLoadFailed", {
									message: scrollError,
								})}
							/>
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
