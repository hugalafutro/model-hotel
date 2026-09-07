/* eslint-disable react-refresh/only-export-components -- bar colour helpers exported beside the quota bar components that use them */
import { useTranslation } from "react-i18next";
import { ArrowLeftRight, RefreshCw } from "@/lib/icons";
import { useTheme } from "../../context/ThemeContext";
import type { ToastType } from "../../context/ToastContext";
import { useLocalStorage } from "../../hooks/useLocalStorage";
import {
	formatRelativeTime,
	formatTimestamp,
	formatTimeUntil,
} from "../../utils/format";
import type { QuotaBarMode } from "../QuotaBadge";
import { Spinner } from "../Spinner";

/** The toast callback every quota modal takes. */
export type OnToast = (msg: string, type: ToastType) => void;

/**
 * Whether the bars read as used or as remaining. One stored preference, so
 * every quota modal and badge flips together.
 */
export function useQuotaBarMode(): [QuotaBarMode, () => void] {
	const [barMode, setBarMode] = useLocalStorage<QuotaBarMode>(
		"quota-bar-mode",
		"remaining",
	);
	return [
		barMode,
		() => setBarMode((prev) => (prev === "remaining" ? "used" : "remaining")),
	];
}

/** A refresh that reports its outcome as a toast. */
export function useQuotaRefreshToast(
	onRefresh: () => Promise<unknown>,
	onToast: OnToast,
) {
	const { t } = useTranslation();
	return async () => {
		try {
			await onRefresh();
			onToast(t("components.providerModals.quotaRefreshed"), "success");
		} catch {
			onToast(t("components.providerModals.failedToRefreshQuota"), "error");
		}
	};
}

/** "45% used" or "55% left", whichever the bar mode asks for. */
export function usedLeftText(
	usedPct: number,
	barMode: QuotaBarMode,
	t: (key: string) => string,
): string {
	return barMode === "used"
		? `${usedPct.toFixed(0)}% ${t("components.providerModals.used")}`
		: `${(100 - usedPct).toFixed(0)}% ${t("components.providerModals.left")}`;
}

/** "resets <timestamp>\n<time until>" for a window's reset time. */
export function resetAtLabel(
	resetTime: string | number | undefined,
	t: (key: string) => string,
): string {
	const ms = resetTime ? new Date(resetTime).getTime() : Number.NaN;
	if (!Number.isFinite(ms)) return t("common.n_a");
	return `${t("components.providerModals.resets")} ${formatTimestamp(resetTime as string | number)}\n${formatTimeUntil(ms)}`;
}

/** When the quota shown was last fetched. */
export function LastRefreshedRow({ at }: { at?: number }) {
	const { t } = useTranslation();
	if (!at) return null;
	return (
		<div className="flex justify-between items-center text-xs text-(--text-muted) pt-2">
			<span>{t("components.providerModals.lastRefreshed")}</span>
			<span>{formatRelativeTime(new Date(at).toISOString())}</span>
		</div>
	);
}

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
	/** Used percentage, 0–100. */
	percentage: number;
	/**
	 * Remaining percentage, 0–100. Defaults to (100 − percentage), which is only
	 * right when the provider reports used and remaining as one complementary
	 * pair; a provider that reports the two independently passes its own.
	 */
	remainingPercentage?: number;
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
	remainingPercentage = 100 - percentage,
	barMode,
	dataTestId,
	fillTestId,
	children,
	footer,
}: QuotaBarProps) {
	return (
		<div>
			<div className="flex justify-between items-center mb-1">
				<span className="text-sm font-medium text-(--text-secondary)">
					{label}
				</span>
				<span className="text-sm text-(--text-tertiary)">{rightText}</span>
			</div>
			<div
				{...(dataTestId ? { "data-testid": dataTestId } : {})}
				className="w-full bg-(--surface-input) ui-bar h-3"
			>
				<div
					{...(fillTestId ? { "data-testid": fillTestId } : {})}
					className={`${barMode === "used" ? usedBarColor(percentage) : remainingBarColor(remainingPercentage)} h-3 ui-bar transition-all`}
					style={{
						width: `${barMode === "used" ? Math.min(percentage, 100) : Math.min(remainingPercentage, 100)}%`,
					}}
				/>
			</div>
			{children && (
				<p className="text-xs text-(--text-muted) mt-1 whitespace-pre-line">
					{children}
				</p>
			)}
			{footer}
		</div>
	);
}

interface QuotaModalHeaderActionsProps {
	/** Which way the bars currently read, for the toggle's tooltip. */
	barMode: QuotaBarMode;
	/** Toggle between remaining/used display. */
	onToggleBarMode: () => void;
	/** Trigger a quota refresh. */
	onRefresh: () => void;
	/** Whether a refresh is in progress. */
	isRefreshing: boolean;
	/** Overrides the toggle's aria-label, for a modal with its own wording. */
	toggleAriaLabel?: string;
	/** Overrides the toggle's tooltip pair, for a modal with its own wording. */
	toggleTitle?: string;
	/** Already-translated aria-label for the refresh button. Defaults to "Refresh". */
	refreshAriaLabel?: string;
	/** Already-translated title (tooltip) for the refresh button. */
	refreshTitle?: string;
}

/**
 * QuotaModalHeaderActions renders the toggle (remaining/used) and refresh
 * buttons in the modal header. All four provider quota modals share this
 * exact layout.
 */
export function QuotaModalHeaderActions({
	barMode,
	onToggleBarMode,
	onRefresh,
	isRefreshing,
	toggleAriaLabel,
	toggleTitle,
	refreshAriaLabel,
	refreshTitle,
}: QuotaModalHeaderActionsProps) {
	const { uiStyle } = useTheme();
	const { t } = useTranslation();
	const defaultToggleTitle =
		barMode === "remaining"
			? t("components.providerModals.showQuotaUsed")
			: t("components.providerModals.showQuotaRemaining");

	return (
		<div className="absolute top-4 right-16 flex items-center gap-1">
			<button
				type="button"
				onClick={() => onToggleBarMode()}
				className="ui-icon-btn p-1.5"
				aria-label={
					toggleAriaLabel ?? t("components.providerModals.toggleRemainingUsed")
				}
				title={toggleTitle ?? defaultToggleTitle}
			>
				<ArrowLeftRight size={18} />
			</button>
			<button
				type="button"
				onClick={onRefresh}
				disabled={isRefreshing}
				className="ui-icon-btn p-1.5"
				aria-label={refreshAriaLabel ?? t("common.refresh")}
				title={refreshTitle ?? t("components.providerModals.refreshQuotaInfo")}
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
