import { useTranslation } from "react-i18next";
import {
	DASHBOARD_RANGES,
	METRIC_TYPES,
	type MetricType,
	type Range,
} from "./types";

// ToggleGroup is the shared button-group shell behind the dashboard's small
// segmented toggles. It is generic over the option type so each toggle only has
// to supply its options, the selected value, and a label lookup; the markup and
// active/inactive styling stay in one place.
function ToggleGroup<T extends string>({
	options,
	value,
	onChange,
	getLabel,
	getTitle,
}: {
	options: readonly T[];
	value: T;
	onChange: (v: T) => void;
	getLabel: (v: T) => string;
	/** The full word behind an abbreviated label: hover text and accessible name. */
	getTitle?: (v: T) => string;
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
						title={getTitle?.(opt)}
						aria-label={getTitle?.(opt)}
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
			options={DASHBOARD_RANGES}
			value={value}
			onChange={onChange}
			getLabel={(r) => labels[r]}
		/>
	);
}

export function MetricToggle({
	value,
	onChange,
	exclude,
}: {
	value: MetricType;
	onChange: (v: MetricType) => void;
	/**
	 * A metric this toggle must not offer, for two toggles that share one
	 * space: the option leaves the group instead of rendering disabled.
	 */
	exclude?: MetricType;
}) {
	const { t } = useTranslation();
	// One character each, the same in every language; the word rides the
	// tooltip. Spend is the dollar sign because every price is in US dollars.
	const labels: Record<MetricType, string> = {
		tokens: "T",
		requests: "R",
		cost: "$",
	};
	const titles: Record<MetricType, string> = {
		tokens: t("dashboard.label.tokens"),
		requests: t("dashboard.label.requests"),
		// The visible "$" is not part of the word, so the name carries both:
		// voice control can then say either.
		cost: `${t("dashboard.label.spend")} ($)`,
	};
	return (
		<ToggleGroup
			options={
				exclude === undefined
					? METRIC_TYPES
					: METRIC_TYPES.filter((m) => m !== exclude)
			}
			value={value}
			onChange={onChange}
			getLabel={(m) => labels[m]}
			getTitle={(m) => titles[m]}
		/>
	);
}
