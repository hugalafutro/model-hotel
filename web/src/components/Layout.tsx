import { Link, useLocation, useNavigate } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { api } from "../api/client";
import {
    LayoutDashboard,
    PlugZap,
    Bot,
    Shuffle,
    KeyRound,
    ScrollText,
    FileText,
    Settings,
    LogOut,
    BookOpen,
    GitBranch,
    MessageSquare,
    MessagesSquare,
    Swords,
    GitCompare,
    Sun,
    Moon,
    Copy,
    ExternalLink,
    AlertTriangle,
} from "lucide-react";
import { Logo } from "./Logo";
import { useTheme } from "../context/ThemeContext";
import { useSidebarMode } from "../context/SidebarModeContext";
import { useToast } from "../context/ToastContext";

const u = "text-(--text-muted)";

function formatDuration(seconds: number) {
    const d = Math.floor(seconds / 86400);
    const h = Math.floor((seconds % 86400) / 3600);
    const m = Math.floor((seconds % 3600) / 60);
    if (d > 0)
        return (
            <>
                {d}
                <span className={u}>d</span> {h}
                <span className={u}>h</span>
            </>
        );
    if (h > 0)
        return (
            <>
                {h}
                <span className={u}>h</span> {m}
                <span className={u}>m</span>
            </>
        );
    return (
        <>
            {m}
            <span className={u}>m</span>
        </>
    );
}

function formatNumber(n: number) {
    if (n >= 1_000_000)
        return (
            <>
                {(n / 1_000_000).toFixed(1)}
                <span className={u}>M</span>
            </>
        );
    if (n >= 1_000)
        return (
            <>
                {(n / 1_000).toFixed(1)}
                <span className={u}>K</span>
            </>
        );
    return n.toLocaleString();
}

function formatMB(mb: number) {
    if (mb < 1)
        return (
            <>
                {mb.toFixed(1)}
                <span className={u}> MB</span>
            </>
        );
    if (mb >= 1024)
        return (
            <>
                {(mb / 1024).toFixed(1)}
                <span className={u}> GB</span>
            </>
        );
    return (
        <>
            {Math.round(mb)}
            <span className={u}> MB</span>
        </>
    );
}

function formatBytesPerSec(bytesPerSec: number) {
    if (bytesPerSec <= 0)
        return (
            <>
                0<span className={u}> B/s</span>
            </>
        );
    if (bytesPerSec >= 1024 * 1024)
        return (
            <>
                {(bytesPerSec / 1024 / 1024).toFixed(1)}
                <span className={u}> MB/s</span>
            </>
        );
    if (bytesPerSec >= 1024)
        return (
            <>
                {(bytesPerSec / 1024).toFixed(1)}
                <span className={u}> KB/s</span>
            </>
        );
    return (
        <>
            {Math.round(bytesPerSec)}
            <span className={u}> B/s</span>
        </>
    );
}

