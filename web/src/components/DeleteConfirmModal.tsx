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
	const modalTitle =
		title ?? (entityType ? `Delete ${entityType}` : "Confirm Delete");

	return (
		<Modal title={modalTitle} onClose={onCancel} maxWidth="max-w-sm">
			<p className="text-sm text-gray-300 mb-4">
				Are you sure you want to delete{" "}
				<span className="text-white font-medium">{entityName}</span>? This
				action cannot be undone.
			</p>
			<div className="flex gap-3 justify-end">
				<button
					type="button"
					onClick={onCancel}
					className="ui-btn ui-btn-secondary"
				>
					Cancel
				</button>
				<button
					type="button"
					onClick={onConfirm}
					disabled={isPending}
					className="ui-btn ui-btn-danger"
				>
					{isPending ? "Deleting…" : "Delete"}
				</button>
			</div>
		</Modal>
	);
}
