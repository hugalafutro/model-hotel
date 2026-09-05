import { useId, useState } from "react";
import { useTranslation } from "react-i18next";
import { AlertTriangle } from "@/lib/icons";
import { Modal } from "./Modal";

/** Shape of a backup signature sidecar: HMAC-SHA256, hex-encoded. */
const SIGNATURE_PATTERN = /^[0-9a-fA-F]{64}$/;

interface RestoreConfirmModalProps {
	/** Whether the modal is open */
	open: boolean;
	/** Called when user closes the modal */
	onClose: () => void;
	/** Called when user confirms restore with admin token. `signature` is the
	 *  pasted contents of the dump's .sig sidecar, or "" for an unsigned restore
	 *  the user has explicitly accepted. */
	onConfirm: (adminToken: string, signature: string) => void;
	/** Whether the restore action is in progress */
	isPending: boolean;
	/** Fleet-managed card: the dialog stays open but cannot restore. */
	managed?: boolean;
}

export function RestoreConfirmModal({
	open,
	onClose,
	onConfirm,
	isPending,
	managed = false,
}: RestoreConfirmModalProps) {
	const { t } = useTranslation();
	const [adminToken, setAdminToken] = useState("");
	const [signature, setSignature] = useState("");
	// An unsigned dump cannot be integrity-checked (the pre-restore inspection
	// sees object types, not rows), so restoring one is a second, separate
	// decision rather than the default outcome of leaving a field blank.
	const [confirmingUnsigned, setConfirmingUnsigned] = useState(false);
	const inputId = useId();
	const signatureId = useId();
	const signatureHelpId = useId();

	// A sidecar is one HMAC-SHA256 in hex, so anything else is a bad paste;
	// catching it here spares the operator uploading a whole dump only to have
	// the server refuse the signature after the fact.
	const sig = signature.trim();
	const signatureMalformed = sig !== "" && !SIGNATURE_PATTERN.test(sig);

	// Only reachable from an enabled button, which the render below already
	// gates on a non-empty token and a well-formed (or empty) signature.
	const handleConfirm = () => {
		const token = adminToken.trim();
		if (sig) {
			onConfirm(token, sig);
			return;
		}
		if (confirmingUnsigned) {
			onConfirm(token, "");
			return;
		}
		setConfirmingUnsigned(true);
	};

	const handleCancel = () => {
		setAdminToken("");
		setSignature("");
		setConfirmingUnsigned(false);
		onClose();
	};

	if (!open) {
		return null;
	}

	// Distinct keys: the two stages must be different Modal instances so the
	// switch remounts the dialog and moves focus into it, rather than leaving
	// focus on a button that no longer exists.
	if (confirmingUnsigned) {
		return (
			<Modal
				key="unsigned"
				title={t("components.restoreConfirmModal.unsignedTitle")}
				onClose={handleCancel}
				maxWidth="max-w-lg"
				scrollable={true}
			>
				<div className="mb-4">
					<div className="flex items-center gap-2 mb-3 text-amber-400">
						<AlertTriangle size={24} />
						<span className="text-lg font-bold">
							{t("components.restoreConfirmModal.unsignedHeadline")}
						</span>
					</div>
					<p className="text-sm text-gray-300">
						{t("components.restoreConfirmModal.unsignedWarning")}
					</p>
				</div>

				<div className="flex gap-3 justify-end">
					<button
						type="button"
						onClick={() => setConfirmingUnsigned(false)}
						disabled={isPending}
						className="ui-btn ui-btn-secondary"
					>
						{t("components.restoreConfirmModal.back")}
					</button>
					<button
						type="button"
						onClick={handleConfirm}
						disabled={isPending}
						className="ui-btn ui-btn-danger"
					>
						{isPending
							? t("components.restoreConfirmModal.restoring")
							: t("components.restoreConfirmModal.restoreAnyway")}
					</button>
				</div>
			</Modal>
		);
	}

	return (
		<Modal
			key="form"
			title={t("components.restoreConfirmModal.title")}
			onClose={handleCancel}
			maxWidth="max-w-lg"
			scrollable={true}
		>
			<div className="mb-4">
				<div className="flex items-center gap-2 mb-3 text-red-400">
					<AlertTriangle size={24} />
					<span className="text-lg font-bold">
						{t("components.restoreConfirmModal.overwriteWarning")}
					</span>
				</div>
				<p className="text-amber-400 text-sm mb-4">
					{t("components.restoreConfirmModal.destroyWarning")}
				</p>
			</div>

			<div className="ui-card bg-gray-800/50 p-4 mb-4">
				<h3 className="text-(--text-primary) font-semibold mb-3">
					{t("components.restoreConfirmModal.requirements")}
				</h3>
				<ul className="space-y-2 text-sm text-gray-300">
					<li className="flex gap-2">
						<span className="text-amber-400">•</span>
						<span>
							<strong className="text-(--text-primary)">
								{t("components.restoreConfirmModal.masterKeyMatch")}
							</strong>{" "}
							{t("components.restoreConfirmModal.masterKeyInfo")}
						</span>
					</li>
					<li className="flex gap-2">
						<span className="text-amber-400">•</span>
						<span>
							<strong className="text-(--text-primary)">
								{t("components.restoreConfirmModal.adminTokenNotInBackup")}
							</strong>{" "}
							{t("components.restoreConfirmModal.adminTokenInfo")}
						</span>
					</li>
					<li className="flex gap-2">
						<span className="text-amber-400">•</span>
						<span>
							<strong className="text-(--text-primary)">
								{t("components.restoreConfirmModal.virtualKeysIrrecoverable")}
							</strong>{" "}
							{t("components.restoreConfirmModal.virtualKeysInfo")}
						</span>
					</li>
				</ul>
			</div>

			<div className="mb-4">
				<label
					htmlFor={signatureId}
					className="block text-sm font-medium text-gray-300 mb-1"
				>
					{t("components.restoreConfirmModal.signatureLabel")}
				</label>
				<textarea
					id={signatureId}
					value={signature}
					onChange={(e) => setSignature(e.target.value)}
					rows={2}
					spellCheck={false}
					aria-describedby={signatureHelpId}
					aria-invalid={signatureMalformed || undefined}
					className="w-full px-3 py-2 bg-gray-900 border border-gray-600 rounded text-(--text-primary) placeholder-gray-400 focus:outline-none focus:border-amber-500 font-mono text-xs"
					placeholder={t("components.restoreConfirmModal.signaturePlaceholder")}
					disabled={isPending}
				/>
				{signatureMalformed && (
					<p role="alert" className="text-xs text-red-400 mt-1">
						{t("components.restoreConfirmModal.signatureInvalid")}
					</p>
				)}
				<p id={signatureHelpId} className="text-xs text-gray-400 mt-1">
					{t("components.restoreConfirmModal.signatureHelp")}
				</p>
			</div>

			<div className="mb-4">
				<label
					htmlFor={inputId}
					className="block text-sm font-medium text-gray-300 mb-1"
				>
					{t("components.restoreConfirmModal.confirmWithAdminToken")}
				</label>
				<input
					id={inputId}
					type="password"
					value={adminToken}
					onChange={(e) => setAdminToken(e.target.value)}
					className="w-full px-3 py-2 bg-gray-900 border border-gray-600 rounded text-(--text-primary) placeholder-gray-400 focus:outline-none focus:border-amber-500"
					placeholder={t("components.restoreConfirmModal.enterAdminToken")}
					disabled={isPending}
				/>
			</div>

			<div className="flex gap-3 justify-end">
				<button
					type="button"
					onClick={handleCancel}
					disabled={isPending}
					className="ui-btn ui-btn-secondary"
				>
					{t("common.cancel")}
				</button>
				<button
					type="button"
					onClick={handleConfirm}
					disabled={
						!adminToken.trim() || signatureMalformed || isPending || managed
					}
					className="ui-btn ui-btn-danger"
				>
					{isPending
						? t("components.restoreConfirmModal.restoring")
						: t("components.restoreConfirmModal.restoreDatabase")}
				</button>
			</div>
		</Modal>
	);
}
