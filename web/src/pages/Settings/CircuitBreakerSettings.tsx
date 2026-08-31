import { useTranslation } from "react-i18next";
import { Shield } from "@/lib/icons";
import { ResetButton } from "../../components/ResetButton";
import { SettingsGroup } from "../../components/SettingsGroup";
import { SettingsSection } from "../../components/SettingsSection";
import { SettingsSlider } from "../../components/SettingsSlider";
import { Toggle } from "../../components/Toggle";
import {
	goDurationToHours,
	goDurationToMinutes,
	goDurationToSeconds,
	hoursToGoDuration,
	minutesToGoDuration,
	secondsToGoDuration,
} from "../../utils/duration";
import { useSettingsMutations } from "./useSettingsMutations";

// Bounds of the quota-pin ceiling slider, in hours. The floor keeps the
// operator off the zero that does not mean what it looks like; the ceiling is
// one week, the longest reset window internal/quota/normalize.go recognises.
// Shared by the clamp and the slider props so the two cannot drift apart.
const QUOTA_PIN_MAX_MIN_HOURS = 1;
const QUOTA_PIN_MAX_MAX_HOURS = 168;

// Bounds of the probe-backoff ceiling slider, in minutes. Minutes rather than
// hours because the backoff starts from a cooldown measured in seconds and
// doubles: 1, 2, 4, 8, 16, 32 minutes at the default cooldown, and an operator
// choosing where that stops wants finer steps than an hour. The floor keeps
// the operator off the zero that restores the default rather than disabling
// anything; the ceiling is a day, the quota pin's default.
const BACKOFF_MAX_MIN_MINUTES = 1;
const BACKOFF_MAX_MAX_MINUTES = 1440;

// Bounds of the model-span slider: how many of a provider's models must hold an
// open circuit before the provider itself is skipped. The floor of 1 is the
// escape hatch the breaker enforces too (internal/failover/model_circuits.go:
// effectiveSpan), and it reproduces the old provider-wide behaviour. The ceiling
// is far above any provider's catalog, so it only ever bites on a typo.
const SPAN_MODELS_MIN = 1;
const SPAN_MODELS_MAX = 100;

interface CircuitBreakerSettingsProps {
	collapsed: boolean;
	onToggle: () => void;
	onResetSection?: () => void;
	managed?: boolean;
}

