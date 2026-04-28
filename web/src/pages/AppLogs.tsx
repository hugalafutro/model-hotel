import { useQuery } from "@tanstack/react-query";
import { api, type AppLogEntry } from "../api/client";
import { useState, useMemo } from "react";
import { ScrollText, FileText } from "lucide-react";
import { useSidebarMode } from "../context/SidebarModeContext";
import { useToast } from "../context/ToastContext";
import { FilterInput } from "../components/FilterInput";
import { EmptyRow, PaginationBar } from "../components/DataTable";

export function AppLogs() {
    const { logsSubMode, setLogsSubMode } = useSidebarMode();
    const [liveEnabled, setLiveEnabled] = useState(true);
    const [searchFilter, setSearchFilter] = useState("");
    const [levelFilter, setLevelFilter] = useState<
        "all" | "info" | "warning" | "error"
    >("all");
    const [page, setPage] = useState(1);
    const [pageSize, setPageSize] = useState(20);
    const { toast } = useToast();

    const {
        data: entries = [],
        isLoading,
        error,
    } = useQuery<AppLogEntry[]>({
        queryKey: ["appLogs"],
        queryFn: () => api.appLogs.list({ limit: 500 }),
        refetchInterval: liveEnabled ? 2000 : false,
    });

    const filteredEntries = useMemo(() => {
        return entries.filter((e) => {
            if (levelFilter !== "all" && e.level !== levelFilter) return false;
            if (
                searchFilter &&
                !e.message.toLowerCase().includes(searchFilter.toLowerCase())
            )
                return false;
            return true;
        });
    }, [entries, levelFilter, searchFilter]);

    // Reverse so latest entries appear first
    const reversedEntries = useMemo(
        () => [...filteredEntries].reverse(),
        [filteredEntries],
    );

    const totalCount = reversedEntries.length;
    const totalPages = Math.max(1, Math.ceil(totalCount / pageSize));
    const safePage = Math.min(page, totalPages);
    const pageStart = (safePage - 1) * pageSize;
    const pageEntries = reversedEntries.slice(pageStart, pageStart + pageSize);

    const levelCounts = useMemo(() => {
        const counts = { info: 0, warning: 0, error: 0 };
        for (const e of entries) {
            if (e.level in counts) counts[e.level as keyof typeof counts]++;
        }
        return counts;
    }, [entries]);

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
            {/* Header */}
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
            </div>

            {/* Controls */}
            <div className="ui-card p-4 shrink-0">
                <div className="flex items-center justify-between">
                    <div className="flex items-center gap-1">
                        <button
                            onClick={() => setLogsSubMode("request")}
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
                            onClick={() => setLogsSubMode("app")}
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
                                        ? "All (" + entries.length + ")"
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
                        <div className="w-px h-4 bg-(--border) mx-1" />
                        <FilterInput
                            value={searchFilter}
                            onChange={(v) => {
                                setSearchFilter(v);
                                setPage(1);
                            }}
                            placeholder="Filter logs…"
                            className="w-50"
                        />
                    </div>
                </div>
            </div>

            {/* Loading / Error / Empty states */}
            {isLoading && entries.length === 0 && (
                <div className="flex items-center justify-center py-20">
                    <div className="w-6 h-6 border-2 border-(--accent) border-t-transparent rounded-full animate-spin" />
                </div>
            )}

            {error && entries.length === 0 && (
                <div className="ui-card p-8 text-center">
                    <p className="text-red-400 text-sm">
                        Failed to load logs: {error?.message || "Unknown error"}
                    </p>
                </div>
            )}

            {!(isLoading && entries.length === 0) && (
                <>
                    <div className="ui-card overflow-x-auto">
                        <table className="w-full ui-table">
                            <thead>
                                <tr>
                                    <th className="text-left px-4 py-2 text-xs font-medium text-(--text-tertiary) w-36">
                                        Time/Date
                                    </th>
                                    <th className="text-left px-4 py-2 text-xs font-medium text-(--text-tertiary) w-17.5">
                                        Level
                                    </th>
                                    <th className="text-left px-4 py-2 text-xs font-medium text-(--text-tertiary)">
                                        Message
                                    </th>
                                </tr>
                            </thead>
                            <tbody>
                                {pageEntries.length > 0 ? (
                                    pageEntries.map((entry, i) => (
                                        <tr
                                            key={pageStart + i}
                                            className="border-b border-(--border) hover:bg-white/2 transition-colors"
                                        >
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
                                            <td
                                                className={`px-4 py-2 whitespace-pre-wrap break-all text-xs font-mono ${getLevelColor(entry.level)}`}
                                            >
                                                {entry.message}
                                            </td>
                                        </tr>
                                    ))
                                ) : (
                                    <EmptyRow
                                        colSpan={3}
                                        message={
                                            entries.length === 0
                                                ? "No log entries yet — logs will appear here as the server generates output"
                                                : "No entries match your filter"
                                        }
                                    />
                                )}
                            </tbody>
                        </table>
                    </div>

                    {/* Pagination */}
                    {totalCount > 0 && (
                        <div className="flex justify-end pt-3">
                            <PaginationBar
                                page={safePage}
                                totalPages={totalPages}
                                totalItems={totalCount}
                                pageSize={pageSize}
                                onPageChange={setPage}
                                onPageSizeChange={(s) => {
                                    setPageSize(s);
                                    setPage(1);
                                }}
                                label="entries"
                            />
                        </div>
                    )}
                </>
            )}
        </div>
    );
}