function SystemStatus() {
    const { data: stats, isError } = useQuery({
        queryKey: ["system"],
        queryFn: () => api.system.get(),
        refetchInterval: 10000,
        staleTime: 3000,
        retry: 1,
    });

    const app = stats?.app;
    const docker = stats?.docker;
    const inContainer = app?.in_container;
    const hasLimit = !!(inContainer && app?.memory_limit_bytes);

    const useDocker = docker?.available;

    const cpuPct = useDocker ? docker.cpu_percent : app?.cpu_percent;
    const procs = useDocker ? docker.procs : app?.procs;
    const netRx = useDocker ? docker.net_rx_bytes_sec : app?.net_rx_bytes_sec;
    const netTx = useDocker ? docker.net_tx_bytes_sec : app?.net_tx_bytes_sec;
    const diskRead = useDocker
        ? docker.disk_read_bytes_sec
        : app?.disk_read_bytes_sec;
    const diskWrite = useDocker
        ? docker.disk_write_bytes_sec
        : app?.disk_write_bytes_sec;

    const dc = (v: number | undefined, w: number, c: number, inv?: boolean) => {
        if (v == null) return "";
        const bad = inv ? v <= c : v >= c;
        const warn = inv ? v <= w : v >= w;
        return bad ? "text-red-400" : warn ? "text-orange-400" : "";
    };

    const dockerMem = useDocker && docker.memory_limit_bytes > 0;
    const memUsagePct = dockerMem
        ? (docker.memory_usage_bytes / docker.memory_limit_bytes) * 100
        : hasLimit && app?.memory_limit_bytes
          ? (app.memory_current_bytes / app.memory_limit_bytes) * 100
          : undefined;
    const appMem = dockerMem ? (
        <>
            {formatMB(docker.memory_usage_bytes / 1024 / 1024)} /{" "}
            {formatMB(docker.memory_limit_bytes / 1024 / 1024)}
        </>
    ) : hasLimit ? (
        <>
            {formatMB(app.memory_current_bytes / 1024 / 1024)} /{" "}
            {formatMB(app.memory_limit_bytes / 1024 / 1024)}
        </>
    ) : app ? (
        <>
            {formatMB(app.heap_alloc_mb)}
            <span className={u}> heap</span>
        </>
    ) : (
        "-"
    );

    const dash = <span className="text-(--text-muted)">-</span>;

    return (
        <div className="space-y-2 text-[11px] font-mono system-status">
            {/* API Status */}
            <div
                className="flex justify-between items-center text-(--text-tertiary)"
                title="Proxy API health status"
            >
                <span>API Status</span>
                <span
                    className={`flex items-center ${isError ? "text-red-400" : "text-green-400"}`}
                >
                    <span
                        className={`w-1.5 h-1.5 rounded-full mr-1.5 ${isError ? "bg-red-400" : "bg-green-400"}`}
                    />
                    {isError ? "Error" : "Online"}
                </span>
            </div>

            {/* Uptime */}
            <div
                className="flex justify-between items-center text-(--text-tertiary)"
                title="How long the server process has been running"
            >
                <span>Uptime</span>
                <span className="text-(--text-secondary)">
                    {app ? formatDuration(app.uptime_seconds) : dash}
                </span>
            </div>

            {/* CPU + Processes */}
            <div
                className="flex justify-between items-center text-(--text-tertiary)"
                title={
                    useDocker
                        ? `Aggregate CPU across ${docker.container_count} compose containers`
                        : "Container CPU usage and process count from cgroup"
                }
            >
                <span>CPU</span>
                <span className={`text-(--text-secondary) ${dc(cpuPct, 75, 90)}`}>
                    {cpuPct != null && cpuPct >= 0 ? (
                        <>
                            <span>
                                {cpuPct.toFixed(1)}
                                <span className={u}>%</span>
                            </span>
                            {procs != null && procs > 0 && (
                                <>
                                    <span className="text-(--text-secondary) mx-1">
                                        |
                                    </span>
                                    <span>
                                        {procs}
                                        <span className={u}>
                                            {" "}
                                            proc{procs !== 1 ? "s" : ""}
                                        </span>
                                    </span>
                                </>
                            )}
                        </>
                    ) : (
                        dash
                    )}
                </span>
            </div>

            {/* Network */}
            <div
                className="flex justify-between items-center text-(--text-tertiary)"
                title={
                    useDocker
                        ? `Aggregate network across ${docker.container_count} compose containers`
                        : "Container network throughput (receive / transmit)"
                }
            >
                <span>Network</span>
                <span className="text-(--text-secondary) tabular-nums">
                    <span className="text-sky-400/60 inline-block min-w-22 text-right">
                        {typeof netRx === "number" ? (
                            <>↓{formatBytesPerSec(netRx)}</>
                        ) : (
                            dash
                        )}
                    </span>
                    <span className="text-amber-400/60 inline-block min-w-22 text-right">
                        {typeof netTx === "number" ? (
                            <>↑{formatBytesPerSec(netTx)}</>
                        ) : (
                            dash
                        )}
                    </span>
                </span>
            </div>

            {/* Disk I/O */}
            <div
                className="flex justify-between items-center text-(--text-tertiary)"
                title={
                    useDocker
                        ? `Aggregate disk I/O across ${docker.container_count} compose containers`
                        : "Container disk I/O throughput (read / write)"
                }
            >
                <span>Disk</span>
                <span className="text-(--text-secondary) tabular-nums">
                    <span className="text-sky-400/60 inline-block min-w-22 text-right">
                        {typeof diskRead === "number" ? (
                            <>↓{formatBytesPerSec(diskRead)}</>
                        ) : (
                            dash
                        )}
                    </span>
                    <span className="text-amber-400/60 inline-block min-w-22 text-right">
                        {typeof diskWrite === "number" ? (
                            <>↑{formatBytesPerSec(diskWrite)}</>
                        ) : (
                            dash
                        )}
                    </span>
                </span>
            </div>

            {/* Memory */}
            <div
                className="flex justify-between items-center text-(--text-tertiary)"
                title={
                    dockerMem
                        ? `Aggregate memory across ${docker.container_count} compose containers`
                        : hasLimit
                          ? "Container memory usage vs cgroup limit"
                          : "Go runtime heap allocation"
                }
            >
                <span>Memory</span>
                <span className={`text-(--text-secondary) ${dc(memUsagePct, 75, 90)}`}>
                    {app ? appMem : dash}
                </span>
            </div>

            {/* Goroutines */}
            <div
                className="flex justify-between items-center text-(--text-tertiary)"
                title="Active Go runtime goroutines (lightweight threads)"
            >
                <span>Go routines</span>
                <span className={`text-(--text-secondary) ${dc(app?.goroutines, 300, 1000)}`}>
                    {app ? app.goroutines.toLocaleString() : dash}
                </span>
            </div>

            {/* Total Requests */}
            <div
                className="flex justify-between items-center text-(--text-tertiary)"
                title="Total number of proxied LLM requests recorded in the database"
            >
                <span>Total Req</span>
                <span className="text-(--text-secondary)">
                    {app && app.total_requests > 0
                        ? formatNumber(app.total_requests)
                        : dash}
                </span>
            </div>

            {/* DB */}
            <div
                className="flex justify-between items-center text-(--text-tertiary)"
                title="Postgres database size, active connections, and buffer cache hit ratio"
            >
                <span>DB</span>
                <span>
                    {stats?.db ? (
                        <>
                            <span className="text-(--text-secondary)">
                                {formatMB(stats.db.size_mb)}
                            </span>
                            <span className="text-(--text-secondary) mx-1">
                                |
                            </span>
                            <span className="text-(--text-secondary)">
                                {stats.db.connections}
                                <span className={u}> conn</span>
                            </span>
                            <span className="text-(--text-secondary) mx-1">
                                |
                            </span>
                            <span className={`text-(--text-secondary) ${dc(stats.db.cache_hit_ratio, 95, 80, true)}`}>
                                Hit {stats.db.cache_hit_ratio}
                                <span className={u}>%</span>
                            </span>
                        </>
                    ) : (
                        dash
                    )}
                </span>
            </div>
        </div>
    );
}

