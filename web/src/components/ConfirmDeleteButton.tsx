import { useState } from "react";
import { useTranslation } from "react-i18next";

interface ConfirmDeleteButtonProps {
	/** Label for the initial delete trigger button */
	label?: string;
	/** Label for the confirm button. Default: "Yes, delete" */
	confirmLabel?: string;
	/** Called when the user confirms deletion */
	onConfirm: () => void;
	/** Whether the delete action is in progress */
	loading?: boolean;
	/** Additional className for the container */
	className?: string;
}

export function ConfirmDeleteButton({
	label,
	confirmLabel,
	onConfirm,
	loading = false,
	className = "",
}: ConfirmDeleteButtonProps) {
	const { t } = useTranslation();
	const [confirming, setConfirming] = useState(false);

	if (!confirming) {
		return (
			<button
				type="button"
				onClick={() => setConfirming(true)}
				className={`ui-btn ui-btn-danger ${className}`}
			>
				{label ?? t("components.confirmDeleteButton.deleteKey")}
			</button>
		);
	}

	return (
		<div className={`flex items-center gap-2 ${className}`}>
			<span className="text-xs text-red-400">
				{t("components.confirmDeleteButton.areYouSure")}
			</span>
			<button
				type="button"
				onClick={onConfirm}
				disabled={loading}
				className="ui-btn ui-btn-danger disabled:opacity-50"
			>
				{loading
					? t("common.deleting")
					: (confirmLabel ?? t("components.confirmDeleteButton.yesDelete"))}
			</button>
			<button
				type="button"
				onClick={() => setConfirming(false)}
				className="ui-btn ui-btn-secondary"
			>
				{t("common.cancel")}
			</button>
		</div>
	);
}
