import { getKimiCodeFiveHourLimit, getKimiCodeWeeklyLimit } from "../kimi";
import type { KimiCodeQuotaResponse } from "../types";
import { type QuotaBodyProps, quotaRightText, resetSublabel } from "./labels";
import { QuotaBar } from "./primitives";

export function KimiCodeQuotaModal({
	providerName,
	payload,
	barMode,
	kit: { shell, t, time },
}: QuotaBodyProps<KimiCodeQuotaResponse>) {
	const fiveHour = getKimiCodeFiveHourLimit(payload);
	const weekly = getKimiCodeWeeklyLimit(payload);
	const parallelLimit = payload.parallel?.limit;
	const totalQuota = payload.totalQuota;

	// The window helpers already return percent USED.
	const rightText = (used: number) => quotaRightText(used, barMode, t);

	return shell({
		title: t("kimiCodePlanQuota", { provider: providerName }),
		subtitle: `${t("plan")}: ${payload.user?.membership?.level ?? "-"}`,
		children: (
			<>
				{fiveHour && (
					<QuotaBar
						label={t("hTokenQuota", { hours: 5 })}
						rightText={rightText(fiveHour.percentage)}
						percentage={fiveHour.percentage}
						barMode={barMode}
						testId="kimi-5h-bar"
						fillTestId="kimi-5h-fill"
					>
						{resetSublabel(fiveHour.resetTime, t, time)}
					</QuotaBar>
				)}

				{weekly && (
					<QuotaBar
						label={t("weeklyTokenQuota")}
						rightText={rightText(weekly.percentage)}
						percentage={weekly.percentage}
						barMode={barMode}
						testId="kimi-weekly-bar"
						fillTestId="kimi-weekly-fill"
					>
						{resetSublabel(weekly.resetTime, t, time)}
					</QuotaBar>
				)}

				{(parallelLimit != null || totalQuota) && (
					<div className="ui-quota-rows">
						{parallelLimit != null && (
							<div className="ui-quota-row" data-testid="kimi-parallel">
								<span>{t("kimiCodeParallelLimit")}</span>
								<span>{parallelLimit}</span>
							</div>
						)}
						{totalQuota && (
							<div className="ui-quota-row" data-testid="kimi-total-quota">
								<span>{t("kimiCodeTotalQuota")}</span>
								<span>
									{totalQuota.remaining ?? "-"} / {totalQuota.limit ?? "-"}
								</span>
							</div>
						)}
					</div>
				)}
			</>
		),
	});
}
