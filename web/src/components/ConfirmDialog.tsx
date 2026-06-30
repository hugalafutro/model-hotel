import { useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { Modal, type ModalHandle } from "./Modal";

interface ConfirmDialogProps {
	title: string;
	message?: string;
	fields: string[];
	confirmLabel?: string;
	onConfirm: () => void;
	onCancel: () => void;
	// Optional test id for the confirm (danger) button. Lets callers drive the
	// confirmation in tests without matching the translated button label.
	confirmTestId?: string;
}

export function ConfirmDialog({
	title,
	message,
	fields,
	confirmLabel,
	onConfirm,
	onCancel,
	confirmTestId,
}: ConfirmDialogProps) {
	const { t } = useTranslation();
	const modalRef = useRef<ModalHandle>(null);
	const confirmingRef = useRef(false);
	const [closing, setClosing] = useState(false);

	const handleClose = () => {
		if (confirmingRef.current) {
			onConfirm();
		} else {
			onCancel();
		}
	};

	return (
		<Modal
			ref={modalRef}
			title={title}
			onClose={handleClose}
			closeOnBackdrop={false}
			maxWidth="max-w-sm"
			zIndex="z-60"
		>
			<p className="text-sm text-gray-300 mb-1">
				{message ?? t("components.confirmDialog.discardChangesTo")}
			</p>
			{fields.length > 0 && (
				<ul className="text-sm text-gray-400 mb-5 list-disc list-inside max-h-60 overflow-y-auto">
					{fields.map((f) => (
						<li key={f}>{f}</li>
					))}
				</ul>
			)}
			<div className="flex gap-3 justify-end">
				<button
					type="button"
					onClick={() => {
						setClosing(true);
						modalRef.current?.close();
					}}
					className="ui-btn ui-btn-secondary"
					disabled={closing}
				>
					{t("common.cancel")}
				</button>
				<button
					type="button"
					onClick={() => {
						confirmingRef.current = true;
						setClosing(true);
						modalRef.current?.close();
					}}
					className="ui-btn ui-btn-danger"
					disabled={closing}
					data-testid={confirmTestId}
				>
					{confirmLabel ?? t("common.delete")}
				</button>
			</div>
		</Modal>
	);
}
