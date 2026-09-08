import { useTranslation } from "react-i18next";
import type {
	OpenCodeGoUsageResponse,
	OpenCodeGoWindowKey,
} from "../../api/types";
import { getOpenCodeGoWindows } from "../../utils/quota";
import {
	QuotaBar,
	type QuotaModalProps,
	QuotaModalShell,
	quotaRightText,
	resetSublabel,
} from "./shared";

/**
 * The label each window is shown under, keyed by window. The order the bars
 * render in comes from getOpenCodeGoWindows, not from this record.
 */
const WINDOW_LABEL_KEYS = {
	rolling: "quota.modal.openCodeGoRolling",
	weekly: "quota.modal.openCodeGoWeekly",
	monthly: "quota.modal.openCodeGoMonthly",
} as const satisfies Record<OpenCodeGoWindowKey, string>;

export function OpenCodeGoQuotaModal({
	providerName,
	payload,
	barMode,
	...shell
}: QuotaModalProps<OpenCodeGoUsageResponse>) {
	const { t } = useTranslation();

	// The selector reports percent USED, clamped, which is what QuotaBar wants,
	// and it drops the windows the payload omits, so a bar exists only for a
	// window OpenCode Go actually reported.
	const windows = getOpenCodeGoWindows(payload);

	return (
		<QuotaModalShell
			title={t("quota.modal.openCodeGoTitle", { provider: providerName })}
			barMode={barMode}
			{...shell}
		>
			{windows.map((w) => (
				<QuotaBar
					key={w.key}
					label={t(WINDOW_LABEL_KEYS[w.key])}
					rightText={quotaRightText(w.percent, barMode, t)}
					percentage={w.percent}
					barMode={barMode}
					testId={`opencode-go-${w.key}-bar`}
					fillTestId={`opencode-go-${w.key}-fill`}
				>
					{resetSublabel(w.resetsAt, t)}
				</QuotaBar>
			))}
		</QuotaModalShell>
	);
}
