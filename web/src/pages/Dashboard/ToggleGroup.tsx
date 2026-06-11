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
		<div className="ui-tab-group">
			{(["1h", "24h", "1w"] as Range[]).map((r) => {
				const active = value === r;
				return (
					<button
						type="button"
						key={r}
						onClick={() => onChange(r)}
						className={`ui-tab px-1.5 py-px leading-[1.6] text-[10px] font-semibold transition-colors ${
							active
								? "text-white"
								: "text-(--text-muted) hover:text-(--text-secondary)"
						}`}
						style={active ? { backgroundColor: "var(--accent)" } : {}}
					>
						<span className="badge-text">{labels[r]}</span>
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
		<div className="ui-tab-group">
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
						className={`ui-tab px-1.5 py-px leading-[1.6] text-[10px] font-semibold transition-colors ${
							active
								? "text-white"
								: "text-(--text-muted) hover:text-(--text-secondary)"
						}`}
						style={active ? { backgroundColor: "var(--accent)" } : {}}
					>
						<span className="badge-text">{label}</span>
					</button>
				);
			})}
		</div>
	);
}
