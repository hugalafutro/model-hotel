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
		return () => scrollEl.removeEventListener("scroll", update);
	}, [scrollEl]);

	if (!show) return null;

	const label = t("components.scrollTopButton.label");
	return (
		<button
			type="button"
			title={label}
			aria-label={label}
			data-testid="scroll-top-button"
			className="ui-btn ui-btn-secondary ui-btn-icon absolute bottom-12 right-6 z-20 shadow-lg"
			onClick={() => scrollEl?.scrollTo({ top: 0, behavior: "smooth" })}
		>
			<ArrowUpFromLine size={16} />
		</button>
	);
}