export function CircuitBreakerSettings({
	collapsed,
	onToggle,
	onResetSection,
	managed,
}: CircuitBreakerSettingsProps) {
	const { t } = useTranslation();
	const { settings, updateMutation, resetSettingMutation, isResetting } =
		useSettingsMutations();

	const circuitBreakerEnabled = settings?.circuit_breaker_enabled !== "false";
	const circuitBreakerThreshold = settings?.circuit_breaker_threshold || "5";
	const circuitBreakerCooldown = settings?.circuit_breaker_cooldown || "1m0s";
	// The fallback mirrors the Go default (defaultSpanModels in
	// internal/failover/model_circuits.go), so an unset key shows the span
	// actually in force rather than a number nothing obeys. Clamped for display
	// only, because PUT /api/settings takes any int and the browser sanitizes the
	// range track against min/max while leaving the number box alone.
	const circuitBreakerSpanModels = Math.min(
		SPAN_MODELS_MAX,
		Math.max(
			SPAN_MODELS_MIN,
			Number(settings?.circuit_breaker_span_models) || 2,
		),
	);
	// Both quota-pin fallbacks mirror the Go defaults the breaker applies when
	// the key is absent (internal/failover/circuitbreaker.go: quotaPinEnabled
	// defaults true, quotaPinMax falls back to 24h). The `|| 24` on the hours is
	// the same fallback for a stored non-positive duration, which the breaker
	// also reads as unset, so the slider shows the ceiling actually in force
	// rather than a number nothing obeys.
	//
	// The clamp must come after that fallback, never merged into it: clamping
	// first would turn a stored 0 into the floor of 1, which is truthy, and the
	// `|| 24` would then never fire.
	//
	// PUT /api/settings accepts any duration, so a stored value can also sit
	// below the floor or above the ceiling. Both are clamped for display, since
	// the browser sanitizes the range track against min/max but leaves the
	// number box alone, and one control showing two different numbers is worse
	// than either number. Display only: SettingsSlider seeds its local state
	// from this prop and fires onChange on interaction, never on mount, so
	// nothing is written back until the operator actually moves the control.
	const quotaPinEnabled =
		settings?.circuit_breaker_quota_pin_enabled !== "false";
	const quotaPinMaxHours = Math.min(
		QUOTA_PIN_MAX_MAX_HOURS,
		Math.max(
			QUOTA_PIN_MAX_MIN_HOURS,
			goDurationToHours(settings?.circuit_breaker_quota_pin_max || "24h") || 24,
		),
	);
	// Same shape as the quota-pin pair, for the same reasons: the fallbacks
	// mirror the Go defaults (backoffEnabled defaults true, backoffMax falls back
	// to 1h, and a stored non-positive duration reads as unset), the fallback
	// comes before the clamp, and the clamp is display only.
	const backoffEnabled = settings?.circuit_breaker_backoff_enabled !== "false";
	const backoffMaxMinutes = Math.min(
		BACKOFF_MAX_MAX_MINUTES,
		Math.max(
			BACKOFF_MAX_MIN_MINUTES,
			goDurationToMinutes(settings?.circuit_breaker_backoff_max || "1h") || 60,
		),
	);
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
			managed={managed}
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

						<SettingsSlider
							id="circuit-breaker-threshold"
							disabled={!circuitBreakerEnabled}
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
							id="circuit-breaker-span-models"
							disabled={!circuitBreakerEnabled}
							label={t("settings.circuitBreaker.spanModels")}
							value={circuitBreakerSpanModels}
							min={SPAN_MODELS_MIN}
							max={SPAN_MODELS_MAX}
							step={1}
							unit="s"
							hideUnit
							onChange={(v) =>
								updateMutation.mutate({
									circuit_breaker_span_models: String(v),
								})
							}
							description={t("settings.circuitBreaker.spanModels.description")}
							onReset={() =>
								resetSettingMutation.mutate(["circuit_breaker_span_models"])
							}
							resetTooltip={t("settings.common.resetSetting")}
						/>

						<SettingsSlider
							id="circuit-breaker-cooldown"
							disabled={!circuitBreakerEnabled}
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

						<div
							className="flex items-center justify-between gap-3"
							data-testid="quota-pin-row"
						>
							<div className="min-w-0">
								<div className="flex items-center gap-1">
									<p className="text-sm font-medium text-gray-300">
										{t("settings.circuitBreaker.quotaPin")}
									</p>
									<ResetButton
										tooltip={t("settings.common.resetSetting")}
										onClick={() =>
											resetSettingMutation.mutate([
												"circuit_breaker_quota_pin_enabled",
											])
										}
										size={12}
										disabled={isResetting}
									/>
								</div>
								<p className="text-gray-500 text-xs mt-0.5">
									{t("settings.circuitBreaker.quotaPinDescription")}
								</p>
							</div>
							<Toggle
								checked={quotaPinEnabled}
								size="sm"
								disabled={!circuitBreakerEnabled}
								onChange={(v) =>
									updateMutation.mutate({
										circuit_breaker_quota_pin_enabled: v ? "true" : "false",
									})
								}
								ariaLabel={t("settings.circuitBreaker.quotaPin")}
							/>
						</div>

						<SettingsSlider
							id="circuit-breaker-quota-pin-max"
							disabled={!circuitBreakerEnabled || !quotaPinEnabled}
							label={t("settings.circuitBreaker.quotaPinMax")}
							value={quotaPinMaxHours}
							min={QUOTA_PIN_MAX_MIN_HOURS}
							max={QUOTA_PIN_MAX_MAX_HOURS}
							step={1}
							unit="h"
							onChange={(v) =>
								updateMutation.mutate({
									circuit_breaker_quota_pin_max: hoursToGoDuration(v),
								})
							}
							description={t("settings.circuitBreaker.quotaPinMax.description")}
							onReset={() =>
								resetSettingMutation.mutate(["circuit_breaker_quota_pin_max"])
							}
							resetTooltip={t("settings.common.resetSetting")}
						/>

						<div
							className="flex items-center justify-between gap-3"
							data-testid="backoff-row"
						>
							<div className="min-w-0">
								<div className="flex items-center gap-1">
									<p className="text-sm font-medium text-gray-300">
										{t("settings.circuitBreaker.backoff")}
									</p>
									<ResetButton
										tooltip={t("settings.common.resetSetting")}
										onClick={() =>
											resetSettingMutation.mutate([
												"circuit_breaker_backoff_enabled",
											])
										}
										size={12}
										disabled={isResetting}
									/>
								</div>
								<p className="text-gray-500 text-xs mt-0.5">
									{t("settings.circuitBreaker.backoffDescription")}
								</p>
							</div>
							<Toggle
								checked={backoffEnabled}
								size="sm"
								disabled={!circuitBreakerEnabled}
								onChange={(v) =>
									updateMutation.mutate({
										circuit_breaker_backoff_enabled: v ? "true" : "false",
									})
								}
								ariaLabel={t("settings.circuitBreaker.backoff")}
							/>
						</div>

						<SettingsSlider
							id="circuit-breaker-backoff-max"
							disabled={!circuitBreakerEnabled || !backoffEnabled}
							label={t("settings.circuitBreaker.backoffMax")}
							value={backoffMaxMinutes}
							min={BACKOFF_MAX_MIN_MINUTES}
							max={BACKOFF_MAX_MAX_MINUTES}
							step={1}
							unit="m"
							onChange={(v) =>
								updateMutation.mutate({
									circuit_breaker_backoff_max: minutesToGoDuration(v),
								})
							}
							description={t("settings.circuitBreaker.backoffMax.description")}
							onReset={() =>
								resetSettingMutation.mutate(["circuit_breaker_backoff_max"])
							}
							resetTooltip={t("settings.common.resetSetting")}
						/>
					</SettingsGroup>

					{/* Right column: the Hedging group with its trade-off notice directly
					    beneath it, so the warning sits next to the toggle it is about. */}
					<div className="space-y-5" data-testid="hedging-column">
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

							<SettingsSlider
								id="hedge-delay"
								disabled={!hedgingEnabled}
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
						</SettingsGroup>

						{hedgingEnabled && (
							<div
								className="ui-callout ui-callout-warning"
								data-testid="hedging-notice"
							>
								<p>{t("settings.circuitBreaker.hedgingNotice")}</p>
							</div>
						)}
					</div>
				</div>
			</div>
		</SettingsSection>
	);
}
