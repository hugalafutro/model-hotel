import { useTranslation } from "react-i18next";
import type { LucideIcon } from "@/lib/icons";
import { Activity, AlertTriangle } from "@/lib/icons";

interface StatusBadgeProps {
	code: number;
	state: string;
	errorMessage?: string;
	/**
	 * Renders at the plain `ui-badge` size instead of the roomier default, so
	 * the badge sits at the same height and text size as the `ui-badge` spans
	 * beside it (the attempt trail puts several on one line).
	 */
	compact?: boolean;
}

type BadgeVariant = "blue" | "red" | "green" | "orange";

const VARIANT_STYLES: Record<BadgeVariant, string> = {
	blue: "ui-badge-info",
	red: "ui-badge-error",
	green: "ui-badge-success",
	orange: "ui-badge-orange",
};

interface StatusDisplay {
	variant: BadgeVariant;
	label: string;
	icon: LucideIcon | null;
	animate: boolean;
}

/**
 * Badge look for a status code. Most codes are read by class (the leading
 * digit); 402 gets its own label because "Client Error" hides the one thing
 * an operator needs from it, that the account cannot pay for the request.
 */
function codeConfig(
	code: number,
): { variant: BadgeVariant; suffixKey: string; icon: LucideIcon } | null {
	if (code === 402) {
		return {
			variant: "orange",
			suffixKey: "components.statusBadge.paymentRequired",
			icon: AlertTriangle,
		};
	}
	switch (Math.floor(code / 100)) {
		case 2:
			return {
				variant: "green",
				suffixKey: "components.statusBadge.ok",
				icon: Activity,
			};
		case 4:
			return {
				variant: "orange",
				suffixKey: "components.statusBadge.clientError",
				icon: AlertTriangle,
			};
		case 5:
			return {
				variant: "red",
				suffixKey: "components.statusBadge.serverError",
				icon: AlertTriangle,
			};
		default:
			return null;
	}
}

function getStatusDisplay(
	code: number,
	state: string,
	t: (key: string) => string,
	errorMessage?: string,
): StatusDisplay | null {
	if (state === "pending" || state === "streaming") {
		return {
			variant: "blue",
			label:
				state === "streaming"
					? t("components.statusBadge.streaming")
					: t("components.statusBadge.pending"),
			icon: null,
			animate: true,
		};
	}

	if (code === 0) {
		const suffix = errorMessage ? `: ${errorMessage}` : "";
		return {
			variant: "red",
			label: `${t("components.statusBadge.failed")}${suffix}`,
			icon: AlertTriangle,
			animate: false,
		};
	}

	const config = codeConfig(code);
	if (config) {
		return {
			variant: config.variant,
			label: `${code} ${t(config.suffixKey)}`,
			icon: config.icon,
			animate: false,
		};
	}

	return null;
}

/**
 * statusBadgeLabel is the text the badge renders for a settled status, so a
 * caller can tell whether another string on the same row only repeats it.
 * Null when the code has no badge of its own.
 */
// eslint-disable-next-line react-refresh/only-export-components -- the label rule belongs beside the badge that renders it
export function statusBadgeLabel(
	code: number,
	t: (key: string) => string,
): string | null {
	return getStatusDisplay(code, "completed", t)?.label ?? null;
}

export function StatusBadge({
	code,
	state,
	errorMessage,
	compact,
}: StatusBadgeProps) {
	const { t } = useTranslation();
	const display = getStatusDisplay(code, state, t, errorMessage);
	if (!display) {
		return <span className="text-xs text-(--text-secondary)">{code}</span>;
	}

	const Icon = display.icon;
	// Compact drops the extra padding and leading so the badge is exactly as
	// tall as a bare ui-badge span; the icon shrinks to match the smaller box.
	const sizing = compact ? "gap-1" : "gap-1.5 px-2.5 py-1 leading-[1.6]";
	return (
		<span
			className={`ui-badge inline-flex items-center ${sizing} text-xs font-medium ${VARIANT_STYLES[display.variant]}`}
		>
			{display.animate ? (
				<span className="w-1.5 h-1.5 rounded-full bg-blue-400 animate-pulse" />
			) : (
				Icon && <Icon size={compact ? 11 : 12} />
			)}
			<span className="badge-text">{display.label}</span>
		</span>
	);
}
