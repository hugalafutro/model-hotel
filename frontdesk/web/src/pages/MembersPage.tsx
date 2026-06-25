import { ArrowSquareOut, Warning } from "@phosphor-icons/react";
import { type FormEvent, useState } from "react";
import { useTranslation } from "react-i18next";
import { ApiError, api } from "../api/client";
import type { MemberView } from "../api/types";
import { ConfirmModal } from "../components/ConfirmModal";
import { Notice } from "../components/Notice";
import { useToast } from "../context/ToastContext";
import { useMembers } from "../hooks/useMembers";
import { formatRelative } from "../utils/time";

// majorityVersion returns the most common non-empty version across members, used
// to flag the odd one(s) out only when the group actually disagrees.
function majorityVersion(members: MemberView[]): string | null {
	const counts = new Map<string, number>();
	for (const m of members) {
		const v = m.status.version;
		if (v) counts.set(v, (counts.get(v) ?? 0) + 1);
	}
	if (counts.size <= 1) return null;
	let best: string | null = null;
	let bestN = 0;
	for (const [v, n] of counts) {
		if (n > bestN) {
			best = v;
			bestN = n;
		}
	}
	return best;
}

export function MembersPage() {
	const { t } = useTranslation();
	const { members, loading, error, refetch } = useMembers();
	const { toast } = useToast();
	const [removing, setRemoving] = useState<MemberView | null>(null);

	const groupVersion = majorityVersion(members);

	const setState = async (m: MemberView, state: "active" | "drained") => {
		try {
			await api.setMemberState(m.id, state);
			toast(
				t(state === "drained" ? "members.drained" : "members.activated", {
					name: m.name,
				}),
				"info",
			);
			refetch();
		} catch {
			toast(t("errors.generic"), "error");
		}
	};

	const confirmRemove = async () => {
		if (!removing) return;
		const m = removing;
		setRemoving(null);
		try {
			await api.deleteMember(m.id);
			toast(t("members.removed", { name: m.name }), "info");
			refetch();
		} catch {
			toast(t("errors.generic"), "error");
		}
	};

	return (
		<div className="fd-stack">
			<h1 className="fd-page-title">{t("members.title")}</h1>

			<div className="ui-card">
				{loading ? (
					<div className="fd-empty">{t("common.loading")}</div>
				) : error ? (
					<div className="fd-empty fd-error-text">{t("errors.network")}</div>
				) : members.length === 0 ? (
					<div className="fd-empty">{t("members.empty")}</div>
				) : (
					<table className="ui-table">
						<thead>
							<tr>
								<th>{t("members.colName")}</th>
								<th>{t("members.colFrontdesk")}</th>
								<th>{t("members.colTraefik")}</th>
								<th>{t("members.colVersion")}</th>
								<th>{t("members.colState")}</th>
								<th />
							</tr>
						</thead>
						<tbody>
							{members.map((m) => (
								<MemberRow
									key={m.id}
									member={m}
									groupVersion={groupVersion}
									onSetState={setState}
									onRemove={() => setRemoving(m)}
								/>
							))}
						</tbody>
					</table>
				)}
			</div>

			<AddMemberForm
				firstMember={!loading && members.length === 0}
				onAdded={refetch}
			/>

			{removing && (
				<ConfirmModal
					title={t("members.removeTitle", { name: removing.name })}
					confirmLabel={t("common.remove")}
					onConfirm={confirmRemove}
					onClose={() => setRemoving(null)}
				>
					<p className="fd-muted">
						{t("members.removeBody", { name: removing.name })}
					</p>
				</ConfirmModal>
			)}
		</div>
	);
}

