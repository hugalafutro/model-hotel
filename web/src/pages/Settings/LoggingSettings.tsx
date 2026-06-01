import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ScrollText } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { api } from "../../api/client";
import { SettingsSection } from "../../components/SettingsSection";
import { SettingsSelect } from "../../components/SettingsSelect";
import { useToast } from "../../context/ToastContext";

interface LoggingSettingsProps {
	collapsed: boolean;
	onToggle: () => void;
}

export function LoggingSettings({ collapsed, onToggle }: LoggingSettingsProps) {
	const { t } = useTranslation();
	const { toast } = useToast();
	const queryClient = useQueryClient();
	const [confirmDelete, setConfirmDelete] = useState(false);
	const [deleteSelection, setDeleteSelection] = useState("");
	const [confirmDeleteAppLogs, setConfirmDeleteAppLogs] = useState(false);

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
			icon={ScrollText}
			title={t("settings.logging.title")}
			collapsed={collapsed}
			onToggle={onToggle}
		>
			<div className="space-y-5">
				<div className="grid grid-cols-2 gap-x-8 gap-y-5">
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
						<div className="flex items-center justify-between">
							<div>
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
							</div>
							<div>
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
				</div>
			</div>
		</SettingsSection>
	);
}
