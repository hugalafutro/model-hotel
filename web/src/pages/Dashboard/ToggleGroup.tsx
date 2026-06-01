import { useTranslation } from "react-i18next";
import type { MetricType, Range } from "./types";

export function RangeToggle({
	value,
	onChange,
}: {
	value: Range;
	onChange: (v: Range) => void;
}) {
	const { t } = useTranslation();
	const labels: Record<Range, string> = {
		"1h": t("dashboard.range.1h"),
		"24h": t("dashboard.range.24h"),
		"1w": t("dashboard.range.1w"),
	};
	return (
		<div className="flex items-center gap-px">
			{(["1h", "24h", "1w"] as Range[]).map((r) => {
				const active = value === r;
				return (
					<button
						type="button"
						key={r}
						onClick={() => onChange(r)}
						className={`px-1 py-0.5 text-[10px] font-semibold rounded transition-colors ${
							active
								? "text-white"
								: "text-(--text-muted) hover:text-(--text-secondary)"
						}`}
						style={active ? { backgroundColor: "var(--accent)" } : {}}
					>
						{labels[r]}
					</button>
				);
			})}
		</div>
	);
}

export function MetricToggle({
	value,
	onChange,
}: {
	value: MetricType;
	onChange: (v: MetricType) => void;
}) {
	const { t } = useTranslation();
	return (
		<div className="flex items-center gap-px">
			{(["tokens", "requests"] as MetricType[]).map((m) => {
				const active = value === m;
				const label =
					m === "tokens"
						? t("dashboard.metric.tokens")
						: t("dashboard.metric.requests");
				return (
					<button
						type="button"
						key={m}
						onClick={() => onChange(m)}
						className={`px-1 py-0.5 text-[10px] font-semibold rounded transition-colors ${
							active
								? "text-white"
								: "text-(--text-muted) hover:text-(--text-secondary)"
						}`}
						style={active ? { backgroundColor: "var(--accent)" } : {}}
					>
						{label}
					</button>
				);
			})}
		</div>
	);
}