function MemberRow({
	member: m,
	groupVersion,
	onSetState,
	onRemove,
}: {
	member: MemberView;
	groupVersion: string | null;
	onSetState: (m: MemberView, state: "active" | "drained") => void;
	onRemove: () => void;
}) {
	const { t } = useTranslation();
	const health = m.status.health;
	const mismatch =
		!!m.status.version && !!groupVersion && m.status.version !== groupVersion;

	return (
		<tr>
			<td>
				<div className="fd-row">
					<a
						className="fd-link"
						href={m.url}
						target="_blank"
						rel="noreferrer"
						title={t("members.openDashboard")}
					>
						{m.name}
						<ArrowSquareOut
							size={13}
							style={{ marginLeft: 4, verticalAlign: "-1px" }}
						/>
					</a>
				</div>
				<div className="fd-faint fd-mono">{m.url}</div>
			</td>
			<td>
				{!health.known ? (
					<span className="ui-badge">{t("members.healthUnknown")}</span>
				) : health.healthy ? (
					<span className="ui-badge ui-badge-ok">
						<span className="ui-badge-dot" />
						{t("members.healthUp")} ·{" "}
						{t("members.latencyMs", { ms: health.latency_ms })}
					</span>
				) : (
					<span className="ui-badge ui-badge-danger">
						<span className="ui-badge-dot" />
						{t("members.healthDown")}
					</span>
				)}
			</td>
			<td>
				{m.status.traefik_status === "UP" ? (
					<span className="ui-badge ui-badge-ok">{t("members.traefikUp")}</span>
				) : m.status.traefik_status === "DOWN" ? (
					<span className="ui-badge ui-badge-danger">
						{t("members.traefikDown")}
					</span>
				) : (
					<span className="fd-faint">{t("members.traefikUnknown")}</span>
				)}
			</td>
			<td>
				{m.status.version ? (
					<span className="fd-row">
						<span className="fd-mono">{m.status.version}</span>
						{mismatch && (
							<span
								className="ui-badge ui-badge-warn"
								title={t("members.versionMismatch")}
							>
								<Warning size={12} weight="bold" />
							</span>
						)}
					</span>
				) : (
					<span className="fd-faint">
						{m.has_token ? "—" : t("members.noToken")}
					</span>
				)}
			</td>
			<td>
				{m.state === "active" ? (
					<span className="ui-badge ui-badge-info">
						{t("members.stateActive")}
					</span>
				) : (
					<span className="ui-badge ui-badge-warn">
						{t("members.stateDrained")}
					</span>
				)}
			</td>
			<td>
				<div className="fd-row" style={{ justifyContent: "flex-end" }}>
					{m.state === "active" ? (
						<button
							type="button"
							className="ui-btn ui-btn-sm"
							onClick={() => onSetState(m, "drained")}
						>
							{t("members.drain")}
						</button>
					) : (
						<button
							type="button"
							className="ui-btn ui-btn-sm"
							onClick={() => onSetState(m, "active")}
						>
							{t("members.activate")}
						</button>
					)}
					<button
						type="button"
						className="ui-btn ui-btn-sm ui-btn-danger"
						onClick={onRemove}
					>
						{t("common.remove")}
					</button>
				</div>
				<div
					className="fd-faint"
					style={{ fontSize: "0.72rem", textAlign: "right", marginTop: 2 }}
				>
					{formatRelative(m.updated_at)}
				</div>
			</td>
		</tr>
	);
}

function AddMemberForm({
	firstMember,
	onAdded,
}: {
	firstMember: boolean;
	onAdded: () => void;
}) {
	const { t } = useTranslation();
	const { toast } = useToast();
	const [name, setName] = useState("");
	const [url, setUrl] = useState("");
	const [token, setToken] = useState("");
	const [busy, setBusy] = useState(false);
	const [error, setError] = useState("");

	const submit = async (e: FormEvent) => {
		e.preventDefault();
		setError("");
		setBusy(true);
		try {
			const created = await api.createMember(name.trim(), url.trim(), token);
			toast(t("members.added", { name: created.name }), "success");
			setName("");
			setUrl("");
			setToken("");
			onAdded();
		} catch (err) {
			if (err instanceof ApiError && err.status === 400) {
				if (/https/i.test(err.message)) setError(t("members.errHttpsRequired"));
				else if (/already exists/i.test(err.message))
					setError(t("members.errDuplicate"));
				else setError(err.message);
			} else {
				setError(t("errors.generic"));
			}
		} finally {
			setBusy(false);
		}
	};

	return (
		<form className="ui-card ui-card-pad" onSubmit={submit}>
			<h2 style={{ fontSize: "1rem", marginBottom: "0.8rem" }}>
				{t("members.addTitle")}
			</h2>
			{firstMember && (
				<Notice variant="info" style={{ marginBottom: "0.8rem" }}>
					{t("members.firstMemberPrimary")}
				</Notice>
			)}
			<div
				className="fd-row"
				style={{ alignItems: "flex-start", gap: "0.8rem", flexWrap: "wrap" }}
			>
				<div
					className="ui-field"
					style={{ flex: "1 1 160px", marginBottom: 0 }}
				>
					<label className="ui-label" htmlFor="add-name">
						{t("members.nameLabel")}
					</label>
					<input
						id="add-name"
						className="ui-input"
						value={name}
						onChange={(e) => setName(e.target.value)}
						placeholder={t("members.namePlaceholder")}
						required
					/>
				</div>
				<div
					className="ui-field"
					style={{ flex: "2 1 240px", marginBottom: 0 }}
				>
					<label className="ui-label" htmlFor="add-url">
						{t("members.urlLabel")}
					</label>
					<input
						id="add-url"
						className="ui-input"
						value={url}
						onChange={(e) => setUrl(e.target.value)}
						placeholder={t("members.urlPlaceholder")}
						required
					/>
				</div>
			</div>
			<div
				className="ui-field"
				style={{ marginTop: "0.8rem", marginBottom: 0 }}
			>
				<label className="ui-label" htmlFor="add-token">
					{t("members.tokenLabel")}
				</label>
				<input
					id="add-token"
					className="ui-input"
					type="password"
					autoComplete="off"
					value={token}
					onChange={(e) => setToken(e.target.value)}
					placeholder={t("members.tokenPlaceholder")}
				/>
				<div
					className="fd-faint"
					style={{ fontSize: "0.78rem", marginTop: "0.3rem" }}
				>
					{t("members.tokenHint")}
				</div>
			</div>
			{error && (
				<div
					className="fd-error-text"
					role="alert"
					style={{ marginTop: "0.7rem" }}
				>
					{error}
				</div>
			)}
			<div style={{ marginTop: "0.9rem" }}>
				<button
					type="submit"
					className="ui-btn ui-btn-primary"
					disabled={busy || !name.trim() || !url.trim()}
				>
					{busy ? t("common.adding") : t("common.add")}
				</button>
			</div>
		</form>
	);
}
