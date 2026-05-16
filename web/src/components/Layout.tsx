import { useQuery } from "@tanstack/react-query";
import {
	AlertTriangle,
	BookOpen,
	Bot,
	Copy,
	ExternalLink,
	FileText,
	GitBranch,
	GitCompare,
	KeyRound,
	LayoutDashboard,
	LogOut,
	MessageSquare,
	MessagesSquare,
	Moon,
	PlugZap,
	ScrollText,
	Settings,
	Shuffle,
	Sun,
	Swords,
	X,
} from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { Link, useLocation, useNavigate } from "react-router-dom";
import { api } from "../api/client";
import type { AppLogEntry, LogEntry } from "../api/types";
import { useSidebarMode } from "../context/SidebarModeContext";
import { useTheme } from "../context/ThemeContext";
import { useToast } from "../context/ToastContext";
import { useGitHubVersion } from "../hooks/useGitHubVersion";
import { formatRelativeTime, formatTimestamp } from "../utils/format";
import { CollapsibleToggle, useCollapsible } from "./CollapsibleToggle";
import { ConfirmDialog } from "./ConfirmDialog";
import { LogDetailModal } from "./LogDetailModal";
import { Logo } from "./Logo";
import { ProviderQuotaPanel } from "./ProviderQuotaPanel";

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

	const { collapsed, toggle: toggleCollapsed } = useCollapsible(
		"sidebarStatsCollapsed",
	);

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
		<div className="sidebar-stats-pill">
			<div className="sidebar-stats-trigger">
				<div
					className="flex justify-between items-center text-[11px] font-mono text-(--text-tertiary) flex-1 min-w-0"
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
			</div>
			<div
				className={`grid transition-[grid-template-rows] duration-300 ease-in-out ${collapsed ? "grid-rows-[0fr]" : "grid-rows-[1fr]"}`}
			>
				<div className="overflow-hidden">
					<div className="sidebar-stats-content space-y-0.5 text-[11px] font-mono system-status">
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
												<span className="text-(--text-secondary) mx-1">|</span>
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
							<span
								className={`text-(--text-secondary) ${dc(memUsagePct, 75, 90)}`}
							>
								{app ? appMem : dash}
							</span>
						</div>

						{/* Goroutines */}
						<div
							className="flex justify-between items-center text-(--text-tertiary)"
							title="Active Go runtime goroutines (lightweight threads)"
						>
							<span>Go routines</span>
							<span
								className={`text-(--text-secondary) ${dc(app?.goroutines, 300, 1000)}`}
							>
								{app ? app.goroutines.toLocaleString() : dash}
							</span>
						</div>

						{/* Requests Today */}
						<div
							className="flex justify-between items-center text-(--text-tertiary)"
							title="Number of proxied LLM requests since midnight local time today"
						>
							<span>Req Today</span>
							<span className="text-(--text-secondary)">
								{app && app.requests_today > 0
									? formatNumber(app.requests_today)
									: dash}
							</span>
						</div>

						{/* DB: size & hit ratio / connections & tx/sec */}
						<div className="flex justify-between items-center text-(--text-tertiary)">
							<span className="self-center">DB</span>
							<span className="grid grid-cols-[1fr_auto_1fr] grid-rows-[auto_auto] gap-x-2 items-center text-right">
								{stats?.db ? (
									<>
										<span
											className="text-(--text-secondary)"
											title="Postgres database size on disk"
										>
											{formatMB(stats.db.size_mb)}
										</span>
										<span className="text-(--text-secondary)">|</span>
										<span
											className={`text-(--text-secondary) ${dc(stats.db.cache_hit_ratio, 95, 80, true)}`}
											title="Buffer cache hit ratio (higher is better)"
										>
											Hit {stats.db.cache_hit_ratio}
											<span className={u}>%</span>
										</span>
										<span
											className="text-(--text-secondary)"
											title="Active database connections"
										>
											{stats.db.connections}
											<span className={u}> conn</span>
										</span>
										<span className="text-(--text-secondary)">|</span>
										<span
											className="text-(--text-secondary)"
											title="Database transactions per second"
										>
											{stats.db.tx_per_sec.toFixed(1)}
											<span className={u}> tx/s</span>
										</span>
									</>
								) : (
									<>
										<span className="text-(--text-muted)">-</span>
										<span className="text-(--text-secondary)">|</span>
										<span className="text-(--text-muted)">-</span>
										<span className="text-(--text-muted)">-</span>
										<span className="text-(--text-secondary)">|</span>
										<span className="text-(--text-muted)">-</span>
									</>
								)}
							</span>
						</div>
					</div>
				</div>
			</div>
			<div className="sidebar-stats-footer">
				<CollapsibleToggle
					collapsed={collapsed}
					onToggle={toggleCollapsed}
					size={10}
					iconStyle="double"
					expandTitle="Expand stats"
					collapseTitle="Collapse stats"
				/>
			</div>
		</div>
	);
}

interface LayoutProps {
	children: React.ReactNode;
}

