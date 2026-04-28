import { useQuery } from "@tanstack/react-query";
import { api, type AppLogEntry } from "../api/client";
import { useEffect, useState, useRef, useMemo } from "react";
import { ScrollText, FileText } from "lucide-react";
import { useSidebarMode } from "../context/SidebarModeContext";
import { useToast } from "../context/ToastContext";

export function AppLogs() {
    const { logsSubMode, setLogsSubMode } = useSidebarMode();
    const [liveEnabled, setLiveEnabled] = useState(true);
    const [searchFilter, setSearchFilter] = useState("");
    const [levelFilter, setLevelFilter] = useState<
        "all" | "info" | "warning" | "error"
    >("all");
    const bottomRef = useRef<HTMLDivElement>(null);
    const containerRef = useRef<HTMLDivElement>(null);
    const [autoScroll, setAutoScroll] = useState(true);
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

    useEffect(() => {
        if (autoScroll && bottomRef.current) {
            bottomRef.current.scrollIntoView({ behavior: "smooth" });
        }
    }, [entries, autoScroll]);

    useEffect(() => {
        const container = containerRef.current;
        if (!container) return;
        const handleScroll = () => {
            const { scrollTop, scrollHeight, clientHeight } = container;
            setAutoScroll(scrollHeight - scrollTop - clientHeight < 60);
        };
        container.addEventListener("scroll", handleScroll);
        return () => container.removeEventListener("scroll", handleScroll);
    }, []);

    const filteredEntries = entries.filter((e) => {
        if (levelFilter !== "all" && e.level !== levelFilter) return false;
        if (
            searchFilter &&
            !e.message.toLowerCase().includes(searchFilter.toLowerCase())
        )
            return false;
        return true;
    });

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
            return d.toLocaleTimeString(undefined, {
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
        <div className="flex flex-col gap-4 min-h-[calc(100vh-64px)]">
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
                            className="flex items-center gap-2 px-3 py-1.5 rounded-full text-sm transition-colors bg-green-500/20 text-green-400 hover:bg-green-500/30"
                            style={{
                                backgroundColor: liveEnabled
                                    ? undefined
                                    : "rgba(55,65,81,1)",
                                color: liveEnabled
                                    ? undefined
                                    : "rgb(156,163,175)",
                            }}
                        >
                            <span
                                className="w-2 h-2 rounded-full transition-colors"
                                style={{
                                    backgroundColor: liveEnabled
                                        ? "rgb(74,222,128)"
                                        : "rgb(107,114,128)",
                                }}
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
                            className={
                                "px-3 py-1 rounded-md text-xs font-medium transition-all " +
                                (logsSubMode === "request"
                                    ? "bg-(--accent)/20 text-(--accent) border border-(--accent)/40 cursor-default"
                                    : "text-(--text-tertiary) hover:text-(--text-secondary) border border-transparent cursor-pointer")
                            }
                        >
                            <ScrollText
                                size={12}
                                className="inline mr-1 -mt-0.5"
                            />
                            Requests
                        </button>
                        <button
                            onClick={() => setLogsSubMode("app")}
                            className={
                                "px-3 py-1 rounded-md text-xs font-medium transition-all " +
                                (logsSubMode === "app"
                                    ? "bg-(--accent)/20 text-(--accent) border border-(--accent)/40 cursor-default"
                                    : "text-(--text-tertiary) hover:text-(--text-secondary) border border-transparent cursor-pointer")
                            }
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
                                    onClick={() => setLevelFilter(lvl)}
                                    className={
                                        "px-2 py-0.5 rounded text-[11px] font-medium transition-all cursor-pointer " +
                                        (levelFilter === lvl
                                            ? "bg-white/15 text-(--text-primary)"
                                            : "text-(--text-tertiary) hover:text-(--text-secondary)")
                                    }
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
                        <input
                            type="text"
                            value={searchFilter}
                            onChange={(e) => setSearchFilter(e.target.value)}
                            placeholder="Filter logs…"
                            className="w-50 px-2.5 py-1 text-xs bg-(--input-bg) border border-(--input-border) rounded-md text-(--text-primary) placeholder:text-(--text-tertiary) focus:outline-none focus:ring-1 focus:ring-(--accent)"
                        />
                    </div>
                </div>
            </div>

            {/* Log viewer */}
            <div className="ui-card flex-1 flex flex-col min-h-0 overflow-hidden">
                {isLoading && entries.length === 0 ? (
                    <div className="flex items-center justify-center py-12 text-(--text-tertiary)">
                        Loading logs…
                    </div>
                ) : error ? (
                    <div className="flex items-center justify-center py-12 text-red-400">
                        Failed to load logs: {error?.message}
                    </div>
                ) : filteredEntries.length === 0 ? (
                    <div className="flex items-center justify-center py-12 text-(--text-tertiary)">
                        {entries.length === 0
                            ? "No log entries yet — logs will appear here as the server generates output"
                            : "No entries match your filter"}
                    </div>
                ) : (
                    <div
                        ref={containerRef}
                        className="flex-1 overflow-y-auto font-mono text-xs"
                    >
                        <table className="w-full ui-table">
                            <thead>
                                <tr>
                                    <th className="text-left px-4 py-2 text-xs font-medium text-(--text-tertiary) w-20">
                                        Time
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
                                {filteredEntries.map((entry, i) => (
                                    <tr
                                        key={i}
                                        className="border-b border-(--border) hover:bg-white/2 transition-colors"
                                    >
                                        <td className="px-4 py-2 whitespace-nowrap text-xs text-gray-400">
                                            {formatTimestamp(entry.timestamp)}
                                        </td>
                                        <td className="px-4 py-2">
                                            <span
                                                className={
                                                    "inline-flex items-center px-1.5 py-0.5 text-[10px] rounded-full font-medium " +
                                                    getLevelBadge(entry.level)
                                                }
                                            >
                                                {entry.level.toUpperCase()}
                                            </span>
                                        </td>
                                        <td
                                            className={
                                                "px-4 py-2 whitespace-pre-wrap break-all text-xs font-mono " +
                                                getLevelColor(entry.level)
                                            }
                                        >
                                            {entry.message}
                                        </td>
                                    </tr>
                                ))}
                            </tbody>
                        </table>
                        <div ref={bottomRef} />
                    </div>
                )}
            </div>
        </div>
    );
}
