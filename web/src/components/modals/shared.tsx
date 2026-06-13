/* eslint-disable react-refresh/only-export-components */
import { ArrowLeftRight, RefreshCw } from "lucide-react";
import { useTheme } from "../../context/ThemeContext";
import { Spinner } from "../Spinner";

/** Returns a Tailwind bg-[color] class based on remaining percentage. */
export function remainingBarColor(remainingPct: number): string {
	if (remainingPct < 20) return "bg-red-500";
	if (remainingPct < 60) return "bg-amber-500";
	return "bg-[#6366F1]";
}

/** Returns a Tailwind bg-[color] class based on used percentage. */
export function usedBarColor(usedPct: number): string {
	if (usedPct < 50) return "bg-amber-500";
	if (usedPct < 80) return "bg-orange-500";
	return "bg-red-500";
}

interface QuotaBarProps {
	/** Already-translated label for the left side of the header row. */
	label: string;
	/** Already-translated/formatted content for the right side of the header row. */
	rightText: React.ReactNode;
	/** Used percentage, 0–100. The component computes remaining as (100 − percentage). */
	percentage: number;
	/** Whether to show "used" or "remaining" coloring and width. */
	barMode: "used" | "remaining";
	/** Optional data-testid for the bar track div. */
	dataTestId?: string;
	/** Optional data-testid for the inner fill div. */
	fillTestId?: string;
	/** Optional sublabel content rendered below the bar. */
	children?: React.ReactNode;
	/** Optional block content rendered as a sibling after the sublabel paragraph. */
	footer?: React.ReactNode;
}

/**
 * QuotaBar renders a labelled progress bar used across provider quota modals.
 *
 * The header row shows `label` on the left and `rightText` on the right.
 * The bar track uses the shared `usedBarColor`/`remainingBarColor` helpers.
 * Pass sublabel content as `children`.
 */
export function QuotaBar({
	label,
	rightText,
	percentage,
	barMode,
	dataTestId,
	fillTestId,
	children,
	footer,
}: QuotaBarProps) {
	return (
		<div>
			<div className="flex justify-between items-center mb-2">
				<span className="text-sm font-medium text-(--text-secondary)">
					{label}
				</span>
				<span className="text-sm text-(--text-tertiary)">{rightText}</span>
			</div>
			<div
				{...(dataTestId ? { "data-testid": dataTestId } : {})}
				className="w-full bg-(--surface-input) rounded-full h-3"
			>
				<div
					{...(fillTestId ? { "data-testid": fillTestId } : {})}
					className={`${barMode === "used" ? usedBarColor(percentage) : remainingBarColor(100 - percentage)} h-3 rounded-full transition-all`}
					style={{
						width: `${barMode === "used" ? Math.min(percentage, 100) : Math.min(100 - percentage, 100)}%`,
					}}
				/>
			</div>
			{children && (
				<p className="text-xs text-(--text-muted) mt-1">{children}</p>
			)}
			{footer}
		</div>
	);
}

interface QuotaModalHeaderActionsProps {
	/** Toggle between remaining/used display. */
	onToggleBarMode: () => void;
	/** Trigger a quota refresh. */
	onRefresh: () => void;
	/** Whether a refresh is in progress. */
	isRefreshing: boolean;
	/** Already-translated aria-label for the toggle button. */
	toggleAriaLabel: string;
	/** Already-translated title (tooltip) for the toggle button. */
	toggleTitle: string;
	/** Already-translated aria-label for the refresh button. */
	refreshAriaLabel: string;
	/** Already-translated title (tooltip) for the refresh button. */
	refreshTitle: string;
}

/**
 * QuotaModalHeaderActions renders the toggle (remaining/used) and refresh
 * buttons in the modal header. All four provider quota modals share this
 * exact layout.
 */
export function QuotaModalHeaderActions({
	onToggleBarMode,
	onRefresh,
	isRefreshing,
	toggleAriaLabel,
	toggleTitle,
	refreshAriaLabel,
	refreshTitle,
}: QuotaModalHeaderActionsProps) {
	const { uiStyle } = useTheme();

	return (
		<div className="flex items-center gap-2">
			<button
				type="button"
				onClick={() => onToggleBarMode()}
				className="ui-icon-btn absolute top-4 right-20 p-1.5"
				aria-label={toggleAriaLabel}
				title={toggleTitle}
			>
				<ArrowLeftRight size={18} />
			</button>
			<button
				type="button"
				onClick={onRefresh}
				disabled={isRefreshing}
				className="ui-icon-btn absolute top-4 right-10 p-1.5"
				aria-label={refreshAriaLabel}
				title={refreshTitle}
			>
				{isRefreshing && uiStyle === "cyber-terminal" ? (
					<Spinner className="w-[18px] h-[18px] text-[18px] leading-[18px]" />
				) : (
					<RefreshCw size={18} className={isRefreshing ? "animate-spin" : ""} />
				)}
			</button>
		</div>
	);
}
