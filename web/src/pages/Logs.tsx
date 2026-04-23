import { useQuery, keepPreviousData } from "@tanstack/react-query";
import { api } from "../api/client";
import { useEffect, useState, useRef } from "react";
import { ScrollText, X, CalendarDays, ChevronLeft, ChevronRight } from "lucide-react";
import type { LogEntry } from "../api/types";
import {
    StaticHeaderNoArrow,
    Row,
    EmptyRow,
    PaginationBar,
} from "../components/DataTable";
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
    return sameMonth ? `${fd}-${td}` : `${fd}/${f.getFullYear().toString().slice(2)} - ${td}`;
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
                    className="text-gray-400 hover:text-white transition-colors p-1 rounded-[var(--radius-button)] hover:bg-gray-700"
                >
                    <ChevronLeft size={16} />
                </button>
                <span className="text-sm font-semibold text-white">
                    {monthName} {year}
                </span>
                <button
                    type="button"
                    onClick={handleNext}
                    className="text-gray-400 hover:text-white transition-colors p-1 rounded-[var(--radius-button)] hover:bg-gray-700"
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
                                text-[11px] w-7 h-7                                 rounded-[var(--radius-button)] flex items-center justify-center transition-colors
                                ${sel
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
    const [page, setPage] = useState(1);
    const [pageSize, setPageSize] = useState(20);
    const [filters, setFilters] = useState({ model_id: "", status_code: "" });
    const [dateFrom, setDateFrom] = useState("");
    const [dateTo, setDateTo] = useState("");
    const [showDatePicker, setShowDatePicker] = useState(false);
    const [pendingFrom, setPendingFrom] = useState("");
    const [pendingTo, setPendingTo] = useState("");

    const datePickerRef = useRef<HTMLDivElement>(null);
    const [overheadBreakdown, setOverheadBreakdown] =
        useState<OverheadBreakdown | null>(null);
    const [liveEnabled, setLiveEnabled] = useState(true);
    const { toast } = useToast();

    const [fallback, setFallback] = useState<{
        entries: LogEntry[];
        total: number;
    }>({ entries: [], total: 0 });

    const { data: logsData } = useQuery({
        queryKey: ["logs", page, pageSize, filters, dateFrom, dateTo],
        queryFn: () =>
            api.logs.list({
                page,
                per_page: pageSize,
                model_id: filters.model_id || undefined,
                status_code: filters.status_code || undefined,
                from: dateFrom || undefined,
                to: dateTo || undefined,
            }),
        refetchInterval: liveEnabled ? 2000 : false,
        placeholderData: keepPreviousData,
    });

    const hasFreshData = (logsData?.entries?.length ?? 0) > 0;
    const freshEntries = logsData?.entries;
    const freshTotal = logsData?.total ?? 0;

    useEffect(() => {
        if (hasFreshData && freshEntries) {
            // eslint-disable-next-line
            setFallback({ entries: freshEntries, total: freshTotal });
        }
    }, [hasFreshData, freshEntries, freshTotal]);

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
            return () => document.removeEventListener("mousedown", handleClickOutside);
        }
    }, [showDatePicker]);

    const displayEntries = hasFreshData && freshEntries ? freshEntries : fallback.entries;
    const displayTotal = hasFreshData ? freshTotal : fallback.total;

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
        if (statusCode >= 200 && statusCode < 300)
            return "bg-green-900/30 text-green-400";
        if (statusCode >= 400 && statusCode < 500)
            return "bg-orange-900/30 text-orange-400";
        if (statusCode >= 500) return "bg-red-900/30 text-red-400";
        return "bg-gray-700 text-gray-300";
    };

    const isInProgress = (log: LogEntry) =>
        log.state === "pending" || log.state === "streaming";

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
            </div>

            <div className="flex flex-col md:flex-row gap-4 items-start md:items-center justify-between">
                <div className="flex-1 flex gap-2">
                    <input
                        type="text"
                        placeholder="Filter by model ID..."
                        value={filters.model_id}
                        onChange={(e) => {
                            setFilters({
                                ...filters,
                                model_id: e.target.value,
                            });
                            setPage(1);
                        }}
                        className="ui-input"
                    />
                </div>
                <div className="md:w-48">
                    <select
                        value={filters.status_code}
                        onChange={(e) => {
                            setFilters({
                                ...filters,
                                status_code: e.target.value,
                            });
                            setPage(1);
                        }}
                        className="ui-input"
                    >
                        <option value="">All Status</option>
                        <option value="200">200 OK</option>
                        <option value="4xx">4XX</option>
                        <option value="5xx">5XX</option>
                    </select>
                </div>

                {/* Calendar picker */}
                <div className="relative" ref={datePickerRef}>
                    <button
                        type="button"
                        onClick={toggleDatePicker}
                        className={`flex items-center gap-2 px-3 py-2 rounded-[var(--radius-button)] text-sm border transition-colors cursor-pointer ${
                            hasDateFilter
                                ? "bg-(--accent)/15 text-(--accent) border-(--accent)/40 hover:bg-(--accent)/25"
                                : "bg-gray-900/40 text-gray-400 border-gray-700/50 hover:text-white hover:border-gray-500"
                        }`}
                        title="Filter by date range"
                    >
                        <CalendarDays size={16} />
                        <span>
                            {hasDateFilter
                                ? formatDateRangeShort(dateFrom, dateTo)
                                : "Date Range"}
                        </span>
                        {hasDateFilter && (
                            <button
                                type="button"
                                className="ml-1 inline-flex items-center justify-center w-5 h-5 rounded-[var(--radius-button)] bg-(--accent)/30 text-(--accent) hover:text-white transition-all cursor-default hover:drop-shadow-[0_0_8px_var(--accent)]"
                                onClick={(e) => {
                                    e.stopPropagation();
                                    clearDateFilter();
                                }}
                                title="Clear date filter"
                            >
                                <X size={14} />
                            </button>
                        )}
                    </button>

                    {showDatePicker && (
                        <div className="absolute right-0 mt-2 w-72 p-4 bg-gray-900 border border-gray-700 rounded-[var(--radius-card)] shadow-2xl z-50">
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

            <div className="ui-card overflow-x-auto">
                <table className="w-full table-fixed ui-table min-w-250">
                    <colgroup>
                        <col className="w-37.5" />
                        <col className="w-32.5" />
                        <col className="w-37.5" />
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
                            <StaticHeaderNoArrow tooltip="Timestamp of the request">
                                Time
                            </StaticHeaderNoArrow>
                            <StaticHeaderNoArrow tooltip="Unique hash of the request body">
                                Hash
                            </StaticHeaderNoArrow>
                            <StaticHeaderNoArrow tooltip="Model ID used for the request">
                                Model
                            </StaticHeaderNoArrow>
                            <StaticHeaderNoArrow tooltip="Provider handling the request">
                                Provider
                            </StaticHeaderNoArrow>
                            <StaticHeaderNoArrow tooltip="HTTP status code of the response">
                                Status
                            </StaticHeaderNoArrow>
                            <StaticHeaderNoArrow tooltip="Prompt + completion tokens (if available)">
                                Tokens
                            </StaticHeaderNoArrow>
                            <StaticHeaderNoArrow tooltip="Tokens generated per second">
                                T/s
                            </StaticHeaderNoArrow>
                            <StaticHeaderNoArrow tooltip="Time to first token">
                                TTFT
                            </StaticHeaderNoArrow>
                            <StaticHeaderNoArrow tooltip="Total request duration">
                                Duration
                            </StaticHeaderNoArrow>
                            <StaticHeaderNoArrow tooltip="Proxy overhead (parsing, lookups, etc)">
                                Overhead
                            </StaticHeaderNoArrow>
                            <StaticHeaderNoArrow tooltip="Virtual key used for authentication">
                                Key
                            </StaticHeaderNoArrow>
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
                                            {log.model_id || "-"}
                                        </td>
                                        <td className="px-4 py-2 whitespace-nowrap text-xs text-gray-300 truncate">
                                            {log.provider_name === "Deleted" &&
                                            !isInProgress(log) ? (
                                                <span className="text-red-400 italic">
                                                    Deleted
                                                </span>
                                            ) : isInProgress(log) &&
                                              (!log.provider_name ||
                                                  log.provider_name ===
                                                      "Deleted") ? (
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
                                                {isInProgress(log) ? (
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
                                                  "admin" ? (
                                                "admin"
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
