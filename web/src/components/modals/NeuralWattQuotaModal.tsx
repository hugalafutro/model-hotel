import { useTranslation } from "react-i18next";
import { Activity, Gauge, RefreshCw } from "@/lib/icons";
import type { NeuralWattQuotaResponse } from "../../api/types";
import {
	formatDate,
	formatDollars,
	formatKwh,
	formatTokens,
	formatWithCommas,
} from "../../utils/format";
import { DetailSectionHeader } from "../DetailSectionHeader";
import { DetailItem } from "../LogDetailItem";
import { Modal } from "../Modal";
import {
	LastRefreshedRow,
	type OnToast,
	QuotaBar,
	QuotaModalHeaderActions,
	useQuotaBarMode,
	useQuotaRefreshToast,
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
	onToast: OnToast;
	lastRefreshed?: number;
}) {
	const { t } = useTranslation();
	const [barMode, toggleBarMode] = useQuotaBarMode();

	const handleRefresh = useQuotaRefreshToast(onRefresh, onToast);

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
						</p>
					</div>
					<QuotaModalHeaderActions
						barMode={barMode}
						onToggleBarMode={toggleBarMode}
						onRefresh={handleRefresh}
						isRefreshing={isRefreshing}
					/>
				</div>
			}
			onClose={onClose}
			scrollable
		>
			<div className="space-y-6">
				{/* ── Credit balance ── */}
				{/* Just the number: NeuralWatt exposes no cumulative draw
				    (credits_used_usd is a hardwired 0 and total_credits_usd
				    re-bases to remaining as spend settles), so a bar or a
				    spent figure here could only ever render as untouched
				    credits / a fabricated $0.00. */}
				<div className="flex justify-between items-center">
					<span className="text-sm font-medium text-(--text-secondary)">
						{t("components.providerModals.neuralwattBalance")}
					</span>
					<span className="text-sm text-(--text-primary) font-medium">
						{quota.balance.credits_remaining_usd != null
							? formatDollars(quota.balance.credits_remaining_usd)
							: "-"}
					</span>
				</div>

				{/* ── kWh energy bar ── */}
				{quota.subscription.kwh_included > 0 && (
					<QuotaBar
						label={t("components.providerModals.neuralwattEnergyQuota")}
						rightText={`${formatKwh(quota.subscription.kwh_used)} / ${formatKwh(quota.subscription.kwh_included)} kWh`}
						percentage={kwhUsed}
						barMode={barMode}
						dataTestId="neuralwatt-kwh-bar"
						footer={
							quota.balance.accounting_method && (
								<p className="text-xs text-(--text-muted) mt-1">
									{t("components.providerModals.neuralwattAccountingMethod")}:{" "}
									<span className="capitalize">
										{quota.balance.accounting_method}
									</span>
								</p>
							)
						}
					>
						{`${kwhUsed.toFixed(1)}% ${t("components.providerModals.used")}. ${formatKwh(quota.subscription.kwh_remaining)} kWh ${t("components.providerModals.remaining")}${
							quota.subscription.current_period_end
								? ` · ${t("components.providerModals.resets")} ${formatDate(quota.subscription.current_period_end)}`
								: ""
						}`}
					</QuotaBar>
				)}

				{/* In overage the provider freezes kwh_used at the included amount
				    and bills further usage against the credit balance, so the bars
				    above stop moving; say where the spend actually goes. */}
				{quota.subscription.in_overage && (
					<p
						data-testid="neuralwatt-overage-note"
						className="text-xs text-red-400"
					>
						{t("components.providerModals.neuralwattOverageNote")}
					</p>
				)}

				{/* ── Subscription details ── */}
				<div>
					<DetailSectionHeader icon={RefreshCw}>
						{t("components.providerModals.neuralwattSubscription")}
					</DetailSectionHeader>
					<div className="grid grid-cols-2 gap-2">
						<DetailItem label={t("components.providerModals.neuralwattPlan")}>
							<div className="text-sm text-(--text-primary) capitalize">
								{quota.subscription.plan}
							</div>
						</DetailItem>
						<DetailItem
							label={t("components.providerModals.neuralwattBillingInterval")}
						>
							<div className="text-sm text-(--text-primary) capitalize">
								{quota.subscription.billing_interval}
							</div>
						</DetailItem>
						<DetailItem
							label={t("components.providerModals.neuralwattBillingPeriod")}
							value={`${formatDate(quota.subscription.current_period_start)} - ${formatDate(quota.subscription.current_period_end)}`}
							className="col-span-2"
						/>
						<DetailItem
							label={t("components.providerModals.neuralwattAutoRenew")}
							value={
								quota.subscription.auto_renew
									? t("components.providerModals.yes")
									: t("components.providerModals.no")
							}
						/>
						<DetailItem
							label={t("components.providerModals.neuralwattInOverage")}
							value={
								quota.subscription.in_overage
									? t("components.providerModals.yes")
									: t("components.providerModals.no")
							}
						/>
					</div>
				</div>

				{/* ── Usage stats ── */}
				<div>
					<DetailSectionHeader icon={Activity}>
						{t("components.providerModals.neuralwattUsage")}
					</DetailSectionHeader>
					<div className="grid grid-cols-5 gap-2 text-xs p-3 ui-detail-section">
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
							{formatWithCommas(quota.usage.current_month.requests)}
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
							{formatWithCommas(quota.usage.lifetime.requests)}
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
					<DetailSectionHeader icon={Gauge}>
						{t("components.providerModals.limits")}
					</DetailSectionHeader>
					<div className="grid grid-cols-3 gap-2">
						<DetailItem
							label={t("components.providerModals.neuralwattOverageLimit")}
							value={
								quota.limits.overage_limit_usd !== null
									? formatDollars(quota.limits.overage_limit_usd)
									: t("components.providerModals.none")
							}
						/>
						<DetailItem
							label={t("components.providerModals.neuralwattRateLimitTier")}
						>
							<div className="text-sm text-(--text-primary) capitalize">
								{quota.limits.rate_limit_tier}
							</div>
						</DetailItem>
						<DetailItem
							label={t("components.providerModals.neuralwattAllowance")}
							value={
								quota.key.allowance !== null
									? formatDollars(quota.key.allowance)
									: t("common.unlimited")
							}
						/>
					</div>
				</div>

				<LastRefreshedRow at={lastRefreshed} />
			</div>
		</Modal>
	);
}
