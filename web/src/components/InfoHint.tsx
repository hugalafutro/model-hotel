import { Info } from "lucide-react";

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
 */
export function InfoHint({
	tooltip,
	size = 12,
	className = "",
}: InfoHintProps) {
	return (
		<span
			title={tooltip}
			className={`ui-icon-btn cursor-help ${className}`.trimEnd()}
		>
			<Info size={size} />
		</span>
	);
}