function LastErrorPills() {
	const navigate = useNavigate();
	const { setLogsSubMode } = useSidebarMode();
	const { toast } = useToast();
	const [detailEntry, setDetailEntry] = useState<{
		log: LogEntry | AppLogEntry;
		type: "request" | "app";
	} | null>(null);
	const [dismissedAppKey, setDismissedAppKey] = useState<string | null>(() => {
		try {
			return localStorage.getItem("dismissedAppErrorKey");
		} catch {
			return null;
		}
	});
	const [dismissedReqKey, setDismissedReqKey] = useState<string | null>(() => {
		try {
			return localStorage.getItem("dismissedReqErrorKey");
		} catch {
			return null;
		}
	});

	useEffect(() => {
		const handler = () => {
			setDismissedAppKey(null);
			setDismissedReqKey(null);
		};
		window.addEventListener("dismissedErrorsReset", handler);
		return () => window.removeEventListener("dismissedErrorsReset", handler);
	}, []);

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

	const lastAppEntry = appLogData?.entries?.[0];
	const lastAppError = lastAppEntry?.message;
	const lastAppTimestamp = lastAppEntry?.timestamp;
	const lastReqEntry = reqLogData?.entries?.[0];
	const lastReqError = lastReqEntry?.error_message;
	const lastReqTimestamp = lastReqEntry?.created_at;

	const appErrorKey =
		lastAppError && lastAppTimestamp
			? `${lastAppTimestamp}:${lastAppError.slice(0, 50)}`
			: null;
	const reqErrorKey =
		lastReqError && lastReqTimestamp
			? `${lastReqTimestamp}:${lastReqError.slice(0, 50)}`
			: null;

	// Show the pill if there's an error and it hasn't been dismissed.
	// If the error key changes (new error), dismissedAppKey no longer matches,
	// so the pill auto-reappears - no useEffect needed.
	const showAppError = lastAppError && appErrorKey !== dismissedAppKey;
	const showReqError = lastReqError && reqErrorKey !== dismissedReqKey;

	const dismissAppError = useCallback((key: string) => {
		setDismissedAppKey(key);
		try {
			localStorage.setItem("dismissedAppErrorKey", key);
		} catch {
			/* ignore */
		}
	}, []);
	const dismissReqError = useCallback((key: string) => {
		setDismissedReqKey(key);
		try {
			localStorage.setItem("dismissedReqErrorKey", key);
		} catch {
			/* ignore */
		}
	}, []);

	if (!showAppError && !showReqError) return null;

	const pill = (
		label: string,
		msg: string,
		subMode: "request" | "app",
		onAcknowledge: () => void,
		timestamp: string | null,
		entry?: LogEntry | AppLogEntry,
	) => (
		<div className="group relative rounded-md border border-red-500/30 bg-red-950/20 overflow-hidden">
			{/* Header row with icon, label, and action buttons */}
			<div className="flex items-center justify-between px-2 py-px bg-red-900/20">
				<div className="flex items-center gap-1.5">
					<AlertTriangle size={10} className="shrink-0 text-red-400" />
					<span
						className="text-[10px] font-semibold text-red-300 uppercase tracking-wider"
						title={timestamp ? formatTimestamp(timestamp) : undefined}
					>
						{timestamp
							? `${label === "App" ? "App Err" : "Req Err"} ${formatRelativeTime(timestamp)}`
							: `${label} Error`}
					</span>
				</div>
				<div className="flex gap-0.5">
					<button
						type="button"
						onClick={(e) => {
							e.stopPropagation();
							navigator.clipboard.writeText(msg);
							toast("Copied to clipboard", "info");
						}}
						className="p-0.5 rounded text-red-400/60 hover:text-red-200 hover:bg-red-900/40 transition-colors cursor-pointer"
						title="Copy error"
					>
						<Copy size={10} />
					</button>
					<button
						type="button"
						onClick={(e) => {
							e.stopPropagation();
							if (entry) {
								setDetailEntry({ log: entry, type: subMode });
							} else {
								setLogsSubMode(subMode);
								navigate("/logs");
							}
						}}
						className="p-0.5 rounded text-red-400/60 hover:text-red-200 hover:bg-red-900/40 transition-colors cursor-pointer"
						title="View details"
					>
						<ExternalLink size={10} />
					</button>
					<button
						type="button"
						onClick={(e) => {
							e.stopPropagation();
							onAcknowledge();
							toast(`${label} error acknowledged`, "info");
						}}
						className="p-0.5 rounded text-red-400/60 hover:text-red-200 hover:bg-red-900/40 transition-colors cursor-pointer"
						title="Acknowledge (dismiss)"
					>
						<X size={10} />
					</button>
				</div>
			</div>
			{/* Error message body */}
			<div className="px-2 py-1">
				<div
					className="font-mono text-[9.5px] text-red-300/80 break-words leading-relaxed line-clamp-3"
					title={msg}
				>
					{msg.length > 200 ? `${msg.slice(0, 200)}…` : msg}
				</div>
			</div>
		</div>
	);

	return (
		<>
			{detailEntry && (
				<LogDetailModal
					log={detailEntry.log}
					type={detailEntry.type}
					onClose={() => setDetailEntry(null)}
				/>
			)}
			<div className="flex flex-col gap-1 mb-2">
				{showAppError &&
					appErrorKey &&
					pill(
						"App",
						lastAppError,
						"app",
						() => {
							dismissAppError(appErrorKey);
							toast("App error acknowledged", "info");
						},
						lastAppTimestamp ?? null,
						lastAppEntry,
					)}
				{showReqError &&
					reqErrorKey &&
					pill(
						"Request",
						lastReqError,
						"request",
						() => {
							dismissReqError(reqErrorKey);
							toast("Request error acknowledged", "info");
						},
						lastReqTimestamp ?? null,
						lastReqEntry,
					)}
			</div>
		</>
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

	const { running, updateAvailable } = useGitHubVersion();

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
	const subModeMap = {
		"/chat": { mode: chatSubMode, setMode: setChatSubMode },
		"/arena": { mode: arenaSubMode, setMode: setArenaSubMode },
		"/logs": { mode: logsSubMode, setMode: setLogsSubMode },
	} as Record<string, { mode: string; setMode: (v: string) => void }>;

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

	const [showLogoutConfirm, setShowLogoutConfirm] = useState(false);

	const handleLogout = () => {
		localStorage.removeItem("adminToken");
		navigate("/dashboard");
		window.location.reload();
	};

	return (
		<div className="flex h-screen ui-surface-bg">
			<aside className="w-64 ui-sidebar shrink-0 flex flex-col min-h-0">
				<div className="px-6 pt-3 pb-3 text-center shrink-0">
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
									? (item.icon as (mode: string) => typeof MessageSquare)(
											currentMode,
										)
									: item.icon;
							const active = isActive(item.href);
							const hasSubModes = "subModes" in item && item.subModes;
							const currentSubLabel =
								hasSubModes && sm
									? item.subModes?.find((s) => s.value === sm.mode)?.label
									: null;
							const otherSub =
								hasSubModes && sm
									? item.subModes?.find((s) => s.value !== sm.mode)
									: null;

							return (
								<li key={item.name}>
									<Link
										to={item.href}
										onClick={
											hasSubModes
												? handleSubModeToggle(item.href, item)
												: undefined
										}
										className={`sidebar-link flex items-center px-4 py-2 transition-colors ${
											active ? "sidebar-link-active" : "sidebar-link-inactive"
										}`}
									>
										<span className="mr-3 text-(--nav-icon)">
											<Icon size={18} strokeWidth={active ? 2.5 : 2} />
										</span>
										{hasSubModes && currentSubLabel ? (
											<span className="flex items-baseline gap-1.5">
												<span className={active ? "font-semibold" : ""}>
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
					<ProviderQuotaPanel />
				</nav>
				<div className="px-4 pb-0.5 shrink-0">
					<LastErrorPills />
					<div className="flex justify-between items-center mb-2">
						<a
							href="https://github.com/hugalafutro/model-hotel"
							target="_blank"
							rel="noopener noreferrer"
							className="sidebar-footer-link flex items-center gap-2 px-2 py-1.5 text-xs text-gray-400 hover:text-white transition-colors rounded-lg hover:bg-white/5"
						>
							<BookOpen size={14} strokeWidth={2} />
							Docs
						</a>
						<button
							type="button"
							onClick={() => setTheme(theme === "dark" ? "light" : "dark")}
							className="sidebar-footer-link flex items-center gap-2 px-2 py-1.5 text-xs text-gray-400 hover:text-white transition-colors rounded-lg hover:bg-white/5 cursor-pointer"
							title={
								theme === "dark"
									? "Switch to light mode"
									: "Switch to dark mode"
							}
						>
							{theme === "dark" ? (
								<Sun size={14} strokeWidth={2} />
							) : (
								<Moon size={14} strokeWidth={2} />
							)}
						</button>
						<a
							href="https://github.com/hugalafutro/model-hotel"
							target="_blank"
							rel="noopener noreferrer"
							aria-label="GitHub repository"
							title={
								updateAvailable
									? `Update available: ${running} → latest`
									: `Running ${running}`
							}
							className={`sidebar-footer-link flex items-center gap-2 px-2 py-1.5 text-xs text-gray-400 hover:text-white transition-colors rounded-lg hover:bg-white/5${updateAvailable ? " shadow-[0_0_8px_rgba(251,191,36,0.4)]" : ""}`}
						>
							<GitBranch size={14} strokeWidth={2} />
							{running}
						</a>
					</div>
					<button
						type="button"
						onClick={() => setShowLogoutConfirm(true)}
						className="w-full sidebar-logout"
					>
						<LogOut size={14} strokeWidth={2} />
						Logout
					</button>
					<SystemStatus />
					{showLogoutConfirm && (
						<ConfirmDialog
							title="Log out?"
							message="You'll need to re-enter your admin token."
							fields={[]}
							confirmLabel="Log out"
							onConfirm={handleLogout}
							onCancel={() => setShowLogoutConfirm(false)}
						/>
					)}
				</div>
			</aside>

			<main className="flex-1 ui-main overflow-auto">
				<div className="p-2 max-w-7xl mx-auto">{children}</div>
			</main>
		</div>
	);
}
