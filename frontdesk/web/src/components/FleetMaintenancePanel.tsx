import { useState } from "react";
import { useTranslation } from "react-i18next";
import { api } from "../api/client";
import type { BackupPruneResult } from "../api/types";
import { useToast } from "../context/ToastContext";
import { ConfirmModal } from "./ConfirmModal";

// FleetMaintenancePanel is Settings -> Fleet maintenance: one-off fleet-wide
// housekeeping the operator runs by hand.
//
// Its only action clears the pg_dumps older Front Desk builds asked each member
// to take before overwriting its config. Front Desk no longer creates them and
// member-side rotation never prunes them, so they sit on every member until
// someone removes them. Deleting backups is destructive, so the run is two-step:
// a preview counts what would go, and the confirmation names that number before
// anything is deleted.
export function FleetMaintenancePanel() {
	const { t } = useTranslation();
	const { toast } = useToast();
	// The preview result, non-null exactly while the confirmation is open.
	const [preview, setPreview] = useState<BackupPruneResult | null>(null);
	const [previewing, setPreviewing] = useState(false);
	const [pruning, setPruning] = useState(false);

	const startPrune = async () => {
		setPreviewing(true);
		try {
			setPreview(await api.pruneFrontDeskBackups(true));
		} catch {
			toast(t("settings.maintenance.pruneFailed"), "error");
		} finally {
			setPreviewing(false);
		}
	};

	const confirmPrune = async () => {
		setPruning(true);
		try {
			const res = await api.pruneFrontDeskBackups();
			setPreview(null);
			toast(
				t("settings.maintenance.pruneDone", { count: res.deleted }),
				res.failed > 0 ? "error" : "success",
			);
		} catch {
			toast(t("settings.maintenance.pruneFailed"), "error");
		} finally {
			setPruning(false);
		}
	};

	// Members Front Desk could not read are reported rather than hidden: their
	// backups were not counted, so the preview number is a floor, not a total.
	const unreadable = preview?.results.filter((r) => r.error) ?? [];

	return (
		<section className="ui-card ui-card-pad fd-stack">
			<div>
				<h2 style={{ fontSize: "1rem" }}>{t("settings.maintenance.title")}</h2>
				<p
					className="fd-faint"
					style={{ fontSize: "0.82rem", margin: "0.3rem 0 0" }}
				>
					{t("settings.maintenance.pruneHint")}
				</p>
			</div>

			<div className="fd-row">
				<button
					type="button"
					className="ui-btn"
					data-testid="prune-frontdesk-backups"
					disabled={previewing || pruning}
					onClick={startPrune}
				>
					{previewing
						? t("settings.maintenance.pruneChecking")
						: t("settings.maintenance.pruneBtn")}
				</button>
			</div>

			{preview && (
				<ConfirmModal
					title={t("settings.maintenance.pruneTitle")}
					confirmLabel={t("settings.maintenance.pruneConfirm")}
					busyLabel={t("settings.maintenance.pruning")}
					busy={pruning}
					confirmDisabled={preview.deleted === 0}
					onConfirm={confirmPrune}
					onClose={() => setPreview(null)}
				>
					<p data-testid="prune-preview-count">
						{preview.deleted === 0
							? t("settings.maintenance.pruneNone")
							: t("settings.maintenance.pruneBody", { count: preview.deleted })}
					</p>
					{unreadable.length > 0 && (
						<p className="fd-faint" style={{ fontSize: "0.82rem" }}>
							{t("settings.maintenance.pruneUnreadable", {
								count: unreadable.length,
							})}
						</p>
					)}
				</ConfirmModal>
			)}
		</section>
	);
}
