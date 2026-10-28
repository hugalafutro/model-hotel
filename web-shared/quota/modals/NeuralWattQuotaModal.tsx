import {
	formatCount,
	formatDollars,
	formatKwh,
	formatTokens,
} from "../../format";
import type {
	NeuralWattQuotaResponse,
	NeuralWattQuotaUsagePeriod,
} from "../types";
import { type QuotaBodyProps, resetSublabel } from "./labels";
import { QuotaBar, QuotaDetailGrid, QuotaDetailItem } from "./primitives";

/**
 * "$cost · requests · tokens · kWh" for one usage period, from whichever of the
 * four figures the provider reported; empty when it reported none.
 */
function periodSummary(p: NeuralWattQuotaUsagePeriod | undefined): string {
	if (!p) return "";
	return [
		p.cost_usd != null && formatDollars(p.cost_usd),
		p.requests != null && formatCount(p.requests),
		p.tokens != null && formatTokens(p.tokens),
		p.energy_kwh != null && `${formatKwh(p.energy_kwh)} kWh`,
	]
		.filter(Boolean)
		.join(" · ");
}

export function NeuralWattQuotaModal({
	providerName,
	payload,
	barMode,
	kit: { shell, t, time },
}: QuotaBodyProps<NeuralWattQuotaResponse>) {
	// Only `balance` is assured: the badge gate keys on it, everything else is
	// whatever the provider put in a 200 (see NeuralWattQuotaResponse). Each
	// figure below therefore stands or falls on its own fields, and one whose
	// fields are missing is left out entirely rather than rendered as a dash
	// (or, for a number, thrown on by its formatter).
	const { balance, subscription, limits, key } = payload;
	const currentMonth = periodSummary(payload.usage?.current_month);
	const lifetime = periodSummary(payload.usage?.lifetime);

	const kwhIncluded = subscription?.kwh_included ?? 0;
	const kwhUsed = subscription?.kwh_used;
	const kwhRemaining = subscription?.kwh_remaining;
	const periodStart = subscription?.current_period_start;
	const periodEnd = subscription?.current_period_end;
	// null and absent differ below: a null overage limit or allowance means
	// "none"/"unlimited", an absent one was simply not reported.
	const overageLimit = limits?.overage_limit_usd;
	const tier = limits?.rate_limit_tier;
	const allowance = key?.allowance;
	const showLimits =
		overageLimit !== undefined || !!tier || allowance !== undefined;

	return shell({
		title: t("neuralWattCredits", { provider: providerName }),
		subtitle: subscription?.in_overage ? (
			<span data-testid="nw-status-overage">{t("neuralwattInOverage")}</span>
		) : (
			subscription?.status && (
				<span data-testid="nw-status">{subscription.status}</span>
			)
		),
		children: (
			<>
				{/* Just the number, no bar: NeuralWatt exposes no cumulative draw
				    (credits_used_usd is a hardwired 0 and total_credits_usd
				    re-bases to remaining as spend settles), so a credits bar could
				    only ever render as untouched. */}
				<QuotaDetailGrid columns={2}>
					<QuotaDetailItem
						label={t("accountBalance")}
						value={
							balance.credits_remaining_usd != null
								? formatDollars(balance.credits_remaining_usd)
								: "-"
						}
						span
					/>
				</QuotaDetailGrid>

				{kwhIncluded > 0 && kwhUsed != null && (
					<QuotaBar
						label={t("neuralwattEnergyQuota")}
						rightText={`${formatKwh(kwhUsed)} / ${formatKwh(kwhIncluded)} kWh`}
						percentage={(kwhUsed / kwhIncluded) * 100}
						// The provider reports used and remaining kWh separately and
						// they do not always add up to the included amount, so the
						// remaining bar reads its own figure rather than the complement
						// (falling back to it only when no remaining figure came).
						remainingPercentage={
							kwhRemaining != null
								? (kwhRemaining / kwhIncluded) * 100
								: undefined
						}
						barMode={barMode}
						testId="nw-kwh-bar"
						fillTestId="nw-kwh-fill"
					>
						{(kwhRemaining != null || periodEnd) && (
							<>
								{kwhRemaining != null && (
									<span data-testid="nw-kwh-remaining">
										{`${formatKwh(kwhRemaining)} kWh ${t("remaining")}`}
									</span>
								)}
								{kwhRemaining != null && periodEnd && "\n"}
								{periodEnd && (
									<span data-testid="nw-kwh-reset">
										{resetSublabel(periodEnd, t, time)}
									</span>
								)}
							</>
						)}
					</QuotaBar>
				)}

				{/* In overage the provider freezes kwh_used at the included amount and
				    bills further usage against the credit balance, so the bar above
				    stops moving; say where the spend actually goes. */}
				{subscription?.in_overage && (
					<p className="ui-quota-overage-note" data-testid="nw-overage-note">
						{t("neuralwattOverageNote")}
					</p>
				)}

				<QuotaDetailGrid columns={2}>
					{subscription?.plan && (
						<QuotaDetailItem label={t("plan")} value={subscription.plan} />
					)}
					{subscription?.billing_interval && (
						<QuotaDetailItem
							label={t("neuralwattBillingInterval")}
							value={subscription.billing_interval}
						/>
					)}
					{periodStart && periodEnd && (
						<QuotaDetailItem
							label={t("neuralwattBillingPeriod")}
							value={`${time.absolute(periodStart)} - ${time.absolute(periodEnd)}`}
							span
						/>
					)}
					{subscription?.auto_renew != null && (
						<QuotaDetailItem
							label={t("neuralwattAutoRenew")}
							value={subscription.auto_renew ? t("yes") : t("no")}
							testId="nw-auto-renew"
						/>
					)}
					<QuotaDetailItem
						label={t("neuralwattAccountingMethod")}
						value={balance.accounting_method || t("none")}
						testId="nw-accounting-method"
					/>
				</QuotaDetailGrid>

				{(currentMonth || lifetime) && (
					<div className="ui-quota-rows">
						{currentMonth && (
							<div className="ui-quota-row" data-testid="nw-usage-current">
								<span>{t("neuralwattCurrentMonth")}</span>
								<span>{currentMonth}</span>
							</div>
						)}
						{lifetime && (
							<div className="ui-quota-row" data-testid="nw-usage-lifetime">
								<span>{t("neuralwattLifetime")}</span>
								<span>{lifetime}</span>
							</div>
						)}
					</div>
				)}

				{showLimits && (
					<QuotaDetailGrid columns={3}>
						{overageLimit !== undefined && (
							<QuotaDetailItem
								label={t("neuralwattOverageLimit")}
								value={
									overageLimit !== null
										? formatDollars(overageLimit)
										: t("none")
								}
								testId="nw-overage-limit"
							/>
						)}
						{tier && (
							<QuotaDetailItem
								label={t("neuralwattRateLimitTier")}
								value={tier}
								testId="nw-rate-limit-tier"
							/>
						)}
						{allowance !== undefined && (
							<QuotaDetailItem
								label={t("neuralwattAllowance")}
								value={
									allowance !== null ? formatDollars(allowance) : t("unlimited")
								}
								testId="nw-allowance"
							/>
						)}
					</QuotaDetailGrid>
				)}
			</>
		),
	});
}
