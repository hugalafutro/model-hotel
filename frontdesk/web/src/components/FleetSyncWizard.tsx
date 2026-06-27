import { useCallback, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { ApiError, api } from "../api/client";
import type {
	FleetMemberStatus,
	FleetStatus,
	FleetSyncState,
	MemberView,
} from "../api/types";
import { useToast } from "../context/ToastContext";
import { reportResults } from "../utils/syncResults";
import { formatRelative } from "../utils/time";
import { ConfirmModal } from "./ConfirmModal";
import { Notice } from "./Notice";

// FleetSyncWizard walks the operator through converging a fleet onto one primary,
// one gated step at a time: choose the primary, verify MASTER_KEY, then sync the
// configuration. A step unlocks only once the previous one is satisfied (for every
// reachable member), so config sync can never be reached before MASTER_KEY is
// verified. A single probe (GET /api/fleet/status) drives every gate, and every
// failure surfaces the real backend message rather than a generic toast.

type Step = 1 | 2 | 3 | 4;
const STEPS: Step[] = [1, 2, 3, 4];

// Reachable members other than the primary: the ones a step can actually act on.
function reachablePeers(s: FleetStatus): FleetMemberStatus[] {
	return s.members.filter((m) => m.member_id !== s.primary_id && m.reachable);
}
// MASTER_KEY blockers: reachable members that provably cannot decrypt the
// primary's keys. null (keyless / not evaluated) never blocks.
function masterKeyBlockers(s: FleetStatus): FleetMemberStatus[] {
	return s.members.filter((m) => m.reachable && m.master_key_matches === false);
}
// Schema blockers: reachable members whose app is too old to receive the
// primary's config. The member checks its schema before the MASTER_KEY canary,
// so a skewed member reports master_key_matches=null and zero diff counts and
// would otherwise slip through every gate to Done, where config sync then fails
// for it. They can only be fixed by upgrading the member, so they hard-block.
function schemaBlockers(s: FleetStatus): FleetMemberStatus[] {
	return reachablePeers(s).filter((m) => !m.schema_ok);
}
function configChanges(s: FleetStatus): FleetMemberStatus[] {
	return reachablePeers(s).filter((m) => m.added + m.updated + m.removed > 0);
}
function offlinePeers(s: FleetStatus): FleetMemberStatus[] {
	return s.members.filter((m) => m.member_id !== s.primary_id && !m.reachable);
}

export function FleetSyncWizard({
	members,
	onChanged,
}: {
	members: MemberView[];
	onChanged: () => void;
}) {
	const { t } = useTranslation();
	const { toast } = useToast();
	const [step, setStep] = useState<Step>(1);
	const [primaryId, setPrimaryId] = useState("");
	const [status, setStatus] = useState<FleetStatus | null>(null);
	const [loading, setLoading] = useState(false);
	const [busy, setBusy] = useState(false);
	const [confirm, setConfirm] = useState<"config" | null>(null);
	const [configDone, setConfigDone] = useState(false);
	const [lastSync, setLastSync] = useState<FleetSyncState | null>(null);

	const nameOf = (id: string) => members.find((m) => m.id === id)?.name ?? id;

	// Surface that the wizard has run before (and against which primary) so a
	// fresh-looking step 1 after a container rebuild does not read as "never set
	// up". 204 (never run) resolves to null and shows nothing.
	const loadLastSync = useCallback(() => {
		api
			.fleetLastSync()
			.then((s) => setLastSync(s ?? null))
			.catch(() => {});
	}, []);
	useEffect(() => {
		loadLastSync();
	}, [loadLastSync]);

	const refresh = useCallback(
		async (id: string) => {
			if (!id) return;
			setLoading(true);
			try {
				const fs = await api.fleetStatus(id);
				// An unusable primary comes back without a member list (Go nil slice
				// serialises to null); normalise so the gate helpers never touch null.
				setStatus({ ...fs, members: fs.members ?? [] });
			} catch (e) {
				toast(e instanceof ApiError ? e.message : t("errors.generic"), "error");
			} finally {
				setLoading(false);
			}
		},
		[toast, t],
	);

	// Pick (or change) the primary, then re-probe. Driven from the pick event
	// rather than an effect on primaryId, since the primary only ever changes
	// through this handler. Clearing configDone matters: it records that *the
	// previous* primary's config was synced, and letting it carry over would
	// unlock step 4 for a new primary whose config was never pushed (canStep4
	// keys off configDone).
	const pickPrimary = (id: string) => {
		setConfigDone(false);
		setPrimaryId(id);
		if (id) void refresh(id);
	};

	// Which steps the operator may jump to, derived purely from the latest probe.
	const canStep2 = !!status && status.primary_reachable;
	// Step 3 (config) unlocks once every reachable member can decrypt the
	// primary's keys (MASTER_KEY) and is on a compatible schema.
	const canStep3 =
		canStep2 &&
		masterKeyBlockers(status as FleetStatus).length === 0 &&
		schemaBlockers(status as FleetStatus).length === 0;
	// Step 4 (done) unlocks only once the config step has been completed, either by
	// running the sync or, when there is nothing to push, by acknowledging it on the
	// config step (configDone). Gating on configDone alone keeps the config step the
	// sole owner of the transition to Done, so Done can never be reached by jumping
	// past the config review.
	const canStep4 = canStep3 && configDone;

	const unlocked = (s: Step): boolean => {
		switch (s) {
			case 1:
				return true;
			case 2:
				return canStep2;
			case 3:
				return canStep3;
			case 4:
				return canStep4;
		}
	};

	const go = (s: Step) => {
		if (unlocked(s)) setStep(s);
	};

	const doConfigSync = async () => {
		setBusy(true);
		try {
			const res = await api.configSync(primaryId);
			const ok = res.results.filter((r) => r.ok).length;
			reportResults(res.results, toast, t);
			toast(
				t("settings.configSyncDone", {
					ok,
					total: res.results.length,
					count: res.results.length,
				}),
				ok === res.results.length ? "success" : "error",
			);
			onChanged();
			setConfigDone(true);
			loadLastSync();
			await refresh(primaryId);
			setStep(4);
		} catch (e) {
			toast(e instanceof ApiError ? e.message : t("errors.generic"), "error");
		} finally {
			setBusy(false);
			setConfirm(null);
		}
	};

	const restart = () => {
		setStep(1);
		setPrimaryId("");
		setStatus(null);
		setConfigDone(false);
	};

	const overwrites = status ? configChanges(status) : [];
	const totalRemoved = overwrites.reduce((n, i) => n + i.removed, 0);

	return (
		<div className="ui-card ui-card-pad">
			<h2 style={{ fontSize: "1rem" }}>{t("settings.wizard.title")}</h2>
			<p
				className="fd-muted"
				style={{ fontSize: "0.85rem", margin: "0.4rem 0 1rem" }}
			>
				{t("settings.wizard.intro")}
			</p>

			{step === 1 && (
				<StepChoosePrimary
					members={members}
					primaryId={primaryId}
					status={status}
					loading={loading}
					lastSync={lastSync}
					onPick={pickPrimary}
				/>
			)}
			{step === 2 && status && (
				<StepMasterKey status={status} nameOf={nameOf} />
			)}
			{step === 3 && status && (
				<StepConfig
					status={status}
					overwrites={overwrites}
					busy={busy}
					onSync={() => setConfirm("config")}
					onContinue={() => {
						setConfigDone(true);
						setStep(4);
					}}
				/>
			)}
			{step === 4 && status && (
				<StepDone
					status={status}
					members={members}
					primaryName={nameOf(status.primary_id)}
				/>
			)}

			<WizardNav
				step={step}
				unlocked={unlocked}
				loading={loading}
				onGo={go}
				onRestart={restart}
			/>

			{confirm === "config" && (
				<ConfirmModal
					title={t("settings.configSyncConfirmTitle", {
						count: overwrites.length,
					})}
					confirmLabel={t("settings.configSyncDo", {
						count: overwrites.length,
					})}
					busy={busy}
					busyLabel={t("settings.configSyncDoing")}
					ackLabel={t("settings.configSyncAck")}
					onConfirm={doConfigSync}
					onClose={() => setConfirm(null)}
				>
					<p className="fd-muted">{t("settings.configSyncConfirmBody")}</p>
					{busy && (
						<p className="fd-muted" style={{ margin: "0.5rem 0" }}>
							<span className="fd-spinner" aria-hidden="true" />{" "}
							{t("settings.configSyncProgress")}
						</p>
					)}
					{totalRemoved > 0 && (
						<p className="fd-error-text" style={{ margin: "0.5rem 0" }}>
							{t("settings.configSyncRemovalWarning", { count: totalRemoved })}
						</p>
					)}
					<ul style={{ margin: "0.6rem 0" }}>
						{overwrites.map((m) => (
							<li key={m.member_id}>
								{m.name}
								<span className="fd-faint">
									{" "}
									(
									<span
										title={t("settings.wizard.configTipAdded", {
											count: m.added,
										})}
									>
										+{m.added}
									</span>{" "}
									<span
										title={t("settings.wizard.configTipUpdated", {
											count: m.updated,
										})}
									>
										~{m.updated}
									</span>{" "}
									<span
										title={t("settings.wizard.configTipRemoved", {
											count: m.removed,
										})}
									>
										-{m.removed}
									</span>
									)
								</span>
							</li>
						))}
					</ul>
					<ConfigLegend />
				</ConfirmModal>
			)}
		</div>
	);
}

// --- Steps -----------------------------------------------------------------

function StepChoosePrimary({
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
							{isOnline(m) ? "" : ` — ${t("settings.wizard.offline")}`}
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

function StepMasterKey({
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

function StepConfig({
	status,
	overwrites,
	busy,
	onSync,
	onContinue,
}: {
	status: FleetStatus;
	overwrites: FleetMemberStatus[];
	busy: boolean;
	onSync: () => void;
	onContinue: () => void;
}) {
	const { t } = useTranslation();
	return (
		<div>
			<h3 className="fd-step-title">{t("settings.wizard.step3Title")}</h3>
			<p className="fd-faint fd-step-intro">
				{t("settings.wizard.step3Intro")}
			</p>
			<MemberTable status={status} kind="config" />
			{overwrites.length === 0 ? (
				<div style={{ marginTop: "0.7rem" }}>
					<Notice variant="info">{t("settings.wizard.step3NoChanges")}</Notice>
					<button
						type="button"
						className="ui-btn ui-btn-primary"
						style={{ marginTop: "0.8rem" }}
						onClick={onContinue}
					>
						{t("settings.wizard.continueNoChanges")}
					</button>
				</div>
			) : (
				<div style={{ marginTop: "0.8rem" }}>
					<ConfigLegend />
					<button
						type="button"
						className="ui-btn"
						disabled={busy}
						onClick={onSync}
					>
						{t("settings.wizard.syncConfigBtn")}
					</button>
				</div>
			)}
		</div>
	);
}

function StepDone({
	status,
	members,
	primaryName,
}: {
	status: FleetStatus;
	members: MemberView[];
	primaryName: string;
}) {
	const { t } = useTranslation();
	const synced = reachablePeers(status).length;
	const offline = offlinePeers(status);

	// The load balancer's pool is exactly the active members (BuildTraefikConfig
	// drops drained ones), so list those as the instances behind it.
	const pool = members.filter((m) => m.state === "active");
	// Front Desk knows the LB port (LB_PORT) but not the operator's public host,
	// so pair the port with the host they reached this UI on: in the single-stack
	// HA compose that is the same machine the load balancer runs on.
	const port = status.lb_port ?? "8080";
	const host =
		typeof window !== "undefined" ? window.location.hostname : "your-host";
	const directURL = `http://${host}:${port}/v1`;
	const forwardURL = `http://${host}:${port}`;

	return (
		<div>
			<h3 className="fd-step-title">{t("settings.wizard.step4Title")}</h3>
			<Notice variant="info" style={{ marginTop: "0.2rem" }}>
				{t("settings.wizard.doneSummary", {
					count: synced,
					primary: primaryName,
				})}
			</Notice>
			{offline.length > 0 && (
				<Notice variant="warn" style={{ marginTop: "0.7rem" }}>
					{t("settings.wizard.skippedOffline")}
					<ul style={{ margin: "0.4rem 0 0" }}>
						{offline.map((m) => (
							<li key={m.member_id}>{m.name}</li>
						))}
					</ul>
				</Notice>
			)}

			<div style={{ marginTop: "1.1rem" }}>
				<h4 style={{ fontSize: "0.95rem", margin: "0 0 0.3rem" }}>
					{t("settings.wizard.doneUseTitle")}
				</h4>
				<p className="fd-faint" style={{ fontSize: "0.83rem", margin: 0 }}>
					{t("settings.wizard.doneUseIntro")}
				</p>

				<div style={{ marginTop: "0.8rem" }}>
					<div style={{ fontSize: "0.85rem", marginBottom: "0.3rem" }}>
						{t("settings.wizard.doneDirectTitle")}
					</div>
					<CopyRow value={directURL} />
				</div>

				<div style={{ marginTop: "0.9rem" }}>
					<div style={{ fontSize: "0.85rem", marginBottom: "0.3rem" }}>
						{t("settings.wizard.doneProxyTitle")}
					</div>
					<div
						className="fd-faint"
						style={{ fontSize: "0.82rem", marginBottom: "0.3rem" }}
					>
						{t("settings.wizard.doneProxyForward")}
					</div>
					<CopyRow value={forwardURL} />
					<div
						className="fd-faint"
						style={{ fontSize: "0.82rem", marginTop: "0.4rem" }}
					>
						{t("settings.wizard.doneProxyClients")}
					</div>
				</div>

				<p
					className="fd-faint"
					style={{ fontSize: "0.78rem", marginTop: "0.7rem" }}
				>
					{t("settings.wizard.donePortNote", { port })}
				</p>

				<div style={{ marginTop: "0.9rem" }}>
					<div style={{ fontSize: "0.85rem", marginBottom: "0.3rem" }}>
						{t("settings.wizard.donePoolTitle")}
					</div>
					{pool.length === 0 ? (
						<div className="fd-faint" style={{ fontSize: "0.82rem" }}>
							{t("settings.wizard.donePoolEmpty")}
						</div>
					) : (
						<ul className="fd-mono" style={{ margin: 0, fontSize: "0.82rem" }}>
							{pool.map((m) => (
								<li key={m.id}>{m.url}</li>
							))}
						</ul>
					)}
				</div>

				<Notice variant="info" style={{ marginTop: "1rem" }}>
					<strong>{t("settings.wizard.doneHttpsTitle")}</strong>{" "}
					{t("settings.wizard.doneHttpsNote")}
				</Notice>
			</div>
		</div>
	);
}

// CopyRow shows a monospace URL with a copy button, mirroring the reset panel's
// clipboard handling (silently no-ops when the clipboard is blocked; the text
// stays selectable).
function CopyRow({ value }: { value: string }) {
	const { t } = useTranslation();
	const [copied, setCopied] = useState(false);
	const copy = async () => {
		try {
			await navigator.clipboard.writeText(value);
			setCopied(true);
			setTimeout(() => setCopied(false), 2000);
		} catch {
			/* clipboard blocked: the value stays selectable */
		}
	};
	return (
		<div className="fd-row" style={{ gap: "0.5rem", alignItems: "center" }}>
			<code
				className="ui-input fd-mono"
				style={{
					flex: "1 1 auto",
					padding: "0.3rem 0.5rem",
					userSelect: "all",
				}}
			>
				{value}
			</code>
			<button type="button" className="ui-btn ui-btn-sm" onClick={copy}>
				{copied ? t("common.copied") : t("common.copy")}
			</button>
		</div>
	);
}

// --- Shared bits ------------------------------------------------------------

// ConfigLegend explains the +added / ~updated / -removed badges, which are
// otherwise bare numbers with no hint at what they count.
function ConfigLegend() {
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
// steps but never skip a gate.
function WizardNav({
	step,
	unlocked,
	loading,
	onGo,
	onRestart,
}: {
	step: Step;
	unlocked: (s: Step) => boolean;
	loading: boolean;
	onGo: (s: Step) => void;
	onRestart: () => void;
}) {
	const { t } = useTranslation();
	const next = (step + 1) as Step;
	return (
		<div className="fd-wizard-nav">
			<div className="fd-row" style={{ gap: "0.5rem" }}>
				{step > 1 && (
					<button
						type="button"
						className="ui-btn"
						onClick={() => onGo((step - 1) as Step)}
					>
						{t("settings.wizard.back")}
					</button>
				)}
				{step < 4 && (
					<button
						type="button"
						className="ui-btn ui-btn-primary"
						disabled={loading || !unlocked(next)}
						onClick={() => onGo(next)}
					>
						{t("settings.wizard.next")}
					</button>
				)}
				{step === 4 && (
					<button type="button" className="ui-btn" onClick={onRestart}>
						{t("settings.wizard.runAgain")}
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
