import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useCallback, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { Link, useLocation } from "react-router";
import {
	AlertTriangle,
	BookOpen,
	Bot,
	FileText,
	GitBranch,
	GitCompare,
	History as HistoryIcon,
	KeyRound,
	Languages,
	LayoutDashboard,
	MessageSquare,
	MessagesSquare,
	Moon,
	PlugZap,
	PowerOff,
	ScrollText,
	Settings,
	ShieldCheck,
	Shuffle,
	Sun,
	Swords,
	Users as UsersIcon,
} from "@/lib/icons";
import { api, clearAuth } from "../api/client";
import { useIdentity } from "../context/IdentityContext";
import { useSidebarMode } from "../context/SidebarModeContext";
import { useTheme } from "../context/ThemeContext";
import { useToast } from "../context/ToastContext";
import {
	type MergedProvider,
	providerHasNoPending,
	retestProvesNothing,
	useDiscrepancies,
} from "../hooks/useDiscrepancies";
import { useGitHubVersion } from "../hooks/useGitHubVersion";
import { useIdleLogout } from "../hooks/useIdleLogout";
import { useManaged } from "../hooks/useManaged";
import { useReadOnly } from "../hooks/useReadOnly";
import { useRefreshDiscoveryBadge } from "../hooks/useRefreshDiscoveryBadge";
import i18next, { LANGUAGE_STORAGE_KEY } from "../i18n";
import { useDiscoveryRetest } from "../pages/Providers/useDiscoveryRetest";
import { CollapsibleToggle, useCollapsible } from "./CollapsibleToggle";
import { ConfirmDialog } from "./ConfirmDialog";
import { CountryFlag } from "./CountryFlag";
import { ErrorShelf } from "./ErrorShelf";
import { Logo } from "./Logo";
import { ModelDiscrepancyModal } from "./ModelDiscrepancyModal";
import { ProviderQuotaPanel } from "./ProviderQuotaPanel";
import {
	formatCount,
	formatMemoryMB,
	formatThroughput,
	formatUptime,
	unitClass,
} from "./systemStatusFormat";

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

	// A cache-hit sample over a near-idle window (one autovacuum scan, a stray
	// catalog read) says nothing about cache health, so below this many block
	// accesses the hit cell shows a dash instead of colour-coding noise. The
	// backend omits cache_window_blocks entirely for no-window samples.
	const cacheHitLive = (stats?.db?.cache_window_blocks ?? 0) >= 1000;

	const dockerMem = useDocker && docker.memory_limit_bytes > 0;
	const memUsagePct = dockerMem
		? (docker.memory_usage_bytes / docker.memory_limit_bytes) * 100
		: hasLimit && app?.memory_limit_bytes
			? (app.memory_current_bytes / app.memory_limit_bytes) * 100
			: undefined;
	const appMem = dockerMem ? (
		<>
			{formatMemoryMB(docker.memory_usage_bytes / 1024 / 1024)} /{" "}
			{formatMemoryMB(docker.memory_limit_bytes / 1024 / 1024)}
		</>
	) : hasLimit ? (
		<>
			{formatMemoryMB(app.memory_current_bytes / 1024 / 1024)} /{" "}
			{formatMemoryMB(app.memory_limit_bytes / 1024 / 1024)}
		</>
	) : app ? (
		<>
			{formatMemoryMB(app.heap_alloc_mb)}
			<span className={unitClass}> {t("layout.stats.heap")}</span>
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
								{app ? formatUptime(app.uptime_seconds) : dash}
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
											<span className={unitClass}>%</span>
										</span>
										{procs != null && procs > 0 && (
											<>
												<span className="text-(--text-secondary) mx-1">|</span>
												<span>
													{procs}
													<span className={unitClass}>
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
										<>↓{formatThroughput(netRx)}</>
									) : (
										dash
									)}
								</span>
								<span className="text-amber-400/60 inline-block min-w-22 text-right">
									{typeof netTx === "number" ? (
										<>↑{formatThroughput(netTx)}</>
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
										<>↓{formatThroughput(diskRead)}</>
									) : (
										dash
									)}
								</span>
								<span className="text-amber-400/60 inline-block min-w-22 text-right">
									{typeof diskWrite === "number" ? (
										<>↑{formatThroughput(diskWrite)}</>
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
									? formatCount(app.requests_today)
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
											{formatMemoryMB(stats.db.size_mb)}
										</span>
										<span className="text-(--text-secondary)">|</span>
										<span
											className={`text-(--text-secondary) ${cacheHitLive ? dc(stats.db.cache_hit_ratio, 90, 80, true) : ""}`}
											title={t("layout.tooltips.dbHitRatio")}
										>
											{t("layout.stats.hit")}{" "}
											{cacheHitLive ? (
												<>
													{stats.db.cache_hit_ratio}
													<span className={unitClass}>%</span>
												</>
											) : (
												dash
											)}
										</span>
										<span
											className="text-(--text-secondary)"
											title={t("layout.tooltips.dbConnections")}
										>
											{stats.db.connections}
											<span className={unitClass}>
												{" "}
												{t("layout.stats.conn")}
											</span>
										</span>
										<span className="text-(--text-secondary)">|</span>
										<span
											className="text-(--text-secondary)"
											title={t("layout.tooltips.dbTxPerSec")}
										>
											{stats.db.tx_per_sec.toFixed(1)}
											<span className={unitClass}>
												{" "}
												{t("layout.stats.txPerSec")}
											</span>
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
				className="sidebar-footer-link text-gray-400 hover:text-white ui-btn hover:bg-white/5"
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
	const { isAdmin, can, me } = useIdentity();
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

	const { running, latest, commit, isDev, updateAvailable } =
		useGitHubVersion();

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
			// The scheduled-disable sweep flips providers off outside any user
			// action, so every open tab must refetch what denormalizes provider
			// enabled state: the providers list (cards + quota badges hide via
			// the enabled filter) and the failover groups the sweep re-synced.
			if (detail?.type === "provider.scheduled_disable") {
				queryClient.invalidateQueries({ queryKey: ["providers"] });
				queryClient.invalidateQueries({ queryKey: ["failover-groups"] });
			}
		};
		window.addEventListener("server-event", handler);
		return () => window.removeEventListener("server-event", handler);
	}, [queryClient]);

	// Live discrepancy state → Models nav badge. `claim_count` is what the number
	// shows; `informational_unseen` only ever produces a dot.
	//
	// `status(false)`, never `status(true)`: the review variant stamps the
	// server-side last-reviewed marker, and a 60s poll doing that would collapse
	// every "since your last visit" flap count to zero, permanently. Only the
	// modal-open fetch inside useDiscrepancies is allowed to stamp.
	const { data: discoveryStatus } = useQuery({
		queryKey: ["discovery-status"],
		queryFn: () => api.discovery.status(false),
		refetchInterval: 60_000,
		placeholderData: (prev) => prev,
	});
	const claimCount = discoveryStatus?.claim_count ?? 0;
	const informationalUnseen = discoveryStatus?.informational_unseen ?? 0;
	// A pinned model moves neither counter: it is never counted (it is a decision,
	// not a problem) and it is not informational news. The badge is the only way
	// into the modal, so without this a status whose sole content is a pin renders
	// no badge at all and the pinned bucket, with its Unpin control, cannot be
	// reached. `?? []` because a server that predates the pin omits the bucket.
	const hasPinned =
		discoveryStatus?.claims.some((p) => (p.pinned ?? []).length > 0) ?? false;
	// One string for the badge's accessible name AND its tooltip, deliberately.
	// The dot carries no visible text, so this IS its accessible name; splitting
	// them would mean a sighted user reads one sentence and a screen-reader user
	// hears another. The counted branches name the count the badge is triggered by
	// (for the news dot the UNSEEN count, not the zone's total, which is what makes
	// it legible next to the modal's "Recent changes" header). Counted keys, so the
	// number agrees in every language.
	//
	// The pin branch is last and carries no count: it is what a dot lit by a pin
	// alone says, and borrowing the news key there would announce "0 unreviewed
	// changes" for a badge that is not about news at all.
	const discoveryBadgeLabel =
		claimCount > 0
			? t("layout.nav.discoveryClaimsBadge", { count: claimCount })
			: informationalUnseen > 0
				? t("layout.nav.discoveryNewsBadge", { count: informationalUnseen })
				: t("layout.nav.discoveryPinnedBadge");
	const [showDiscrepancies, setShowDiscrepancies] = useState(false);
	// Called unconditionally at Layout's top level, and Layout never unmounts
	// while the dashboard is up. That is load-bearing, not incidental: the hook
	// keys its fetch on a session counter that only advances on an open
	// transition, so unmounting it on close would reset the counter, replay the
	// first query from cache on reopen, and stop the per-open ?review=1 stamp.
	const {
		snapshot,
		groupClaims,
		informational,
		refresh,
		loading: discrepanciesLoading,
		isError: discrepanciesFailed,
		error: discrepanciesError,
		refreshError,
		dismissClaim,
	} = useDiscrepancies(showDiscrepancies);
	const [retestErrors, setRetestErrors] = useState<Record<string, string>>({});
	const [retestAllProgress, setRetestAllProgress] = useState<
		{ done: number; total: number } | undefined
	>(undefined);
	const { toast } = useToast();
	const readOnly = useReadOnly();
	// The pin is synced config, so on a managed member the modal's Unpin control
	// is the primary's to use: this member's next sync pass re-applies the
	// primary's list either way.
	const managed = useManaged();

	// Requirement 5 keeps the ?review=1 stamp off the 60s timer; the `exact: true`
	// inside this hook is what keeps it off a click. See useRefreshDiscoveryBadge.
	//
	// Retests do NOT call it from here: useDiscoveryRetest owns that, so a retest
	// run from the Providers page refreshes the badge too.
	const refreshBadge = useRefreshDiscoveryBadge();

	// The retest response's own diff is deliberately discarded. It describes what
	// THAT run changed, which is empty when the model is still missing, and
	// reading that emptiness as "fixed" is the original defect. Truth comes from
	// re-reading /api/discovery/status instead.
	const { retestAsync, retestingKey, isAnyRetesting } = useDiscoveryRetest(
		() => {},
	);

	// Serialises retests inside this component. A ref, not `isAnyRetesting`: a
	// running walk holds a closure from one render, so any state read inside it is
	// frozen at that render's value and cannot act as a lock.
	const retestInFlight = useRef(false);

	const runRetest = useCallback(
		async (
			providerId: string,
			providerName: string,
			// The walk silences the shared hook's per-provider toast and reports once
			// at the end: eight providers would otherwise stack eight toasts, none of
			// which ToastContext can dedupe because each names a different provider.
			silent = false,
		): Promise<boolean> => {
			if (retestInFlight.current) return false;
			retestInFlight.current = true;
			try {
				await retestAsync(
					{
						providerName,
						providerId,
						// keyOf() prefers entryKey, so this is what `retestingKey` becomes
						// and is what the modal matches against `provider_id`.
						entryKey: providerId,
					},
					silent,
				);
				setRetestErrors((prev) => {
					if (!(providerId in prev)) return prev;
					const next = { ...prev };
					delete next[providerId];
					return next;
				});
				await refresh();
				return true;
			} catch (err) {
				// Recorded per provider, not only toasted: a toast fades before it is
				// read, and the section must keep saying why it did not clear.
				setRetestErrors((prev) => ({
					...prev,
					[providerId]: err instanceof Error ? err.message : String(err),
				}));
				return false;
			} finally {
				retestInFlight.current = false;
			}
		},
		[retestAsync, refresh],
	);

	// Set by Cancel, read at the top of each walk iteration.
	const cancelRetestAll = useRef(false);
	const onCancelRetestAll = useCallback(() => {
		cancelRetestAll.current = true;
	}, []);

	const onRetestAll = useCallback(async () => {
		// Same predicate the modal's controls use, and applied HERE because this is
		// the walk. Gating only the button meant a mixed fleet still retested the
		// retired-only providers: the button rendered for the one that needed it,
		// and the walk then visited every provider with anything pending, each
		// pointless one costing a slow upstream call while its own pill sat
		// disabled saying so. The progress readout counted them too.
		const targets = snapshot.filter(
			(p: MergedProvider) =>
				!providerHasNoPending(p) && !retestProvesNothing(p),
		);
		if (targets.length === 0 || retestInFlight.current) return;
		cancelRetestAll.current = false;
		setRetestAllProgress({ done: 0, total: targets.length });
		let failed = 0;
		let done = 0;
		for (const p of targets) {
			// Checked here and nowhere else: Cancel stops the walk BEFORE the next
			// provider starts, so the run already in flight finishes. Aborting a
			// discovery request mid-call would leave that provider half-applied.
			if (cancelRetestAll.current) break;
			if (!(await runRetest(p.provider_id, p.provider_name, true))) failed++;
			done++;
			setRetestAllProgress({ done, total: targets.length });
		}
		// Read before the reset: the summary below must know whether the walk ran
		// out of providers or was stopped.
		const cancelled = cancelRetestAll.current;
		cancelRetestAll.current = false;
		setRetestAllProgress(undefined);
		// One report for the whole walk, since the per-provider toasts are silenced.
		// Failures also stay bannered in their own provider section, so this is a
		// summary and not the only record.
		//
		// Cancellation is reported ahead of failures precisely because it is the one
		// outcome with no other record: a failed provider keeps its banner, but a
		// walk that stopped early leaves untouched providers looking retested.
		if (cancelled) {
			toast(
				t("providers.discrepancies.retestAllCancelled", { count: done }),
				"info",
			);
		} else if (failed > 0) {
			toast(
				t("providers.discrepancies.retestAllFailed", { count: failed }),
				"error",
			);
		} else {
			toast(
				t("providers.discrepancies.retestAllDone", { count: done }),
				"success",
			);
		}
	}, [snapshot, runRetest, toast, t]);

	const onDismissAll = useCallback(
		async (providerId: string, modelIds: string[]) => {
			if (modelIds.length === 0) return;
			// NOT optimistic, unlike the per-row path, and that is the whole point.
			//
			// A batch can come back short, and `updated` does not say WHICH ids it
			// missed. Marking every requested row dismissed up front and correcting
			// afterwards cannot be made to work: only a successful refresh knows which
			// took, `refresh` absorbs its own failure, and the rollback that was tried
			// here could not survive a concurrent refresh.
			//
			// The cost of getting it wrong is not cosmetic. With every row dismissed,
			// providerHasNoPending goes true, so the pill swaps Retest and Dismiss all
			// for Clean: the operator loses the controls for models the server never
			// dismissed. A refresh-error banner does not give those back.
			//
			// So nothing is claimed that the server did not confirm. A full-count
			// response is confirmation for every id; a short one is confirmation for
			// none of them in particular, so the rows stay actionable and the refresh
			// reconciles. Costs the strike-through for one round trip, behind a
			// confirmation dialog, which is a fair price for never showing a provider
			// as clean when it is not.
			try {
				const res = await api.discovery.dismiss(providerId, modelIds);
				// The response NAMES what it dismissed, so a partial result needs no
				// guessing: mark exactly those and leave the rest pending for the
				// refresh. Marking all of them would strike through models the server
				// skipped; marking none would let the merge read the ones that did land
				// as "listed again", and could swap the pill to Clean on a partial.
				dismissClaim(providerId, new Set(res.dismissed));
				await refresh();
				if (res.updated < modelIds.length) {
					toast(
						t("providers.discrepancies.dismissAllPartial", {
							count: res.updated,
							total: modelIds.length,
						}),
						"warning",
					);
					return;
				}
				toast(
					t("providers.discrepancies.dismissAllDone", { count: res.updated }),
					"success",
				);
			} catch (err) {
				// No rollback needed: nothing was claimed before the response.
				toast(
					t("providers.discrepancies.dismissFailed", {
						message: err instanceof Error ? err.message : String(err),
					}),
					"error",
				);
			} finally {
				// `finally`, not the success path: a request that rejects can still
				// have landed — the response is what was lost — and the rows correctly
				// stay actionable either way. Re-reading the badge is how it learns
				// which of the two happened.
				refreshBadge();
			}
		},
		[dismissClaim, refresh, refreshBadge, toast, t],
	);

	const onDismissEverything = useCallback(
		async (batches: { providerID: string; modelIDs: string[] }[]) => {
			if (batches.length === 0) return;
			const total = batches.reduce((n, b) => n + b.modelIDs.length, 0);
			const results = await Promise.allSettled(
				batches.map((b) => api.discovery.dismiss(b.providerID, b.modelIDs)),
			);
			// Per batch, claim only what that provider's own response confirmed; see
			// onDismissAll for why nothing is claimed up front. A rejected batch and a
			// short batch are both "not confirmed" and both stay actionable, so no
			// rollback is needed for either.
			let dismissed = 0;
			batches.forEach((b, i) => {
				const r = results[i];
				if (r.status !== "fulfilled") return;
				dismissed += r.value.updated;
				dismissClaim(b.providerID, new Set(r.value.dismissed));
			});
			await refresh();
			// After the batches, not per batch: one invalidation covers every
			// provider's worth of dismissals. Unconditional even when nothing was
			// confirmed, because a batch that rejects can still have landed — the
			// response is what was lost — and the badge is better re-read than
			// inferred from a count the client only partly knows.
			refreshBadge();
			if (dismissed === 0) {
				toast(t("providers.discrepancies.dismissEverythingFailed"), "error");
				return;
			}
			toast(
				dismissed < total
					? t("providers.discrepancies.dismissAllPartial", {
							count: dismissed,
							total,
						})
					: t("providers.discrepancies.dismissAllDone", { count: dismissed }),
				dismissed < total ? "warning" : "success",
			);
		},
		[dismissClaim, refresh, refreshBadge, toast, t],
	);

	const onDismiss = useCallback(
		async (providerId: string, modelId: string) => {
			try {
				// Exactly one model per request. The endpoint 200s with a short
				// `updated` for a mixed list and only 404s when NOTHING matched, so a
				// batch cannot say which models it missed; one at a time makes
				// `updated: 0` an unambiguous failure for this model.
				const res = await api.discovery.dismiss(providerId, [modelId]);
				// Unreachable today: the server 404s (and fetchJSON throws before `res`
				// exists) whenever `affected == 0`, so a 0-updated response cannot reach
				// this branch. Kept as the guard for the one-model-per-call contract
				// described above, in case the server ever starts 200ing on a partial or
				// zero-count match.
				if (res.updated === 0) {
					throw new Error(t("providers.discrepancies.dismissNoMatch"));
				}
				// Marked AFTER the server confirms, never before, and from the ids the
				// response names, which is the same rule the provider-wide and
				// modal-wide paths follow.
				//
				// Marking it up front raced any refresh that landed while the request was
				// out: that refresh still saw the model reported, so it rebuilt the row as
				// `pending` and dropped the dismissed status. The next refresh then found
				// the model absent, and an absent row that is not marked dismissed reads
				// as `resolved` - "is listed again" - for a model the operator had just
				// dismissed by hand. Exactly the false relist this rework exists to
				// remove. Setting the status only once the write is confirmed leaves
				// nothing for a refresh to strip.
				dismissClaim(providerId, new Set(res.dismissed));
				await refresh();
				toast(
					t("providers.discrepancies.dismissed", { model: modelId }),
					"success",
				);
			} catch (err) {
				// No rollback: nothing was claimed before the response.
				toast(
					t("providers.discrepancies.dismissFailed", {
						message: err instanceof Error ? err.message : String(err),
					}),
					"error",
				);
			} finally {
				// See onDismissAll: a rejected request can still have landed.
				refreshBadge();
			}
		},
		[dismissClaim, refresh, refreshBadge, toast, t],
	);

	// Hands one pinned model back to automatic management.
	//
	// One model per request, like onDismiss and for the same reason: the response
	// names the ids it cleared, and asking about one makes a short answer
	// unambiguous. There is no `updated` count to read here; the server 404s (and
	// fetchJSON throws) when nothing matched.
	//
	// The row is marked with `dismissClaim` rather than being removed, once the
	// server confirms. An unpinned model leaves /api/discovery/status outright -
	// the pin is gone and the miss streak is reset, so there is no claim left to
	// report - and that absence is caused by the operator, exactly like a
	// dismissal. Left unmarked, the next refresh would read it as `resolved` and
	// the cleared summary would announce "is listed again" for a model whose
	// listing has not changed at all.
	const onUnpin = useCallback(
		async (providerId: string, modelId: string) => {
			try {
				const res = await api.discovery.unpin(providerId, [modelId]);
				dismissClaim(providerId, new Set(res.unpinned));
				toast(
					t("providers.discrepancies.unpinned", { model: modelId }),
					"success",
				);
			} catch (err) {
				// No rollback: nothing was claimed before the response.
				toast(
					t("providers.discrepancies.unpinFailed", {
						message: err instanceof Error ? err.message : String(err),
					}),
					"error",
				);
			} finally {
				// See onDismiss: a rejected request can still have landed, so both
				// reads happen on either path. The badge is the half unique to a pin:
				// a pinned row is what keeps it lit when nothing is counted, so the
				// last unpin has to be able to put it out.
				await refresh();
				refreshBadge();
			}
		},
		[dismissClaim, refresh, refreshBadge, toast, t],
	);

	// Expanding the journal is what marks it read; the destructive ack-on-open is
	// gone, so nothing clears the dot until the operator actually looks.
	const onExpandInformational = useCallback(() => {
		api.discovery
			.ackChanges()
			.catch(() => {
				// Badge dot simply stays lit for a later attempt.
			})
			.finally(refreshBadge);
	}, [refreshBadge]);

	useEffect(() => {
		const handler = (e: Event) => {
			const detail = (e as CustomEvent).detail;
			if (detail?.type === "discovery.changes_pending") {
				refreshBadge();
			}
		};
		window.addEventListener("server-event", handler);
		return () => window.removeEventListener("server-event", handler);
	}, [refreshBadge]);

	// A failed fetch must reach the modal: it renders a failure banner and, more
	// importantly, suppresses the "nothing is wrong" empty state.
	const discrepancyLoadError = discrepanciesFailed
		? discrepanciesError instanceof Error
			? discrepanciesError.message
			: String(discrepanciesError)
		: refreshError?.message;

	// Each item names the access it needs: a grant key checked via can()
	// (admins pass everything) or "admin" for admin-only surfaces. Cosmetic
	// gating only; the server enforces per request.
	const allNavigation = [
		{
			name: t("layout.nav.dashboard"),
			href: "/dashboard",
			icon: LayoutDashboard,
			access: "usage",
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
			access: "chat",
		},
		{
			name: t("layout.nav.arena"),
			href: "/arena",
			icon: (mode: string) => (mode === "compare" ? GitCompare : Swords),
			subModes: [
				{ label: t("layout.nav.arena"), value: "competition" as const },
				{ label: t("layout.nav.compare"), value: "compare" as const },
			],
			access: "chat",
		},
		{
			name: t("layout.nav.providers"),
			href: "/providers",
			icon: PlugZap,
			access: "admin",
		},
		{
			name: t("layout.nav.models"),
			href: "/models",
			icon: Bot,
			access: "models",
		},
		{
			name: t("layout.nav.failover"),
			href: "/failover",
			icon: Shuffle,
			access: "admin",
		},
		{
			name: t("layout.nav.virtualKeys"),
			href: "/virtual-keys",
			icon: KeyRound,
			access: "virtual_keys",
		},
		{
			name: t("layout.nav.logs"),
			href: "/logs",
			icon: (mode: string) => (mode === "app" ? FileText : ScrollText),
			// App logs are admin-only server-side, so the sub-mode toggle only
			// shows for admins; grant holders get the requests view alone.
			subModes: isAdmin
				? [
						{ label: t("layout.nav.requests"), value: "request" as const },
						{ label: t("layout.nav.appLogs"), value: "app" as const },
					]
				: undefined,
			access: "logs",
		},
		{
			name: t("layout.nav.users"),
			href: "/users",
			icon: UsersIcon,
			access: "admin",
		},
		{
			name: t("layout.nav.security"),
			href: "/security",
			icon: ShieldCheck,
			// Self-service 2FA exists only for users-row identities; the env-token
			// admin manages TOTP under Settings.
			access: "user_account",
		},
		{
			name: t("layout.nav.audit"),
			href: "/audit",
			icon: HistoryIcon,
			access: "admin",
		},
		{
			name: t("layout.nav.settings"),
			href: "/settings",
			icon: Settings,
			access: "admin",
		},
	];
	const navigation = allNavigation.filter((item) => {
		if (item.access === "admin") return isAdmin;
		if (item.access === "user_account") return Boolean(me?.user_account);
		return can(item.access);
	});

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
			// Best-effort server-side session revoke via the always-mounted
			// endpoint. It revokes whatever session the caller presents (passkey OR
			// TOTP session token) and clears both auth cookies, so it must run for
			// every session type and works whether or not passkeys are configured. A
			// raw admin token with no server session is a harmless no-op. This
			// matters for idle auto-logout: a TOTP-only admin's session must die
			// server-side too.
			await api.auth.logout();
		} catch {
			// Server-side logout failure is non-fatal.
		}
		// The logout call revoked the session and cleared the httpOnly session
		// cookie server-side. Drop the client-visible auth signal so
		// isAuthenticated() flips false, cancel any in-flight queries so they don't
		// race the reload, then reload into the login screen.
		clearAuth();
		queryClient.cancelQueries();
		window.location.reload();
	};

	// Sign out after the configured period of inactivity (0 = never). Reuses the
	// same logout path as the manual button.
	useIdleLogout(handleLogout);

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

														// Quota-pinned circuits wait until the provider's
														// quota window resets, which can be days. Listing
														// them beside ordinary sixty-second cooldowns
														// reads as "back shortly", so they get their own
														// line.
														//
														// The state check is load-bearing: quota_pinned
														// stays set for the whole life of the circuit,
														// and a pinned circuit whose deadline has passed
														// is reported as half-open (ready to probe) with
														// no next_retry_at. Bucketing on the flag alone
														// would keep claiming a provider is waiting on a
														// quota window that has already reset. This
														// mirrors the per-entry rule, where cooldown-over
														// wins over the quota tooltip.
														const stillPinned = (
															p: (typeof unhealthy)[number],
														) => Boolean(p.quota_pinned) && p.state === "open";
														const names = (list: typeof unhealthy) =>
															list
																.map((p) => p.provider_name || p.provider_id)
																.join(", ");
														const pinned = unhealthy.filter(stillPinned);
														const ordinary = unhealthy.filter(
															(p) => !stillPinned(p),
														);

														const lines = [explain];
														if (ordinary.length > 0) {
															lines.push(
																t("layout.nav.failoverBadgeTooltip", {
																	count: ordinary.length,
																	providers: names(ordinary),
																}),
															);
														}
														if (pinned.length > 0) {
															lines.push(
																t("layout.nav.failoverBadgeQuotaTooltip", {
																	count: pinned.length,
																	providers: names(pinned),
																}),
															);
														}
														return lines.join("\n");
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
											(claimCount > 0 ||
												informationalUnseen > 0 ||
												hasPinned) &&
											!showDiscrepancies ? (
											<span className="flex items-center gap-1.5">
												<span>{item.name}</span>
												{/* biome-ignore lint/a11y/useSemanticElements: a real <button> can't nest inside the nav <a>; role+keydown make this span an accessible control */}
												<span
													role="button"
													tabIndex={0}
													data-testid="discovery-status-badge"
													// A number means "something may be broken". A bare dot
													// means "there is news". Price churn moves on nearly
													// every scan, so it must never produce a count.
													data-variant={claimCount > 0 ? "count" : "dot"}
													onClick={(e) => {
														e.preventDefault();
														e.stopPropagation();
														setShowDiscrepancies(true);
													}}
													onKeyDown={(e) => {
														if (e.key === "Enter" || e.key === " ") {
															e.preventDefault();
															e.stopPropagation();
															setShowDiscrepancies(true);
														}
													}}
													className={
														claimCount > 0
															? "inline-flex items-center leading-[1.6] translate-y-[1px] ui-badge ui-badge-accent cursor-pointer"
															: // The dot is only 8x8 CSS px but it is the sole way
																// into the informational journal, so a ::before
																// overlay widens the hit area to 24x24 without
																// changing how it looks or shifting the nav row
																// (a pseudo-element takes no space in the flow, and
																// clicks on it target its originating element).
																"relative inline-block size-2 shrink-0 translate-y-[1px] rounded-full bg-(--accent) cursor-pointer before:absolute before:-inset-2 before:content-['']"
													}
													// Same string on both, by construction: see
													// discoveryBadgeLabel.
													aria-label={discoveryBadgeLabel}
													title={discoveryBadgeLabel}
												>
													{claimCount > 0 ? (
														<>
															<span
																aria-hidden="true"
																className="opacity-70 mr-px"
															>
																!
															</span>
															{claimCount}
														</>
													) : null}
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
							className="sidebar-footer-link text-gray-400 hover:text-white ui-btn hover:bg-white/5"
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
							className="sidebar-footer-link text-gray-400 hover:text-white ui-btn hover:bg-white/5"
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
								// Dev builds sit ahead of the last release far more
								// often than behind it, so they never advertise an
								// update; their useful identity is the build commit.
								if (isDev) {
									return commit
										? t("layout.builtFromCommit", { commit })
										: t("layout.running", { running });
								}
								const base =
									!updateAvailable && latest !== "GitHub"
										? t("layout.runningLatest", { running })
										: updateAvailable
											? t("layout.updateAvailable", { running, latest })
											: t("layout.running", { running });
								// Append the source commit SHA (build stamp, not
								// translatable) so the exact build commit stays visible.
								// The backend already returns a normalized short SHA.
								return commit ? `${base} · ${commit}` : base;
							})()}
							className={`sidebar-footer-link text-gray-400 hover:text-white ui-btn hover:bg-white/5`}
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
						className="w-full sidebar-logout min-w-0"
						aria-label={t("layout.auth.logout")}
						title={t("layout.auth.logout")}
						data-testid="logout-button"
					>
						<PowerOff size={14} strokeWidth={2} />
						<span className="truncate">
							{me?.display_name || me?.username || t("layout.auth.logout")}
						</span>
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

			{showDiscrepancies && (
				<ModelDiscrepancyModal
					providers={snapshot}
					groupClaims={groupClaims}
					informational={informational}
					onClose={() => {
						setShowDiscrepancies(false);
						// Retest failures are per visit. Carrying them into the next open
						// would banner a stale reason next to freshly fetched claims.
						setRetestErrors({});
					}}
					onRetest={(providerId, providerName) => {
						void runRetest(providerId, providerName);
					}}
					onRetestAll={() => {
						void onRetestAll();
					}}
					onCancelRetestAll={onCancelRetestAll}
					onDismiss={(providerId, modelId) => {
						void onDismiss(providerId, modelId);
					}}
					onDismissAll={(providerId, modelIds) => {
						void onDismissAll(providerId, modelIds);
					}}
					onDismissEverything={(batches) => {
						void onDismissEverything(batches);
					}}
					onUnpin={(providerId, modelId) => {
						void onUnpin(providerId, modelId);
					}}
					retestingProviderId={retestingKey}
					isRetesting={isAnyRetesting}
					retestAllProgress={retestAllProgress}
					errors={retestErrors}
					onExpandInformational={onExpandInformational}
					loadError={discrepancyLoadError}
					loading={discrepanciesLoading}
					readOnly={readOnly}
					managed={managed}
				/>
			)}
		</div>
	);
}
