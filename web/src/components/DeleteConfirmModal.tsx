import { useTranslation } from "react-i18next";
import { Modal } from "./Modal";

interface DeleteConfirmModalProps {
	/** The name/identifier of the entity being deleted (displayed in bold) */
	entityName: string;
	/** Optional entity type label, e.g. "provider", "failover group" */
	entityType?: string;
	/** Title for the modal. Defaults to "Delete {entityType}" or "Confirm Delete" */
	title?: string;
	/** Whether the delete action is in progress */
	isPending: boolean;
	/** Called when user confirms deletion */
	onConfirm: () => void;
	/** Called when user cancels */
	onCancel: () => void;
}

export function DeleteConfirmModal({
	entityName,
	entityType,
	title,
	isPending,
	onConfirm,
	onCancel,
}: DeleteConfirmModalProps) {
	const { t } = useTranslation();
	const modalTitle =
		title ??
		(entityType
			? t("components.deleteConfirmModal.deleteType", { entityType })
			: t("components.deleteConfirmModal.confirmDelete"));

	return (
		<Modal title={modalTitle} onClose={onCancel} maxWidth="max-w-sm">
			<p className="text-sm text-gray-300 mb-4">
				{t("components.deleteConfirmModal.areYouSure", { entityName })}
			</p>
			<div className="flex gap-3 justify-end">
				<button
					type="button"
					onClick={onCancel}
					className="ui-btn ui-btn-secondary"
				>
					{t("common.cancel")}
				</button>
				<button
					type="button"
					onClick={onConfirm}
					disabled={isPending}
					className="ui-btn ui-btn-danger"
				>
					{isPending ? t("common.deleting") : t("common.delete")}
				</button>
			</div>
		</Modal>
	);
}
