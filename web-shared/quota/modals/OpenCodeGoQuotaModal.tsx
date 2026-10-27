import { getOpenCodeGoWindows, type OpenCodeGoWindowKey } from "../opencodeGo";
import type { OpenCodeGoUsageResponse } from "../types";
import { type QuotaBodyProps, quotaRightText, resetSublabel } from "./labels";
import { QuotaBar } from "./primitives";

/**
 * The label each window is shown under, keyed by window. The order the bars
 * render in comes from getOpenCodeGoWindows, not from this record.
 */
const WINDOW_LABEL_KEYS = {
	rolling: "openCodeGoRolling",
	weekly: "openCodeGoWeekly",
	monthly: "openCodeGoMonthly",
} as const satisfies Record<OpenCodeGoWindowKey, string>;

export function OpenCodeGoQuotaModal({
	providerName,
	payload,
	barMode,
	kit: { shell, t, time },
}: QuotaBodyProps<OpenCodeGoUsageResponse>) {
	// The selector reports percent USED, clamped, which is what QuotaBar wants,
	// and it drops the windows the payload omits, so a bar exists only for a
	// window OpenCode Go actually reported.
	const windows = getOpenCodeGoWindows(payload);

	return shell({
		title: t("openCodeGoPlanQuota", { provider: providerName }),
		children: windows.map((w) => (
			<QuotaBar
				key={w.key}
				label={t(WINDOW_LABEL_KEYS[w.key])}
				rightText={quotaRightText(w.percent, barMode, t)}
				percentage={w.percent}
				barMode={barMode}
				testId={`opencode-go-${w.key}-bar`}
				fillTestId={`opencode-go-${w.key}-fill`}
			>
				{resetSublabel(w.resetsAt, t, time)}
			</QuotaBar>
		)),
	});
}
