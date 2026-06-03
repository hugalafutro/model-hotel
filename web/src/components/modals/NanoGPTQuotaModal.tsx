import { ArrowLeftRight, RefreshCw } from "lucide-react";
import { useTranslation } from "react-i18next";
import type { NanoGPTUsage } from "../../api/types";
import { useTheme } from "../../context/ThemeContext";
import { useLocalStorage } from "../../hooks/useLocalStorage";
import {
	formatDate,
	formatRelativeTime,
	formatTimestamp,
	formatTimeUntil,
	formatTokens,
} from "../../utils/format";
import { Modal } from "../Modal";
import { Spinner } from "../Spinner";
import { remainingBarColor, usedBarColor } from "./shared";

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
							className="absolute top-4 right-20 text-gray-400 hover:text-(--text-primary) transition-all cursor-pointer p-1.5 hover:drop-shadow-[var(--glow-accent-lg)]"
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
							{t("components.providerModals.cancelAtPeriodEnd", {
								date: formatDate(usage.period.currentPeriodEnd),
							})}
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
