import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Database } from "@/lib/icons";
import { api } from "../../api/client";
import { ACKED_KEYS_STORAGE } from "../../components/ErrorShelf/useErrorShelf";
import { SettingsGroup } from "../../components/SettingsGroup";
import { SettingsSection } from "../../components/SettingsSection";
import { SettingsSlider } from "../../components/SettingsSlider";
import { SettingToggleRow } from "../../components/SettingToggleRow";
import { useStorage } from "../../context/StorageContext";
import { useToast } from "../../context/ToastContext";
import { storedBool, useLocalStorage } from "../../hooks/useLocalStorage";
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
import { settingOr } from "./defaults";
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

	// Both are browser-local preferences other screens read with
	// useLocalStorageValue; the hook's write-through setter announces each
	// change on "localStorageChange", which is what those readers subscribe to.
	const [quotaDisabled, setQuotaDisabled] = useLocalStorage(
		"sidebarQuotaDisabled",
		false,
		{ deserialize: storedBool },
	);
	const [refreshSec, setRefreshSec] = useLocalStorage(
		"dashboardRefreshSec",
		30,
		{ deserialize: Number },
	);

	const handleDashboardRefreshChange = (val: number) => {
		setRefreshSec(val);
		toast(
			val === 0
				? t("settings.dashboard.disabled")
				: t("settings.dashboard.intervalSet", {
						seconds: String(val),
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

	// The three session-persistence switches differ only in their state pair and
	// their i18n stem; every suffix (Description/Confirm/Enabled/Disabled) is the
	// same under each.
	const persistenceToggles = [
		{ stem: "persistChat", value: persistChat, set: setPersistChat },
		{ stem: "persistArena", value: persistArena, set: setPersistArena },
		{
			stem: "persistConversation",
			value: persistConversation,
			set: setPersistConversation,
		},
	];

	// Read once at mount and again after a clear, rather than rescanning
	// localStorage twice per render for the description and the disabled flag.
	const [cacheCount, setCacheCount] = useState(getProviderCacheCount);
	const [historyCount, setHistoryCount] = useState(getArenaHistoryCount);

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

	const logRetention = settingOr(settings, "log_retention");
	const staleRequestTimeout = settingOr(settings, "stale_request_timeout");
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
									i18nStem="settings.logging.deleteRequests"
									mutation={purgeMutation}
									state={requestsPurge}
								/>

								<PurgeLogsControl
									i18nStem="settings.logging.deleteAppLogs"
									mutation={purgeAppLogsMutation}
									state={appLogsPurge}
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
											count: cacheCount,
										})}
									</p>
								</div>
								<button
									type="button"
									onClick={() => {
										if (confirm(t("settings.dataStorage.clearCacheConfirm"))) {
											clearProviderCache();
											setCacheCount(0);
											toast(
												t("settings.dataStorage.clearCacheCleared"),
												"info",
											);
										}
									}}
									className="ui-btn ui-btn-danger"
									disabled={cacheCount === 0}
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
										try {
											localStorage.removeItem(ACKED_KEYS_STORAGE);
										} catch {
											/* ignore */
										}
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
									setQuotaDisabled(!v);
									toast(
										v
											? t("settings.sidebarQuota.enabledQuotas")
											: t("settings.sidebarQuota.disabledQuotas"),
										v ? "success" : "info",
									);
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
							{persistenceToggles.map(({ stem, value, set }) => (
								<SettingToggleRow
									key={stem}
									label={t(`settings.dataStorage.${stem}`)}
									description={t(`settings.dataStorage.${stem}Description`)}
									checked={value}
									onChange={(v) => {
										// Switching one off drops what is already stored, so the
										// confirm guards the destructive direction only.
										if (
											!v &&
											!confirm(t(`settings.dataStorage.${stem}Confirm`))
										)
											return;
										set(v);
										toast(
											t(
												`settings.dataStorage.${stem}${v ? "Enabled" : "Disabled"}`,
											),
											v ? "success" : "info",
										);
									}}
								/>
							))}
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
											count: historyCount,
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
											setHistoryCount(0);
											toast(
												t("settings.dataStorage.clearHistoryAllCleared"),
												"info",
											);
										}
									}}
									className="ui-btn ui-btn-danger"
									disabled={historyCount === 0}
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
								value={refreshSec}
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
