import { useQuery, keepPreviousData } from "@tanstack/react-query";
import { api } from "../api/client";
import { useEffect, useState, useRef, useCallback } from "react";
import {
    ScrollText,
    X,
    CalendarDays,
    ChevronLeft,
    ChevronRight,
} from "lucide-react";
import type { LogEntry } from "../api/types";
import { FilterInput } from "../components/FilterInput";
import {
    SortableHeader,
    StaticHeader,
    Row,
    EmptyRow,
    PaginationBar,
} from "../components/DataTable";
import type { SortState } from "../components/DataTable";
import { useToast } from "../context/ToastContext";

/* =========================================================
   Date helpers for the accent-themed calendar picker
   ===================================================== */
function toISODate(d: Date): string {
    return d.toISOString().split("T")[0];
}

function todayISO(): string {
    return toISODate(new Date());
}

function daysInMonth(year: number, month: number): number {
    return new Date(year, month + 1, 0).getDate();
}

function firstDayOfMonth(year: number, month: number): number {
    return new Date(year, month, 1).getDay();
}

function pad(n: number): string {
    return n.toString().padStart(2, "0");
}

/* =========================================================
   Small helpers
   ===================================================== */
function formatDateRangeShort(from: string, to: string): string {
    const f = new Date(from);
    const t = new Date(to);
    const sameMonth =
        f.getMonth() === t.getMonth() && f.getFullYear() === t.getFullYear();
    const fd = `${pad(f.getDate())}/${pad(f.getMonth() + 1)}`;
    const td = `${pad(t.getDate())}/${pad(t.getMonth() + 1)}/${t.getFullYear()}`;
    return sameMonth
        ? `${fd}-${td}`
        : `${fd}/${f.getFullYear().toString().slice(2)} - ${td}`;
}

function formatTPS(t: number | null): string {
    if (t == null) return "-";
    return t.toFixed(1);
}

function formatMs(v: number | null | undefined, decimals: number = 2): string {
    if (v == null || v === 0) return "-";
    return v.toFixed(decimals) + "ms";
}

/* =========================================================
   Accent-themed inline calendar
   ===================================================== */
function AccentCalendar({
    initialYear,
    initialMonth,
    from,
    to,
    onSelect,
}: {
    initialYear: number;
    initialMonth: number;
    from: string;
    to: string;
    onSelect: (dateStr: string) => void;
}) {
    const [year, setYear] = useState(initialYear);
    const [month, setMonth] = useState(initialMonth);
    const today = todayISO();

    const days = daysInMonth(year, month);
    const firstDay = firstDayOfMonth(year, month);
    const blanks = firstDay;

    const monthName = new Date(year, month, 1).toLocaleString("en-GB", {
        month: "long",
    });

    const handlePrev = () => {
        if (month === 0) {
            setMonth(11);
            setYear((y) => y - 1);
        } else {
            setMonth((m) => m - 1);
        }
    };

    const handleNext = () => {
        if (month === 11) {
            setMonth(0);
            setYear((y) => y + 1);
        } else {
            setMonth((m) => m + 1);
        }
    };

    const isInRange = (day: number): boolean => {
        if (!from || !to) return false;
        const dStr = `${year}-${pad(month + 1)}-${pad(day)}`;
        return dStr >= from && dStr <= to;
    };

    const isStart = (day: number): boolean => {
        if (!from) return false;
        const dStr = `${year}-${pad(month + 1)}-${pad(day)}`;
        return dStr === from;
    };

    const isEnd = (day: number): boolean => {
        if (!to) return false;
        const dStr = `${year}-${pad(month + 1)}-${pad(day)}`;
        return dStr === to;
    };

    const isSelected = (day: number): boolean => isStart(day) || isEnd(day);

    return (
        <div>
            <div className="flex items-center justify-between mb-3">
                <button
                    type="button"
                    onClick={handlePrev}
                    className="text-gray-400 hover:text-white transition-colors p-1 rounded-(--radius-button) hover:bg-gray-700"
                >
                    <ChevronLeft size={16} />
                </button>
                <span className="text-sm font-semibold text-white">
                    {monthName} {year}
                </span>
                <button
                    type="button"
                    onClick={handleNext}
                    className="text-gray-400 hover:text-white transition-colors p-1 rounded-(--radius-button) hover:bg-gray-700"
                >
                    <ChevronRight size={16} />
                </button>
            </div>
            <div className="grid grid-cols-7 gap-0.5 text-center text-[10px] text-gray-500 mb-1">
                {["Su", "Mo", "Tu", "We", "Th", "Fr", "Sa"].map((d) => (
                    <div key={d}>{d}</div>
                ))}
            </div>
            <div className="grid grid-cols-7 gap-0.5">
                {Array.from({ length: blanks }).map((_, i) => (
                    <div key={`blank-${i}`} />
                ))}
                {Array.from({ length: days }).map((_, i) => {
                    const day = i + 1;
                    const dStr = `${year}-${pad(month + 1)}-${pad(day)}`;
                    const inRange = isInRange(day);
                    const sel = isSelected(day);
                    const isToday = dStr === today;

                    return (
                        <button
                            key={day}
                            type="button"
                            onClick={() => onSelect(dStr)}
                            className={`
                                text-[11px] w-7 h-7                                 rounded-(--radius-button) flex items-center justify-center transition-colors
                                ${
                                    sel
                                        ? "bg-(--accent) text-white font-semibold"
                                        : inRange
                                          ? "bg-(--accent)/20 text-(--accent)"
                                          : isToday
                                            ? "border border-(--accent)/50 text-(--accent)"
                                            : "text-gray-300 hover:bg-gray-700"
                                }
                            `}
                        >
                            {day}
                        </button>
                    );
                })}
            </div>
        </div>
    );
}

