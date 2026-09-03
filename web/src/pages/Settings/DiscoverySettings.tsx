import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { Play, Search } from "@/lib/icons";
import { api } from "../../api/client";
import { SettingsGroup } from "../../components/SettingsGroup";
import { SettingsSection } from "../../components/SettingsSection";
import { SettingsSlider } from "../../components/SettingsSlider";
import { SettingToggleRow } from "../../components/SettingToggleRow";
import { Spinner } from "../../components/Spinner";
import { useToast } from "../../context/ToastContext";
import { useRefreshDiscoveryBadge } from "../../hooks/useRefreshDiscoveryBadge";
import { goDurationToHours, hoursToGoDuration } from "../../utils/duration";
import { formatDateTimeShort } from "../../utils/format";
import { SETTING_DEFAULTS } from "./defaults";
import { useSettingsMutations } from "./useSettingsMutations";

interface DiscoverySettingsProps {
	collapsed: boolean;
	onToggle: () => void;
	onResetSection?: () => void;
	managed?: boolean;
}

export function DiscoverySettings({
	collapsed,
	onToggle,
	onResetSection,
	managed,
}: DiscoverySettingsProps) {
	const { t } = useTranslation();
	const { toast } = useToast();
	const queryClient = useQueryClient();
	// "Discover all" moves the claim set behind the Models nav badge, and the
	// badge is a 60s poll that nothing here otherwise touches. See
	// useRefreshDiscoveryBadge.
	const refreshBadge = useRefreshDiscoveryBadge();

	const { settings, updateMutation, resetSettingMutation, isResetting } =
		useSettingsMutations();

	// Catalog size, so the Manual column shows what "Discover all" acts on
	// instead of a lone button. Cached query keys shared with the rest of the app.
	const { data: providers } = useQuery({
		queryKey: ["providers"],
		queryFn: () => api.providers.list(),
	});
	const { data: models } = useQuery({
		queryKey: ["models"],
		queryFn: () => api.models.list(),
	});

	// Most recent per-provider discovery time stands in for "last run" (every
	// discovery path stamps providers.last_discovered_at), so no extra backend.
	const lastRun = useMemo(() => {
		const times = (providers ?? [])
			.map((p) => p.last_discovered_at)
			.filter((t): t is string => Boolean(t))
			// Compare as instants, not strings: a lexicographic sort only matches
			// chronological order for UTC ("Z") timestamps, and would mis-rank a
			// value carrying an explicit offset (e.g. "+05:00").
			.sort((a, b) => Date.parse(a) - Date.parse(b));
		return times.at(-1) ?? null;
	}, [providers]);

	const discoverAllMutation = useMutation({
		mutationFn: () => api.providers.discoverAll(),
		onSuccess: () => {
			toast(t("settings.discovery.discoverAllComplete"), "success");
		},
		onError: (err: Error) => {
			toast(
				t("settings.discovery.discoverAllFailed", { message: err.message }),
				"error",
			);
		},
		onSettled: () => {
			// `onSettled`, like useDiscoveryRetest: a sweep that errors partway has
			// still upserted whatever it reached, so the catalogue counts above and
			// the claim set can both move without a success.
			queryClient.invalidateQueries({ queryKey: ["providers"] });
			queryClient.invalidateQueries({ queryKey: ["models"] });
			refreshBadge();
		},
	});

	const isUpdating = updateMutation.isPending || discoverAllMutation.isPending;

	const discoveryIntervalHours = goDurationToHours(
		settings?.discovery_interval || "6h",
	);
	const discoveryOnStartup = settings?.discovery_on_startup !== "false";
	const discoveryOnCreate = settings?.discovery_on_provider_create !== "false";
	const modelPruneDays = Number(
		settings?.model_prune_days || SETTING_DEFAULTS.model_prune_days,
	);

	return (
		<SettingsSection
			icon={Search}
			title={t("settings.discovery.title")}
			collapsed={collapsed}
			onToggle={onToggle}
			onResetSection={onResetSection}
			managed={managed}
		>
			<div className="space-y-5">
				<p className="text-gray-400 text-sm col-span-2">
					{t("settings.discovery.description")}
				</p>
				<div className="grid grid-cols-2 gap-x-6 gap-y-5 [align-items:start]">
					<SettingsGroup title={t("settings.discovery.automaticGroup")}>
						<SettingToggleRow
							label={t("settings.discovery.discoverOnStartup")}
							description={t("settings.discovery.discoverOnStartupDescription")}
							checked={discoveryOnStartup}
							disabled={isUpdating}
							onChange={(v) =>
								updateMutation.mutate({
									discovery_on_startup: v ? "true" : "false",
								})
							}
							onReset={() =>
								resetSettingMutation.mutate(["discovery_on_startup"])
							}
							resetDisabled={isResetting}
						/>

						<SettingToggleRow
							label={t("settings.discovery.discoverOnProviderCreation")}
							description={t(
								"settings.discovery.discoverOnProviderCreationDescription",
							)}
							checked={discoveryOnCreate}
							disabled={isUpdating}
							onChange={(v) =>
								updateMutation.mutate({
									discovery_on_provider_create: v ? "true" : "false",
								})
							}
							onReset={() =>
								resetSettingMutation.mutate(["discovery_on_provider_create"])
							}
							resetDisabled={isResetting}
						/>

						<SettingsSlider
							id="discovery-interval"
							label={t("settings.discovery.discoveryInterval")}
							value={discoveryIntervalHours}
							min={0}
							max={48}
							step={0.5}
							clampStep={0.5}
							// Deliberately NOT infinityValue={0}: 0 turns this OFF, it does not
							// lift a limit, and ∞ read as the opposite. Same defect as the TTFT
							// probe slider.
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

						<SettingsSlider
							id="model-prune-days"
							label={t("settings.discovery.pruneRetired")}
							value={modelPruneDays}
							min={0}
							max={180}
							step={1}
							infinityValue={0}
							unit="d"
							disabled={isUpdating}
							onChange={(v) => {
								updateMutation.mutate({
									model_prune_days: String(v),
								});
							}}
							description={t("settings.discovery.pruneRetired.description")}
							onReset={() => resetSettingMutation.mutate(["model_prune_days"])}
							resetTooltip={t("settings.common.resetSetting")}
						/>
					</SettingsGroup>

					<SettingsGroup title={t("settings.discovery.manualGroup")}>
						<div className="flex">
							<button
								type="button"
								onClick={() => discoverAllMutation.mutate()}
								disabled={isUpdating}
								className="ui-btn ui-btn-primary"
							>
								{discoverAllMutation.isPending ? (
									<Spinner />
								) : (
									<Play size={12} />
								)}
								{t("settings.discovery.discoverAll")}
							</button>
						</div>
						<dl className="text-xs text-gray-500 space-y-0.5">
							<div className="flex justify-between gap-2">
								<dt>{t("layout.nav.models")}</dt>
								<dd className="text-(--text-primary) tabular-nums">
									{models?.length ?? 0}
								</dd>
							</div>
							<div className="flex justify-between gap-2">
								<dt>{t("layout.nav.providers")}</dt>
								<dd className="text-(--text-primary) tabular-nums">
									{providers?.length ?? 0}
								</dd>
							</div>
							{lastRun && (
								<div className="flex justify-between gap-2">
									<dt>{t("settings.discovery.lastRun")}</dt>
									<dd className="text-(--text-primary)">
										{formatDateTimeShort(lastRun)}
									</dd>
								</div>
							)}
						</dl>
					</SettingsGroup>
				</div>
			</div>
		</SettingsSection>
	);
}
