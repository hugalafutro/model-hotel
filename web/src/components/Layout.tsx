import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { Link, useLocation, useNavigate } from "react-router-dom";
import {
	AlertTriangle,
	BookOpen,
	Bot,
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
} from "@/lib/icons";
import { api } from "../api/client";
import { useSidebarMode } from "../context/SidebarModeContext";
import { useTheme } from "../context/ThemeContext";
import { useGitHubVersion } from "../hooks/useGitHubVersion";
import { useReadOnly } from "../hooks/useReadOnly";
import i18next, { LANGUAGE_STORAGE_KEY } from "../i18n";
import {
	type DiscoverySummaryEntry,
	DiscoverySummaryModal,
} from "../pages/Providers/DiscoverySummaryModal";
import { useDiscoveryRetest } from "../pages/Providers/useDiscoveryRetest";
import { isWebAuthnAvailable } from "../utils/webauthn";
import { CollapsibleToggle, useCollapsible } from "./CollapsibleToggle";
import { ConfirmDialog } from "./ConfirmDialog";
import { CountryFlag } from "./CountryFlag";
import { ErrorShelf } from "./ErrorShelf";
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
	const {
		data: stats,
		isError,
		dataUpdatedAt,
	} = useQuery({
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
	// HA fleet membership. Present only while Front Desk is in contact; a
	// standalone instance has no `fleet` block and renders no HA line.
	const fleet = stats?.fleet;
	const haColor = fleet
		? {
				primary: "text-green-400",
				member: "text-green-400",
				warning: "text-orange-400",
				member_sync_blocked: "text-red-400",
			}[fleet.state]
		: "";
	const haValue = fleet
		? {
				primary: t("layout.ha.primary"),
				member: t("layout.ha.member"),
				warning: t("layout.ha.warning"),
				member_sync_blocked: t("layout.ha.error"),
			}[fleet.state]
		: "";
	const haTooltip = fleet
		? {
				primary: t("layout.ha.tooltips.primary"),
				member: fleet.primary_name
					? t("layout.ha.tooltips.memberFrom", { name: fleet.primary_name })
					: t("layout.ha.tooltips.member"),
				warning: t("layout.ha.tooltips.warning"),
				member_sync_blocked: t("layout.ha.tooltips.error"),
			}[fleet.state]
		: "";
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
							// Remount on each successful refetch so the online dot
							// replays its one-shot pulse; offline keeps a constant key
							// so its looping pulse runs uninterrupted.
							key={isError ? "offline" : dataUpdatedAt}
							className={`w-1.5 h-1.5 rounded-full mr-1.5 ${isError ? "bg-red-400 status-dot-offline" : "bg-green-400 status-dot-online"}`}
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
						{/* HA fleet membership (only while managed by a Front Desk) */}
						{fleet && (
							<div
								className="flex justify-between items-center text-(--text-tertiary)"
								title={haTooltip}
								data-testid="ha-status"
							>
								<span>HA</span>
								<span className={haColor}>{haValue}</span>
							</div>
						)}

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

// Language names are autonyms (each language in its own script), shown
// identically in every UI locale — the industry standard for language pickers,
// so a user stranded in the wrong language can still recognize their own.
// English is intentionally last so it sits at the bottom of the upward-opening
// menu (nearest the trigger) in every locale.
const SUPPORTED_LANGUAGES = [
	{ code: "af", label: "Afrikaans" },
	{ code: "ar", label: "العربية" },
	{ code: "ca", label: "Català" },
	{ code: "cs", label: "Čeština" },
	{ code: "da", label: "Dansk" },
	{ code: "de", label: "Deutsch" },
	{ code: "el", label: "Ελληνικά" },
	{ code: "es", label: "Español" },
	{ code: "fi", label: "Suomi" },
	{ code: "fr", label: "Français" },
	{ code: "he", label: "עברית" },
	{ code: "hu", label: "Magyar" },
	{ code: "it", label: "Italiano" },
	{ code: "ja", label: "日本語" },
	{ code: "ko", label: "한국어" },
	{ code: "nl", label: "Nederlands" },
	{ code: "no", label: "Norsk" },
	{ code: "pl", label: "Polski" },
	{ code: "pt", label: "Português" },
	{ code: "ro", label: "Română" },
	{ code: "ru", label: "Русский" },
	{ code: "sk", label: "Slovenčina" },
	{ code: "sr", label: "Српски" },
	{ code: "sv", label: "Svenska" },
	{ code: "tr", label: "Türkçe" },
	{ code: "uk", label: "Українська" },
	{ code: "vi", label: "Tiếng Việt" },
	{ code: "zh", label: "中文" },
	{ code: "en", label: "English" },
] as const;

function LanguageSelector() {
	const { t, i18n } = useTranslation();
	const [open, setOpen] = useState(false);
	const ref = useRef<HTMLDivElement>(null);
	const scrollRef = useRef<HTMLDivElement>(null);

	// Set document direction for RTL languages
	useEffect(() => {
		const rtlLanguages = new Set(["ar", "he"]);
		const lang = i18n.resolvedLanguage as string;
		document.documentElement.dir = rtlLanguages.has(lang) ? "rtl" : "ltr";
	}, [i18n.resolvedLanguage]);

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

	// Scroll the active language into view when dropdown opens
	useEffect(() => {
		if (open && scrollRef.current) {
			const active = scrollRef.current.querySelector("[aria-selected='true']");
			active?.scrollIntoView({ block: "nearest" });
		}
	}, [open]);

	if (SUPPORTED_LANGUAGES.length <= 1) return null;

	return (
		<div ref={ref} className="relative">
			<button
				type="button"
				onClick={() => setOpen((v) => !v)}
				className="sidebar-footer-link flex items-center justify-center px-1.5 py-1.5 text-xs text-gray-400 hover:text-white transition-colors ui-btn hover:bg-white/5"
				title={t("layout.language.label")}
				aria-label={t("layout.language.label")}
				data-testid="language-trigger"
			>
				<Languages size={14} strokeWidth={2} />
			</button>
			{open && (
				// Outer wrapper owns the rounding + border and clips its overflow so
				// the inner scrollbar stays inside the rounded corners instead of
				// painting over them. The scroll lives on the inner element.
				<div className="ui-popover absolute bottom-full left-1/2 -translate-x-1/2 mb-1 min-w-[120px] bg-gray-800 border border-gray-700 rounded-(--radius-card) shadow-lg z-50 overflow-hidden">
					<div
						ref={scrollRef}
						className="py-1 max-h-[50vh] overflow-y-auto overscroll-contain"
						role="listbox"
					>
						{SUPPORTED_LANGUAGES.map((lang) => (
							<button
								key={lang.code}
								type="button"
								role="option"
								aria-selected={
									(i18n.resolvedLanguage ?? i18n.language) === lang.code
								}
								data-testid={`language-option-${lang.code}`}
								id={`language-option-${lang.code}`}
								onClick={() => {
									i18next.changeLanguage(lang.code);
									// Persist every deliberate choice — including English —
									// so the effective priority is strictly
									// user choice > system locale > English. The browser
									// locale is never auto-cached (caches: [] in
									// i18n/index.ts), so an explicit pick always wins on
									// the next visit until the user changes it again.
									localStorage.setItem(LANGUAGE_STORAGE_KEY, lang.code);
									setOpen(false);
								}}
								className={`w-full text-left px-3 py-1.5 text-xs transition-colors flex items-center gap-1.5 ${
									(i18n.resolvedLanguage ?? i18n.language) === lang.code
										? "text-white bg-white/10"
										: "text-gray-400 hover:text-white hover:bg-white/5"
								}`}
							>
								<CountryFlag code={lang.code} />
								{lang.label}
							</button>
						))}
					</div>
				</div>
			)}
		</div>
	);
}

// ReadOnlyBanner is shown on every page when the server runs in read-only
// (demo) mode, explaining why mutation controls are hidden / requests 403.
function ReadOnlyBanner() {
	const { t } = useTranslation();
	const readOnly = useReadOnly();
	if (!readOnly) return null;
	return (
		<div
			role="status"
			data-testid="read-only-banner"
			className="mb-2 flex items-center gap-2 rounded-md border border-[var(--error-border)] bg-[var(--error-bg)] px-3 py-1.5 text-xs text-[var(--error-text)]"
		>
			<AlertTriangle size={14} className="shrink-0 text-[var(--error-icon)]" />
			<span>{t("layout.readOnly.banner")}</span>
		</div>
	);
}

export function Layout({ children }: LayoutProps) {
	const { t } = useTranslation();
	const location = useLocation();
	const navigate = useNavigate();
	const { theme, setTheme, uiStyle } = useTheme();
	// Separator between paired labels/counts in the sidebar. The terminal theme
	// keeps a literal "/" (fits its monospace aesthetic); other themes use a
	// middle dot, which reads as two independent values rather than a fraction.
	const navSep = uiStyle === "cyber-terminal" ? "/" : "·";

	const {
		chatSubMode,
		setChatSubMode,
		arenaSubMode,
		setArenaSubMode,
		logsSubMode,
		setLogsSubMode,
	} = useSidebarMode();

	const { running, latest, commit, updateAvailable } = useGitHubVersion();

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

	// Unseen changes recorded by background discovery → Models nav badge.
	const { data: discoveryChanges } = useQuery({
		queryKey: ["discovery-changes"],
		queryFn: () => api.discovery.changes(),
		refetchInterval: 60_000,
		placeholderData: (prev) => prev,
	});
	const discoveryChangeCount = discoveryChanges?.count ?? 0;
	const [showDiscoveryChanges, setShowDiscoveryChanges] = useState(false);
	const [discoveryChangeEntries, setDiscoveryChangeEntries] = useState<
		DiscoverySummaryEntry[]
	>([]);
	const { onRetest: onRetestDiscovery, retestingKey: discoveryRetestingKey } =
		useDiscoveryRetest((key, diff) =>
			setDiscoveryChangeEntries((prev) =>
				prev.map((e) =>
					(e.entryKey ?? e.providerName) === key ? { ...e, diff } : e,
				),
			),
		);
	// Guards against a re-entrant open (rapid double-click / repeated Enter) while
	// the ack is in flight: the first ack returns and clears the rows, a second
	// would return an empty list and blank the modal. A ref (not state) so the
	// re-entrant call sees the flag synchronously, before any await yields.
	const ackInFlight = useRef(false);

	useEffect(() => {
		const handler = (e: Event) => {
			const detail = (e as CustomEvent).detail;
			if (detail?.type === "discovery.changes_pending") {
				queryClient.invalidateQueries({ queryKey: ["discovery-changes"] });
			}
		};
		window.addEventListener("server-event", handler);
		return () => window.removeEventListener("server-event", handler);
	}, [queryClient]);

	// Ack atomically marks the unseen rows seen and returns exactly those rows, so
	// the modal snapshots from the ack response rather than the possibly-stale
	// query cache — a change recorded between the last poll and this click is shown
	// instead of silently buried. Fall back to the cached entries if ack fails (the
	// badge then stays until a later successful ack).
	const openDiscoveryChanges = async () => {
		if (ackInFlight.current) return;
		ackInFlight.current = true;
		const failoverLabel = t("providers.discoverySummary.failover");
		let entries = discoveryChanges?.entries ?? [];
		try {
			entries = (await api.discovery.ackChanges()).entries;
		} catch {
			// Keep the cached snapshot; the badge persists for a later retry.
		} finally {
			ackInFlight.current = false;
			queryClient.invalidateQueries({ queryKey: ["discovery-changes"] });
		}
		setDiscoveryChangeEntries(
			entries.map((entry, i) => ({
				providerName: entry.provider_name || failoverLabel,
				diff: entry.diff,
				entryKey: `${entry.provider_name}-${entry.detected_at}-${i}`,
				providerId: entry.provider_id,
			})),
		);
		setShowDiscoveryChanges(true);
	};

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
					<Link
						to="/dashboard"
						className="block"
						title={t("layout.nav.dashboard")}
					>
						<Logo className="h-10 w-auto text-white mx-auto" />
						<p className="ui-tagline text-xs text-(--accent) mt-1 italic">
							{t("layout.tagline")}
						</p>
					</Link>
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
													{navSep}
												</span>
												<span className="text-[11px] text-(--text-tertiary)">
													{otherSub?.label}
												</span>
											</span>
										) : item.href === "/failover" &&
											cbStatus &&
											(cbStatus.half_open > 0 || cbStatus.open > 0) ? (
											<span className="flex items-center gap-1.5">
												<span>{item.name}</span>
												<span
													className="inline-flex items-center gap-[2px] leading-[1.6] translate-y-[1px] ui-badge ui-badge-neutral"
													title={(() => {
														// Always explain what the counts mean (they track
														// providers, not groups — a common mix-up).
														const explain = t(
															"layout.nav.failoverBadgeExplain",
															{
																closed: cbStatus.closed,
																halfOpen: cbStatus.half_open,
																open: cbStatus.open,
															},
														);
														const unhealthy = cbStatus.providers?.filter(
															(p) =>
																p.state === "open" || p.state === "half-open",
														);
														if (!unhealthy || unhealthy.length === 0)
															return explain;
														return `${explain}\n${t(
															"layout.nav.failoverBadgeTooltip",
															{
																count: unhealthy.length,
																providers: unhealthy
																	.map((p) => p.provider_name || p.provider_id)
																	.join(", "),
															},
														)}`;
													})()}
												>
													<span className="text-amber-400 badge-text">
														{cbStatus.half_open}
													</span>
													<span className="text-(--text-secondary)">
														{navSep}
													</span>
													<span className="text-red-400 badge-text">
														{cbStatus.open}
													</span>
												</span>
											</span>
										) : item.href === "/models" &&
											discoveryChangeCount > 0 &&
											!showDiscoveryChanges ? (
											<span className="flex items-center gap-1.5">
												<span>{item.name}</span>
												{/* biome-ignore lint/a11y/useSemanticElements: a real <button> can't nest inside the nav <a>; role+keydown make this span an accessible control */}
												<span
													role="button"
													tabIndex={0}
													data-testid="discovery-changes-badge"
													onClick={(e) => {
														e.preventDefault();
														e.stopPropagation();
														void openDiscoveryChanges();
													}}
													onKeyDown={(e) => {
														if (e.key === "Enter" || e.key === " ") {
															e.preventDefault();
															e.stopPropagation();
															void openDiscoveryChanges();
														}
													}}
													className="inline-flex items-center leading-[1.6] translate-y-[1px] ui-badge ui-badge-accent cursor-pointer"
													aria-label={t("layout.nav.discoveryChangesBadge", {
														count: discoveryChangeCount,
													})}
													title={t("layout.nav.discoveryChangesBadge", {
														count: discoveryChangeCount,
													})}
												>
													<span aria-hidden="true" className="opacity-70 mr-px">
														±
													</span>
													{discoveryChangeCount}
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
					<ErrorShelf />
					<div className="flex justify-between items-center mb-2 gap-1">
						<a
							href="https://github.com/hugalafutro/model-hotel/wiki"
							target="_blank"
							rel="noopener noreferrer"
							className="sidebar-footer-link flex items-center gap-2 px-2 py-1.5 text-xs text-gray-400 hover:text-white transition-colors ui-btn hover:bg-white/5"
						>
							<BookOpen size={14} strokeWidth={2} />
							{/* "Wiki" is a fixed brand/proper-noun label for the link to
							    the GitHub wiki — intentionally not translated, so it
							    reads the same in every locale. Not routed through t();
							    see the autonym pattern above. */}
							Wiki
						</a>
						<button
							type="button"
							onClick={() => setTheme(theme === "dark" ? "light" : "dark")}
							className="sidebar-footer-link flex items-center gap-2 px-2 py-1.5 text-xs text-gray-400 hover:text-white transition-colors ui-btn hover:bg-white/5"
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
							title={(() => {
								const base =
									!updateAvailable && latest !== "GitHub"
										? t("layout.runningLatest", { running })
										: updateAvailable
											? t("layout.updateAvailable", { running, latest })
											: t("layout.running", { running });
								// Append the source commit SHA (build stamp, not
								// translatable) so a `dev` build's exact commit is visible.
								// The backend already returns a normalized short SHA.
								return commit ? `${base} · ${commit}` : base;
							})()}
							className={`sidebar-footer-link flex items-center gap-2 px-2 py-1.5 text-xs text-gray-400 hover:text-white transition-colors ui-btn hover:bg-white/5`}
						>
							<span
								className={
									updateAvailable
										? "text-amber-400 [text-shadow:var(--glow-amber)]"
										: ""
								}
							>
								{running}
							</span>
							<GitBranch size={14} strokeWidth={2} />
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
				<div className="p-2 max-w-7xl mx-auto h-full">
					<ReadOnlyBanner />
					{children}
				</div>
			</main>

			{showDiscoveryChanges && (
				<DiscoverySummaryModal
					results={discoveryChangeEntries}
					onClose={() => setShowDiscoveryChanges(false)}
					onRetest={onRetestDiscovery}
					retestingKey={discoveryRetestingKey}
				/>
			)}
		</div>
	);
}
