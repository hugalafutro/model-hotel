import { useTranslation } from "react-i18next";
import type {
	FleetMemberStatus,
	FleetStatus,
	FleetSyncState,
	FleetVersionCheck,
	MemberView,
} from "../../api/types";
import { formatRelative } from "../../utils/time";
import { Notice } from "../Notice";
import { masterKeyBlockers, STEPS, type Step, schemaBlockers } from "./gates";

export function StepChoosePrimary({
	members,
	primaryId,
	status,
	loading,
	lastSync,
	onPick,
}: {
	members: MemberView[];
	primaryId: string;
	status: FleetStatus | null;
	loading: boolean;
	lastSync: FleetSyncState | null;
	onPick: (id: string) => void;
}) {
	const { t } = useTranslation();
	const isOnline = (m: MemberView) =>
		m.status.health.known && m.status.health.healthy;
	return (
		<div>
			<h3 className="fd-step-title">{t("settings.wizard.step1Title")}</h3>
			<p className="fd-faint fd-step-intro">
				{t("settings.wizard.step1Intro")}
			</p>
			{lastSync && (
				<Notice variant="info" style={{ margin: "0 0 0.8rem" }}>
					{t("settings.wizard.lastRunBanner", {
						when: formatRelative(lastSync.last_run_at),
						name: lastSync.primary_name,
					})}
				</Notice>
			)}
			<div className="ui-field" style={{ maxWidth: 360 }}>
				<label className="ui-label" htmlFor="wizard-primary">
					{t("settings.wizard.primaryLabel")}
				</label>
				<select
					id="wizard-primary"
					className="ui-select"
					value={primaryId}
					onChange={(e) => onPick(e.target.value)}
				>
					<option value="">{t("settings.wizard.selectPrimary")}</option>
					{members.map((m) => (
						<option key={m.id} value={m.id} disabled={!isOnline(m)}>
							{m.name}
							{isOnline(m) ? "" : ` (${t("settings.wizard.offline")})`}
						</option>
					))}
				</select>
			</div>
			{loading && <div className="fd-faint">{t("common.loading")}</div>}
			{status && !status.primary_reachable && (
				<Notice variant="warn" style={{ marginTop: "0.7rem" }}>
					{t("settings.wizard.primaryUnusable", {
						note: status.primary_note ?? "",
					})}
				</Notice>
			)}
		</div>
	);
}

export function StepMasterKey({
	status,
	nameOf,
}: {
	status: FleetStatus;
	nameOf: (id: string) => string;
}) {
	const { t } = useTranslation();
	const blockers = masterKeyBlockers(status);
	const tooOld = schemaBlockers(status);
	return (
		<div>
			<h3 className="fd-step-title">{t("settings.wizard.step2Title")}</h3>
			<p className="fd-faint fd-step-intro">
				{t("settings.wizard.step2Intro")}
			</p>
			<MemberTable status={status} kind="masterKey" />
			{tooOld.length > 0 && (
				<Notice variant="warn" style={{ marginTop: "0.7rem" }}>
					{t("settings.wizard.schemaRemedy")}
					<ul style={{ margin: "0.4rem 0 0" }}>
						{tooOld.map((m) => (
							<li key={m.member_id}>{nameOf(m.member_id)}</li>
						))}
					</ul>
				</Notice>
			)}
			{blockers.length > 0 && (
				<Notice variant="warn" style={{ marginTop: "0.7rem" }}>
					{t("settings.wizard.step2Remedy")}
					<ul style={{ margin: "0.4rem 0 0" }}>
						{blockers.map((m) => (
							<li key={m.member_id}>{nameOf(m.member_id)}</li>
						))}
					</ul>
				</Notice>
			)}
			{blockers.length === 0 && tooOld.length === 0 && (
				<Notice variant="info" style={{ marginTop: "0.7rem" }}>
					{t("settings.wizard.step2Ok")}
				</Notice>
			)}
		</div>
	);
}

