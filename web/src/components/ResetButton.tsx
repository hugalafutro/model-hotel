import { RotateCcw } from "lucide-react";

interface ResetButtonProps {
	/** Tooltip text (already i18n'd) */
	tooltip: string;
	/** Click handler */
	onClick: () => void;
	/** Icon size, defaults to 14 */
	size?: number;
	/** Additional class names */
	className?: string;
	/** Disable the button */
	disabled?: boolean;
}

export function ResetButton({
	tooltip,
	onClick,
	size = 14,
	className = "",
	disabled = false,
}: ResetButtonProps) {
	return (
		<button
			type="button"
			onClick={onClick}
			disabled={disabled}
			title={tooltip}
			aria-label={tooltip}
			className={`text-gray-500 hover:text-amber-400 disabled:opacity-30 disabled:cursor-not-allowed transition-colors ${className}`}
		>
			<RotateCcw size={size} />
		</button>
	);
}
