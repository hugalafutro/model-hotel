import { useTranslation } from "react-i18next";
import type { GenerationParams } from "../api/types";
import {
	getParamIncompatibility,
	isParamDisabled,
	isParamHidden,
} from "../utils/paramCompat";
import { PARAM_SPECS } from "../utils/params";
import { ApplyRecommendedButton } from "./ApplyRecommendedButton";
import { ParamSlider } from "./ParamSlider";
import { ReasoningEffortSelect } from "./ReasoningEffortSelect";

/**
 * The generation-parameter editor: one slider per entry of PARAM_SPECS that the
 * provider does not hide, the reasoning-effort select for a reasoning model,
 * and the "apply recommended" button.
 *
 * `onChange` receives the whole parameter object, so a caller storing it
 * elsewhere replaces its copy rather than merging.
 */
export function GenerationParamSliders({
	modelId,
	provider,
	params,
	onChange,
	reasoning = false,
	sliderGap = "space-y-2",
}: {
	/** Model id used to look up recommended settings. */
	modelId: string;
	provider: string;
	params: GenerationParams;
	onChange: (params: GenerationParams) => void;
	/** True for a model that accepts a reasoning effort. */
	reasoning?: boolean;
	/** Vertical rhythm between sliders, which differs between the panel and the modal. */
	sliderGap?: string;
}) {
	const { t } = useTranslation();
	const incompatReason = (param: keyof GenerationParams) => {
		const key = getParamIncompatibility(provider, param);
		return key ? t(key) : undefined;
	};

	return (
		<>
			<div className={sliderGap}>
				{PARAM_SPECS.filter((spec) => !isParamHidden(provider, spec.key)).map(
					(spec) => (
						<ParamSlider
							key={spec.key}
							label={t(spec.labelKey)}
							value={params[spec.key]}
							min={spec.min}
							max={spec.max}
							step={spec.step}
							disabled={isParamDisabled(provider, spec.key)}
							disabledReason={incompatReason(spec.key)}
							onChange={(v) =>
								onChange({
									...params,
									[spec.key]:
										spec.integer && v !== undefined ? Math.round(v) : v,
								})
							}
						/>
					),
				)}
			</div>
			{reasoning && !isParamHidden(provider, "reasoning_effort") && (
				<ReasoningEffortSelect
					value={params.reasoning_effort}
					onChange={(v) => onChange({ ...params, reasoning_effort: v })}
				/>
			)}
			<ApplyRecommendedButton
				modelId={modelId}
				providerName={provider}
				onApply={(recommended) => onChange({ ...params, ...recommended })}
			/>
		</>
	);
}
