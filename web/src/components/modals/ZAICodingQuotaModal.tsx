import { useTranslation } from "react-i18next";
import type { ZAICodingQuotaResponse } from "../../api/types";
import { useLocalStorage } from "../../hooks/useLocalStorage";
import {
	formatRelativeTime,
	formatTimestamp,
	formatTimeUntil,
} from "../../utils/format";
import { Modal } from "../Modal";
import { QuotaBar, QuotaModalHeaderActions } from "./shared";

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
				{fiveHourLimit && (
					<QuotaBar
						label={t("components.providerModals.hTokenQuota", { hours: 5 })}
						rightText={
							barMode === "used"
								? `${fiveHourLimit.percentage.toFixed(0)}% ${t("components.providerModals.used")}`
								: `${(100 - fiveHourLimit.percentage).toFixed(0)}% ${t("components.providerModals.left")}`
						}
						percentage={fiveHourLimit.percentage}
						barMode={barMode}
					>
						{fiveHourLimit.percentage.toFixed(0)}%{" "}
						{t("components.providerModals.used")}.{" "}
						{t("components.providerModals.resets")}{" "}
						{fiveHourLimit.nextResetTime
							? `${formatTimestamp(fiveHourLimit.nextResetTime)} - ${formatTimeUntil(fiveHourLimit.nextResetTime)}`
							: "N/A"}
					</QuotaBar>
				)}

				{weeklyLimit && (
					<QuotaBar
						label={t("components.providerModals.weeklyTokenQuota")}
						rightText={
							barMode === "used"
								? `${weeklyLimit.percentage.toFixed(0)}% ${t("components.providerModals.used")}`
								: `${(100 - weeklyLimit.percentage).toFixed(0)}% ${t("components.providerModals.left")}`
						}
						percentage={weeklyLimit.percentage}
						barMode={barMode}
					>
						{weeklyLimit.percentage.toFixed(0)}%{" "}
						{t("components.providerModals.used")}.{" "}
						{t("components.providerModals.resets")}{" "}
						{weeklyLimit.nextResetTime
							? `${formatTimestamp(weeklyLimit.nextResetTime)} - ${formatTimeUntil(weeklyLimit.nextResetTime)}`
							: "N/A"}
					</QuotaBar>
				)}

				{mcpLimit && (
					<QuotaBar
						label={t("components.providerModals.mcpTokenQuota")}
						rightText={
							barMode === "used"
								? `${mcpLimit.percentage.toFixed(0)}% ${t("components.providerModals.used")}`
								: `${(100 - mcpLimit.percentage).toFixed(0)}% ${t("components.providerModals.left")}`
						}
						percentage={mcpLimit.percentage}
						barMode={barMode}
					>
						{mcpLimit.percentage.toFixed(0)}%{" "}
						{t("components.providerModals.used")}.{" "}
						{t("components.providerModals.resets")}{" "}
						{mcpLimit.nextResetTime
							? `${formatTimestamp(mcpLimit.nextResetTime)} - ${formatTimeUntil(mcpLimit.nextResetTime)}`
							: "N/A"}
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
					</QuotaBar>
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
