import { memo } from "react";

export type BadgeVariant =
	| "error"
	| "warning"
	| "info"
	| "success"
	| "muted"
	| "accent"
	| "orange"
	| "custom";

const VARIANT_CLASSES: Record<BadgeVariant, string> = {
	error: "bg-red-900/30 text-red-400",
	warning: "bg-yellow-900/30 text-yellow-400",
	info: "bg-blue-900/30 text-blue-400",
	success: "bg-green-900/30 text-green-400",
	muted: "bg-gray-700/30 text-gray-400",
	accent: "bg-(--accent)/20 text-(--accent)",
	orange: "bg-orange-900/30 text-orange-400",
	custom: "",
};

interface BadgeProps {
	variant?: BadgeVariant;
	className?: string;
	children: React.ReactNode;
}

/**
 * Compact pill badge for status indicators, log levels, and category labels.
 * Use `variant` for predefined color schemes or `className` for custom colors.
 */
export const Badge = memo(function Badge({
	variant = "info",
	className,
	children,
}: BadgeProps) {
	return (
		<span
			className={`inline-flex items-center px-1.5 py-0.5 text-[10px] rounded-full font-medium ${VARIANT_CLASSES[variant]}${className ? ` ${className}` : ""}`}
		>
			{children}
		</span>
	);
});
