const OPTIONS = [
	{ value: "low", label: "Low" },
	{ value: "medium", label: "Medium" },
	{ value: "high", label: "High" },
] as const;

interface ReasoningEffortSelectProps {
	value: string | undefined;
	onChange: (v: string | undefined) => void;
}

export function ReasoningEffortSelect({
	value,
	onChange,
}: ReasoningEffortSelectProps) {
	return (
		<div>
			<div className="flex items-center justify-between">
				<span className="text-[10px] uppercase tracking-wider text-(--text-tertiary)">
					Reasoning Effort
				</span>
				{value !== undefined && (
					<button
						type="button"
						onClick={() => onChange(undefined)}
						className="text-[10px] text-red-400/80 hover:text-red-400 transition-colors cursor-pointer"
					>
						off
					</button>
				)}
			</div>
			<div className="flex gap-1 mt-0.5">
				{OPTIONS.map((opt) => (
					<button
						key={opt.value}
						type="button"
						onClick={() =>
							onChange(value === opt.value ? undefined : opt.value)
						}
						className={`flex-1 px-2 py-1 rounded text-[10px] font-medium transition-all cursor-pointer ${
							value === opt.value
								? "bg-(--accent) text-white shadow-[var(--glow-accent)]"
								: "bg-(--surface-hover) text-(--text-secondary) hover:bg-(--surface-hover)/80"
						}`}
					>
						{opt.label}
					</button>
				))}
			</div>
		</div>
	);
}
