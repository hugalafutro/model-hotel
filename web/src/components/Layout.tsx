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
    Settings,
    LogOut,
    BookOpen,
    GitBranch,
} from "lucide-react";
import { Logo } from "./Logo";

function formatDuration(seconds: number): string {
    const d = Math.floor(seconds / 86400);
    const h = Math.floor((seconds % 86400) / 3600);
    const m = Math.floor((seconds % 3600) / 60);
    if (d > 0) return `${d}d ${h}h`;
    if (h > 0) return `${h}h ${m}m`;
    return `${m}m`;
}

function formatNumber(n: number): string {
    if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
    if (n >= 1_000) return `${(n / 1_000).toFixed(1)}K`;
    return n.toLocaleString();
}

function formatMB(mb: number): string {
    if (mb < 1) return `${mb.toFixed(1)} MB`;
    if (mb >= 1024) return `${(mb / 1024).toFixed(1)} GB`;
    return `${Math.round(mb)} MB`;
}

function formatBytesPerSec(bytesPerSec: number): string {
    if (bytesPerSec <= 0) return "0 B/s";
    if (bytesPerSec >= 1024 * 1024)
        return `${(bytesPerSec / 1024 / 1024).toFixed(1)} MB/s`;
    if (bytesPerSec >= 1024) return `${(bytesPerSec / 1024).toFixed(1)} KB/s`;
    return `${Math.round(bytesPerSec)} B/s`;
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
    const diskRead = useDocker ? docker.disk_read_bytes_sec : app?.disk_read_bytes_sec;
    const diskWrite = useDocker ? docker.disk_write_bytes_sec : app?.disk_write_bytes_sec;

    const dockerMem = useDocker && docker.memory_limit_bytes > 0;
    const appMem = dockerMem
        ? formatMB(docker.memory_usage_bytes / 1024 / 1024) +
          " / " +
          formatMB(docker.memory_limit_bytes / 1024 / 1024)
        : hasLimit
          ? formatMB(app.memory_current_bytes / 1024 / 1024) +
            " / " +
            formatMB(app.memory_limit_bytes / 1024 / 1024)
          : app
            ? formatMB(app.heap_alloc_mb) + " heap"
            : "-";

    const showCgroup = inContainer || useDocker;
    const dash = <span className="text-(--text-muted)">-</span>;

    return (
        <div className="space-y-2 text-[11px] font-mono system-status">
            {/* API Status */}
            <div
                className="flex justify-between items-center text-(--text-tertiary)"
                title="Proxy API health status"
            >
                <span>API Status</span>
                <span className={`flex items-center ${isError ? "text-red-400" : "text-green-400"}`}>
                    <span className={`w-1.5 h-1.5 rounded-full mr-1.5 ${isError ? "bg-red-400" : "bg-green-400"}`} />
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
                title={useDocker
                    ? `Aggregate CPU across ${docker.container_count} compose containers`
                    : "Container CPU usage and process count from cgroup"}
            >
                <span>CPU</span>
                <span className="text-(--text-secondary)">
                    {cpuPct != null && cpuPct >= 0 ? (
                        <>
                            <span>{cpuPct.toFixed(1)}%</span>
                            {procs != null && procs > 0 && (
                                <>
                                    <span className="text-(--text-secondary) mx-1">|</span>
                                    <span>{procs} proc{procs !== 1 ? "s" : ""}</span>
                                </>
                            )}
                        </>
                    ) : dash}
                </span>
            </div>

            {/* Network */}
            {showCgroup && (
                <div
                    className="flex justify-between items-center text-(--text-tertiary)"
                    title={useDocker
                        ? `Aggregate network across ${docker.container_count} compose containers`
                        : "Container network throughput (receive / transmit)"}
                >
                    <span>Network</span>
                    <span className="text-(--text-secondary) tabular-nums">
                        <span className="text-sky-400/60 inline-block min-w-22">
                            ↓{formatBytesPerSec(netRx ?? 0)}
                        </span>
                        <span className="text-amber-400/60 inline-block min-w-22">
                            ↑{formatBytesPerSec(netTx ?? 0)}
                        </span>
                    </span>
                </div>
            )}

            {/* Disk I/O */}
            {showCgroup && (
                <div
                    className="flex justify-between items-center text-(--text-tertiary)"
                    title={useDocker
                        ? `Aggregate disk I/O across ${docker.container_count} compose containers`
                        : "Container disk I/O throughput (read / write)"}
                >
                    <span>Disk</span>
                    <span className="text-(--text-secondary) tabular-nums">
                        <span className="text-sky-400/60 inline-block min-w-22">
                            ↓{formatBytesPerSec(diskRead ?? 0)}
                        </span>
                        <span className="text-amber-400/60 inline-block min-w-22">
                            ↑{formatBytesPerSec(diskWrite ?? 0)}
                        </span>
                    </span>
                </div>
            )}

            {/* Memory */}
            <div
                className="flex justify-between items-center text-(--text-tertiary)"
                title={dockerMem
                    ? `Aggregate memory across ${docker.container_count} compose containers`
                    : hasLimit
                      ? "Container memory usage vs cgroup limit"
                      : "Go runtime heap allocation"}
            >
                <span>Memory</span>
                <span className="text-(--text-secondary)">
                    {app ? appMem : dash}
                </span>
            </div>

            {/* Goroutines */}
            <div
                className="flex justify-between items-center text-(--text-tertiary)"
                title="Active Go runtime goroutines (lightweight threads)"
            >
                <span>Go routines</span>
                <span className="text-(--text-secondary)">
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
                    {app && app.total_requests > 0 ? formatNumber(app.total_requests) : dash}
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
                            <span className="text-(--text-secondary) mx-1">|</span>
                            <span className="text-(--text-secondary)">
                                {stats.db.connections} conn
                            </span>
                            <span className="text-(--text-secondary) mx-1">|</span>
                            <span className="text-(--text-secondary)">
                                Hit {stats.db.cache_hit_ratio}%
                            </span>
                        </>
                    ) : dash}
                </span>
            </div>
        </div>
    );
}

