import { useTranslation } from "react-i18next";
import { SettingsGroup } from "../../components/SettingsGroup";
import { SettingsSlider } from "../../components/SettingsSlider";
import { SettingToggleRow } from "../../components/SettingToggleRow";
import { goDurationToMinutes, minutesToGoDuration } from "../../utils/duration";
import { SETTING_DEFAULTS, settingOr } from "./defaults";
import { useSettingsMutations } from "./useSettingsMutations";

// Bounds of the two learner knobs. The grow counter is how many clean
// completions a capped provider must serve per +1 of allowance: the floor of 5
// keeps a single lucky burst from widening the window, the ceiling of 100 is
// already glacial. The forget horizon is in minutes: a capped window returns
// to uncapped after this long without a cut, so the floor of 1 minute keeps it
// a horizon rather than a disable, and the ceiling of an hour is past any
// transient congestion worth remembering.
const GROW_AFTER_MIN = 5;
const GROW_AFTER_MAX = 100;
const FORGET_MIN_MINUTES = 1;
const FORGET_MAX_MINUTES = 60;

// The adaptive concurrency group: the in-flight learner that cuts a provider's
// allowance on a saturated 429 and grows it back on clean completions, so the
// router spills to the next entry sooner. Rendered inside the Circuit Breaker
// & Failover section beside the 429-handling group it acts on; its own file
// keeps every component under the size ceilings.
export function InflightLimiterGroup() {
	const { t } = useTranslation();
	const { settings, updateMutation, resetSettingMutation, isResetting } =
		useSettingsMutations();

	// SETTING_DEFAULTS mirrors the Go defaults (internal/proxy/inflight.go).
	// Fallback before clamp, clamp for display only; a stored value the parse
	// rejects falls back to the default too.
	const limiterEnabled = settings?.inflight_limiter_enabled !== "false";
	const growAfter = Math.min(
		GROW_AFTER_MAX,
		Math.max(
			GROW_AFTER_MIN,
			Number(settingOr(settings, "inflight_grow_after")) ||
				Number(SETTING_DEFAULTS.inflight_grow_after),
		),
	);
	const forgetMinutes = Math.min(
		FORGET_MAX_MINUTES,
		Math.max(
			FORGET_MIN_MINUTES,
			goDurationToMinutes(settingOr(settings, "inflight_forget_after")) ||
				goDurationToMinutes(SETTING_DEFAULTS.inflight_forget_after),
		),
	);

	return (
		<SettingsGroup title={t("settings.circuitBreaker.inflightGroup")}>
			<SettingToggleRow
				testId="inflight-limiter-row"
				label={t("settings.circuitBreaker.inflightLimiter")}
				description={t("settings.circuitBreaker.inflightLimiterDescription")}
				checked={limiterEnabled}
				onChange={(v) =>
					updateMutation.mutate({
						inflight_limiter_enabled: v ? "true" : "false",
					})
				}
				onReset={() =>
					resetSettingMutation.mutate(["inflight_limiter_enabled"])
				}
				resetDisabled={isResetting}
			/>

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
