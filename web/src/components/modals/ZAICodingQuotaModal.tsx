import {
	getZaiCodingFiveHourLimit,
	getZaiCodingMcpLimit,
	getZaiCodingWeeklyLimit,
} from "@web-shared/quota";
import { useTranslation } from "react-i18next";
import type { ZAICodingQuotaResponse } from "../../api/types";
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
	onToast: OnToast;
	lastRefreshed?: number;
}) {
	const { t } = useTranslation();
	const [barMode, toggleBarMode] = useQuotaBarMode();
	const fiveHourLimit = getZaiCodingFiveHourLimit(usage);
	const weeklyLimit = getZaiCodingWeeklyLimit(usage);
	const mcpLimit = getZaiCodingMcpLimit(usage);

	const handleRefresh = useQuotaRefreshToast(onRefresh, onToast);

	return (
		<Modal
			header={
				<div className="flex justify-between items-start mb-6">
					<div>
						<h2 className="text-xl font-bold text-(--text-primary)">
							{t("components.providerModals.zAICodingPlanQuota")}
						</h2>
						<p className="text-sm text-(--text-tertiary) mt-1">
							{t("components.providerModals.plan")}{" "}
							<span className="text-gray-200 capitalize">
								{usage.data?.level ?? "-"}
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
				{fiveHourLimit && (
					<QuotaBar
						label={t("components.providerModals.hTokenQuota", { hours: 5 })}
						rightText={usedLeftText(fiveHourLimit.percentage, barMode, t)}
						percentage={fiveHourLimit.percentage}
						barMode={barMode}
					>
						{resetAtLabel(fiveHourLimit.nextResetTime, t)}
					</QuotaBar>
				)}

				{weeklyLimit && (
					<QuotaBar
						label={t("components.providerModals.weeklyTokenQuota")}
						rightText={usedLeftText(weeklyLimit.percentage, barMode, t)}
						percentage={weeklyLimit.percentage}
						barMode={barMode}
					>
						{resetAtLabel(weeklyLimit.nextResetTime, t)}
					</QuotaBar>
				)}

				{mcpLimit && (
					<QuotaBar
						label={t("components.providerModals.mcpTokenQuota")}
						rightText={usedLeftText(mcpLimit.percentage, barMode, t)}
						percentage={mcpLimit.percentage}
						barMode={barMode}
						footer={
							mcpLimit.usageDetails &&
							mcpLimit.usageDetails.length > 0 && (
								<div className="mt-2 space-y-1 p-3 ui-detail-section">
									{mcpLimit.usageDetails.map((detail) => (
										<div
											key={detail.modelCode}
											className="flex justify-between text-xs text-(--text-muted)"
										>
											<span className="capitalize">{detail.modelCode}</span>
											<span>
												{detail.usage} {t("components.providerModals.used")}
											</span>
										</div>
									))}
								</div>
							)
						}
					>
						{resetAtLabel(mcpLimit.nextResetTime, t)}
					</QuotaBar>
				)}

				<LastRefreshedRow at={lastRefreshed} />
			</div>
		</Modal>
	);
}
