import type { ZAICodingQuotaResponse } from "../types";
import {
	getZaiCodingFiveHourLimit,
	getZaiCodingMcpLimit,
	getZaiCodingWeeklyLimit,
} from "../zai";
import {
	type QuotaBodyProps,
	quotaRightText,
	resetSublabelFromEpoch,
} from "./labels";
import { QuotaBar } from "./primitives";

export function ZAICodingQuotaModal({
	providerName,
	payload,
	barMode,
	kit: { shell, t, time },
}: QuotaBodyProps<ZAICodingQuotaResponse>) {
	const fiveHour = getZaiCodingFiveHourLimit(payload);
	const weekly = getZaiCodingWeeklyLimit(payload);
	const mcp = getZaiCodingMcpLimit(payload);

	// Z.ai reports percent USED directly, which is what QuotaBar wants.
	const rightText = (used: number) => quotaRightText(used, barMode, t);

	return shell({
		title: t("zAICodingPlanQuota", { provider: providerName }),
		subtitle: `${t("plan")}: ${payload.data?.level ?? "-"}`,
		children: (
			<>
				{fiveHour && (
					<QuotaBar
						label={t("hTokenQuota", { hours: 5 })}
						rightText={rightText(fiveHour.percentage)}
						percentage={fiveHour.percentage}
						barMode={barMode}
						testId="zai-5h-bar"
						fillTestId="zai-5h-fill"
					>
						{resetSublabelFromEpoch(fiveHour.nextResetTime, t, time)}
					</QuotaBar>
				)}

				{weekly && (
					<QuotaBar
						label={t("weeklyTokenQuota")}
						rightText={rightText(weekly.percentage)}
						percentage={weekly.percentage}
						barMode={barMode}
						testId="zai-weekly-bar"
						fillTestId="zai-weekly-fill"
					>
						{resetSublabelFromEpoch(weekly.nextResetTime, t, time)}
					</QuotaBar>
				)}

				{mcp && (
					<QuotaBar
						label={t("mcpTokenQuota")}
						rightText={rightText(mcp.percentage)}
						percentage={mcp.percentage}
						barMode={barMode}
						testId="zai-mcp-bar"
						fillTestId="zai-mcp-fill"
						footer={
							mcp.usageDetails && mcp.usageDetails.length > 0 ? (
								<div className="ui-quota-rows">
									{mcp.usageDetails.map((d) => (
										<div
											key={d.modelCode}
											className="ui-quota-row"
											data-testid={`zai-mcp-detail-${d.modelCode}`}
										>
											<span>{d.modelCode}</span>
											<span>
												{d.usage} {t("used")}
											</span>
										</div>
									))}
								</div>
							) : null
						}
					>
						{resetSublabelFromEpoch(mcp.nextResetTime, t, time)}
					</QuotaBar>
				)}
			</>
		),
	});
}
