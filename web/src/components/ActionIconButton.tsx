import type { LucideIcon } from "@/lib/icons";

interface ActionIconButtonProps {
	icon: LucideIcon;
	onClick: () => void;
	title: string;
	color: "amber" | "red";
	pulse?: boolean;
	size?: number;
	label?: string;
	/** Use ui-btn styling instead of icon-only */
	withLabel?: boolean;
}

const colorClasses = {
	amber: "ui-icon-btn ui-icon-btn-warning",
	red: "ui-icon-btn ui-icon-btn-danger",
};

export function ActionIconButton({
	icon: Icon,
	onClick,
	title,
	color,
	pulse = false,
	size = 14,
	label,
	withLabel = false,
}: ActionIconButtonProps) {
	const iconClasses = colorClasses[color];

	if (withLabel && label) {
		return (
			<button
				type="button"
				onClick={onClick}
				className={`ui-btn flex items-center gap-2 ${iconClasses}`}
			>
				<Icon size={size} />
				{label}
			</button>
		);
	}

	return (
		<button
			type="button"
			onClick={onClick}
			className={`${iconClasses} p-1.5 rounded-md ${
				pulse ? "animate-[pulse-ring_1.5s_ease-in-out_infinite]" : ""
			}`}
			title={title}
			aria-label={title}
		>
			<Icon size={size} />
		</button>
	);
}
