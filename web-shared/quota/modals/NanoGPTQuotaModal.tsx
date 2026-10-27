import { formatCount, formatTokens } from "../../format";
import type { NanoGPTUsage } from "../types";
import { type QuotaBodyProps, resetSublabelFromEpoch } from "./labels";
import { QuotaBar, QuotaDetailGrid, QuotaDetailItem } from "./primitives";

export function NanoGPTQuotaModal({
	providerName,
	payload,
	barMode,
	kit: { shell, t, time },
}: QuotaBodyProps<NanoGPTUsage>) {
	const weeklyLimit = payload.limits?.weeklyInputTokens ?? 0;
	const weeklyUsed = payload.weeklyInputTokens?.used ?? 0;
	const weeklyPctUsed = weeklyLimit > 0 ? (weeklyUsed / weeklyLimit) * 100 : 0;

	// NanoGPT reports percentUsed as a fraction (0 to 1, and past 1 once over
	// the allowance), not a percentage: "percentUsed is a fraction and may
	// exceed 1" (docs.nano-gpt.com, subscription usage endpoint).
	const images = payload.dailyImages;
	const dailyTokens = payload.dailyInputTokens;

	return shell({
		title: t("nanoGPTSubscription", { provider: providerName }),
		subtitle: payload.active ? (
			<span data-testid="nano-status-active">{t("active")}</span>
		) : (
			<span data-testid="nano-status-inactive">{t("inactive")}</span>
		),
		children: (
			<>
				<QuotaBar
					label={t("weeklyTokenQuota")}
					rightText={`${formatTokens(weeklyUsed)} / ${formatTokens(weeklyLimit)}`}
					percentage={weeklyPctUsed}
					barMode={barMode}
					testId="nano-weekly-bar"
					fillTestId="nano-weekly-fill"
				>
					{weeklyLimit > 0
						? resetSublabelFromEpoch(
								payload.weeklyInputTokens?.resetAt,
								t,
								time,
							)
						: t("noLimitSet")}
				</QuotaBar>

				{images && (
					<QuotaBar
						label={t("dailyImages")}
						rightText={`${formatCount(images.used)} / ${
							payload.limits?.dailyImages != null
								? formatCount(payload.limits.dailyImages)
								: t("unlimited")
						}`}
						percentage={images.percentUsed * 100}
						barMode={barMode}
						testId="nano-images-bar"
						fillTestId="nano-images-fill"
					>
						{resetSublabelFromEpoch(images.resetAt, t, time)}
					</QuotaBar>
				)}

				{dailyTokens && (
					<QuotaBar
						label={t("dailyInputTokens")}
						rightText={`${formatTokens(dailyTokens.used)} / ${
							payload.limits?.dailyInputTokens
								? formatTokens(payload.limits.dailyInputTokens)
								: t("unlimited")
						}`}
						percentage={dailyTokens.percentUsed * 100}
						barMode={barMode}
						testId="nano-daily-tokens-bar"
						fillTestId="nano-daily-tokens-fill"
					>
						{resetSublabelFromEpoch(dailyTokens.resetAt, t, time)}
					</QuotaBar>
				)}

				<QuotaDetailGrid columns={2}>
					<QuotaDetailItem label={t("provider")} value={payload.provider} />
					<QuotaDetailItem label={t("status")} value={payload.providerStatus} />
					<QuotaDetailItem
						label={t("periodEnd")}
						value={time.absolute(payload.period?.currentPeriodEnd)}
					/>
					<QuotaDetailItem
						label={t("allowOverage")}
						value={payload.allowOverage ? t("yes") : t("no")}
						testId="nano-allow-overage"
					/>
				</QuotaDetailGrid>

				{payload.cancelAtPeriodEnd && (
					<p className="ui-quota-notice" data-testid="nano-cancel-notice">
						{t("cancelAtPeriodEnd", {
							date: time.absolute(payload.period?.currentPeriodEnd),
						})}
					</p>
				)}
			</>
		),
	});
}
