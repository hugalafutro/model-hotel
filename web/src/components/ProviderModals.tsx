import { ArrowLeftRight, RefreshCw } from "lucide-react";
import { useTranslation } from "react-i18next";
import type {
	NanoGPTUsage,
	OpenRouterBalance,
	ZAICodingQuotaResponse,
} from "../api/types";
import { useTheme } from "../context/ThemeContext";
import { useLocalStorage } from "../hooks/useLocalStorage";
import {
	formatDate,
	formatRelativeTime,
	formatTimestamp,
	formatTimeUntil,
	formatTokens,
} from "../utils/format";
import { Modal } from "./Modal";
import { Spinner } from "./Spinner";

/** Returns a Tailwind bg-[color] class based on remaining percentage. */
function remainingBarColor(remainingPct: number): string {
	if (remainingPct < 20) return "bg-red-500";
	if (remainingPct < 60) return "bg-amber-500";
	return "bg-[#6366F1]";
}

/** Returns a Tailwind bg-[color] class based on used percentage. */
function usedBarColor(usedPct: number): string {
	if (usedPct < 50) return "bg-amber-500";
	if (usedPct < 80) return "bg-orange-500";
	return "bg-red-500";
}

export function NanoGPTQuotaModal({
	usage,
	onClose,
	onRefresh,
	isRefreshing,
	onToast,
	lastRefreshed,
}: {
	usage: NanoGPTUsage;
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
	const weeklyLimit = usage.limits.weeklyInputTokens ?? 0;
	const weeklyUsed = usage.weeklyInputTokens?.used ?? 0;
	const weeklyRemaining =
		weeklyLimit > 0 ? ((weeklyLimit - weeklyUsed) / weeklyLimit) * 100 : 100;

	const handleRefresh = async () => {
		try {
			await onRefresh();
			onToast(t("components.providerModals.quotaRefreshed"), "success");
		} catch {
			onToast(t("components.providerModals.failedToRefreshQuota"), "error");
		}
	};

	return (
		<Modal
			header={
				<div className="flex justify-between items-start mb-6">
					<div>
						<h2 className="text-xl font-bold text-(--text-primary)">
							{t("components.providerModals.nanoGPTSubscription")}
						</h2>
						<p className="text-sm text-gray-400 mt-1">
							{usage.active ? (
								<span className="inline-flex items-center gap-1.5">
									<span
										data-testid="status-dot-active"
										className="w-2 h-2 rounded-full bg-green-400"
									></span>
									{t("components.providerModals.active")}
								</span>
							) : (
								<span className="inline-flex items-center gap-1.5">
									<span
										data-testid="status-dot-inactive"
										className="w-2 h-2 rounded-full bg-red-400"
									></span>
									{t("components.providerModals.inactive")}
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
							aria-label={t("components.providerModals.toggleRemainingUsed")}
							title={
								barMode === "remaining"
									? t("components.providerModals.showQuotaUsed")
									: t("components.providerModals.showQuotaRemaining")
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
							title={t("components.providerModals.refreshQuotaInfo")}
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
							{t("components.providerModals.weeklyTokenQuota")}
						</span>
						<span className="text-sm text-gray-400">
							{formatTokens(weeklyUsed)} / {formatTokens(weeklyLimit)}
						</span>
					</div>
					<div
						data-testid="weekly-progress-bar"
						className="w-full bg-gray-700 rounded-full h-3"
					>
						<div
							data-testid="weekly-progress-fill"
							className={`${barMode === "used" ? usedBarColor(100 - weeklyRemaining) : remainingBarColor(weeklyRemaining)} h-3 rounded-full transition-all`}
							style={{
								width: `${barMode === "used" ? Math.min(100 - weeklyRemaining, 100) : Math.min(weeklyRemaining, 100)}%`,
							}}
						/>
					</div>
					<p className="text-xs text-gray-500 mt-1">
						{weeklyLimit > 0
							? `${(100 - weeklyRemaining).toFixed(1)}% ${t("components.providerModals.used")}`
							: t("components.providerModals.noLimitSet")}
						{usage.weeklyInputTokens?.resetAt
							? `. ${t("components.providerModals.resets")} ${formatTimestamp(usage.weeklyInputTokens.resetAt)} - ${formatTimeUntil(usage.weeklyInputTokens.resetAt)}`
							: ""}
					</p>
				</div>

				{usage.dailyImages && (
					<div>
						<div className="flex justify-between items-center mb-2">
							<span className="text-sm font-medium text-gray-300">
								{t("components.providerModals.dailyImages")}
							</span>
							<span className="text-sm text-gray-400">
								{usage.dailyImages.used} / {usage.limits.dailyImages ?? "∞"}
							</span>
						</div>
						<div className="w-full bg-gray-700 rounded-full h-3">
							<div
								className={`${barMode === "used" ? usedBarColor(usage.dailyImages.percentUsed * 100) : remainingBarColor(100 - usage.dailyImages.percentUsed * 100)} h-3 rounded-full transition-all`}
								style={{
									width: `${barMode === "used" ? Math.min(usage.dailyImages.percentUsed * 100, 100) : Math.min(100 - usage.dailyImages.percentUsed * 100, 100)}%`,
								}}
							/>
						</div>
						<p className="text-xs text-gray-500 mt-1">
							{usage.dailyImages.percentUsed.toFixed(1)}%{" "}
							{t("components.providerModals.used")}.{" "}
							{t("components.providerModals.resets")}{" "}
							{usage.dailyImages.resetAt
								? `${formatTimestamp(usage.dailyImages.resetAt)} - ${formatTimeUntil(usage.dailyImages.resetAt)}`
								: "N/A"}
						</p>
					</div>
				)}

				{usage.dailyInputTokens && (
					<div>
						<div className="flex justify-between items-center mb-2">
							<span className="text-sm font-medium text-gray-300">
								{t("components.providerModals.dailyInputTokens")}
							</span>
							<span className="text-sm text-gray-400">
								{formatTokens(usage.dailyInputTokens.used)} /{" "}
								{usage.limits.dailyInputTokens
									? formatTokens(usage.limits.dailyInputTokens)
									: "∞"}
							</span>
						</div>
						<div className="w-full bg-gray-700 rounded-full h-3">
							<div
								className={`${barMode === "used" ? usedBarColor(usage.dailyInputTokens.percentUsed * 100) : remainingBarColor(100 - usage.dailyInputTokens.percentUsed * 100)} h-3 rounded-full transition-all`}
								style={{
									width: `${barMode === "used" ? Math.min(usage.dailyInputTokens.percentUsed * 100, 100) : Math.min(100 - usage.dailyInputTokens.percentUsed * 100, 100)}%`,
								}}
							/>
						</div>
						<p className="text-xs text-gray-500 mt-1">
							{usage.dailyInputTokens.percentUsed.toFixed(1)}%{" "}
							{t("components.providerModals.used")}.{" "}
							{t("components.providerModals.resets")}{" "}
							{usage.dailyInputTokens.resetAt
								? `${formatTimestamp(usage.dailyInputTokens.resetAt)} - ${formatTimeUntil(usage.dailyInputTokens.resetAt)}`
								: "N/A"}
						</p>
					</div>
				)}

				<div>
					<h3 className="text-sm font-medium text-gray-300 mb-3">
						{t("components.providerModals.subscriptionDetails")}
					</h3>
					<div className="grid grid-cols-2 gap-3 text-sm">
						<div>
							<span className="text-gray-500">
								{t("components.providerModals.provider")}
							</span>
							<p className="text-gray-200 capitalize">{usage.provider}</p>
						</div>
						<div>
							<span className="text-gray-500">
								{t("components.providerModals.status")}
							</span>
							<p className="text-gray-200 capitalize">{usage.providerStatus}</p>
						</div>
						<div>
							<span className="text-gray-500">
								{t("components.providerModals.periodEnd")}
							</span>
							<p className="text-gray-200">
								{formatDate(usage.period.currentPeriodEnd)}
							</p>
						</div>
						<div>
							<span className="text-gray-500">
								{t("components.providerModals.allowOverage")}
							</span>
							<p className="text-gray-200">
								{usage.allowOverage
									? t("components.providerModals.yes")
									: t("components.providerModals.no")}
							</p>
						</div>
					</div>
				</div>

				{usage.cancelAtPeriodEnd && (
					<div className="p-3 bg-yellow-900/30 border border-yellow-700/50 rounded-lg">
						<p className="text-sm text-yellow-300">
							Subscription will cancel at period end (
							{formatDate(usage.period.currentPeriodEnd)})
						</p>
					</div>
				)}

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

export function ZAICodingQuotaModal({
	usage,
	onClose,
	onRefresh,
	isRefreshing,
	onToast,
	lastRefreshed,
}: {
	usage: ZAICodingQuotaResponse;
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
	const limits = usage.data?.limits || [];

	const fiveHourLimit = limits.find(
		(l) => l.type === "TOKENS_LIMIT" && l.unit === 3,
	);
	const weeklyLimit = limits.find(
		(l) => l.type === "TOKENS_LIMIT" && l.unit === 6,
	);
	const mcpLimit = limits.find((l) => l.type === "TIME_LIMIT" && l.unit === 5);

	const handleRefresh = async () => {
		try {
			await onRefresh();
			onToast(t("components.providerModals.quotaRefreshed"), "success");
		} catch {
			onToast(t("components.providerModals.failedToRefreshQuota"), "error");
		}
	};

	return (
		<Modal
			header={
				<div className="flex justify-between items-start mb-6">
					<div>
						<h2 className="text-xl font-bold text-(--text-primary)">
							{t("components.providerModals.zAICodingPlanQuota")}
						</h2>
						<p className="text-sm text-gray-400 mt-1">
							{t("components.providerModals.plan")}{" "}
							<span className="text-gray-200 capitalize">
								{usage.data?.level ?? "-"}
							</span>
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
							aria-label={t("components.providerModals.toggleRemainingUsed")}
							title={
								barMode === "remaining"
									? t("components.providerModals.showQuotaUsed")
									: t("components.providerModals.showQuotaRemaining")
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
							title={t("components.providerModals.refreshQuotaInfo")}
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
				{fiveHourLimit && (
					<div>
						<div className="flex justify-between items-center mb-2">
							<span className="text-sm font-medium text-gray-300">
								{t("components.providerModals.hTokenQuota", { hours: 5 })}
							</span>
							<span className="text-sm text-gray-400">
								{barMode === "used"
									? `${fiveHourLimit.percentage.toFixed(0)}% ${t("components.providerModals.used")}`
									: `${(100 - fiveHourLimit.percentage).toFixed(0)}% ${t("components.providerModals.left")}`}
							</span>
						</div>
						<div className="w-full bg-gray-700 rounded-full h-3">
							<div
								className={`${barMode === "used" ? usedBarColor(fiveHourLimit.percentage) : remainingBarColor(100 - fiveHourLimit.percentage)} h-3 rounded-full transition-all`}
								style={{
									width: `${barMode === "used" ? Math.min(fiveHourLimit.percentage, 100) : Math.min(100 - fiveHourLimit.percentage, 100)}%`,
								}}
							/>
						</div>
						<p className="text-xs text-gray-500 mt-1">
							{fiveHourLimit.percentage.toFixed(0)}%{" "}
							{t("components.providerModals.used")}.{" "}
							{t("components.providerModals.resets")}{" "}
							{fiveHourLimit.nextResetTime
								? `${formatTimestamp(fiveHourLimit.nextResetTime)} - ${formatTimeUntil(fiveHourLimit.nextResetTime)}`
								: "N/A"}
						</p>
					</div>
				)}

				{weeklyLimit && (
					<div>
						<div className="flex justify-between items-center mb-2">
							<span className="text-sm font-medium text-gray-300">
								{t("components.providerModals.weeklyTokenQuota")}
							</span>
							<span className="text-sm text-gray-400">
								{barMode === "used"
									? `${weeklyLimit.percentage.toFixed(0)}% ${t("components.providerModals.used")}`
									: `${(100 - weeklyLimit.percentage).toFixed(0)}% ${t("components.providerModals.left")}`}
							</span>
						</div>
						<div className="w-full bg-gray-700 rounded-full h-3">
							<div
								className={`${barMode === "used" ? usedBarColor(weeklyLimit.percentage) : remainingBarColor(100 - weeklyLimit.percentage)} h-3 rounded-full transition-all`}
								style={{
									width: `${barMode === "used" ? Math.min(weeklyLimit.percentage, 100) : Math.min(100 - weeklyLimit.percentage, 100)}%`,
								}}
							/>
						</div>
						<p className="text-xs text-gray-500 mt-1">
							{weeklyLimit.percentage.toFixed(0)}%{" "}
							{t("components.providerModals.used")}.{" "}
							{t("components.providerModals.resets")}{" "}
							{weeklyLimit.nextResetTime
								? `${formatTimestamp(weeklyLimit.nextResetTime)} - ${formatTimeUntil(weeklyLimit.nextResetTime)}`
								: "N/A"}
						</p>
					</div>
				)}

				{mcpLimit && (
					<div>
						<div className="flex justify-between items-center mb-2">
							<span className="text-sm font-medium text-gray-300">
								{t("components.providerModals.mcpTokenQuota")}
							</span>
							<span className="text-sm text-gray-400">
								{barMode === "used"
									? `${mcpLimit.percentage.toFixed(0)}% ${t("components.providerModals.used")}`
									: `${(100 - mcpLimit.percentage).toFixed(0)}% ${t("components.providerModals.left")}`}
							</span>
						</div>
						<div className="w-full bg-gray-700 rounded-full h-3">
							<div
								className={`${barMode === "used" ? usedBarColor(mcpLimit.percentage) : remainingBarColor(100 - mcpLimit.percentage)} h-3 rounded-full transition-all`}
								style={{
									width: `${barMode === "used" ? Math.min(mcpLimit.percentage, 100) : Math.min(100 - mcpLimit.percentage, 100)}%`,
								}}
							/>
						</div>
						<p className="text-xs text-gray-500 mt-1">
							{mcpLimit.percentage.toFixed(0)}%{" "}
							{t("components.providerModals.used")}.{" "}
							{t("components.providerModals.resets")}{" "}
							{mcpLimit.nextResetTime
								? `${formatTimestamp(mcpLimit.nextResetTime)} - ${formatTimeUntil(mcpLimit.nextResetTime)}`
								: "N/A"}
						</p>
						{mcpLimit.usageDetails && mcpLimit.usageDetails.length > 0 && (
							<div className="mt-2 space-y-1">
								{mcpLimit.usageDetails.map((detail) => (
									<div
										key={detail.modelCode}
										className="flex justify-between text-xs text-gray-500"
									>
										<span className="capitalize">{detail.modelCode}</span>
										<span>{detail.usage} used</span>
									</div>
								))}
							</div>
						)}
					</div>
				)}

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

	const formatDollars = (v: number) =>
		v.toLocaleString("en-US", {
			style: "currency",
			currency: "USD",
		});

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
							? `${formatDollars(balance.credits_used)} ${t("components.providerModals.spentTotal", { amount: formatDollars(balance.credits_used) })}`
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
								{formatDollars(balance.limit_remaining ?? 0)} remaining
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
