import { AlertTriangle } from "lucide-react";
import { useId, useState } from "react";
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
			title="Restore Database Backup"
			onClose={handleCancel}
			maxWidth="max-w-lg"
			scrollable={true}
		>
			<div className="mb-4">
				<div className="flex items-center gap-2 mb-3 text-red-400">
					<AlertTriangle size={24} />
					<span className="text-lg font-bold">
						This will permanently overwrite all data
					</span>
				</div>
				<p className="text-amber-400 text-sm mb-4">
					This will permanently destroy all current data and replace it with the
					backup. This action cannot be undone.
				</p>
			</div>

			<div className="ui-card bg-gray-800/50 p-4 mb-4">
				<h3 className="text-white font-semibold mb-3">Requirements:</h3>
				<ul className="space-y-2 text-sm text-gray-300">
					<li className="flex gap-2">
						<span className="text-amber-400">•</span>
						<span>
							<strong className="text-white">MASTER_KEY must match:</strong>{" "}
							Provider API keys are AES-256-GCM encrypted using a key derived
							from your MASTER_KEY. Restoring with a different MASTER_KEY will
							leave all provider keys unrecoverable.
						</span>
					</li>
					<li className="flex gap-2">
						<span className="text-amber-400">•</span>
						<span>
							<strong className="text-white">
								Admin token is not in the backup:
							</strong>{" "}
							It is stored on the filesystem. Your current admin token will
							continue to work after restore.
						</span>
					</li>
					<li className="flex gap-2">
						<span className="text-amber-400">•</span>
						<span>
							<strong className="text-white">
								Virtual keys are irrecoverable:
							</strong>{" "}
							Only SHA-256 hashes are stored. If you lose the plaintext virtual
							keys, they cannot be recovered.
						</span>
					</li>
				</ul>
			</div>

			<div className="mb-4">
				<label
					htmlFor={inputId}
					className="block text-sm font-medium text-gray-300 mb-1"
				>
					Confirm with admin token
				</label>
				<input
					id={inputId}
					type="password"
					value={adminToken}
					onChange={(e) => setAdminToken(e.target.value)}
					className="w-full px-3 py-2 bg-gray-900 border border-gray-600 rounded text-white placeholder-gray-400 focus:outline-none focus:border-amber-500"
					placeholder="Enter admin token"
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
					Cancel
				</button>
				<button
					type="button"
					onClick={handleConfirm}
					disabled={!adminToken.trim() || isPending}
					className="ui-btn ui-btn-danger"
				>
					{isPending ? "Restoring…" : "Restore Database"}
				</button>
			</div>
		</Modal>
	);
}
