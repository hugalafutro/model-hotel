import { useTranslation } from "react-i18next";

interface ReasoningEffortSelectProps {
	value: string | undefined;
	onChange: (v: string | undefined) => void;
}

export function ReasoningEffortSelect({
	value,
	onChange,
}: ReasoningEffortSelectProps) {
	const { t } = useTranslation();
	return (
		<div>
			<div className="flex items-center justify-between">
				<span className="text-[10px] uppercase tracking-wider text-(--text-tertiary)">
					{t("components.reasoningEffortSelect.reasoningEffort")}
				</span>
				{value !== undefined && (
					<button
						type="button"
						onClick={() => onChange(undefined)}
						className="text-[10px] text-red-400/80 hover:text-red-400 transition-colors"
					>
						{t("components.reasoningEffortSelect.off")}
					</button>
				)}
			</div>
			<div className="flex gap-1 mt-0.5">
				{[
					{ value: "low", label: t("components.reasoningEffortSelect.low") },
					{
						value: "medium",
						label: t("components.reasoningEffortSelect.medium"),
					},
					{ value: "high", label: t("components.reasoningEffortSelect.high") },
				].map((opt) => (
					<button
						key={opt.value}
						type="button"
						onClick={() =>
							onChange(value === opt.value ? undefined : opt.value)
						}
						className={`ui-tab flex-1 px-2 py-1 text-[10px] font-medium transition-all ${
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
