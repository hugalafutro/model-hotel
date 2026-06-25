import { useCallback, useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { api } from "../api/client";
import type { FdEvent } from "../api/types";
import { useMembers } from "../hooks/useMembers";
import { useSSE } from "../hooks/useSSE";
import { formatAbsolute } from "../utils/time";

const PAGE_SIZE = 25;

// Event types the control plane emits, offered as exact-match filter options
// (the backend filters type by equality). Kept in sync with poller.go/server.go.
const EVENT_TYPES = [
	"member.added",
	"member.removed",
	"member.state_changed",
	"health.up",
	"health.down",
	"version.fetch_failed",
	"version.fetch_recovered",
	"traefik.stale",
];

const SEVERITIES = ["info", "success", "warning", "error"] as const;

// Relative "since" presets (ms), 0 = no lower bound.
const RANGES: { key: string; ms: number }[] = [
	{ key: "all", ms: 0 },
	{ key: "1h", ms: 3_600_000 },
	{ key: "24h", ms: 86_400_000 },
	{ key: "7d", ms: 604_800_000 },
	{ key: "30d", ms: 2_592_000_000 },
];

function severityBadgeClass(sev: string): string {
	switch (sev) {
		case "success":
			return "ui-badge ui-badge-ok";
		case "warning":
			return "ui-badge ui-badge-warn";
		case "error":
			return "ui-badge ui-badge-danger";
		default:
			return "ui-badge ui-badge-info";
	}
}

export function EventsPage() {
	const { t } = useTranslation();
	const { members } = useMembers();
	const [memberId, setMemberId] = useState("");
	const [type, setType] = useState("");
	const [severity, setSeverity] = useState("");
	const [range, setRange] = useState("all");
	const [page, setPage] = useState(0);
	const [events, setEvents] = useState<FdEvent[]>([]);
	const [total, setTotal] = useState(0);
	const [loading, setLoading] = useState(true);
	const [error, setError] = useState(false);

	const memberName = useMemo(() => {
		const map = new Map(members.map((m) => [m.id, m.name]));
		return (id?: string) => (id ? (map.get(id) ?? id) : "");
	}, [members]);

	const buildParams = useCallback(() => {
		const p = new URLSearchParams();
		if (memberId) p.set("member_id", memberId);
		if (type) p.set("type", type);
		if (severity) p.set("severity", severity);
		const rangeMs = RANGES.find((r) => r.key === range)?.ms ?? 0;
		if (rangeMs > 0)
			p.set("since", new Date(Date.now() - rangeMs).toISOString());
		p.set("limit", String(PAGE_SIZE));
		p.set("offset", String(page * PAGE_SIZE));
		return p;
	}, [memberId, type, severity, range, page]);

	const refetch = useCallback(() => {
		api
			.listEvents(buildParams())
			.then((res) => {
				setEvents(res.events ?? []);
				setTotal(res.total);
				setError(false);
			})
			.catch(() => setError(true))
			.finally(() => setLoading(false));
	}, [buildParams]);

	useEffect(refetch, [refetch]);

	// Changing a filter resets to the first page (done in the handlers, not an
	// effect, to avoid a cascading-render setState-in-effect).
	const onFilter =
		<T,>(setter: (v: T) => void) =>
		(v: T) => {
			setter(v);
			setPage(0);
		};

	// Live updates only while viewing the first, unfiltered-by-page top of the log.
	useSSE(
		useCallback(() => {
			if (page === 0) refetch();
		}, [page, refetch]),
		true,
	);

	const clearFilters = () => {
		setMemberId("");
		setType("");
		setSeverity("");
		setRange("all");
	};

	const from = total === 0 ? 0 : page * PAGE_SIZE + 1;
	const to = Math.min(total, (page + 1) * PAGE_SIZE);
	const hasFilters = memberId || type || severity || range !== "all";

	return (
		<div className="fd-stack">
			<h1 className="fd-page-title">{t("events.title")}</h1>

			<div className="ui-card ui-card-pad">
				<div className="fd-row" style={{ flexWrap: "wrap", gap: "0.6rem" }}>
					<select
						className="ui-select"
						style={{ width: "auto" }}
						value={memberId}
						onChange={(e) => onFilter(setMemberId)(e.target.value)}
						aria-label={t("events.filterMember")}
					>
						<option value="">{t("events.allMembers")}</option>
						{members.map((m) => (
							<option key={m.id} value={m.id}>
								{m.name}
							</option>
						))}
					</select>
					<select
						className="ui-select"
						style={{ width: "auto" }}
						value={type}
						onChange={(e) => onFilter(setType)(e.target.value)}
						aria-label={t("events.filterType")}
					>
						<option value="">{t("events.allTypes")}</option>
						{EVENT_TYPES.map((ty) => (
							<option key={ty} value={ty}>
								{ty}
							</option>
						))}
					</select>
					<select
						className="ui-select"
						style={{ width: "auto" }}
						value={severity}
						onChange={(e) => onFilter(setSeverity)(e.target.value)}
						aria-label={t("events.filterSeverity")}
					>
						<option value="">{t("events.allSeverities")}</option>
						{SEVERITIES.map((s) => (
							<option key={s} value={s}>
								{t(`events.sev${s.charAt(0).toUpperCase()}${s.slice(1)}`)}
							</option>
						))}
					</select>
					<select
						className="ui-select"
						style={{ width: "auto" }}
						value={range}
						onChange={(e) => onFilter(setRange)(e.target.value)}
						aria-label={t("events.filterTime")}
					>
						{RANGES.map((r) => (
							<option key={r.key} value={r.key}>
								{r.key === "all" ? t("events.rangeAll") : r.key}
							</option>
						))}
					</select>
					{hasFilters && (
						<button
							type="button"
							className="ui-btn ui-btn-sm ui-btn-ghost"
							onClick={clearFilters}
						>
							{t("events.clearFilters")}
						</button>
					)}
				</div>
			</div>

			<div className="ui-card">
				{loading ? (
					<div className="fd-empty">{t("common.loading")}</div>
				) : error ? (
					<div className="fd-empty fd-error-text">{t("errors.network")}</div>
				) : events.length === 0 ? (
					<div className="fd-empty">{t("events.empty")}</div>
				) : (
					<table className="ui-table">
						<thead>
							<tr>
								<th>{t("events.colTime")}</th>
								<th>{t("events.colSeverity")}</th>
								<th>{t("events.colSource")}</th>
								<th>{t("events.colMessage")}</th>
							</tr>
						</thead>
						<tbody>
							{events.map((e) => (
								<tr key={e.id}>
									<td className="fd-faint" style={{ whiteSpace: "nowrap" }}>
										{formatAbsolute(e.created_at)}
									</td>
									<td>
										<span className={severityBadgeClass(e.severity)}>
											{t(
												`events.sev${e.severity.charAt(0).toUpperCase()}${e.severity.slice(1)}`,
												{ defaultValue: e.severity },
											)}
										</span>
									</td>
									<td className="fd-mono fd-faint">{e.source}</td>
									<td>
										{e.message}
										{e.member_id && (
											<span className="fd-faint">
												{" "}
												· {memberName(e.member_id)}
											</span>
										)}
									</td>
								</tr>
							))}
						</tbody>
					</table>
				)}
			</div>

			{total > 0 && (
				<div className="fd-spread">
					<span className="fd-faint">
						{t("events.pageOf", { from, to, total })}
					</span>
					<div className="fd-row">
						<button
							type="button"
							className="ui-btn ui-btn-sm"
							disabled={page === 0}
							onClick={() => setPage((p) => Math.max(0, p - 1))}
						>
							{t("events.prev")}
						</button>
						<button
							type="button"
							className="ui-btn ui-btn-sm"
							disabled={to >= total}
							onClick={() => setPage((p) => p + 1)}
						>
							{t("events.next")}
						</button>
					</div>
				</div>
			)}
		</div>
	);
}
