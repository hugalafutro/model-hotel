import {
	keepPreviousData,
	useQuery,
	useQueryClient,
} from "@tanstack/react-query";
import { useCallback, useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { FileText, ScrollText } from "@/lib/icons";
import { api } from "../api/client";
import type { LogEntry } from "../api/types";
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
	ENDPOINT_FILTER_OPTIONS,
	EndpointTypeBadge,
	LiveToggleButton,
	LogsErrorState,
	ViewModeToggle,
} from "../components/logs";
import { LOG_COL_WIDTHS } from "../components/logTableWidths";
import { PageHeader } from "../components/PageHeader";
import { VirtualLogTable } from "../components/VirtualLogTable";
import { useIdentity } from "../context/IdentityContext";
import { useSidebarMode } from "../context/SidebarModeContext";
import { useBidirectionalFetch } from "../hooks/useBidirectionalFetch";
import { useDateRangePicker } from "../hooks/useDateRangePicker";
import { useDebounce } from "../hooks/useDebounce";
import { useLocalStorage } from "../hooks/useLocalStorage";
import { useWheelPaging } from "../hooks/useWheelPaging";
import { encodeCursor, formatNumber } from "../utils/format";
import {
	formatDurationCell,
	getRowStatusVariant,
	isCancelled,
	isInProgress as isInProgressShared,
	isStale as isStaleShared,
	liveDurationMs,
} from "../utils/logHelpers";
import { AppLogs } from "./AppLogs";
import { formatMs, formatTPS } from "./Logs/utils";

/* =========================================================
   Main Logs page
   ===================================================== */
