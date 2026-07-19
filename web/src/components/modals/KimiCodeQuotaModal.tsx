import { useTranslation } from "react-i18next";
import type { KimiCodeQuotaResponse } from "../../api/types";
import { useLocalStorage } from "../../hooks/useLocalStorage";
import {
	getKimiCodeFiveHourLimit,
	getKimiCodeWeeklyLimit,
} from "../../hooks/useQuotaData";
import {
	formatRelativeTime,
	formatTimestamp,
	formatTimeUntil,
} from "../../utils/format";
import { Modal } from "../Modal";
import { QuotaBar, QuotaModalHeaderActions } from "./shared";

/** Renders "resets <timestamp>\n<time-until>" for an ISO reset time. */
function resetLabel(resetTime: string, resetsWord: string): string {
	if (!resetTime) return "N/A";
	const ms = new Date(resetTime).getTime();
	if (!Number.isFinite(ms)) return "N/A";
	return `${resetsWord} ${formatTimestamp(resetTime)}\n${formatTimeUntil(ms)}`;
}

export function KimiCodeQuotaModal({
	usage,
	onClose,
	onRefresh,
	isRefreshing,
	onToast,
	lastRefreshed,
}: {
	usage: KimiCodeQuotaResponse;
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

	const fiveHour = getKimiCodeFiveHourLimit(usage);
	const weekly = getKimiCodeWeeklyLimit(usage);
	const level = usage.user?.membership?.level;
	const parallelLimit = usage.parallel?.limit;
	const totalQuota = usage.totalQuota;

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
							{t("components.providerModals.kimiCodePlanQuota")}
						</h2>
						<p className="text-sm text-(--text-tertiary) mt-1">
							{t("components.providerModals.plan")}{" "}
							<span
								className="text-gray-200 capitalize"
								data-testid="kimi-code-membership"
							>
								{level ?? "-"}
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
				{fiveHour && (
					<QuotaBar
						label={t("components.providerModals.hTokenQuota", { hours: 5 })}
						rightText={
							barMode === "used"
								? `${fiveHour.percentage.toFixed(0)}% ${t("components.providerModals.used")}`
								: `${(100 - fiveHour.percentage).toFixed(0)}% ${t("components.providerModals.left")}`
						}
						percentage={fiveHour.percentage}
						barMode={barMode}
						dataTestId="kimi-code-5h-bar"
						fillTestId="kimi-code-5h-fill"
					>
						{resetLabel(
							fiveHour.resetTime,
							t("components.providerModals.resets"),
						)}
					</QuotaBar>
				)}

				{weekly && (
					<QuotaBar
						label={t("components.providerModals.weeklyTokenQuota")}
						rightText={
							barMode === "used"
								? `${weekly.percentage.toFixed(0)}% ${t("components.providerModals.used")}`
								: `${(100 - weekly.percentage).toFixed(0)}% ${t("components.providerModals.left")}`
						}
						percentage={weekly.percentage}
						barMode={barMode}
						dataTestId="kimi-code-weekly-bar"
						fillTestId="kimi-code-weekly-fill"
					>
						{resetLabel(
							weekly.resetTime,
							t("components.providerModals.resets"),
						)}
					</QuotaBar>
				)}

				{(parallelLimit != null || totalQuota) && (
					<div className="p-3 ui-detail-section space-y-1">
						{parallelLimit != null && (
							<div
								className="flex justify-between text-xs text-(--text-muted)"
								data-testid="kimi-code-parallel"
							>
								<span>
									{t("components.providerModals.kimiCodeParallelLimit")}
								</span>
								<span>{parallelLimit}</span>
							</div>
						)}
						{totalQuota && (
							<div
								className="flex justify-between text-xs text-(--text-muted)"
								data-testid="kimi-code-total-quota"
							>
								<span>{t("components.providerModals.kimiCodeTotalQuota")}</span>
								<span>
									{totalQuota.remaining ?? "-"} / {totalQuota.limit ?? "-"}
								</span>
							</div>
						)}
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
