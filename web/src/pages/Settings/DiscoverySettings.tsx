import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { Play, Search } from "@/lib/icons";
import { api } from "../../api/client";
import { ResetButton } from "../../components/ResetButton";
import { SettingsSection } from "../../components/SettingsSection";
import { SettingsSlider } from "../../components/SettingsSlider";
import { Spinner } from "../../components/Spinner";
import { Toggle } from "../../components/Toggle";
import { useToast } from "../../context/ToastContext";
import { goDurationToHours, hoursToGoDuration } from "../../utils/duration";
import { useSettingsMutations } from "./useSettingsMutations";

interface DiscoverySettingsProps {
	collapsed: boolean;
	onToggle: () => void;
	onResetSection?: () => void;
}

export function DiscoverySettings({
	collapsed,
	onToggle,
	onResetSection,
}: DiscoverySettingsProps) {
	const { t } = useTranslation();
	const { toast } = useToast();
	const queryClient = useQueryClient();

	const { settings, updateMutation, resetSettingMutation, isResetting } =
		useSettingsMutations();

	const discoverAllMutation = useMutation({
		mutationFn: () => api.providers.discoverAll(),
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: ["providers"] });
			queryClient.invalidateQueries({ queryKey: ["models"] });
			toast(t("settings.discovery.discoverAllComplete"), "success");
		},
		onError: (err: Error) => {
			toast(
				t("settings.discovery.discoverAllFailed", { message: err.message }),
				"error",
			);
		},
	});

	const isUpdating = updateMutation.isPending || discoverAllMutation.isPending;

	const discoveryIntervalHours = goDurationToHours(
		settings?.discovery_interval || "6h",
	);
	const discoveryOnStartup = settings?.discovery_on_startup !== "false";
	const discoveryOnCreate = settings?.discovery_on_provider_create !== "false";

	return (
		<SettingsSection
			icon={Search}
			title={t("settings.discovery.title")}
			collapsed={collapsed}
			onToggle={onToggle}
			onResetSection={onResetSection}
		>
			<div className="space-y-5">
				<p className="text-gray-400 text-sm col-span-2">
					{t("settings.discovery.description")}
				</p>
				<div className="grid grid-cols-2 gap-x-8 gap-y-5 [align-items:start]">
					<div className="space-y-5">
						<div className="flex items-center justify-between p-3 ui-detail-tile">
							<div>
								<div className="flex items-center gap-1">
									<p className="text-sm font-medium text-gray-300">
										{t("settings.discovery.discoverOnStartup")}
									</p>
									<ResetButton
										tooltip={t("settings.common.resetSetting")}
										onClick={() =>
											resetSettingMutation.mutate(["discovery_on_startup"])
										}
										size={12}
										disabled={isResetting}
									/>
								</div>
								<p className="text-gray-500 text-xs mt-0.5">
									{t("settings.discovery.discoverOnStartupDescription")}
								</p>
							</div>
							<Toggle
								checked={discoveryOnStartup}
								size="sm"
								onChange={(v) =>
									updateMutation.mutate({
										discovery_on_startup: v ? "true" : "false",
									})
								}
								disabled={isUpdating}
								ariaLabel={t("settings.discovery.discoverOnStartup")}
							/>
						</div>

						<div className="flex items-center justify-between p-3 ui-detail-tile">
							<div>
								<div className="flex items-center gap-1">
									<p className="text-sm font-medium text-gray-300">
										{t("settings.discovery.discoverOnProviderCreation")}
									</p>
									<ResetButton
										tooltip={t("settings.common.resetSetting")}
										onClick={() =>
											resetSettingMutation.mutate([
												"discovery_on_provider_create",
											])
										}
										size={12}
										disabled={isResetting}
									/>
								</div>
								<p className="text-gray-500 text-xs mt-0.5">
									{t(
										"settings.discovery.discoverOnProviderCreationDescription",
									)}
								</p>
							</div>
							<Toggle
								checked={discoveryOnCreate}
								size="sm"
								onChange={(v) =>
									updateMutation.mutate({
										discovery_on_provider_create: v ? "true" : "false",
									})
								}
								disabled={isUpdating}
								ariaLabel={t("settings.discovery.discoverOnProviderCreation")}
							/>
						</div>
					</div>
					<div className="space-y-5">
						<SettingsSlider
							id="discovery-interval"
							label={t("settings.discovery.discoveryInterval")}
							value={discoveryIntervalHours}
							min={0}
							max={48}
							step={0.5}
							clampStep={0.5}
							infinityValue={0}
							unit="h"
							disabled={isUpdating}
							onChange={(v) =>
								updateMutation.mutate({
									discovery_interval: hoursToGoDuration(v),
								})
							}
							description={t(
								"settings.discovery.discoveryInterval.description",
							)}
							onReset={() =>
								resetSettingMutation.mutate(["discovery_interval"])
							}
							resetTooltip={t("settings.common.resetSetting")}
						/>
						<div className="flex justify-end">
							<button
								type="button"
								onClick={() => discoverAllMutation.mutate()}
								disabled={isUpdating}
								className="ui-btn ui-btn-primary disabled:opacity-50 disabled:cursor-not-allowed"
							>
								{discoverAllMutation.isPending ? (
									<Spinner />
								) : (
									<Play size={12} />
								)}
								{t("settings.discovery.discoverAll")}
							</button>
						</div>
					</div>
				</div>
			</div>
		</SettingsSection>
	);
}
