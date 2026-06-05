import { useQuery, useQueryClient } from "@tanstack/react-query";
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
	Languages,
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
import { useCallback, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { Link, useLocation, useNavigate } from "react-router-dom";
import { api } from "../api/client";
import type { AppLogEntry, LogEntry } from "../api/types";
import { useSidebarMode } from "../context/SidebarModeContext";
import { useTheme } from "../context/ThemeContext";
import { useToast } from "../context/ToastContext";
import { useGitHubVersion } from "../hooks/useGitHubVersion";
import i18next from "../i18n";
import { formatRelativeTime, formatTimestamp } from "../utils/format";
import { isWebAuthnAvailable } from "../utils/webauthn";
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
	const { t } = useTranslation();
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
			<span className={u}> {t("layout.stats.heap")}</span>
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
					title={t("layout.status.apiStatus")}
				>
					<span>{t("layout.status.apiStatus")}</span>
					<span
						className={`flex items-center ${isError ? "text-red-400" : "text-green-400"}`}
					>
						<span
							className={`w-1.5 h-1.5 rounded-full mr-1.5 ${isError ? "bg-red-400" : "bg-green-400"}`}
						/>
						{isError ? t("layout.status.error") : t("layout.status.online")}
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
							title={t("layout.tooltips.uptime")}
						>
							<span>{t("layout.stats.uptime")}</span>
							<span className="text-(--text-secondary)">
								{app ? formatDuration(app.uptime_seconds) : dash}
							</span>
						</div>

						{/* CPU + Processes */}
						<div
							className="flex justify-between items-center text-(--text-tertiary)"
							title={
								useDocker
									? t("layout.stats.aggregateCpu", {
											count: docker.container_count,
										})
									: t("layout.stats.cpu")
							}
						>
							<span>{t("layout.stats.cpu")}</span>
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
														{t("layout.stats.procs", { count: procs })}
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
									? t("layout.stats.aggregateNetwork", {
											count: docker.container_count,
										})
									: t("layout.stats.network")
							}
						>
							<span>{t("layout.stats.network")}</span>
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
									? t("layout.stats.aggregateDisk", {
											count: docker.container_count,
										})
									: t("layout.stats.disk")
							}
						>
							<span>{t("layout.stats.disk")}</span>
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
									? t("layout.stats.aggregateMemory", {
											count: docker.container_count,
										})
									: t("layout.stats.memory")
							}
						>
							<span>{t("layout.stats.memory")}</span>
							<span
								className={`text-(--text-secondary) ${dc(memUsagePct, 75, 90)}`}
							>
								{app ? appMem : dash}
							</span>
						</div>

						{/* Goroutines */}
						<div
							className="flex justify-between items-center text-(--text-tertiary)"
							title={t("layout.tooltips.goroutines")}
						>
							<span>{t("layout.stats.goroutines")}</span>
							<span
								className={`text-(--text-secondary) ${dc(app?.goroutines, 300, 1000)}`}
							>
								{app ? app.goroutines.toLocaleString() : dash}
							</span>
						</div>

						{/* Requests Today */}
						<div
							className="flex justify-between items-center text-(--text-tertiary)"
							title={t("layout.tooltips.requestsToday")}
						>
							<span>{t("layout.stats.requestsToday")}</span>
							<span className="text-(--text-secondary)">
								{app && app.requests_today > 0
									? formatNumber(app.requests_today)
									: dash}
							</span>
						</div>

						{/* DB: size & hit ratio / connections & tx/sec */}
						<div className="flex justify-between items-center text-(--text-tertiary)">
							<span className="self-center">{t("layout.stats.db")}</span>
							<span className="grid grid-cols-[1fr_auto_1fr] grid-rows-[auto_auto] gap-x-2 items-center text-right">
								{stats?.db ? (
									<>
										<span
											className="text-(--text-secondary)"
											title={t("layout.tooltips.dbSize")}
										>
											{formatMB(stats.db.size_mb)}
										</span>
										<span className="text-(--text-secondary)">|</span>
										<span
											className={`text-(--text-secondary) ${dc(stats.db.cache_hit_ratio, 90, 80, true)}`}
											title={t("layout.tooltips.dbHitRatio")}
										>
											{t("layout.stats.hit")} {stats.db.cache_hit_ratio}
											<span className={u}>%</span>
										</span>
										<span
											className="text-(--text-secondary)"
											title={t("layout.tooltips.dbConnections")}
										>
											{stats.db.connections}
											<span className={u}> {t("layout.stats.conn")}</span>
										</span>
										<span className="text-(--text-secondary)">|</span>
										<span
											className="text-(--text-secondary)"
											title={t("layout.tooltips.dbTxPerSec")}
										>
											{stats.db.tx_per_sec.toFixed(1)}
											<span className={u}> {t("layout.stats.txPerSec")}</span>
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
					expandTitle={t("layout.expandStats")}
					collapseTitle={t("layout.collapseStats")}
				/>
			</div>
		</div>
	);
}