/* =========================================================
   Overhead modal
   ===================================================== */
interface OverheadBreakdown {
    proxy_overhead_ms: number;
    parse_ms: number;
    model_lookup_ms: number;
    provider_lookup_ms: number;
    key_decrypt_ms: number;
}

function OverheadModal({
    breakdown,
    onClose,
}: {
    breakdown: OverheadBreakdown;
    onClose: () => void;
}) {
    const total =
        breakdown.parse_ms +
        breakdown.model_lookup_ms +
        breakdown.provider_lookup_ms +
        breakdown.key_decrypt_ms;
    return (
        <div
            className="fixed inset-0 z-50 flex items-center justify-center bg-black/60"
            role="dialog"
            aria-modal="true"
        >
            <div
                className="ui-card relative p-5 min-w-[320px] shadow-2xl"
                role="document"
            >
                <div className="flex justify-between items-center mb-4">
                    <h3 className="text-lg font-semibold text-white">
                        Proxy Overhead Breakdown
                    </h3>
                    <button
                        type="button"
                        onClick={onClose}
                        className="absolute top-4 right-4 text-gray-400 hover:text-white transition-all cursor-default leading-none p-1 hover:drop-shadow-[0_0_8px_var(--accent)]"
                    >
                        <X size={20} />
                    </button>
                </div>
                <div className="space-y-2">
                    <div className="flex justify-between text-sm">
                        <span className="text-gray-400">Request parsing</span>
                        <span className="text-gray-200 font-mono">
                            {formatMs(breakdown.parse_ms)}
                        </span>
                    </div>
                    <div className="flex justify-between text-sm">
                        <span className="text-gray-400">
                            Model/failover lookup
                        </span>
                        <span className="text-gray-200 font-mono">
                            {formatMs(breakdown.model_lookup_ms)}
                        </span>
                    </div>
                    <div className="flex justify-between text-sm">
                        <span className="text-gray-400">Provider lookup</span>
                        <span className="text-gray-200 font-mono">
                            {formatMs(breakdown.provider_lookup_ms)}
                        </span>
                    </div>
                    <div className="flex justify-between text-sm">
                        <span className="text-gray-400">Key decryption</span>
                        <span className="text-gray-200 font-mono">
                            {formatMs(breakdown.key_decrypt_ms)}
                        </span>
                    </div>
                    <div className="border-t border-gray-700 my-2" />
                    <div className="flex justify-between text-sm font-semibold">
                        <span className="text-gray-300">Total overhead</span>
                        <span className="text-(--accent) font-mono">
                            {formatMs(total)}
                        </span>
                    </div>
                </div>
            </div>
        </div>
    );
}

/* =========================================================
   Main Logs page
   ===================================================== */
