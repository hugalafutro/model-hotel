import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Database } from "@/lib/icons";
import { api } from "../../api/client";
import { SettingsGroup } from "../../components/SettingsGroup";
import { SettingsSection } from "../../components/SettingsSection";
import { SettingsSlider } from "../../components/SettingsSlider";
import { SettingToggleRow } from "../../components/SettingToggleRow";
import { useStorage } from "../../context/StorageContext";
import { useToast } from "../../context/ToastContext";
import {
	clearArenaHistory,
	getArenaHistoryCount,
} from "../../utils/arenaHistory";
import {
	goDurationToMinutes,
	hoursToGoDuration,
	logRetentionToDays,
	minutesToGoDuration,
} from "../../utils/duration";
import { clearProviderCache, getProviderCacheCount } from "./constants";
import { PurgeLogsControl } from "./PurgeLogsControl";
import { usePurgeState } from "./purgeState";
import { useSettingsMutations } from "./useSettingsMutations";

interface DataStorageSettingsProps {
	collapsed: boolean;
	onToggle: () => void;
	onResetSection?: () => void;
	managed?: boolean;
}

export function DataStorageSettings({
	collapsed,
	onToggle,
	onResetSection,
	managed,
}: DataStorageSettingsProps) {
	const { t } = useTranslation();
	const { toast } = useToast();
	const queryClient = useQueryClient();
	const { settings, updateMutation, resetSettingMutation } =
		useSettingsMutations();

	const [quotaDisabled, setQuotaDisabled] = useState(() => {
		try {
			return localStorage.getItem("sidebarQuotaDisabled") === "true";
		} catch {
			return false;
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

	const requestsPurge = usePurgeState();
	const appLogsPurge = usePurgeState();

	const purgeMutation = useMutation({
		mutationFn: (olderThan: string) => api.logs.purge(olderThan),
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: ["logs"] });
			toast(t("settings.common.requestsDeleted"), "success");
			requestsPurge.settled(true);
		},
		onError: (err: Error) => {
			toast(
				t("settings.common.failedToDeleteRequests", { message: err.message }),
				"error",
			);
			requestsPurge.settled(false);
		},
	});

	const purgeAppLogsMutation = useMutation({
		mutationFn: (olderThan: string) => api.appLogs.purge(olderThan),
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: ["appLogs"] });
			toast(t("settings.common.logsDeleted"), "success");
			appLogsPurge.settled(true);
		},
		onError: (err: Error) => {
			toast(
				t("settings.common.failedToDeleteAppLogs", { message: err.message }),
				"error",
			);
			appLogsPurge.settled(false);
		},
	});

	const logRetention = settings?.log_retention || "0";
	const staleRequestTimeout = settings?.stale_request_timeout || "30m0s";
	// Quota sidebar refresh interval is a server setting (minutes, 0 = off).
	const quotaRefreshMin = Number(settings?.quota_refresh_interval_min ?? 5);
	// The slider is in days; the backend stores a Go duration in hours.
	const logRetentionDays = logRetentionToDays(logRetention);
	const staleTimeoutMinutes = goDurationToMinutes(staleRequestTimeout);

	return (
		<SettingsSection
			icon={Database}
			title={t("settings.dataStorageAndLogging.title")}
			collapsed={collapsed}
			onToggle={onToggle}
			onResetSection={onResetSection}
			managed={managed}
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
								value={logRetentionDays}
								min={0}
								max={30}
								step={1}
								clampStep={1}
								infinityValue={0}
								unit="d"
								onChange={(v) =>
									updateMutation.mutate({
										log_retention: hoursToGoDuration(v * 24),
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
								<PurgeLogsControl
									mutation={purgeMutation}
									state={requestsPurge}
									labels={{
										button: t("settings.logging.deleteRequests"),
										tooltip: t("settings.logging.deleteRequests.tooltip"),
										selectRange: t(
											"settings.logging.deleteRequests.selectRange",
										),
										olderThan1d: t(
											"settings.logging.deleteRequests.olderThan1d",
										),
										olderThan1w: t(
											"settings.logging.deleteRequests.olderThan1w",
										),
										olderThan1m: t(
											"settings.logging.deleteRequests.olderThan1m",
										),
										allLogs: t("settings.logging.deleteRequests.allLogs"),
										confirm: t("settings.logging.deleteRequests.confirm"),
										cancel: t("settings.logging.deleteRequests.cancel"),
									}}
								/>

								<PurgeLogsControl
									mutation={purgeAppLogsMutation}
									state={appLogsPurge}
									labels={{
										button: t("settings.logging.deleteAppLogs"),
										tooltip: t("settings.logging.deleteAppLogs.tooltip"),
										selectRange: t(
											"settings.logging.deleteAppLogs.selectRange",
										),
										olderThan1d: t(
											"settings.logging.deleteAppLogs.olderThan1d",
										),
										olderThan1w: t(
											"settings.logging.deleteAppLogs.olderThan1w",
										),
										olderThan1m: t(
											"settings.logging.deleteAppLogs.olderThan1m",
										),
										allLogs: t("settings.logging.deleteAppLogs.allLogs"),
										confirm: t("settings.logging.deleteAppLogs.confirm"),
										cancel: t("settings.logging.deleteAppLogs.cancel"),
										deleting: t("settings.logging.deleteAppLogs.deleting"),
									}}
								/>
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
									className="ui-btn ui-btn-danger"
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
									className="ui-btn ui-btn-danger"
									title={t("settings.dataStorage.reset.tooltip")}
								>
									{t("settings.dataStorage.reset")}
								</button>
							</div>
						</SettingsGroup>

						<SettingsGroup title={t("settings.dataStorage.quotaBadges")}>
							<SettingToggleRow
								label={t("settings.sidebarQuota.showQuotasPill")}
								description={t(
									"settings.sidebarQuota.showQuotasPillDescription",
								)}
								checked={!quotaDisabled}
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

							<SettingsSlider
								id="quota-refresh-interval"
								label={t("settings.sidebarQuota.refreshInterval")}
								value={quotaRefreshMin}
								min={0}
								max={30}
								step={1}
								clampStep={1}
								// Deliberately NOT infinityValue={0}: 0 turns this OFF, it does not
								// lift a limit, and ∞ read as the opposite. Same defect as the TTFT
								// probe slider.
								unit="m"
								disabled={quotaDisabled}
								onChange={(v) => {
									updateMutation.mutate({
										quota_refresh_interval_min: String(v),
									});
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
							<SettingToggleRow
								label={t("settings.dataStorage.persistChat")}
								description={t("settings.dataStorage.persistChatDescription")}
								checked={persistChat}
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

							<SettingToggleRow
								label={t("settings.dataStorage.persistArena")}
								description={t("settings.dataStorage.persistArenaDescription")}
								checked={persistArena}
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

							<SettingToggleRow
								label={t("settings.dataStorage.persistConversation")}
								description={t(
									"settings.dataStorage.persistConversationDescription",
								)}
								checked={persistConversation}
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
						</SettingsGroup>

						<SettingsGroup title={t("settings.dataStorage.arenaHistory")}>
							<SettingToggleRow
								label={t("settings.dataStorage.saveMatchHistory")}
								description={t(
									"settings.dataStorage.saveMatchHistoryDescription",
								)}
								checked={arenaHistoryEnabled}
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
									className="ui-btn ui-btn-danger"
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
								// Deliberately NOT infinityValue={0}: 0 turns this OFF, it does not
								// lift a limit, and ∞ read as the opposite. Same defect as the TTFT
								// probe slider.
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
