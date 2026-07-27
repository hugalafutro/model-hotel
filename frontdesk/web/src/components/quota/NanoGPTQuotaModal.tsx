import { useTranslation } from "react-i18next";
import type { NanoGPTUsage } from "../../api/types";
import { formatCount, formatTokens } from "../../utils/format";
import type { QuotaBarMode } from "../../utils/quota";
import { formatAbsolute } from "../../utils/time";
import {
	QuotaBar,
	QuotaDetailGrid,
	QuotaDetailItem,
	QuotaModalShell,
	resetSublabelFromEpoch,
} from "./shared";

export interface NanoGPTQuotaModalProps {
	providerName: string;
	payload: NanoGPTUsage;
	fetchedAt: string;
	barMode: QuotaBarMode;
	onToggleBarMode: () => void;
	onRefresh: () => void;
	isRefreshing: boolean;
	onClose: () => void;
}

export function NanoGPTQuotaModal({
	providerName,
	payload,
	fetchedAt,
	barMode,
	onToggleBarMode,
	onRefresh,
	isRefreshing,
	onClose,
}: NanoGPTQuotaModalProps) {
	const { t } = useTranslation();

	const weeklyLimit = payload.limits?.weeklyInputTokens ?? 0;
	const weeklyUsed = payload.weeklyInputTokens?.used ?? 0;
	const weeklyPctUsed = weeklyLimit > 0 ? (weeklyUsed / weeklyLimit) * 100 : 0;

	// NanoGPT reports percentUsed as a fraction (0 to 1), not a percentage.
	const images = payload.dailyImages;
	const dailyTokens = payload.dailyInputTokens;

	return (
		<QuotaModalShell
			title={t("quota.modal.nanoTitle", { provider: providerName })}
			subtitle={
				payload.active ? (
					<span data-testid="nano-status-active">
						{t("quota.modal.active")}
					</span>
				) : (
					<span data-testid="nano-status-inactive">
						{t("quota.modal.inactive")}
					</span>
				)
			}
			barMode={barMode}
			onToggleBarMode={onToggleBarMode}
			onRefresh={onRefresh}
			isRefreshing={isRefreshing}
			fetchedAt={fetchedAt}
			onClose={onClose}
		>
			<QuotaBar
				label={t("quota.modal.weeklyTokenQuota")}
				rightText={`${formatTokens(weeklyUsed)} / ${formatTokens(weeklyLimit)}`}
				percentage={weeklyPctUsed}
				barMode={barMode}
				testId="nano-weekly-bar"
				fillTestId="nano-weekly-fill"
			>
				{weeklyLimit > 0
					? resetSublabelFromEpoch(payload.weeklyInputTokens?.resetAt, t)
					: t("quota.modal.noLimitSet")}
			</QuotaBar>

			{images && (
				<QuotaBar
					label={t("quota.modal.dailyImages")}
					rightText={`${formatCount(images.used)} / ${
						payload.limits?.dailyImages != null
							? formatCount(payload.limits.dailyImages)
							: t("quota.modal.unlimited")
					}`}
					percentage={images.percentUsed * 100}
					barMode={barMode}
					testId="nano-images-bar"
					fillTestId="nano-images-fill"
				>
					{resetSublabelFromEpoch(images.resetAt, t)}
				</QuotaBar>
			)}

			{dailyTokens && (
				<QuotaBar
					label={t("quota.modal.dailyInputTokens")}
					rightText={`${formatTokens(dailyTokens.used)} / ${
						payload.limits?.dailyInputTokens
							? formatTokens(payload.limits.dailyInputTokens)
							: t("quota.modal.unlimited")
					}`}
					percentage={dailyTokens.percentUsed * 100}
					barMode={barMode}
					testId="nano-daily-tokens-bar"
					fillTestId="nano-daily-tokens-fill"
				>
					{resetSublabelFromEpoch(dailyTokens.resetAt, t)}
				</QuotaBar>
			)}

			<QuotaDetailGrid columns={2}>
				<QuotaDetailItem
					label={t("quota.modal.provider")}
					value={payload.provider}
				/>
				<QuotaDetailItem
					label={t("quota.modal.status")}
					value={payload.providerStatus}
				/>
				<QuotaDetailItem
					label={t("quota.modal.periodEnd")}
					value={formatAbsolute(payload.period?.currentPeriodEnd)}
				/>
				<QuotaDetailItem
					label={t("quota.modal.allowOverage")}
					value={
						payload.allowOverage ? t("quota.modal.yes") : t("quota.modal.no")
					}
					testId="nano-allow-overage"
				/>
			</QuotaDetailGrid>

			{payload.cancelAtPeriodEnd && (
				<p className="fd-quota-notice" data-testid="nano-cancel-notice">
					{t("quota.modal.cancelAtPeriodEnd", {
						date: formatAbsolute(payload.period?.currentPeriodEnd),
					})}
				</p>
			)}
		</QuotaModalShell>
	);
}
