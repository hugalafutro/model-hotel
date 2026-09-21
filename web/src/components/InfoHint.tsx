import { useTranslation } from "react-i18next";
import { Info } from "@/lib/icons";
import { priceSourceKey } from "../utils/model";

interface InfoHintProps {
	/** Tooltip text shown on hover (native title attribute). */
	tooltip: string;
	/** Icon size in px (default 12). */
	size?: number;
	/** Extra classes appended to the span (e.g. shrink-0, ui-icon-btn-in-group). */
	className?: string;
}

/**
 * Shared "(i)" help hint: a small Info icon with a help cursor and a native
 * tooltip. Replaces the copies that previously inlined
 * `<span className="ui-icon-btn cursor-help" title={…}><Info size={12} /></span>`.
 *
 * inline-flex, not inline: Tailwind's preflight makes every svg a block, and
 * a block inside an inline span breaks the line around it, so a hint placed
 * mid-text (a section header) would drop its icon onto the next line.
 */
export function InfoHint({
	tooltip,
	size = 12,
	className = "",
}: InfoHintProps) {
	return (
		// Focusable, with the hint as its accessible name, so a keyboard or
		// screen-reader user reaches the same text the title shows on hover.
		<span
			title={tooltip}
			role="img"
			aria-label={tooltip}
			// biome-ignore lint/a11y/noNoninteractiveTabindex: the hint text is only reachable through focus
			tabIndex={0}
			className={`ui-icon-btn cursor-help inline-flex items-center ${className}`.trimEnd()}
		>
			<Info size={size} />
		</span>
	);
}

/**
 * The "(i)" beside a price, naming where the backend got that price from.
 * Every price the dashboard shows says its source, so the
 * `models.priceSource.*` key prefix is spelled here and nowhere else.
 */
export function PriceSourceHint({
	source,
	className,
}: {
	source: string | undefined;
	className?: string;
}) {
	const { t } = useTranslation();
	return (
		<InfoHint
			tooltip={t(`models.priceSource.${priceSourceKey(source)}`)}
			className={className}
		/>
	);
}
