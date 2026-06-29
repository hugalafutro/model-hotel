import { ArrowsClockwiseIcon } from "@phosphor-icons/react";
import { useEffect, useState } from "react";
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
import { formatTimeOfDay } from "../utils/time";

function errorRate(tr: MemberTraffic): string {
	if (tr.total_requests === 0) return "0%";
	return `${((tr.total_errors / tr.total_requests) * 100).toFixed(1)}%`;
}

function MemberTrafficCard({
	member,
	reloadKey,
}: {
	member: MemberView;
	reloadKey: number;
}) {
	const { t } = useTranslation();
	const [data, setData] = useState<MemberTraffic | null>(null);
	const [loading, setLoading] = useState(true);
	// When the displayed data was last fetched (client clock). There is no
	// server-side "generated at" on the traffic payload, so this is simply the
	// moment the UI pulled it - which is exactly the freshness the operator wants.
	const [updatedAt, setUpdatedAt] = useState<string | null>(null);

	// biome-ignore lint/correctness/useExhaustiveDependencies: reloadKey is a refetch nonce - it isn't read in the body, its only job is to re-run the fetch when the Refresh button bumps it
	useEffect(() => {
		// Re-runs on member id change and whenever reloadKey is bumped by the
		// page-level Refresh button. Stale data stays on screen during a refetch
		// (we don't reset to the loading state) so the chart doesn't flash; the
		// "updated" timestamp moving is the confirmation the refresh landed.
		let active = true;
		api
			.memberTraffic(member.id)
			.then((d) => {
				if (!active) return;
				setData(d);
				setUpdatedAt(new Date().toISOString());
			})
			.catch(() => active && setData(null))
			.finally(() => active && setLoading(false));
		return () => {
			active = false;
		};
	}, [member.id, reloadKey]);

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
				<div className="fd-empty fd-faint">{t("traffic.unreachable")}</div>
			) : data.total_requests === 0 ? (
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
					<div style={{ width: "100%", height: 140 }}>
						<ResponsiveContainer>
							<AreaChart
								data={data.points}
								margin={{ top: 4, right: 8, bottom: 0, left: -20 }}
							>
								<CartesianGrid strokeDasharray="3 3" stroke="var(--border)" />
								<XAxis dataKey="bucket" hide />
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
								/>
								<Area
									type="monotone"
									dataKey="requests"
									name={t("traffic.requests")}
									stroke="var(--accent)"
									fill="var(--accent)"
									fillOpacity={0.18}
								/>
								<Area
									type="monotone"
									dataKey="errors"
									name={t("traffic.errors")}
									stroke="var(--danger)"
									fill="var(--danger)"
									fillOpacity={0.18}
								/>
							</AreaChart>
						</ResponsiveContainer>
					</div>
				</>
			)}

			{updatedAt && (
				<div
					className="fd-faint"
					style={{ fontSize: "0.72rem", marginTop: "0.5rem" }}
					data-testid="traffic-updated"
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

	// Bumped by the Refresh button; threaded into every card's fetch effect so a
	// single click re-pulls all graphs. The members list is refetched too, in
	// case membership changed since the page mounted (FD has no URL routing, so a
	// browser reload drops back to the Members tab - this is the in-page way to
	// pull fresh numbers without leaving Traffic).
	const [reloadKey, setReloadKey] = useState(0);
	const refresh = () => {
		refetch();
		setReloadKey((k) => k + 1);
	};

	return (
		<div className="fd-stack">
			<div className="fd-spread">
				<h1 className="fd-page-title">{t("traffic.title")}</h1>
				<button
					type="button"
					className="ui-btn ui-btn-ghost ui-btn-sm"
					onClick={refresh}
					disabled={loading}
					data-testid="traffic-refresh"
				>
					<ArrowsClockwiseIcon size={14} />
					{t("traffic.refresh")}
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
				<div className="fd-stack">
					{tokenMembers.map((m) => (
						<MemberTrafficCard key={m.id} member={m} reloadKey={reloadKey} />
					))}
					{hasTokenless && (
						<p className="fd-faint" style={{ fontSize: "0.82rem" }}>
							{t("traffic.noTokenMembers")}
						</p>
					)}
				</div>
			)}
		</div>
	);
}
