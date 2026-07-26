import { useTranslation } from "react-i18next";
import type { ZAICodingQuotaResponse } from "../../api/types";
import {
	getZaiCodingFiveHourLimit,
	getZaiCodingWeeklyLimit,
	type QuotaBarMode,
} from "../../utils/quota";
import {
	QuotaBar,
	QuotaModalShell,
	quotaRightText,
	resetSublabelFromEpoch,
} from "./shared";

export interface ZAICodingQuotaModalProps {
	providerName: string;
	payload: ZAICodingQuotaResponse;
	fetchedAt: string;
	barMode: QuotaBarMode;
	onToggleBarMode: () => void;
	onRefresh: () => void;
	isRefreshing: boolean;
	onClose: () => void;
}

export function ZAICodingQuotaModal({
	providerName,
	payload,
	fetchedAt,
	barMode,
	onToggleBarMode,
	onRefresh,
	isRefreshing,
	onClose,
}: ZAICodingQuotaModalProps) {
	const { t } = useTranslation();

	const fiveHour = getZaiCodingFiveHourLimit(payload);
	const weekly = getZaiCodingWeeklyLimit(payload);
	const mcp = payload.data?.limits?.find(
		(l) => l.type === "TIME_LIMIT" && l.unit === 5,
	);

	// Z.ai reports percent USED directly, which is what QuotaBar wants.
	const rightText = (used: number) => quotaRightText(used, barMode, t);

	return (
		<QuotaModalShell
			title={t("quota.modal.zaiTitle", { provider: providerName })}
			subtitle={`${t("quota.modal.plan")}: ${payload.data?.level ?? "-"}`}
			barMode={barMode}
			onToggleBarMode={onToggleBarMode}
			onRefresh={onRefresh}
			isRefreshing={isRefreshing}
			fetchedAt={fetchedAt}
			onClose={onClose}
		>
			{fiveHour && (
				<QuotaBar
					label={t("quota.modal.hourTokenQuota", { hours: 5 })}
					rightText={rightText(fiveHour.percentage)}
					percentage={fiveHour.percentage}
					barMode={barMode}
					testId="zai-5h-bar"
					fillTestId="zai-5h-fill"
				>
					{resetSublabelFromEpoch(fiveHour.nextResetTime, t)}
				</QuotaBar>
			)}

			{weekly && (
				<QuotaBar
					label={t("quota.modal.weeklyTokenQuota")}
					rightText={rightText(weekly.percentage)}
					percentage={weekly.percentage}
					barMode={barMode}
					testId="zai-weekly-bar"
					fillTestId="zai-weekly-fill"
				>
					{resetSublabelFromEpoch(weekly.nextResetTime, t)}
				</QuotaBar>
			)}

			{mcp && (
				<QuotaBar
					label={t("quota.modal.mcpTimeQuota")}
					rightText={rightText(mcp.percentage)}
					percentage={mcp.percentage}
					barMode={barMode}
					testId="zai-mcp-bar"
					fillTestId="zai-mcp-fill"
					footer={
						mcp.usageDetails && mcp.usageDetails.length > 0 ? (
							<div className="fd-quota-rows">
								{mcp.usageDetails.map((d) => (
									<div
										key={d.modelCode}
										className="fd-quota-row"
										data-testid={`zai-mcp-detail-${d.modelCode}`}
									>
										<span>{d.modelCode}</span>
										<span>{d.usage}</span>
									</div>
								))}
							</div>
						) : null
					}
				>
					{resetSublabelFromEpoch(mcp.nextResetTime, t)}
				</QuotaBar>
			)}
		</QuotaModalShell>
	);
}
