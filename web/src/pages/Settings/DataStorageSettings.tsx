import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Database } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { api } from "../../api/client";
import { SettingsSection } from "../../components/SettingsSection";
import { SettingsSelect } from "../../components/SettingsSelect";
import { Toggle } from "../../components/Toggle";
import { useStorage } from "../../context/StorageContext";
import { useToast } from "../../context/ToastContext";
import {
	clearArenaHistory,
	getArenaHistoryCount,
} from "../../utils/arenaHistory";
import {
	clearProviderCache,
	getProviderCacheCount,
	getProviderCacheNames,
} from "./constants";

interface DataStorageSettingsProps {
	collapsed: boolean;
	onToggle: () => void;
}

export function DataStorageSettings({
	collapsed,
	onToggle,
}: DataStorageSettingsProps) {
	const { t } = useTranslation();
	const { toast } = useToast();
	const queryClient = useQueryClient();
	const [confirmDelete, setConfirmDelete] = useState(false);
	const [deleteSelection, setDeleteSelection] = useState("");
	const [confirmDeleteAppLogs, setConfirmDeleteAppLogs] = useState(false);

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

	const { data: settings } = useQuery({
		queryKey: ["settings"],
		queryFn: () => api.settings.get(),
	});

	const updateMutation = useMutation({
		mutationFn: (updates: Record<string, string>) =>
			api.settings.update(updates),
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: ["settings"] });
			toast(t("settings.common.settingsSaved"), "success");
		},
		onError: (err: Error) => {
			toast(
				t("settings.common.failedToSave", { message: err.message }),
				"error",
			);
		},
	});

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
		mutationFn: () => api.appLogs.purge(),
		onSuccess: (data) => {
			queryClient.invalidateQueries({ queryKey: ["appLogs"] });
			toast(
				t("settings.common.entriesDeleted", { count: data.deleted }),
				"success",
			);
			setConfirmDeleteAppLogs(false);
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

	const getDeleteOlderThan = (selection: string): string => {
		switch (selection) {
			case "1d":
				return "24h";
			case "1w":
				return "168h";
			case "1m":
				return "720h";
			case "all":
				return "all";
			default:
				return "";
		}
	};

	const LOG_RETENTION_OPTIONS = [
		{ value: "0", label: t("settings.logging.retention.disabled") },
		{ value: "24h", label: t("settings.logging.retention.24h") },
		{ value: "168h", label: t("settings.logging.retention.168h") },
		{ value: "720h", label: t("settings.logging.retention.720h") },
	];

	const STALE_REQUEST_TIMEOUT_OPTIONS = [
		{ value: "5m0s", label: t("settings.logging.staleTimeout.5m0s") },
		{ value: "10m0s", label: t("settings.logging.staleTimeout.10m0s") },
		{ value: "15m0s", label: t("settings.logging.staleTimeout.15m0s") },
		{ value: "30m0s", label: t("settings.logging.staleTimeout.30m0s") },
		{ value: "1h0m0s", label: t("settings.logging.staleTimeout.1h0m0s") },
		{ value: "2h0m0s", label: t("settings.logging.staleTimeout.2h0m0s") },
		{ value: "0s", label: t("settings.logging.staleTimeout.disabled") },
	];

	return (
		<SettingsSection
			icon={Database}
			title={t("settings.dataStorageAndLogging.title")}
			collapsed={collapsed}
			onToggle={onToggle}
		>
			<div className="space-y-5">
				<p className="text-gray-400 text-sm">
					{t("settings.dataStorage.description")}
				</p>

				{/* Session Persistence */}
				<div>
					<h3 className="text-xs font-semibold uppercase tracking-wider text-gray-500 mb-3">
						{t("settings.dataStorage.sessionPersistence")}
					</h3>
					<div className="grid grid-cols-2 gap-x-8 gap-y-5 [align-items:start]">
						<div className="space-y-5">
							<div className="flex items-center justify-between">
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

							<div className="flex items-center justify-between">
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

							<div className="flex items-center justify-between">
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
						</div>
						<div className="space-y-5">
							<div className="flex items-center justify-between">
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

							<SettingsSelect
								id="quota-refresh-interval"
								label={t("settings.sidebarQuota.refreshInterval")}
								value={refreshMin}
								options={[
									{ value: "1", label: t("settings.sidebarQuota.intervals.1") },
									{ value: "2", label: t("settings.sidebarQuota.intervals.2") },
									{ value: "5", label: t("settings.sidebarQuota.intervals.5") },
									{
										value: "10",
										label: t("settings.sidebarQuota.intervals.10"),
									},
									{
										value: "15",
										label: t("settings.sidebarQuota.intervals.15"),
									},
									{
										value: "30",
										label: t("settings.sidebarQuota.intervals.30"),
									},
									{
										value: "0",
										label: t("settings.sidebarQuota.intervals.disabled"),
									},
								]}
								onChange={(val) => {
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
										val === "0"
											? t("settings.sidebarQuota.disabled")
											: t("settings.sidebarQuota.intervalSet", {
													minutes: val,
													count: Number(val),
												}),
										"success",
									);
								}}
								disabled={quotaDisabled}
								inline
								description={t(
									"settings.sidebarQuota.refreshInterval.description",
								)}
							/>
						</div>
					</div>
				</div>

				{/* Arena History */}
				<div>
					<h3 className="text-xs font-semibold uppercase tracking-wider text-gray-500 mb-3">
						{t("settings.dataStorage.arenaHistory")}
					</h3>
					<div className="space-y-5">
						<div className="flex items-center justify-between">
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

						<SettingsSelect
							id="history-limit"
							label={t("settings.dataStorage.maxSavedMatches")}
							value={String(arenaHistoryLimit)}
							options={[
								{
									value: "10",
									label: t("settings.dataStorage.matches.10"),
								},
								{
									value: "25",
									label: t("settings.dataStorage.matches.25"),
								},
								{
									value: "50",
									label: t("settings.dataStorage.matches.50"),
								},
								{
									value: "100",
									label: t("settings.dataStorage.matches.100"),
								},
							]}
							onChange={(v) => {
								const val = Number(v);
								setArenaHistoryLimit(val);
								toast(
									t("settings.dataStorage.historyLimitToast", { count: val }),
									"success",
								);
							}}
							disabled={!arenaHistoryEnabled}
							description={t(
								"settings.dataStorage.maxSavedMatches.description",
							)}
						/>

						<div className="flex items-center justify-between">
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
									if (confirm(t("settings.dataStorage.clearHistoryConfirm"))) {
										clearArenaHistory();
										toast(
											t("settings.dataStorage.clearHistoryAllCleared"),
											"info",
										);
									}
								}}
								className="ui-btn ui-btn-danger text-xs px-3 py-1.5"
								disabled={getArenaHistoryCount() === 0}
							>
								{t("settings.dataStorage.clearHistoryAll")}
							</button>
						</div>
					</div>
				</div>

				{/* Logging */}
				<div>
					<h3 className="text-xs font-semibold uppercase tracking-wider text-gray-500 mb-3">
						{t("settings.logging.title")}
					</h3>
					<div className="grid grid-cols-2 gap-x-8 gap-y-5 [align-items:start]">
						<div className="space-y-5">
							<SettingsSelect
								id="log-retention"
								label={t("settings.logging.logRetention")}
								value={logRetention}
								options={LOG_RETENTION_OPTIONS}
								onChange={(v) => updateMutation.mutate({ log_retention: v })}
								description={
									logRetention === "0" ? (
										<span className="text-amber-400">
											{t("settings.logging.logRetention.disabled")}
										</span>
									) : (
										t("settings.logging.logRetention.description")
									)
								}
							/>

							<SettingsSelect
								id="stale-request-timeout"
								label={t("settings.logging.staleRequestTimeout")}
								value={staleRequestTimeout}
								options={STALE_REQUEST_TIMEOUT_OPTIONS}
								onChange={(v) =>
									updateMutation.mutate({ stale_request_timeout: v })
								}
								description={
									staleRequestTimeout === "0s" ? (
										<span className="text-amber-400">
											{t("settings.logging.staleRequestTimeout.disabled")}
										</span>
									) : (
										t("settings.logging.staleRequestTimeout.description")
									)
								}
							/>
						</div>
						<div className="space-y-5">
							{!confirmDelete ? (
								<button
									type="button"
									onClick={() => setConfirmDelete(true)}
									className="ui-btn ui-btn-danger"
								>
									{t("settings.logging.deleteRequests")}
								</button>
							) : (
								<div className="flex items-center gap-2">
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
								</div>
							)}

							{!confirmDeleteAppLogs ? (
								<button
									type="button"
									onClick={() => setConfirmDeleteAppLogs(true)}
									className="ui-btn ui-btn-danger"
								>
									{t("settings.logging.deleteAppLogs")}
								</button>
							) : (
								<div className="flex items-center gap-2">
									<span className="text-xs text-red-400">
										{t("settings.logging.deleteAppLogs.confirmText")}
									</span>
									<button
										type="button"
										onClick={() => purgeAppLogsMutation.mutate()}
										disabled={purgeAppLogsMutation.isPending}
										className="ui-btn ui-btn-danger disabled:opacity-50 disabled:cursor-not-allowed"
									>
										{purgeAppLogsMutation.isPending
											? t("settings.logging.deleteAppLogs.deleting")
											: t("settings.logging.deleteAppLogs.confirm")}
									</button>
									<button
										type="button"
										onClick={() => setConfirmDeleteAppLogs(false)}
										className="ui-btn ui-btn-secondary"
									>
										{t("settings.logging.deleteAppLogs.cancel")}
									</button>
								</div>
							)}
						</div>
					</div>
				</div>

				{/* Cache & Resets */}
				<div>
					<h3 className="text-xs font-semibold uppercase tracking-wider text-gray-500 mb-3">
						{t("settings.dataStorage.cacheAndResets")}
					</h3>
					<div className="grid grid-cols-2 gap-x-8 gap-y-5 [align-items:start]">
						<div className="space-y-5">
							<div>
								<p className="text-sm font-medium text-gray-300">
									{t("settings.dataStorage.providerQuotaCache")}
								</p>
								<p className="text-gray-500 text-xs mt-0.5">
									{t("settings.dataStorage.providerQuotaCacheDescription", {
										count: getProviderCacheCount(),
										providers: getProviderCacheNames().join(", "),
									})}
								</p>
							</div>

							<div>
								<p className="text-sm font-medium text-gray-300">
									{t("settings.dataStorage.dismissedErrorBanners")}
								</p>
								<p className="text-gray-500 text-xs mt-0.5">
									{t("settings.dataStorage.dismissedErrorBannersDescription")}
								</p>
							</div>
						</div>

						<div className="space-y-5">
							<button
								type="button"
								onClick={() => {
									if (confirm(t("settings.dataStorage.clearCacheConfirm"))) {
										clearProviderCache();
										toast(t("settings.dataStorage.clearCacheCleared"), "info");
									}
								}}
								className="ui-btn ui-btn-danger text-xs px-3 py-1.5"
								disabled={getProviderCacheCount() === 0}
							>
								{t("settings.dataStorage.clearCache")}
							</button>

							<button
								type="button"
								onClick={() => {
									localStorage.removeItem("dismissedAppErrorKey");
									localStorage.removeItem("dismissedReqErrorKey");
									window.dispatchEvent(new CustomEvent("dismissedErrorsReset"));
									toast(
										t("settings.dataStorage.resetDismissedBanners"),
										"info",
									);
								}}
								className="ui-btn ui-btn-danger text-xs px-3 py-1.5"
							>
								{t("settings.dataStorage.reset")}
							</button>
						</div>
					</div>
				</div>
			</div>
		</SettingsSection>
	);
}
