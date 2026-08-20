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
import type { AutoSyncConfig, MemberView } from "../api/types";
import { ConfirmModal } from "../components/ConfirmModal";
import { Notice } from "../components/Notice";
import { useToast } from "../context/ToastContext";
import { useMembers } from "../hooks/useMembers";
import type { Build } from "../utils/build";
import {
	buildLabel,
	buildsDiffer,
	buildTitle,
	stampedCommit,
} from "../utils/build";
import { formatRelative, formatTimeOfDay } from "../utils/time";

// memberBuild reads a member's build identity off its polled status. Front Desk
// clears both halves together when a read fails, so an empty version means "not
// confirmed" rather than "no version".
function memberBuild(m: MemberView): Build {
	return { version: m.status.version ?? "", commit: m.status.commit ?? "" };
}

// buildKey identifies a build for counting. The commit joins the key only when
// it names a real build, so members that cannot report one still group by their
// version instead of each landing in a bucket of its own.
function buildKey(b: Build): string {
	return stampedCommit(b.commit) ? `${b.version}@${b.commit}` : b.version;
}

// majorityBuild returns the most common known build across members, used to flag
// the odd one(s) out only when the group actually disagrees. Keyed on the build
// rather than the version: on a fleet of "dev" images every version matches, so
// a version-keyed count can never see a disagreement.
function majorityBuild(members: MemberView[]): Build | null {
	const counts = new Map<string, { build: Build; n: number }>();
	for (const m of members) {
		const b = memberBuild(m);
		if (!b.version) continue;
		const k = buildKey(b);
		const cur = counts.get(k);
		if (cur) cur.n += 1;
		else counts.set(k, { build: b, n: 1 });
	}
	if (counts.size <= 1) return null;
	let best: Build | null = null;
	let bestN = 0;
	for (const { build, n } of counts.values()) {
		if (n > bestN) {
			best = build;
			bestN = n;
		}
	}
	return best;
}

// fleetStateBadgeClass maps a fleet state to its badge variant, mirroring
// severityBadgeClass in EventsPage.tsx. Anything unrecognised (or a future
// state) degrades to the neutral info badge rather than rendering unstyled.
function fleetStateBadgeClass(state: string): string {
	switch (state) {
		case "ok":
			return "ui-badge ui-badge-ok";
		case "degraded":
			return "ui-badge ui-badge-warn";
		case "faulty":
			return "ui-badge ui-badge-danger";
		default:
			return "ui-badge ui-badge-info";
	}
}

