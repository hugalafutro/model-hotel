import type { LucideIcon } from "lucide-react";
import { Activity, AlertTriangle } from "lucide-react";
import { useTranslation } from "react-i18next";

interface StatusBadgeProps {
	code: number;
	state: string;
	errorMessage?: string;
}

type BadgeVariant = "blue" | "red" | "green" | "orange";

const VARIANT_STYLES: Record<BadgeVariant, string> = {
	blue: "bg-blue-500/15 text-blue-400 border-blue-500/30",
	red: "bg-red-500/15 text-red-400 border-red-500/30",
	green: "bg-green-500/15 text-green-400 border-green-500/30",
	orange: "bg-orange-500/15 text-orange-400 border-orange-500/30",
};

interface StatusDisplay {
	variant: BadgeVariant;
	label: string;
	icon: LucideIcon | null;
	animate: boolean;
}

function getStatusDisplay(
	code: number,
	state: string,
	t: (key: string) => string,
	errorMessage?: string,
): StatusDisplay | null {
	const activeStates = new Set(["pending", "streaming"]);
	if (activeStates.has(state)) {
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

	const hundred = Math.floor(code / 100);
	const codeMap = new Map<
		number,
		{ variant: BadgeVariant; suffix: string; icon: LucideIcon }
	>([
		[
			2,
			{
				variant: "green",
				suffix: t("components.statusBadge.ok"),
				icon: Activity,
			},
		],
		[
			4,
			{
				variant: "orange",
				suffix: t("components.statusBadge.clientError"),
				icon: AlertTriangle,
			},
		],
		[
			5,
			{
				variant: "red",
				suffix: t("components.statusBadge.serverError"),
				icon: AlertTriangle,
			},
		],
	]);

	const config = codeMap.get(hundred);
	if (config) {
		return {
			variant: config.variant,
			label: `${code} ${config.suffix}`,
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
			className={`ui-badge inline-flex items-center gap-1.5 px-2.5 py-1 leading-[1.6] text-xs font-medium border ${VARIANT_STYLES[display.variant]}`}
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
