import { useQuery, keepPreviousData } from "@tanstack/react-query";
import { api } from "../api/client";
import { useState, useEffect, useMemo, useCallback } from "react";
import { ScrollText, FileText } from "lucide-react";
import { useSidebarMode } from "../context/SidebarModeContext";
import { useToast } from "../context/ToastContext";
import { FilterInput } from "../components/FilterInput";
import {
    EmptyRow,
    PaginationBar,
    Row,
    SortableHeader,
} from "../components/DataTable";
import type { SortState } from "../components/DataTable";

type AppLogSortField = "time" | "level" | "source" | "message";

export function AppLogs() {
    const { logsSubMode, setLogsSubMode } = useSidebarMode();
    const [liveEnabled, setLiveEnabled] = useState(true);
    const [searchFilter, setSearchFilter] = useState("");
    const [debouncedSearch, setDebouncedSearch] = useState("");
    const [levelFilter, setLevelFilter] = useState<
        "all" | "info" | "warning" | "error"
    >("all");
    const [sourceFilter, setSourceFilter] = useState<string>("all");
    const [sort, setSort] = useState<SortState<AppLogSortField>>({
        field: "time",
        dir: "desc",
    });
    const [page, setPage] = useState(1);
    const [pageSize, setPageSize] = useState(20);
    const { toast } = useToast();

    useEffect(() => {
        const timer = setTimeout(() => setDebouncedSearch(searchFilter), 300);
        return () => clearTimeout(timer);
    }, [searchFilter]);

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

    const { data: ringBufferData = [] } = useQuery({
        queryKey: ["appLogs"],
        queryFn: () => api.appLogs.list({ limit: 500 }),
        refetchInterval: false,
        staleTime: 30_000,
    });

    const entries = useMemo(
        () => historyData?.entries ?? [],
        [historyData?.entries],
    );
    const totalItems = historyData?.total ?? 0;

    const levelCounts = useMemo(() => {
        const counts = { info: 0, warning: 0, error: 0 };
        for (const e of ringBufferData) {
            if (e.level in counts) counts[e.level as keyof typeof counts]++;
        }
        return counts;
    }, [ringBufferData]);

    const sources = useMemo(() => {
        const set = new Set<string>();
        for (const e of ringBufferData) {
            if (e.source) set.add(e.source);
        }
        return Array.from(set).sort();
    }, [ringBufferData]);

    const sourceCounts = useMemo(() => {
        const counts: Record<string, number> = {};
        for (const e of ringBufferData) {
            if (e.source) counts[e.source] = (counts[e.source] || 0) + 1;
        }
        return counts;
    }, [ringBufferData]);

    const totalPages = Math.max(1, Math.ceil(totalItems / pageSize));
    const safePage = Math.min(page, totalPages);

    const getLevelBadge = (level: string) => {
        switch (level) {
            case "error":
                return "bg-red-900/30 text-red-400";
            case "warning":
                return "bg-yellow-900/30 text-yellow-400";
            default:
                return "bg-blue-900/30 text-blue-400";
        }
    };

    const getSourceBadge = (source: string) => {
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
                return "bg-lime-900/30 text-lime-400";
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
            <div className="flex justify-between items-center shrink-0">
                <div>
                    <div className="flex items-center gap-3">
                        <FileText
                            size={28}
                            strokeWidth={2}
                            className="text-(--accent)"
                        />
                        <h1 className="text-3xl font-bold text-white">Logs</h1>
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
                        Server application log output
                    </p>
                </div>
                {totalItems > 0 && (
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
                )}
            </div>

            <div className="ui-card p-4 shrink-0">
                <div className="flex items-center justify-between">
                    <div className="flex items-center gap-1">
                        <button
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
                            <ScrollText
                                size={12}
                                className="inline mr-1 -mt-0.5"
                            />
                            Requests
                        </button>
                        <button
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
                            <FileText
                                size={12}
                                className="inline mr-1 -mt-0.5"
                            />
                            Logs
                        </button>
                    </div>
                    <div className="flex items-center gap-2">
                        {(["all", "info", "warning", "error"] as const).map(
                            (lvl) => (
                                <button
                                    key={lvl}
                                    onClick={() => {
                                        setLevelFilter(lvl);
                                        setPage(1);
                                    }}
                                    className={`px-2 py-0.5 rounded text-[11px] font-medium transition-all cursor-pointer ${
                                        levelFilter === lvl
                                            ? "bg-white/15 text-(--text-primary)"
                                            : "text-(--text-tertiary) hover:text-(--text-secondary)"
                                    }`}
                                >
                                    {lvl === "all"
                                        ? "All (" + totalItems + ")"
                                        : lvl.charAt(0).toUpperCase() +
                                          lvl.slice(1) +
                                          " (" +
                                          (levelCounts[
                                              lvl as keyof typeof levelCounts
                                          ] ?? 0) +
                                          ")"}
                                </button>
                            ),
                        )}
                        {sources.length > 1 && (
                            <>
                                <div className="w-px h-4 bg-(--border) mx-1" />
                                <button
                                    onClick={() => {
                                        setSourceFilter("all");
                                        setPage(1);
                                    }}
                                    className={`px-2 py-0.5 rounded text-[11px] font-medium transition-all cursor-pointer ${
                                        sourceFilter === "all"
                                            ? "bg-white/15 text-(--text-primary)"
                                            : "text-(--text-tertiary) hover:text-(--text-secondary)"
                                    }`}
                                >
                                    All
                                </button>
                                {sources.map((src) => (
                                    <button
                                        key={src}
                                        onClick={() => {
                                            setSourceFilter(src);
                                            setPage(1);
                                        }}
                                        className={`px-2 py-0.5 rounded text-[11px] font-medium transition-all cursor-pointer ${
                                            sourceFilter === src
                                                ? "bg-white/15 text-(--text-primary)"
                                                : "text-(--text-tertiary) hover:text-(--text-secondary)"
                                        }`}
                                    >
                                        {src} ({sourceCounts[src] ?? 0})
                                    </button>
                                ))}
                            </>
                        )}
                        <div className="w-px h-4 bg-(--border) mx-1" />
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

            {isLoading && !historyData && (
                <div className="flex items-center justify-center py-20">
                    <div className="w-6 h-6 border-2 border-(--accent) border-t-transparent rounded-full animate-spin" />
                </div>
            )}

            {error && !historyData && entries.length === 0 && (
                <div className="ui-card p-8 text-center">
                    <p className="text-red-400 text-sm">
                        Failed to load logs: {error?.message || "Unknown error"}
                    </p>
                </div>
            )}

            {(!isLoading || historyData) && (
                <>
                    <div className="ui-card overflow-x-auto">
                        <table className="w-full ui-table">
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
                                    entries.map((entry, i) => (
                                        <Row key={i} index={i}>
                                            <td className="px-4 py-2 whitespace-nowrap text-xs text-gray-400">
                                                {formatTimestamp(
                                                    entry.timestamp,
                                                )}
                                            </td>
                                            <td className="px-4 py-2">
                                                <span
                                                    className={`inline-flex items-center px-1.5 py-0.5 text-[10px] rounded-full font-medium ${getLevelBadge(entry.level)}`}
                                                >
                                                    {entry.level.toUpperCase()}
                                                </span>
                                            </td>
                                            <td className="px-4 py-2">
                                                {entry.source ? (
                                                    <span
                                                        className={`inline-flex items-center px-1.5 py-0.5 text-[10px] rounded-full font-medium ${getSourceBadge(entry.source)}`}
                                                    >
                                                        {entry.source}
                                                    </span>
                                                ) : (
                                                    <span className="text-gray-600">
                                                        —
                                                    </span>
                                                )}
                                            </td>
                                            <td
                                                className={`px-4 py-2 whitespace-pre-wrap break-all text-xs font-mono ${getLevelColor(entry.level)}`}
                                            >
                                                {entry.message}
                                            </td>
                                        </Row>
                                    ))
                                ) : (
                                    <EmptyRow
                                        colSpan={4}
                                        message={
                                            totalItems === 0
                                                ? "No log entries yet — logs will appear here as the server generates output"
                                                : "No entries match your filter"
                                        }
                                    />
                                )}
                            </tbody>
                        </table>
                    </div>
                </>
            )}
        </div>
    );
}
