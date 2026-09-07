import { useTranslation } from "react-i18next";
import type { KimiCodeQuotaResponse } from "../../api/types";
import {
	getKimiCodeFiveHourLimit,
	getKimiCodeWeeklyLimit,
} from "../../hooks/useQuotaData";
import { Modal } from "../Modal";
import {
	LastRefreshedRow,
	type OnToast,
	QuotaBar,
	QuotaModalHeaderActions,
	resetAtLabel,
	usedLeftText,
	useQuotaBarMode,
	useQuotaRefreshToast,
} from "./shared";

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
	onToast: OnToast;
	lastRefreshed?: number;
}) {
	const { t } = useTranslation();
	const [barMode, toggleBarMode] = useQuotaBarMode();

	const fiveHour = getKimiCodeFiveHourLimit(usage);
	const weekly = getKimiCodeWeeklyLimit(usage);
	const level = usage.user?.membership?.level;
	const parallelLimit = usage.parallel?.limit;
	const totalQuota = usage.totalQuota;

	const handleRefresh = useQuotaRefreshToast(onRefresh, onToast);

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
				{fiveHour && (
					<QuotaBar
						label={t("components.providerModals.hTokenQuota", { hours: 5 })}
						rightText={usedLeftText(fiveHour.percentage, barMode, t)}
						percentage={fiveHour.percentage}
						barMode={barMode}
						dataTestId="kimi-code-5h-bar"
						fillTestId="kimi-code-5h-fill"
					>
						{resetAtLabel(fiveHour.resetTime, t)}
					</QuotaBar>
				)}

				{weekly && (
					<QuotaBar
						label={t("components.providerModals.weeklyTokenQuota")}
						rightText={usedLeftText(weekly.percentage, barMode, t)}
						percentage={weekly.percentage}
						barMode={barMode}
						dataTestId="kimi-code-weekly-bar"
						fillTestId="kimi-code-weekly-fill"
					>
						{resetAtLabel(weekly.resetTime, t)}
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

				<LastRefreshedRow at={lastRefreshed} />
			</div>
		</Modal>
	);
}
