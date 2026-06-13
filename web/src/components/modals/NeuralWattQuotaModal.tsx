import { useTranslation } from "react-i18next";
import type { NeuralWattQuotaResponse } from "../../api/types";
import { useLocalStorage } from "../../hooks/useLocalStorage";
import {
	formatDate,
	formatDollars,
	formatKwh,
	formatRelativeTime,
	formatTokens,
} from "../../utils/format";
import { Modal } from "../Modal";
import {
	QuotaModalHeaderActions,
	remainingBarColor,
	usedBarColor,
} from "./shared";

export function NeuralWattQuotaModal({
	quota,
	onClose,
	onRefresh,
	isRefreshing,
	onToast,
	lastRefreshed,
}: {
	quota: NeuralWattQuotaResponse;
	onClose: () => void;
	onRefresh: () => Promise<unknown>;
	isRefreshing: boolean;
	onToast: (msg: string, type: "success" | "error" | "info") => void;
	lastRefreshed?: number;
}) {
	const { t } = useTranslation();
	const [barMode, setBarMode] = useLocalStorage<"remaining" | "used">(
		"quota-bar-mode",
		"remaining",
	);

	const handleRefresh = async () => {
		try {
			await onRefresh();
			onToast(t("components.providerModals.quotaRefreshed"), "success");
		} catch {
			onToast(t("components.providerModals.failedToRefreshQuota"), "error");
		}
	};

	const creditsRemaining =
		quota.balance.total_credits_usd > 0
			? (quota.balance.credits_remaining_usd /
					quota.balance.total_credits_usd) *
				100
			: 100;

	const creditsUsed =
		quota.balance.total_credits_usd > 0
			? (quota.balance.credits_used_usd / quota.balance.total_credits_usd) * 100
			: 0;

	const kwhRemaining =
		quota.subscription.kwh_included > 0
			? (quota.subscription.kwh_remaining / quota.subscription.kwh_included) *
				100
			: 100;

	const kwhUsed =
		quota.subscription.kwh_included > 0
			? (quota.subscription.kwh_used / quota.subscription.kwh_included) * 100
			: 0;

	return (
		<Modal
			header={
				<div className="flex justify-between items-start mb-6">
					<div>
						<h2 className="text-xl font-bold text-(--text-primary)">
							{t("components.providerModals.neuralWattCredits")}
						</h2>
						<p className="text-sm text-(--text-tertiary) mt-1">
							<span className="inline-flex items-center gap-1.5">
								<span
									data-testid="neuralwatt-status-dot"
									className={`w-2 h-2 rounded-full ${quota.subscription.in_overage ? "bg-red-400" : quota.subscription.status === "active" ? "bg-green-400" : "bg-amber-400"}`}
								></span>
								<span className="capitalize">{quota.subscription.status}</span>
								{quota.subscription.in_overage && (
									<span className="text-red-400 text-xs">
										({t("components.providerModals.neuralwattInOverage")})
									</span>
								)}
							</span>
							{quota.balance.accounting_method && (
								<span className="text-(--text-muted) text-xs ml-2">
									{t("components.providerModals.neuralwattAccountingMethod")}:{" "}
									<span className="capitalize">
										{quota.balance.accounting_method}
									</span>
								</span>
							)}
						</p>
					</div>
					<QuotaModalHeaderActions
						onToggleBarMode={() =>
							setBarMode((prev) =>
								prev === "remaining" ? "used" : "remaining",
							)
						}
						onRefresh={handleRefresh}
						isRefreshing={isRefreshing}
						toggleAriaLabel={t("components.providerModals.toggleRemainingUsed")}
						toggleTitle={
							barMode === "remaining"
								? t("components.providerModals.showQuotaUsed")
								: t("components.providerModals.showQuotaRemaining")
						}
						refreshAriaLabel={t("common.refresh")}
						refreshTitle={t("components.providerModals.refreshQuotaInfo")}
					/>
				</div>
			}
			onClose={onClose}
			scrollable
		>
			<div className="space-y-6">
				{/* ── Credit balance bar ── */}
				<div>
					<div className="flex justify-between items-center mb-2">
						<span className="text-sm font-medium text-(--text-secondary)">
							{t("components.providerModals.neuralwattBalance")}
						</span>
						<span className="text-sm text-(--text-primary) font-medium">
							{formatDollars(quota.balance.credits_remaining_usd)}
						</span>
					</div>
					{quota.balance.total_credits_usd > 0 && (
						<div
							data-testid="neuralwatt-credits-bar"
							className="w-full bg-(--surface-input) rounded-full h-3"
						>
							<div
								className={`${barMode === "used" ? usedBarColor(creditsUsed) : remainingBarColor(creditsRemaining)} h-3 rounded-full transition-all`}
								style={{
									width: `${barMode === "used" ? Math.min(creditsUsed, 100) : Math.min(creditsRemaining, 100)}%`,
								}}
							/>
						</div>
					)}
					<p className="text-xs text-(--text-muted) mt-1">
						{quota.balance.total_credits_usd > 0
							? t("components.providerModals.spentTotal", {
									amount: formatDollars(quota.balance.credits_used_usd),
								})
							: t("components.providerModals.noCredits")}
					</p>
				</div>

				{/* ── kWh energy bar ── */}
				{quota.subscription.kwh_included > 0 && (
					<div>
						<div className="flex justify-between items-center mb-2">
							<span className="text-sm font-medium text-(--text-secondary)">
								{t("components.providerModals.neuralwattEnergyQuota")}
							</span>
							<span className="text-sm text-(--text-tertiary)">
								{formatKwh(quota.subscription.kwh_used)} /{" "}
								{formatKwh(quota.subscription.kwh_included)} kWh
							</span>
						</div>
						<div
							data-testid="neuralwatt-kwh-bar"
							className="w-full bg-(--surface-input) rounded-full h-3"
						>
							<div
								className={`${barMode === "used" ? usedBarColor(kwhUsed) : remainingBarColor(kwhRemaining)} h-3 rounded-full transition-all`}
								style={{
									width: `${barMode === "used" ? Math.min(kwhUsed, 100) : Math.min(kwhRemaining, 100)}%`,
								}}
							/>
						</div>
						<p className="text-xs text-(--text-muted) mt-1">
							{kwhUsed.toFixed(1)}% {t("components.providerModals.used")}.{" "}
							{formatKwh(quota.subscription.kwh_remaining)} kWh{" "}
							{t("components.providerModals.remaining")}
							{quota.subscription.current_period_end &&
								` · ${t("components.providerModals.resets")} ${formatDate(quota.subscription.current_period_end)}`}
						</p>
					</div>
				)}

				{/* ── Subscription details ── */}
				<div>
					<h3 className="text-sm font-medium text-(--text-secondary) mb-3">
						{t("components.providerModals.neuralwattSubscription")}
					</h3>
					<div className="grid grid-cols-2 gap-3 text-sm">
						<div>
							<span className="text-(--text-muted)">
								{t("components.providerModals.neuralwattPlan")}
							</span>
							<p className="text-gray-200 capitalize">
								{quota.subscription.plan}
							</p>
						</div>
						<div>
							<span className="text-(--text-muted)">
								{t("components.providerModals.neuralwattStatus")}
							</span>
							<p className="text-gray-200 capitalize">
								{quota.subscription.status}
							</p>
						</div>
						<div>
							<span className="text-(--text-muted)">
								{t("components.providerModals.neuralwattBillingPeriod")}
							</span>
							<p className="text-gray-200">
								{formatDate(quota.subscription.current_period_start)} -{" "}
								{formatDate(quota.subscription.current_period_end)}
							</p>
						</div>
						<div>
							<span className="text-(--text-muted)">
								{t("components.providerModals.neuralwattBillingInterval")}
							</span>
							<p className="text-gray-200 capitalize">
								{quota.subscription.billing_interval}
							</p>
						</div>
						<div>
							<span className="text-(--text-muted)">
								{t("components.providerModals.neuralwattAutoRenew")}
							</span>
							<p className="text-gray-200">
								{quota.subscription.auto_renew
									? t("components.providerModals.yes")
									: t("components.providerModals.no")}
							</p>
						</div>
						<div>
							<span className="text-(--text-muted)">
								{t("components.providerModals.neuralwattInOverage")}
							</span>
							<p className="text-gray-200">
								{quota.subscription.in_overage
									? t("components.providerModals.yes")
									: t("components.providerModals.no")}
							</p>
						</div>
					</div>
				</div>

				{/* ── Energy allocation ── */}
				<div>
					<h3 className="text-sm font-medium text-(--text-secondary) mb-3">
						{t("components.providerModals.neuralwattEnergyAllocation")}
					</h3>
					<div className="grid grid-cols-3 gap-3 text-sm">
						<div>
							<span className="text-(--text-muted)">
								{t("components.providerModals.neuralwattKwhIncluded")}
							</span>
							<p className="text-gray-200">
								{formatKwh(quota.subscription.kwh_included)} kWh
							</p>
						</div>
						<div>
							<span className="text-(--text-muted)">
								{t("components.providerModals.neuralwattKwhUsed")}
							</span>
							<p className="text-gray-200">
								{formatKwh(quota.subscription.kwh_used)} kWh
							</p>
						</div>
						<div>
							<span className="text-(--text-muted)">
								{t("components.providerModals.neuralwattKwhRemaining")}
							</span>
							<p
								className={`text-gray-200 ${quota.subscription.kwh_remaining < quota.subscription.kwh_included * 0.2 ? "text-amber-400" : ""}`}
							>
								{formatKwh(quota.subscription.kwh_remaining)} kWh
							</p>
						</div>
					</div>
				</div>

				{/* ── Usage stats ── */}
				<div>
					<h3 className="text-sm font-medium text-(--text-secondary) mb-3">
						{t("components.providerModals.neuralwattUsage")}
					</h3>
					<div className="grid grid-cols-5 gap-2 text-xs">
						<div></div>
						<div>
							<span className="text-(--text-muted)">
								{t("components.providerModals.neuralwattCost")}
							</span>
						</div>
						<div>
							<span className="text-(--text-muted)">
								{t("components.providerModals.neuralwattRequests")}
							</span>
						</div>
						<div>
							<span className="text-(--text-muted)">
								{t("components.providerModals.neuralwattTokens")}
							</span>
						</div>
						<div>
							<span className="text-(--text-muted)">
								{t("components.providerModals.neuralwattEnergy")}
							</span>
						</div>
						<span className="text-(--text-muted)">
							{t("components.providerModals.neuralwattCurrentMonth")}
						</span>
						<p className="text-gray-200">
							{formatDollars(quota.usage.current_month.cost_usd)}
						</p>
						<p className="text-gray-200">
							{quota.usage.current_month.requests.toLocaleString("en-US")}
						</p>
						<p className="text-gray-200">
							{formatTokens(quota.usage.current_month.tokens)}
						</p>
						<p className="text-gray-200">
							{formatKwh(quota.usage.current_month.energy_kwh)} kWh
						</p>
						<span className="text-(--text-muted)">
							{t("components.providerModals.neuralwattLifetime")}
						</span>
						<p className="text-gray-200">
							{formatDollars(quota.usage.lifetime.cost_usd)}
						</p>
						<p className="text-gray-200">
							{quota.usage.lifetime.requests.toLocaleString("en-US")}
						</p>
						<p className="text-gray-200">
							{formatTokens(quota.usage.lifetime.tokens)}
						</p>
						<p className="text-gray-200">
							{formatKwh(quota.usage.lifetime.energy_kwh)} kWh
						</p>
					</div>
				</div>

				{/* ── Limits ── */}
				<div>
					<h3 className="text-sm font-medium text-(--text-secondary) mb-3">
						{t("components.providerModals.limits")}
					</h3>
					<div className="grid grid-cols-3 gap-3 text-sm">
						<div>
							<span className="text-(--text-muted)">
								{t("components.providerModals.neuralwattOverageLimit")}
							</span>
							<p className="text-gray-200">
								{quota.limits.overage_limit_usd !== null
									? formatDollars(quota.limits.overage_limit_usd)
									: t("components.providerModals.none")}
							</p>
						</div>
						<div>
							<span className="text-(--text-muted)">
								{t("components.providerModals.neuralwattRateLimitTier")}
							</span>
							<p className="text-gray-200 capitalize">
								{quota.limits.rate_limit_tier}
							</p>
						</div>
						<div>
							<span className="text-(--text-muted)">
								{t("components.providerModals.neuralwattAllowance")}
							</span>
							<p className="text-gray-200">
								{quota.key.allowance !== null
									? formatDollars(quota.key.allowance)
									: t("common.unlimited")}
							</p>
						</div>
					</div>
				</div>

				{lastRefreshed ? (
					<div className="flex justify-between items-center text-xs text-(--text-muted) pt-2 ">
						<span>{t("components.providerModals.lastRefreshed")}</span>
						<span>
							{formatRelativeTime(new Date(lastRefreshed).toISOString())}
						</span>
					</div>
				) : null}
			</div>
		</Modal>
	);
}
