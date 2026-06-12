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
			className={`ui-icon-btn p-1 rounded-md ${className}`}
		>
			<RotateCcw size={size} />
		</button>
	);
}
