import { memo } from "react";
import { useTranslation } from "react-i18next";
import { nonTextOutputs, outputKinds } from "../utils/model";
import { OUTPUT_META, type OutputMeta } from "./outputMeta";

/**
 * Renders one pill per known non-text output modality ("what this model
 * produces"). Text-only chat models render nothing, so the pill row stays
 * exactly as before for them.
 */
export const OutputBadges = memo(function OutputBadges({
	outputModalities,
}: {
	outputModalities?: string;
}) {
	const { t } = useTranslation();
	const outputs = nonTextOutputs({ output_modalities: outputModalities });
	const metas = OUTPUT_META.filter((m) => outputs.includes(m.key));
	if (metas.length === 0) return null;
	return (
		<>
			{metas.map((m) => (
				<span
					key={m.key}
					className={`ui-badge inline-flex items-center px-1.5 py-0.5 text-[11px] font-medium border ${m.style}`}
				>
					{t(m.labelKey)}
				</span>
			))}
		</>
	);
});

/**
 * An output modality's icon sized like a text pill: the icon scales with the
 * badge font, and a zero-width space gives the badge the same line box a
 * label would, so icon and text pills share one height in every UI style.
 * The badge's own horizontal padding is trimmed (!px: .ui-badge is unlayered
 * CSS and outranks plain utilities) to keep the icon badge square.
 */
export function OutputIcon({ meta }: { meta: OutputMeta }) {
	return (
		<>
			<meta.icon size="1.2em" aria-hidden />
			{"\u200b"}
		</>
	);
}

/** Base classes for an icon badge; callers add the meta's colour classes. */
export const OUTPUT_ICON_BADGE =
	"ui-badge ui-badge-icon inline-flex items-center border";

/**
 * The models tables' Output cell: one icon per declared output modality, text
 * included, so a model that cannot answer in text stands out by the missing
 * text icon.
 */
export const OutputIcons = memo(function OutputIcons({
	outputModalities,
}: {
	outputModalities?: string;
}) {
	const { t } = useTranslation();
	const outputs = outputKinds({ output_modalities: outputModalities });
	return (
		<div className="flex flex-wrap gap-1">
			{OUTPUT_META.filter((m) => outputs.includes(m.key)).map((m) => (
				<span
					key={m.key}
					role="img"
					title={t(m.labelKey)}
					aria-label={t(m.labelKey)}
					className={`${OUTPUT_ICON_BADGE} ${m.style}`}
				>
					<OutputIcon meta={m} />
				</span>
			))}
		</div>
	);
});
