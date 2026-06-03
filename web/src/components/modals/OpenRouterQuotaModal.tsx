import { ArrowLeftRight, RefreshCw } from "lucide-react";
import { useTranslation } from "react-i18next";
import type { OpenRouterBalance } from "../../api/types";
import { useTheme } from "../../context/ThemeContext";
import { useLocalStorage } from "../../hooks/useLocalStorage";
import {
	formatDollars,
	formatRelativeTime,
	formatTimestamp,
	formatTimeUntil,
} from "../../utils/format";
import { Modal } from "../Modal";
import { Spinner } from "../Spinner";
import { remainingBarColor, usedBarColor } from "./shared";

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
	onToast: (msg: string, type: "success" | "error" | "info") => void;
	lastRefreshed?: number;
}) {
	const { uiStyle } = useTheme();
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
		balance.credits_total > 0
			? (balance.credits_remaining / balance.credits_total) * 100
			: 100;

	return (
		<Modal
			header={
				<div className="flex justify-between items-start mb-6">
					<div>
						<h2 className="text-xl font-bold text-(--text-primary)">
							{t("components.providerModals.openRouterCredits")}
						</h2>
						<p className="text-sm text-gray-400 mt-1">
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
					<div className="flex items-center gap-2">
						<button
							type="button"
							onClick={() =>
								setBarMode((prev) =>
									prev === "remaining" ? "used" : "remaining",
								)
							}
							className="absolute top-4 right-20 text-gray-400 hover:text-(--text-primary) transition-all cursor-pointer p-1.5"
							aria-label={t("providers.credits.toggleLabel")}
							title={
								barMode === "remaining"
									? t("providers.credits.showUsed")
									: t("providers.credits.showRemaining")
							}
						>
							<ArrowLeftRight size={18} />
						</button>
						<button
							type="button"
							onClick={handleRefresh}
							disabled={isRefreshing}
							className="absolute top-4 right-10 text-gray-400 hover:text-(--text-primary) transition-all cursor-pointer p-1.5 hover:drop-shadow-[var(--glow-accent-lg)]"
							aria-label={t("common.refresh")}
							title={t("components.providerModals.refreshBalanceInfo")}
						>
							{isRefreshing && uiStyle === "cyber-terminal" ? (
								<Spinner className="w-[18px] h-[18px] text-[18px] leading-[18px]" />
							) : (
								<RefreshCw
									size={18}
									className={isRefreshing ? "animate-spin" : ""}
								/>
							)}
						</button>
					</div>
				</div>
			}
			onClose={onClose}
			scrollable
		>
			<div className="space-y-6">
				<div>
					<div className="flex justify-between items-center mb-2">
						<span className="text-sm font-medium text-gray-300">
							{t("components.providerModals.accountBalance")}
						</span>
						<span className="text-sm text-(--text-primary) font-medium">
							{formatDollars(balance.credits_remaining)}
						</span>
					</div>
					{balance.credits_total > 0 && (
						<div className="w-full bg-gray-700 rounded-full h-3">
							<div
								className={`${barMode === "used" ? usedBarColor(100 - creditsRemaining) : remainingBarColor(creditsRemaining)} h-3 rounded-full transition-all`}
								style={{
									width: `${barMode === "used" ? Math.min(100 - creditsRemaining, 100) : Math.min(creditsRemaining, 100)}%`,
								}}
							/>
						</div>
					)}
					<p className="text-xs text-gray-500 mt-1">
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
							<span className="text-sm font-medium text-gray-300">
								{t("components.providerModals.keySpendingLimit")}
							</span>
							<span className="text-sm text-gray-400">
								{formatDollars(balance.limit_remaining ?? 0)}{" "}
								{t("components.providerModals.remaining")}
							</span>
						</div>
						<div className="w-full bg-gray-700 rounded-full h-3">
							<div
								className={`${balance.limit > 0 ? (barMode === "used" ? usedBarColor(100 - ((balance.limit_remaining ?? 0) / balance.limit) * 100) : remainingBarColor(((balance.limit_remaining ?? 0) / balance.limit) * 100)) : "bg-amber-500"} h-3 rounded-full transition-all`}
								style={{
									width: `${
										balance.limit > 0
											? barMode === "used"
												? Math.min(
														100 -
															((balance.limit_remaining ?? 0) / balance.limit) *
																100,
														100,
													)
												: Math.min(
														((balance.limit_remaining ?? 0) / balance.limit) *
															100,
														100,
													)
											: 0
									}%`,
								}}
							/>
						</div>
						<p className="text-xs text-gray-500 mt-1">
							{balance.limit > 0
								? `${barMode === "used" ? (100 - ((balance.limit_remaining ?? 0) / balance.limit) * 100).toFixed(1) : (((balance.limit_remaining ?? 0) / balance.limit) * 100).toFixed(1)}% ${barMode === "used" ? t("components.providerModals.used") : t("components.providerModals.remaining")}`
								: balance.limit === 0
									? `$0 ${t("components.providerModals.limitReset")}`
									: t("components.providerModals.noLimitSet")}
							{balance.limit_reset
								? ` · ${t("components.providerModals.resets")} ${formatTimestamp(balance.limit_reset)} - ${formatTimeUntil(new Date(balance.limit_reset).getTime())}`
								: ""}
						</p>
					</div>
				)}

				<div>
					<h3 className="text-sm font-medium text-gray-300 mb-3">
						{t("components.providerModals.keyUsage")}
					</h3>
					<p className="text-xs text-gray-500 mb-3">
						{t("components.providerModals.spendingByThisKey")}
					</p>
					<div className="grid grid-cols-2 gap-3 text-sm">
						<div>
							<span className="text-gray-500">{t("common.today")}</span>
							<p className="text-gray-200">
								{formatDollars(balance.usage_daily)}
							</p>
						</div>
						<div>
							<span className="text-gray-500">{t("common.thisWeek")}</span>
							<p className="text-gray-200">
								{formatDollars(balance.usage_weekly)}
							</p>
						</div>
						<div>
							<span className="text-gray-500">{t("common.thisMonth")}</span>
							<p className="text-gray-200">
								{formatDollars(balance.usage_monthly)}
							</p>
						</div>
						<div>
							<span className="text-gray-500">{t("common.allTime")}</span>
							<p className="text-gray-200">{formatDollars(balance.usage)}</p>
						</div>
					</div>
				</div>

				{lastRefreshed ? (
					<div className="flex justify-between items-center text-xs text-gray-500 pt-2 ">
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