interface LayoutProps {
	children: React.ReactNode;
}

function LastErrorPills() {
	const { t } = useTranslation();
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
		<div className="group relative rounded-md border border-[var(--error-border)] bg-[var(--error-bg)] overflow-hidden">
			{/* Header row with icon, label, and action buttons */}
			<div className="flex items-center justify-between px-2 py-px bg-[var(--error-bg-strong)]">
				<div className="flex items-center gap-1.5">
					<AlertTriangle
						size={10}
						className="shrink-0 text-[var(--error-icon)]"
					/>
					<span
						className="text-[10px] font-semibold text-[var(--error-text)] uppercase tracking-wider"
						title={timestamp ? formatTimestamp(timestamp) : undefined}
					>
						{timestamp
							? `${label === "App" ? t("layout.errorPill.appError") : t("layout.errorPill.requestError")} ${formatRelativeTime(timestamp)}`
							: `${label}${t("layout.errorPill.error")}`}
					</span>
				</div>
				<div className="flex gap-0.5">
					<button
						type="button"
						onClick={(e) => {
							e.stopPropagation();
							navigator.clipboard.writeText(msg);
							toast(t("common.copiedToClipboard"), "info");
						}}
						className="p-0.5 rounded text-[var(--error-text-muted)] hover:text-[var(--error-text)] hover:bg-[var(--error-bg-strong)] transition-colors cursor-pointer"
						title={t("layout.errorPill.copyError")}
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
						className="p-0.5 rounded text-[var(--error-text-muted)] hover:text-[var(--error-text)] hover:bg-[var(--error-bg-strong)] transition-colors cursor-pointer"
						title={t("layout.errorPill.viewDetails")}
					>
						<ExternalLink size={10} />
					</button>
					<button
						type="button"
						onClick={(e) => {
							e.stopPropagation();
							onAcknowledge();
							toast(
								label === "App"
									? t("layout.toast.appErrorAcknowledged")
									: t("layout.toast.requestErrorAcknowledged"),
								"info",
							);
						}}
						className="p-0.5 rounded text-[var(--error-text-muted)] hover:text-[var(--error-text)] hover:bg-[var(--error-bg-strong)] transition-colors cursor-pointer"
						title={t("layout.errorPill.acknowledge")}
					>
						<X size={10} />
					</button>
				</div>
			</div>
			{/* Error message body */}
			<div className="px-2 py-1">
				<div
					className="font-mono text-[9.5px] text-[var(--error-text-muted)] break-words leading-relaxed line-clamp-3"
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
							toast(t("layout.toast.appErrorAcknowledged"), "info");
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
							toast(t("layout.toast.requestErrorAcknowledged"), "info");
						},
						lastReqTimestamp ?? null,
						lastReqEntry,
					)}
			</div>
		</>
	);
}

const SUPPORTED_LANGUAGES = [
	{ code: "en", labelKey: "layout.language.english" },
] as const;

