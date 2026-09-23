import { useTranslation } from "react-i18next";

interface ReasoningEffortSelectProps {
	value: string | undefined;
	onChange: (v: string | undefined) => void;
}

// Default and None are different requests, not two names for the same one.
// Default omits reasoning_effort so the provider applies its own policy, which
// for a thinking model means it keeps thinking. None sends "none", which the
// egress translators turn into an explicit off switch: no thinking block on
// Anthropic, reasoning.effort "none" on the Responses API, a zero thinking
// budget on Vertex. Selecting Default is therefore the only way back to
// "whatever the model would do on its own".
//
// Default also stays the safe choice on a model that rejects the literal
// "none". The value is forwarded unvalidated on the provider types that do not
// strip reasoning_effort (openai, xai among them), so an o3-era or grok-mini
// model answers a None request with a 400 naming its supported values, and
// Default is the way back. The hints below say which button does what, since
// the difference is the whole point of having two.
// Two rows rather than one of five. The panel is max-w-sm, which leaves about
// 55px of text per button across five, and the longest translations of Default
// need more than that ("Alapértelmezett", "Predeterminado", "За умовчанням").
// The split is also the honest grouping: whether to reason at all, then how
// hard. Each row keeps flex-1 so its own buttons stay even.
const EFFORT_ROWS: { value: string | undefined; labelKey: string }[][] = [
	[
		{ value: undefined, labelKey: "default" },
		{ value: "none", labelKey: "none" },
	],
	[
		{ value: "low", labelKey: "low" },
		{ value: "medium", labelKey: "medium" },
		{ value: "high", labelKey: "high" },
	],
];

// Only Default and None need explaining; the three levels say what they are.
const HINT_KEYS = new Set(["default", "none"]);

export function ReasoningEffortSelect({
	value,
	onChange,
}: ReasoningEffortSelectProps) {
	const { t } = useTranslation();
	return (
		<div>
			<span className="ui-overline">
				{t("components.reasoningEffortSelect.reasoningEffort")}
			</span>
			{EFFORT_ROWS.map((row) => (
				<div key={row[0].labelKey} className="flex gap-1 mt-0.5">
					{row.map((opt) => {
						const hint = HINT_KEYS.has(opt.labelKey)
							? t(`components.reasoningEffortSelect.${opt.labelKey}Hint`)
							: undefined;
						return (
							<button
								key={opt.labelKey}
								type="button"
								title={hint}
								// Selecting the active option again is a no-op rather than a
								// toggle back to Default: Default is its own button now, so a
								// toggle would make None and Default reachable by two paths and
								// leave no way to re-pick the level you are already on.
								onClick={() => onChange(opt.value)}
								aria-pressed={value === opt.value}
								className={`ui-tab flex-1 px-1.5 py-1 text-[10px] font-medium transition-all ${
									value === opt.value
										? "bg-(--accent) text-white shadow-[var(--glow-accent)]"
										: "bg-(--surface-hover) text-(--text-secondary) hover:bg-(--surface-hover)/80"
								}`}
							>
								{t(`components.reasoningEffortSelect.${opt.labelKey}`)}
							</button>
						);
					})}
				</div>
			))}
		</div>
	);
}