interface LayoutProps {
    children: React.ReactNode;
}

export function Layout({ children }: LayoutProps) {
    const location = useLocation();
    const navigate = useNavigate();

    const navigation = [
        { name: "Dashboard", href: "/dashboard", icon: LayoutDashboard },
        { name: "Providers", href: "/providers", icon: PlugZap },
        { name: "Models", href: "/models", icon: Bot },
        { name: "Failover", href: "/failover", icon: Shuffle },
        { name: "Virtual Keys", href: "/virtual-keys", icon: KeyRound },
        { name: "Logs", href: "/logs", icon: ScrollText },
        { name: "Settings", href: "/settings", icon: Settings },
    ];

    const isActive = (path: string) => location.pathname === path;

    const handleLogout = () => {
        localStorage.removeItem("adminToken");
        navigate("/dashboard");
        window.location.reload();
    };

    return (
        <div className="flex h-screen ui-surface-bg">
            <aside className="w-64 ui-sidebar shrink-0">
                <div className="p-6 text-center">
                    <Logo className="h-12 w-auto text-white mx-auto ml-[9%]" />
                    <p className="text-sm text-gray-200 mt-2">
                        Multi-Provider AI Gateway
                    </p>
                    <p className="text-xs text-(--accent) mt-1 italic">
                        "Because we have LiteLLM at home"
                    </p>
                </div>
                <nav className="flex-1 p-4 overflow-y-auto">
                    <ul className="space-y-1">
                        {navigation.map((item) => {
                            const Icon = item.icon;
                            const active = isActive(item.href);
                            return (
                                <li key={item.name}>
                                    <Link
                                        to={item.href}
                                        className={`sidebar-link flex items-center px-4 py-3 transition-colors ${
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
                                        {item.name}
                                    </Link>
                                </li>
                            );
                        })}
                    </ul>
                </nav>
                <div className="p-4 shrink-0">
                    <div className="flex justify-between mb-3">
                        <a
                            href="https://github.com/hugalafutro/llm-proxy"
                            target="_blank"
                            rel="noopener noreferrer"
                            className="flex items-center gap-2 px-3 py-2 text-xs text-gray-400 hover:text-white transition-colors rounded-lg hover:bg-white/5"
                        >
                            <BookOpen size={14} strokeWidth={2} />
                            Docs
                        </a>
                        <a
                            href="https://github.com/hugalafutro/llm-proxy"
                            target="_blank"
                            rel="noopener noreferrer"
                            className="flex items-center gap-2 px-3 py-2 text-xs text-gray-400 hover:text-white transition-colors rounded-lg hover:bg-white/5"
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
                <div className="p-8 max-w-7xl mx-auto">{children}</div>
            </main>
        </div>
    );
}
