import { getOpenCodeGoWindows } from "@web-shared/quota";
import { useTranslation } from "react-i18next";
import type { OpenCodeGoUsageResponse } from "../../api/types";
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

/** The label each window is shown under, in the order OpenCode Go reports. */
const WINDOW_LABEL_KEYS = {
	rolling: "components.providerModals.openCodeGoRolling",
	weekly: "components.providerModals.openCodeGoWeekly",
	monthly: "components.providerModals.openCodeGoMonthly",
} as const;

export function OpenCodeGoQuotaModal({
	usage,
	onClose,
	onRefresh,
	isRefreshing,
	onToast,
	lastRefreshed,
}: {
	usage: OpenCodeGoUsageResponse;
	onClose: () => void;
	onRefresh: () => Promise<unknown>;
	isRefreshing: boolean;
	onToast: OnToast;
	lastRefreshed?: number;
}) {
	const { t } = useTranslation();
	const [barMode, toggleBarMode] = useQuotaBarMode();

	const windows = getOpenCodeGoWindows(usage);
	const handleRefresh = useQuotaRefreshToast(onRefresh, onToast);

	return (
		<Modal
			header={
				<div className="flex justify-between items-start mb-6">
					<h2 className="text-xl font-bold text-(--text-primary)">
						{t("components.providerModals.openCodeGoPlanQuota")}
					</h2>
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
				{windows.map((w) => (
					<QuotaBar
						key={w.key}
						label={t(WINDOW_LABEL_KEYS[w.key])}
						rightText={usedLeftText(w.percent, barMode, t)}
						percentage={w.percent}
						barMode={barMode}
						dataTestId={`opencode-go-${w.key}-bar`}
						fillTestId={`opencode-go-${w.key}-fill`}
					>
						{resetAtLabel(w.resetsAt, t)}
					</QuotaBar>
				))}

				<LastRefreshedRow at={lastRefreshed} />
			</div>
		</Modal>
	);
}
