import { useCallback, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { ApiError, api } from "../../api/client";
import type {
	AutoSyncConfig,
	FleetStatus,
	FleetSyncState,
	FleetVersionCheck,
	MemberView,
} from "../../api/types";
import { useToast } from "../../context/ToastContext";
import { reportResults } from "../../utils/syncResults";
import { ConfirmModal } from "../ConfirmModal";
import { Notice } from "../Notice";
import {
	configChanges,
	masterKeyBlockers,
	type Step,
	schemaBlockers,
} from "./gates";
import { StepResting } from "./resting";
import {
	ConfigLegend,
	StepChoosePrimary,
	StepConfig,
	StepMasterKey,
	TokenField,
	WizardNav,
} from "./steps";

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

type View = "loading" | "wizard" | "resting";
type ConfirmKind = "config" | "change" | null;

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
	// Version-alignment gate on the sync step. `skew` holds the latest check
	// result (null until one lands: proceed stays blocked, failing closed);
	// `devAck` opens the all-dev-fleet acknowledgment modal.
	const [skew, setSkew] = useState<FleetVersionCheck | null>(null);
	const [checkingSkew, setCheckingSkew] = useState(false);
	const [devAck, setDevAck] = useState(false);

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

	// Re-poll the fleet's versions and compare against the primary. The endpoint
	// probes members on demand, so an operator who just aligned a member sees
	// the block clear on Refresh instead of waiting for the poll interval.
	const runSkewCheck = useCallback(
		async (id: string) => {
			if (!id) return;
			setCheckingSkew(true);
			// A stale result must not vouch for the fleet while the fresh check is
			// in flight (the primary may have changed since it landed).
			setSkew(null);
			try {
				setSkew(await api.fleetVersionCheck(id));
			} catch (e) {
				// No result keeps the sync action blocked (fail closed).
				setSkew(null);
				toast(e instanceof ApiError ? e.message : t("errors.generic"), "error");
			} finally {
				setCheckingSkew(false);
			}
		},
		[toast, t],
	);

	const hasSkew = (skew?.skewed.length ?? 0) > 0;
	// The whole aligned fleet reports the "dev" placeholder version and no commit
	// vouched for the alignment: string equality cannot speak for the actual
	// builds, so syncing needs an explicit operator acknowledgment (wizard-only;
	// autosync never prompts). A dev fleet whose members all reported a commit
	// was compared build to build, so it is aligned in fact and prompts nothing.
	const isDevFleet =
		!hasSkew && skew?.primary_version === "dev" && !skew?.commit_vouched;

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
		if (!unlocked(s)) return;
		setStep(s);
		// Entering the sync step re-checks version alignment: versions may have
		// moved since the wizard was opened, and a stale pass would defeat the gate.
		if (s === 3) void runSkewCheck(primaryId);
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
			// operator can retry. Coded 409s route on their machine code
			// (fleet_too_small: the two-member floor); an uncoded 409 is the
			// same-host guard: the selected host is already the primary, reached
			// under a different address.
			setCommitError(
				e instanceof ApiError && e.code === "fleet_too_small"
					? t("settings.wizard.fleetTooSmallError")
					: e instanceof ApiError && e.status === 409
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
		// Version gate: no successful check, a check in flight, or a skewed member
		// all block the sync. The proceed button is disabled too; StepConfig shows
		// the skewed members and the Refresh action.
		if (hasSkew || checkingSkew || !skew) return;
		if (isDevFleet && !devAck) {
			setDevAck(true); // open the dev-fleet acknowledgment modal
			return;
		}
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
				<h2 className="fd-card-title">{t("settings.wizard.restingTitle")}</h2>
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
			<h2 className="fd-card-title">{t("settings.wizard.title")}</h2>
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
					skew={skew}
					checkingSkew={checkingSkew}
					onRefreshSkew={() => void runSkewCheck(primaryId)}
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

			{devAck && (
				<ConfirmModal
					title={t("settings.wizard.devAckTitle")}
					confirmLabel={t("settings.wizard.devAckConfirm")}
					onConfirm={() => {
						setDevAck(false);
						// Past the dev gate: re-enter the normal confirm/commit path.
						setCommitToken("");
						setCommitError("");
						if (overwrites.length > 0) setConfirm("config");
						else if (changingPrimary) setConfirm("change");
						else void commit();
					}}
					onClose={() => setDevAck(false)}
				>
					<p className="fd-muted" data-testid="dev-sync-ack-modal">
						{t("settings.wizard.devAckBody")}
					</p>
				</ConfirmModal>
			)}
		</div>
	);
}

// --- Resting screen ---------------------------------------------------------
