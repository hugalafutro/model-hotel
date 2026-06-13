import { useTranslation } from "react-i18next";
import { dropTrailingZero } from "../../utils/format";

export function Gauge({
	label,
	value,
	decimals,
	suffix,
	color,
	onClick,
	tooltip,
	maxScale,
	formatValue,
}: {
	label: string;
	value: number;
	decimals: number;
	suffix: string;
	color: string;
	onClick?: () => void;
	tooltip?: string;
	maxScale?: number;
	/** Custom display formatter for the value (e.g. compact 1.2K). When set,
	 * it owns the entire display string and `suffix` is not appended. */
	formatValue?: (value: number) => string;
}) {
	const { t } = useTranslation();
	const radius = 40;
	const circumference = 2 * Math.PI * radius;
	const pathArc = circumference / 2;
	// For percentage metrics (error rate), cap at 100. For absolute metrics
	// (requests, ms), scale relative to maxScale so the arc is meaningful.
	const scaleMax = maxScale ?? 100;
	const pct = Math.min(Math.max((value / scaleMax) * 100, 0), 100);
	const dashOffset = pathArc - (pathArc * pct) / 100;

	return (
		<button
			type="button"
			onClick={onClick}
			title={tooltip}
			className={`flex flex-col items-center ${onClick ? "hover:opacity-80 transition-opacity" : "cursor-default"}`}
		>
			<div className="relative w-28 h-14">
				<svg className="w-full h-full" viewBox="0 0 100 60">
					<title>{t("dashboard.gauge.svgTitle")}</title>
					<path
						className="gauge-arc"
						d="M 10 50 A 40 40 0 0 1 90 50"
						fill="none"
						stroke="var(--border-subtle)"
						strokeWidth="8"
						strokeLinecap="round"
					/>
					<path
						className="gauge-arc"
						d="M 10 50 A 40 40 0 0 1 90 50"
						fill="none"
						stroke={color}
						strokeWidth="8"
						strokeLinecap="round"
						strokeDasharray={pathArc}
						strokeDashoffset={dashOffset}
						style={{ transition: "stroke-dashoffset 1s ease-out" }}
					/>
				</svg>
				<div className="absolute inset-x-0 bottom-0 text-center">
					<p className="text-sm font-bold text-(--text-primary)">
						{formatValue ? (
							// formatValue owns the full display string (incl. any unit),
							// so suffix is intentionally not appended here.
							formatValue(value)
						) : (
							<>
								{dropTrailingZero(value, decimals)}
								{suffix}
							</>
						)}
					</p>
				</div>
			</div>
			<p className="text-[10px] uppercase tracking-wider text-(--text-secondary) mt-2">
				{label}
			</p>
		</button>
	);
}
