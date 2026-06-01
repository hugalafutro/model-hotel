import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Search } from "lucide-react";
import { useTranslation } from "react-i18next";
import { api } from "../../api/client";
import { SettingsSection } from "../../components/SettingsSection";
import { SettingsSelect } from "../../components/SettingsSelect";
import { Toggle } from "../../components/Toggle";
import { useToast } from "../../context/ToastContext";

interface DiscoverySettingsProps {
	collapsed: boolean;
	onToggle: () => void;
}

export function DiscoverySettings({
	collapsed,
	onToggle,
}: DiscoverySettingsProps) {
	const { t } = useTranslation();
	const { toast } = useToast();
	const queryClient = useQueryClient();

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

	const isUpdating = updateMutation.isPending;
	const discoveryInterval = settings?.discovery_interval || "6h";

	const DISCOVERY_INTERVALS = [
		{ value: "30m", label: t("settings.discovery.intervals.30m") },
		{ value: "1h", label: t("settings.discovery.intervals.1h") },
		{ value: "6h", label: t("settings.discovery.intervals.6h") },
		{ value: "12h", label: t("settings.discovery.intervals.12h") },
		{ value: "24h", label: t("settings.discovery.intervals.24h") },
		{ value: "0", label: t("settings.discovery.intervals.disabled") },
	];
	const discoveryOnStartup = settings?.discovery_on_startup !== "false";
	const discoveryOnCreate = settings?.discovery_on_provider_create !== "false";

	return (
		<SettingsSection
			icon={Search}
			title={t("settings.discovery.title")}
			collapsed={collapsed}
			onToggle={onToggle}
		>
			<div className="space-y-5">
				<p className="text-gray-400 text-sm">
					{t("settings.discovery.description")}
				</p>
				<SettingsSelect
					id="discovery-interval"
					label={t("settings.discovery.discoveryInterval")}
					value={discoveryInterval}
					options={DISCOVERY_INTERVALS}
					onChange={(v) => updateMutation.mutate({ discovery_interval: v })}
					disabled={isUpdating}
					description={
						discoveryInterval === "0" ? (
							<span className="text-amber-400">
								{t("settings.discovery.discoveryInterval.disabled")}
							</span>
						) : (
							t("settings.discovery.discoveryInterval.description")
						)
					}
				/>

				<div className="flex items-center justify-between">
					<div>
						<p className="text-sm font-medium text-gray-300">
							{t("settings.discovery.discoverOnStartup")}
						</p>
						<p className="text-gray-500 text-xs mt-0.5">
							{t("settings.discovery.discoverOnStartupDescription")}
						</p>
					</div>
					<Toggle
						checked={discoveryOnStartup}
						onChange={(v) =>
							updateMutation.mutate({
								discovery_on_startup: v ? "true" : "false",
							})
						}
						disabled={isUpdating}
						ariaLabel={t("settings.discovery.discoverOnStartup")}
					/>
				</div>

				<div className="flex items-center justify-between">
					<div>
						<p className="text-sm font-medium text-gray-300">
							{t("settings.discovery.discoverOnProviderCreation")}
						</p>
						<p className="text-gray-500 text-xs mt-0.5">
							{t("settings.discovery.discoverOnProviderCreationDescription")}
						</p>
					</div>
					<Toggle
						checked={discoveryOnCreate}
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
		</SettingsSection>
	);
}
