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

function MemoryBar({ current, limit }: { current: number; limit: number }) {
    if (!limit) return null;
    const pct = Math.min((current / limit) * 100, 100);
    return (
        <div className="flex items-center gap-1.5">
            <div className="flex-1 h-1 rounded-full overflow-hidden bg-gray-700">
                <div
                    className="h-full rounded-full transition-all duration-700"
                    style={{
                        width: `${pct}%`,
                        backgroundColor:
                            pct > 90
                                ? "#ef4444"
                                : pct > 75
                                  ? "#f59e0b"
                                  : "var(--accent)",
                    }}
                />
            </div>
            <span className="text-[10px] text-(--text-muted)">
                {pct.toFixed(0)}%
            </span>
        </div>
    );
}

function SystemStatus() {
    const { data: stats } = useQuery({
        queryKey: ["system"],
        queryFn: () => api.system.get(),
        refetchInterval: 10000,
        retry: false,
    });

    const app = stats?.app;
    const inContainer = app?.in_container;
    const hasLimit = !!(inContainer && app?.memory_limit_bytes);

    const appMem = hasLimit
        ? formatMB(app.memory_current_bytes / 1024 / 1024) +
          " / " +
          formatMB(app.memory_limit_bytes / 1024 / 1024)
        : app
          ? formatMB(app.heap_alloc_mb) + " heap"
          : "-";

    return (
        <div className="space-y-2 text-[11px] font-mono system-status">
            {/* API Status */}
            <div
                className="flex justify-between items-center text-(--text-tertiary)"
                title="Proxy API health status"
            >
                <span>API Status</span>
                <span className="flex items-center text-green-400">
                    <span className="w-1.5 h-1.5 bg-green-400 rounded-full mr-1.5" />
                    Online
                </span>
            </div>

            {/* Uptime */}
            {app && (
                <div
                    className="flex justify-between items-center text-(--text-tertiary)"
                    title="How long the server process has been running"
                >
                    <span>Uptime</span>
                    <span className="text-(--text-secondary)">
                        {formatDuration(app.uptime_seconds)}
                    </span>
                </div>
            )}

            {/* Container CPU */}
            {inContainer && app.cpu_percent >= 0 && (
                <div
                    className="flex justify-between items-center text-(--text-tertiary)"
                    title="Container CPU usage percentage from cgroup"
                >
                    <span>CPU</span>
                    <span className="text-(--text-secondary)">
                        {app.cpu_percent.toFixed(1)}%
                    </span>
                </div>
            )}

            {/* Memory with bar */}
            {app && (
                <div className="space-y-1">
                    <div
                        className="flex justify-between items-center text-(--text-tertiary)"
                        title={
                            hasLimit
                                ? "Container memory usage vs cgroup limit"
                                : "Go runtime heap allocation"
                        }
                    >
                        <span>Memory</span>
                        <span className="text-(--text-secondary)">
                            {appMem}
                        </span>
                    </div>
                    {hasLimit && (
                        <MemoryBar
                            current={app.memory_current_bytes}
                            limit={app.memory_limit_bytes}
                        />
                    )}
                </div>
            )}

            {/* Goroutines */}
            {app && (
                <div
                    className="flex justify-between items-center text-(--text-tertiary)"
                    title="Active Go runtime goroutines (lightweight threads)"
                >
                    <span>Go routines</span>
                    <span className="text-(--text-secondary)">
                        {app.goroutines.toLocaleString()}
                    </span>
                </div>
            )}

            {/* Total Requests */}
            {app && app.total_requests > 0 && (
                <div
                    className="flex justify-between items-center text-(--text-tertiary)"
                    title="Total number of proxied LLM requests recorded in the database"
                >
                    <span>Total Req</span>
                    <span className="text-(--text-secondary)">
                        {formatNumber(app.total_requests)}
                    </span>
                </div>
            )}

            {/* DB */}
            {stats?.db && (
                <div
                    className="flex justify-between items-center text-(--text-tertiary)"
                    title="Postgres database size, active connections, and buffer cache hit ratio"
                >
                    <span>DB</span>
                    <span className="text-(--text-secondary)">
                        {formatMB(stats.db.size_mb)}
                        <span className="text-(--text-muted) mx-1">|</span>
                        {stats.db.connections} conn
                        <span className="text-(--text-muted) mx-1">|</span>
                        Hit {stats.db.cache_hit_ratio}%
                    </span>
                </div>
            )}
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
                <div className="p-6">
                    <Logo className="h-8 w-auto text-white" />
                    <p className="text-sm ui-sidebar-subtitle mt-2 ml-1">
                        Multi-Provider AI Gateway
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
