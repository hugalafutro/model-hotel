import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
	AlertTriangle,
	Download,
	HardDrive,
	Plus,
	Trash2,
	Upload,
} from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { api, getAuthHeaders } from "../../api/client";
import type { BackupClassification } from "../../api/types";
import { LoadingSpinner } from "../../components/LoadingSpinner";
import { Modal } from "../../components/Modal";
import { RestoreConfirmModal } from "../../components/RestoreConfirmModal";
import { SettingsSection } from "../../components/SettingsSection";
import { SettingsSlider } from "../../components/SettingsSlider";
import { Spinner } from "../../components/Spinner";
import { Toggle } from "../../components/Toggle";
import { useToast } from "../../context/ToastContext";
import { formatDateTimeShort } from "../../utils/format";

interface DatabaseBackupSettingsProps {
	collapsed: boolean;
	onToggle: () => void;
}

export function DatabaseBackupSettings({
	collapsed,
	onToggle,
}: DatabaseBackupSettingsProps) {
	const { t } = useTranslation();
	const { toast } = useToast();
	const queryClient = useQueryClient();
	const [confirmDelete, setConfirmDelete] = useState<string | null>(null);
	const [restoreFile, setRestoreFile] = useState<File | null>(null);
	const [showRestoreModal, setShowRestoreModal] = useState(false);
	const [isRestoring, setIsRestoring] = useState(false);
	const [showEnableConfirm, setShowEnableConfirm] = useState(false);
	const [prunePreview, setPrunePreview] = useState<BackupClassification | null>(
		null,
	);
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
			toast(
				t("settings.backup.backupFailed", { message: err.message }),
				"error",
			);
		},
	});

	const deleteMutation = useMutation({
		mutationFn: (filename: string) => api.backups.delete(filename),
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: ["backups"] });
			setConfirmDelete(null);
			toast(t("settings.backup.backupDeleted"), "success");
		},
		onError: (err: Error) => {
			toast(
				t("settings.backup.deleteFailed", { message: err.message }),
				"error",
			);
		},
	});

	// Settings for periodic backup
	const { data: settings } = useQuery({
		queryKey: ["settings"],
		queryFn: () => api.settings.get(),
	});

	const settingsUpdateMutation = useMutation({
		mutationFn: (updates: Record<string, string>) =>
			api.settings.update(updates),
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: ["settings"] });
		},
		onError: (err: Error) => {
			toast(
				t("settings.common.failedToSave", { message: err.message }),
				"error",
			);
		},
	});

	const backupEnabled = settings?.backup_enabled === "true";
	const backupInterval = Number(settings?.backup_interval || "86400");
	const sonRetention = Number(settings?.backup_son_retention || "7");
	const fatherRetention = Number(settings?.backup_father_retention || "4");
	const grandfatherRetention = Number(
		settings?.backup_grandfather_retention || "3",
	);

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
			toast(
				t("settings.backup.downloadFailed", {
					message: (err as Error).message,
				}),
				"error",
			);
		}
	};

	return (
		<SettingsSection
			icon={HardDrive}
			title={t("settings.backup.title")}
			collapsed={collapsed}
			onToggle={onToggle}
		>
			<div className="space-y-4">
				<p className="text-(--text-secondary) text-sm">
					{t("settings.backup.description")}
				</p>

				{/* Periodic backup toggle */}
				<div className="bg-(--surface-elevated) rounded-[var(--radius-card,0.375rem)] p-3 space-y-3">
					<div className="flex items-center justify-between">
						<div>
							<div className="flex items-center gap-1">
								<p className="text-sm font-medium text-(--text-primary)">
									{t("settings.backup.rotation.title")}
								</p>
							</div>
							<p className="text-xs text-(--text-muted) mt-0.5">
								{t("settings.backup.rotation.enabledDescription")}
							</p>
						</div>
						<Toggle
							checked={backupEnabled}
							onChange={async (v) => {
								if (v) {
									try {
										const preview = await api.backups.prunePreview();
										setPrunePreview(preview);
										setShowEnableConfirm(true);
									} catch {
										toast(
											t("settings.backup.rotation.prunePreviewFailed"),
											"error",
										);
									}
								} else {
									settingsUpdateMutation.mutate({
										backup_enabled: "false",
									});
								}
							}}
						/>
					</div>

					{backupEnabled && (
						<div className="space-y-3 pt-2 border-t border-(--border-default)">
							<SettingsSlider
								id="backup-interval"
								label={t("settings.backup.rotation.interval")}
								value={backupInterval}
								min={300}
								max={604800}
								step={300}
								clampStep={300}
								unit="s"
								onChange={(v) =>
									settingsUpdateMutation.mutate({
										backup_interval: String(v),
									})
								}
								description={t("settings.backup.rotation.intervalDescription")}
							/>
							<SettingsSlider
								id="backup-son-retention"
								label={t("settings.backup.rotation.sonRetention")}
								value={sonRetention}
								min={1}
								max={365}
								step={1}
								clampStep={1}
								onChange={(v) =>
									settingsUpdateMutation.mutate({
										backup_son_retention: String(v),
									})
								}
								description={t(
									"settings.backup.rotation.sonRetentionDescription",
								)}
							/>
							<SettingsSlider
								id="backup-father-retention"
								label={t("settings.backup.rotation.fatherRetention")}
								value={fatherRetention}
								min={0}
								max={52}
								step={1}
								clampStep={1}
								onChange={(v) =>
									settingsUpdateMutation.mutate({
										backup_father_retention: String(v),
									})
								}
								description={t(
									"settings.backup.rotation.fatherRetentionDescription",
								)}
							/>
							<SettingsSlider
								id="backup-grandfather-retention"
								label={t("settings.backup.rotation.grandfatherRetention")}
								value={grandfatherRetention}
								min={0}
								max={120}
								step={1}
								clampStep={1}
								onChange={(v) =>
									settingsUpdateMutation.mutate({
										backup_grandfather_retention: String(v),
									})
								}
								description={t(
									"settings.backup.rotation.grandfatherRetentionDescription",
								)}
							/>
						</div>
					)}
				</div>

				{/* Double-confirm modal for enabling periodic backup */}
				{showEnableConfirm && (
					<Modal
						onClose={() => {
							setShowEnableConfirm(false);
							setPrunePreview(null);
						}}
						title={t("settings.backup.rotation.confirmEnableTitle")}
						maxWidth="max-w-lg"
					>
						<div className="space-y-3">
							<div className="flex items-start gap-2 text-amber-400">
								<AlertTriangle size={18} className="shrink-0 mt-0.5" />
								<p className="text-sm text-(--text-secondary)">
									{t("settings.backup.rotation.confirmEnableDescription")}
								</p>
							</div>
							{prunePreview && prunePreview.prune.length > 0 ? (
								<div className="space-y-2">
									<p className="text-sm text-(--text-primary)">
										{t("settings.backup.rotation.confirmEnableWouldRemove", {
											count: prunePreview.prune.length,
										})}
									</p>
									<div className="max-h-40 overflow-y-auto rounded bg-(--surface-elevated) border border-(--border-default) p-2">
										{prunePreview.prune.map((b) => (
											<div
												key={b.filename}
												className="text-xs font-mono text-(--text-secondary) py-0.5"
											>
												{b.filename}
											</div>
										))}
									</div>
								</div>
							) : (
								<p className="text-sm text-(--text-secondary)">
									{t("settings.backup.rotation.confirmEnableNoRemoval")}
								</p>
							)}
							<div className="flex justify-end gap-2 pt-2">
								<button
									type="button"
									onClick={() => {
										setShowEnableConfirm(false);
										setPrunePreview(null);
									}}
									className="ui-btn ui-btn-secondary text-sm px-4 py-2"
								>
									{t("common.cancel")}
								</button>
								<button
									type="button"
									onClick={async () => {
										try {
											if (prunePreview && prunePreview.prune.length > 0) {
												await api.backups.prune();
											}
											settingsUpdateMutation.mutate({
												backup_enabled: "true",
											});
											toast(
												t("settings.backup.rotation.pruneSuccess", {
													count: prunePreview?.prune.length ?? 0,
												}),
												"success",
											);
										} catch {
											toast(t("settings.backup.rotation.pruneFailed"), "error");
										} finally {
											setShowEnableConfirm(false);
											setPrunePreview(null);
											queryClient.invalidateQueries({
												queryKey: ["backups"],
											});
										}
									}}
									className="ui-btn ui-btn-primary text-sm px-4 py-2"
								>
									{t("settings.backup.confirm")}
								</button>
							</div>
						</div>
					</Modal>
				)}

				{/* Restore requirements */}
				<div className="bg-(--surface-elevated) rounded-[var(--radius-card,0.375rem)] p-3 space-y-2">
					<h4 className="text-xs font-semibold uppercase tracking-wider text-(--text-secondary)">
						{t("settings.backup.restoreRequirements")}
					</h4>
					<ul className="text-xs text-(--text-secondary) space-y-1 list-disc list-inside">
						<li>
							<strong className="text-(--text-primary)">
								{t("settings.backup.restoreRequirements.masterKey")}
							</strong>
							: {t("settings.backup.restoreRequirements.masterKeyDescription")}
						</li>
						<li>
							<strong className="text-(--text-primary)">
								{t("settings.backup.restoreRequirements.adminToken")}
							</strong>
							: {t("settings.backup.restoreRequirements.adminTokenDescription")}
						</li>
						<li>
							<strong className="text-(--text-primary)">
								{t("settings.backup.restoreRequirements.virtualKeys")}
							</strong>
							:{" "}
							{t("settings.backup.restoreRequirements.virtualKeysDescription")}
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
								toast(t("settings.backup.restoreSuccess"), "success");
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
												toast(t("settings.backup.serverBackOnline"), "success");
												return;
											}
										} catch {
											// Server not up yet
										}
										await new Promise((r) => setTimeout(r, 2000));
										attempts++;
									}
									if (pollingRef.current) {
										toast(t("settings.backup.serverRestarting"), "warning");
									}
								};
								checkServer();
							} catch (err) {
								toast(
									t("settings.backup.restoreFailed", {
										message: (err as Error).message,
									}),
									"error",
								);
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
						{createMutation.isPending ? <Spinner /> : <Plus size={14} />}
						{createMutation.isPending
							? t("settings.backup.creatingBackup")
							: t("settings.backup.createBackup")}
					</button>
					<div className="flex items-center gap-2">
						<input
							ref={fileInputRef}
							type="file"
							accept=".dump"
							className="hidden"
							aria-label={t("settings.backup.selectBackupFile")}
							onChange={(e) => {
								const file = e.target.files?.[0];
								if (file) {
									setRestoreFile(file);
									setShowRestoreModal(true);
								}
								e.target.value = "";
							}}
						/>
						<button
							type="button"
							onClick={() => fileInputRef.current?.click()}
							disabled={isRestoring}
							className="ui-btn flex items-center gap-2 border border-dashed border-(--border-default) text-(--text-secondary) hover:text-(--text-primary) hover:border-(--accent) hover:bg-(--surface-elevated) transition-colors"
						>
							<Upload size={14} />
							{isRestoring
								? t("settings.backup.restoring")
								: t("settings.backup.uploadRestore")}
						</button>
					</div>
				</div>

				{/* Backup list */}
				{isLoading ? (
					<LoadingSpinner />
				) : backups && backups.length > 0 ? (
					<div className="space-y-2 max-h-[300px] overflow-y-auto">
						<h4 className="text-xs font-semibold uppercase tracking-wider text-(--text-muted) py-1">
							{t("settings.backup.availableBackups", { count: backups.length })}
						</h4>
						{backups.map((backup) => (
							<div
								key={backup.filename}
								className="flex items-center justify-between bg-(--surface-elevated) rounded-[var(--radius-card,0.375rem)] border border-(--border-default) p-3"
							>
								<div className="min-w-0 flex-1">
									<p className="text-sm font-medium text-(--text-primary) truncate">
										{backup.filename}
									</p>
									<p className="text-xs text-(--text-muted)">
										{formatBytes(backup.size_bytes)} -{" "}
										{formatDateTimeShort(backup.created_at)}
									</p>
								</div>
								<div className="flex items-center gap-2 ml-3 shrink-0">
									{confirmDelete === backup.filename ? (
										<>
											<span className="text-xs text-red-400">
												{t("settings.backup.deleteConfirm")}
											</span>
											<button
												type="button"
												onClick={() => deleteMutation.mutate(backup.filename)}
												disabled={deleteMutation.isPending}
												className="ui-btn ui-btn-danger text-xs px-2 py-1"
											>
												{t("settings.backup.confirm")}
											</button>
											<button
												type="button"
												onClick={() => setConfirmDelete(null)}
												className="ui-btn ui-btn-secondary text-xs px-2 py-1"
											>
												{t("settings.backup.cancel")}
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
												{t("settings.backup.download")}
											</button>
											<button
												type="button"
												onClick={() => setConfirmDelete(backup.filename)}
												className="ui-btn ui-btn-danger text-xs px-2 py-1"
												title={t("settings.backup.delete")}
												aria-label={t("settings.backup.delete")}
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
					<p className="text-xs text-(--text-muted)">
						{t("settings.backup.noBackups")}
					</p>
				)}
			</div>
		</SettingsSection>
	);
}
