import { useTranslation } from "react-i18next";
import { formatCompact, formatNumber } from "../utils/format";

/**
 * The status line under every data table (models, request and app logs,
 * audit): "Showing 1–45 of 1.1K" on the left, the row range grouped in the
 * active locale and the total compact, with the exact total in its tooltip;
 * on the right, what is still
 * loading and any extra controls (a spinner, pagination). One component so
 * every table ends the same way: a small gap under the table, the line, a
 * slightly larger gap to the bottom of the page.
 */
export function TableFooter({
	start,
	end,
	total,
	isLoadingBefore = false,
	isLoadingAfter = false,
	children,
}: {
	/** 1-based index of the first row shown; ignored when end is 0. */
	start: number;
	/** 1-based index of the last row shown, 0 when none are. */
	end: number;
	total: number;
	isLoadingBefore?: boolean;
	isLoadingAfter?: boolean;
	/** Extra status or controls, shown after the loading labels. */
	children?: React.ReactNode;
}) {
	const { t } = useTranslation();
	const status = (fmtTotal: (n: number) => string) =>
		end > 0
			? t("common.showingRange", {
					start: formatNumber(start),
					end: formatNumber(end),
					total: fmtTotal(total),
				})
			: t("common.showingNone", { total: fmtTotal(total) });
	return (
		<div className="flex items-center justify-between gap-3 px-1 pt-1.5 pb-1 text-sm text-gray-500 shrink-0">
			<span title={status(formatNumber)}>{status(formatCompact)}</span>
			<span className="flex items-center gap-2">
				{isLoadingBefore && (
					<span className="text-(--accent)">{t("common.loadingNewer")}</span>
				)}
				{isLoadingAfter && (
					<span className="text-(--accent)">{t("common.loadingOlder")}</span>
				)}
				{children}
			</span>
		</div>
	);
}
