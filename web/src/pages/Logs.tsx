import {
	keepPreviousData,
	useQuery,
	useQueryClient,
} from "@tanstack/react-query";
import { CalendarDays, FileText, ScrollText, X } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import { api } from "../api/client";
import type { LogEntry } from "../api/types";
import { Badge } from "../components/Badge";
import type { SortState } from "../components/DataTable";
import {
	EmptyRow,
	PaginationBar,
	Row,
	SortableHeader,
	StaticHeader,
} from "../components/DataTable";
import { FilterDropdown } from "../components/FilterDropdown";
import { FilterInput } from "../components/FilterInput";
import { LoadingSpinner } from "../components/LoadingSpinner";
import { LogDetailModal } from "../components/LogDetailModal";
import { PageHeader } from "../components/PageHeader";
import { useSidebarMode } from "../context/SidebarModeContext";
import { useToast } from "../context/ToastContext";
import { useDebounce } from "../hooks/useDebounce";
import { AppLogs } from "./AppLogs";
import { AccentCalendar } from "./Logs/AccentCalendar";

import {
	formatDateRangeShort,
	formatMs,
	formatTPS,
	todayISO,
} from "./Logs/utils";

/* =========================================================
   Main Logs page
   ===================================================== */
function RequestLogs() {
	type LogSortField =
		| "time"
		| "model"
		| "provider"
		| "status"
		| "tokens"
		| "tps"
		| "ttft"
		| "duration"
		| "overhead"
		| "key";

	const { logsSubMode, setLogsSubMode } = useSidebarMode();
	const [page, setPage] = useState(1);
	const [pageSize, setPageSize] = useState(20);
	const [filters, setFilters] = useState({
		model_id: "",
		provider_id: "",
		status_code: "",
	});
	const debouncedModelId = useDebounce(filters.model_id, 300);
	const debouncedProviderId = useDebounce(filters.provider_id, 300);
	const [dateFrom, setDateFrom] = useState("");
	const [dateTo, setDateTo] = useState("");
	const [sort, setSort] = useState<SortState<LogSortField>>({
		field: "time",
		dir: "desc",
	});
	const [showDatePicker, setShowDatePicker] = useState(false);
	const [pendingFrom, setPendingFrom] = useState("");
	const [pendingTo, setPendingTo] = useState("");

	const datePickerRef = useRef<HTMLDivElement>(null);
	const [selectedLog, setSelectedLog] = useState<LogEntry | null>(null);
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
				event.type === "request.completed"
			) {
				queryClient.invalidateQueries({ queryKey: ["logs"] });
			}
		};
		window.addEventListener("server-event", handler);
		return () => window.removeEventListener("server-event", handler);
	}, [liveEnabled, queryClient]);
	const { toast } = useToast();

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
				from: dateFrom || undefined,
				to: dateTo || undefined,
				sort_by: sort.field,
				sort_dir: sort.dir,
			}),
		refetchInterval: liveEnabled && isVisible ? 30000 : false,
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

	useEffect(() => {
		function handleClickOutside(e: MouseEvent) {
			if (
				datePickerRef.current &&
				!datePickerRef.current.contains(e.target as Node)
			) {
				setShowDatePicker(false);
			}
		}
		if (showDatePicker) {
			document.addEventListener("mousedown", handleClickOutside);
			return () =>
				document.removeEventListener("mousedown", handleClickOutside);
		}
	}, [showDatePicker]);

	const now = new Date();
	const pickerYear = showDatePicker
		? new Date(pendingFrom || todayISO()).getFullYear()
		: now.getFullYear();
	const pickerMonth = showDatePicker
		? new Date(pendingFrom || todayISO()).getMonth()
		: now.getMonth();

	const handleCalendarSelect = (dStr: string) => {
		if (!pendingFrom || (pendingFrom && pendingTo)) {
			setPendingFrom(dStr);
			setPendingTo("");
		} else if (dStr < pendingFrom) {
			setPendingTo(pendingFrom);
			setPendingFrom(dStr);
		} else {
			setPendingTo(dStr);
		}
	};

	const applyDateFilter = () => {
		if (pendingFrom) {
			// Construct dates in the browser's local timezone so the filter
			// range matches what the user sees via toLocaleString() rather
			// than UTC (which would shift near midnight).
			setDateFrom(new Date(`${pendingFrom}T00:00:00`).toISOString());
			if (pendingTo && pendingTo >= pendingFrom) {
				setDateTo(new Date(`${pendingTo}T23:59:59.999`).toISOString());
			} else {
				setDateTo(new Date(`${pendingFrom}T23:59:59.999`).toISOString());
			}
		} else {
			setDateFrom("");
			setDateTo("");
		}
		setShowDatePicker(false);
		setPage(1);
	};

	const clearDateFilter = () => {
		setDateFrom("");
		setDateTo("");
		setPendingFrom("");
		setPendingTo("");
		setShowDatePicker(false);
		setPage(1);
	};

	const toggleDatePicker = () => {
		if (!showDatePicker) {
			setPendingFrom(dateFrom ? dateFrom.split("T")[0] : "");
			setPendingTo(dateTo ? dateTo.split("T")[0] : "");
		}
		setShowDatePicker((s) => !s);
	};

	const isCancelled = (errorMessage?: string) => {
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
	useEffect(() => {
		const id = setInterval(() => {
			setNowMs(Date.now());
		}, 60_000);
		return () => clearInterval(id);
	}, []);

	const isStale = (log: LogEntry) => {
		if (log.state !== "pending" && log.state !== "streaming") return false;
		const age = nowMs - new Date(log.created_at).getTime();
		return age > STALE_THRESHOLD_MS;
	};

	const isInProgress = (log: LogEntry) =>
		!isStale(log) && (log.state === "pending" || log.state === "streaming");

	const hasDateFilter = !!dateFrom && !!dateTo;

	return (
		<div className="space-y-4">
			{selectedLog && (
				<LogDetailModal
					log={selectedLog}
					type="request"
					onClose={() => setSelectedLog(null)}
				/>
			)}

			<PageHeader
				icon={ScrollText}
				title="Requests"
				description="Monitor API requests across all providers and keys"
				badge={
					<button
						type="button"
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
					displayTotal > 0 ? (
						<PaginationBar
							page={page}
							totalPages={Math.ceil(displayTotal / pageSize)}
							totalItems={displayTotal}
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

			{/* Controls */}
			<div className="ui-card p-4 shrink-0">
				<div className="flex items-center justify-between">
					<div className="flex items-center gap-1">
						<button
							type="button"
							onClick={() => setLogsSubMode("request")}
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
							onClick={() => setLogsSubMode("app")}
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
						<FilterInput
							value={filters.model_id}
							onChange={(v) => {
								setFilters({ ...filters, model_id: v });
								setPage(1);
							}}
							placeholder="Filter by model ID…"
							className="w-[320px]"
							autoFocus
						/>
						<FilterInput
							value={filters.provider_id}
							onChange={(v) => {
								setFilters({ ...filters, provider_id: v });
								setPage(1);
							}}
							placeholder="Filter by provider…"
							className="w-50"
						/>
						<FilterDropdown
							value={filters.status_code}
							onChange={(v) => {
								setFilters({ ...filters, status_code: v });
								setPage(1);
							}}
							placeholder="Status"
							options={[
								{ value: "2xx", label: "2XX" },
								{ value: "4xx", label: "4XX" },
								{ value: "5xx", label: "5XX" },
								{ value: "0", label: "0" },
							]}
							className="w-28"
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
								<div className="absolute left-0 mt-2 w-72 p-4 bg-gray-900 border border-gray-700 rounded-(--radius-card) shadow-2xl z-50">
									<div className="flex items-center justify-between mb-3">
										<span className="text-sm font-semibold text-white">
											Select date range
										</span>
										<button
											type="button"
											onClick={() => setShowDatePicker(false)}
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
											<span className="text-(--accent)">Select end date…</span>
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

			{/* Initial loading state - show spinner when first fetch hasn't arrived */}
			{isLoading && !logsData && <LoadingSpinner />}

			{/* Error state - show message when fetch fails and no fallback data */}
			{error && !logsData && displayEntries.length === 0 && (
				<div className="ui-card p-8 text-center">
					<p className="text-red-400 text-sm">
						Failed to load logs: {(error as Error).message || "Unknown error"}
					</p>
				</div>
			)}

			{(!isLoading || logsData) && (
				<div className="ui-card overflow-x-auto">
					<table className="w-full table-fixed ui-table min-w-250">
						<colgroup>
							<col className="w-30" />
							<col className="w-27" />
							<col className="w-50" />
							<col className="w-25" />
							<col className="w-24" />
							<col className="w-17.5" />
							<col className="w-13.75" />
							<col className="w-16.25" />
							<col className="w-16.25" />
							<col className="w-17.5" />
							<col className="w-25" />
						</colgroup>
						<thead>
							<tr>
								<SortableHeader
									label="Time/Date"
									field="time"
									sort={sort}
									onSort={handleSort}
									tooltip="Timestamp of the request"
								/>
								<StaticHeader tooltip="Unique hash of the request body">
									Hash
								</StaticHeader>
								<SortableHeader
									label="Model"
									field="model"
									sort={sort}
									onSort={handleSort}
									tooltip="Model ID used for the request"
								/>
								<SortableHeader
									label="Provider"
									field="provider"
									sort={sort}
									onSort={handleSort}
									tooltip="Provider handling the request"
								/>
								<SortableHeader
									label="Status"
									field="status"
									sort={sort}
									onSort={handleSort}
									tooltip="HTTP status code of the response"
								/>
								<SortableHeader
									label="Tokens"
									field="tokens"
									sort={sort}
									onSort={handleSort}
									tooltip="Prompt + completion tokens (if available)"
								/>
								<SortableHeader
									label="T/s"
									field="tps"
									sort={sort}
									onSort={handleSort}
									tooltip="Tokens generated per second"
								/>
								<SortableHeader
									label="TTFT"
									field="ttft"
									sort={sort}
									onSort={handleSort}
									tooltip="Time to first token"
								/>
								<SortableHeader
									label="Duration"
									field="duration"
									sort={sort}
									onSort={handleSort}
									tooltip="Total request duration"
								/>
								<SortableHeader
									label="Overhead"
									field="overhead"
									sort={sort}
									onSort={handleSort}
									tooltip="Proxy overhead (parsing, lookups, etc)"
								/>
								<SortableHeader
									label="Key"
									field="key"
									sort={sort}
									onSort={handleSort}
									tooltip="Virtual key used for authentication"
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
															? log.model_id.slice(
																	log.model_id.indexOf("/") + 1,
																)
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
												) : isInProgress(log) && !log.provider_name ? (
													<span className="text-blue-400/60 italic">
														Resolving…
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
													{isStale(log) ? (
														<span className="text-yellow-500/70">⚠</span>
													) : isInProgress(log) ? (
														<span className="text-blue-400">
															{log.state === "streaming" ? "Live" : "…"}
														</span>
													) : (
														log.status_code
													)}
												</Badge>
											</td>
											<td className="px-2 py-1 whitespace-nowrap text-xs text-gray-400 font-mono">
												{isCancelled(log.error_message)
													? "Interrupted"
													: log.tokens_prompt + log.tokens_completion > 0
														? `${log.tokens_prompt}+${log.tokens_completion}`
														: "-"}
											</td>
											<td className="px-2 py-1 whitespace-nowrap text-xs text-gray-400 font-mono">
												{isCancelled(log.error_message)
													? "-"
													: formatTPS(log.tokens_per_second)}
											</td>
											<td className="px-2 py-1 whitespace-nowrap text-xs text-gray-400 font-mono">
												{log.ttft_ms > 0 ? formatMs(log.ttft_ms, 1) : "-"}
											</td>
											<td className="px-2 py-1 whitespace-nowrap text-xs text-gray-400 font-mono">
												{isInProgress(log) && log.duration_ms === 0 ? (
													<span className="inline-block animate-pulse text-blue-400">
														-
													</span>
												) : log.duration_ms > 0 ? (
													log.duration_ms >= 1000 ? (
														`${(log.duration_ms / 1000).toFixed(1)}s`
													) : (
														`${log.duration_ms.toFixed(0)}ms`
													)
												) : (
													"-"
												)}
											</td>
											<td className="px-2 py-1 whitespace-nowrap text-xs font-mono">
												{log.proxy_overhead_ms != null &&
												log.proxy_overhead_ms > 0 ? (
													<span
														className={
															hasOverhead ? "text-(--accent)" : "text-gray-400"
														}
													>
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
										</Row>
									);
								})
							) : (
								<EmptyRow colSpan={11} message="No logs found" />
							)}
						</tbody>
					</table>
				</div>
			)}
		</div>
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
