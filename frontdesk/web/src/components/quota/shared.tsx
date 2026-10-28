import {
	ArrowClockwiseIcon,
	ArrowsLeftRightIcon,
	XIcon,
} from "@phosphor-icons/react";
import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import type { QuotaBarMode } from "../../utils/quota";
import { formatAbsolute } from "../../utils/time";
import { Modal } from "../Modal";

/**
 * What every provider's quota modal takes: its own payload plus the shell's own
 * controls, forwarded straight through. The shell owns the header, so a modal
 * writes only the parts that are its own (the title, the subtitle and the bars)
 * and spreads the rest, which means adding a shell control touches the shell
 * alone rather than all seven modals.
 */
export type QuotaModalProps<P> = Omit<
	QuotaModalShellProps,
	"title" | "subtitle" | "children"
> & {
	providerName: string;
	payload: P;
};

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
 * Chrome shared by all seven quota modals: title, optional subtitle, the bar-mode
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
