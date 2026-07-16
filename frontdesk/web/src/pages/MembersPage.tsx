import { ArrowSquareOutIcon, WarningIcon } from "@phosphor-icons/react";
import {
	type SyntheticEvent,
	useCallback,
	useEffect,
	useRef,
	useState,
} from "react";
import { useTranslation } from "react-i18next";
import { ApiError, api } from "../api/client";
import type { MemberView } from "../api/types";
import { ConfirmModal } from "../components/ConfirmModal";
import { Notice } from "../components/Notice";
import { useToast } from "../context/ToastContext";
import { useMembers } from "../hooks/useMembers";
import { formatRelative, formatTimeOfDay } from "../utils/time";

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
	const [primaryId, setPrimaryId] = useState<string | null>(null);
	// Monotonic sequence counter: refreshPrimary can be called concurrently (on
	// mount and on SSE events), so only the newest in-flight response is applied.
	// Without this, a slower earlier request can land after a newer one and, for
	// example, restore the badge on a primary that was just removed. Mirrors the
	// seqRef pattern useMembers already uses for its own refetch.
	const primarySeqRef = useRef(0);
	// The designated fleet primary (GET /api/fleet/autosync -> auto_sync_primary_id)
	// is the single source of truth for "who is primary": the same value the
	// backend delete-guard and the Fleet Sync wizard use. Null/"" when none is set.
	// Refreshed below on the events that can change it. (This deliberately does NOT
	// read /api/fleet/last-sync, whose primary_id is only a cosmetic "last run"
	// marker and could name a since-removed host.)
	const refreshPrimary = useCallback(() => {
		const seq = ++primarySeqRef.current;
		api
			.getAutoSync()
			.then((cfg) => {
				if (seq === primarySeqRef.current) setPrimaryId(cfg.primary_id || null);
			})
			.catch(() => {});
	}, []);
	// useMembers owns the page's single SSE subscription; piggyback on it to
	// refresh the primary when membership or a sync changes, rather than opening
	// a second stream to /api/sse.
	const { members, loading, error, refetch, lastUpdatedAt } = useMembers(
		useCallback(
			(e) => {
				if (e.type.startsWith("member.") || e.type.startsWith("config.")) {
					refreshPrimary();
				}
			},
			[refreshPrimary],
		),
	);
	const { toast } = useToast();
	const [removing, setRemoving] = useState<MemberView | null>(null);
	useEffect(refreshPrimary, [refreshPrimary]);

	const groupVersion = majorityVersion(members);
	// With a designated primary, version divergence is anchored to it (that is
	// what holds config sync); the majority "odd one out" flag only fills in
	// when no primary is set and there is nothing else to anchor to.
	const primaryVersion = primaryId
		? (members.find((m) => m.id === primaryId)?.status.version ?? null)
		: null;
	// Pin the fleet primary to the top; every other member keeps its order.
	const orderedMembers = primaryId
		? [
				...members.filter((m) => m.id === primaryId),
				...members.filter((m) => m.id !== primaryId),
			]
		: members;

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

	// Only non-primary members are removable (the primary row has no Remove
	// button, and the backend refuses a primary delete with 409). The primary is
	// the config source of truth; it is changed only by re-running the Fleet Sync
	// wizard.
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
					<table className="ui-table ui-table--nowrap">
						<thead>
							<tr>
								<th>{t("members.colName")}</th>
								<th>{t("members.colFrontdesk")}</th>
								<th>{t("members.colTraefik")}</th>
								<th>{t("members.colVersion")}</th>
								<th>{t("members.colVerified")}</th>
								<th>{t("members.colLastSync")}</th>
								<th>{t("members.colState")}</th>
								<th>{t("members.colUpdated")}</th>
								<th />
							</tr>
						</thead>
						<tbody>
							{orderedMembers.map((m) => (
								<MemberRow
									key={m.id}
									member={m}
									groupVersion={primaryId ? null : groupVersion}
									primaryVersion={primaryVersion}
									isPrimary={m.id === primaryId}
									onSetState={setState}
									onRemove={() => setRemoving(m)}
								/>
							))}
						</tbody>
					</table>
				)}
			</div>
			{lastUpdatedAt && !loading && !error && (
				<div
					className="fd-faint"
					style={{
						fontSize: "0.8rem",
						textAlign: "right",
						marginTop: "-1rem",
					}}
					data-testid="members-last-updated"
				>
					{t("members.lastUpdated", {
						when: formatTimeOfDay(lastUpdatedAt),
					})}
				</div>
			)}

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
	primaryVersion,
	isPrimary,
	onSetState,
	onRemove,
}: {
	member: MemberView;
	groupVersion: string | null;
	// The designated primary's version, or null when no primary is set (or its
	// version is unknown). Non-null anchors the "sync held" badge.
	primaryVersion: string | null;
	isPrimary: boolean;
	onSetState: (m: MemberView, state: "active" | "drained") => void;
	onRemove: () => void;
}) {
	const { t } = useTranslation();
	const health = m.status.health;
	const mismatch =
		!!m.status.version && !!groupVersion && m.status.version !== groupVersion;
	// Mirrors the backend gate: config sync (autosync and the wizard) holds this
	// member while its version differs from the primary's.
	const heldForSkew =
		!isPrimary &&
		!!primaryVersion &&
		!!m.status.version &&
		m.status.version !== primaryVersion;

	return (
		<tr className={isPrimary ? "fd-row-primary" : undefined}>
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
						<ArrowSquareOutIcon
							size={13}
							style={{ marginLeft: 4, verticalAlign: "-1px" }}
						/>
					</a>
					{isPrimary && (
						<span
							className="ui-badge ui-badge-info"
							title={t("members.primaryTip")}
							data-testid="primary-badge"
						>
							{t("members.primaryBadge")}
						</span>
					)}
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
								<WarningIcon size={12} weight="bold" />
							</span>
						)}
						{heldForSkew && (
							<span
								className="ui-badge ui-badge-warn"
								data-testid="member-sync-held"
								title={t("members.syncHeldTip")}
							>
								{t("members.syncHeld")}
							</span>
						)}
					</span>
				) : (
					<span className="fd-faint">
						{m.has_token ? t("members.versionUnknown") : t("members.noToken")}
					</span>
				)}
			</td>
			<td data-testid="member-verified">
				{isPrimary ? (
					<span className="fd-faint" title={t("members.verifiedPrimaryTip")}>
						{t("members.verifiedPrimary")}
					</span>
				) : m.status.auto_sync_verified_at ? (
					<span
						className="fd-faint"
						title={t("members.verifiedTip", {
							when: formatRelative(m.status.auto_sync_verified_at),
						})}
					>
						{t("members.verifiedWhen", {
							when: formatRelative(m.status.auto_sync_verified_at),
						})}
					</span>
				) : (
					<span className="fd-faint" title={t("members.verifiedNeverTip")}>
						{t("members.verifiedNever")}
					</span>
				)}
			</td>
			<td>
				{m.last_config_sync_at ? (
					<span
						className="fd-faint"
						title={t("members.lastSyncTip", {
							when: formatRelative(m.last_config_sync_at),
							reason:
								m.last_config_sync_reason ?? t("members.lastSyncReasonUnknown"),
						})}
					>
						{formatRelative(m.last_config_sync_at)}
					</span>
				) : isPrimary ? (
					<span className="fd-faint" title={t("members.lastSyncPrimaryTip")}>
						{t("members.lastSyncPrimary")}
					</span>
				) : (
					<span className="fd-faint">{t("members.lastSyncNever")}</span>
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
			<td className="fd-faint">{formatRelative(m.updated_at)}</td>
			<td>
				<div className="fd-row" style={{ justifyContent: "flex-end" }}>
					{m.state === "active" ? (
						<button
							type="button"
							className="ui-btn ui-btn-sm"
							title={t("members.drainTip")}
							onClick={() => onSetState(m, "drained")}
						>
							{t("members.drain")}
						</button>
					) : (
						<button
							type="button"
							className="ui-btn ui-btn-sm"
							title={t("members.activateTip")}
							onClick={() => onSetState(m, "active")}
						>
							{t("members.activate")}
						</button>
					)}
					{/* The primary is the config source of truth and cannot be removed
					    here; it is changed only by re-running the Fleet Sync wizard. */}
					{!isPrimary && (
						<button
							type="button"
							className="ui-btn ui-btn-sm ui-btn-danger"
							title={t("members.removeTip")}
							onClick={onRemove}
						>
							{t("common.remove")}
						</button>
					)}
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

	const submit = async (e: SyntheticEvent) => {
		e.preventDefault();
		setError("");
		setBusy(true);
		try {
			// An add now succeeds only once the host replied and verified (token
			// accepted, not the fleet primary), so there is no "saved but unconfirmed"
			// warning path here anymore: a failure throws and is shown below.
			const created = await api.createMember(name.trim(), url.trim(), token);
			toast(t("members.added", { name: created.name }), "success");
			setName("");
			setUrl("");
			setToken("");
			onAdded();
		} catch (err) {
			if (
				err instanceof ApiError &&
				(err.status === 400 || err.status === 409)
			) {
				// Prefer the stable machine code the backend now sends; fall back to
				// matching the message text for any response that predates the code.
				const c = err.code;
				if (c === "insecure_url" || /https/i.test(err.message))
					setError(t("members.errHttpsRequired"));
				else if (c === "duplicate" || /already exists/i.test(err.message))
					setError(t("members.errDuplicate"));
				else if (
					c === "already_primary" ||
					/already the fleet primary/i.test(err.message)
				)
					setError(t("members.errAlreadyPrimary"));
				else if (
					c === "already_member" ||
					/already a member/i.test(err.message)
				)
					setError(t("members.errAlreadyMember"));
				else if (c === "unreachable" || /could not reach/i.test(err.message))
					setError(t("members.errUnreachable"));
				else if (c === "identity_unverified")
					setError(t("members.errIdentityUnverified"));
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
					required
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
					disabled={busy || !name.trim() || !url.trim() || !token.trim()}
				>
					{busy ? t("common.adding") : t("common.add")}
				</button>
			</div>
		</form>
	);
}
