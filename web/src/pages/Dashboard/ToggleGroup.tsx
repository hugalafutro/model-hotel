import { useTranslation } from "react-i18next";
import type { MetricType, Range } from "./types";

// ToggleGroup is the shared button-group shell behind the dashboard's small
// segmented toggles. It is generic over the option type so each toggle only has
// to supply its options, the selected value, and a label lookup; the markup and
// active/inactive styling stay in one place.
function ToggleGroup<T extends string>({
	options,
	value,
	onChange,
	getLabel,
}: {
	options: readonly T[];
	value: T;
	onChange: (v: T) => void;
	getLabel: (v: T) => string;
}) {
	return (
		<div className="flex items-center gap-px">
			{options.map((opt) => {
				const active = value === opt;
				return (
					<button
						type="button"
						key={opt}
						onClick={() => onChange(opt)}
						className={`ui-tab px-1.5 py-px leading-[1.6] text-[10px] font-semibold transition-colors ${
							active
								? "ui-tab-active"
								: "text-(--text-muted) hover:text-(--text-secondary)"
						}`}
					>
						<span className="badge-text">{getLabel(opt)}</span>
					</button>
				);
			})}
		</div>
	);
}

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
		<ToggleGroup
			options={["1h", "24h", "1w"] as const}
			value={value}
			onChange={onChange}
			getLabel={(r) => labels[r]}
		/>
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
	const labels: Record<MetricType, string> = {
		tokens: t("dashboard.metric.tokens"),
		requests: t("dashboard.metric.requests"),
	};
	return (
		<ToggleGroup
			options={["tokens", "requests"] as const}
			value={value}
			onChange={onChange}
			getLabel={(m) => labels[m]}
		/>
	);
}
