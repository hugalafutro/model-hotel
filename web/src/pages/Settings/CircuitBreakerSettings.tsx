import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Shield } from "lucide-react";
import { useTranslation } from "react-i18next";
import { api } from "../../api/client";
import { ResetButton } from "../../components/ResetButton";
import { SettingsSection } from "../../components/SettingsSection";
import { SettingsSlider } from "../../components/SettingsSlider";
import { Toggle } from "../../components/Toggle";
import { useToast } from "../../context/ToastContext";
import { goDurationToSeconds, secondsToGoDuration } from "../../utils/duration";

interface CircuitBreakerSettingsProps {
	collapsed: boolean;
	onToggle: () => void;
	onResetSection?: () => void;
}

export function CircuitBreakerSettings({
	collapsed,
	onToggle,
	onResetSection,
}: CircuitBreakerSettingsProps) {
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

	const circuitBreakerEnabled = settings?.circuit_breaker_enabled !== "false";
	const circuitBreakerThreshold = settings?.circuit_breaker_threshold || "5";
	const circuitBreakerCooldown = settings?.circuit_breaker_cooldown || "1m0s";
	const failoverOnRateLimit = settings?.failover_on_rate_limit === "true";

	return (
		<SettingsSection
			icon={Shield}
			title={t("settings.circuitBreaker.title")}
			collapsed={collapsed}
			onToggle={onToggle}
			onResetSection={onResetSection}
		>
			<div className="space-y-5">
				<p className="text-gray-400 text-sm">
					{t("settings.circuitBreaker.description")}
				</p>
				<div className="grid grid-cols-2 gap-x-8 gap-y-5 [align-items:start]">
					<div className="space-y-5">
						<div className="flex items-center justify-between">
							<div>
								<div className="flex items-center gap-1">
									<p className="text-sm font-medium text-gray-300">
										{t("settings.circuitBreaker.enable")}
									</p>
									<ResetButton
										tooltip={t("settings.common.resetSetting")}
										onClick={() =>
											resetSettingMutation.mutate(["circuit_breaker_enabled"])
										}
										size={12}
										disabled={isResetting}
									/>
								</div>
								<p className="text-gray-500 text-xs mt-0.5">
									{t("settings.circuitBreaker.enableDescription")}
								</p>
							</div>
							<Toggle
								checked={circuitBreakerEnabled}
								onChange={(v) =>
									updateMutation.mutate({
										circuit_breaker_enabled: v ? "true" : "false",
									})
								}
								ariaLabel={t("settings.circuitBreaker.enable")}
							/>
						</div>

						<div className="flex items-center justify-between">
							<div>
								<div className="flex items-center gap-1">
									<p className="text-sm font-medium text-gray-300">
										{t("settings.circuitBreaker.failoverOnRateLimit")}
									</p>
									<ResetButton
										tooltip={t("settings.common.resetSetting")}
										onClick={() =>
											resetSettingMutation.mutate(["failover_on_rate_limit"])
										}
										size={12}
										disabled={isResetting}
									/>
								</div>
								<p className="text-gray-500 text-xs mt-0.5">
									{t("settings.circuitBreaker.failoverOnRateLimitDescription")}
								</p>
							</div>
							<Toggle
								checked={failoverOnRateLimit}
								onChange={(v) =>
									updateMutation.mutate({
										failover_on_rate_limit: v ? "true" : "false",
									})
								}
								ariaLabel={t("settings.circuitBreaker.failoverOnRateLimit")}
							/>
						</div>
					</div>

					<div className="space-y-5">
						{circuitBreakerEnabled && (
							<>
								<SettingsSlider
									id="circuit-breaker-threshold"
									label={t("settings.circuitBreaker.failureThreshold")}
									value={Number(circuitBreakerThreshold)}
									min={1}
									max={50}
									step={1}
									unit="s"
									hideUnit
									onChange={(v) =>
										updateMutation.mutate({
											circuit_breaker_threshold: String(v),
										})
									}
									description={t(
										"settings.circuitBreaker.failureThreshold.description",
									)}
									onReset={() =>
										resetSettingMutation.mutate(["circuit_breaker_threshold"])
									}
									resetTooltip={t("settings.common.resetSetting")}
								/>

								<SettingsSlider
									id="circuit-breaker-cooldown"
									label={t("settings.circuitBreaker.cooldownPeriod")}
									value={goDurationToSeconds(circuitBreakerCooldown)}
									min={30}
									max={600}
									step={30}
									clampStep={30}
									unit="s"
									onChange={(v) =>
										updateMutation.mutate({
											circuit_breaker_cooldown: secondsToGoDuration(v),
										})
									}
									description={t(
										"settings.circuitBreaker.cooldownPeriod.description",
									)}
									onReset={() =>
										resetSettingMutation.mutate(["circuit_breaker_cooldown"])
									}
									resetTooltip={t("settings.common.resetSetting")}
								/>
							</>
						)}
					</div>
				</div>
			</div>
		</SettingsSection>
	);
}
