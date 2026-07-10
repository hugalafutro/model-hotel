import { useCallback, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { ApiError, api } from "../api/client";
import type {
	AutoSyncConfig,
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

// FleetSyncWizard is the single control for designating a source-of-truth member
// and keeping the fleet converged on it. It has two faces:
//
//   - Resting screen (a primary is already designated): shows which member is
//     primary, the automatic-sync state with a Pause/Resume switch, where to send
//     traffic, and a red, token-gated "Re-run" that re-designates the primary and
//     overwrites the fleet.
//   - Wizard (no primary yet, or a re-run): the gated flow choose -> verify
//     MASTER_KEY -> sync. A step unlocks only once the previous one is satisfied
//     for every reachable member, so config can never be pushed before MASTER_KEY
//     is verified. A single probe (GET /api/fleet/status) drives every gate.
//
// Completing the wizard persists {enabled: true, primary_id} so "a primary is
// set" means "the wizard converged the fleet at least once", and auto-sync then
// keeps the fleet matched. The one exception is a re-run that re-selects the
// same primary: that is a manual re-sync and preserves the operator's paused
// state rather than silently resuming auto-sync. The token gate on a *change*
// runs before the destructive push, so a wrong token fails with nothing
// overwritten.

type Step = 1 | 2 | 3;
const STEPS: Step[] = [1, 2, 3];
type View = "loading" | "wizard" | "resting";
type ConfirmKind = "config" | "change" | null;

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
// would otherwise slip through every gate, where config sync then fails for it.
// They can only be fixed by upgrading the member, so they hard-block.
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
	const [view, setView] = useState<View>("loading");
	// The persisted designation. primary_id === "" means none is set yet.
	const [autoSync, setAutoSync] = useState<AutoSyncConfig>({
		enabled: false,
		primary_id: "",
	});
	const [step, setStep] = useState<Step>(1);
	const [primaryId, setPrimaryId] = useState("");
	const [status, setStatus] = useState<FleetStatus | null>(null);
	const [loading, setLoading] = useState(false);
	const [busy, setBusy] = useState(false);
	const [confirm, setConfirm] = useState<ConfirmKind>(null);
	const [commitToken, setCommitToken] = useState("");
	const [commitError, setCommitError] = useState("");
	const [lastSync, setLastSync] = useState<FleetSyncState | null>(null);

	const nameOf = (id: string) => members.find((m) => m.id === id)?.name ?? id;

	// Changing an already-designated primary to a different member is the gated,
	// destructive case; the very first designation applies without a token.
	const changingPrimary =
		autoSync.primary_id !== "" && autoSync.primary_id !== primaryId;
	// Re-selecting the current primary as the "new" primary is not a valid change:
	// the primary is the source of truth and cannot be replaced with itself. The
	// backend also rejects the same physical host reached under a different URL
	// (409); here we block the trivial same-member-row case in the UI. (There is no
	// manual re-sync path: pause/resume the auto-syncer instead.)
	const sameHostReselected =
		autoSync.primary_id !== "" && autoSync.primary_id === primaryId;

	const loadLastSync = useCallback(() => {
		api
			.fleetLastSync()
			.then((s) => setLastSync(s ?? null))
			.catch(() => {});
	}, []);

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

	// On mount, decide which face to show from the persisted designation. A set
	// primary lands on the resting screen and probes once for the usage details
	// (lb_port, pool); no primary opens the wizard. A failed read degrades to the
	// wizard rather than dead-ending.
	useEffect(() => {
		loadLastSync();
		api
			.getAutoSync()
			.then((cfg) => {
				setAutoSync(cfg);
				if (cfg.primary_id) {
					setPrimaryId(cfg.primary_id);
					setView("resting");
					void refresh(cfg.primary_id);
				} else {
					setView("wizard");
				}
			})
			.catch(() => setView("wizard"));
	}, [loadLastSync, refresh]);

	// Pick (or change) the primary, then re-probe. Driven from the pick event
	// rather than an effect on primaryId, since the primary only ever changes
	// through this handler.
	const pickPrimary = (id: string) => {
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

	const unlocked = (s: Step): boolean => {
		switch (s) {
			case 1:
				return true;
			case 2:
				return canStep2;
			case 3:
				return canStep3;
		}
	};

	const go = (s: Step) => {
		if (unlocked(s)) setStep(s);
	};

	const overwrites = status ? configChanges(status) : [];
	const totalRemoved = overwrites.reduce((n, i) => n + i.removed, 0);

	const closeConfirm = () => {
		setConfirm(null);
		setCommitToken("");
		setCommitError("");
	};

	// commit persists the designation (+ auto-sync on) and then, when there is
	// anything to push, syncs the fleet. Order matters: the token-gated persist
	// runs first, so a rejected token fails before a single member is overwritten.
	const commit = async () => {
		setBusy(true);
		setCommitError("");
		let saved: AutoSyncConfig;
		try {
			saved = await api.putAutoSync(
				{ enabled: true, primary_id: primaryId },
				changingPrimary ? commitToken.trim() : undefined,
			);
		} catch (e) {
			// Designation rejected. Nothing was pushed; keep the modal open so the
			// operator can retry. A 409 is the same-host guard: the selected host is
			// already the primary, reached under a different address.
			setCommitError(
				e instanceof ApiError && e.status === 409
					? t("settings.wizard.sameHostError")
					: e instanceof ApiError && (e.status === 400 || e.status === 403)
						? e.message
						: t("errors.generic"),
			);
			setBusy(false);
			return;
		}
		setAutoSync(saved);
		closeConfirm();
		try {
			if (overwrites.length > 0) {
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
			} else {
				toast(t("settings.wizard.savedToast"), "success");
			}
			onChanged();
			loadLastSync();
			await refresh(primaryId);
			setView("resting");
		} catch (e) {
			// The designation is saved and auto-sync is on, so the loop will still
			// converge; surface the push failure but stay on the config step to retry.
			toast(e instanceof ApiError ? e.message : t("errors.generic"), "error");
			await refresh(primaryId);
		} finally {
			setBusy(false);
		}
	};

	// Step 3's single "proceed" action. With changes to push (or a primary change
	// needing a token) it routes through a confirmation; a first, clean setup with
	// nothing to push commits straight through.
	const proceed = () => {
		// Re-selecting the current primary is not a valid change (the source of
		// truth cannot be replaced with itself). The proceed button is disabled in
		// this case; this is a belt-and-suspenders guard.
		if (sameHostReselected) return;
		setCommitToken("");
		setCommitError("");
		if (overwrites.length > 0) setConfirm("config");
		else if (changingPrimary) setConfirm("change");
		else void commit();
	};

	// Pause / resume auto-sync without touching the primary. Flipping only the
	// enabled flag never needs the token (the backend gates primary changes only).
	const toggleEnabled = async () => {
		const next = { ...autoSync, enabled: !autoSync.enabled };
		try {
			setAutoSync(await api.putAutoSync(next));
			toast(t("settings.wizard.savedToast"), "success");
		} catch (e) {
			toast(e instanceof ApiError ? e.message : t("errors.generic"), "error");
		}
	};

	// Enter the wizard to re-designate the primary. The saved designation is left
	// in place until the re-run commits, so `changingPrimary` still gates the
	// token and a cancelled re-run changes nothing.
	const startRerun = () => {
		setPrimaryId("");
		setStatus(null);
		setStep(1);
		closeConfirm();
		setView("wizard");
	};

	// Return to the resting screen from a re-run without committing.
	const cancelRerun = () => {
		setPrimaryId(autoSync.primary_id);
		setStatus(null);
		closeConfirm();
		setView("resting");
		void refresh(autoSync.primary_id);
	};

	if (view === "loading")
		return (
			<div className="ui-card ui-card-pad">
				<div className="fd-faint">{t("common.loading")}</div>
			</div>
		);

	if (view === "resting")
		return (
			<div className="ui-card ui-card-pad">
				<h2 style={{ fontSize: "1rem" }}>
					{t("settings.wizard.restingTitle")}
				</h2>
				<StepResting
					status={status}
					members={members}
					primaryName={nameOf(autoSync.primary_id)}
					enabled={autoSync.enabled}
					lastSync={lastSync}
					onToggleEnabled={() => void toggleEnabled()}
					onRerun={startRerun}
				/>
			</div>
		);

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
					changingPrimary={changingPrimary}
					blockedReason={
						sameHostReselected ? t("settings.wizard.sameHostError") : ""
					}
					onProceed={proceed}
				/>
			)}

			<WizardNav
				step={step}
				unlocked={unlocked}
				loading={loading}
				onGo={go}
				onCancel={autoSync.primary_id ? cancelRerun : undefined}
			/>

			{confirm === "config" && (
				<ConfirmModal
					title={t("settings.configSyncConfirmTitle", {
						count: overwrites.length,
					})}
					confirmLabel={t("settings.configSyncDo", {
						count: overwrites.length,
					})}
					confirmDisabled={changingPrimary && !commitToken.trim()}
					busy={busy}
					busyLabel={t("settings.configSyncDoing")}
					ackLabel={t("settings.configSyncAck")}
					onConfirm={() => void commit()}
					onClose={closeConfirm}
				>
					<p className="fd-muted">{t("settings.configSyncConfirmBody")}</p>
					{busy && (
						<Notice variant="warn" style={{ margin: "0.5rem 0" }}>
							<span className="fd-spinner" aria-hidden="true" />{" "}
							{t("settings.configSyncProgress")}
						</Notice>
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
					{changingPrimary && (
						<TokenField
							value={commitToken}
							error={commitError}
							note={t("settings.wizard.changeTokenNote")}
							onChange={setCommitToken}
						/>
					)}
				</ConfirmModal>
			)}

			{confirm === "change" && (
				<ConfirmModal
					title={t("settings.wizard.changeTitle")}
					confirmLabel={t("settings.wizard.changeConfirm")}
					confirmDisabled={!commitToken.trim()}
					busy={busy}
					onConfirm={() => void commit()}
					onClose={closeConfirm}
				>
					<p className="fd-muted">
						{t("settings.wizard.changeBody", { name: nameOf(primaryId) })}
					</p>
					<TokenField
						value={commitToken}
						error={commitError}
						onChange={setCommitToken}
					/>
				</ConfirmModal>
			)}
		</div>
	);
}

// --- Resting screen ---------------------------------------------------------

function StepResting({
	status,
	members,
	primaryName,
	enabled,
	lastSync,
	onToggleEnabled,
	onRerun,
}: {
	status: FleetStatus | null;
	members: MemberView[];
	primaryName: string;
	enabled: boolean;
	lastSync: FleetSyncState | null;
	onToggleEnabled: () => void;
	onRerun: () => void;
}) {
	const { t } = useTranslation();
	return (
		<div className="fd-stack">
			<Notice variant="info" style={{ marginTop: "0.2rem" }}>
				<strong>{t("settings.wizard.restingPrimaryLabel")}:</strong>{" "}
				{primaryName}
				<div
					className="fd-faint"
					style={{ fontSize: "0.82rem", marginTop: "0.3rem" }}
				>
					{lastSync
						? t("settings.wizard.restingLastRun", {
								when: formatRelative(lastSync.last_run_at),
							})
						: t("settings.wizard.restingNeverRun")}
				</div>
			</Notice>

			{status && offlinePeers(status).length > 0 && (
				<Notice variant="warn">
					{t("settings.wizard.skippedOffline")}
					<ul style={{ margin: "0.4rem 0 0" }}>
						{offlinePeers(status).map((m) => (
							<li key={m.member_id}>{m.name}</li>
						))}
					</ul>
				</Notice>
			)}

			<div className="fd-row" style={{ gap: "0.6rem", alignItems: "center" }}>
				<span
					className={`ui-badge ${enabled ? "ui-badge-ok" : "ui-badge-warn"}`}
				>
					{t(
						enabled
							? "settings.wizard.autoSyncOn"
							: "settings.wizard.autoSyncOff",
					)}
				</span>
				<button
					type="button"
					className="ui-btn ui-btn-sm"
					onClick={onToggleEnabled}
				>
					{t(
						enabled ? "settings.wizard.pauseBtn" : "settings.wizard.resumeBtn",
					)}
				</button>
			</div>
			<p className="fd-faint" style={{ fontSize: "0.8rem", margin: 0 }}>
				{t(
					enabled
						? "settings.wizard.autoSyncOnHint"
						: "settings.wizard.autoSyncOffHint",
				)}
			</p>

			{status && <UsageSection status={status} members={members} />}

			<div
				className="fd-stack"
				style={{
					marginTop: "0.6rem",
					paddingTop: "0.8rem",
					borderTop: "1px solid var(--border, rgba(128,128,128,0.25))",
				}}
			>
				<h4 style={{ fontSize: "0.95rem", margin: 0 }}>
					{t("settings.wizard.rerunHeading")}
				</h4>
				<Notice variant="warn">{t("settings.wizard.rerunWarning")}</Notice>
				<div>
					<button
						type="button"
						className="ui-btn ui-btn-danger"
						onClick={onRerun}
					>
						{t("settings.wizard.rerunBtn")}
					</button>
				</div>
			</div>
		</div>
	);
}

// UsageSection tells the operator where to send traffic once the fleet is
// converged: the direct /v1 URL, the reverse-proxy forward target, and the
// active-member pool behind the load balancer.
function UsageSection({
	status,
	members,
}: {
	status: FleetStatus;
	members: MemberView[];
}) {
	const { t } = useTranslation();
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
	);
}

// --- Wizard steps -----------------------------------------------------------

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
	changingPrimary,
	blockedReason,
	onProceed,
}: {
	status: FleetStatus;
	overwrites: FleetMemberStatus[];
	busy: boolean;
	changingPrimary: boolean;
	// Non-empty when the selected host cannot be set as primary (re-selecting the
	// current primary). Shows the reason and disables the proceed action.
	blockedReason: string;
	onProceed: () => void;
}) {
	const { t } = useTranslation();
	const blocked = blockedReason !== "";
	return (
		<div>
			<h3 className="fd-step-title">{t("settings.wizard.step3Title")}</h3>
			<p className="fd-faint fd-step-intro">
				{t("settings.wizard.step3Intro")}
			</p>
			<MemberTable status={status} kind="config" />
			{blocked && (
				<Notice variant="warn" style={{ marginTop: "0.7rem" }}>
					{blockedReason}
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

// TokenField is the admin-token input shown in a confirm modal when the action
// re-designates an existing primary (the backend gates that change on the token).
function TokenField({
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
// steps but never skip a gate. onCancel (present only on a re-run) returns to the
// resting screen without committing.
function WizardNav({
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
