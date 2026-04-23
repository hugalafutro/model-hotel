import { Link, useLocation, useNavigate } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { api } from "../api/client";
import {
    LayoutDashboard,
    PlugZap,
    Bot,
    Shuffle,
    Hotel,
    KeyRound,
    ScrollText,
    Settings,
    LogOut,
} from "lucide-react";

function formatMB(mb: number): string {
    if (mb < 1) return `${mb.toFixed(1)} MB`;
    if (mb >= 1024) return `${(mb / 1024).toFixed(1)} GB`;
    return `${Math.round(mb)} MB`;
}

function SystemStatus() {
    const { data: stats } = useQuery({
        queryKey: ["system"],
        queryFn: () => api.system.get(),
        refetchInterval: 10000,
        retry: false,
    });

    const appMem =
        stats?.app?.in_container && stats?.app?.memory_limit_bytes
            ? formatMB(stats.app.memory_current_bytes / 1024 / 1024) +
              " / " +
              formatMB(stats.app.memory_limit_bytes / 1024 / 1024)
            : stats?.app
              ? formatMB(stats.app.heap_alloc_mb) + " heap"
              : "-";

    return (
        <div className="space-y-1.5 text-[11px] font-mono system-status">
            <div className="flex justify-between items-center text-(--text-tertiary)">
                <span>API Status</span>
                <span className="flex items-center text-green-400">
                    <span className="w-1.5 h-1.5 bg-green-400 rounded-full mr-1.5" />
                    Online
                </span>
            </div>
            {stats?.app && (
                <div className="flex justify-between items-center text-(--text-tertiary)">
                    <span>App</span>
                    <span className="text-(--text-secondary)">
                        {appMem}
                        <span className="text-(--text-muted) mx-1">|</span>
                        {stats.app.goroutines} goroutines
                    </span>
                </div>
            )}
            {stats?.db && (
                <div className="flex justify-between items-center text-(--text-tertiary)">
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
                    <h1 className="text-xl font-bold ui-sidebar-title flex items-center gap-2">
                        <Hotel size={22} strokeWidth={2} />
                        Model Hotel
                    </h1>
                    <p className="text-sm ui-sidebar-subtitle mt-1">
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
