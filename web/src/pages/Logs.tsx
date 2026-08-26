import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { useCallback, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { ScrollText } from "@/lib/icons";
import { api } from "../api/client";
import type { LogEntry } from "../api/types";
import type { SortState } from "../components/DataTable";
import {
	EmptyRow,
	PaginationBar,
	SortableHeader,
} from "../components/DataTable";
import { LoadingSpinner } from "../components/LoadingSpinner";
import { LogDetailModal } from "../components/LogDetailModal";
import { LiveToggleButton, LogsErrorState } from "../components/logs";
import { LOG_COL_WIDTHS, LOG_TABLE_MIN_W } from "../components/logTableWidths";
import { PageHeader } from "../components/PageHeader";
import { VirtualLogTable } from "../components/VirtualLogTable";
import { useIdentity } from "../context/IdentityContext";
import { useSidebarMode } from "../context/SidebarModeContext";
import { useBidirectionalFetch } from "../hooks/useBidirectionalFetch";
import { useDateRangePicker } from "../hooks/useDateRangePicker";
import { useDebounce } from "../hooks/useDebounce";
import { useLocalStorage } from "../hooks/useLocalStorage";
import { useWheelPaging } from "../hooks/useWheelPaging";
import { encodeCursor } from "../utils/format";
import { AppLogs } from "./AppLogs";
import {
	RequestLogFilters,
	type RequestLogFilterValues,
} from "./Logs/RequestLogFilters";
import { RequestLogRow } from "./Logs/RequestLogRow";
import { type LogSortField, requestLogColumns } from "./Logs/requestLogColumns";
import { useRequestLogLiveUpdates } from "./Logs/useRequestLogLiveUpdates";
import { useStaleClock } from "./Logs/useStaleClock";

function RequestLogs() {
	const { t } = useTranslation();
	const { logsSubMode, setLogsSubMode } = useSidebarMode();
	const [page, setPage] = useState(1);
	const [pageSize, setPageSize] = useLocalStorage("requestLogsPageSize", 20);
	const [filters, setFilters] = useState<RequestLogFilterValues>({
		model_id: "",
		provider_id: "",
		status_code: "",
		endpoint_type: "",
		virtual_key_id: "",
		owner_user_id: "",
	});

	// Owner filter is admin-only: non-admins are already server-scoped to
	// their own keys, so the dropdown would be dead weight for them.
	const { isAdmin, can } = useIdentity();
	const { data: ownerOptions } = useQuery({
		queryKey: ["users"],
		queryFn: () => api.users.list(),
		enabled: isAdmin,
		staleTime: 60_000,
	});
	// Key filter rides the same card: the roster endpoint needs the
	// virtual-keys grant, and the server scopes non-admins to their own keys.
	const { data: keyOptions } = useQuery({
		// Same key as the Virtual Keys page, so VK create/rename/delete
		// invalidations refresh this dropdown too.
		queryKey: ["virtualKeys"],
		queryFn: () => api.virtualKeys.list(),
		enabled: can("virtual_keys"),
		staleTime: 60_000,
	});
	const debouncedModelId = useDebounce(filters.model_id, 300);
	const debouncedProviderId = useDebounce(filters.provider_id, 300);
	const datePicker = useDateRangePicker(() => setPage(1));
	const { dateFrom, dateTo } = datePicker;
	const [sort, setSort] = useState<SortState<LogSortField>>({
		field: "time",
		dir: "desc",
	});

	const [selectedLog, setSelectedLog] = useState<LogEntry | null>(null);
	const [viewMode, setViewMode] = useLocalStorage<"paginate" | "scroll">(
		"requestLogsViewMode",
		"scroll",
	);
	const [liveEnabled, setLiveEnabled] = useState(true);

	const handleSort = useCallback((field: LogSortField) => {
		setSort((prev) => ({
			field,
			dir: prev.field === field && prev.dir === "asc" ? "desc" : "asc",
		}));
		setPage(1);
	}, []);

	const { data: settings } = useQuery({
		queryKey: ["settings"],
		queryFn: () => api.settings.get(),
	});

	// --- Virtual scroll mode data ---
	const scrollSortDir = sort.dir; // same sort direction as pagination

	const cursorFilters = useMemo(
		() => ({
			model_id: debouncedModelId || undefined,
			provider_id: debouncedProviderId || undefined,
			status_code: filters.status_code || undefined,
			endpoint_type: filters.endpoint_type || undefined,
			virtual_key_id: filters.virtual_key_id || undefined,
			owner_user_id: filters.owner_user_id || undefined,
			from: dateFrom || undefined,
			to: dateTo || undefined,
		}),
		[
			debouncedModelId,
			debouncedProviderId,
			filters.status_code,
			filters.endpoint_type,
			filters.virtual_key_id,
			filters.owner_user_id,
			dateFrom,
			dateTo,
		],
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
		mergeEntries: scrollMergeEntries,
		error: scrollError,
	} = useBidirectionalFetch<LogEntry>({
		fetchFn: (params) =>
			api.logs.cursor({
				cursor: params.cursor,
				direction: params.direction,
				limit: params.limit,
				sort_dir: params.sort_dir,
				model_id: params.model_id as string | undefined,
				provider_id: params.provider_id as string | undefined,
				status_code: params.status_code as string | undefined,
				endpoint_type: params.endpoint_type as string | undefined,
				virtual_key_id: params.virtual_key_id as string | undefined,
				owner_user_id: params.owner_user_id as string | undefined,
				from: params.from as string | undefined,
				to: params.to as string | undefined,
			}),
		filters: cursorFilters,
		sortDir: scrollSortDir,
		getCursor: (entry) =>
			encodeCursor({ created_at: entry.created_at, id: entry.id }),
		getId: (entry) => entry.id,
	});

	const { isVisible } = useRequestLogLiveUpdates({
		viewMode,
		liveEnabled,
		fetchNewer: scrollFetchNewer,
		mergeEntries: scrollMergeEntries,
	});

	const {
		data: logsData,
		isLoading,
		error,
	} = useQuery({
		queryKey: [
			"logs",
			page,
			pageSize,
			debouncedModelId,
			debouncedProviderId,
			filters.status_code,
			filters.endpoint_type,
			filters.virtual_key_id,
			filters.owner_user_id,
			dateFrom,
			dateTo,
			sort,
		],
		queryFn: () =>
			api.logs.list({
				page,
				per_page: pageSize,
				model_id: debouncedModelId || undefined,
				provider_id: debouncedProviderId || undefined,
				status_code: filters.status_code || undefined,
				endpoint_type: filters.endpoint_type || undefined,
				virtual_key_id: filters.virtual_key_id || undefined,
				owner_user_id: filters.owner_user_id || undefined,
				from: dateFrom || undefined,
				to: dateTo || undefined,
				sort_by: sort.field,
				sort_dir: sort.dir,
			}),
		refetchInterval:
			viewMode === "paginate" && liveEnabled && isVisible ? 30000 : false,
		refetchIntervalInBackground: false,
		refetchOnWindowFocus: "always",
		placeholderData: keepPreviousData,
	});

	// Distinguish between "no data has arrived yet" (loading) and
	// "data arrived but the result set is empty" (0 matching rows).
	// placeholderData: keepPreviousData handles showing previous data
	// during refetch, so we only need to check if data has arrived.
	const displayEntries = logsData?.entries ?? [];
	const displayTotal = logsData?.total ?? 0;
	const logsTotalPages = Math.ceil(displayTotal / pageSize);
	// Clamp before wheel paging so a prev-nudge always snaps back into range if
	// the page count shrank mid-session (matches AppLogs).
	const logsSafePage = Math.min(page, Math.max(1, logsTotalPages));
	const wheelPagingRef = useWheelPaging<HTMLDivElement>({
		enabled: viewMode === "paginate" && logsTotalPages > 1,
		canPrev: logsSafePage > 1,
		canNext: logsSafePage < logsTotalPages,
		onPrev: () => setPage(logsSafePage - 1),
		onNext: () => setPage(logsSafePage + 1),
	});

	const { nowMs, staleThresholdMs } = useStaleClock(
		settings?.stale_request_timeout,
		displayEntries,
	);
	const columns = requestLogColumns(t);

	return (
		<>
			{selectedLog && (
				<LogDetailModal
					log={selectedLog}
					type="request"
					onClose={() => setSelectedLog(null)}
				/>
			)}

			<div
				className={`space-y-4 flex flex-col ${viewMode === "scroll" ? "overflow-hidden h-[calc(100dvh-1rem)]" : "flex-1 min-h-0"}`}
			>
				<PageHeader
					icon={ScrollText}
					title={t("logs.tabs.requests")}
					description={t("logs.description")}
					badge={
						<LiveToggleButton enabled={liveEnabled} onToggle={setLiveEnabled} />
					}
					actions={
						viewMode === "paginate" && displayTotal > 0 ? (
							<PaginationBar
								page={page}
								totalPages={logsTotalPages}
								totalItems={displayTotal}
								pageSize={pageSize}
								onPageChange={setPage}
								onPageSizeChange={(s) => {
									setPageSize(s);
									setPage(1);
								}}
								label={t("logs.pagination.label")}
							/>
						) : undefined
					}
				/>

				<RequestLogFilters
					logsSubMode={logsSubMode}
					onSubModeChange={setLogsSubMode}
					viewMode={viewMode}
					onViewModeChange={setViewMode}
					filters={filters}
					onFilterChange={(patch) => {
						setFilters({ ...filters, ...patch });
						setPage(1);
					}}
					keyOptions={keyOptions}
					ownerOptions={ownerOptions}
					isAdmin={isAdmin}
					datePicker={datePicker}
				/>

				{/* Initial loading state - show spinner when first fetch hasn't arrived */}
				{isLoading && !logsData && <LoadingSpinner />}

				{/* Error state - show message when fetch fails and no fallback data */}
				{error && !logsData && displayEntries.length === 0 && (
					<LogsErrorState
						message={t("logs.toast.loadFailed", {
							message: (error as Error).message || t("common.unknownError"),
						})}
					/>
				)}

				{viewMode === "paginate" && (!isLoading || logsData) && (
					<div ref={wheelPagingRef} className="ui-card overflow-x-auto">
						<table className={`w-full table-fixed ui-table ${LOG_TABLE_MIN_W}`}>
							<colgroup>
								{LOG_COL_WIDTHS.map((col) => (
									<col key={col.key} className={col.width} />
								))}
							</colgroup>
							<thead>
								<tr>
									{columns.map((col) => (
										<SortableHeader
											key={col.field}
											label={col.label}
											field={col.field}
											sort={sort}
											onSort={handleSort}
											tooltip={col.tooltip}
										/>
									))}
								</tr>
							</thead>
							<tbody>
								{displayEntries && displayEntries.length > 0 ? (
									displayEntries.map((log) => (
										<RequestLogRow
											key={log.id}
											log={log}
											nowMs={nowMs}
											staleThresholdMs={staleThresholdMs}
											onClick={() => setSelectedLog(log)}
										/>
									))
								) : (
									<EmptyRow
										colSpan={12}
										message={t("logs.emptyState.requests")}
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
								message={t("logs.toast.scrollLoadFailed", {
									message: scrollError,
								})}
							/>
						)}
						{(!isScrollLoading || scrollEntries.length > 0) && (
							<VirtualLogTable
								entries={scrollEntries}
								total={scrollTotal}
								hasBefore={hasBefore}
								hasAfter={hasAfter}
								isLoadingBefore={isLoadingBefore}
								isLoadingAfter={isLoadingAfter}
								onFetchNewer={scrollFetchNewer}
								onFetchOlder={scrollFetchOlder}
								onRowClick={(entry) => setSelectedLog(entry)}
								nowMs={nowMs}
								staleThresholdMs={staleThresholdMs}
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

/* =========================================================
    Logs page export - switches between Request Logs and App Logs
   ===================================================== */
export function Logs() {
	const { logsSubMode } = useSidebarMode();

	if (logsSubMode === "app") {
		return <AppLogs />;
	}

	return <RequestLogs />;
}
