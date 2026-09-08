import { useTranslation } from "react-i18next";
import { DollarSign } from "@/lib/icons";
import type { OpenRouterBalance } from "../../api/types";
import {
	formatDollars,
	formatTimestamp,
	formatTimeUntil,
} from "../../utils/format";
import { DetailSectionHeader } from "../DetailSectionHeader";
import { DetailItem } from "../LogDetailItem";
import { Modal } from "../Modal";
import {
	LastRefreshedRow,
	type OnToast,
	QuotaModalHeaderActions,
	remainingBarColor,
	usedBarColor,
	useQuotaBarMode,
	useQuotaRefreshToast,
} from "./shared";

export function OpenRouterQuotaModal({
	balance,
	onClose,
	onRefresh,
	isRefreshing,
	onToast,
	lastRefreshed,
}: {
	balance: OpenRouterBalance;
	onClose: () => void;
	onRefresh: () => Promise<unknown>;
	isRefreshing: boolean;
	onToast: OnToast;
	lastRefreshed?: number;
}) {
	const { t } = useTranslation();
	const [barMode, toggleBarMode] = useQuotaBarMode();

	const handleRefresh = useQuotaRefreshToast(onRefresh, onToast);

	const creditsRemaining =
		balance.credits_total > 0
			? (balance.credits_remaining / balance.credits_total) * 100
			: 100;

	// Share of the key's spending limit still available, 0 when there is no
	// limit to divide by.
	const limitPct =
		balance.limit && balance.limit > 0
			? ((balance.limit_remaining ?? 0) / balance.limit) * 100
			: 0;

	return (
		<Modal
			header={
				<div className="flex justify-between items-start mb-6">
					<div>
						<h2 className="text-xl font-bold text-(--text-primary)">
							{t("components.providerModals.openRouterCredits")}
						</h2>
						<p className="text-sm text-(--text-tertiary) mt-1">
							{balance.is_free_tier ? (
								<span className="inline-flex items-center gap-1.5">
									<span className="w-2 h-2 rounded-full bg-yellow-400"></span>
									{t("components.providerModals.freeTier")}
								</span>
							) : (
								<span className="inline-flex items-center gap-1.5">
									<span className="w-2 h-2 rounded-full bg-green-400"></span>
									{t("components.providerModals.paidAccount")}
								</span>
							)}
						</p>
					</div>
					<QuotaModalHeaderActions
						barMode={barMode}
						onToggleBarMode={toggleBarMode}
						onRefresh={handleRefresh}
						isRefreshing={isRefreshing}
						toggleAriaLabel={t("providers.credits.toggleLabel")}
						toggleTitle={
							barMode === "remaining"
								? t("providers.credits.showUsed")
								: t("providers.credits.showRemaining")
						}
						refreshTitle={t("components.providerModals.refreshBalanceInfo")}
					/>
				</div>
			}
			onClose={onClose}
			scrollable
		>
			<div className="space-y-6">
				<div>
					<div className="flex justify-between items-center mb-2">
						<span className="text-sm font-medium text-(--text-secondary)">
							{t("components.providerModals.accountBalance")}
						</span>
						<span className="text-sm text-(--text-primary) font-medium">
							{formatDollars(balance.credits_remaining)}
						</span>
					</div>
					{balance.credits_total > 0 && (
						<div className="w-full bg-(--surface-input) ui-bar h-3">
							<div
								className={`${barMode === "used" ? usedBarColor(100 - creditsRemaining) : remainingBarColor(creditsRemaining)} h-3 ui-bar transition-all`}
								style={{
									width: `${barMode === "used" ? Math.min(100 - creditsRemaining, 100) : Math.min(creditsRemaining, 100)}%`,
								}}
							/>
						</div>
					)}
					<p className="text-xs text-(--text-muted) mt-1">
						{balance.credits_total > 0
							? t("components.providerModals.spentTotal", {
									amount: formatDollars(balance.credits_used),
								})
							: t("components.providerModals.noCredits")}
					</p>
				</div>

				{balance.limit !== null && (
					<div>
						<div className="flex justify-between items-center mb-2">
							<span className="text-sm font-medium text-(--text-secondary)">
								{t("components.providerModals.keySpendingLimit")}
							</span>
							<span className="text-sm text-(--text-tertiary)">
								{formatDollars(balance.limit_remaining ?? 0)}{" "}
								{t("components.providerModals.remaining")}
							</span>
						</div>
						<div className="w-full bg-(--surface-input) ui-bar h-3">
							<div
								className={`${balance.limit > 0 ? (barMode === "used" ? usedBarColor(100 - limitPct) : remainingBarColor(limitPct)) : "bg-amber-500"} h-3 ui-bar transition-all`}
								style={{
									width: `${
										balance.limit > 0
											? Math.min(
													barMode === "used" ? 100 - limitPct : limitPct,
													100,
												)
											: 0
									}%`,
								}}
							/>
						</div>
						<p className="text-xs text-(--text-muted) mt-1 whitespace-pre-line">
							{balance.limit > 0
								? `${(barMode === "used" ? 100 - limitPct : limitPct).toFixed(1)}% ${barMode === "used" ? t("components.providerModals.used") : t("components.providerModals.remaining")}`
								: balance.limit === 0
									? `$0 ${t("components.providerModals.limitReset")}`
									: t("components.providerModals.noLimitSet")}
							{balance.limit_reset
								? ` · ${t("components.providerModals.resets")} ${formatTimestamp(balance.limit_reset)}\n${formatTimeUntil(new Date(balance.limit_reset).getTime())}`
								: ""}
						</p>
					</div>
				)}

				<div>
					<DetailSectionHeader icon={DollarSign}>
						{t("components.providerModals.keyUsage")}
					</DetailSectionHeader>
					<p className="text-xs text-(--text-muted) mb-3">
						{t("components.providerModals.spendingByThisKey")}
					</p>
					<div className="grid grid-cols-2 gap-2">
						<DetailItem
							emphasis="stat"
							label={t("common.today")}
							value={formatDollars(balance.usage_daily)}
							mono
						/>
						<DetailItem
							emphasis="stat"
							label={t("common.thisWeek")}
							value={formatDollars(balance.usage_weekly)}
							mono
						/>
						<DetailItem
							emphasis="stat"
							label={t("common.thisMonth")}
							value={formatDollars(balance.usage_monthly)}
							mono
						/>
						<DetailItem
							emphasis="stat"
							label={t("common.allTime")}
							value={formatDollars(balance.usage)}
							mono
						/>
					</div>
				</div>

				<LastRefreshedRow at={lastRefreshed} />
			</div>
		</Modal>
	);
}
