import { useTranslation } from "react-i18next";
import type { GenerationParams } from "../../api/types";
import { ApplyRecommendedButton } from "../../components/ApplyRecommendedButton";
import { Modal } from "../../components/Modal";
import { ParamSlider } from "../../components/ParamSlider";
import { ReasoningEffortSelect } from "../../components/ReasoningEffortSelect";
import { providerFromModelID } from "../../utils/model";
import {
	getParamIncompatibility,
	isParamDisabled,
	isParamHidden,
} from "../../utils/paramCompat";
import { hasAnyParam } from "../../utils/params";

export function ParamEditorModal({
	modelId,
	params,
	onChange,
	onClose,
	knownProviders,
	reasoning,
}: {
	modelId: string;
	params: GenerationParams;
	onChange: (params: GenerationParams) => void;
	onClose: () => void;
	knownProviders: string[];
	reasoning?: boolean;
}) {
	const { t } = useTranslation();
	const providerName = providerFromModelID(modelId, knownProviders);
	const incompatReason = (prov: string, param: keyof GenerationParams) => {
		const key = getParamIncompatibility(prov, param);
		return key ? t(key) : undefined;
	};

	return (
		<Modal title={modelId} onClose={onClose} maxWidth="max-w-sm">
			<div className="space-y-4">
				<div className="space-y-3">
					{!isParamHidden(providerName, "temperature") && (
						<ParamSlider
							label={t("arena.params.temperature")}
							value={params.temperature}
							min={0}
							max={2}
							step={0.01}
							disabled={isParamDisabled(providerName, "temperature")}
							disabledReason={incompatReason(providerName, "temperature")}
							onChange={(v) => onChange({ ...params, temperature: v })}
						/>
					)}
					{!isParamHidden(providerName, "max_tokens") && (
						<ParamSlider
							label={t("arena.params.maxTokens")}
							value={params.max_tokens}
							min={1}
							max={32768}
							step={1}
							disabled={isParamDisabled(providerName, "max_tokens")}
							disabledReason={incompatReason(providerName, "max_tokens")}
							onChange={(v) =>
								onChange({
									...params,
									max_tokens: v === undefined ? undefined : Math.round(v),
								})
							}
						/>
					)}
					{!isParamHidden(providerName, "top_p") && (
						<ParamSlider
							label={t("arena.params.topP")}
							value={params.top_p}
							min={0}
							max={1}
							step={0.01}
							disabled={isParamDisabled(providerName, "top_p")}
							disabledReason={incompatReason(providerName, "top_p")}
							onChange={(v) => onChange({ ...params, top_p: v })}
						/>
					)}
					{!isParamHidden(providerName, "min_p") && (
						<ParamSlider
							label={t("arena.params.minP")}
							value={params.min_p}
							min={0}
							max={1}
							step={0.01}
							disabled={isParamDisabled(providerName, "min_p")}
							disabledReason={incompatReason(providerName, "min_p")}
							onChange={(v) => onChange({ ...params, min_p: v })}
						/>
					)}
					{!isParamHidden(providerName, "top_k") && (
						<ParamSlider
							label={t("arena.params.topK")}
							value={params.top_k}
							min={1}
							max={100}
							step={1}
							disabled={isParamDisabled(providerName, "top_k")}
							disabledReason={incompatReason(providerName, "top_k")}
							onChange={(v) =>
								onChange({
									...params,
									top_k: v === undefined ? undefined : Math.round(v),
								})
							}
						/>
					)}
					{!isParamHidden(providerName, "frequency_penalty") && (
						<ParamSlider
							label={t("arena.params.freqPenalty")}
							value={params.frequency_penalty}
							min={-2}
							max={2}
							step={0.01}
							disabled={isParamDisabled(providerName, "frequency_penalty")}
							disabledReason={incompatReason(providerName, "frequency_penalty")}
							onChange={(v) => onChange({ ...params, frequency_penalty: v })}
						/>
					)}
					{!isParamHidden(providerName, "presence_penalty") && (
						<ParamSlider
							label={t("arena.params.presPenalty")}
							value={params.presence_penalty}
							min={-2}
							max={2}
							step={0.01}
							disabled={isParamDisabled(providerName, "presence_penalty")}
							disabledReason={incompatReason(providerName, "presence_penalty")}
							onChange={(v) => onChange({ ...params, presence_penalty: v })}
						/>
					)}
				</div>
				{reasoning && !isParamHidden(providerName, "reasoning_effort") && (
					<ReasoningEffortSelect
						value={params.reasoning_effort}
						onChange={(v) => onChange({ ...params, reasoning_effort: v })}
					/>
				)}

				<ApplyRecommendedButton
					modelId={modelId}
					providerName={providerName}
					onApply={(recommended) => onChange({ ...params, ...recommended })}
				/>

				<div className="flex items-center justify-between pt-2">
					{hasAnyParam(params) && (
						<button
							type="button"
							onClick={() => onChange({})}
							className="text-[11px] text-red-400 hover:text-red-300 transition-colors cursor-pointer"
						>
							{t("arena.params.resetAll")}
						</button>
					)}
					<div />
					<button
						type="button"
						onClick={onClose}
						className="ui-btn ui-btn-primary text-xs px-3 py-1"
					>
						{t("arena.params.done")}
					</button>
				</div>
			</div>
		</Modal>
	);
}