export function Logs() {
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

    const [page, setPage] = useState(1);
    const [pageSize, setPageSize] = useState(20);
    const [filters, setFilters] = useState({ model_id: "", status_code: "" });
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
    const [overheadBreakdown, setOverheadBreakdown] =
        useState<OverheadBreakdown | null>(null);
    const [liveEnabled, setLiveEnabled] = useState(true);
    const { toast } = useToast();

    const handleSort = useCallback((field: LogSortField) => {
        setSort((prev) => ({
            field,
            dir: prev.field === field && prev.dir === "asc" ? "desc" : "asc",
        }));
        setPage(1);
    }, []);

    const [fallback, setFallback] = useState<{
        entries: LogEntry[];
        total: number;
    }>({ entries: [], total: 0 });

    const { data: settings } = useQuery({
        queryKey: ["settings"],
        queryFn: () => api.settings.get(),
    });

    const { data: logsData } = useQuery({
        queryKey: ["logs", page, pageSize, filters, dateFrom, dateTo, sort],
        queryFn: () =>
            api.logs.list({
                page,
                per_page: pageSize,
                model_id: filters.model_id || undefined,
                status_code: filters.status_code || undefined,
                from: dateFrom || undefined,
                to: dateTo || undefined,
                sort_by: sort.field,
                sort_dir: sort.dir,
            }),
        refetchInterval: liveEnabled ? 2000 : false,
        placeholderData: keepPreviousData,
    });

    // Distinguish between "no data has arrived yet" (loading) and
    // "data arrived but the result set is empty" (0 matching rows).
    // The fallback pattern exists so that during a refetch the previous
    // data is still visible; but when the server legitimately returns
    // zero entries (e.g. filtering for 5XX with no 5XX rows) we must
    // show an empty list, not stale data from a different filter.
    const hasFetchedData = logsData !== undefined;
    const freshEntries = logsData?.entries;
    const freshTotal = logsData?.total ?? 0;

    useEffect(() => {
        if (hasFetchedData && freshEntries) {
            // eslint-disable-next-line
            setFallback({ entries: freshEntries, total: freshTotal });
        }
    }, [hasFetchedData, freshEntries, freshTotal]);

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

    const displayEntries = hasFetchedData
        ? (freshEntries ?? [])
        : fallback.entries;
    const displayTotal = hasFetchedData ? freshTotal : fallback.total;

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
            setDateFrom(pendingFrom + "T00:00:00.000Z");
            if (pendingTo && pendingTo >= pendingFrom) {
                setDateTo(pendingTo + "T23:59:59.999Z");
            } else {
                setDateTo(pendingFrom + "T23:59:59.999Z");
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

    const getStatusBg = (statusCode: number, errorMessage?: string) => {
        if (isCancelled(errorMessage))
            return "bg-yellow-900/30 text-yellow-400";
        if (statusCode === 0) return "bg-red-900/30 text-red-400";
        if (statusCode >= 200 && statusCode < 300)
            return "bg-green-900/30 text-green-400";
        if (statusCode >= 400 && statusCode < 500)
            return "bg-orange-900/30 text-orange-400";
        if (statusCode >= 500) return "bg-red-900/30 text-red-400";
        return "bg-gray-700 text-gray-300";
    };

    // A request stuck in pending/streaming longer than the configured timeout
    // is almost certainly dead (server crash, unhandled error, etc.) — treat it
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
    const [nowMs] = useState(() => Date.now());

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
            {overheadBreakdown && (
                <OverheadModal
                    breakdown={overheadBreakdown}
                    onClose={() => setOverheadBreakdown(null)}
                />
            )}

            <div className="flex justify-between items-center">
                <div>
                    <div className="flex items-center gap-3">
                        <ScrollText
                            size={28}
                            strokeWidth={2}
                            className="text-(--accent)"
                        />
                        <h1 className="text-3xl font-bold text-white">
                            Request Logs
                        </h1>
                        <button
                            type="button"
                            onClick={() => {
                                setLiveEnabled(!liveEnabled);
                                toast(
                                    liveEnabled
                                        ? "Live updates paused"
                                        : "Live updates resumed",
                                    "info",
                                );
                            }}
                            className={`flex items-center gap-2 px-3 py-1.5 rounded-full text-sm transition-colors ${
                                liveEnabled
                                    ? "bg-green-500/20 text-green-400 hover:bg-green-500/30"
                                    : "bg-gray-700 text-gray-400 hover:bg-gray-600"
                            }`}
                        >
                            <span
                                className={`w-2 h-2 rounded-full transition-colors ${
                                    liveEnabled ? "bg-green-400" : "bg-gray-500"
                                }`}
                            />
                            Live
                        </button>
                    </div>
                    <p className="text-gray-400">
                        Monitor API requests across all providers and keys
                    </p>
                </div>
            </div>

            <div className="flex items-start gap-4">
                {/* Left half: filters — fixed width, never shifts */}
                <div className="flex items-center gap-2 shrink-0">
                    <FilterInput
                        value={filters.model_id}
                        onChange={(v) => {
                            setFilters({ ...filters, model_id: v });
                            setPage(1);
                        }}
                        placeholder="Filter by model ID..."
                        className="w-[320px]"
                        autoFocus
                    />
                    <select
                        value={filters.status_code}
                        onChange={(e) => {
                            setFilters({
                                ...filters,
                                status_code: e.target.value,
                            });
                            setPage(1);
                        }}
                        className="ui-input h-9 py-0! w-30! text-xs pr-6"
                    >
                        <option value="">All Status</option>
                        <option value="0">0 No Response</option>
                        <option value="200">200 OK</option>
                        <option value="4xx">4XX</option>
                        <option value="5xx">5XX</option>
                    </select>

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
                                        ? `Date filter: ${formatDateRangeShort(dateFrom, dateTo)} — click to change`
                                        : "Filter by date range"
                                }
                            >
                                <CalendarDays size={16} />
                            </button>
                            {hasDateFilter && (
                                <button
                                    type="button"
                                    className="inline-flex items-center justify-center h-9 w-6 rounded-(--radius-button) bg-(--accent)/30 text-(--accent) hover:text-white transition-all cursor-default hover:drop-shadow-[0_0_8px_var(--accent)]"
                                    onClick={clearDateFilter}
                                    title={`Clear date filter (${formatDateRangeShort(dateFrom, dateTo)})`}
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
                                        className="text-gray-400 hover:text-white transition-colors leading-none p-1 hover:drop-shadow-[0_0_8px_var(--accent)]"
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
                                            {formatDateRangeShort(
                                                pendingFrom,
                                                pendingTo,
                                            )}
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

                {/* Right half: pagination — stretches/shrinks freely */}
                <div className="flex-1 flex justify-end">
                    {displayTotal > 0 && (
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
                    )}
                </div>
            </div>

            <div className="ui-card overflow-x-auto">
                <table className="w-full table-fixed ui-table min-w-250">
                    <colgroup>
                        <col className="w-30" />
                        <col className="w-27" />
                        <col className="w-50" />
                        <col className="w-25" />
                        <col className="w-15" />
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
                                label="Time"
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
                            displayEntries.map((log, idx) => {
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
                                        index={idx}
                                        className={
                                            isInProgress(log)
                                                ? "animate-pulse-subtle"
                                                : ""
                                        }
                                    >
                                        <td className="px-4 py-2 whitespace-nowrap text-xs text-gray-400">
                                            {log.created_at
                                                ? new Date(
                                                      log.created_at,
                                                  ).toLocaleString()
                                                : "-"}
                                        </td>
                                        <td
                                            className="px-4 py-2 whitespace-nowrap text-xs font-mono text-gray-400"
                                            title={log.request_hash}
                                        >
                                            {log.request_hash
                                                ? log.request_hash.slice(0, 16)
                                                : "-"}
                                        </td>
                                        <td
                                            className="px-4 py-2 whitespace-nowrap text-xs text-gray-200 truncate"
                                            title={log.model_id}
                                        >
                                            {log.model_id
                                                ? log.model_id.startsWith(
                                                      "hotel/",
                                                  )
                                                    ? log.model_id
                                                    : log.model_id.includes("/")
                                                      ? log.model_id.slice(
                                                            log.model_id.indexOf(
                                                                "/",
                                                            ) + 1,
                                                        )
                                                      : log.model_id
                                                : "-"}
                                        </td>
                                        <td className="px-4 py-2 whitespace-nowrap text-xs text-gray-300 truncate">
                                            {log.provider_name === "Deleted" ? (
                                                <span
                                                    className="text-red-400 italic"
                                                    title="Provider was deleted"
                                                >
                                                    Deleted
                                                </span>
                                            ) : isInProgress(log) &&
                                              !log.provider_name ? (
                                                <span className="text-blue-400/60 italic">
                                                    Resolving...
                                                </span>
                                            ) : (
                                                log.provider_name || "-"
                                            )}
                                        </td>
                                        <td className="px-4 py-2 whitespace-nowrap">
                                            <span
                                                className={`inline-flex items-center gap-1 px-1.5 py-0.5 text-[10px] rounded-full whitespace-nowrap ${getStatusBg(log.status_code, log.error_message)}`}
                                            >
                                                {isStale(log) ? (
                                                    <span className="text-yellow-500/70">
                                                        ⚠
                                                    </span>
                                                ) : isInProgress(log) ? (
                                                    <span className="text-blue-400">
                                                        {log.state ===
                                                        "streaming"
                                                            ? "Live"
                                                            : "..."}
                                                    </span>
                                                ) : (
                                                    log.status_code
                                                )}
                                            </span>
                                        </td>
                                        <td className="px-4 py-2 whitespace-nowrap text-xs text-gray-400 font-mono">
                                            {isCancelled(log.error_message)
                                                ? "Interrupted"
                                                : log.tokens_prompt +
                                                        log.tokens_completion >
                                                    0
                                                  ? `${log.tokens_prompt}+${log.tokens_completion}`
                                                  : "-"}
                                        </td>
                                        <td className="px-4 py-2 whitespace-nowrap text-xs text-gray-400 font-mono">
                                            {isCancelled(log.error_message)
                                                ? "-"
                                                : formatTPS(
                                                      log.tokens_per_second,
                                                  )}
                                        </td>
                                        <td className="px-4 py-2 whitespace-nowrap text-xs text-gray-400 font-mono">
                                            {log.ttft_ms > 0
                                                ? formatMs(log.ttft_ms, 1)
                                                : "-"}
                                        </td>
                                        <td className="px-4 py-2 whitespace-nowrap text-xs text-gray-400 font-mono">
                                            {isInProgress(log) &&
                                            log.duration_ms === 0 ? (
                                                <span className="inline-block animate-pulse text-blue-400">
                                                    —
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
                                        <td className="px-4 py-2 whitespace-nowrap text-xs font-mono">
                                            {log.proxy_overhead_ms != null &&
                                            log.proxy_overhead_ms > 0 ? (
                                                <button
                                                    type="button"
                                                    className={`${hasOverhead ? "text-(--accent) hover:text-(--accent-hover) cursor-pointer" : "text-gray-400"}`}
                                                    onClick={() =>
                                                        hasOverhead
                                                            ? setOverheadBreakdown(
                                                                  {
                                                                      proxy_overhead_ms:
                                                                          log.proxy_overhead_ms,
                                                                      parse_ms:
                                                                          log.parse_ms ||
                                                                          0,
                                                                      model_lookup_ms:
                                                                          log.model_lookup_ms ||
                                                                          0,
                                                                      provider_lookup_ms:
                                                                          log.provider_lookup_ms ||
                                                                          0,
                                                                      key_decrypt_ms:
                                                                          log.key_decrypt_ms ||
                                                                          0,
                                                                  },
                                                              )
                                                            : undefined
                                                    }
                                                >
                                                    {formatMs(
                                                        log.proxy_overhead_ms,
                                                    )}
                                                </button>
                                            ) : (
                                                <span className="text-gray-400">
                                                    -
                                                </span>
                                            )}
                                        </td>
                                        <td className="px-4 py-2 whitespace-nowrap text-xs text-gray-400">
                                            {log.virtual_key_deleted ? (
                                                <span className="text-red-400 italic">
                                                    Deleted
                                                </span>
                                            ) : log.virtual_key_name &&
                                              log.virtual_key_name.toLowerCase() ===
                                                  "internal" ? (
                                                <span className="text-gray-400 italic">
                                                    internal
                                                </span>
                                            ) : (
                                                log.virtual_key_name ||
                                                log.virtual_key_id ||
                                                "-"
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
        </div>
    );
}
