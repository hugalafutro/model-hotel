import { ArrowLeftRight, RefreshCw } from "lucide-react";
import { useState } from "react";
import type {
	NanoGPTUsage,
	OpenRouterBalance,
	ZAICodingQuotaResponse,
} from "../api/types";
import { useTheme } from "../context/ThemeContext";
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
	const [barMode, setBarMode] = useState<"remaining" | "used">("remaining");
	const weeklyLimit = usage.limits.weeklyInputTokens ?? 0;
	const weeklyUsed = usage.weeklyInputTokens?.used ?? 0;
	const weeklyRemaining =
		weeklyLimit > 0 ? ((weeklyLimit - weeklyUsed) / weeklyLimit) * 100 : 100;

	const handleRefresh = async () => {
		try {
			await onRefresh();
			onToast("Quota refreshed", "success");
		} catch {
			onToast("Failed to refresh quota", "error");
		}
	};

	return (
		<Modal
			header={
				<div className="flex justify-between items-start mb-6">
					<div>
						<h2 className="text-xl font-bold text-white">
							NanoGPT Subscription
						</h2>
						<p className="text-sm text-gray-400 mt-1">
							{usage.active ? (
								<span className="inline-flex items-center gap-1.5">
									<span
										data-testid="status-dot-active"
										className="w-2 h-2 rounded-full bg-green-400"
									></span>
									Active
								</span>
							) : (
								<span className="inline-flex items-center gap-1.5">
									<span
										data-testid="status-dot-inactive"
										className="w-2 h-2 rounded-full bg-red-400"
									></span>
									Inactive
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
							className="absolute top-4 right-20 text-gray-400 hover:text-white transition-all cursor-pointer p-1.5"
							aria-label="Toggle between remaining and used"
							title={
								barMode === "remaining"
									? "Show quota used"
									: "Show quota remaining"
							}
						>
							<ArrowLeftRight size={18} />
						</button>
						<button
							type="button"
							onClick={handleRefresh}
							disabled={isRefreshing}
							className="absolute top-4 right-10 text-gray-400 hover:text-white transition-all cursor-pointer p-1.5 hover:drop-shadow-[var(--glow-accent-lg)]"
							aria-label="Refresh"
							title="Refresh quota info"
						>
							{isRefreshing && uiStyle === "cyber-terminal" ? (
								<Spinner />
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
							Weekly Token Quota
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
							? `${(100 - weeklyRemaining).toFixed(1)}% used`
							: "No limit set"}
						{usage.weeklyInputTokens?.resetAt
							? `. Resets ${formatTimestamp(usage.weeklyInputTokens.resetAt)} - ${formatTimeUntil(usage.weeklyInputTokens.resetAt)}`
							: ""}
					</p>
				</div>

				{usage.dailyImages && (
					<div>
						<div className="flex justify-between items-center mb-2">
							<span className="text-sm font-medium text-gray-300">
								Daily Images
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
							{usage.dailyImages.percentUsed.toFixed(1)}% used. Resets{" "}
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
								Daily Input Tokens
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
							{usage.dailyInputTokens.percentUsed.toFixed(1)}% used. Resets{" "}
							{usage.dailyInputTokens.resetAt
								? `${formatTimestamp(usage.dailyInputTokens.resetAt)} - ${formatTimeUntil(usage.dailyInputTokens.resetAt)}`
								: "N/A"}
						</p>
					</div>
				)}

				<div>
					<h3 className="text-sm font-medium text-gray-300 mb-3">
						Subscription Details
					</h3>
					<div className="grid grid-cols-2 gap-3 text-sm">
						<div>
							<span className="text-gray-500">Provider</span>
							<p className="text-gray-200 capitalize">{usage.provider}</p>
						</div>
						<div>
							<span className="text-gray-500">Status</span>
							<p className="text-gray-200 capitalize">{usage.providerStatus}</p>
						</div>
						<div>
							<span className="text-gray-500">Period End</span>
							<p className="text-gray-200">
								{formatDate(usage.period.currentPeriodEnd)}
							</p>
						</div>
						<div>
							<span className="text-gray-500">Allow Overage</span>
							<p className="text-gray-200">
								{usage.allowOverage ? "Yes" : "No"}
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
						<span>Last refreshed</span>
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
	const [barMode, setBarMode] = useState<"remaining" | "used">("remaining");
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
			onToast("Quota refreshed", "success");
		} catch {
			onToast("Failed to refresh quota", "error");
		}
	};

	return (
		<Modal
			header={
				<div className="flex justify-between items-start mb-6">
					<div>
						<h2 className="text-xl font-bold text-white">
							Z.ai Coding Plan Quota
						</h2>
						<p className="text-sm text-gray-400 mt-1">
							Plan:{" "}
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
							className="absolute top-4 right-20 text-gray-400 hover:text-white transition-all cursor-pointer p-1.5"
							aria-label="Toggle between remaining and used"
							title={
								barMode === "remaining"
									? "Show quota used"
									: "Show quota remaining"
							}
						>
							<ArrowLeftRight size={18} />
						</button>
						<button
							type="button"
							onClick={handleRefresh}
							disabled={isRefreshing}
							className="absolute top-4 right-10 text-gray-400 hover:text-white transition-all cursor-pointer p-1.5 hover:drop-shadow-[var(--glow-accent-lg)]"
							aria-label="Refresh"
							title="Refresh quota info"
						>
							{isRefreshing && uiStyle === "cyber-terminal" ? (
								<Spinner />
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
								5h Token Quota
							</span>
							<span className="text-sm text-gray-400">
								{barMode === "used"
									? `${fiveHourLimit.percentage.toFixed(0)}% used`
									: `${(100 - fiveHourLimit.percentage).toFixed(0)}% left`}
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
							{fiveHourLimit.percentage.toFixed(0)}% used. Resets{" "}
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
								Weekly Token Quota
							</span>
							<span className="text-sm text-gray-400">
								{barMode === "used"
									? `${weeklyLimit.percentage.toFixed(0)}% used`
									: `${(100 - weeklyLimit.percentage).toFixed(0)}% left`}
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
							{weeklyLimit.percentage.toFixed(0)}% used. Resets{" "}
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
								MCP Time Quota
							</span>
							<span className="text-sm text-gray-400">
								{barMode === "used"
									? `${mcpLimit.percentage.toFixed(0)}% used`
									: `${(100 - mcpLimit.percentage).toFixed(0)}% left`}
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
							{mcpLimit.percentage.toFixed(0)}% used. Resets{" "}
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
						<span>Last refreshed</span>
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
	const [barMode, setBarMode] = useState<"remaining" | "used">("remaining");

	const handleRefresh = async () => {
		try {
			await onRefresh();
			onToast("Balance refreshed", "success");
		} catch {
			onToast("Failed to refresh balance", "error");
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
						<h2 className="text-xl font-bold text-white">OpenRouter Credits</h2>
						<p className="text-sm text-gray-400 mt-1">
							{balance.is_free_tier ? (
								<span className="inline-flex items-center gap-1.5">
									<span className="w-2 h-2 rounded-full bg-yellow-400"></span>
									Free Tier
								</span>
							) : (
								<span className="inline-flex items-center gap-1.5">
									<span className="w-2 h-2 rounded-full bg-green-400"></span>
									Paid Account
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
							className="absolute top-4 right-20 text-gray-400 hover:text-white transition-all cursor-pointer p-1.5"
							aria-label="Toggle between remaining and used"
							title={
								barMode === "remaining"
									? "Show credits used"
									: "Show credits remaining"
							}
						>
							<ArrowLeftRight size={18} />
						</button>
						<button
							type="button"
							onClick={handleRefresh}
							disabled={isRefreshing}
							className="absolute top-4 right-10 text-gray-400 hover:text-white transition-all cursor-pointer p-1.5 hover:drop-shadow-[var(--glow-accent-lg)]"
							aria-label="Refresh"
							title="Refresh balance info"
						>
							{isRefreshing && uiStyle === "cyber-terminal" ? (
								<Spinner />
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
							Account Balance
						</span>
						<span className="text-sm text-white font-medium">
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
							? `${formatDollars(balance.credits_used)} spent total`
							: "No credits"}
					</p>
				</div>

				{balance.limit !== null && (
					<div>
						<div className="flex justify-between items-center mb-2">
							<span className="text-sm font-medium text-gray-300">
								Key Spending Limit
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
								? `${barMode === "used" ? (100 - ((balance.limit_remaining ?? 0) / balance.limit) * 100).toFixed(1) : (((balance.limit_remaining ?? 0) / balance.limit) * 100).toFixed(1)}% ${barMode === "used" ? "used" : "remaining"}`
								: balance.limit === 0
									? "$0 limit - spending blocked"
									: "No limit set"}
							{balance.limit_reset
								? ` · Resets ${formatTimestamp(balance.limit_reset)} - ${formatTimeUntil(new Date(balance.limit_reset).getTime())}`
								: ""}
						</p>
					</div>
				)}

				<div>
					<h3 className="text-sm font-medium text-gray-300 mb-3">Key Usage</h3>
					<p className="text-xs text-gray-500 mb-3">
						Spending by this API key (account total may differ)
					</p>
					<div className="grid grid-cols-2 gap-3 text-sm">
						<div>
							<span className="text-gray-500">Today</span>
							<p className="text-gray-200">
								{formatDollars(balance.usage_daily)}
							</p>
						</div>
						<div>
							<span className="text-gray-500">This Week</span>
							<p className="text-gray-200">
								{formatDollars(balance.usage_weekly)}
							</p>
						</div>
						<div>
							<span className="text-gray-500">This Month</span>
							<p className="text-gray-200">
								{formatDollars(balance.usage_monthly)}
							</p>
						</div>
						<div>
							<span className="text-gray-500">All Time</span>
							<p className="text-gray-200">{formatDollars(balance.usage)}</p>
						</div>
					</div>
				</div>

				{lastRefreshed ? (
					<div className="flex justify-between items-center text-xs text-gray-500 pt-2 ">
						<span>Last refreshed</span>
						<span>
							{formatRelativeTime(new Date(lastRefreshed).toISOString())}
						</span>
					</div>
				) : null}
			</div>
		</Modal>
	);
}
