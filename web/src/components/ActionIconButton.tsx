import type { LucideIcon } from "lucide-react";

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
	amber: {
		base: "text-amber-400",
		glow: "hover:drop-shadow-[var(--glow-amber)]",
	},
	red: {
		base: "text-red-500",
		glow: "hover:drop-shadow-[var(--glow-red)]",
	},
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
	const { base, glow } = colorClasses[color];

	if (withLabel && label) {
		return (
			<button
				type="button"
				onClick={onClick}
				className={`ui-btn flex items-center gap-2 ${base} ${glow}`}
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
			className={`p-1.5 rounded-md transition-all ${base} ${
				pulse ? "animate-[pulse-ring_1.5s_ease-in-out_infinite]" : glow
			}`}
			title={title}
			aria-label={title}
		>
			<Icon size={size} />
		</button>
	);
}