export function StepConfig({
	status,
	overwrites,
	busy,
	changingPrimary,
	blockedReason,
	skew,
	checkingSkew,
	onRefreshSkew,
	onProceed,
}: {
	status: FleetStatus;
	overwrites: FleetMemberStatus[];
	busy: boolean;
	changingPrimary: boolean;
	// Non-empty when the selected host cannot be set as primary (re-selecting the
	// current primary). Shows the reason and disables the proceed action.
	blockedReason: string;
	// Latest version-alignment check; null while none has landed (blocks the
	// sync action, failing closed) or after a failed check.
	skew: FleetVersionCheck | null;
	checkingSkew: boolean;
	onRefreshSkew: () => void;
	onProceed: () => void;
}) {
	const { t } = useTranslation();
	const hasSkew = (skew?.skewed.length ?? 0) > 0;
	const blocked = blockedReason !== "" || hasSkew || checkingSkew || !skew;
	return (
		<div>
			<h3 className="fd-step-title">{t("settings.wizard.step3Title")}</h3>
			<p className="fd-faint fd-step-intro">
				{t("settings.wizard.step3Intro")}
			</p>
			<MemberTable status={status} kind="config" />
			{blockedReason !== "" && (
				<Notice variant="warn" style={{ marginTop: "0.7rem" }}>
					{blockedReason}
				</Notice>
			)}
			{hasSkew && skew && (
				<Notice variant="warn" style={{ marginTop: "0.7rem" }}>
					<div data-testid="version-skew-block">
						<p style={{ margin: 0 }}>
							{t("settings.wizard.versionSkewBlock", {
								count: skew.skewed.length,
							})}
						</p>
						<ul className="fd-mono" style={{ margin: "0.4rem 0 0.5rem" }}>
							{skew.skewed.map((m) => (
								<li key={m.member_id}>
									{m.name} ({m.version || t("members.versionUnknown")}
									{/* The commit is what names the difference on a fleet whose
									    members all report "dev": without it these rows read
									    identical to an aligned one. Shown only when it is a real
									    commit, since "unknown" identifies nothing. */}
									{m.commit && m.commit !== "unknown" ? ` · ${m.commit}` : ""})
								</li>
							))}
						</ul>
						<button
							type="button"
							className="ui-btn ui-btn-sm"
							data-testid="version-skew-refresh"
							disabled={checkingSkew}
							onClick={onRefreshSkew}
						>
							{checkingSkew
								? t("common.loading")
								: t("settings.wizard.versionSkewRefresh")}
						</button>
					</div>
				</Notice>
			)}
			{overwrites.length === 0 ? (
				<div style={{ marginTop: "0.7rem" }}>
					<Notice variant="info">{t("settings.wizard.step3NoChanges")}</Notice>
					<button
						type="button"
						className="ui-btn ui-btn-primary"
						style={{ marginTop: "0.8rem" }}
						disabled={busy || blocked}
						onClick={onProceed}
					>
						{t(
							changingPrimary
								? "settings.wizard.setPrimaryBtn"
								: "settings.wizard.continueNoChanges",
						)}
					</button>
				</div>
			) : (
				<div style={{ marginTop: "0.8rem" }}>
					<ConfigLegend />
					<button
						type="button"
						className="ui-btn"
						disabled={busy || blocked}
						onClick={onProceed}
					>
						{t("settings.wizard.syncConfigBtn")}
					</button>
				</div>
			)}
		</div>
	);
}

// --- Shared bits ------------------------------------------------------------

// TokenField is the admin-token input shown in a confirm modal when the action
// re-designates an existing primary (the backend gates that change on the token).
export function TokenField({
	value,
	error,
	note,
	onChange,
}: {
	value: string;
	error: string;
	note?: string;
	onChange: (v: string) => void;
}) {
	const { t } = useTranslation();
	return (
		<div className="ui-field" style={{ marginTop: "0.4rem" }}>
			{note && (
				<div
					className="fd-faint"
					style={{ fontSize: "0.8rem", marginBottom: "0.3rem" }}
				>
					{note}
				</div>
			)}
			<label className="ui-label" htmlFor="fd-wizard-confirm-token">
				{t("settings.wizard.confirmTokenLabel")}
			</label>
			<input
				id="fd-wizard-confirm-token"
				className="ui-input"
				type="password"
				autoComplete="current-password"
				value={value}
				onChange={(e) => onChange(e.target.value)}
				placeholder={t("settings.wizard.confirmTokenPlaceholder")}
			/>
			{error && (
				<div
					className="fd-error-text"
					role="alert"
					style={{ marginTop: "0.3rem" }}
				>
					{error}
				</div>
			)}
		</div>
	);
}

// ConfigLegend explains the +added / ~updated / -removed badges, which are
// otherwise bare numbers with no hint at what they count.
export function ConfigLegend() {
	const { t } = useTranslation();
	const rows: [string, string][] = [
		["ui-badge-ok", "settings.wizard.configLegendAdded"],
		["ui-badge-info", "settings.wizard.configLegendUpdated"],
		["ui-badge-warn", "settings.wizard.configLegendRemoved"],
	];
	const sign = ["+", "~", "-"];
	return (
		<div
			className="fd-faint"
			style={{ fontSize: "0.78rem", marginBottom: "0.8rem" }}
		>
			<div style={{ marginBottom: "0.4rem" }}>
				{t("settings.wizard.configLegendTitle")}
			</div>
			<ul style={{ margin: 0, listStyle: "none", padding: 0 }}>
				{rows.map(([cls, key], i) => (
					<li
						key={cls}
						className="fd-row"
						style={{ gap: "0.4rem", marginTop: "0.2rem" }}
					>
						<span className={`ui-badge ${cls}`}>{sign[i]}N</span>
						<span>{t(key)}</span>
					</li>
				))}
			</ul>
		</div>
	);
}

