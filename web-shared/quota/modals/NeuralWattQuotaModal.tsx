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
import type { QuotaBodyProps } from "./labels";
import { QuotaBar, QuotaDetailGrid, QuotaDetailItem } from "./primitives";

/** "$cost · requests · tokens · kWh" for one usage period. */
function periodSummary(p: NeuralWattQuotaUsagePeriod): string {
	return `${formatDollars(p.cost_usd)} · ${formatCount(p.requests)} · ${formatTokens(p.tokens)} · ${formatKwh(p.energy_kwh)} kWh`;
}

export function NeuralWattQuotaModal({
	providerName,
	payload,
	barMode,
	kit: { shell, t, time },
}: QuotaBodyProps<NeuralWattQuotaResponse>) {
	// Only `balance` is assured: the badge gate keys on it, everything else is
	// whatever the provider put in a 200 (see NeuralWattQuotaResponse). Each
	// block below therefore stands or falls on its own object, and a block whose
	// object is missing is left out entirely rather than rendered as a row of
	// dashes.
	const { balance, subscription, usage, limits, key } = payload;
	const currentMonth = usage?.current_month;
	const lifetime = usage?.lifetime;

	const kwhIncluded = subscription?.kwh_included ?? 0;
	const hasKwh = kwhIncluded > 0;

	return shell({
		title: t("neuralWattCredits", { provider: providerName }),
		subtitle:
			subscription &&
			(subscription.in_overage ? (
				<span data-testid="nw-status-overage">{t("neuralwattInOverage")}</span>
			) : (
				<span data-testid="nw-status">{subscription.status}</span>
			)),
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

				{subscription && hasKwh && (
					<QuotaBar
						label={t("neuralwattEnergyQuota")}
						rightText={`${formatKwh(subscription.kwh_used)} / ${formatKwh(kwhIncluded)} kWh`}
						percentage={(subscription.kwh_used / kwhIncluded) * 100}
						// The provider reports used and remaining kWh separately and
						// they do not always add up to the included amount, so the
						// remaining bar reads its own figure rather than the complement.
						remainingPercentage={
							(subscription.kwh_remaining / kwhIncluded) * 100
						}
						barMode={barMode}
						testId="nw-kwh-bar"
						fillTestId="nw-kwh-fill"
					>
						{`${formatKwh(subscription.kwh_remaining)} kWh ${t("remaining")}`}
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
					{subscription && (
						<>
							<QuotaDetailItem label={t("plan")} value={subscription.plan} />
							<QuotaDetailItem
								label={t("neuralwattBillingInterval")}
								value={subscription.billing_interval}
							/>
							<QuotaDetailItem
								label={t("neuralwattBillingPeriod")}
								value={`${time.absolute(subscription.current_period_start)} - ${time.absolute(subscription.current_period_end)}`}
								span
							/>
							<QuotaDetailItem
								label={t("neuralwattAutoRenew")}
								value={subscription.auto_renew ? t("yes") : t("no")}
								testId="nw-auto-renew"
							/>
						</>
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
								<span>{periodSummary(currentMonth)}</span>
							</div>
						)}
						{lifetime && (
							<div className="ui-quota-row" data-testid="nw-usage-lifetime">
								<span>{t("neuralwattLifetime")}</span>
								<span>{periodSummary(lifetime)}</span>
							</div>
						)}
					</div>
				)}

				{(limits || key) && (
					<QuotaDetailGrid columns={3}>
						{limits && (
							<>
								<QuotaDetailItem
									label={t("neuralwattOverageLimit")}
									value={
										limits.overage_limit_usd !== null
											? formatDollars(limits.overage_limit_usd)
											: t("none")
									}
									testId="nw-overage-limit"
								/>
								<QuotaDetailItem
									label={t("neuralwattRateLimitTier")}
									value={limits.rate_limit_tier}
								/>
							</>
						)}
						{key && (
							<QuotaDetailItem
								label={t("neuralwattAllowance")}
								value={
									key.allowance !== null
										? formatDollars(key.allowance)
										: t("unlimited")
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
