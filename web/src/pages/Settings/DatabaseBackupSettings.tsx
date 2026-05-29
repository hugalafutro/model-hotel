import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Download, HardDrive, Plus, Trash2, Upload } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { api, getAuthHeaders } from "../../api/client";
import { LoadingSpinner } from "../../components/LoadingSpinner";
import { RestoreConfirmModal } from "../../components/RestoreConfirmModal";
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
	const [restoreFile, setRestoreFile] = useState<File | null>(null);
	const [showRestoreModal, setShowRestoreModal] = useState(false);
	const [isRestoring, setIsRestoring] = useState(false);
	const fileInputRef = useRef<HTMLInputElement>(null);
	const pollingRef = useRef(false);

	useEffect(() => {
		return () => {
			pollingRef.current = false;
		};
	}, []);

	const { data: backups, isLoading } = useQuery({
		queryKey: ["backups"],
		queryFn: () => api.backups.list(),
	});

	const createMutation = useMutation({
		mutationFn: () => api.backups.create(),
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: ["backups"] });
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

				{/* Restore requirements */}
				<div className="bg-gray-800/50 rounded-lg p-3 space-y-2">
					<h4 className="text-xs font-semibold uppercase tracking-wider text-gray-400">
						Restore Requirements
					</h4>
					<ul className="text-xs text-gray-400 space-y-1 list-disc list-inside">
						<li>
							<strong className="text-gray-300">MASTER_KEY must match</strong>:
							Provider API keys are AES-256-GCM encrypted. Restoring with a
							different MASTER_KEY will leave all provider keys unrecoverable.
						</li>
						<li>
							<strong className="text-gray-300">
								Admin token is not in the backup
							</strong>
							: Your current admin token will continue to work after restore.
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

				{showRestoreModal && restoreFile && (
					<RestoreConfirmModal
						open={showRestoreModal}
						onClose={() => {
							setShowRestoreModal(false);
							setRestoreFile(null);
						}}
						onConfirm={async (adminToken) => {
							setIsRestoring(true);
							try {
								await api.backups.restore(restoreFile, adminToken);
								toast(
									"Database restored. The server is restarting…",
									"success",
								);
								// Poll for server to come back
								setShowRestoreModal(false);
								setRestoreFile(null);
								pollingRef.current = true;
								const checkServer = async () => {
									let attempts = 0;
									while (pollingRef.current && attempts < 60) {
										try {
											const res = await fetch("/api/backups", {
												headers: getAuthHeaders(),
											});
											if (res.ok) {
												queryClient.invalidateQueries({
													queryKey: ["backups"],
												});
												toast("Server is back online", "success");
												return;
											}
										} catch {
											// Server not up yet
										}
										await new Promise((r) => setTimeout(r, 2000));
										attempts++;
									}
									if (pollingRef.current) {
										toast(
											"Server is taking longer than expected to restart",
											"warning",
										);
									}
								};
								checkServer();
							} catch (err) {
								toast(`Restore failed: ${(err as Error).message}`, "error");
							} finally {
								setIsRestoring(false);
							}
						}}
						isPending={isRestoring}
					/>
				)}

				{/* Action buttons row */}
				<div className="flex items-center justify-between">
					<button
						type="button"
						onClick={() => createMutation.mutate()}
						disabled={createMutation.isPending}
						className="ui-btn ui-btn-primary flex items-center gap-2"
					>
						<Plus size={14} />
						{createMutation.isPending ? "Creating backup…" : "Create Backup"}
					</button>
					<div className="flex items-center gap-2">
						<input
							ref={fileInputRef}
							type="file"
							accept=".dump"
							className="hidden"
							aria-label="Select backup file to restore"
							onChange={(e) => {
								const file = e.target.files?.[0];
								if (file) {
									setRestoreFile(file);
									setShowRestoreModal(true);
								}
								// Reset so re-selecting the same file triggers onChange
								e.target.value = "";
							}}
						/>
						<button
							type="button"
							onClick={() => fileInputRef.current?.click()}
							disabled={isRestoring}
							className="ui-btn ui-btn-secondary flex items-center gap-2"
						>
							<Upload size={14} />
							{isRestoring ? "Restoring…" : "Upload & Restore"}
						</button>
					</div>
				</div>

				{/* Backup list */}
				{isLoading ? (
					<LoadingSpinner />
				) : backups && backups.length > 0 ? (
					<div className="space-y-2 max-h-[300px] overflow-y-auto">
						<h4 className="text-xs font-semibold uppercase tracking-wider text-gray-500 sticky top-0 bg-(--surface-elevated) py-1">
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
