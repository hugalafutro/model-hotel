export interface ToggleProps {
	checked: boolean;
	onChange: (checked: boolean) => void;
	disabled?: boolean;
	size?: "default" | "sm";
	showFocusRing?: boolean;
	ariaLabel?: string;
	/** Native tooltip, e.g. to explain why a disabled toggle is locked. */
	title?: string;
	className?: string;
}

export function Toggle({
	checked,
	onChange,
	disabled = false,
	size = "default",
	showFocusRing = false,
	ariaLabel,
	title,
	className,
}: ToggleProps) {
	const sizeClasses = size === "sm" ? "h-4 w-7" : "h-6 w-11";

	const dotSize = size === "sm" ? "h-3 w-3" : "h-4 w-4";
	const onTranslate =
		size === "sm" ? "translate-x-[14px]" : "translate-x-[24px]";
	const offTranslate =
		size === "sm" ? "translate-x-[2px]" : "translate-x-[4px]";

	const focusClasses = showFocusRing
		? "focus:ring-2 focus:ring-(--accent) focus:ring-offset-2 focus:ring-offset-gray-800"
		: "";

	return (
		<button
			type="button"
			role="switch"
			aria-checked={checked}
			aria-label={ariaLabel}
			title={title}
			disabled={disabled}
			onClick={() => onChange(!checked)}
			className={`ui-toggle relative inline-flex shrink-0 ${sizeClasses} items-center rounded-full transition-colors translate-z-0 ${focusClasses} ${disabled ? "opacity-50 cursor-not-allowed" : ""} ${
				checked ? "bg-(--accent)" : "bg-gray-600"
			} ${className ?? ""}`}
		>
			<span
				className={`ui-toggle-dot inline-block ${dotSize} transform rounded-full bg-white transition-transform ${
					checked ? onTranslate : offTranslate
				}`}
			/>
		</button>
	);
}
