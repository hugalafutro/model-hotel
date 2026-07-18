import { PauseIcon, PlayIcon } from "@phosphor-icons/react";
import { useCallback, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import {
	Area,
	AreaChart,
	CartesianGrid,
	ResponsiveContainer,
	Tooltip,
	XAxis,
	YAxis,
} from "recharts";
import { api } from "../api/client";
import type { MemberTraffic, MemberView } from "../api/types";
import { useMembers } from "../hooks/useMembers";
import { formatHourTick, formatTimeOfDay } from "../utils/time";

// The Traffic page auto-refreshes every graph on this interval while it is not
// paused. It is deliberately short (metrics are cheap routing/metering counts)
// so an operator watching a rollout sees load shift without touching anything.
const AUTO_REFRESH_MS = 5000;

function errorRate(tr: MemberTraffic): string {
	if (tr.total_requests === 0) return "0%";
	return `${((tr.total_errors / tr.total_requests) * 100).toFixed(1)}%`;
}

function MemberTrafficCard({
	member,
	reloadKey,
	onUpdated,
	showLocalUpdated,
}: {
	member: MemberView;
	reloadKey: number;
	// Reports this card's fetch time (or null on a failed read) up to the page so
	// it can collapse identical stamps into a single page-level one.
	onUpdated: (id: string, iso: string | null) => void;
	// True only when another card's read failed, so the page can't show a single
	// trustworthy shared stamp and each card carries its own instead (this card's
	// own stamp is still hidden when its data is null).
	showLocalUpdated: boolean;
}) {
	const { t } = useTranslation();
	const [data, setData] = useState<MemberTraffic | null>(null);
	const [loading, setLoading] = useState(true);
	// When the displayed data was last fetched (client clock). There is no
	// server-side "generated at" on the traffic payload, so this is simply the
	// moment the UI pulled it - which is exactly the freshness the operator wants.
	const [updatedAt, setUpdatedAt] = useState<string | null>(null);
	// Per-card refetch nonce. The page-level auto-refresh reloads every card via
	// reloadKey; this lets a single unreachable member be retried on its own,
	// right where the "could not read metrics" message appears.
	const [localReload, setLocalReload] = useState(0);

	// biome-ignore lint/correctness/useExhaustiveDependencies: reloadKey and localReload are refetch nonces - they aren't read in the body, their only job is to re-run the fetch when the page auto-refresh or the retry button bumps them
	useEffect(() => {
		// Re-runs on member id change and whenever reloadKey is bumped by the
		// page-level auto-refresh. Stale data stays on screen during a refetch
		// (we don't reset to the loading state) so the chart doesn't flash; the
		// "updated" timestamp moving is the confirmation the refresh landed.
		let active = true;
		api
			.memberTraffic(member.id)
			.then((d) => {
				if (!active) return;
				setData(d);
				// Only a reachable snapshot counts as a fresh stamp. An unreachable
				// member replies 200 with reachable:false (the backend could not read
				// its stats), which is not a refresh of any shown data - stamping it
				// would let one failed card ride along under a shared "Updated" time.
				if (d.reachable) {
					const iso = new Date().toISOString();
					setUpdatedAt(iso);
					onUpdated(member.id, iso);
				} else {
					setUpdatedAt(null);
					onUpdated(member.id, null);
				}
			})
			.catch(() => {
				// Clear the timestamp too: a failed refresh must not leave an old
				// "Updated ..." stamp next to the "could not read metrics" state,
				// which would make a failed read look like fresh status.
				if (!active) return;
				setData(null);
				setUpdatedAt(null);
				onUpdated(member.id, null);
			})
			.finally(() => active && setLoading(false));
		return () => {
			active = false;
		};
	}, [member.id, reloadKey, localReload, onUpdated]);

	// One hour-aligned tick per bucket that lands on the hour, so the X axis reads
	// as an hourly clock rather than one label per 5-minute bucket. Buckets whose
	// timestamp can't be parsed contribute no tick (the axis falls back to
	// recharts' defaults) rather than showing raw strings.
	const hourTicks = (data?.points ?? [])
		.filter((p) => {
			const d = new Date(p.bucket);
			return !Number.isNaN(d.getTime()) && d.getMinutes() === 0;
		})
		.map((p) => p.bucket);

	return (
		<div className="ui-card ui-card-pad">
			<div className="fd-spread" style={{ marginBottom: "0.6rem" }}>
				<strong>{member.name}</strong>
				{data?.reachable && (
					<span className="fd-faint" style={{ fontSize: "0.8rem" }}>
						{t("traffic.window", { n: data.window_minutes })}
					</span>
				)}
			</div>

			{loading ? (
				<div className="fd-empty">{t("common.loading")}</div>
			) : !data?.reachable ? (
				<div
					className="fd-empty fd-faint"
					style={{
						display: "flex",
						flexDirection: "column",
						alignItems: "center",
						gap: "0.75rem",
					}}
				>
					<span>{t("traffic.unreachable")}</span>
					<button
						type="button"
						className="ui-btn ui-btn-ghost ui-btn-sm"
						onClick={() => setLocalReload((n) => n + 1)}
						data-testid="traffic-member-refresh"
					>
						{t("traffic.refresh")}
					</button>
				</div>
			) : data.points.length === 0 ? (
				// Only a genuinely empty series (no buckets to plot) gets the empty
				// state. A reachable member with all-zero buckets still charts, so the
				// flat green requests baseline shows instead of a bare "No data".
				<div className="fd-empty fd-faint">{t("traffic.noData")}</div>
			) : (
				<>
					<div
						className="fd-row"
						style={{ gap: "1.5rem", marginBottom: "0.8rem" }}
					>
						<Metric
							label={t("traffic.requests")}
							value={String(data.total_requests)}
						/>
						<Metric
							label={t("traffic.errors")}
							value={String(data.total_errors)}
						/>
						<Metric label={t("traffic.errorRate")} value={errorRate(data)} />
					</div>
					<div style={{ width: "100%", height: 160 }}>
						<ResponsiveContainer>
							<AreaChart
								data={data.points}
								margin={{ top: 4, right: 8, bottom: 0, left: -20 }}
							>
								<CartesianGrid strokeDasharray="3 3" stroke="var(--border)" />
								<XAxis
									dataKey="bucket"
									ticks={hourTicks.length ? hourTicks : undefined}
									tickFormatter={formatHourTick}
									tick={{ fontSize: 10, fill: "var(--text-faint)" }}
									tickLine={false}
									axisLine={{ stroke: "var(--border)" }}
									minTickGap={4}
								/>
								<YAxis
									tick={{ fontSize: 11, fill: "var(--text-faint)" }}
									allowDecimals={false}
								/>
								<Tooltip
									contentStyle={{
										background: "var(--surface-2)",
										border: "1px solid var(--border-strong)",
										borderRadius: 8,
										fontSize: 12,
									}}
									labelStyle={{ color: "var(--text-muted)" }}
									labelFormatter={(label) => formatHourTick(String(label))}
								/>
								{/* Errors first, requests second: recharts paints later series on
								    top, so the green requests line always sits above the red
								    errors line. With no traffic both are flat at zero and the
								    green line is what shows, not a bare red baseline. */}
								<Area
									type="monotone"
									dataKey="errors"
									name={t("traffic.errors")}
									stroke="var(--danger)"
									fill="var(--danger)"
									fillOpacity={0.18}
								/>
								<Area
									type="monotone"
									dataKey="requests"
									name={t("traffic.requests")}
									stroke="var(--ok)"
									fill="var(--ok)"
									fillOpacity={0.18}
								/>
							</AreaChart>
						</ResponsiveContainer>
					</div>
				</>
			)}

			{showLocalUpdated && updatedAt && (
				<div
					className="fd-faint"
					style={{ fontSize: "0.72rem", marginTop: "0.35rem" }}
					data-testid="traffic-updated-local"
				>
					{t("traffic.updated", { time: formatTimeOfDay(updatedAt) })}
				</div>
			)}
		</div>
	);
}

function Metric({ label, value }: { label: string; value: string }) {
	return (
		<div>
			<div
				className="fd-faint"
				style={{
					fontSize: "0.72rem",
					textTransform: "uppercase",
					letterSpacing: "0.04em",
				}}
			>
				{label}
			</div>
			<div style={{ fontSize: "1.3rem", fontWeight: 600 }}>{value}</div>
		</div>
	);
}

export function TrafficPage() {
	const { t } = useTranslation();
	const { members, loading, refetch } = useMembers();
	const tokenMembers = members.filter((m) => m.has_token);
	const hasTokenless = members.some((m) => !m.has_token);

	// Bumped by the auto-refresh timer; threaded into every card's fetch effect so
	// one tick re-pulls all graphs. The members list is refetched too, in case
	// membership changed since the page mounted (FD has no URL routing, so a
	// browser reload drops back to the Members tab - this is the in-page way to
	// pull fresh numbers without leaving Traffic).
	const [reloadKey, setReloadKey] = useState(0);
	const refresh = useCallback(() => {
		refetch();
		setReloadKey((k) => k + 1);
	}, [refetch]);

	// Auto-refresh is on by default; the header button pauses it. Pausing is
	// page-local state, so leaving the tab or reloading the page restarts it - the
	// intended "on next visit it is live again" behaviour.
	const [paused, setPaused] = useState(false);
	useEffect(() => {
		if (paused) return;
		const id = setInterval(refresh, AUTO_REFRESH_MS);
		return () => clearInterval(id);
	}, [paused, refresh]);

	// Each card reports its last fetch time here (null when its read failed, or
	// still absent before its first load). The page shows one shared "updated"
	// line whenever every token card holds a fresh reachable time, using the most
	// recent of them - mirroring the Members tab. Only a genuinely failed card
	// (an explicit null) splits the page into per-card stamps; a card that is
	// merely mid-refetch keeps its previous time, so a refresh tick where one card
	// commits its new time a beat before the other does NOT flip the layout to
	// per-card and back (which showed up as a ~15px jump under the second graph).
	const [updatedAts, setUpdatedAts] = useState<Record<string, string | null>>(
		{},
	);
	const onUpdated = useCallback((id: string, iso: string | null) => {
		setUpdatedAts((prev) => ({ ...prev, [id]: iso }));
	}, []);
	const cardStamps = tokenMembers.map((m) => updatedAts[m.id]);
	const anyFailed = cardStamps.some((v) => v === null);
	const anyLoading = cardStamps.some((v) => v === undefined);
	// Latest reported ISO wins for the shared line; ISO strings sort chronologically.
	const latestIso = cardStamps.reduce<string | null>(
		(latest, v) => (v && (!latest || v > latest) ? v : latest),
		null,
	);
	const sharedTime =
		!anyFailed && !anyLoading && latestIso ? formatTimeOfDay(latestIso) : null;

	return (
		<div className="fd-stack">
			<div className="fd-spread">
				<h1 className="fd-page-title">{t("traffic.title")}</h1>
				<button
					type="button"
					className="ui-btn ui-btn-ghost ui-btn-sm"
					onClick={() => setPaused((p) => !p)}
					aria-pressed={paused}
					data-testid="traffic-pause"
				>
					{paused ? <PlayIcon size={14} /> : <PauseIcon size={14} />}
					{paused ? t("traffic.resume") : t("traffic.pause")}
				</button>
			</div>
			<p className="fd-muted" style={{ marginTop: "-0.5rem" }}>
				{t("traffic.intro")}
			</p>

			{loading ? (
				<div className="ui-card">
					<div className="fd-empty">{t("common.loading")}</div>
				</div>
			) : tokenMembers.length === 0 ? (
				<div className="ui-card">
					<div className="fd-empty">{t("traffic.empty")}</div>
				</div>
			) : (
				<>
					<div className="fd-stack">
						{tokenMembers.map((m) => (
							<MemberTrafficCard
								key={m.id}
								member={m}
								reloadKey={reloadKey}
								onUpdated={onUpdated}
								showLocalUpdated={anyFailed}
							/>
						))}
						{hasTokenless && (
							<p className="fd-faint" style={{ fontSize: "0.82rem" }}>
								{t("traffic.noTokenMembers")}
							</p>
						)}
					</div>
					{sharedTime && (
						<div
							className="fd-faint"
							style={{
								fontSize: "0.8rem",
								textAlign: "right",
								marginTop: "-0.5rem",
							}}
							data-testid="traffic-updated"
						>
							{t("traffic.updated", { time: sharedTime })}
						</div>
					)}
				</>
			)}
		</div>
	);
}
