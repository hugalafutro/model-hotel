import { getNeuralWattCreditsSpent } from "@quota-shared";
import { useTranslation } from "react-i18next";
import type { NeuralWattQuotaResponse } from "../../api/types";
import { formatDollars, formatKwh, formatTokens } from "../../utils/format";
import type { QuotaBarMode } from "../../utils/quota";
import { formatAbsolute } from "../../utils/time";
import {
	QuotaBar,
	QuotaDetailGrid,
	QuotaDetailItem,
	QuotaModalShell,
} from "./shared";

export interface NeuralWattQuotaModalProps {
	providerName: string;
	payload: NeuralWattQuotaResponse;
	fetchedAt: string;
	barMode: QuotaBarMode;
	onToggleBarMode: () => void;
	onRefresh: () => void;
	isRefreshing: boolean;
	onClose: () => void;
}

export function NeuralWattQuotaModal({
	providerName,
	payload,
	fetchedAt,
	barMode,
	onToggleBarMode,
	onRefresh,
	isRefreshing,
	onClose,
}: NeuralWattQuotaModalProps) {
	const { t } = useTranslation();
	// Only `balance` is assured: the badge gate keys on it, everything else is
	// whatever the provider put in a 200 (see NeuralWattQuotaResponse). Each
	// block below therefore stands or falls on its own object, and a block whose
	// object is missing is left out entirely rather than rendered as a row of
	// dashes, which is how this file already treats an absent energy quota or an
	// absent credit total.
	const { balance, subscription, usage, limits, key } = payload;
	const currentMonth = usage?.current_month;
	const lifetime = usage?.lifetime;

	const hasCredits = balance.total_credits_usd > 0;
	const creditsSpent = getNeuralWattCreditsSpent(balance);
	const creditsPctUsed = hasCredits
		? (creditsSpent / balance.total_credits_usd) * 100
		: 0;

	const hasKwh = (subscription?.kwh_included ?? 0) > 0;
	const kwhPctUsed =
		subscription && hasKwh
			? (subscription.kwh_used / subscription.kwh_included) * 100
			: 0;

	return (
		<QuotaModalShell
			title={t("quota.modal.neuralWattTitle", { provider: providerName })}
			subtitle={
				subscription &&
				(subscription.in_overage ? (
					<span data-testid="nw-status-overage">
						{t("quota.modal.inOverage")}
					</span>
				) : (
					<span data-testid="nw-status">{subscription.status}</span>
				))
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
					rightText={formatDollars(balance.credits_remaining_usd)}
					percentage={creditsPctUsed}
					barMode={barMode}
					testId="nw-credits-bar"
					fillTestId="nw-credits-fill"
				>
					{t("quota.modal.spentTotal", {
						amount: formatDollars(creditsSpent),
					})}
				</QuotaBar>
			) : (
				<QuotaDetailGrid columns={2}>
					<QuotaDetailItem
						label={t("quota.modal.accountBalance")}
						value={formatDollars(balance.credits_remaining_usd)}
						span
					/>
				</QuotaDetailGrid>
			)}

			{subscription && hasKwh && (
				<QuotaBar
					label={t("quota.modal.energyQuota")}
					rightText={`${formatKwh(subscription.kwh_used)} / ${formatKwh(subscription.kwh_included)} kWh`}
					percentage={kwhPctUsed}
					barMode={barMode}
					testId="nw-kwh-bar"
					fillTestId="nw-kwh-fill"
				>
					{`${formatKwh(subscription.kwh_remaining)} kWh ${t("quota.modal.remaining")}`}
				</QuotaBar>
			)}

			{/* In overage the provider freezes kwh_used at the included amount and
			    bills further usage against the credit balance, so the bars above
			    stop moving; say where the spend actually goes. */}
			{subscription?.in_overage && (
				<p className="fd-quota-overage-note" data-testid="nw-overage-note">
					{t("quota.modal.neuralwattOverageNote")}
				</p>
			)}

			<QuotaDetailGrid columns={2}>
				{subscription && (
					<>
						<QuotaDetailItem
							label={t("quota.modal.plan")}
							value={subscription.plan}
						/>
						<QuotaDetailItem
							label={t("quota.modal.billingInterval")}
							value={subscription.billing_interval}
						/>
						<QuotaDetailItem
							label={t("quota.modal.billingPeriod")}
							value={`${formatAbsolute(subscription.current_period_start)} - ${formatAbsolute(subscription.current_period_end)}`}
							span
						/>
						<QuotaDetailItem
							label={t("quota.modal.autoRenew")}
							value={
								subscription.auto_renew
									? t("quota.modal.yes")
									: t("quota.modal.no")
							}
							testId="nw-auto-renew"
						/>
					</>
				)}
				<QuotaDetailItem
					label={t("quota.modal.accountingMethod")}
					value={balance.accounting_method || t("quota.modal.none")}
					testId="nw-accounting-method"
				/>
			</QuotaDetailGrid>

			{(currentMonth || lifetime) && (
				<div className="fd-quota-rows">
					{currentMonth && (
						<div className="fd-quota-row" data-testid="nw-usage-current">
							<span>{t("quota.modal.currentMonth")}</span>
							<span>
								{formatDollars(currentMonth.cost_usd)} ·{" "}
								{currentMonth.requests.toLocaleString("en-US")} ·{" "}
								{formatTokens(currentMonth.tokens)} ·{" "}
								{formatKwh(currentMonth.energy_kwh)} kWh
							</span>
						</div>
					)}
					{lifetime && (
						<div className="fd-quota-row" data-testid="nw-usage-lifetime">
							<span>{t("quota.modal.lifetime")}</span>
							<span>
								{formatDollars(lifetime.cost_usd)} ·{" "}
								{lifetime.requests.toLocaleString("en-US")} ·{" "}
								{formatTokens(lifetime.tokens)} ·{" "}
								{formatKwh(lifetime.energy_kwh)} kWh
							</span>
						</div>
					)}
				</div>
			)}

			{(limits || key) && (
				<QuotaDetailGrid columns={3}>
					{limits && (
						<>
							<QuotaDetailItem
								label={t("quota.modal.overageLimit")}
								value={
									limits.overage_limit_usd !== null
										? formatDollars(limits.overage_limit_usd)
										: t("quota.modal.none")
								}
								testId="nw-overage-limit"
							/>
							<QuotaDetailItem
								label={t("quota.modal.rateLimitTier")}
								value={limits.rate_limit_tier}
							/>
						</>
					)}
					{key && (
						<QuotaDetailItem
							label={t("quota.modal.allowance")}
							value={
								key.allowance !== null
									? formatDollars(key.allowance)
									: t("quota.modal.unlimited")
							}
							testId="nw-allowance"
						/>
					)}
				</QuotaDetailGrid>
			)}
		</QuotaModalShell>
	);
}
