import { memo } from "react";
import { useTranslation } from "react-i18next";
import { nonTextOutputs } from "../utils/model";
import { OUTPUT_META } from "./outputMeta";

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
