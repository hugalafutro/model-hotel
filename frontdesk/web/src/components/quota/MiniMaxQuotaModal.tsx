import { useTranslation } from "react-i18next";
import type {
	MiniMaxModelRemains,
	MiniMaxQuotaResponse,
} from "../../api/types";
import type { QuotaBarMode } from "../../utils/quota";
import {
	QuotaBar,
	QuotaModalShell,
	quotaRightText,
	resetSublabelFromEpoch,
	type Translate,
} from "./shared";

/** Human label for a MiniMax model class; unknown classes show their raw name. */
function classLabel(modelName: string, t: Translate): string {
	if (modelName === "general") return t("quota.modal.miniMaxChatModels");
	if (modelName === "video") return t("quota.modal.miniMaxVideoModels");
	return modelName;
}

function ModelClassRows({
	entry,
	barMode,
	fetchedAt,
}: {
	entry: MiniMaxModelRemains;
	barMode: QuotaBarMode;
	fetchedAt: string;
}) {
	const { t } = useTranslation();
	const name = entry.model_name;
	const label = classLabel(name, t);

	// Status 3 means the class is not part of the active plan: there is no quota
	// to draw, so show a placeholder rather than two empty bars.
	if (entry.current_interval_status === 3) {
		return (
			<div className="fd-quota-class">
				<p className="fd-quota-class-label">{label}</p>
				<p
					className="fd-quota-bar-sub"
					data-testid={`minimax-${name}-not-in-plan`}
				>
					{t("quota.modal.miniMaxNotInPlan")}
				</p>
			</div>
		);
	}

	// MiniMax reports REMAINING percentages, so invert for the used share.
	const fiveHourUsed = 100 - entry.current_interval_remaining_percent;
	const weeklyUsed = 100 - entry.current_weekly_remaining_percent;

	// Interval length varies by class (chat 5h, video 24h), so derive it from the
	// window bounds instead of hardcoding 5.
	const intervalHours =
		entry.end_time != null &&
		entry.start_time != null &&
		entry.end_time > entry.start_time
			? Math.round((entry.end_time - entry.start_time) / 3_600_000)
			: 5;

	const rightText = (used: number) => quotaRightText(used, barMode, t);

	// remains_time/weekly_remains_time are durations measured when the provider
	// answered, which is fetchedAt: anchor there so the reset instant stays put
	// across re-renders instead of walking forward with the clock.
	const fetchedAtMs = new Date(fetchedAt).getTime();
	const fiveHourResetAt =
		entry.remains_time > 0 ? fetchedAtMs + entry.remains_time : null;
	const weeklyResetAt =
		entry.weekly_remains_time > 0
			? fetchedAtMs + entry.weekly_remains_time
			: null;

	return (
		<div className="fd-quota-class">
			<p className="fd-quota-class-label">{label}</p>
			<QuotaBar
				label={
					<span data-testid={`minimax-${name}-5h-label`}>
						{t("quota.modal.hourTokenQuota", { hours: intervalHours })}
					</span>
				}
				rightText={rightText(fiveHourUsed)}
				percentage={fiveHourUsed}
				barMode={barMode}
				testId={`minimax-${name}-5h-bar`}
				fillTestId={`minimax-${name}-5h-fill`}
			>
				{resetSublabelFromEpoch(fiveHourResetAt, t)}
			</QuotaBar>
			<QuotaBar
				label={t("quota.modal.weeklyTokenQuota")}
				rightText={rightText(weeklyUsed)}
				percentage={weeklyUsed}
				barMode={barMode}
				testId={`minimax-${name}-weekly-bar`}
				fillTestId={`minimax-${name}-weekly-fill`}
			>
				{resetSublabelFromEpoch(weeklyResetAt, t)}
			</QuotaBar>
		</div>
	);
}

export interface MiniMaxQuotaModalProps {
	providerName: string;
	payload: MiniMaxQuotaResponse;
	fetchedAt: string;
	barMode: QuotaBarMode;
	onToggleBarMode: () => void;
	onRefresh: () => void;
	isRefreshing: boolean;
	onClose: () => void;
}

export function MiniMaxQuotaModal({
	providerName,
	payload,
	fetchedAt,
	barMode,
	onToggleBarMode,
	onRefresh,
	isRefreshing,
	onClose,
}: MiniMaxQuotaModalProps) {
	const { t } = useTranslation();
	// Unlike getMiniMaxGeneralEntry (web-shared/quota), this does not gate on
	// base_resp.status_code === 0. That is safe, not an oversight: a non-zero
	// status produces no visible badge (toBadgeModels/isQuotaPayloadVisible), so
	// this modal is never opened for that payload in the first place. Model
	// Hotel's equivalent modal makes the same simplification.
	const entries = payload.model_remains ?? [];

	return (
		<QuotaModalShell
			title={t("quota.modal.miniMaxTitle", { provider: providerName })}
			barMode={barMode}
			onToggleBarMode={onToggleBarMode}
			onRefresh={onRefresh}
			isRefreshing={isRefreshing}
			fetchedAt={fetchedAt}
			onClose={onClose}
		>
			{entries.map((entry) => (
				<ModelClassRows
					key={entry.model_name}
					entry={entry}
					barMode={barMode}
					fetchedAt={fetchedAt}
				/>
			))}
		</QuotaModalShell>
	);
}
