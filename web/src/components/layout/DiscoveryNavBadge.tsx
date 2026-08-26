import type { DiscoveryBadge } from "./useDiscrepancyModal";

/**
 * The badge beside the Models item and the only way into the discrepancy
 * modal. A number means "something may be broken". A bare dot means "there
 * is news". Price churn moves on nearly every scan, so it must never produce
 * a count.
 */
export function DiscoveryNavBadge({
	badge,
	onOpen,
}: {
	badge: DiscoveryBadge;
	onOpen: () => void;
}) {
	const { claimCount, label } = badge;
	return (
		// biome-ignore lint/a11y/useSemanticElements: a real <button> can't nest inside the nav <a>; role+keydown make this span an accessible control
		<span
			role="button"
			tabIndex={0}
			data-testid="discovery-status-badge"
			data-variant={claimCount > 0 ? "count" : "dot"}
			onClick={(e) => {
				e.preventDefault();
				e.stopPropagation();
				onOpen();
			}}
			onKeyDown={(e) => {
				if (e.key === "Enter" || e.key === " ") {
					e.preventDefault();
					e.stopPropagation();
					onOpen();
				}
			}}
			className={
				claimCount > 0
					? "inline-flex items-center leading-[1.6] translate-y-[1px] ui-badge ui-badge-accent cursor-pointer"
					: // The dot is only 8x8 CSS px but it is the sole way into the
						// informational journal, so a ::before overlay widens the hit
						// area to 24x24 without changing how it looks or shifting the
						// nav row (a pseudo-element takes no space in the flow, and
						// clicks on it target its originating element).
						"relative inline-block size-2 shrink-0 translate-y-[1px] rounded-full bg-(--accent) cursor-pointer before:absolute before:-inset-2 before:content-['']"
			}
			// Same string on both, by construction: see DiscoveryBadge.label.
			aria-label={label}
			title={label}
		>
			{claimCount > 0 ? (
				<>
					<span aria-hidden="true" className="opacity-70 mr-px">
						!
					</span>
					{claimCount}
				</>
			) : null}
		</span>
	);
}