// MemberTable renders one row per member with a badge that depends on the step.
function MemberTable({
	status,
	kind,
}: {
	status: FleetStatus;
	kind: "masterKey" | "config";
}) {
	const { t } = useTranslation();
	return (
		<table className="ui-table" style={{ marginTop: "0.5rem" }}>
			<tbody>
				{status.members.map((m) => (
					<tr key={m.member_id}>
						<td>
							{m.name}
							{m.member_id === status.primary_id && (
								<span className="fd-faint"> · {t("settings.primaryTag")}</span>
							)}
							{m.note && (
								<div className="fd-faint" style={{ fontSize: "0.78rem" }}>
									{m.note}
								</div>
							)}
						</td>
						<td style={{ textAlign: "right" }}>
							<MemberBadge
								member={m}
								primaryId={status.primary_id}
								kind={kind}
							/>
						</td>
					</tr>
				))}
			</tbody>
		</table>
	);
}

function MemberBadge({
	member,
	primaryId,
	kind,
}: {
	member: FleetMemberStatus;
	primaryId: string;
	kind: "masterKey" | "config";
}) {
	const { t } = useTranslation();
	if (member.member_id === primaryId)
		return (
			<span className="ui-badge ui-badge-ok">{t("settings.primaryTag")}</span>
		);
	if (!member.has_token)
		return (
			<span className="ui-badge ui-badge-danger">
				{t("settings.wizard.badgeNoToken")}
			</span>
		);
	if (!member.reachable)
		return (
			<span className="ui-badge ui-badge-warn">
				{t("settings.wizard.badgeOffline")}
			</span>
		);

	if (kind === "masterKey") {
		// A schema-skewed member never ran the MASTER_KEY canary (master_key_matches
		// stays null), so "nothing to verify" would mislead: flag the real blocker.
		if (!member.schema_ok)
			return (
				<span className="ui-badge ui-badge-danger">
					{t("settings.wizard.badgeTooOld")}
				</span>
			);
		if (member.master_key_matches === null)
			return (
				<span className="ui-badge ui-badge-info">
					{t("settings.wizard.badgeKeyless")}
				</span>
			);
		return member.master_key_matches ? (
			<span className="ui-badge ui-badge-ok">
				{t("settings.wizard.badgeMatch")}
			</span>
		) : (
			<span className="ui-badge ui-badge-danger">
				{t("settings.wizard.badgeMismatch")}
			</span>
		);
	}
	// config
	const changes = member.added + member.updated + member.removed;
	if (changes === 0)
		return (
			<span className="ui-badge ui-badge-ok">
				{t("settings.wizard.badgeMatch")}
			</span>
		);
	return (
		<span
			className="fd-row"
			style={{ gap: "0.35rem", justifyContent: "flex-end" }}
		>
			{member.added > 0 && (
				<span
					className="ui-badge ui-badge-ok"
					title={t("settings.wizard.configTipAdded", { count: member.added })}
				>
					+{member.added}
				</span>
			)}
			{member.updated > 0 && (
				<span
					className="ui-badge ui-badge-info"
					title={t("settings.wizard.configTipUpdated", {
						count: member.updated,
					})}
				>
					~{member.updated}
				</span>
			)}
			{member.removed > 0 && (
				<span
					className="ui-badge ui-badge-warn"
					title={t("settings.wizard.configTipRemoved", {
						count: member.removed,
					})}
				>
					-{member.removed}
				</span>
			)}
		</span>
	);
}

// WizardNav: Back / Next plus the dotted step indicator at the bottom. A dot is
// clickable only when that step is unlocked, so the operator can review earlier
// steps but never skip a gate. onCancel (present only on a re-run) returns to the
// resting screen without committing.
export function WizardNav({
	step,
	unlocked,
	loading,
	onGo,
	onCancel,
}: {
	step: Step;
	unlocked: (s: Step) => boolean;
	loading: boolean;
	onGo: (s: Step) => void;
	onCancel?: () => void;
}) {
	const { t } = useTranslation();
	const next = (step + 1) as Step;
	return (
		<div className="fd-wizard-nav">
			<div className="fd-row" style={{ gap: "0.5rem" }}>
				{onCancel && (
					<button type="button" className="ui-btn" onClick={onCancel}>
						{t("settings.wizard.cancelRerun")}
					</button>
				)}
				{step > 1 && (
					<button
						type="button"
						className="ui-btn"
						onClick={() => onGo((step - 1) as Step)}
					>
						{t("settings.wizard.back")}
					</button>
				)}
				{step < 3 && (
					<button
						type="button"
						className="ui-btn ui-btn-primary"
						disabled={loading || !unlocked(next)}
						onClick={() => onGo(next)}
					>
						{t("settings.wizard.next")}
					</button>
				)}
			</div>
			<div className="fd-wizard-dots" aria-hidden="true">
				{STEPS.map((s) => {
					const state = s === step ? "current" : s < step ? "done" : "ahead";
					const reachable = unlocked(s);
					return (
						<button
							type="button"
							key={s}
							className={`fd-dot fd-dot-${state}`}
							disabled={!reachable}
							onClick={() => onGo(s)}
							aria-label={t("settings.wizard.stepN", { n: s })}
						/>
					);
				})}
			</div>
		</div>
	);
}
