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
	error: "ui-badge-error",
	warning: "ui-badge-warning",
	info: "ui-badge-info",
	success: "ui-badge-success",
	muted: "ui-badge-neutral",
	accent: "ui-badge-accent",
	orange: "ui-badge-orange",
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
			data-test-variant={variant}
			className={`inline-flex items-center whitespace-nowrap px-1.5 py-px leading-[1.6] text-[10px] font-medium ui-badge ${VARIANT_CLASSES[variant]}${className ? ` ${className}` : ""}`}
		>
			<span className="badge-text">{children}</span>
		</span>
	);
});
