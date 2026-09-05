import { useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { AlertTriangle } from "@/lib/icons";
import { api } from "../../api/client";
import { Modal } from "../../components/Modal";
import { useToast } from "../../context/ToastContext";
import type { BackupActions } from "./useBackupActions";

/** The double-confirm modal shown before periodic backup is switched on. */
export function BackupEnableConfirm({
	showEnableConfirm,
	setShowEnableConfirm,
	prunePreview,
	setPrunePreview,
	settingsUpdateMutation,
	managed,
}: {
	showEnableConfirm: BackupActions["showEnableConfirm"];
	setShowEnableConfirm: BackupActions["setShowEnableConfirm"];
	prunePreview: BackupActions["prunePreview"];
	setPrunePreview: BackupActions["setPrunePreview"];
	settingsUpdateMutation: BackupActions["settingsUpdateMutation"];
	/** Fleet-managed card: the dialog stays open but cannot enable or prune. */
	managed: boolean | undefined;
}) {
	const { t } = useTranslation();
	const { toast } = useToast();
	const queryClient = useQueryClient();
	return (
		showEnableConfirm && (
			<Modal
				onClose={() => {
					setShowEnableConfirm(false);
					setPrunePreview(null);
				}}
				title={t("settings.backup.rotation.confirmEnableTitle")}
				maxWidth="max-w-lg"
			>
				<div className="space-y-3">
					<div className="flex items-start gap-2 text-amber-400">
						<AlertTriangle size={18} className="shrink-0 mt-0.5" />
						<p className="text-sm text-(--text-secondary)">
							{t("settings.backup.rotation.confirmEnableDescription")}
						</p>
					</div>
					<div className="space-y-2">
						<p className="text-sm text-(--text-primary)">
							{t("settings.backup.rotation.confirmEnableWouldRemove", {
								count: prunePreview?.prune?.length ?? 0,
							})}
						</p>
						<div className="max-h-40 overflow-y-auto rounded bg-(--surface-elevated) border border-(--border-default) p-2">
							{(prunePreview?.prune ?? []).map((b) => (
								<div
									key={b.filename}
									className="text-xs font-mono text-(--text-secondary) py-0.5"
								>
									{b.filename}
								</div>
							))}
						</div>
					</div>
					<div className="flex justify-end gap-2 pt-2">
						<button
							type="button"
							onClick={() => {
								setShowEnableConfirm(false);
								setPrunePreview(null);
							}}
							className="ui-btn ui-btn-secondary"
						>
							{t("common.cancel")}
						</button>
						<button
							type="button"
							disabled={managed}
							onClick={async () => {
								// The dialog is a portal outside the disabled fieldset, so it
								// checks managed itself: the prune below is a real delete.
								if (managed) return;
								try {
									if ((prunePreview?.prune?.length ?? 0) > 0) {
										await api.backups.prune();
									}
									await settingsUpdateMutation.mutateAsync({
										backup_enabled: "true",
									});
									toast(
										t("settings.backup.rotation.pruneSuccess", {
											count: prunePreview?.prune?.length ?? 0,
										}),
										"success",
									);
								} catch {
									toast(t("settings.backup.rotation.pruneFailed"), "error");
								} finally {
									setShowEnableConfirm(false);
									setPrunePreview(null);
									queryClient.invalidateQueries({
										queryKey: ["backups"],
									});
								}
							}}
							className="ui-btn ui-btn-primary"
						>
							{t("settings.backup.confirm")}
						</button>
					</div>
				</div>
			</Modal>
		)
	);
}
