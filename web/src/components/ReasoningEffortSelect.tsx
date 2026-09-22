import { useTranslation } from "react-i18next";

interface ReasoningEffortSelectProps {
	value: string | undefined;
	onChange: (v: string | undefined) => void;
}

// Default and None are different requests, not two names for the same one.
// Default omits reasoning_effort so the provider applies its own policy, which
// for a thinking model means it keeps thinking. None sends "none", which the
// egress translators turn into an explicit off switch: a zero thinking budget
// on Gemini, no thinking block on Anthropic, reasoning.effort "none" on the
// Responses API. Selecting Default is therefore the only way back to "whatever
// the model would do on its own", and it stays the value that is sent as an
// absent field for a provider that would reject "none".
const EFFORTS: { value: string | undefined; labelKey: string }[] = [
	{ value: undefined, labelKey: "default" },
	{ value: "none", labelKey: "none" },
	{ value: "low", labelKey: "low" },
	{ value: "medium", labelKey: "medium" },
	{ value: "high", labelKey: "high" },
];

export function ReasoningEffortSelect({
	value,
	onChange,
}: ReasoningEffortSelectProps) {
	const { t } = useTranslation();
	return (
		<div>
			<span className="text-[10px] uppercase tracking-wider text-(--text-tertiary)">
				{t("components.reasoningEffortSelect.reasoningEffort")}
			</span>
			<div className="flex gap-1 mt-0.5">
				{EFFORTS.map((opt) => (
					<button
						key={opt.labelKey}
						type="button"
						// Selecting the active option again is a no-op rather than a
						// toggle back to Default: Default is its own button now, so a
						// toggle would make None and Default reachable by two paths and
						// leave no way to re-pick the level you are already on.
						onClick={() => onChange(opt.value)}
						className={`ui-tab flex-1 px-1.5 py-1 text-[10px] font-medium transition-all ${
							value === opt.value
								? "bg-(--accent) text-white shadow-[var(--glow-accent)]"
								: "bg-(--surface-hover) text-(--text-secondary) hover:bg-(--surface-hover)/80"
						}`}
					>
						{t(`components.reasoningEffortSelect.${opt.labelKey}`)}
					</button>
				))}
			</div>
		</div>
	);
}