export function MembersPage() {
	const { t } = useTranslation();
	// The full auto-sync status (GET /api/fleet/autosync) drives both "who is
	// primary" and the fleet-state badge, so the whole payload is held rather than
	// just primary_id. Null until the first read applies.
	const [autoSync, setAutoSync] = useState<AutoSyncConfig | null>(null);
	// Monotonic sequence counter: refreshPrimary can be called concurrently (on
	// mount and on SSE events), so only the newest in-flight response is applied.
	// Without this, a slower earlier request can land after a newer one and, for
	// example, restore the badge on a primary that was just removed. Mirrors the
	// seqRef pattern useMembers already uses for its own refetch.
	const primarySeqRef = useRef(0);
	// The designated fleet primary (GET /api/fleet/autosync -> primary_id) is the
	// single source of truth for "who is primary": the same value the backend
	// delete-guard and the Fleet Sync wizard use. The response also carries the
	// server's fleet-state verdict for the header badge. Refreshed below on the
	// events that can change either. (This deliberately does NOT read
	// /api/fleet/last-sync, whose primary_id is only a cosmetic "last run" marker
	// and could name a since-removed host.)
	const refreshPrimary = useCallback(() => {
		const seq = ++primarySeqRef.current;
		api
			.getAutoSync()
			.then((cfg) => {
				if (seq === primarySeqRef.current) setAutoSync(cfg);
			})
			.catch(() => {});
	}, []);
	const primaryId = autoSync?.primary_id || null;
	// useMembers owns the page's single SSE subscription; piggyback on it to
	// refresh the auto-sync status when membership, a sync, health, a fleet /
	// Traefik signal, or a settings change lands, rather than opening a second
	// stream to /api/sse. The health/fleet/traefik events are what move the
	// fleet-state badge, and settings.changed is the only event emitted when
	// auto-sync is toggled or the primary is repointed, so the filter is wider
	// here than the membership-only refresh the primary needed.
	const { members, loading, error, refetch, lastUpdatedAt } = useMembers(
		useCallback(
			(e) => {
				if (
					e.type.startsWith("member.") ||
					e.type.startsWith("config.") ||
					e.type.startsWith("health.") ||
					e.type.startsWith("fleet.") ||
					e.type.startsWith("traefik.") ||
					e.type.startsWith("settings.")
				) {
					refreshPrimary();
				}
			},
			[refreshPrimary],
		),
	);
	const { toast } = useToast();
	const [removing, setRemoving] = useState<MemberView | null>(null);
	useEffect(refreshPrimary, [refreshPrimary]);

	const groupBuild = majorityBuild(members);
	// With a designated primary, version divergence is anchored to it (that is
	// what holds config sync); the majority "odd one out" flag only fills in
	// when no primary is set and there is nothing else to anchor to.
	const primaryMember = primaryId
		? members.find((m) => m.id === primaryId)
		: undefined;
	const primaryBuild = primaryMember ? memberBuild(primaryMember) : null;
	// Pin the fleet primary to the top; every other member keeps its order.
	const orderedMembers = primaryId
		? [
				...members.filter((m) => m.id === primaryId),
				...members.filter((m) => m.id !== primaryId),
			]
		: members;
	// At most one active member means draining the active one would empty the
	// routing pool, so its drain control is disabled (the backend also refuses it).
	const soleActive = members.filter((m) => m.state === "active").length <= 1;

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
	// wizard. A fleet is never allowed to shrink to a single member: at two
	// members (or a lone just-added row) the same Remove disbands the whole
	// fleet, primary included, and the confirm modal says so. A lone row is the
	// one place even a (stale-)designated primary gets a Remove button: with
	// nothing to sync it protects nothing, and disbanding is the only exit from
	// that legacy state (the wizard refuses sub-two fleets, so it cannot recur).
	const disbandOnRemove = members.length <= 2;
	const loneRow = members.length === 1;
	const confirmRemove = async () => {
		if (!removing) return;
		const m = removing;
		const disband = disbandOnRemove;
		setRemoving(null);
		try {
			await api.deleteMember(m.id);
			toast(
				t(disband ? "members.disbanded" : "members.removed", { name: m.name }),
				"info",
			);
			refetch();
			// A disband also drops the primary designation; refresh so the badge
			// and fleet-state header clear without waiting for an SSE round-trip.
			refreshPrimary();
		} catch (err) {
			// last_active_member is still reachable in a 3+ fleet (peers drained);
			// membership_changed means the roster moved between the confirm and the
			// delete, so the action shown may not be the action that would happen.
			if (err instanceof ApiError && err.code === "last_active_member") {
				toast(t("members.removeLastActiveError"), "error");
			} else if (err instanceof ApiError && err.code === "membership_changed") {
				toast(t("members.membershipChangedError"), "error");
				refetch();
			} else {
				toast(t("errors.generic"), "error");
			}
		}
	};

	return (
		<div className="fd-stack">
			<div className="fd-page-title-row">
				<h1 className="fd-page-title">{t("members.title")}</h1>
				{autoSync?.fleet_state && (
					<span
						className={fleetStateBadgeClass(autoSync.fleet_state)}
						data-testid="fleet-state-badge"
						title={(autoSync.fleet_state_reasons ?? [])
							.map((r) => t(`members.fleetReason.${r}`, { defaultValue: r }))
							.join(" · ")}
					>
						{t(`members.fleetState.${autoSync.fleet_state}`)}
					</span>
				)}
			</div>

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
								{/* Header tooltips carry the semantic the two columns are
								    routinely misread over: Verified is the live hash check
								    (the actual "in sync" claim), Last Config Sync is the last
								    WRITE to that member, not config freshness. */}
								<th
									data-testid="col-verified"
									title={t("members.colVerifiedTip")}
								>
									{t("members.colVerified")}
								</th>
								<th
									data-testid="col-last-sync"
									title={t("members.colLastSyncTip")}
								>
									{t("members.colLastSync")}
								</th>
								<th>{t("members.colState")}</th>
								<th />
							</tr>
						</thead>
						<tbody>
							{orderedMembers.map((m) => (
								<MemberRow
									key={m.id}
									member={m}
									groupBuild={primaryId ? null : groupBuild}
									primaryBuild={primaryBuild}
									isPrimary={m.id === primaryId}
									soleActive={soleActive}
									disbandOnRemove={disbandOnRemove}
									loneRow={loneRow}
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
					title={t(
						disbandOnRemove ? "members.disbandTitle" : "members.removeTitle",
						{ name: removing.name },
					)}
					confirmLabel={t(
						disbandOnRemove ? "members.disbandConfirm" : "common.remove",
					)}
					onConfirm={confirmRemove}
					onClose={() => setRemoving(null)}
				>
					<p className="fd-muted">
						{t(disbandOnRemove ? "members.disbandBody" : "members.removeBody", {
							name: removing.name,
						})}
					</p>
				</ConfirmModal>
			)}
		</div>
	);
}

function MemberRow({
	member: m,
	groupBuild,
	primaryBuild,
	isPrimary,
	soleActive,
	disbandOnRemove,
	loneRow,
	onSetState,
	onRemove,
}: {
	member: MemberView;
	groupBuild: Build | null;
	// The designated primary's build, or null when no primary is set. A build with
	// an empty version means the primary's own build is unconfirmed, which the
	// gate treats as skew against every member rather than as "nothing to
	// compare" - so it anchors the badge too.
	primaryBuild: Build | null;
	isPrimary: boolean;
	// True when the fleet has at most one active member. The drain control is
	// disabled for the active member in that case: draining the last active member
	// would empty the routing pool (the backend refuses it with a 409).
	soleActive: boolean;
	// True when the fleet has at most two members, so removing this one disbands
	// the whole fleet (a fleet below two members is not allowed to exist).
	disbandOnRemove: boolean;
	// True when this is the only member row. A lone row is always removable,
	// designated primary or not: disbanding is the only exit from a legacy
	// one-member fleet.
	loneRow: boolean;
	onSetState: (m: MemberView, state: "active" | "drained") => void;
	onRemove: () => void;
}) {
	const { t } = useTranslation();
	const health = m.status.health;
	const build = memberBuild(m);
	const mismatch =
		!!build.version && !!groupBuild && buildsDiffer(build, groupBuild);
	// Mirrors the backend gate: config sync (autosync and the wizard) holds a
	// tokened member while its BUILD differs from the primary's, including an
	// unknown version on EITHER side (the gate fails closed, so a primary whose
	// own version is unread holds the whole fleet). Comparing versions alone would
	// leave this badge silent on a "dev" fleet held for a commit difference,
	// telling the operator a member is in sync while sync is refusing it.
	// Tokenless members are skipped by sync entirely, never held.
	const heldForSkew =
		!isPrimary &&
		m.has_token &&
		!!primaryBuild &&
		buildsDiffer(build, primaryBuild);

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
				<span className="fd-row">
					{build.version ? (
						<span className="fd-mono" title={buildTitle(build)}>
							{buildLabel(build)}
						</span>
					) : (
						<span className="fd-faint">
							{m.has_token ? t("members.versionUnknown") : t("members.noToken")}
						</span>
					)}
					{mismatch && (
						<span
							className="ui-badge ui-badge-warn"
							title={t("members.versionMismatch")}
						>
							<WarningIcon size={12} weight="bold" />
						</span>
					)}
					{/* Rendered for unknown versions too: the gate fails closed, so a
					    tokened member we cannot read is held and must show as held. */}
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
			<td>
				<div className="fd-row" style={{ justifyContent: "flex-end" }}>
					{m.state === "active" ? (
						<button
							type="button"
							className="ui-btn ui-btn-sm"
							// The last active member cannot be drained: the routing pool
							// would be empty and all proxy traffic would fail. The backend
							// enforces this with a 409; disabling here avoids a doomed tap.
							title={
								soleActive
									? t("members.drainLastActiveTip")
									: t("members.drainTip")
							}
							disabled={soleActive}
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
					    here; it is changed only by re-running the Fleet Sync wizard.
					    Exception: a lone row protects nothing, so it is removable
					    (disband) even while a stale designation names it. */}
					{(!isPrimary || loneRow) && (
						<button
							type="button"
							className="ui-btn ui-btn-sm ui-btn-danger"
							title={t(
								disbandOnRemove ? "members.disbandTip" : "members.removeTip",
							)}
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
			<h2 className="fd-card-title" style={{ marginBottom: "0.8rem" }}>
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
