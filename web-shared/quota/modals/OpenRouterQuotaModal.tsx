import { formatDollars } from "../../format";
import type { OpenRouterBalance } from "../types";
import { type QuotaBodyProps, resetSublabel } from "./labels";
import { QuotaBar, QuotaDetailGrid, QuotaDetailItem } from "./primitives";

export function OpenRouterQuotaModal({
	providerName,
	payload,
	barMode,
	kit: { shell, t, time },
}: QuotaBodyProps<OpenRouterBalance>) {
	const hasCredits = payload.credits_total > 0;
	const creditsPctUsed = hasCredits
		? (payload.credits_used / payload.credits_total) * 100
		: 0;

	const limit = payload.limit;
	const limitRemaining = payload.limit_remaining ?? 0;
	// The `limit > 0` guard avoids a division by zero.
	const limitPctUsed =
		limit != null && limit > 0 ? 100 - (limitRemaining / limit) * 100 : 0;

	return shell({
		title: t("openRouterCredits", { provider: providerName }),
		subtitle: payload.is_free_tier ? (
			<span data-testid="or-tier-free">{t("freeTier")}</span>
		) : (
			<span data-testid="or-tier-paid">{t("paidAccount")}</span>
		),
		children: (
			<>
				{hasCredits ? (
					<QuotaBar
						label={t("accountBalance")}
						rightText={formatDollars(payload.credits_remaining)}
						percentage={creditsPctUsed}
						barMode={barMode}
						testId="or-credits-bar"
						fillTestId="or-credits-fill"
					>
						{t("spentTotal", { amount: formatDollars(payload.credits_used) })}
					</QuotaBar>
				) : (
					<QuotaDetailGrid columns={2}>
						<QuotaDetailItem
							label={t("accountBalance")}
							value={formatDollars(payload.credits_remaining)}
							span
						/>
					</QuotaDetailGrid>
				)}

				{limit != null && (
					<QuotaBar
						label={t("keySpendingLimit")}
						rightText={`${formatDollars(limitRemaining)} ${t("remaining")}`}
						percentage={limitPctUsed}
						barMode={barMode}
						testId="or-limit-bar"
						fillTestId="or-limit-fill"
					>
						{limit > 0 ? (
							<span data-testid="or-limit-reset">
								{resetSublabel(payload.limit_reset || null, t, time)}
							</span>
						) : (
							<span data-testid="or-limit-reached">{t("limitReset")}</span>
						)}
					</QuotaBar>
				)}

				{/* The usage figures are the key's own spend, not the account's:
				    say so, or they read as contradicting the balance above. */}
				<p className="ui-quota-bar-sub" data-testid="or-key-usage-hint">
					{t("spendingByThisKey")}
				</p>
				<QuotaDetailGrid columns={2}>
					<QuotaDetailItem
						label={t("today")}
						value={formatDollars(payload.usage_daily)}
						testId="or-usage-daily"
					/>
					<QuotaDetailItem
						label={t("thisWeek")}
						value={formatDollars(payload.usage_weekly)}
						testId="or-usage-weekly"
					/>
					<QuotaDetailItem
						label={t("thisMonth")}
						value={formatDollars(payload.usage_monthly)}
						testId="or-usage-monthly"
					/>
					<QuotaDetailItem
						label={t("allTime")}
						value={formatDollars(payload.usage)}
						testId="or-usage-all"
					/>
				</QuotaDetailGrid>
			</>
		),
	});
}