function LanguageSelector() {
	const { t, i18n } = useTranslation();
	const [open, setOpen] = useState(false);
	const ref = useRef<HTMLDivElement>(null);

	useEffect(() => {
		function handleClickOutside(e: MouseEvent) {
			if (ref.current && !ref.current.contains(e.target as Node)) {
				setOpen(false);
			}
		}
		if (open) {
			document.addEventListener("mousedown", handleClickOutside);
			return () =>
				document.removeEventListener("mousedown", handleClickOutside);
		}
	}, [open]);

	if (SUPPORTED_LANGUAGES.length <= 1) return null;

	return (
		<div ref={ref} className="relative">
			<button
				type="button"
				onClick={() => setOpen((v) => !v)}
				className="sidebar-footer-link flex items-center justify-center px-1.5 py-1.5 text-xs text-gray-400 hover:text-white transition-colors rounded-lg hover:bg-white/5 cursor-pointer"
				title={t("layout.language.label")}
				aria-label={t("layout.language.label")}
			>
				<Languages size={14} strokeWidth={2} />
			</button>
			{open && (
				<div className="absolute bottom-full left-1/2 -translate-x-1/2 mb-1 py-1 min-w-[120px] bg-gray-800 border border-gray-700 rounded-lg shadow-lg z-50">
					{SUPPORTED_LANGUAGES.map((lang) => (
						<button
							key={lang.code}
							type="button"
							onClick={() => {
								i18next.changeLanguage(lang.code);
								setOpen(false);
							}}
							className={`w-full text-left px-3 py-1.5 text-xs transition-colors cursor-pointer ${
								i18n.language === lang.code
									? "text-white bg-white/10"
									: "text-gray-400 hover:text-white hover:bg-white/5"
							}`}
						>
							{t(lang.labelKey)}
						</button>
					))}
				</div>
			)}
		</div>
	);
}

