import { useTranslation } from "react-i18next";
import type { NanoGPTUsage } from "../../api/types";
import { useLocalStorage } from "../../hooks/useLocalStorage";
import {
	formatDate,
	formatRelativeTime,
	formatTimestamp,
	formatTimeUntil,
	formatTokens,
} from "../../utils/format";
import { Modal } from "../Modal";
import {
	QuotaBar,
	QuotaModalHeaderActions,
	remainingBarColor,
	usedBarColor,
} from "./shared";

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
	onToast: (msg: string, type: "success" | "info" | "error") => void;
	lastRefreshed?: number;
}) {
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
						<p className="text-sm text-(--text-tertiary) mt-1">
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
				<QuotaBar
					label={t("components.providerModals.weeklyTokenQuota")}
					rightText={`${formatTokens(weeklyUsed)} / ${formatTokens(weeklyLimit)}`}
					percentage={100 - weeklyRemaining}
					barMode={barMode}
					dataTestId="weekly-progress-bar"
					fillTestId="weekly-progress-fill"
				>
					{weeklyLimit > 0
						? `${(100 - weeklyRemaining).toFixed(1)}% ${t("components.providerModals.used")}`
						: t("components.providerModals.noLimitSet")}
					{usage.weeklyInputTokens?.resetAt
						? `. ${t("components.providerModals.resets")} ${formatTimestamp(usage.weeklyInputTokens.resetAt)} - ${formatTimeUntil(usage.weeklyInputTokens.resetAt)}`
						: ""}
				</QuotaBar>

				{usage.dailyImages && (
					<div>
						<div className="flex justify-between items-center mb-2">
							<span className="text-sm font-medium text-(--text-secondary)">
								{t("components.providerModals.dailyImages")}
							</span>
							<span className="text-sm text-(--text-tertiary)">
								{usage.dailyImages.used} / {usage.limits.dailyImages ?? "∞"}
							</span>
						</div>
						<div className="w-full bg-(--surface-input) rounded-full h-3">
							<div
								className={`${barMode === "used" ? usedBarColor(usage.dailyImages.percentUsed * 100) : remainingBarColor(100 - usage.dailyImages.percentUsed * 100)} h-3 rounded-full transition-all`}
								style={{
									width: `${barMode === "used" ? Math.min(usage.dailyImages.percentUsed * 100, 100) : Math.min(100 - usage.dailyImages.percentUsed * 100, 100)}%`,
								}}
							/>
						</div>
						<p className="text-xs text-(--text-muted) mt-1">
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
							<span className="text-sm font-medium text-(--text-secondary)">
								{t("components.providerModals.dailyInputTokens")}
							</span>
							<span className="text-sm text-(--text-tertiary)">
								{formatTokens(usage.dailyInputTokens.used)} /{" "}
								{usage.limits.dailyInputTokens
									? formatTokens(usage.limits.dailyInputTokens)
									: "∞"}
							</span>
						</div>
						<div className="w-full bg-(--surface-input) rounded-full h-3">
							<div
								className={`${barMode === "used" ? usedBarColor(usage.dailyInputTokens.percentUsed * 100) : remainingBarColor(100 - usage.dailyInputTokens.percentUsed * 100)} h-3 rounded-full transition-all`}
								style={{
									width: `${barMode === "used" ? Math.min(usage.dailyInputTokens.percentUsed * 100, 100) : Math.min(100 - usage.dailyInputTokens.percentUsed * 100, 100)}%`,
								}}
							/>
						</div>
						<p className="text-xs text-(--text-muted) mt-1">
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
					<h3 className="text-sm font-medium text-(--text-secondary) mb-3">
						{t("components.providerModals.subscriptionDetails")}
					</h3>
					<div className="grid grid-cols-2 gap-3 text-sm">
						<div>
							<span className="text-(--text-muted)">
								{t("components.providerModals.provider")}
							</span>
							<p className="text-gray-200 capitalize">{usage.provider}</p>
						</div>
						<div>
							<span className="text-(--text-muted)">
								{t("components.providerModals.status")}
							</span>
							<p className="text-gray-200 capitalize">{usage.providerStatus}</p>
						</div>
						<div>
							<span className="text-(--text-muted)">
								{t("components.providerModals.periodEnd")}
							</span>
							<p className="text-gray-200">
								{formatDate(usage.period.currentPeriodEnd)}
							</p>
						</div>
						<div>
							<span className="text-(--text-muted)">
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
