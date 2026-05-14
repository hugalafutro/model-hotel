import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Download, HardDrive, Plus, Trash2 } from "lucide-react";
import { useState } from "react";
import { api, getAuthHeaders } from "../../api/client";
import { LoadingSpinner } from "../../components/LoadingSpinner";
import { SettingsSection } from "../../components/SettingsSection";
import { useToast } from "../../context/ToastContext";

interface DatabaseBackupSettingsProps {
	collapsed: boolean;
	onToggle: () => void;
}

export function DatabaseBackupSettings({
	collapsed,
	onToggle,
}: DatabaseBackupSettingsProps) {
	const { toast } = useToast();
	const queryClient = useQueryClient();
	const [confirmDelete, setConfirmDelete] = useState<string | null>(null);

	const { data: backups, isLoading } = useQuery({
		queryKey: ["backups"],
		queryFn: () => api.backups.list(),
	});

	const createMutation = useMutation({
		mutationFn: () => api.backups.create(),
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: ["backups"] });
			toast("Backup created", "success");
		},
		onError: (err: Error) => {
			toast(`Backup failed: ${err.message}`, "error");
		},
	});

	const deleteMutation = useMutation({
		mutationFn: (filename: string) => api.backups.delete(filename),
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: ["backups"] });
			setConfirmDelete(null);
			toast("Backup deleted", "success");
		},
		onError: (err: Error) => {
			toast(`Delete failed: ${err.message}`, "error");
		},
	});

	function formatBytes(bytes: number): string {
		if (bytes === 0) return "0 B";
		const k = 1024;
		const sizes = ["B", "KB", "MB", "GB", "TB"];
		const i = Math.min(
			Math.floor(Math.log(bytes) / Math.log(k)),
			sizes.length - 1,
		);
		return `${Number.parseFloat((bytes / k ** i).toFixed(1))} ${sizes[i]}`;
	}

	function formatDate(iso: string): string {
		try {
			return new Date(iso).toLocaleString();
		} catch {
			return iso;
		}
	}

	const downloadBackup = async (filename: string) => {
		try {
			const response = await fetch(api.backups.downloadUrl(filename), {
				headers: getAuthHeaders(),
			});
			if (!response.ok) {
				throw new Error(`Download failed: ${response.status}`);
			}
			const blob = await response.blob();
			const url = URL.createObjectURL(blob);
			const a = document.createElement("a");
			a.href = url;
			a.download = filename;
			document.body.appendChild(a);
			a.click();
			document.body.removeChild(a);
			URL.revokeObjectURL(url);
		} catch (err) {
			toast(`Download failed: ${(err as Error).message}`, "error");
		}
	};

	return (
		<SettingsSection
			icon={HardDrive}
			title="Database Backup"
			collapsed={collapsed}
			onToggle={onToggle}
		>
			<div className="space-y-4">
				<p className="text-gray-400 text-sm">
					Create and download PostgreSQL backups. Uses{" "}
					<code className="text-xs bg-gray-800 px-1 py-0.5 rounded">
						pg_dump
					</code>{" "}
					with custom format for efficient compression.
				</p>

				{/* Restore instructions */}
				<div className="bg-gray-800/50 rounded-lg p-3 space-y-3">
					<h4 className="text-xs font-semibold uppercase tracking-wider text-gray-400">
						Restore Instructions
					</h4>
					<code className="block text-xs text-green-400 bg-black/30 rounded p-2 overflow-x-auto">
						pg_restore --clean --if-exists -d YOUR_DB backup_file.dump
					</code>
					<p className="text-xs text-gray-500">
						Or via Docker:{" "}
						<code className="text-xs text-gray-400">
							docker exec -i postgres-container pg_restore --clean --if-exists
							-U user -d dbname {"<"} backup_file.dump
						</code>
					</p>
					<div className="space-y-1.5 border-t border-gray-700/50 pt-2">
						<h5 className="text-xs font-semibold text-amber-400">
							Requirements for a working restore
						</h5>
						<ul className="text-xs text-gray-400 space-y-1 list-disc list-inside">
							<li>
								<strong className="text-gray-300">MASTER_KEY must match</strong>
								: Provider API keys are AES-256-GCM encrypted using a key
								derived from your MASTER_KEY. Restoring with a different
								MASTER_KEY will leave all provider keys unrecoverable.
							</li>
							<li>
								<strong className="text-gray-300">
									Admin token is not in the backup
								</strong>
								: It is stored on the filesystem in{" "}
								<code className="text-xs bg-gray-800 px-1 py-0.5 rounded">
									DATA_DIR/admin-token
								</code>
								. If lost, a new token is auto-generated on next boot (check
								startup logs).
							</li>
							<li>
								<strong className="text-gray-300">
									Virtual keys are irrecoverable
								</strong>
								: Only SHA-256 hashes are stored. If you lose the plaintext
								virtual keys, they cannot be recovered from the backup.
							</li>
						</ul>
					</div>
				</div>

				{/* Create button */}
				<button
					type="button"
					onClick={() => createMutation.mutate()}
					disabled={createMutation.isPending}
					className="ui-btn ui-btn-primary flex items-center gap-2"
				>
					<Plus size={14} />
					{createMutation.isPending ? "Creating backup…" : "Create Backup"}
				</button>

				{/* Backup list */}
				{isLoading ? (
					<LoadingSpinner />
				) : backups && backups.length > 0 ? (
					<div className="space-y-2">
						<h4 className="text-xs font-semibold uppercase tracking-wider text-gray-500">
							Available Backups ({backups.length})
						</h4>
						{backups.map((backup) => (
							<div
								key={backup.filename}
								className="flex items-center justify-between bg-gray-800/30 rounded-lg p-3"
							>
								<div className="min-w-0 flex-1">
									<p className="text-sm font-medium text-gray-200 truncate">
										{backup.filename}
									</p>
									<p className="text-xs text-gray-500">
										{formatBytes(backup.size_bytes)} -{" "}
										{formatDate(backup.created_at)}
									</p>
								</div>
								<div className="flex items-center gap-2 ml-3 shrink-0">
									{confirmDelete === backup.filename ? (
										<>
											<span className="text-xs text-red-400">Delete?</span>
											<button
												type="button"
												onClick={() => deleteMutation.mutate(backup.filename)}
												disabled={deleteMutation.isPending}
												className="ui-btn ui-btn-danger text-xs px-2 py-1"
											>
												Confirm
											</button>
											<button
												type="button"
												onClick={() => setConfirmDelete(null)}
												className="ui-btn ui-btn-secondary text-xs px-2 py-1"
											>
												Cancel
											</button>
										</>
									) : (
										<>
											<button
												type="button"
												onClick={() => downloadBackup(backup.filename)}
												className="ui-btn ui-btn-secondary text-xs px-2 py-1 flex items-center gap-1"
											>
												<Download size={12} />
												Download
											</button>
											<button
												type="button"
												onClick={() => setConfirmDelete(backup.filename)}
												className="ui-btn ui-btn-danger text-xs px-2 py-1"
												title="Delete backup"
												aria-label="Delete backup"
											>
												<Trash2 size={12} />
											</button>
										</>
									)}
								</div>
							</div>
						))}
					</div>
				) : (
					<p className="text-xs text-gray-500">No backups yet.</p>
				)}
			</div>
		</SettingsSection>
	);
}
