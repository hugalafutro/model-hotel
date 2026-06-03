import { ArrowLeftRight, RefreshCw } from "lucide-react";
import { useTranslation } from "react-i18next";
import type { ZAICodingQuotaResponse } from "../../api/types";
import { useTheme } from "../../context/ThemeContext";
import { useLocalStorage } from "../../hooks/useLocalStorage";
import {
	formatRelativeTime,
	formatTimestamp,
	formatTimeUntil,
} from "../../utils/format";
import { Modal } from "../Modal";
import { Spinner } from "../Spinner";
import { remainingBarColor, usedBarColor } from "./shared";

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
										<span>
											{detail.usage} {t("components.providerModals.used")}
										</span>
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
