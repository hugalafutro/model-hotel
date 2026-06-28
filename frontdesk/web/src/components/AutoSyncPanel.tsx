import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { ApiError, api } from "../api/client";
import type { AutoSyncConfig, MemberView } from "../api/types";
import { useToast } from "../context/ToastContext";
import { ConfirmModal } from "./ConfirmModal";
import { Notice } from "./Notice";

// AutoSyncPanel is the "set and forget" control for HA config replication: pick a
// primary and flip auto-sync on, and Front Desk's poller propagates the primary's
// config to the rest of the fleet whenever it changes, snapshotting each member
// first. It sits beside the manual wizard, which stays for first-time setup and
// for forcing a sync on demand.
//
// Changes persist immediately (no separate Save button) so the toggle behaves
// like a switch. Enabling is only offered once a primary with a stored admin
// token is chosen, because the loop needs that token to read the primary's
// config; the backend enforces the same rule.
export function AutoSyncPanel({ members }: { members: MemberView[] }) {
	const { t } = useTranslation();
	const { toast } = useToast();
	const [cfg, setCfg] = useState<AutoSyncConfig | null>(null);
	const [loadError, setLoadError] = useState(false);
	const [saving, setSaving] = useState(false);
	const [saveError, setSaveError] = useState("");
	// Candidate primary awaiting admin-token confirmation (null = no prompt). "" is
	// a real candidate meaning "clear the primary", so it must stay distinct.
	const [pendingPrimary, setPendingPrimary] = useState<string | null>(null);
	const [confirmToken, setConfirmToken] = useState("");
	const [confirmError, setConfirmError] = useState("");

	useEffect(() => {
		api
			.getAutoSync()
			.then(setCfg)
			.catch(() => setLoadError(true));
	}, []);

	if (loadError || !cfg) return null; // stay quiet on load; the wizard still works

	const errorMessage = (err: unknown): string =>
		err instanceof ApiError && (err.status === 400 || err.status === 403)
			? err.message
			: t("errors.generic");

	const persist = async (next: AutoSyncConfig, confirm?: string) => {
		setSaveError("");
		setSaving(true);
		try {
			setCfg(await api.putAutoSync(next, confirm));
			toast(t("settings.autoSync.saved"), "success");
			return true;
		} catch (err) {
			// A primary was configured concurrently (another admin), so the server
			// now gates this change even though our snapshot showed none set. Recover
			// by prompting for the admin token instead of dead-ending on an error.
			// Never do this while a confirmation is already pending: that would
			// overwrite the operator's chosen primary with this save's value.
			if (
				err instanceof ApiError &&
				err.status === 403 &&
				confirm === undefined &&
				pendingPrimary === null
			) {
				setConfirmToken("");
				setConfirmError("");
				setPendingPrimary(next.primary_id);
				return false;
			}
			setSaveError(errorMessage(err));
			return false;
		} finally {
			setSaving(false);
		}
	};

	// Repointing or clearing a primary that is already set is high-impact, so route
	// it through an admin-token confirmation. The first selection (none set yet)
	// applies immediately.
	const onSelectPrimary = (value: string) => {
		if (cfg.primary_id !== "" && value !== cfg.primary_id) {
			// Drop any stale persist error so it doesn't linger beneath the modal.
			setSaveError("");
			setConfirmToken("");
			setConfirmError("");
			setPendingPrimary(value);
			return;
		}
		// First selection (none set yet). If our snapshot is stale because a primary
		// was set concurrently, the server gates this PUT with a 403 and persist()
		// opens the confirmation modal to recover, so we never dead-end.
		void persist({ ...cfg, primary_id: value });
	};

	const closeConfirm = () => {
		setPendingPrimary(null);
		setConfirmToken("");
		setConfirmError("");
	};

	const confirmPrimaryChange = async () => {
		if (pendingPrimary === null) return;
		setConfirmError("");
		setSaving(true);
		try {
			// Only the primary changes here. The server preserves the stored enabled
			// flag on a repoint (see SetAutoSyncGuarded), so a concurrent enable/
			// disable is never reverted by this write, and we adopt whatever enabled
			// state the server returns.
			const saved = await api.putAutoSync(
				{ ...cfg, primary_id: pendingPrimary },
				confirmToken.trim(),
			);
			setCfg(saved);
			toast(t("settings.autoSync.saved"), "success");
			closeConfirm();
		} catch (err) {
			setConfirmError(errorMessage(err));
		} finally {
			setSaving(false);
		}
	};

	const tokenedMembers = members.filter((m) => m.has_token);
	// While a primary-change confirmation is open, freeze the other controls: an
	// unrelated save (e.g. toggling enabled) could otherwise fire, hit the gate,
	// and clobber the operator's pending choice.
	const confirming = pendingPrimary !== null;

	return (
		<div className="ui-card ui-card-pad fd-stack">
			<h2 style={{ fontSize: "1rem" }}>{t("settings.autoSync.title")}</h2>
			<p
				className="fd-faint"
				style={{ fontSize: "0.82rem", margin: "0.3rem 0 0.6rem" }}
			>
				{t("settings.autoSync.hint")}
			</p>

			<div className="ui-field">
				<label className="ui-label" htmlFor="autosync-primary">
					{t("settings.autoSync.primaryLabel")}
				</label>
				<select
					id="autosync-primary"
					className="ui-input"
					value={cfg.primary_id}
					disabled={saving || confirming}
					onChange={(e) => onSelectPrimary(e.target.value)}
				>
					<option value="">{t("settings.autoSync.primaryNone")}</option>
					{tokenedMembers.map((m) => (
						<option key={m.id} value={m.id}>
							{m.name}
						</option>
					))}
				</select>
				<div
					className="fd-faint"
					style={{ fontSize: "0.78rem", marginTop: "0.3rem" }}
				>
					{t("settings.autoSync.primaryHint")}
				</div>
			</div>

			<label className="fd-row" style={{ cursor: "pointer" }}>
				<input
					type="checkbox"
					checked={cfg.enabled}
					disabled={saving || !cfg.primary_id || confirming}
					onChange={(e) => persist({ ...cfg, enabled: e.target.checked })}
				/>
				<span>
					<span style={{ fontWeight: 500 }}>
						{t("settings.autoSync.enableLabel")}
					</span>
					<span
						className="fd-faint"
						style={{ display: "block", fontSize: "0.78rem" }}
					>
						{t("settings.autoSync.enableHint")}
					</span>
				</span>
			</label>

			{cfg.enabled && (
				<Notice variant="warn">{t("settings.autoSync.activeWarning")}</Notice>
			)}
			{saveError && (
				<div className="fd-error-text" role="alert">
					{saveError}
				</div>
			)}

			{pendingPrimary !== null && (
				<ConfirmModal
					title={t("settings.autoSync.confirmPrimaryTitle")}
					confirmLabel={t("settings.autoSync.confirmPrimaryAction")}
					confirmDisabled={!confirmToken.trim()}
					busy={saving}
					onConfirm={() => void confirmPrimaryChange()}
					onClose={closeConfirm}
				>
					<p style={{ marginTop: 0 }}>
						{pendingPrimary === ""
							? t("settings.autoSync.confirmPrimaryClearBody")
							: t("settings.autoSync.confirmPrimaryChangeBody")}
					</p>
					<div className="ui-field">
						<label className="ui-label" htmlFor="fd-autosync-confirm-token">
							{t("settings.autoSync.confirmTokenLabel")}
						</label>
						<input
							id="fd-autosync-confirm-token"
							className="ui-input"
							type="password"
							autoComplete="current-password"
							value={confirmToken}
							onChange={(e) => setConfirmToken(e.target.value)}
							placeholder={t("settings.autoSync.confirmTokenPlaceholder")}
						/>
					</div>
					{confirmError && (
						<div className="fd-error-text" role="alert">
							{confirmError}
						</div>
					)}
				</ConfirmModal>
			)}
		</div>
	);
}
