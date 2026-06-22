import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useMemo, useRef, useState } from "react";
import { Trans, useTranslation } from "react-i18next";
import {
	AlertTriangle,
	Download,
	HardDrive,
	Plus,
	Trash2,
	Upload,
} from "@/lib/icons";
import { api, getAuthHeaders } from "../../api/client";
import type { BackupClassification } from "../../api/types";
import { LoadingSpinner } from "../../components/LoadingSpinner";
import { Modal } from "../../components/Modal";
import { RestoreConfirmModal } from "../../components/RestoreConfirmModal";
import { SettingsGroup } from "../../components/SettingsGroup";
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

	// GFS bucket per backup, so each row can carry a Grandfather/Father/Son tag.
	// Sourced from the prune-preview classifier (it groups every backup by age
	// against the configured retention), so the labels track the same rotation
	// the sliders above configure.
	const { data: classification } = useQuery({
		queryKey: ["backups", "classification"],
		queryFn: () => api.backups.prunePreview(),
		enabled: (backups?.length ?? 0) > 0,
	});

	const gfsLabel = useMemo(() => {
		const m = new Map<string, "G" | "F" | "S">();
		for (const b of classification?.grandfather ?? []) m.set(b.filename, "G");
		for (const b of classification?.father ?? []) m.set(b.filename, "F");
		for (const b of classification?.son ?? []) m.set(b.filename, "S");
		return m;
	}, [classification]);

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
	// Parse interval: backend stores as Go duration string (e.g. "86400s" or "24h").
	// Display and edit in hours.
	const rawInterval = settings?.backup_interval || "24h";
	const intervalHours = (() => {
		const hMatch = rawInterval.match(/^(\d+(?:\.\d+)?)h$/);
		if (hMatch) return Number(hMatch[1]);
		const sMatch = rawInterval.match(/^(\d+(?:\.\d+)?)s$/);
		if (sMatch) return Math.round((Number(sMatch[1]) / 3600) * 10) / 10;
		return 24;
	})();
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
			onResetSection={() =>
				settingsUpdateMutation.mutate({
					backup_enabled: "false",
					backup_interval: "24h",
					backup_son_retention: "7",
					backup_father_retention: "4",
					backup_grandfather_retention: "3",
				})
			}
			resetTooltip={t("settings.common.resetSection")}
		>
			<div className="space-y-4">
				<p className="text-(--text-secondary) text-sm">
					{/* Locale strings carry <code>pg_dump</code> tags; if a translation
					    ever drops them, the text still renders, just unstyled. */}
					<Trans
						i18nKey="settings.backup.description"
						components={{
							code: <code className="font-mono text-(--text-primary)" />,
						}}
					/>
				</p>

				{/* Periodic backup */}
				<SettingsGroup title={t("settings.backup.rotation.title")}>
					<div className="flex items-center justify-between">
						<div>
							<p className="text-xs text-(--text-muted) mt-0.5">
								{t("settings.backup.rotation.enabledDescription")}
							</p>
						</div>
						<Toggle
							checked={backupEnabled}
							size="sm"
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

					<div className="space-y-3 pt-2">
						<SettingsSlider
							id="backup-interval"
							disabled={!backupEnabled}
							label={t("settings.backup.rotation.interval")}
							value={intervalHours}
							min={0.5}
							max={168}
							step={0.5}
							clampStep={0.5}
							unit="h"
							onReset={() =>
								settingsUpdateMutation.mutate({ backup_interval: "24h" })
							}
							resetTooltip={t("settings.common.resetToDefault")}
							onChange={(v) =>
								settingsUpdateMutation.mutate({
									backup_interval: `${v}h`,
								})
							}
							description={t("settings.backup.rotation.intervalDescription")}
						/>
						<SettingsSlider
							id="backup-son-retention"
							disabled={!backupEnabled}
							label={t("settings.backup.rotation.sonRetention")}
							value={sonRetention}
							min={1}
							max={365}
							step={1}
							clampStep={1}
							unit="d"
							onReset={() =>
								settingsUpdateMutation.mutate({ backup_son_retention: "7" })
							}
							resetTooltip={t("settings.common.resetToDefault")}
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
							disabled={!backupEnabled}
							label={t("settings.backup.rotation.fatherRetention")}
							value={fatherRetention}
							min={0}
							max={52}
							step={1}
							clampStep={1}
							unit="w"
							onReset={() =>
								settingsUpdateMutation.mutate({
									backup_father_retention: "4",
								})
							}
							resetTooltip={t("settings.common.resetToDefault")}
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
							disabled={!backupEnabled}
							label={t("settings.backup.rotation.grandfatherRetention")}
							value={grandfatherRetention}
							min={0}
							max={120}
							step={1}
							clampStep={1}
							unit="m"
							onReset={() =>
								settingsUpdateMutation.mutate({
									backup_grandfather_retention: "3",
								})
							}
							resetTooltip={t("settings.common.resetToDefault")}
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
				</SettingsGroup>

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
							{(prunePreview?.prune?.length ?? 0) > 0 ? (
								<div className="space-y-2">
									<p className="text-sm text-(--text-primary)">
										{t("settings.backup.rotation.confirmEnableWouldRemove", {
											count: prunePreview?.prune?.length ?? 0,
										})}
									</p>
									<div className="max-h-40 overflow-y-auto rounded bg-(--surface-elevated) border border-(--border-default) p-2">
										{(prunePreview?.prune ?? []).map((b) => (
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
											if ((prunePreview?.prune?.length ?? 0) > 0) {
												await api.backups.prune();
											}
											await settingsUpdateMutation.mutateAsync({
												backup_enabled: "true",
											});
											toast(
												t("settings.backup.rotation.pruneSuccess", {
													count: prunePreview?.prune?.length ?? 0,
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
				<div className="rounded-[var(--radius-card,0.375rem)] border border-(--accent)/30 bg-(--accent)/5 p-3 space-y-2">
					<h4 className="text-xs font-semibold uppercase tracking-wider text-(--accent)">
						{t("settings.backup.restoreRequirements")}
					</h4>
					<ul className="text-xs text-(--text-secondary) space-y-1 list-disc list-outside ps-5">
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

				{/* Available backups */}
				<SettingsGroup title={t("settings.backup.availableBackupsTitle")}>
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
							{backups.map((backup) => (
								<div
									key={backup.filename}
									className="flex items-center justify-between bg-(--surface-elevated) rounded-[var(--radius-card,0.375rem)] border border-(--border-default) p-3"
								>
									<div className="min-w-0 flex-1">
										<div className="flex items-center gap-2">
											{backup.origin !== "manual" &&
												gfsLabel.get(backup.filename) && (
													<span className="shrink-0 inline-flex h-4 w-4 items-center justify-center rounded text-[10px] font-bold bg-(--accent)/15 text-(--accent)">
														{gfsLabel.get(backup.filename)}
													</span>
												)}
											<p className="text-sm font-medium text-(--text-primary) truncate">
												{backup.filename}
											</p>
										</div>
										<p className="text-xs text-(--text-muted)">
											{backup.origin === "manual" && (
												<span className="text-(--accent)">
													{t("settings.backup.manuallyCreated")} ·{" "}
												</span>
											)}
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
				</SettingsGroup>
			</div>
		</SettingsSection>
	);
}