function RequestLogs() {
	const { t } = useTranslation();
	type LogSortField =
		| "time"
		| "model"
		| "provider"
		| "status"
		| "tokens"
		| "tps"
		| "response_header_ms"
		| "ttft"
		| "duration"
		| "overhead"
		| "key";

	const { logsSubMode, setLogsSubMode } = useSidebarMode();
	const [page, setPage] = useState(1);
	const [pageSize, setPageSize] = useLocalStorage("requestLogsPageSize", 20);
	const [filters, setFilters] = useState({
		model_id: "",
		provider_id: "",
		status_code: "",
		endpoint_type: "",
		owner_user_id: "",
	});

	// Owner filter is admin-only: non-admins are already server-scoped to
	// their own keys, so the dropdown would be dead weight for them.
	const { isAdmin } = useIdentity();
	const { data: ownerOptions } = useQuery({
		queryKey: ["users"],
		queryFn: () => api.users.list(),
		enabled: isAdmin,
		staleTime: 60_000,
	});
	const debouncedModelId = useDebounce(filters.model_id, 300);
	const debouncedProviderId = useDebounce(filters.provider_id, 300);
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
	const [isVisible, setIsVisible] = useState(!document.hidden);
	useEffect(() => {
		const handler = () => setIsVisible(!document.hidden);
		document.addEventListener("visibilitychange", handler);
		return () => document.removeEventListener("visibilitychange", handler);
	}, []);

	const queryClient = useQueryClient();

	useEffect(() => {
		if (!liveEnabled) return;
		const handler = (e: Event) => {
			const event = (e as CustomEvent).detail;
			if (
				event.type === "request.started" ||
				event.type === "request.streaming" ||
				event.type === "request.completed"
			) {
				if (viewMode === "paginate") {
					queryClient.invalidateQueries({ queryKey: ["logs"] });
				}
			}
		};
		window.addEventListener("server-event", handler);
		return () => window.removeEventListener("server-event", handler);
	}, [liveEnabled, queryClient, viewMode]);
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

	// --- Virtual scroll mode data ---
	const scrollSortDir = sort.dir; // same sort direction as pagination

	const cursorFilters = useMemo(
		() => ({
			model_id: debouncedModelId || undefined,
			provider_id: debouncedProviderId || undefined,
			status_code: filters.status_code || undefined,
			endpoint_type: filters.endpoint_type || undefined,
			owner_user_id: filters.owner_user_id || undefined,
			from: dateFrom || undefined,
			to: dateTo || undefined,
		}),
		[
			debouncedModelId,
			debouncedProviderId,
			filters.status_code,
			filters.endpoint_type,
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

	// Slow poll fallback for scroll mode (catches SSE disconnects)
	useEffect(() => {
		if (viewMode !== "scroll" || !liveEnabled) return;
		const interval = setInterval(() => {
			if (!document.hidden) {
				scrollFetchNewer();
			}
		}, 60000);
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

	// SSE-driven live updates for scroll mode
	useEffect(() => {
		if (viewMode !== "scroll" || !liveEnabled) return;
		const handler = async (e: Event) => {
			const event = (e as CustomEvent).detail;
			if (event.type === "request.completed") {
				// Fetch the completed entry by ID and merge it into
				// the existing entries so the row shows final metrics
				// (provider, tokens, duration, etc.) instead of the
				// placeholder values from the initial INSERT.
				const requestId: string | undefined = event.metadata?.request_id;
				if (requestId) {
					try {
						const entry = await api.logs.get(requestId);
						scrollMergeEntries([entry]);
					} catch {
						// Fall back to fetchNewer on error (e.g. row
						// was purged between event and fetch)
						scrollFetchNewer();
					}
				} else {
					// Fallback when request_id is missing from the
					// event payload (e.g. schema change, old server)
					scrollFetchNewer();
				}
				// Always fetchNewer after request.completed to cover
				// the race where the pending row hasn't been added to
				// the list yet (request.started fetch still in-flight).
				// fetchNewer is guarded against concurrent calls.
				scrollFetchNewer();
			} else if (event.type === "request.streaming") {
				// The provider just committed mid-stream: fetch the row by
				// ID and merge so it swaps "Resolving" for the real
				// provider/model (and the "Streaming" state) without waiting
				// for request.completed. mergeEntries only updates rows that
				// are already in the list, so the trailing fetchNewer covers
				// the race where the pending row hasn't landed yet.
				const requestId: string | undefined = event.metadata?.request_id;
				if (requestId) {
					try {
						const entry = await api.logs.get(requestId);
						scrollMergeEntries([entry]);
					} catch {
						// Fall back to fetchNewer on error (e.g. row
						// was purged between event and fetch)
						scrollFetchNewer();
						return;
					}
				} else {
					// Fallback when request_id is missing from the
					// event payload (e.g. schema change, old server)
					scrollFetchNewer();
					return;
				}
				// Always fetchNewer after request.streaming to cover
				// the race where the pending row hasn't been added to
				// the list yet (request.started fetch still in-flight).
				// fetchNewer is guarded against concurrent calls.
				scrollFetchNewer();
			} else if (event.type === "request.started") {
				scrollFetchNewer();
			}
		};
		window.addEventListener("server-event", handler);
		return () => window.removeEventListener("server-event", handler);
	}, [viewMode, liveEnabled, scrollFetchNewer, scrollMergeEntries]);

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

	// A request stuck in pending/streaming longer than the configured timeout
	// is almost certainly dead (server crash, unhandled error, etc.) - treat it
	// as stale rather than showing a permanently pulsing "Resolving…" / "Live" row.
	// Default 30m to accommodate providers with long time-to-first-token.
	// The setting is stored as a Go duration string (e.g. "30m0s", "1h0m0s").
	const parseGoDuration = (d: string): number => {
		let ms = 0;
		const h = d.match(/(\d+)h/);
		const m = d.match(/(\d+)m(?!s)/);
		const s = d.match(/(\d+)s/);
		if (h) ms += parseInt(h[1], 10) * 3600000;
		if (m) ms += parseInt(m[1], 10) * 60000;
		if (s) ms += parseInt(s[1], 10) * 1000;
		return ms;
	};
	const staleMs = parseGoDuration(settings?.stale_request_timeout || "30m0s");
	const STALE_THRESHOLD_MS = staleMs > 0 ? staleMs : 30 * 60 * 1000;
	const [nowMs, setNowMs] = useState(() => Date.now());
	// In-progress rows show a live-ticking duration, so when any are present we
	// tick every second; otherwise a coarse 60s tick is enough to age rows into
	// the "stale" state without re-rendering the table needlessly.
	const hasLiveEntries = displayEntries.some(
		(log) => log.state === "pending" || log.state === "streaming",
	);
	useEffect(() => {
		const id = setInterval(
			() => {
				setNowMs(Date.now());
			},
			hasLiveEntries ? 1_000 : 60_000,
		);
		return () => clearInterval(id);
	}, [hasLiveEntries]);

	const isStale = (log: LogEntry) =>
		isStaleShared(log, nowMs, STALE_THRESHOLD_MS);

	const isInProgress = (log: LogEntry) =>
		isInProgressShared(log, nowMs, STALE_THRESHOLD_MS);

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

				{/* Controls */}
				<div className="ui-card has-dropdown p-4 shrink-0">
					<div className="flex items-center justify-between">
						<div className="flex items-center gap-1">
							<button
								type="button"
								onClick={() => setLogsSubMode("request")}
								className={`ui-btn px-3 py-1 text-xs font-medium transition-all ${
									logsSubMode === "request"
										? "ui-btn-primary cursor-default"
										: "ui-btn-secondary"
								}`}
							>
								<ScrollText size={12} className="inline mr-1 -mt-0.5" />
								{t("logs.tabs.requests")}
							</button>
							<button
								type="button"
								onClick={() => setLogsSubMode("app")}
								className={`ui-btn px-3 py-1 text-xs font-medium transition-all ${
									logsSubMode === "app"
										? "ui-btn-primary cursor-default"
										: "ui-btn-secondary"
								}`}
							>
								<FileText size={12} className="inline mr-1 -mt-0.5" />
								{t("logs.tabs.logs")}
							</button>
						</div>
						<div className="flex items-center gap-2">
							<ViewModeToggle viewMode={viewMode} onChange={setViewMode} />
							<FilterInput
								value={filters.model_id}
								onChange={(v) => {
									setFilters({ ...filters, model_id: v });
									setPage(1);
								}}
								placeholder={t("logs.filters.modelPlaceholder")}
								className="w-50"
								autoFocus
							/>
							<FilterInput
								value={filters.provider_id}
								onChange={(v) => {
									setFilters({ ...filters, provider_id: v });
									setPage(1);
								}}
								placeholder={t("logs.filters.providerPlaceholder")}
								className="w-50"
							/>
							<FilterDropdown
								value={filters.status_code}
								onChange={(v) => {
									setFilters({ ...filters, status_code: v });
									setPage(1);
								}}
								placeholder={t("logs.filters.status")}
								allLabel={t("logs.filters.allStatus")}
								options={[
									{ value: "2xx", label: "2XX" },
									{ value: "4xx", label: "4XX" },
									{ value: "5xx", label: "5XX" },
									{ value: "0", label: "0" },
								]}
								className="w-36"
							/>
							<FilterDropdown
								value={filters.endpoint_type}
								onChange={(v) => {
									setFilters({ ...filters, endpoint_type: v });
									setPage(1);
								}}
								placeholder={t("logs.filters.endpoint")}
								allLabel={t("logs.filters.allEndpoints")}
								options={ENDPOINT_FILTER_OPTIONS.map((o) => ({
									value: o.value,
									label: t(o.labelKey),
								}))}
								className="w-36"
							/>

							{isAdmin && (ownerOptions?.length ?? 0) > 0 && (
								<FilterDropdown
									value={filters.owner_user_id}
									onChange={(v) => {
										setFilters({ ...filters, owner_user_id: v });
										setPage(1);
									}}
									placeholder={t("logs.filters.owner")}
									allLabel={t("logs.filters.allOwners")}
									options={(ownerOptions ?? []).map((u) => ({
										value: u.id,
										label: u.username,
									}))}
									className="w-36"
								/>
							)}

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
										onCalendarSelect={handleCalendarSelect}
										onApply={applyDateFilter}
										onClear={clearDateFilter}
										onClose={closeDatePicker}
										anchor="right"
										triggerRef={datePickerRef}
									/>
								)}
							</div>
						</div>
					</div>
				</div>

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
						<table className="w-full table-fixed ui-table min-w-250">
							<colgroup>
								{LOG_COL_WIDTHS.map((col) => (
									<col key={col.key} className={col.width} />
								))}
							</colgroup>
							<thead>
								<tr>
									<SortableHeader
										label={t("logs.table.timeDate")}
										field="time"
										sort={sort}
										onSort={handleSort}
										tooltip={t("logs.tooltip.timeDate")}
									/>
									<SortableHeader
										label={t("logs.table.model")}
										field="model"
										sort={sort}
										onSort={handleSort}
										tooltip={t("logs.tooltip.model")}
									/>
									<SortableHeader
										label={t("logs.table.provider")}
										field="provider"
										sort={sort}
										onSort={handleSort}
										tooltip={t("logs.tooltip.provider")}
									/>
									<SortableHeader
										label={t("logs.table.status")}
										field="status"
										sort={sort}
										onSort={handleSort}
										tooltip={t("logs.tooltip.status")}
									/>
									<SortableHeader
										label={t("logs.table.tokens")}
										field="tokens"
										sort={sort}
										onSort={handleSort}
										tooltip={t("logs.tooltip.tokens")}
									/>
									<SortableHeader
										label={t("logs.table.tps")}
										field="tps"
										sort={sort}
										onSort={handleSort}
										tooltip={t("logs.tooltip.tps")}
									/>
									<SortableHeader
										label={t("logs.table.headers")}
										field="response_header_ms"
										sort={sort}
										onSort={handleSort}
										tooltip={t("logs.tooltip.headers")}
									/>
									<SortableHeader
										label={t("logs.table.ttft")}
										field="ttft"
										sort={sort}
										onSort={handleSort}
										tooltip={t("logs.tooltip.ttft")}
									/>
									<SortableHeader
										label={t("logs.table.duration")}
										field="duration"
										sort={sort}
										onSort={handleSort}
										tooltip={t("logs.tooltip.duration")}
									/>
									<SortableHeader
										label={t("logs.table.overhead")}
										field="overhead"
										sort={sort}
										onSort={handleSort}
										tooltip={t("logs.tooltip.overhead")}
									/>
									<SortableHeader
										label={t("logs.table.key")}
										field="key"
										sort={sort}
										onSort={handleSort}
										tooltip={t("logs.tooltip.key")}
									/>
								</tr>
							</thead>
							<tbody>
								{displayEntries && displayEntries.length > 0 ? (
									displayEntries.map((log) => {
										const hasOverhead =
											log.proxy_overhead_ms != null &&
											log.proxy_overhead_ms > 0 &&
											(log.parse_ms > 0 ||
												log.model_lookup_ms > 0 ||
												log.provider_lookup_ms > 0 ||
												log.key_decrypt_ms > 0);
										return (
											<Row
												key={log.id}
												className={
													isInProgress(log) ? "animate-pulse-subtle" : ""
												}
												onClick={() => setSelectedLog(log)}
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
															title={t("logs.table.providerDeleted")}
														>
															{t("logs.table.deletedProvider")}
														</span>
													) : isInProgress(log) && !log.provider_name ? (
														<span className="text-blue-400/60 italic">
															{t("logs.table.resolving")}
														</span>
													) : (
														log.provider_name || "-"
													)}
												</td>
												<td className="px-2 py-1 whitespace-nowrap">
													<Badge
														variant={getRowStatusVariant(
															log,
															nowMs,
															STALE_THRESHOLD_MS,
														)}
														className="gap-1 whitespace-nowrap"
													>
														{isStale(log) ? (
															<span className="text-yellow-500/70">⚠</span>
														) : isInProgress(log) ? (
															<span className="text-blue-400">
																{log.state === "streaming"
																	? t("logs.table.live")
																	: "…"}
															</span>
														) : (
															log.status_code
														)}
													</Badge>
												</td>
												<td className="px-2 py-1 whitespace-nowrap text-xs text-gray-400 font-mono">
													{isCancelled(log) ? (
														t("logs.table.interrupted")
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
																	? t("logs.table.cacheInflated")
																	: undefined
															}
														>
															{formatTPS(log.tokens_per_second)}
														</span>
													)}
												</td>
												<td className="px-2 py-1 whitespace-nowrap text-xs text-gray-400 font-mono">
													{log.response_header_ms > 0
														? formatMs(log.response_header_ms, 1)
														: "-"}
												</td>
												<td className="px-2 py-1 whitespace-nowrap text-xs text-gray-400 font-mono">
													{log.ttft_ms > 0 ? formatMs(log.ttft_ms, 1) : "-"}
												</td>
												<td className="px-2 py-1 whitespace-nowrap text-xs text-gray-400 font-mono">
													{isInProgress(log) && log.duration_ms === 0 ? (
														<span className="inline-block text-blue-400">
															{formatDurationCell(
																liveDurationMs(log.created_at, nowMs),
															)}
														</span>
													) : log.duration_ms > 0 ? (
														formatDurationCell(log.duration_ms)
													) : (
														"-"
													)}
												</td>
												<td className="px-2 py-1 whitespace-nowrap text-xs font-mono">
													{log.proxy_overhead_ms != null &&
													log.proxy_overhead_ms > 0 ? (
														<span
															className={
																hasOverhead
																	? "text-(--accent)"
																	: "text-gray-400"
															}
														>
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
															{t("logs.table.keyDeleted")}
														</span>
													) : log.virtual_key_name &&
														log.virtual_key_name.toLowerCase() ===
															"internal" ? (
														<span className="text-gray-400 italic">
															{t("common.internal")}
														</span>
													) : (
														log.virtual_key_name || log.virtual_key_id || "-"
													)}
												</td>
											</Row>
										);
									})
								) : (
									<EmptyRow
										colSpan={11}
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
								staleThresholdMs={STALE_THRESHOLD_MS}
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