interface LayoutProps {
    children: React.ReactNode;
}

const TEST_APP_ERROR = "TESTING: failover group validation failed — no enabled providers available for model glm-4";
const TEST_REQ_ERROR = "TESTING: upstream provider returned 503 Service Unavailable after exhausting all failover candidates";

function LastErrorPills() {
    const navigate = useNavigate();
    const { setLogsSubMode } = useSidebarMode();
    const { toast } = useToast();

    const { data: appLogData } = useQuery({
        queryKey: ["appLogHistory", "lastError"],
        queryFn: () =>
            api.appLogs.history({
                page: 1,
                per_page: 1,
                level: "error",
                sort_by: "time",
                sort_dir: "desc",
            }),
        refetchInterval: 15000,
        staleTime: 10000,
    });

    const { data: reqLogData } = useQuery({
        queryKey: ["logs", "lastError"],
        queryFn: () =>
            api.logs.list({
                page: 1,
                per_page: 1,
                status_code: "5xx",
                sort_by: "time",
                sort_dir: "desc",
            }),
        refetchInterval: 15000,
        staleTime: 10000,
    });

    const lastAppError = TEST_APP_ERROR || appLogData?.entries?.[0]?.message;
    const lastReqError =
        TEST_REQ_ERROR || reqLogData?.entries?.[0]?.error_message;

    if (!lastAppError && !lastReqError) return null;

    const pill = (
        label: string,
        msg: string,
        subMode: "request" | "app",
    ) => (
        <div className="group relative rounded-md border border-red-500/20 bg-red-950/30 px-2 py-1 text-[10px] leading-tight text-red-300/90 overflow-hidden max-h-[2.5em] hover:max-h-40 transition-[max-h] duration-200">
            <div className="flex items-start gap-1">
                <AlertTriangle size={10} className="shrink-0 mt-0.5 text-red-400/70" />
                <span className="font-semibold text-red-400/80 shrink-0">
                    {label}
                </span>
            </div>
            <div className="line-clamp-1 group-hover:line-clamp-none mt-0.5 font-mono text-[9px] text-red-300/70 break-all">
                {msg}
            </div>
            <div className="absolute top-0.5 right-0.5 flex gap-0.5 opacity-0 group-hover:opacity-100 transition-opacity">
                <button
                    type="button"
                    onClick={(e) => {
                        e.stopPropagation();
                        navigator.clipboard.writeText(msg);
                        toast("Copied to clipboard", "info");
                    }}
                    className="p-0.5 rounded text-red-300/50 hover:text-red-200 hover:bg-red-900/40 transition-colors cursor-pointer"
                    title="Copy error"
                >
                    <Copy size={10} />
                </button>
                <button
                    type="button"
                    onClick={(e) => {
                        e.stopPropagation();
                        setLogsSubMode(subMode);
                        navigate("/logs");
                    }}
                    className="p-0.5 rounded text-red-300/50 hover:text-red-200 hover:bg-red-900/40 transition-colors cursor-pointer"
                    title="View in logs"
                >
                    <ExternalLink size={10} />
                </button>
            </div>
        </div>
    );

    return (
        <div className="flex flex-col gap-1 mb-2">
            {lastAppError && pill("App", lastAppError, "app")}
            {lastReqError && pill("Request", lastReqError, "request")}
        </div>
    );
}

