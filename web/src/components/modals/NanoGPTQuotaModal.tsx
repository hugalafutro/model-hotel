import { useTranslation } from "react-i18next";
import { RefreshCw } from "@/lib/icons";
import type { NanoGPTUsage } from "../../api/types";
import {
	formatDate,
	formatDecimal,
	formatTimestamp,
	formatTimeUntil,
	formatTokens,
	formatWithCommas,
} from "../../utils/format";
import { DetailSectionHeader } from "../DetailSectionHeader";
import { DetailItem } from "../LogDetailItem";
import { Modal } from "../Modal";
import {
	LastRefreshedRow,
	type OnToast,
	QuotaBar,
	QuotaModalHeader,
	resetAtLabel,
	useQuotaBarMode,
	useQuotaRefreshToast,
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
	onToast: OnToast;
	lastRefreshed?: number;
}) {
	const { t } = useTranslation();
	const [barMode, toggleBarMode] = useQuotaBarMode();
	const weeklyLimit = usage.limits.weeklyInputTokens ?? 0;
	const weeklyUsed = usage.weeklyInputTokens?.used ?? 0;
	const weeklyRemaining =
		weeklyLimit > 0 ? ((weeklyLimit - weeklyUsed) / weeklyLimit) * 100 : 100;

	const handleRefresh = useQuotaRefreshToast(onRefresh, onToast);

	return (
		<Modal
			header={
				<QuotaModalHeader
					title={t("components.providerModals.nanoGPTSubscription")}
					subtitle={
						usage.active ? (
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
						)
					}
					barMode={barMode}
					onToggleBarMode={toggleBarMode}
					onRefresh={handleRefresh}
					isRefreshing={isRefreshing}
				/>
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
						? `${formatDecimal(100 - weeklyRemaining, 1)}% ${t("components.providerModals.used")}`
						: t("components.providerModals.noLimitSet")}
					{usage.weeklyInputTokens?.resetAt
						? `. ${t("components.providerModals.resets")} ${formatTimestamp(usage.weeklyInputTokens.resetAt)}\n${formatTimeUntil(usage.weeklyInputTokens.resetAt)}`
						: ""}
				</QuotaBar>

				{usage.dailyImages && (
					<QuotaBar
						label={t("components.providerModals.dailyImages")}
						rightText={`${formatWithCommas(usage.dailyImages.used)} / ${
							usage.limits.dailyImages != null
								? formatWithCommas(usage.limits.dailyImages)
								: "∞"
						}`}
						percentage={usage.dailyImages.percentUsed}
						barMode={barMode}
						fillTestId="nanogpt-daily-images-fill"
					>
						{`${formatDecimal(usage.dailyImages.percentUsed, 1)}% ${t("components.providerModals.used")}. ${resetAtLabel(usage.dailyImages.resetAt, t)}`}
					</QuotaBar>
				)}

				{usage.dailyInputTokens && (
					<QuotaBar
						label={t("components.providerModals.dailyInputTokens")}
						rightText={`${formatTokens(usage.dailyInputTokens.used)} / ${
							usage.limits.dailyInputTokens
								? formatTokens(usage.limits.dailyInputTokens)
								: "∞"
						}`}
						percentage={usage.dailyInputTokens.percentUsed}
						barMode={barMode}
						fillTestId="nanogpt-daily-input-tokens-fill"
					>
						{`${formatDecimal(usage.dailyInputTokens.percentUsed, 1)}% ${t("components.providerModals.used")}. ${resetAtLabel(usage.dailyInputTokens.resetAt, t)}`}
					</QuotaBar>
				)}

				<div>
					<DetailSectionHeader icon={RefreshCw}>
						{t("components.providerModals.subscriptionDetails")}
					</DetailSectionHeader>
					<div className="grid grid-cols-2 gap-2">
						<DetailItem label={t("components.providerModals.provider")}>
							<div className="text-sm text-(--text-primary) capitalize">
								{usage.provider}
							</div>
						</DetailItem>
						<DetailItem label={t("components.providerModals.status")}>
							<div className="text-sm text-(--text-primary) capitalize">
								{usage.providerStatus}
							</div>
						</DetailItem>
						<DetailItem
							label={t("components.providerModals.periodEnd")}
							value={formatDate(usage.period.currentPeriodEnd)}
						/>
						<DetailItem
							label={t("components.providerModals.allowOverage")}
							value={
								usage.allowOverage
									? t("components.providerModals.yes")
									: t("components.providerModals.no")
							}
						/>
					</div>
				</div>

				{usage.cancelAtPeriodEnd && (
					<div className="ui-callout ui-callout-warning">
						<p>
							{t("components.providerModals.cancelAtPeriodEnd", {
								// Non-breaking spaces keep the parenthesised date on one line.
								date: formatDate(usage.period.currentPeriodEnd).replace(
									/ /g,
									"\u00A0",
								),
							})}
						</p>
					</div>
				)}

				<LastRefreshedRow at={lastRefreshed} />
			</div>
		</Modal>
	);
}
