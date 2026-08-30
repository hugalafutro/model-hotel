import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { api } from "../api/client";
import { useIdentity } from "../context/IdentityContext";
import { CollapsibleToggle, useCollapsible } from "./CollapsibleToggle";
import {
	formatCount,
	formatMemoryMB,
	formatThroughput,
	formatUptime,
	unitClass,
} from "./systemStatusFormat";

// SystemStatus is the collapsible health pill at the top of the sidebar:
// API reachability plus the live process / Docker / database gauges.
export function SystemStatus() {
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

	// GET /api/system counts requests_today across every virtual key for an
	// admin and only the caller's own keys for anyone else, so the label has to
	// name the scope it is showing. The role it reads is `me`, the identity as
	// actually resolved, not the context's isAdmin: that one answers "admin"
	// while /api/auth/me is in flight AND whenever it has failed, which is the
	// right fallback for deciding what the nav offers but would leave a
	// non-admin's own count captioned as everyone's for as long as the call
	// keeps failing. Until the role is known the label claims no scope at all.
	const { me } = useIdentity();
	const requestsTodayKeys = me
		? me.role === "admin"
			? {
					label: "layout.stats.requestsTodayAll",
					tooltip: "layout.tooltips.requestsTodayAll",
				}
			: {
					label: "layout.stats.requestsTodayOwn",
					tooltip: "layout.tooltips.requestsTodayOwn",
				}
		: {
				label: "layout.stats.requestsToday",
				tooltip: "layout.tooltips.requestsToday",
			};

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
							title={t(requestsTodayKeys.tooltip)}
						>
							<span>{t(requestsTodayKeys.label)}</span>
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
