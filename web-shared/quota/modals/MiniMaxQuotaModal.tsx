import type { QuotaBarMode } from "../display";
import type { MiniMaxModelRemains, MiniMaxQuotaResponse } from "../types";
import {
	type QuotaBodyProps,
	type QuotaModalKit,
	quotaRightText,
	resetSublabelFromEpoch,
	type Translate,
} from "./labels";
import { QuotaBar } from "./primitives";

/** Human label for a MiniMax model class; unknown classes show their raw name. */
function classLabel(modelName: string, t: Translate): string {
	if (modelName === "general") return t("miniMaxChatModels");
	if (modelName === "video") return t("miniMaxVideoModels");
	return modelName;
}

function ModelClassRows({
	entry,
	barMode,
	fetchedAt,
	kit: { t, time },
}: {
	entry: MiniMaxModelRemains;
	barMode: QuotaBarMode;
	fetchedAt: string;
	kit: QuotaModalKit;
}) {
	const name = entry.model_name;
	const label = classLabel(name, t);

	// Status 3 means the class is not part of the active plan: there is no quota
	// to draw, so show a placeholder rather than two empty bars.
	if (entry.current_interval_status === 3) {
		return (
			<div className="ui-quota-class">
				<p className="ui-quota-class-label">{label}</p>
				<p
					className="ui-quota-bar-sub"
					data-testid={`minimax-${name}-not-in-plan`}
				>
					{t("miniMaxNotInPlan")}
				</p>
			</div>
		);
	}

	// MiniMax reports REMAINING percentages, so invert for the used share.
	const fiveHourUsed = 100 - entry.current_interval_remaining_percent;
	const weeklyUsed = 100 - entry.current_weekly_remaining_percent;

	// Interval length varies by class (chat 5h, video 24h), so derive it from the
	// window bounds instead of hardcoding 5.
	const intervalHours =
		entry.end_time != null &&
		entry.start_time != null &&
		entry.end_time > entry.start_time
			? Math.round((entry.end_time - entry.start_time) / 3_600_000)
			: 5;

	const rightText = (used: number) => quotaRightText(used, barMode, t);

	// remains_time/weekly_remains_time are durations measured when the provider
	// answered, which is fetchedAt: anchor there so the reset instant stays put
	// across re-renders instead of walking forward with the clock.
	const fetchedAtMs = new Date(fetchedAt).getTime();
	const fiveHourResetAt =
		entry.remains_time > 0 ? fetchedAtMs + entry.remains_time : null;
	const weeklyResetAt =
		entry.weekly_remains_time > 0
			? fetchedAtMs + entry.weekly_remains_time
			: null;

	return (
		<div className="ui-quota-class">
			<p className="ui-quota-class-label">{label}</p>
			<QuotaBar
				label={
					<span data-testid={`minimax-${name}-5h-label`}>
						{t("hTokenQuota", { hours: intervalHours })}
					</span>
				}
				rightText={rightText(fiveHourUsed)}
				percentage={fiveHourUsed}
				barMode={barMode}
				testId={`minimax-${name}-5h-bar`}
				fillTestId={`minimax-${name}-5h-fill`}
			>
				{resetSublabelFromEpoch(fiveHourResetAt, t, time)}
			</QuotaBar>
			<QuotaBar
				label={t("weeklyTokenQuota")}
				rightText={rightText(weeklyUsed)}
				percentage={weeklyUsed}
				barMode={barMode}
				testId={`minimax-${name}-weekly-bar`}
				fillTestId={`minimax-${name}-weekly-fill`}
			>
				{resetSublabelFromEpoch(weeklyResetAt, t, time)}
			</QuotaBar>
		</div>
	);
}

export function MiniMaxQuotaModal({
	providerName,
	payload,
	barMode,
	fetchedAt,
	kit,
}: QuotaBodyProps<MiniMaxQuotaResponse>) {
	// Unlike getMiniMaxGeneralEntry, this does not gate on
	// base_resp.status_code === 0. That is safe, not an oversight: a non-zero
	// status produces no visible badge (isQuotaPayloadVisible), so this modal is
	// never opened for that payload in the first place.
	const entries = payload.model_remains ?? [];

	return kit.shell({
		title: kit.t("miniMaxPlanQuota", { provider: providerName }),
		children: entries.map((entry) => (
			<ModelClassRows
				key={entry.model_name}
				entry={entry}
				barMode={barMode}
				fetchedAt={fetchedAt}
				kit={kit}
			/>
		)),
	});
}
