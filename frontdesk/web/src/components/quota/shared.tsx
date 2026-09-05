import {
	ArrowClockwiseIcon,
	ArrowsLeftRightIcon,
	XIcon,
} from "@phosphor-icons/react";
import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { barTone, type QuotaBarMode } from "../../utils/quota";
import { formatAbsolute, formatRelative } from "../../utils/time";
import { Modal } from "../Modal";

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
	barMode,
	testId,
	fillTestId,
	children,
	footer,
}: QuotaBarProps) {
	const shown = barMode === "used" ? percentage : 100 - percentage;
	const width = Math.min(Math.max(shown, 0), 100);
	const tone = barTone(percentage, barMode);

	return (
		<div className="fd-quota-bar-block">
			<div className="fd-quota-bar-head">
				<span className="fd-quota-bar-label">{label}</span>
				<span className="fd-quota-bar-right">{rightText}</span>
			</div>
			<div
				className="fd-quota-bar-track"
				{...(testId ? { "data-testid": testId } : {})}
			>
				<div
					{...(fillTestId ? { "data-testid": fillTestId } : {})}
					className={`fd-quota-bar-fill fd-quota-fill-${tone}`}
					style={{ width: `${width}%` }}
				/>
			</div>
			{children && <p className="fd-quota-bar-sub">{children}</p>}
			{footer}
		</div>
	);
}

export interface QuotaModalShellProps {
	title: string;
	subtitle?: ReactNode;
	barMode: QuotaBarMode;
	onToggleBarMode: () => void;
	onRefresh: () => void;
	isRefreshing: boolean;
	/** ISO stamp from the snapshot, i.e. when the PRIMARY last fetched upstream. */
	fetchedAt: string;
	onClose: () => void;
	children: ReactNode;
}

/**
 * Chrome shared by all six quota modals: title, optional subtitle, the bar-mode
 * toggle and refresh buttons, and the snapshot timestamp footer.
 *
 * The footer is not decoration. Front Desk shows the primary's stored snapshot
 * rather than a live fetch, and a provider that starts failing keeps its
 * last-good payload in the export, so `fetched_at` is the only signal that a
 * number is old.
 */
export function QuotaModalShell({
	title,
	subtitle,
	barMode,
	onToggleBarMode,
	onRefresh,
	isRefreshing,
	fetchedAt,
	onClose,
	children,
}: QuotaModalShellProps) {
	const { t } = useTranslation();

	return (
		<Modal
			title={title}
			subtitle={subtitle}
			onClose={onClose}
			headerActions={
				<>
					<button
						type="button"
						data-testid="quota-modal-toggle"
						className="fd-quota-modal-btn"
						onClick={onToggleBarMode}
						aria-label={t("quota.modal.toggleBarMode")}
						title={
							barMode === "remaining"
								? t("quota.modal.showUsed")
								: t("quota.modal.showRemaining")
						}
					>
						<ArrowsLeftRightIcon size={18} />
					</button>
					<button
						type="button"
						data-testid="quota-modal-refresh"
						className="fd-quota-modal-btn"
						onClick={onRefresh}
						disabled={isRefreshing}
						aria-label={t("quota.modal.refreshLabel")}
						title={t("quota.modal.refreshTitle")}
					>
						<ArrowClockwiseIcon
							size={18}
							className={isRefreshing ? "fd-spin" : undefined}
						/>
					</button>
					{/* Close stays LAST on purpose. Modal focuses the first focusable
					    control on open and headerActions renders before the body, so
					    putting close first would move initial focus off the bar-mode
					    toggle onto a dismiss button. */}
					<button
						type="button"
						data-testid="quota-modal-close"
						className="fd-quota-modal-btn"
						onClick={onClose}
						aria-label={t("common.close")}
						title={t("common.close")}
					>
						<XIcon size={18} />
					</button>
				</>
			}
		>
			<div className="fd-quota-modal-body">
				{children}
				<p
					className="fd-quota-modal-stamp"
					data-testid="quota-modal-fetched-at"
				>
					{t("quota.fetchedAt", { time: formatAbsolute(fetchedAt) })}
				</p>
			</div>
		</Modal>
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
		<div className={`fd-quota-detail-grid fd-quota-detail-grid-${columns}`}>
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
			className={`fd-quota-detail-item${span ? " fd-quota-detail-span" : ""}`}
			{...(testId ? { "data-testid": testId } : {})}
		>
			<span className="fd-quota-detail-label">{label}</span>
			<span className="fd-quota-detail-value">{value}</span>
		</div>
	);
}

export type Translate = (key: string, opts?: Record<string, unknown>) => string;

/**
 * "Resets <absolute>\n<relative>" for an ISO reset time, or "-" when the
 * provider did not report one. Rendered inside a QuotaBar sublabel, which is
 * `white-space: pre-line`, so the newline becomes a second line.
 */
// eslint-disable-next-line react-refresh/only-export-components -- label helper shared by the quota modals beside their bar components
export function resetSublabel(
	iso: string | null | undefined,
	t: Translate,
): string {
	if (!iso) return "-";
	const ms = new Date(iso).getTime();
	if (!Number.isFinite(ms)) return "-";
	return `${t("quota.modal.resets", { when: formatAbsolute(iso) })}\n${formatRelative(iso)}`;
}

/** Same, from an epoch-milliseconds instant (Z.ai) or a computed one (MiniMax). */
// eslint-disable-next-line react-refresh/only-export-components -- label helper shared by the quota modals beside their bar components
export function resetSublabelFromEpoch(
	ms: number | null | undefined,
	t: Translate,
): string {
	// `ms == null` is redundant AT RUNTIME: Number.isFinite does not coerce, so
	// Number.isFinite(null) and Number.isFinite(undefined) are already false,
	// and no test can independently kill this clause. It is NOT redundant at
	// COMPILE TIME: Number.isFinite's TS signature is `(number: unknown) =>
	// boolean`, not a type guard, so it does not narrow `ms` from `number |
	// null | undefined` down to `number`. This explicit check is what narrows
	// `ms` for the `new Date(ms)` call below; deleting it as "dead" breaks the
	// build (TS18049 / TS2769), it does not just leave untested code.
	if (ms == null || !Number.isFinite(ms) || ms <= 0) return "-";
	return resetSublabel(new Date(ms).toISOString(), t);
}

/**
 * The right-hand figure on a quota bar header: "40% used" or "60% left",
 * whichever the current bar mode calls for. `used` is a percent used, matching
 * QuotaBar's `percentage`.
 */
// eslint-disable-next-line react-refresh/only-export-components -- label helper shared by the quota modals beside their bar components
export function quotaRightText(
	used: number,
	barMode: QuotaBarMode,
	t: Translate,
): string {
	return barMode === "used"
		? `${used.toFixed(0)}% ${t("quota.modal.used")}`
		: `${(100 - used).toFixed(0)}% ${t("quota.modal.left")}`;
}
