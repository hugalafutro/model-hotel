import { Spinner } from "../../components/Spinner";
import { AnimatedValue } from "./AnimatedValue";

export function StatCard({
	label,
	value,
	decimals,
	suffix,
	icon: Icon,
	accent,
	formatter,
	onClick,
	tooltip,
	loading,
}: {
	label: string;
	value: number;
	decimals?: number;
	suffix?: string;
	icon: React.ElementType;
	accent: string;
	formatter?: (val: number) => string;
	onClick?: () => void;
	tooltip?: string;
	loading?: boolean;
}) {
	return (
		// biome-ignore lint/a11y/noStaticElementInteractions: interactive only when onClick is provided, role/tabIndex/onKeyDown are set conditionally
		<div
			onClick={onClick}
			title={tooltip}
			className={`ui-card p-5 group text-left w-full ${onClick ? "cursor-pointer hover:brightness-110 transition-all" : ""}`}
			role={onClick ? "button" : undefined}
			tabIndex={onClick ? 0 : undefined}
			onKeyDown={
				onClick
					? (e) => {
							if (e.key === "Enter" || e.key === " ") {
								e.preventDefault();
								onClick();
							}
						}
					: undefined
			}
		>
			<div className="flex items-center justify-between mb-2">
				<div
					className="w-9 h-9 flex items-center justify-center rounded-lg"
					style={{ backgroundColor: `${accent}18` }}
				>
					<Icon size={18} style={{ color: accent }} />
				</div>
				<span
					className="font-semibold uppercase tracking-wider text-(--text-muted) text-right"
					style={{ fontSize: "clamp(8px, 0.55vw, 10px)" }}
				>
					{label}
				</span>
			</div>
			<p
				data-testid="stat-value"
				className="font-bold text-(--text-primary)"
				style={{ fontSize: "clamp(14px, 1.2vw, 20px)", textTransform: "none" }}
			>
				<AnimatedValue
					value={value}
					decimals={decimals}
					suffix={suffix}
					formatter={formatter}
				/>
				{loading && <Spinner className="ml-1" />}
			</p>
		</div>
	);
}