export function Layout({ children }: LayoutProps) {
	const { t } = useTranslation();
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

	const { running, latest, updateAvailable } = useGitHubVersion();

	const { data: cbStatus } = useQuery({
		queryKey: ["circuit-breaker-status"],
		queryFn: () => api.failoverGroups.circuitBreakerStatus(true),
		refetchInterval: 15_000,
		placeholderData: (prev) => prev,
	});

	// Invalidate CB status on circuit_breaker SSE events for real-time badge updates
	const queryClient = useQueryClient();
	useEffect(() => {
		const handler = (e: Event) => {
			const detail = (e as CustomEvent).detail;
			if (detail?.type?.startsWith("circuit_breaker.")) {
				queryClient.invalidateQueries({ queryKey: ["circuit-breaker-status"] });
			}
		};
		window.addEventListener("server-event", handler);
		return () => window.removeEventListener("server-event", handler);
	}, [queryClient]);

	const navigation = [
		{
			name: t("layout.nav.dashboard"),
			href: "/dashboard",
			icon: LayoutDashboard,
		},
		{
			name: t("layout.nav.chat"),
			href: "/chat",
			icon: (mode: string) =>
				mode === "conversation" ? MessagesSquare : MessageSquare,
			subModes: [
				{ label: t("layout.nav.chat"), value: "chat" as const },
				{ label: t("layout.nav.conversation"), value: "conversation" as const },
			],
		},
		{
			name: t("layout.nav.arena"),
			href: "/arena",
			icon: (mode: string) => (mode === "compare" ? GitCompare : Swords),
			subModes: [
				{ label: t("layout.nav.arena"), value: "competition" as const },
				{ label: t("layout.nav.compare"), value: "compare" as const },
			],
		},
		{ name: t("layout.nav.providers"), href: "/providers", icon: PlugZap },
		{ name: t("layout.nav.models"), href: "/models", icon: Bot },
		{ name: t("layout.nav.failover"), href: "/failover", icon: Shuffle },
		{
			name: t("layout.nav.virtualKeys"),
			href: "/virtual-keys",
			icon: KeyRound,
		},
		{
			name: t("layout.nav.logs"),
			href: "/logs",
			icon: (mode: string) => (mode === "app" ? FileText : ScrollText),
			subModes: [
				{ label: t("layout.nav.requests"), value: "request" as const },
				{ label: t("layout.nav.appLogs"), value: "app" as const },
			],
		},
		{ name: t("layout.nav.settings"), href: "/settings", icon: Settings },
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

	const handleLogout = async () => {
		try {
			const available = await isWebAuthnAvailable();
			if (available) {
				await api.webauthn.logout();
			}
		} catch {
			// Server-side logout failure is non-fatal.
		}
		localStorage.removeItem("adminToken");
		navigate("/dashboard");
		window.location.reload();
	};

	return (
		<div className="flex h-screen ui-surface-bg">
			<aside className="w-64 ui-sidebar shrink-0 flex flex-col min-h-0">
				<div className="px-6 pt-3 pb-3 text-center shrink-0">
					<Logo className="h-10 w-auto text-white mx-auto" />
					<p className="text-sm text-gray-200 mt-1">{t("layout.subtitle")}</p>
					<p className="text-xs text-(--accent) mt-0.5 italic">
						{t("layout.tagline")}
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
										) : item.href === "/failover" &&
											cbStatus &&
											(cbStatus.closed > 0 ||
												cbStatus.half_open > 0 ||
												cbStatus.open > 0) ? (
											<span className="flex items-center gap-1.5">
												<span>{item.name}</span>
												<span
													className="inline-flex items-center gap-[2px] text-[0.625rem] leading-[1.6] font-medium bg-white/10 px-[7px] py-[1px] rounded-full translate-y-[1px]"
													title={(() => {
														if (!cbStatus.providers) return undefined;
														const unhealthy = cbStatus.providers.filter(
															(p) =>
																p.state === "open" || p.state === "half-open",
														);
														if (unhealthy.length === 0) return undefined;
														return t("layout.nav.failoverBadgeTooltip", {
															count: unhealthy.length,
															providers: unhealthy
																.map((p) => p.provider_name || p.provider_id)
																.join(", "),
														});
													})()}
												>
													<span
														className="text-emerald-400 badge-text"
														title={t("layout.nav.failoverClosed", {
															count: cbStatus.closed,
														})}
													>
														{cbStatus.closed}
													</span>
													<span className="text-(--text-muted) opacity-50">
														/
													</span>
													<span
														className="text-amber-400 badge-text"
														title={t("layout.nav.failoverHalfOpen", {
															count: cbStatus.half_open,
														})}
													>
														{cbStatus.half_open}
													</span>
													<span className="text-(--text-muted) opacity-50">
														/
													</span>
													<span
														className="text-red-400 badge-text"
														title={t("layout.nav.failoverOpen", {
															count: cbStatus.open,
														})}
													>
														{cbStatus.open}
													</span>
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
					<div className="flex justify-between items-center mb-2 gap-1">
						<a
							href="https://github.com/hugalafutro/model-hotel"
							target="_blank"
							rel="noopener noreferrer"
							className="sidebar-footer-link flex items-center gap-2 px-2 py-1.5 text-xs text-gray-400 hover:text-white transition-colors rounded-lg hover:bg-white/5"
						>
							<BookOpen size={14} strokeWidth={2} />
							{t("layout.docs")}
						</a>
						<button
							type="button"
							onClick={() => setTheme(theme === "dark" ? "light" : "dark")}
							className="sidebar-footer-link flex items-center gap-2 px-2 py-1.5 text-xs text-gray-400 hover:text-white transition-colors rounded-lg hover:bg-white/5 cursor-pointer"
							title={
								theme === "dark"
									? t("layout.theme.switchToLight")
									: t("layout.theme.switchToDark")
							}
						>
							{theme === "dark" ? (
								<Sun size={14} strokeWidth={2} />
							) : (
								<Moon size={14} strokeWidth={2} />
							)}
						</button>
						<LanguageSelector />
						<a
							href="https://github.com/hugalafutro/model-hotel"
							target="_blank"
							rel="noopener noreferrer"
							aria-label={t("layout.githubRepo")}
							title={
								!updateAvailable && latest !== "GitHub"
									? t("layout.runningLatest", { running })
									: updateAvailable
										? t("layout.updateAvailable", { running, latest })
										: t("layout.running", { running })
							}
							className={`sidebar-footer-link flex items-center gap-2 px-2 py-1.5 text-xs text-gray-400 hover:text-white transition-colors rounded-lg hover:bg-white/5`}
						>
							<GitBranch size={14} strokeWidth={2} />
							<span
								className={
									updateAvailable
										? "text-amber-400 outline-solid outline-1 outline-amber-400/60 rounded px-0.5"
										: ""
								}
							>
								{running}
							</span>
						</a>
					</div>
					<button
						type="button"
						onClick={() => setShowLogoutConfirm(true)}
						className="w-full sidebar-logout"
					>
						<LogOut size={14} strokeWidth={2} />
						{t("layout.auth.logout")}
					</button>
					<SystemStatus />
					{showLogoutConfirm && (
						<ConfirmDialog
							title={t("layout.auth.logoutConfirm")}
							message={t("layout.auth.logoutMessage")}
							fields={[]}
							confirmLabel={t("layout.auth.logout")}
							onConfirm={handleLogout}
							onCancel={() => setShowLogoutConfirm(false)}
						/>
					)}
				</div>
			</aside>

			<main className="flex-1 ui-main overflow-auto">
				<div className="p-2 max-w-7xl mx-auto h-full">{children}</div>
			</main>
		</div>
	);
}
