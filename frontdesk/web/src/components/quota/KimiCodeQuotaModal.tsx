import { useTranslation } from "react-i18next";
import type { KimiCodeQuotaResponse } from "../../api/types";
import {
	getKimiCodeFiveHourLimit,
	getKimiCodeWeeklyLimit,
	type QuotaBarMode,
} from "../../utils/quota";
import {
	QuotaBar,
	QuotaModalShell,
	quotaRightText,
	resetSublabel,
} from "./shared";

export interface KimiCodeQuotaModalProps {
	providerName: string;
	payload: KimiCodeQuotaResponse;
	fetchedAt: string;
	barMode: QuotaBarMode;
	onToggleBarMode: () => void;
	onRefresh: () => void;
	isRefreshing: boolean;
	onClose: () => void;
}

export function KimiCodeQuotaModal({
	providerName,
	payload,
	fetchedAt,
	barMode,
	onToggleBarMode,
	onRefresh,
	isRefreshing,
	onClose,
}: KimiCodeQuotaModalProps) {
	const { t } = useTranslation();

	const fiveHour = getKimiCodeFiveHourLimit(payload);
	const weekly = getKimiCodeWeeklyLimit(payload);
	const parallelLimit = payload.parallel?.limit;
	const totalQuota = payload.totalQuota;

	// The window helpers already return percent USED.
	const rightText = (used: number) => quotaRightText(used, barMode, t);

	return (
		<QuotaModalShell
			title={t("quota.modal.kimiTitle", { provider: providerName })}
			subtitle={`${t("quota.modal.plan")}: ${payload.user?.membership?.level ?? "-"}`}
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
					testId="kimi-5h-bar"
					fillTestId="kimi-5h-fill"
				>
					{resetSublabel(fiveHour.resetTime, t)}
				</QuotaBar>
			)}

			{weekly && (
				<QuotaBar
					label={t("quota.modal.weeklyTokenQuota")}
					rightText={rightText(weekly.percentage)}
					percentage={weekly.percentage}
					barMode={barMode}
					testId="kimi-weekly-bar"
					fillTestId="kimi-weekly-fill"
				>
					{resetSublabel(weekly.resetTime, t)}
				</QuotaBar>
			)}

			{(parallelLimit != null || totalQuota) && (
				<div className="fd-quota-rows">
					{parallelLimit != null && (
						<div className="fd-quota-row" data-testid="kimi-parallel">
							<span>{t("quota.modal.kimiParallelLimit")}</span>
							<span>{parallelLimit}</span>
						</div>
					)}
					{totalQuota && (
						<div className="fd-quota-row" data-testid="kimi-total-quota">
							<span>{t("quota.modal.kimiTotalQuota")}</span>
							<span>
								{totalQuota.remaining ?? "-"} / {totalQuota.limit ?? "-"}
							</span>
						</div>
					)}
				</div>
			)}
		</QuotaModalShell>
	);
}
