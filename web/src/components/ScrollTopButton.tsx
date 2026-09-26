import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { ArrowUpFromLine } from "@/lib/icons";

/**
 * Floating "back to top" control for the virtualised tables. It appears once
 * the scroller is more than one viewport of rows down, which is the point at
 * which the header has been out of sight long enough to be worth a shortcut.
 *
 * Absolutely positioned, so the scroller's wrapper must be `relative`; it sits
 * above the table footer rather than over the last row.
 */
/** How many screens up a return to the top may still animate. */
const SMOOTH_SCREENS = 3;

export function ScrollTopButton({
	scrollEl,
}: {
	scrollEl: HTMLElement | null;
}) {
	const { t } = useTranslation();
	const [show, setShow] = useState(false);

	useEffect(() => {
		if (!scrollEl) return;
		const update = () => setShow(scrollEl.scrollTop > scrollEl.clientHeight);
		update();
		scrollEl.addEventListener("scroll", update, { passive: true });
		// The scroller's height follows the viewport, so a viewport change moves
		// the threshold without producing a scroll event of its own.
		window.addEventListener("resize", update);
		return () => {
			scrollEl.removeEventListener("scroll", update);
			window.removeEventListener("resize", update);
		};
	}, [scrollEl]);

	if (!show) return null;

	const label = t("components.scrollTopButton.label");
	const scrollToTop = () => {
		if (!scrollEl) return;
		// Returning to the top unmounts this button, so a keyboard user would
		// lose focus to <body> and have to tab in from the start of the page.
		// Hand focus to the scroller instead, the way a skip link hands focus to
		// its target: arrow keys then keep scrolling the rows. The scroller
		// carries tabIndex={-1} so it can take focus without joining tab order.
		scrollEl.focus({ preventScroll: true });
		// Smooth only from near the top. The virtualizer corrects the scroll
		// position whenever rows it had only estimated get measured, and any
		// such correction cancels a smooth scroll in flight: from far down a
		// list of uneven rows, the animation stalls part way. Within a few
		// screens the rows on the way up are already measured.
		const near = scrollEl.scrollTop <= scrollEl.clientHeight * SMOOTH_SCREENS;
		const reduced = window.matchMedia(
			"(prefers-reduced-motion: reduce)",
		).matches;
		scrollEl.scrollTo({
			top: 0,
			behavior: near && !reduced ? "smooth" : "auto",
		});
	};

	return (
		<button
			type="button"
			title={label}
			aria-label={label}
			data-testid="scroll-top-button"
			className="ui-btn ui-btn-secondary ui-btn-icon absolute bottom-12 end-6 z-20 shadow-lg"
			onClick={scrollToTop}
		>
			<ArrowUpFromLine size={16} />
		</button>
	);
}
