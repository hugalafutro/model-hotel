import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Settings as SettingsIcon } from "@/lib/icons";
import { api } from "../api/client";
import { useCollapsible } from "../components/CollapsibleToggle";
import { ConfirmDialog } from "../components/ConfirmDialog";
import { LoadingSpinner } from "../components/LoadingSpinner";
import { Modal } from "../components/Modal";
import { PageHeader } from "../components/PageHeader";
import { ResetButton } from "../components/ResetButton";
import { useToast } from "../context/ToastContext";
import { AppearanceSettings } from "./Settings/AppearanceSettings";
import { CircuitBreakerSettings } from "./Settings/CircuitBreakerSettings";
import { DatabaseBackupSettings } from "./Settings/DatabaseBackupSettings";
import { DataStorageSettings } from "./Settings/DataStorageSettings";
import { DiscoverySettings } from "./Settings/DiscoverySettings";
import {
	SECTION_SETTINGS,
	SETTING_LABELS,
	type SettingKey,
} from "./Settings/defaults";
import { PasskeySettings } from "./Settings/PasskeySettings";
import { ProxySettings } from "./Settings/ProxySettings";
import { RateLimitSettings } from "./Settings/RateLimitSettings";

export function Settings() {
	const { t } = useTranslation();
	const { toast } = useToast();
	const queryClient = useQueryClient();

	const { collapsed: modelDiscoveryCollapsed, toggle: toggleModelDiscovery } =
		useCollapsible("settings_modelDiscoveryCollapsed");
	const { collapsed: appearanceCollapsed, toggle: toggleAppearance } =
		useCollapsible("settings_appearanceCollapsed");
	const { collapsed: dataStorageCollapsed, toggle: toggleDataStorage } =
		useCollapsible("settings_dataStorageCollapsed");
	const { collapsed: backupCollapsed, toggle: toggleBackup } = useCollapsible(
		"settings_backupCollapsed",
	);
	const { collapsed: rateLimitCollapsed, toggle: toggleRateLimit } =
		useCollapsible("settings_rateLimitCollapsed");
	const { collapsed: circuitBreakerCollapsed, toggle: toggleCircuitBreaker } =
		useCollapsible("settings_circuitBreakerCollapsed");
	const { collapsed: proxyCollapsed, toggle: toggleProxy } = useCollapsible(
		"settings_proxyCollapsed",
	);
	const { collapsed: passkeyCollapsed, toggle: togglePasskey } = useCollapsible(
		"settings_passkeyCollapsed",
	);

	const { isLoading } = useQuery({
		queryKey: ["settings"],
		queryFn: () => api.settings.get(),
	});

	// --- Reset all settings (double-confirm: type RESET) ---
	const [resetAllOpen, setResetAllOpen] = useState(false);
	const [resetAllConfirmText, setResetAllConfirmText] = useState("");

	const resetAllMutation = useMutation({
		mutationFn: () => api.settings.reset(),
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: ["settings"] });
			toast(t("settings.common.resetAllDone"), "success");
			setResetAllOpen(false);
			setResetAllConfirmText("");
		},
		onError: (err: Error) => {
			toast(
				t("settings.common.resetFailed", { message: err.message }),
				"error",
			);
			setResetAllOpen(false);
			setResetAllConfirmText("");
		},
	});

	// --- Reset section (single confirm) ---
	const [resetSection, setResetSection] = useState<
		keyof typeof SECTION_SETTINGS | null
	>(null);

	const resetSectionMutation = useMutation({
		mutationFn: (keys: string[]) => api.settings.reset(keys),
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: ["settings"] });
			toast(t("settings.common.resetSectionDone"), "success");
			setResetSection(null);
		},
		onError: (err: Error) => {
			toast(
				t("settings.common.resetFailed", { message: err.message }),
				"error",
			);
			setResetSection(null);
		},
	});

	if (isLoading) {
		return <LoadingSpinner />;
	}

	return (
		<div className="space-y-8 max-w-5xl pb-8">
			<div className="flex items-start justify-between">
				<PageHeader
					icon={SettingsIcon}
					title={t("settings.title")}
					description={t("settings.description")}
				/>
				<ResetButton
					tooltip={t("settings.common.resetAllSettings")}
					onClick={() => setResetAllOpen(true)}
					size={18}
					className="mt-2"
				/>
			</div>

			<div className="space-y-6">
				<DiscoverySettings
					collapsed={modelDiscoveryCollapsed}
					onToggle={toggleModelDiscovery}
					onResetSection={() => setResetSection("discovery")}
				/>

				<PasskeySettings
					collapsed={passkeyCollapsed}
					onToggle={togglePasskey}
				/>

				<AppearanceSettings
					collapsed={appearanceCollapsed}
					onToggle={toggleAppearance}
				/>

				<DataStorageSettings
					collapsed={dataStorageCollapsed}
					onToggle={toggleDataStorage}
					onResetSection={() => setResetSection("dataStorage")}
				/>

				<DatabaseBackupSettings
					collapsed={backupCollapsed}
					onToggle={toggleBackup}
				/>

				<RateLimitSettings
					collapsed={rateLimitCollapsed}
					onToggle={toggleRateLimit}
					onResetSection={() => setResetSection("rateLimit")}
				/>

				<CircuitBreakerSettings
					collapsed={circuitBreakerCollapsed}
					onToggle={toggleCircuitBreaker}
					onResetSection={() => setResetSection("circuitBreaker")}
				/>

				<ProxySettings
					collapsed={proxyCollapsed}
					onToggle={toggleProxy}
					onResetSection={() => setResetSection("proxy")}
				/>
			</div>

			{/* Double-confirm: type RESET to reset all */}
			{resetAllOpen && (
				<Modal
					title={t("settings.common.resetAllConfirmTitle")}
					onClose={() => {
						setResetAllOpen(false);
						setResetAllConfirmText("");
					}}
					maxWidth="max-w-sm"
				>
					<p className="text-sm text-amber-400 mb-3">
						{t("settings.common.resetAllConfirmMessage")}
					</p>
					<input
						type="text"
						value={resetAllConfirmText}
						onChange={(e) => setResetAllConfirmText(e.target.value)}
						placeholder={t("settings.common.resetAllConfirmField")}
						className="w-full px-3 py-2 bg-gray-900 border border-gray-600 rounded text-(--text-primary) placeholder-gray-400 focus:outline-none focus:border-amber-500 mb-4"
					/>
					<div className="flex gap-3 justify-end">
						<button
							type="button"
							onClick={() => {
								setResetAllOpen(false);
								setResetAllConfirmText("");
							}}
							className="ui-btn ui-btn-secondary"
						>
							{t("common.cancel")}
						</button>
						<button
							type="button"
							disabled={
								resetAllConfirmText !== "RESET" || resetAllMutation.isPending
							}
							onClick={() => resetAllMutation.mutate()}
							className="ui-btn ui-btn-danger"
						>
							{t("settings.common.resetToDefaults")}
						</button>
					</div>
				</Modal>
			)}

			{/* Section reset: single confirm */}
			{resetSection && (
				<ConfirmDialog
					title={t("settings.common.resetSectionConfirmTitle")}
					message={t("settings.common.resetSectionConfirmMessage")}
					fields={SECTION_SETTINGS[resetSection].map((k) =>
						t(SETTING_LABELS[k as SettingKey] ?? k),
					)}
					confirmLabel={t("settings.common.resetToDefaults")}
					onConfirm={() =>
						resetSectionMutation.mutate(SECTION_SETTINGS[resetSection])
					}
					onCancel={() => setResetSection(null)}
				/>
			)}
		</div>
	);
}
