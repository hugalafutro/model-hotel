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
	// Only credits_remaining is guaranteed (the visibility gate checks it), so
	// every other figure renders only when the snapshot carries it.
	const creditsTotal = payload.credits_total ?? 0;
	const creditsUsed = payload.credits_used;
	const hasCredits = creditsTotal > 0 && Number.isFinite(creditsUsed);
	const creditsPctUsed = hasCredits
		? ((creditsUsed as number) / creditsTotal) * 100
		: 0;
	const usageTiles = [
		[t("today"), payload.usage_daily, "or-usage-daily"],
		[t("thisWeek"), payload.usage_weekly, "or-usage-weekly"],
		[t("thisMonth"), payload.usage_monthly, "or-usage-monthly"],
		[t("allTime"), payload.usage, "or-usage-all"],
	] as const;
	const shownUsage = usageTiles.filter(([, value]) => Number.isFinite(value));

	const limit = payload.limit;
	const limitRemaining = payload.limit_remaining ?? 0;
	// The `limit > 0` guard avoids a division by zero.
	const limitPctUsed =
		limit != null && limit > 0 ? 100 - (limitRemaining / limit) * 100 : 0;
	const limitReset = resetSublabel(payload.limit_reset || null, t, time);

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
						{t("spentTotal", { amount: formatDollars(creditsUsed as number) })}
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

				{/* A zero limit blocks all spending: an empty amber bar in both
				    modes, never the full healthy one 0% used would derive. */}
				{limit != null && (
					<QuotaBar
						label={t("keySpendingLimit")}
						rightText={`${formatDollars(limitRemaining)} ${t("remaining")}`}
						percentage={limitPctUsed}
						{...(limit > 0
							? {}
							: { remainingPercentage: 0, tone: "warn" as const })}
						barMode={barMode}
						testId="or-limit-bar"
						fillTestId="or-limit-fill"
					>
						{limit > 0 ? (
							<span data-testid="or-limit-reset">{limitReset}</span>
						) : (
							<span data-testid="or-limit-reached">
								{`${formatDollars(0)} ${t("limitReset")}`}
								{limitReset !== "-" && ` · ${limitReset}`}
							</span>
						)}
					</QuotaBar>
				)}

				{/* The usage figures are the key's own spend, not the account's:
				    say so, or they read as contradicting the balance above. */}
				{shownUsage.length > 0 && (
					<>
						<p className="ui-quota-bar-sub" data-testid="or-key-usage-hint">
							{t("spendingByThisKey")}
						</p>
						<QuotaDetailGrid columns={2}>
							{shownUsage.map(([label, value, testId]) => (
								<QuotaDetailItem
									key={testId}
									label={label}
									value={formatDollars(value as number)}
									testId={testId}
								/>
							))}
						</QuotaDetailGrid>
					</>
				)}
			</>
		),
	});
}
