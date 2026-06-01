import { AlertTriangle } from "lucide-react";
import { useId, useState } from "react";
import { useTranslation } from "react-i18next";
import { Modal } from "./Modal";

interface RestoreConfirmModalProps {
	/** Whether the modal is open */
	open: boolean;
	/** Called when user closes the modal */
	onClose: () => void;
	/** Called when user confirms restore with admin token */
	onConfirm: (adminToken: string) => void;
	/** Whether the restore action is in progress */
	isPending: boolean;
}

export function RestoreConfirmModal({
	open,
	onClose,
	onConfirm,
	isPending,
}: RestoreConfirmModalProps) {
	const { t } = useTranslation();
	const [adminToken, setAdminToken] = useState("");
	const inputId = useId();

	const handleConfirm = () => {
		if (adminToken.trim()) {
			onConfirm(adminToken.trim());
		}
	};

	const handleCancel = () => {
		setAdminToken("");
		onClose();
	};

	if (!open) {
		return null;
	}

	return (
		<Modal
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
					disabled={!adminToken.trim() || isPending}
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
