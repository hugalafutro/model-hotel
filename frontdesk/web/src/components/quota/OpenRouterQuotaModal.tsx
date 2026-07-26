import { useTranslation } from "react-i18next";
import type { OpenRouterBalance } from "../../api/types";
import { formatDollars } from "../../utils/format";
import type { QuotaBarMode } from "../../utils/quota";
import {
	QuotaBar,
	QuotaDetailGrid,
	QuotaDetailItem,
	QuotaModalShell,
	resetSublabel,
} from "./shared";

export interface OpenRouterQuotaModalProps {
	providerName: string;
	payload: OpenRouterBalance;
	fetchedAt: string;
	barMode: QuotaBarMode;
	onToggleBarMode: () => void;
	onRefresh: () => void;
	isRefreshing: boolean;
	onClose: () => void;
}

export function OpenRouterQuotaModal({
	providerName,
	payload,
	fetchedAt,
	barMode,
	onToggleBarMode,
	onRefresh,
	isRefreshing,
	onClose,
}: OpenRouterQuotaModalProps) {
	const { t } = useTranslation();

	const hasCredits = payload.credits_total > 0;
	const creditsPctUsed = hasCredits
		? (payload.credits_used / payload.credits_total) * 100
		: 0;

	const limit = payload.limit;
	const limitRemaining = payload.limit_remaining ?? 0;
	const limitPctUsed =
		limit != null && limit > 0 ? 100 - (limitRemaining / limit) * 100 : 0;

	return (
		<QuotaModalShell
			title={t("quota.modal.openRouterTitle", { provider: providerName })}
			subtitle={
				payload.is_free_tier
					? t("quota.modal.freeTier")
					: t("quota.modal.paidAccount")
			}
			barMode={barMode}
			onToggleBarMode={onToggleBarMode}
			onRefresh={onRefresh}
			isRefreshing={isRefreshing}
			fetchedAt={fetchedAt}
			onClose={onClose}
		>
			{hasCredits ? (
				<QuotaBar
					label={t("quota.modal.accountBalance")}
					rightText={formatDollars(payload.credits_remaining)}
					percentage={creditsPctUsed}
					barMode={barMode}
					testId="or-credits-bar"
					fillTestId="or-credits-fill"
				>
					{t("quota.modal.spentTotal", {
						amount: formatDollars(payload.credits_used),
					})}
				</QuotaBar>
			) : (
				<QuotaDetailGrid columns={2}>
					<QuotaDetailItem
						label={t("quota.modal.accountBalance")}
						value={formatDollars(payload.credits_remaining)}
						span
					/>
				</QuotaDetailGrid>
			)}

			{limit != null && (
				<QuotaBar
					label={t("quota.modal.keySpendingLimit")}
					rightText={`${formatDollars(limitRemaining)} ${t("quota.modal.remaining")}`}
					percentage={limitPctUsed}
					barMode={barMode}
					testId="or-limit-bar"
					fillTestId="or-limit-fill"
				>
					{limit > 0
						? resetSublabel(payload.limit_reset || null, t)
						: t("quota.modal.limitReset")}
				</QuotaBar>
			)}

			<QuotaDetailGrid columns={2}>
				<QuotaDetailItem
					label={t("quota.modal.today")}
					value={formatDollars(payload.usage_daily)}
					testId="or-usage-daily"
				/>
				<QuotaDetailItem
					label={t("quota.modal.thisWeek")}
					value={formatDollars(payload.usage_weekly)}
					testId="or-usage-weekly"
				/>
				<QuotaDetailItem
					label={t("quota.modal.thisMonth")}
					value={formatDollars(payload.usage_monthly)}
					testId="or-usage-monthly"
				/>
				<QuotaDetailItem
					label={t("quota.modal.allTime")}
					value={formatDollars(payload.usage)}
					testId="or-usage-all"
				/>
			</QuotaDetailGrid>
		</QuotaModalShell>
	);
}
