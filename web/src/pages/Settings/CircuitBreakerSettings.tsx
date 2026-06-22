import { useTranslation } from "react-i18next";
import { Shield } from "@/lib/icons";
import { ResetButton } from "../../components/ResetButton";
import { SettingsGroup } from "../../components/SettingsGroup";
import { SettingsSection } from "../../components/SettingsSection";
import { SettingsSlider } from "../../components/SettingsSlider";
import { Toggle } from "../../components/Toggle";
import { goDurationToSeconds, secondsToGoDuration } from "../../utils/duration";
import { useSettingsMutations } from "./useSettingsMutations";

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
	const { settings, updateMutation, resetSettingMutation, isResetting } =
		useSettingsMutations();

	const circuitBreakerEnabled = settings?.circuit_breaker_enabled !== "false";
	const circuitBreakerThreshold = settings?.circuit_breaker_threshold || "5";
	const circuitBreakerCooldown = settings?.circuit_breaker_cooldown || "1m0s";
	const failoverOnRateLimit = settings?.failover_on_rate_limit === "true";
	const hedgingEnabled = settings?.hedging_enabled === "true";
	const hedgeDelay = settings?.hedge_delay || "4s";

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
				<div className="grid grid-cols-2 gap-x-6 gap-y-5 [align-items:start]">
					<SettingsGroup title={t("settings.circuitBreaker.failoverGroup")}>
						<div className="flex items-center justify-between gap-3">
							<div className="min-w-0">
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
								size="sm"
								onChange={(v) =>
									updateMutation.mutate({
										circuit_breaker_enabled: v ? "true" : "false",
									})
								}
								ariaLabel={t("settings.circuitBreaker.enable")}
							/>
						</div>

						<div className="flex items-center justify-between gap-3">
							<div className="min-w-0">
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
								size="sm"
								onChange={(v) =>
									updateMutation.mutate({
										failover_on_rate_limit: v ? "true" : "false",
									})
								}
								ariaLabel={t("settings.circuitBreaker.failoverOnRateLimit")}
							/>
						</div>

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
					</SettingsGroup>

					<SettingsGroup title={t("settings.circuitBreaker.hedgingGroup")}>
						<div className="flex items-center justify-between gap-3">
							<div className="min-w-0">
								<div className="flex items-center gap-1">
									<p className="text-sm font-medium text-gray-300">
										{t("settings.circuitBreaker.hedging")}
									</p>
									<ResetButton
										tooltip={t("settings.common.resetSetting")}
										onClick={() =>
											resetSettingMutation.mutate(["hedging_enabled"])
										}
										size={12}
										disabled={isResetting}
									/>
								</div>
								<p className="text-gray-500 text-xs mt-0.5">
									{t("settings.circuitBreaker.hedgingDescription")}
								</p>
							</div>
							<Toggle
								checked={hedgingEnabled}
								size="sm"
								onChange={(v) =>
									updateMutation.mutate({
										hedging_enabled: v ? "true" : "false",
									})
								}
								ariaLabel={t("settings.circuitBreaker.hedging")}
							/>
						</div>

						{hedgingEnabled && (
							<SettingsSlider
								id="hedge-delay"
								label={t("settings.circuitBreaker.hedgeDelay")}
								value={goDurationToSeconds(hedgeDelay)}
								min={1}
								max={15}
								step={1}
								unit="s"
								onChange={(v) =>
									updateMutation.mutate({
										hedge_delay: secondsToGoDuration(v),
									})
								}
								description={t(
									"settings.circuitBreaker.hedgeDelay.description",
								)}
								onReset={() => resetSettingMutation.mutate(["hedge_delay"])}
								resetTooltip={t("settings.common.resetSetting")}
							/>
						)}
					</SettingsGroup>
				</div>

				{hedgingEnabled && (
					<div className="p-3 bg-amber-900/30 border border-amber-600 rounded-(--radius-box)">
						<p className="text-sm text-amber-300 text-justify">
							{t("settings.circuitBreaker.hedgingNotice")}
						</p>
					</div>
				)}
			</div>
		</SettingsSection>
	);
}
