import { Info } from "@/lib/icons";

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
		<span
			title={tooltip}
			className={`ui-icon-btn cursor-help inline-flex items-center ${className}`.trimEnd()}
		>
			<Info size={size} />
		</span>
	);
}
