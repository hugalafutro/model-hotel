import type { ReactNode } from "react";
import { clamp } from "../../format";
import { barTone, type QuotaBarMode } from "../display";

export interface QuotaBarProps {
	/** Already-translated label for the left of the header row. */
	label: ReactNode;
	/** Already-translated/formatted content for the right of the header row. */
	rightText: ReactNode;
	/**
	 * Percent USED, 0 to 100. Always the used share, in every caller; the
	 * component derives the remaining view itself. Passing a remaining
	 * percentage here silently inverts the bar.
	 */
	percentage: number;
	/**
	 * Percent REMAINING, 0 to 100. Defaults to 100 minus `percentage`, which is
	 * only right when the provider reports used and remaining as one
	 * complementary pair; a provider that reports the two independently passes
	 * its own.
	 */
	remainingPercentage?: number;
	barMode: QuotaBarMode;
	testId?: string;
	fillTestId?: string;
	/** Sublabel rendered under the bar. */
	children?: ReactNode;
	/** Block content rendered after the sublabel. */
	footer?: ReactNode;
}

/** A labelled progress bar, shared by every quota modal. */
export function QuotaBar({
	label,
	rightText,
	percentage,
	remainingPercentage = 100 - percentage,
	barMode,
	testId,
	fillTestId,
	children,
	footer,
}: QuotaBarProps) {
	const used = barMode === "used" ? percentage : 100 - remainingPercentage;
	// Bounded both ways: a window consumed past its cap reads as a full bar in
	// used mode and an empty one in remaining mode, not a negative width.
	const width = clamp(barMode === "used" ? used : 100 - used, 0, 100);
	const tone = barTone(used, barMode);

	return (
		<div className="ui-quota-bar-block">
			<div className="ui-quota-bar-head">
				<span className="ui-quota-bar-label">{label}</span>
				<span className="ui-quota-bar-right">{rightText}</span>
			</div>
			<div
				className="ui-quota-bar-track"
				{...(testId ? { "data-testid": testId } : {})}
			>
				<div
					{...(fillTestId ? { "data-testid": fillTestId } : {})}
					className={`ui-quota-bar-fill ui-quota-fill-${tone}`}
					style={{ width: `${width}%` }}
				/>
			</div>
			{children && <p className="ui-quota-bar-sub">{children}</p>}
			{footer}
		</div>
	);
}

export function QuotaDetailGrid({
	columns,
	children,
}: {
	columns: 2 | 3;
	children: ReactNode;
}) {
	return (
		<div className={`ui-quota-detail-grid ui-quota-detail-grid-${columns}`}>
			{children}
		</div>
	);
}

export function QuotaDetailItem({
	label,
	value,
	span,
	testId,
}: {
	label: string;
	value: ReactNode;
	span?: boolean;
	testId?: string;
}) {
	return (
		<div
			className={`ui-quota-detail-item${span ? " ui-quota-detail-span" : ""}`}
			{...(testId ? { "data-testid": testId } : {})}
		>
			<span className="ui-quota-detail-label">{label}</span>
			<span className="ui-quota-detail-value">{value}</span>
		</div>
	);
}
