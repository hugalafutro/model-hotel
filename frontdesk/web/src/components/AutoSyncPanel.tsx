import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { ApiError, api } from "../api/client";
import type { AutoSyncConfig, MemberView } from "../api/types";
import { useToast } from "../context/ToastContext";
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

	useEffect(() => {
		api
			.getAutoSync()
			.then(setCfg)
			.catch(() => setLoadError(true));
	}, []);

	if (loadError || !cfg) return null; // stay quiet on load; the wizard still works

	const persist = async (next: AutoSyncConfig) => {
		setSaveError("");
		setSaving(true);
		try {
			setCfg(await api.putAutoSync(next));
			toast(t("settings.autoSync.saved"), "success");
		} catch (err) {
			setSaveError(
				err instanceof ApiError && err.status === 400
					? err.message
					: t("errors.generic"),
			);
		} finally {
			setSaving(false);
		}
	};

	const tokenedMembers = members.filter((m) => m.has_token);

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
					disabled={saving}
					onChange={(e) => persist({ ...cfg, primary_id: e.target.value })}
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
					disabled={saving || !cfg.primary_id}
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
		</div>
	);
}
