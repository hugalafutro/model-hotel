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

function errorRate(tr: MemberTraffic): string {
	if (tr.total_requests === 0) return "0%";
	return `${((tr.total_errors / tr.total_requests) * 100).toFixed(1)}%`;
}

function MemberTrafficCard({ member }: { member: MemberView }) {
	const { t } = useTranslation();
	const [data, setData] = useState<MemberTraffic | null>(null);
	const [loading, setLoading] = useState(true);

	useEffect(() => {
		// Each card is keyed by member id, so this effect runs once per instance;
		// loading starts true, so no synchronous setState is needed here.
		let active = true;
		api
			.memberTraffic(member.id)
			.then((d) => active && setData(d))
			.catch(() => active && setData(null))
			.finally(() => active && setLoading(false));
		return () => {
			active = false;
		};
	}, [member.id]);

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
			) : !data || !data.reachable ? (
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
	const { members, loading } = useMembers();
	const tokenMembers = members.filter((m) => m.has_token);
	const hasTokenless = members.some((m) => !m.has_token);

	return (
		<div className="fd-stack">
			<h1 className="fd-page-title">{t("traffic.title")}</h1>
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
						<MemberTrafficCard key={m.id} member={m} />
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
