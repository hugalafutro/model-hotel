import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Gauge } from "lucide-react";
import { useTranslation } from "react-i18next";
import { api } from "../../api/client";
import { ResetButton } from "../../components/ResetButton";
import { SettingsSection } from "../../components/SettingsSection";
import { SettingsSlider } from "../../components/SettingsSlider";
import { Toggle } from "../../components/Toggle";
import { useToast } from "../../context/ToastContext";

interface RateLimitSettingsProps {
	collapsed: boolean;
	onToggle: () => void;
	onResetSection?: () => void;
}

export function RateLimitSettings({
	collapsed,
	onToggle,
	onResetSection,
}: RateLimitSettingsProps) {
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

	const resetSettingMutation = useMutation({
		mutationFn: (keys: string[]) => api.settings.reset(keys),
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: ["settings"] });
			toast(t("settings.common.resetSettingDone"), "success");
		},
		onError: (err: Error) => {
			toast(
				t("settings.common.resetFailed", { message: err.message }),
				"error",
			);
		},
	});
	const isResetting = resetSettingMutation.isPending;

	const rateLimitEnabled = settings?.rate_limit_enabled !== "false";
	const rateLimitRPS = settings?.rate_limit_rps || "10";
	const rateLimitBurst = settings?.rate_limit_burst || "20";
	const rateLimitIpEnabled = settings?.rate_limit_ip_enabled !== "false";
	const rateLimitIpRPS = settings?.rate_limit_ip_rps || "30";
	const rateLimitIpBurst = settings?.rate_limit_ip_burst || "60";
	const rateLimitMaxWaitMs = settings?.rate_limit_max_wait_ms || "200";

	return (
		<SettingsSection
			icon={Gauge}
			title={t("settings.rateLimit.title")}
			collapsed={collapsed}
			onToggle={onToggle}
			onResetSection={onResetSection}
		>
			<div className="space-y-5">
				<p className="text-gray-400 text-sm">
					{t("settings.rateLimit.description")}
				</p>
				<div className="grid grid-cols-2 gap-x-8 gap-y-5 [align-items:start]">
					<div className="space-y-5">
						<div className="flex items-center justify-between">
							<div>
								<div className="flex items-center gap-1">
									<p className="text-sm font-medium text-gray-300">
										{t("settings.rateLimit.enable")}
									</p>
									<ResetButton
										tooltip={t("settings.common.resetSetting")}
										onClick={() =>
											resetSettingMutation.mutate(["rate_limit_enabled"])
										}
										size={12}
										disabled={isResetting}
									/>
								</div>
								<p className="text-gray-500 text-xs mt-0.5">
									{t("settings.rateLimit.enableDescription")}
								</p>
							</div>
							<Toggle
								checked={rateLimitEnabled}
								onChange={(v) =>
									updateMutation.mutate({
										rate_limit_enabled: v ? "true" : "false",
									})
								}
							/>
						</div>

						<div className="flex items-center justify-between">
							<div>
								<div className="flex items-center gap-1">
									<p className="text-sm font-medium text-gray-300">
										{t("settings.rateLimit.ipRateLimiting")}
									</p>
									<ResetButton
										tooltip={t("settings.common.resetSetting")}
										onClick={() =>
											resetSettingMutation.mutate(["rate_limit_ip_enabled"])
										}
										size={12}
										disabled={isResetting}
									/>
								</div>
								<p className="text-gray-500 text-xs mt-0.5">
									{t("settings.rateLimit.ipRateLimitingDescription")}
								</p>
							</div>
							<Toggle
								checked={rateLimitIpEnabled}
								onChange={(v) =>
									updateMutation.mutate({
										rate_limit_ip_enabled: v ? "true" : "false",
									})
								}
							/>
						</div>

						{(rateLimitEnabled || rateLimitIpEnabled) && (
							<>
								<p className="text-sm font-medium text-gray-300">
									{t("settings.rateLimit.backpressure")}
								</p>
								<p className="text-gray-500 text-xs">
									{t("settings.rateLimit.backpressureDescription")}
								</p>
								<SettingsSlider
									id="rate-limit-max-wait"
									label={t("settings.rateLimit.maxWait")}
									value={Number(rateLimitMaxWaitMs)}
									min={0}
									max={10000}
									step={100}
									clampStep={100}
									unit="ms"
									onChange={(v) =>
										updateMutation.mutate({
											rate_limit_max_wait_ms: String(v),
										})
									}
									description={t("settings.rateLimit.maxWait.description")}
									onReset={() =>
										resetSettingMutation.mutate(["rate_limit_max_wait_ms"])
									}
									resetTooltip={t("settings.common.resetSetting")}
								/>
							</>
						)}
					</div>

					<div className="space-y-5">
						{rateLimitEnabled && (
							<SettingsSlider
								id="rate-limit-rps"
								label={t("settings.rateLimit.requestsPerSecond")}
								value={Number(rateLimitRPS)}
								min={0}
								max={200}
								step={5}
								clampStep={5}
								infinityValue={0}
								unit="s"
								hideUnit
								onChange={(v) =>
									updateMutation.mutate({ rate_limit_rps: String(v) })
								}
								description={t(
									"settings.rateLimit.requestsPerSecond.description",
								)}
								onReset={() => resetSettingMutation.mutate(["rate_limit_rps"])}
								resetTooltip={t("settings.common.resetSetting")}
							/>
						)}

						{rateLimitIpEnabled && (
							<SettingsSlider
								id="rate-limit-ip-rps"
								label={t("settings.rateLimit.ipRequestsPerSecond")}
								value={Number(rateLimitIpRPS)}
								min={0}
								max={200}
								step={5}
								clampStep={5}
								infinityValue={0}
								unit="s"
								hideUnit
								onChange={(v) =>
									updateMutation.mutate({ rate_limit_ip_rps: String(v) })
								}
								description={t(
									"settings.rateLimit.ipRequestsPerSecond.description",
								)}
								onReset={() =>
									resetSettingMutation.mutate(["rate_limit_ip_rps"])
								}
								resetTooltip={t("settings.common.resetSetting")}
							/>
						)}

						{rateLimitEnabled && (
							<SettingsSlider
								id="rate-limit-burst"
								label={t("settings.rateLimit.burstSize")}
								value={Number(rateLimitBurst)}
								min={5}
								max={500}
								step={5}
								clampStep={5}
								unit="s"
								hideUnit
								onChange={(v) =>
									updateMutation.mutate({
										rate_limit_burst: String(v),
									})
								}
								description={t("settings.rateLimit.burstSize.description")}
								onReset={() =>
									resetSettingMutation.mutate(["rate_limit_burst"])
								}
								resetTooltip={t("settings.common.resetSetting")}
							/>
						)}

						{rateLimitIpEnabled && (
							<SettingsSlider
								id="rate-limit-ip-burst"
								label={t("settings.rateLimit.ipBurstSize")}
								value={Number(rateLimitIpBurst)}
								min={5}
								max={500}
								step={5}
								clampStep={5}
								unit="s"
								hideUnit
								onChange={(v) =>
									updateMutation.mutate({
										rate_limit_ip_burst: String(v),
									})
								}
								description={t("settings.rateLimit.ipBurstSize.description")}
								onReset={() =>
									resetSettingMutation.mutate(["rate_limit_ip_burst"])
								}
								resetTooltip={t("settings.common.resetSetting")}
							/>
						)}
					</div>
				</div>
			</div>
		</SettingsSection>
	);
}
