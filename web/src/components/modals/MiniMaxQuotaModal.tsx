import { useTranslation } from "react-i18next";
import type {
	MiniMaxModelRemains,
	MiniMaxQuotaResponse,
} from "../../api/types";
import { useLocalStorage } from "../../hooks/useLocalStorage";
import { formatRelativeTime, formatTimeUntil } from "../../utils/format";
import { Modal } from "../Modal";
import { QuotaBar, QuotaModalHeaderActions } from "./shared";

/** Human label for a MiniMax model class. */
function classLabel(modelName: string, t: (k: string) => string): string {
	if (modelName === "general")
		return t("components.providerModals.miniMaxChatModels");
	if (modelName === "video")
		return t("components.providerModals.miniMaxVideoModels");
	return modelName;
}

/** Renders "resets <time-until>" for a millisecond reset duration. */
function resetCountdown(ms: number, resetsWord: string): string {
	if (!Number.isFinite(ms) || ms <= 0) return "N/A";
	return `${resetsWord} ${formatTimeUntil(Date.now() + ms)}`;
}

function ModelClassRows({
	entry,
	barMode,
}: {
	entry: MiniMaxModelRemains;
	barMode: "used" | "remaining";
}) {
	const { t } = useTranslation();
	const name = entry.model_name;
	const label = classLabel(name, t);

	// A class in status 3 is not part of the active plan: render a placeholder
	// row instead of quota bars.
	if (entry.current_interval_status === 3) {
		return (
			<div>
				<p className="text-sm font-medium text-(--text-secondary) mb-1">
					{label}
				</p>
				<p
					className="text-xs text-(--text-muted)"
					data-testid={`minimax-${name}-not-in-plan`}
				>
					{t("components.providerModals.miniMaxNotInPlan")}
				</p>
			</div>
		);
	}

	const fiveHourUsed = 100 - entry.current_interval_remaining_percent;
	const weeklyUsed = 100 - entry.current_weekly_remaining_percent;
	const resetsWord = t("components.providerModals.resets");
	// Interval length varies by model class (chat 5h, video 24h), so derive it
	// from the window bounds instead of hardcoding 5.
	const intervalHours =
		entry.end_time != null &&
		entry.start_time != null &&
		entry.end_time > entry.start_time
			? Math.round((entry.end_time - entry.start_time) / 3_600_000)
			: 5;

	const rightText = (used: number) =>
		barMode === "used"
			? `${used.toFixed(0)}% ${t("components.providerModals.used")}`
			: `${(100 - used).toFixed(0)}% ${t("components.providerModals.left")}`;

	return (
		<div className="space-y-4">
			<p className="text-sm font-medium text-(--text-secondary)">{label}</p>
			<QuotaBar
				label={t("components.providerModals.hTokenQuota", {
					hours: intervalHours,
				})}
				rightText={rightText(fiveHourUsed)}
				percentage={fiveHourUsed}
				barMode={barMode}
				dataTestId={`minimax-${name}-5h-bar`}
				fillTestId={`minimax-${name}-5h-fill`}
			>
				{resetCountdown(entry.remains_time, resetsWord)}
			</QuotaBar>
			<QuotaBar
				label={t("components.providerModals.weeklyTokenQuota")}
				rightText={rightText(weeklyUsed)}
				percentage={weeklyUsed}
				barMode={barMode}
				dataTestId={`minimax-${name}-weekly-bar`}
				fillTestId={`minimax-${name}-weekly-fill`}
			>
				{resetCountdown(entry.weekly_remains_time, resetsWord)}
			</QuotaBar>
		</div>
	);
}

export function MiniMaxQuotaModal({
	usage,
	onClose,
	onRefresh,
	isRefreshing,
	onToast,
	lastRefreshed,
}: {
	usage: MiniMaxQuotaResponse;
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

	const entries = usage.model_remains ?? [];

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
							{t("components.providerModals.miniMaxPlanQuota")}
						</h2>
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
				{entries.map((entry) => (
					<ModelClassRows
						key={entry.model_name}
						entry={entry}
						barMode={barMode}
					/>
				))}

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
