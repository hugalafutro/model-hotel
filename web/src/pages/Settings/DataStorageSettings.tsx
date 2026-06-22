import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Database } from "@/lib/icons";
import { api } from "../../api/client";
import { SettingsGroup } from "../../components/SettingsGroup";
import { SettingsSection } from "../../components/SettingsSection";
import { SettingsSlider } from "../../components/SettingsSlider";
import { Toggle } from "../../components/Toggle";
import { useStorage } from "../../context/StorageContext";
import { useToast } from "../../context/ToastContext";
import {
	clearArenaHistory,
	getArenaHistoryCount,
} from "../../utils/arenaHistory";
import {
	goDurationToHours,
	goDurationToMinutes,
	hoursToGoDuration,
	minutesToGoDuration,
} from "../../utils/duration";
import { clearProviderCache, getProviderCacheCount } from "./constants";
import { useSettingsMutations } from "./useSettingsMutations";

interface DataStorageSettingsProps {
	collapsed: boolean;
	onToggle: () => void;
	onResetSection?: () => void;
}

export function DataStorageSettings({
	collapsed,
	onToggle,
	onResetSection,
}: DataStorageSettingsProps) {
	const { t } = useTranslation();
	const { toast } = useToast();
	const queryClient = useQueryClient();
	const { settings, updateMutation, resetSettingMutation } =
		useSettingsMutations();
	const [confirmDelete, setConfirmDelete] = useState(false);
	const [deleteSelection, setDeleteSelection] = useState("");
	const [confirmDeleteAppLogs, setConfirmDeleteAppLogs] = useState(false);
	const [appLogsDeleteSelection, setAppLogsDeleteSelection] = useState("");

	const [quotaDisabled, setQuotaDisabled] = useState(() => {
		try {
			return localStorage.getItem("sidebarQuotaDisabled") === "true";
		} catch {
			return false;
		}
	});
	const [refreshMin, setRefreshMin] = useState(() => {
		try {
			return localStorage.getItem("sidebarQuotaRefreshMin") || "5";
		} catch {
			return "5";
		}
	});
	const [refreshSec, setRefreshSec] = useState(() => {
		try {
			return localStorage.getItem("dashboardRefreshSec") || "30";
		} catch {
			return "30";
		}
	});

	const handleDashboardRefreshChange = (val: number) => {
		const valStr = String(val);
		setRefreshSec(valStr);
		try {
			localStorage.setItem("dashboardRefreshSec", valStr);
		} catch {
			/* ignore */
		}
		window.dispatchEvent(new CustomEvent("dashboardRefreshChange"));
		toast(
			val === 0
				? t("settings.dashboard.disabled")
				: t("settings.dashboard.intervalSet", {
						seconds: valStr,
						count: val,
					}),
			"success",
		);
	};

	const {
		persistChat,
		setPersistChat,
		persistArena,
		setPersistArena,
		persistConversation,
		setPersistConversation,
		arenaHistoryEnabled,
		setArenaHistoryEnabled,
		arenaHistoryLimit,
		setArenaHistoryLimit,
	} = useStorage();

	const purgeMutation = useMutation({
		mutationFn: (olderThan: string) => api.logs.purge(olderThan),
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: ["logs"] });
			toast(t("settings.common.requestsDeleted"), "success");
			setConfirmDelete(false);
			setDeleteSelection("");
		},
		onError: (err: Error) => {
			toast(
				t("settings.common.failedToDeleteRequests", { message: err.message }),
				"error",
			);
			setConfirmDelete(false);
		},
	});

	const purgeAppLogsMutation = useMutation({
		mutationFn: (olderThan: string) => api.appLogs.purge(olderThan),
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: ["appLogs"] });
			toast(t("settings.common.logsDeleted"), "success");
			setConfirmDeleteAppLogs(false);
			setAppLogsDeleteSelection("");
		},
		onError: (err: Error) => {
			toast(
				t("settings.common.failedToDeleteAppLogs", { message: err.message }),
				"error",
			);
			setConfirmDeleteAppLogs(false);
		},
	});

	const logRetention = settings?.log_retention || "0";
	const staleRequestTimeout = settings?.stale_request_timeout || "30m0s";
	const logRetentionHours = goDurationToHours(logRetention);
	const staleTimeoutMinutes = goDurationToMinutes(staleRequestTimeout);

	// The dropdown values (1d/1w/1m/all) are exactly the tokens the backend's
	// purge endpoints accept, so pass the selection through and only guard the
	// empty "select a range" placeholder.
	const getDeleteOlderThan = (selection: string): string =>
		["1d", "1w", "1m", "all"].includes(selection) ? selection : "";

	return (
		<SettingsSection
			icon={Database}
			title={t("settings.dataStorageAndLogging.title")}
			collapsed={collapsed}
			onToggle={onToggle}
			onResetSection={onResetSection}
		>
			<div className="space-y-5">
				<p className="text-gray-400 text-sm">
					{t("settings.dataStorage.description")}
				</p>

				<div className="grid grid-cols-2 gap-x-6">
					<div className="space-y-5">
						<SettingsGroup title={t("settings.logging.title")}>
							<SettingsSlider
								id="log-retention"
								label={t("settings.logging.logRetention")}
								value={logRetentionHours}
								min={0}
								max={720}
								step={24}
								clampStep={24}
								infinityValue={0}
								unit="h"
								onChange={(v) =>
									updateMutation.mutate({
										log_retention: hoursToGoDuration(v),
									})
								}
								description={t("settings.logging.logRetention.description")}
								onReset={() => resetSettingMutation.mutate(["log_retention"])}
								resetTooltip={t("settings.common.resetSetting")}
							/>

							<SettingsSlider
								id="stale-request-timeout"
								label={t("settings.logging.staleRequestTimeout")}
								value={staleTimeoutMinutes}
								min={0}
								max={120}
								step={5}
								clampStep={5}
								infinityValue={0}
								unit="m"
								onChange={(v) =>
									updateMutation.mutate({
										stale_request_timeout: minutesToGoDuration(v),
									})
								}
								description={t(
									"settings.logging.staleRequestTimeout.description",
								)}
								onReset={() =>
									resetSettingMutation.mutate(["stale_request_timeout"])
								}
								resetTooltip={t("settings.common.resetSetting")}
							/>

							<div className="flex items-center gap-2 flex-wrap">
								{!confirmDelete ? (
									<button
										type="button"
										onClick={() => setConfirmDelete(true)}
										className="ui-btn ui-btn-danger"
										title={t("settings.logging.deleteRequests.tooltip")}
									>
										{t("settings.logging.deleteRequests")}
									</button>
								) : (
									<>
										<select
											value={deleteSelection}
											onChange={(e) => setDeleteSelection(e.target.value)}
											className="ui-input px-3 py-1.5 text-xs"
										>
											<option value="">
												{t("settings.logging.deleteRequests.selectRange")}
											</option>
											<option value="1d">
												{t("settings.logging.deleteRequests.olderThan1d")}
											</option>
											<option value="1w">
												{t("settings.logging.deleteRequests.olderThan1w")}
											</option>
											<option value="1m">
												{t("settings.logging.deleteRequests.olderThan1m")}
											</option>
											<option value="all">
												{t("settings.logging.deleteRequests.allLogs")}
											</option>
										</select>
										<button
											type="button"
											disabled={!deleteSelection}
											onClick={() => {
												const olderThan = getDeleteOlderThan(deleteSelection);
												if (olderThan) purgeMutation.mutate(olderThan);
											}}
											className="ui-btn ui-btn-danger disabled:opacity-50 disabled:cursor-not-allowed"
										>
											{t("settings.logging.deleteRequests.confirm")}
										</button>
										<button
											type="button"
											onClick={() => {
												setConfirmDelete(false);
												setDeleteSelection("");
											}}
											className="ui-btn ui-btn-secondary"
										>
											{t("settings.logging.deleteRequests.cancel")}
										</button>
									</>
								)}

								{!confirmDeleteAppLogs ? (
									<button
										type="button"
										onClick={() => setConfirmDeleteAppLogs(true)}
										className="ui-btn ui-btn-danger"
										title={t("settings.logging.deleteAppLogs.tooltip")}
									>
										{t("settings.logging.deleteAppLogs")}
									</button>
								) : (
									<>
										<select
											value={appLogsDeleteSelection}
											onChange={(e) =>
												setAppLogsDeleteSelection(e.target.value)
											}
											className="ui-input px-3 py-1.5 text-xs"
										>
											<option value="">
												{t("settings.logging.deleteAppLogs.selectRange")}
											</option>
											<option value="1d">
												{t("settings.logging.deleteAppLogs.olderThan1d")}
											</option>
											<option value="1w">
												{t("settings.logging.deleteAppLogs.olderThan1w")}
											</option>
											<option value="1m">
												{t("settings.logging.deleteAppLogs.olderThan1m")}
											</option>
											<option value="all">
												{t("settings.logging.deleteAppLogs.allLogs")}
											</option>
										</select>
										<button
											type="button"
											disabled={
												!appLogsDeleteSelection ||
												purgeAppLogsMutation.isPending
											}
											onClick={() => {
												const olderThan = getDeleteOlderThan(
													appLogsDeleteSelection,
												);
												if (olderThan) purgeAppLogsMutation.mutate(olderThan);
											}}
											className="ui-btn ui-btn-danger disabled:opacity-50 disabled:cursor-not-allowed"
										>
											{purgeAppLogsMutation.isPending
												? t("settings.logging.deleteAppLogs.deleting")
												: t("settings.logging.deleteAppLogs.confirm")}
										</button>
										<button
											type="button"
											onClick={() => {
												setConfirmDeleteAppLogs(false);
												setAppLogsDeleteSelection("");
											}}
											className="ui-btn ui-btn-secondary"
										>
											{t("settings.logging.deleteAppLogs.cancel")}
										</button>
									</>
								)}
							</div>
						</SettingsGroup>

						<SettingsGroup title={t("settings.dataStorage.cacheAndResets")}>
							<div className="flex items-center justify-between gap-2">
								<div>
									<p className="text-sm font-medium text-gray-300">
										{t("settings.dataStorage.providerQuotaCache")}
									</p>
									<p className="text-gray-500 text-xs mt-0.5">
										{t("settings.dataStorage.providerQuotaCacheDescription", {
											count: getProviderCacheCount(),
										})}
									</p>
								</div>
								<button
									type="button"
									onClick={() => {
										if (confirm(t("settings.dataStorage.clearCacheConfirm"))) {
											clearProviderCache();
											toast(
												t("settings.dataStorage.clearCacheCleared"),
												"info",
											);
										}
									}}
									className="ui-btn ui-btn-danger text-xs px-3 py-1.5"
									disabled={getProviderCacheCount() === 0}
									title={t("settings.dataStorage.clearCache.tooltip")}
								>
									{t("settings.dataStorage.clearCache")}
								</button>
							</div>

							<div className="flex items-center justify-between gap-2">
								<div>
									<p className="text-sm font-medium text-gray-300">
										{t("settings.dataStorage.dismissedErrorBanners")}
									</p>
									<p className="text-gray-500 text-xs mt-0.5">
										{t("settings.dataStorage.dismissedErrorBannersDescription")}
									</p>
								</div>
								<button
									type="button"
									onClick={() => {
										localStorage.removeItem("ackedErrorKeys");
										window.dispatchEvent(
											new CustomEvent("dismissedErrorsReset"),
										);
										toast(
											t("settings.dataStorage.resetDismissedBanners"),
											"info",
										);
									}}
									className="ui-btn ui-btn-danger text-xs px-3 py-1.5"
									title={t("settings.dataStorage.reset.tooltip")}
								>
									{t("settings.dataStorage.reset")}
								</button>
							</div>
						</SettingsGroup>

						<SettingsGroup title={t("settings.dataStorage.quotaBadges")}>
							<div className="flex items-center justify-between gap-2">
								<div>
									<p className="text-sm font-medium text-gray-300">
										{t("settings.sidebarQuota.showQuotasPill")}
									</p>
									<p className="text-gray-500 text-xs mt-0.5">
										{t("settings.sidebarQuota.showQuotasPillDescription")}
									</p>
								</div>
								<Toggle
									checked={!quotaDisabled}
									size="sm"
									onChange={(v) => {
										const newVal = !v;
										setQuotaDisabled(newVal);
										try {
											localStorage.setItem(
												"sidebarQuotaDisabled",
												String(newVal),
											);
										} catch {
											/* ignore */
										}
										toast(
											newVal
												? t("settings.sidebarQuota.disabledQuotas")
												: t("settings.sidebarQuota.enabledQuotas"),
											newVal ? "info" : "success",
										);
										window.dispatchEvent(new CustomEvent("sidebarQuotaToggle"));
									}}
								/>
							</div>

							<SettingsSlider
								id="quota-refresh-interval"
								label={t("settings.sidebarQuota.refreshInterval")}
								value={Number(refreshMin)}
								min={0}
								max={30}
								step={1}
								clampStep={1}
								infinityValue={0}
								unit="m"
								disabled={quotaDisabled}
								onChange={(v) => {
									const val = String(v);
									setRefreshMin(val);
									try {
										localStorage.setItem("sidebarQuotaRefreshMin", val);
									} catch {
										/* ignore */
									}
									window.dispatchEvent(
										new CustomEvent("sidebarQuotaRefreshChange"),
									);
									toast(
										v === 0
											? t("settings.sidebarQuota.disabled")
											: t("settings.sidebarQuota.intervalSet", {
													minutes: v,
													count: v,
												}),
										"success",
									);
								}}
								description={t(
									"settings.sidebarQuota.refreshInterval.description",
								)}
							/>
						</SettingsGroup>
					</div>

					<div className="space-y-5">
						<SettingsGroup title={t("settings.dataStorage.sessionPersistence")}>
							<div className="flex items-center justify-between gap-2">
								<div>
									<p className="text-sm font-medium text-gray-300">
										{t("settings.dataStorage.persistChat")}
									</p>
									<p className="text-gray-500 text-xs mt-0.5">
										{t("settings.dataStorage.persistChatDescription")}
									</p>
								</div>
								<Toggle
									checked={persistChat}
									size="sm"
									onChange={(v) => {
										const next = v;
										if (
											!next &&
											!confirm(t("settings.dataStorage.persistChatConfirm"))
										)
											return;
										setPersistChat(next);
										toast(
											next
												? t("settings.dataStorage.persistChatEnabled")
												: t("settings.dataStorage.persistChatDisabled"),
											next ? "success" : "info",
										);
									}}
								/>
							</div>

							<div className="flex items-center justify-between gap-2">
								<div>
									<p className="text-sm font-medium text-gray-300">
										{t("settings.dataStorage.persistArena")}
									</p>
									<p className="text-gray-500 text-xs mt-0.5">
										{t("settings.dataStorage.persistArenaDescription")}
									</p>
								</div>
								<Toggle
									checked={persistArena}
									size="sm"
									onChange={(v) => {
										const next = v;
										if (
											!next &&
											!confirm(t("settings.dataStorage.persistArenaConfirm"))
										)
											return;
										setPersistArena(next);
										toast(
											next
												? t("settings.dataStorage.persistArenaEnabled")
												: t("settings.dataStorage.persistArenaDisabled"),
											next ? "success" : "info",
										);
									}}
								/>
							</div>

							<div className="flex items-center justify-between gap-2">
								<div>
									<p className="text-sm font-medium text-gray-300">
										{t("settings.dataStorage.persistConversation")}
									</p>
									<p className="text-gray-500 text-xs mt-0.5">
										{t("settings.dataStorage.persistConversationDescription")}
									</p>
								</div>
								<Toggle
									checked={persistConversation}
									size="sm"
									onChange={(v) => {
										const next = v;
										if (
											!next &&
											!confirm(
												t("settings.dataStorage.persistConversationConfirm"),
											)
										)
											return;
										setPersistConversation(next);
										toast(
											next
												? t("settings.dataStorage.persistConversationEnabled")
												: t("settings.dataStorage.persistConversationDisabled"),
											next ? "success" : "info",
										);
									}}
								/>
							</div>
						</SettingsGroup>

						<SettingsGroup title={t("settings.dataStorage.arenaHistory")}>
							<div className="flex items-center justify-between gap-2">
								<div>
									<p className="text-sm font-medium text-gray-300">
										{t("settings.dataStorage.saveMatchHistory")}
									</p>
									<p className="text-gray-500 text-xs mt-0.5">
										{t("settings.dataStorage.saveMatchHistoryDescription")}
									</p>
								</div>
								<Toggle
									checked={arenaHistoryEnabled}
									size="sm"
									onChange={(v) => {
										const next = v;
										setArenaHistoryEnabled(next);
										toast(
											next
												? t("settings.dataStorage.saveMatchHistoryEnabled")
												: t("settings.dataStorage.saveMatchHistoryDisabled"),
											next ? "success" : "info",
										);
									}}
								/>
							</div>

							<SettingsSlider
								id="history-limit"
								label={t("settings.dataStorage.maxSavedMatches")}
								value={arenaHistoryLimit}
								min={10}
								max={100}
								step={5}
								clampStep={5}
								unit="m"
								hideUnit
								disabled={!arenaHistoryEnabled}
								onChange={(v) => {
									setArenaHistoryLimit(v);
									toast(
										t("settings.dataStorage.historyLimitToast", { count: v }),
										"success",
									);
								}}
								description={t(
									"settings.dataStorage.maxSavedMatches.description",
								)}
							/>

							<div className="flex items-center gap-2">
								<div>
									<p className="text-sm font-medium text-gray-300">
										{t("settings.dataStorage.clearHistory")}
									</p>
									<p className="text-gray-500 text-xs mt-0.5">
										{t("settings.dataStorage.clearHistoryDescription", {
											count: getArenaHistoryCount(),
										})}
									</p>
								</div>
								<button
									type="button"
									onClick={() => {
										if (
											confirm(t("settings.dataStorage.clearHistoryConfirm"))
										) {
											clearArenaHistory();
											toast(
												t("settings.dataStorage.clearHistoryAllCleared"),
												"info",
											);
										}
									}}
									className="ui-btn ui-btn-danger text-xs px-3 py-1.5"
									disabled={getArenaHistoryCount() === 0}
									title={t("settings.dataStorage.clearHistoryAll.tooltip")}
								>
									{t("settings.dataStorage.clearHistoryAll")}
								</button>
							</div>
						</SettingsGroup>

						<SettingsGroup title={t("settings.dashboard.title")}>
							<SettingsSlider
								id="dashboard-refresh-interval"
								label={t("settings.dashboard.refreshInterval")}
								value={Number(refreshSec)}
								min={0}
								max={600}
								step={10}
								clampStep={10}
								infinityValue={0}
								unit="s"
								onChange={handleDashboardRefreshChange}
								description={t(
									"settings.dashboard.refreshInterval.description",
								)}
							/>
						</SettingsGroup>
					</div>
				</div>
			</div>
		</SettingsSection>
	);
}
