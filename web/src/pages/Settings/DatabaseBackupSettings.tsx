import { useQueryClient } from "@tanstack/react-query";
import { Trans, useTranslation } from "react-i18next";
import { Copy, Download, HardDrive, Plus, Trash2, Upload } from "@/lib/icons";
import { api, getAuthHeaders } from "../../api/client";
import { LoadingSpinner } from "../../components/LoadingSpinner";
import { RestoreConfirmModal } from "../../components/RestoreConfirmModal";
import { SettingsGroup } from "../../components/SettingsGroup";
import { SettingsSection } from "../../components/SettingsSection";
import { SettingsSlider } from "../../components/SettingsSlider";
import { Spinner } from "../../components/Spinner";
import { Toggle } from "../../components/Toggle";
import { useToast } from "../../context/ToastContext";
import { formatDateTimeShort } from "../../utils/format";
import { BackupEnableConfirm } from "./BackupEnableConfirm";
import { useBackupActions } from "./useBackupActions";

interface DatabaseBackupSettingsProps {
	collapsed: boolean;
	onToggle: () => void;
	managed?: boolean;
}

export function DatabaseBackupSettings({
	collapsed,
	onToggle,
	managed,
}: DatabaseBackupSettingsProps) {
	const { t } = useTranslation();
	const { toast } = useToast();
	const queryClient = useQueryClient();
	const {
		confirmDelete,
		setConfirmDelete,
		restoreFile,
		setRestoreFile,
		showRestoreModal,
		setShowRestoreModal,
		isRestoring,
		setIsRestoring,
		showEnableConfirm,
		setShowEnableConfirm,
		prunePreview,
		setPrunePreview,
		fileInputRef,
		pollingRef,
		gfsLabel,
		createMutation,
		deleteMutation,
		settingsUpdateMutation,
		backupEnabled,
		intervalHours,
		sonRetention,
		fatherRetention,
		grandfatherRetention,
		copySignature,
		downloadBackup,
		backups,
		isLoading,
		formatBytes,
	} = useBackupActions();

	return (
		<SettingsSection
			icon={HardDrive}
			title={t("settings.backup.title")}
			collapsed={collapsed}
			onToggle={onToggle}
			managed={managed}
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
							ariaLabel={t("settings.backup.rotation.title")}
							onChange={async (v) => {
								if (!v) {
									settingsUpdateMutation.mutate({
										backup_enabled: "false",
									});
									return;
								}
								try {
									const preview = await api.backups.prunePreview();
									// Nothing falls outside the rotation window, so enabling
									// deletes nothing. Skip the confirmation modal and just turn
									// it on, the same way every other backup setting saves.
									if ((preview.prune?.length ?? 0) === 0) {
										settingsUpdateMutation.mutate({
											backup_enabled: "true",
										});
										return;
									}
									setPrunePreview(preview);
									setShowEnableConfirm(true);
								} catch {
									toast(
										t("settings.backup.rotation.prunePreviewFailed"),
										"error",
									);
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

				{/* Both confirms render in portals outside the disabled fieldset, so
				    each is told about managed itself: they stay open (a polled flip
				    must not wipe a typed token or an in-flight restore) but their
				    action buttons go inert. */}
				<BackupEnableConfirm
					managed={managed}
					showEnableConfirm={showEnableConfirm}
					setShowEnableConfirm={setShowEnableConfirm}
					prunePreview={prunePreview}
					setPrunePreview={setPrunePreview}
					settingsUpdateMutation={settingsUpdateMutation}
				/>

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
						managed={managed}
						open={showRestoreModal}
						onClose={() => {
							setShowRestoreModal(false);
							setRestoreFile(null);
						}}
						onConfirm={async (adminToken, signature) => {
							setIsRestoring(true);
							try {
								await api.backups.restore(restoreFile, adminToken, signature);
								// A supplied signature that did not match would have been a 400,
								// so reaching here with one means the dump was verified.
								toast(
									t(
										signature
											? "settings.backup.restoreSuccessVerified"
											: "settings.backup.restoreSuccess",
									),
									"success",
								);
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
							className="ui-btn ui-btn-primary"
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
								className="ui-btn ui-btn-dashed text-(--text-secondary) hover:text-(--text-primary) hover:bg-(--surface-elevated)"
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
											{backup.origin === "scheduled" &&
												gfsLabel.get(backup.filename) && (
													<span className="shrink-0 inline-flex h-4 w-4 items-center justify-center rounded text-[10px] font-bold bg-(--accent)/15 text-(--accent)">
														{gfsLabel.get(backup.filename)}
													</span>
												)}
											{backup.origin === "frontdesk" && (
												<span
													className="shrink-0 inline-flex h-4 items-center justify-center rounded px-1 text-[10px] font-bold bg-(--accent)/15 text-(--accent)"
													title={t("settings.backup.frontDeskCreated")}
												>
													FD
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
													className="ui-btn ui-btn-danger"
												>
													{t("settings.backup.confirm")}
												</button>
												<button
													type="button"
													onClick={() => setConfirmDelete(null)}
													className="ui-btn ui-btn-secondary"
												>
													{t("settings.backup.cancel")}
												</button>
											</>
										) : (
											<>
												{backup.signed && (
													<button
														type="button"
														onClick={() => copySignature(backup.filename)}
														className="ui-btn ui-btn-secondary"
														title={t("settings.backup.copySignature")}
														aria-label={t("settings.backup.copySignature")}
													>
														<Copy size={12} />
													</button>
												)}
												<button
													type="button"
													onClick={() => downloadBackup(backup.filename)}
													className="ui-btn ui-btn-secondary"
												>
													<Download size={12} />
													{t("settings.backup.download")}
												</button>
												<button
													type="button"
													onClick={() => setConfirmDelete(backup.filename)}
													className="ui-btn ui-btn-danger"
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
