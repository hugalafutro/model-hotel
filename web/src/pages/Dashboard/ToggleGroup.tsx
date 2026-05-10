import type { MetricType, Range } from "./types";

export function RangeToggle({
	value,
	onChange,
}: {
	value: Range;
	onChange: (v: Range) => void;
}) {
	const labels: Record<Range, string> = { "1h": "1H", "24h": "1D", "7d": "7D" };
	return (
		<div className="flex items-center gap-px">
			{(["1h", "24h", "7d"] as Range[]).map((r) => {
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
	return (
		<div className="flex items-center gap-px">
			{(["tokens", "requests"] as MetricType[]).map((m) => {
				const active = value === m;
				const label = m === "tokens" ? "Tok" : "Req";
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
