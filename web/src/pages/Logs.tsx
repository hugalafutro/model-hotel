import { useQuery, keepPreviousData } from "@tanstack/react-query";
import { api } from "../api/client";
import { useState } from "react";
import { ScrollText } from "lucide-react";
import { StaticHeaderNoArrow, Row, EmptyRow } from "../components/DataTable";
import { useToast } from "../context/ToastContext";

function formatTPS(t: number | null): string {
    if (t == null) return "-";
    return t.toFixed(1);
}

function formatMs(v: number | null | undefined, decimals: number = 2): string {
    if (v == null || v === 0) return "-";
    return v.toFixed(decimals) + "ms";
}

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
                        className="absolute top-4 right-4 text-gray-400 hover:text-white transition-all cursor-default text-xl leading-none hover:drop-shadow-[0_0_8px_var(--accent)]"
                    >
                        &times;
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

export function Logs() {
    const [page, setPage] = useState(1);
    const [pageSize, setPageSize] = useState(25);
    const [filters, setFilters] = useState({ model_id: "", status_code: "" });
    const [overheadBreakdown, setOverheadBreakdown] =
        useState<OverheadBreakdown | null>(null);
    const [liveEnabled, setLiveEnabled] = useState(true);
    const { toast } = useToast();

    const { data: logsData } = useQuery({
        queryKey: ["logs", page, pageSize, filters],
        queryFn: () =>
            api.logs.list({
                page,
                per_page: pageSize,
                model_id: filters.model_id || undefined,
                status_code: filters.status_code || undefined,
            }),
        refetchInterval: liveEnabled ? 2000 : false,
        placeholderData: keepPreviousData,
    });

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

            <div className="flex flex-col md:flex-row gap-4">
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
                        {logsData?.entries && logsData.entries.length > 0 ? (
                            logsData.entries.map((log, idx) => {
                                const hasOverhead =
                                    log.proxy_overhead_ms != null &&
                                    log.proxy_overhead_ms > 0 &&
                                    (log.parse_ms > 0 ||
                                        log.model_lookup_ms > 0 ||
                                        log.provider_lookup_ms > 0 ||
                                        log.key_decrypt_ms > 0);
                                const isInProgress =
                                    log.status_code === 0 &&
                                    log.duration_ms === 0;
                                return (
                                    <Row
                                        key={log.id}
                                        index={idx}
                                        className={
                                            isInProgress
                                                ? "animate-row-pulse"
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
                                            {log.provider_name === "Deleted" ? (
                                                <span className="text-red-400 italic">
                                                    Deleted
                                                </span>
                                            ) : (
                                                log.provider_name || "-"
                                            )}
                                        </td>
                                        <td className="px-4 py-2 whitespace-nowrap">
                                            <span
                                                className={`px-1.5 py-0.5 text-[10px] rounded-full ${getStatusBg(log.status_code, log.error_message)}`}
                                            >
                                                {log.status_code}
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
                                            {log.duration_ms > 0
                                                ? log.duration_ms >= 1000
                                                    ? `${(log.duration_ms / 1000).toFixed(1)}s`
                                                    : `${log.duration_ms.toFixed(0)}ms`
                                                : "-"}
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

            {logsData && logsData.total > 0 && (
                <div className="flex items-center justify-between">
                    <div className="text-sm text-gray-500">
                        Showing {(page - 1) * pageSize + 1} to{" "}
                        {Math.min(page * pageSize, logsData.total)} of{" "}
                        {logsData.total} entries
                    </div>
                    <div className="flex items-center gap-3">
                        <select
                            value={pageSize}
                            onChange={(e) => {
                                setPageSize(Number(e.target.value));
                                setPage(1);
                            }}
                            className="ui-input ui-input-sm"
                        >
                            <option value={25}>25 / page</option>
                            <option value={50}>50 / page</option>
                            <option value={75}>75 / page</option>
                            <option value={100}>100 / page</option>
                            <option value={125}>125 / page</option>
                            <option value={150}>150 / page</option>
                            <option value={175}>175 / page</option>
                            <option value={200}>200 / page</option>
                        </select>
                        {Math.ceil(logsData.total / pageSize) > 1 && (
                            <div className="flex items-center gap-1">
                                <button
                                    type="button"
                                    onClick={() =>
                                        setPage((p) => Math.max(1, p - 1))
                                    }
                                    disabled={page === 1}
                                    className="px-2 py-1 text-xs rounded border bg-gray-700 text-gray-300 border-gray-600 hover:bg-gray-600 disabled:opacity-50 disabled:cursor-not-allowed"
                                >
                                    Prev
                                </button>
                                {Array.from(
                                    {
                                        length: Math.min(
                                            7,
                                            Math.ceil(
                                                logsData.total / pageSize,
                                            ),
                                        ),
                                    },
                                    (_, i) => {
                                        const totalPages = Math.ceil(
                                            logsData.total / pageSize,
                                        );
                                        let pageNum: number;
                                        if (totalPages <= 7) {
                                            pageNum = i + 1;
                                        } else if (page <= 4) {
                                            pageNum = i + 1;
                                            if (i === 6) pageNum = totalPages;
                                        } else if (page >= totalPages - 3) {
                                            pageNum = totalPages - 6 + i;
                                            if (i === 0) pageNum = 1;
                                        } else {
                                            pageNum = page - 3 + i;
                                            if (i === 0) pageNum = 1;
                                            if (i === 6) pageNum = totalPages;
                                        }
                                        return (
                                            <button
                                                key={pageNum}
                                                type="button"
                                                onClick={() => setPage(pageNum)}
                                                className={`px-2 py-1 text-xs rounded border ${
                                                    page === pageNum
                                                        ? "bg-(--accent) text-white border-(--accent)"
                                                        : "bg-gray-700 text-gray-300 border-gray-600 hover:bg-gray-600"
                                                }`}
                                            >
                                                {pageNum}
                                            </button>
                                        );
                                    },
                                )}
                                <button
                                    type="button"
                                    onClick={() =>
                                        setPage((p) =>
                                            Math.min(
                                                Math.ceil(
                                                    logsData.total / pageSize,
                                                ),
                                                p + 1,
                                            ),
                                        )
                                    }
                                    disabled={page * pageSize >= logsData.total}
                                    className="px-2 py-1 text-xs rounded border bg-gray-700 text-gray-300 border-gray-600 hover:bg-gray-600 disabled:opacity-50 disabled:cursor-not-allowed"
                                >
                                    Next
                                </button>
                            </div>
                        )}
                    </div>
                </div>
            )}
        </div>
    );
}
