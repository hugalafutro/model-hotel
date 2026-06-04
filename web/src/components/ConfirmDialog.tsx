import { useTranslation } from "react-i18next";
import { Modal } from "./Modal";

interface ConfirmDialogProps {
	title: string;
	message?: string;
	fields: string[];
	confirmLabel?: string;
	onConfirm: () => void;
	onCancel: () => void;
}

export function ConfirmDialog({
	title,
	message,
	fields,
	confirmLabel,
	onConfirm,
	onCancel,
}: ConfirmDialogProps) {
	const { t } = useTranslation();

	return (
		<Modal
			title={title}
			onClose={onCancel}
			closeOnBackdrop={false}
			maxWidth="max-w-sm"
			zIndex="z-60"
		>
			<p className="text-sm text-gray-300 mb-1">
				{message ?? t("components.confirmDialog.discardChangesTo")}
			</p>
			{fields.length > 0 && (
				<ul className="text-sm text-gray-400 mb-5 list-disc list-inside">
					{fields.map((f) => (
						<li key={f}>{f}</li>
					))}
				</ul>
			)}
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
					className="ui-btn ui-btn-danger"
				>
					{confirmLabel ?? t("common.delete")}
				</button>
			</div>
		</Modal>
	);
}
