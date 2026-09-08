import { useTranslation } from "react-i18next";
import type { LucideIcon } from "@/lib/icons";
import { Activity, AlertTriangle } from "@/lib/icons";

interface StatusBadgeProps {
	code: number;
	state: string;
	errorMessage?: string;
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

/** Badge look for a status class (the code's leading digit). */
function codeConfig(
	hundred: number,
): { variant: BadgeVariant; suffixKey: string; icon: LucideIcon } | null {
	switch (hundred) {
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

	const config = codeConfig(Math.floor(code / 100));
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

export function StatusBadge({ code, state, errorMessage }: StatusBadgeProps) {
	const { t } = useTranslation();
	const display = getStatusDisplay(code, state, t, errorMessage);
	if (!display) {
		return <span className="text-xs text-(--text-secondary)">{code}</span>;
	}

	const Icon = display.icon;
	return (
		<span
			className={`ui-badge inline-flex items-center gap-1.5 px-2.5 py-1 leading-[1.6] text-xs font-medium ${VARIANT_STYLES[display.variant]}`}
		>
			{display.animate ? (
				<span className="w-1.5 h-1.5 rounded-full bg-blue-400 animate-pulse" />
			) : (
				Icon && <Icon size={12} />
			)}
			<span className="badge-text">{display.label}</span>
		</span>
	);
}