export function Layout({ children }: LayoutProps) {
    const location = useLocation();
    const navigate = useNavigate();
    const { theme, setTheme } = useTheme();

    const {
        chatSubMode,
        setChatSubMode,
        arenaSubMode,
        setArenaSubMode,
        logsSubMode,
        setLogsSubMode,
    } = useSidebarMode();

    const navigation = [
        { name: "Dashboard", href: "/dashboard", icon: LayoutDashboard },
        {
            name: "Chat",
            href: "/chat",
            icon: (mode: string) =>
                mode === "conversation" ? MessagesSquare : MessageSquare,
            subModes: [
                { label: "Chat", value: "chat" as const },
                { label: "Conversation", value: "conversation" as const },
            ],
        },
        {
            name: "Arena",
            href: "/arena",
            icon: (mode: string) => (mode === "compare" ? GitCompare : Swords),
            subModes: [
                { label: "Arena", value: "competition" as const },
                { label: "Compare", value: "compare" as const },
            ],
        },
        { name: "Providers", href: "/providers", icon: PlugZap },
        { name: "Models", href: "/models", icon: Bot },
        { name: "Failover", href: "/failover", icon: Shuffle },
        { name: "Virtual Keys", href: "/virtual-keys", icon: KeyRound },
        {
            name: "Logs",
            href: "/logs",
            icon: (mode: string) => (mode === "app" ? FileText : ScrollText),
            subModes: [
                { label: "Requests", value: "request" as const },
                { label: "Logs", value: "app" as const },
            ],
        },
        { name: "Settings", href: "/settings", icon: Settings },
    ];

    // Generic sub-mode state: maps each nav href to its current mode and setter.
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const subModeMap: Record<string, { mode: string; setMode: any }> = {
        "/chat": { mode: chatSubMode, setMode: setChatSubMode },
        "/arena": { mode: arenaSubMode, setMode: setArenaSubMode },
        "/logs": { mode: logsSubMode, setMode: setLogsSubMode },
    };

    const handleSubModeToggle =
        (href: string, item: (typeof navigation)[number]) =>
        (e: React.MouseEvent) => {
            // Only toggle sub-mode when already on this page;
            // otherwise let the Link navigate normally (first click opens default).
            if (location.pathname !== href) return;
            e.preventDefault();
            const entry = subModeMap[href];
            if (!entry || !("subModes" in item) || !item.subModes) return;
            const other = item.subModes.find((s) => s.value !== entry.mode);
            if (other) {
                entry.setMode(other.value);
            }
        };

    const isActive = (path: string) => location.pathname === path;

    const handleLogout = () => {
        localStorage.removeItem("adminToken");
        navigate("/dashboard");
        window.location.reload();
    };

    return (
        <div className="flex h-screen ui-surface-bg">
            <aside className="w-64 ui-sidebar shrink-0 flex flex-col min-h-0">
                <div className="px-6 pt-5 pb-3 text-center shrink-0">
                    <Logo className="h-10 w-auto text-white mx-auto" />
                    <p className="text-sm text-gray-200 mt-1">
                        Multi-Provider AI Gateway
                    </p>
                    <p className="text-xs text-(--accent) mt-0.5 italic">
                        "Because we have LiteLLM at home"
                    </p>
                </div>
                <nav className="flex-1 min-h-0 px-4 py-2 overflow-y-auto">
                    <ul className="space-y-0.5">
                        {navigation.map((item) => {
                            const sm = subModeMap[item.href];
                            const currentMode = sm?.mode ?? "";
                            const Icon: typeof MessageSquare =
                                typeof item.icon === "function"
                                    ? (
                                          item.icon as (
                                              mode: string,
                                          ) => typeof MessageSquare
                                      )(currentMode)
                                    : item.icon;
                            const active = isActive(item.href);
                            const hasSubModes =
                                "subModes" in item && item.subModes;
                            const currentSubLabel =
                                hasSubModes && sm
                                    ? item.subModes!.find(
                                          (s) => s.value === sm.mode,
                                      )?.label
                                    : null;
                            const otherSub =
                                hasSubModes && sm
                                    ? item.subModes!.find(
                                          (s) => s.value !== sm.mode,
                                      )
                                    : null;

                            return (
                                <li key={item.name}>
                                    <Link
                                        to={item.href}
                                        onClick={
                                            hasSubModes
                                                ? handleSubModeToggle(
                                                      item.href,
                                                      item,
                                                  )
                                                : undefined
                                        }
                                        className={`sidebar-link flex items-center px-4 py-2 transition-colors ${
                                            active
                                                ? "sidebar-link-active"
                                                : "sidebar-link-inactive"
                                        }`}
                                    >
                                        <span className="mr-3 text-(--nav-icon)">
                                            <Icon
                                                size={18}
                                                strokeWidth={active ? 2.5 : 2}
                                            />
                                        </span>
                                        {hasSubModes && currentSubLabel ? (
                                            <span className="flex items-baseline gap-1.5">
                                                <span
                                                    className={
                                                        active
                                                            ? "font-semibold"
                                                            : ""
                                                    }
                                                >
                                                    {currentSubLabel}
                                                </span>
                                                <span className="text-(--text-muted) text-[10px] opacity-60">
                                                    /
                                                </span>
                                                <span className="text-[11px] text-(--text-tertiary)">
                                                    {otherSub?.label}
                                                </span>
                                            </span>
                                        ) : (
                                            item.name
                                        )}
                                    </Link>
                                </li>
                            );
                        })}
                    </ul>
                </nav>
                <div className="px-4 pb-4 shrink-0">
                    <LastErrorPills />
                    <div className="flex justify-between mb-2">
                        <a
                            href="https://github.com/hugalafutro/llm-proxy"
                            target="_blank"
                            rel="noopener noreferrer"
                            className="sidebar-footer-link flex items-center gap-2 px-2 py-1.5 text-xs text-gray-400 hover:text-white transition-colors rounded-lg hover:bg-white/5"
                        >
                            <BookOpen size={14} strokeWidth={2} />
                            Docs
                        </a>
                        <button
                            type="button"
                            onClick={() =>
                                setTheme(theme === "dark" ? "light" : "dark")
                            }
                            className="sidebar-footer-link flex items-center gap-2 px-2 py-1.5 text-xs text-gray-400 hover:text-white transition-colors rounded-lg hover:bg-white/5 cursor-pointer"
                            title={
                                theme === "dark"
                                    ? "Switch to light mode"
                                    : "Switch to dark mode"
                            }
                        >
                            {theme === "dark" ? (
                                <Moon size={14} strokeWidth={2} />
                            ) : (
                                <Sun size={14} strokeWidth={2} />
                            )}
                        </button>
                        <a
                            href="https://github.com/hugalafutro/llm-proxy"
                            target="_blank"
                            rel="noopener noreferrer"
                            className="sidebar-footer-link flex items-center gap-2 px-2 py-1.5 text-xs text-gray-400 hover:text-white transition-colors rounded-lg hover:bg-white/5"
                        >
                            <GitBranch size={14} strokeWidth={2} />
                            GitHub
                        </a>
                    </div>
                    <button
                        type="button"
                        onClick={handleLogout}
                        className="w-full sidebar-logout"
                    >
                        <LogOut size={14} strokeWidth={2} />
                        Logout
                    </button>
                    <SystemStatus />
                </div>
            </aside>

            <main className="flex-1 ui-main overflow-auto">
                <div className="p-2 max-w-7xl mx-auto">{children}</div>
            </main>
        </div>
    );
}
