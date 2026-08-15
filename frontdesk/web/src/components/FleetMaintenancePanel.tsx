import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { api } from "../api/client";
import type { BackupPruneResult } from "../api/types";
import { useToast } from "../context/ToastContext";
import { ConfirmModal } from "./ConfirmModal";

// FleetMaintenancePanel is Settings -> Fleet maintenance: one-off fleet-wide
// housekeeping the operator runs by hand.
//
// Its only action deletes the frontdesk-origin pg_dumps a member holds. Nothing
// produces them any more and member-side rotation prunes only the member's own
// scheduled files, so they stay until an operator clears them here. That makes
// the section legacy cleanup: it renders only while such dumps are known to
// exist and retires itself once a run has cleared them, so a fleet that never
// had any never sees it. "Known to exist" comes from the server's count of the
// last listing it read per member (its backup watchdog, or a prune run), so
// opening Settings costs no member calls. Deleting backups is destructive, so
// the run is two-step: a preview counts what would go, and the confirmation
// names that number.
export function FleetMaintenancePanel() {
	const { t } = useTranslation();
	const { toast } = useToast();
	// Whether frontdesk-origin dumps are known to exist. Starts false so nothing
	// flashes in and out while the count is in flight; a failed read is not
	// evidence of dumps and leaves it false.
	const [hasBackups, setHasBackups] = useState(false);
	// The preview result, non-null exactly while the confirmation is open.
	const [preview, setPreview] = useState<BackupPruneResult | null>(null);
	const [previewing, setPreviewing] = useState(false);
	const [pruning, setPruning] = useState(false);

	useEffect(() => {
		let cancelled = false;
		api
			.frontDeskBackupCount()
			.then((res) => {
				if (!cancelled) setHasBackups(res.count > 0);
			})
			.catch(() => {});
		return () => {
			cancelled = true;
		};
	}, []);

	const startPrune = async () => {
		setPreviewing(true);
		try {
			setPreview(await api.previewFrontDeskBackupPrune());
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
			// The section stays for another go while anything may remain: a refused
			// delete certainly does, and a member that could not be read (or had no
			// stored token) was never counted, so its dumps are unknown rather than
			// gone. Only a run that reached every member and was refused nothing
			// retires it. Deliberately stricter than the mount rule, which needs
			// positive evidence to show the section in the first place.
			setHasBackups(res.failed > 0 || res.results.some((r) => r.error));
			toast(
				t("settings.maintenance.pruneDone", { count: res.deleted }),
				res.failed > 0 ? "error" : "success",
			);
		} catch {
			// The run is detached from this request, so a lost response is not a lost
			// run: a fleet with thousands of dumps deletes them one at a time and can
			// outlast the browser's patience. Point at where the outcome lands rather
			// than claiming a failure nobody observed.
			toast(t("settings.maintenance.pruneUnknown"), "error");
		} finally {
			setPruning(false);
		}
	};

	// Members Front Desk could not read are reported rather than hidden: their
	// backups were not counted, so the preview number is a floor, not a total.
	const unreadable = preview?.results.filter((r) => r.error) ?? [];

	if (!hasBackups) return null;

	return (
		<section className="ui-card ui-card-pad fd-stack">
			<div>
				<h2 className="fd-card-title">{t("settings.maintenance.title")}</h2>
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
							{t("settings.maintenance.pruneUnreadable")}
						</p>
					)}
				</ConfirmModal>
			)}
		</section>
	);
}
