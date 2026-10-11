import { useEffect, useId, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { Modal } from "./Modal";

interface DeleteConfirmModalProps {
	/**
	 * What the operator must type, exactly, before the delete button arms: the
	 * entity's saved name, or the translated confirm word
	 * (`components.deleteConfirmModal.confirmWord`) for a batch.
	 */
	confirmText: string;
	/** The entity the default sentence names (displayed verbatim). */
	entityName?: string;
	/** Optional entity type label, e.g. "provider", "failover group" */
	entityType?: string;
	/** Title for the modal. Defaults to "Delete {entityType}" or "Confirm Delete" */
	title?: string;
	/** Replaces the default "Are you sure you want to delete …" sentence. */
	message?: string;
	/** Items the delete covers, listed under the sentence. */
	fields?: string[];
	/** Whether the delete action is in progress */
	isPending: boolean;
	/** Called when user confirms deletion */
	onConfirm: () => void;
	/** Called when user cancels */
	onCancel: () => void;
	/** Stable handle for the danger button in tests. */
	confirmTestId?: string;
}

/**
 * The one dialog behind every irreversible delete. The danger button stays
 * disabled until the operator has typed `confirmText` and nothing else, so a
 * stray click or an Enter on the wrong row cannot delete anything.
 */
export function DeleteConfirmModal({
	confirmText,
	entityName,
	entityType,
	title,
	message,
	fields,
	isPending,
	onConfirm,
	onCancel,
	confirmTestId,
}: DeleteConfirmModalProps) {
	const { t } = useTranslation();
	const inputId = useId();
	const [typed, setTyped] = useState("");
	const inputRef = useRef<HTMLInputElement>(null);
	// Focus from an effect rather than autoFocus: Modal's own focus effect runs
	// after this one (children first) and steps aside when focus is already
	// inside the dialog, and StrictMode's effect replay repeats this one too,
	// where a DOM autoFocus fires only on insertion.
	useEffect(() => {
		inputRef.current?.focus();
	}, []);
	const armed = typed === confirmText;
	const modalTitle =
		title ??
		(entityType
			? t("components.deleteConfirmModal.deleteType", { entityType })
			: t("components.deleteConfirmModal.confirmDelete"));

	return (
		<Modal
			title={modalTitle}
			onClose={onCancel}
			maxWidth="max-w-sm"
			zIndex="z-60"
		>
			<form
				onSubmit={(e) => {
					e.preventDefault();
					if (armed && !isPending) onConfirm();
				}}
			>
				<p className="text-sm text-gray-300 mb-1">
					{message ??
						t("components.deleteConfirmModal.areYouSure", { entityName })}
				</p>
				{fields && fields.length > 0 && (
					<ul className="text-sm text-gray-400 mb-3 list-disc list-inside max-h-60 overflow-y-auto">
						{fields.map((f) => (
							<li key={f}>{f}</li>
						))}
					</ul>
				)}
				<label
					htmlFor={inputId}
					className="block text-sm text-gray-300 mt-3 mb-1"
				>
					{t("components.deleteConfirmModal.typeToConfirm")}{" "}
					<span
						className="font-mono font-semibold text-red-400"
						data-testid="delete-confirm-expected"
					>
						{confirmText}
					</span>
				</label>
				<input
					id={inputId}
					type="text"
					value={typed}
					onChange={(e) => setTyped(e.target.value)}
					ref={inputRef}
					autoComplete="off"
					spellCheck={false}
					className="ui-input mb-4"
					data-testid="delete-confirm-input"
				/>
				<div className="flex gap-3 justify-end">
					<button
						type="button"
						onClick={onCancel}
						className="ui-btn ui-btn-secondary"
					>
						{t("common.cancel")}
					</button>
					<button
						type="submit"
						disabled={isPending || !armed}
						className="ui-btn ui-btn-danger"
						data-testid={confirmTestId}
					>
						{isPending ? t("common.deleting") : t("common.delete")}
					</button>
				</div>
			</form>
		</Modal>
	);
}
