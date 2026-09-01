import { useTranslation } from "react-i18next";
import { ResetButton } from "../../components/ResetButton";
import { SettingsGroup } from "../../components/SettingsGroup";
import { SettingsSlider } from "../../components/SettingsSlider";
import { Toggle } from "../../components/Toggle";
import { goDurationToMinutes, minutesToGoDuration } from "../../utils/duration";
import { useSettingsMutations } from "./useSettingsMutations";

// Bounds of the two learner knobs. The grow counter is how many clean
// completions a capped provider must serve per +1 of allowance: the floor of 5
// keeps a single lucky burst from widening the window, the ceiling of 100 is
// already glacial. The forget horizon is in minutes: a capped window returns
// to uncapped after this long without a cut, so the floor of 1 minute keeps it
// a horizon rather than a disable, and an hour is far past any transient
// congestion worth remembering.
const GROW_AFTER_MIN = 5;
const GROW_AFTER_MAX = 100;
const FORGET_MIN_MINUTES = 1;
const FORGET_MAX_MINUTES = 60;

// The adaptive concurrency group: the in-flight learner that cuts a provider's
// allowance on a saturated 429 and grows it back on clean completions, so the
// router spills to the next entry before anyone has to say 429 again. Rendered
// inside the Circuit Breaker & Failover section beside the 429-handling group
// it acts on; its own file keeps every component under the size ceilings.
export function InflightLimiterGroup() {
	const { t } = useTranslation();
	const { settings, updateMutation, resetSettingMutation, isResetting } =
		useSettingsMutations();

	// Fallbacks mirror the Go defaults (internal/proxy/inflight.go: enabled,
	// grow after 20, forget after 10m); fallback before clamp, clamp for
	// display only, as everywhere else in this section.
	const limiterEnabled = settings?.inflight_limiter_enabled !== "false";
	const growAfter = Math.min(
		GROW_AFTER_MAX,
		Math.max(GROW_AFTER_MIN, Number(settings?.inflight_grow_after) || 20),
	);
	const forgetMinutes = Math.min(
		FORGET_MAX_MINUTES,
		Math.max(
			FORGET_MIN_MINUTES,
			goDurationToMinutes(settings?.inflight_forget_after || "10m") || 10,
		),
	);

	return (
		<SettingsGroup title={t("settings.circuitBreaker.inflightGroup")}>
			<div
				className="flex items-center justify-between gap-3"
				data-testid="inflight-limiter-row"
			>
				<div className="min-w-0">
					<div className="flex items-center gap-1">
						<p className="text-sm font-medium text-gray-300">
							{t("settings.circuitBreaker.inflightLimiter")}
						</p>
						<ResetButton
							tooltip={t("settings.common.resetSetting")}
							onClick={() =>
								resetSettingMutation.mutate(["inflight_limiter_enabled"])
							}
							size={12}
							disabled={isResetting}
						/>
					</div>
					<p className="text-gray-500 text-xs mt-0.5">
						{t("settings.circuitBreaker.inflightLimiterDescription")}
					</p>
				</div>
				<Toggle
					checked={limiterEnabled}
					size="sm"
					onChange={(v) =>
						updateMutation.mutate({
							inflight_limiter_enabled: v ? "true" : "false",
						})
					}
					ariaLabel={t("settings.circuitBreaker.inflightLimiter")}
				/>
			</div>

			<SettingsSlider
				id="inflight-grow-after"
				disabled={!limiterEnabled}
				label={t("settings.circuitBreaker.inflightGrowAfter")}
				value={growAfter}
				min={GROW_AFTER_MIN}
				max={GROW_AFTER_MAX}
				step={5}
				unit="s"
				hideUnit
				onChange={(v) =>
					updateMutation.mutate({ inflight_grow_after: String(v) })
				}
				description={t("settings.circuitBreaker.inflightGrowAfter.description")}
				onReset={() => resetSettingMutation.mutate(["inflight_grow_after"])}
				resetTooltip={t("settings.common.resetSetting")}
			/>

			<SettingsSlider
				id="inflight-forget-after"
				disabled={!limiterEnabled}
				label={t("settings.circuitBreaker.inflightForgetAfter")}
				value={forgetMinutes}
				min={FORGET_MIN_MINUTES}
				max={FORGET_MAX_MINUTES}
				step={1}
				unit="m"
				onChange={(v) =>
					updateMutation.mutate({
						inflight_forget_after: minutesToGoDuration(v),
					})
				}
				description={t(
					"settings.circuitBreaker.inflightForgetAfter.description",
				)}
				onReset={() => resetSettingMutation.mutate(["inflight_forget_after"])}
				resetTooltip={t("settings.common.resetSetting")}
			/>
		</SettingsGroup>
	);
}
