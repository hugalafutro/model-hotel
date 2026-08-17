import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Settings as SettingsIcon } from "@/lib/icons";
import { api } from "../api/client";
import { useCollapsible } from "../components/CollapsibleToggle";
import { ConfirmDialog } from "../components/ConfirmDialog";
import { LoadingSpinner } from "../components/LoadingSpinner";
import { ManagedBanner } from "../components/ManagedBanner";
import { Modal } from "../components/Modal";
import { PageHeader } from "../components/PageHeader";
import { ResetButton } from "../components/ResetButton";
import { useToast } from "../context/ToastContext";
import { useManaged } from "../hooks/useManaged";
import { AlertsSettings } from "./Settings/AlertsSettings";
import { AppearanceSettings } from "./Settings/AppearanceSettings";
import { AuthenticationSettings } from "./Settings/AuthenticationSettings";
import { CircuitBreakerSettings } from "./Settings/CircuitBreakerSettings";
import { DatabaseBackupSettings } from "./Settings/DatabaseBackupSettings";
import { DataStorageSettings } from "./Settings/DataStorageSettings";
import { DiscoverySettings } from "./Settings/DiscoverySettings";
import {
	SECTION_SETTINGS,
	SETTING_LABELS,
	type SettingKey,
} from "./Settings/defaults";
import { ObservabilitySettings } from "./Settings/ObservabilitySettings";
import { ProxySettings } from "./Settings/ProxySettings";
import { RateLimitSettings } from "./Settings/RateLimitSettings";
import { invalidateAlertReads } from "./Settings/useSettingsMutations";

/**
 * Double-confirm dialog for "reset every setting": the operator has to type
 * RESET before the destructive button unlocks.
 *
 * The typed text is state *here*, not on `Settings`. It used to live on the
 * page root, which meant every single keystroke re-rendered all ten settings
 * sections (~26 range inputs plus everything else) just to re-render one text
 * field. That is wasted work in the browser, and under v8 coverage
 * instrumentation it was slow enough that the reset tests blew the per-test
 * timeout. Keeping the field's state local re-renders only this dialog.
 *
 * The dialog is unmounted by its parent when closed, so the field resets
 * itself on the next open and no explicit clearing is needed.
 */
function ResetAllDialog({
	pending,
	onConfirm,
	onClose,
}: {
	pending: boolean;
	onConfirm: () => void;
	onClose: () => void;
}) {
	const { t } = useTranslation();
	const [confirmText, setConfirmText] = useState("");

	return (
		<Modal
			title={t("settings.common.resetAllConfirmTitle")}
			onClose={onClose}
			maxWidth="max-w-sm"
		>
			<p className="text-sm text-amber-400 mb-3">
				{t("settings.common.resetAllConfirmMessage")}
			</p>
			<input
				type="text"
				value={confirmText}
				onChange={(e) => setConfirmText(e.target.value)}
				placeholder={t("settings.common.resetAllConfirmField")}
				className="w-full px-3 py-2 bg-gray-900 border border-gray-600 rounded text-(--text-primary) placeholder-gray-400 focus:outline-none focus:border-amber-500 mb-4"
			/>
			<div className="flex gap-3 justify-end">
				<button
					type="button"
					onClick={onClose}
					className="ui-btn ui-btn-secondary"
				>
					{t("common.cancel")}
				</button>
				<button
					type="button"
					disabled={confirmText !== "RESET" || pending}
					onClick={onConfirm}
					className="ui-btn ui-btn-danger"
				>
					{t("settings.common.resetToDefaults")}
				</button>
			</div>
		</Modal>
	);
}

export function Settings() {
	const managed = useManaged();
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
	const { collapsed: authenticationCollapsed, toggle: toggleAuthentication } =
		useCollapsible("settings_authenticationCollapsed");
	const { collapsed: observabilityCollapsed, toggle: toggleObservability } =
		useCollapsible("settings_observabilityCollapsed");
	const { collapsed: alertsCollapsed, toggle: toggleAlerts } = useCollapsible(
		"settings_alertsCollapsed",
	);

	const { isLoading } = useQuery({
		queryKey: ["settings"],
		queryFn: () => api.settings.get(),
	});

	// --- Reset all settings (double-confirm: type RESET) ---
	const [resetAllOpen, setResetAllOpen] = useState(false);

	const resetAllMutation = useMutation({
		mutationFn: () => api.settings.reset(),
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: ["settings"] });
			invalidateAlertReads(queryClient);
			toast(t("settings.common.resetAllDone"), "success");
			setResetAllOpen(false);
		},
		onError: (err: Error) => {
			toast(
				t("settings.common.resetFailed", { message: err.message }),
				"error",
			);
			setResetAllOpen(false);
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
			invalidateAlertReads(queryClient);
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
		<div className="space-y-8 pb-8">
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
				<AuthenticationSettings
					collapsed={authenticationCollapsed}
					onToggle={toggleAuthentication}
					managed={managed}
				/>

				<AppearanceSettings
					collapsed={appearanceCollapsed}
					onToggle={toggleAppearance}
				/>

				<ObservabilitySettings
					collapsed={observabilityCollapsed}
					onToggle={toggleObservability}
				/>

				<AlertsSettings
					collapsed={alertsCollapsed}
					onToggle={toggleAlerts}
					// Alerts is a mixed section: it does not forward `managed` to
					// SettingsSection (that would disable the instance-local Apprise
					// inputs too). Drop the section reset while managed so the header
					// matches the six fully-synced sections, which hide theirs.
					onResetSection={managed ? undefined : () => setResetSection("alerts")}
					managed={managed}
				/>

				{/* Sections above are local to this instance (Authentication and
				    Alerts partially, each behind its own in-section note); every
				    section below is owned by the fleet primary, so the managed
				    banner sits on the boundary and its "configuration below"
				    claim reads true. On unmanaged instances it renders
				    nothing. */}
				<ManagedBanner />

				<DiscoverySettings
					collapsed={modelDiscoveryCollapsed}
					onToggle={toggleModelDiscovery}
					onResetSection={() => setResetSection("discovery")}
					managed={managed}
				/>

				<DataStorageSettings
					collapsed={dataStorageCollapsed}
					onToggle={toggleDataStorage}
					onResetSection={() => setResetSection("dataStorage")}
					managed={managed}
				/>

				<DatabaseBackupSettings
					collapsed={backupCollapsed}
					onToggle={toggleBackup}
					managed={managed}
				/>

				<RateLimitSettings
					collapsed={rateLimitCollapsed}
					onToggle={toggleRateLimit}
					onResetSection={() => setResetSection("rateLimit")}
					managed={managed}
				/>

				<CircuitBreakerSettings
					collapsed={circuitBreakerCollapsed}
					onToggle={toggleCircuitBreaker}
					onResetSection={() => setResetSection("circuitBreaker")}
					managed={managed}
				/>

				<ProxySettings
					collapsed={proxyCollapsed}
					onToggle={toggleProxy}
					onResetSection={() => setResetSection("proxy")}
					managed={managed}
				/>
			</div>

			{/* Double-confirm: type RESET to reset all */}
			{resetAllOpen && (
				<ResetAllDialog
					pending={resetAllMutation.isPending}
					onConfirm={() => resetAllMutation.mutate()}
					onClose={() => setResetAllOpen(false)}
				/>
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
