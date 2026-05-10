import type { GenerationParams } from "../../api/types";
import { ApplyRecommendedButton } from "../../components/ApplyRecommendedButton";
import { Modal } from "../../components/Modal";
import { ParamSlider } from "../../components/ParamSlider";
import { providerFromModelID } from "../../utils/model";
import {
	getParamIncompatibility,
	isParamDisabled,
} from "../../utils/paramCompat";
import { hasAnyParam } from "../../utils/params";

export function ParamEditorModal({
	modelId,
	params,
	onChange,
	onClose,
	knownProviders,
}: {
	modelId: string;
	params: GenerationParams;
	onChange: (params: GenerationParams) => void;
	onClose: () => void;
	knownProviders: string[];
}) {
	const providerName = providerFromModelID(modelId, knownProviders);

	return (
		<Modal title={modelId} onClose={onClose} maxWidth="max-w-sm">
			<div className="space-y-4">
				<div className="space-y-3">
					<ParamSlider
						label="Temperature"
						value={params.temperature}
						min={0}
						max={2}
						step={0.01}
						disabled={isParamDisabled(providerName, "temperature")}
						disabledReason={
							getParamIncompatibility(providerName, "temperature") ?? undefined
						}
						onChange={(v) => onChange({ ...params, temperature: v })}
					/>
					<ParamSlider
						label="Max Tokens"
						value={params.max_tokens}
						min={1}
						max={32768}
						step={1}
						disabled={isParamDisabled(providerName, "max_tokens")}
						disabledReason={
							getParamIncompatibility(providerName, "max_tokens") ?? undefined
						}
						onChange={(v) =>
							onChange({
								...params,
								max_tokens: v === undefined ? undefined : Math.round(v),
							})
						}
					/>
					<ParamSlider
						label="Top P"
						value={params.top_p}
						min={0}
						max={1}
						step={0.01}
						disabled={isParamDisabled(providerName, "top_p")}
						disabledReason={
							getParamIncompatibility(providerName, "top_p") ?? undefined
						}
						onChange={(v) => onChange({ ...params, top_p: v })}
					/>
					<ParamSlider
						label="Min P"
						value={params.min_p}
						min={0}
						max={1}
						step={0.01}
						disabled={isParamDisabled(providerName, "min_p")}
						disabledReason={
							getParamIncompatibility(providerName, "min_p") ?? undefined
						}
						onChange={(v) => onChange({ ...params, min_p: v })}
					/>
					<ParamSlider
						label="Top K"
						value={params.top_k}
						min={1}
						max={100}
						step={1}
						disabled={isParamDisabled(providerName, "top_k")}
						disabledReason={
							getParamIncompatibility(providerName, "top_k") ?? undefined
						}
						onChange={(v) =>
							onChange({
								...params,
								top_k: v === undefined ? undefined : Math.round(v),
							})
						}
					/>
					<ParamSlider
						label="Freq Penalty"
						value={params.frequency_penalty}
						min={-2}
						max={2}
						step={0.01}
						disabled={isParamDisabled(providerName, "frequency_penalty")}
						disabledReason={
							getParamIncompatibility(providerName, "frequency_penalty") ??
							undefined
						}
						onChange={(v) => onChange({ ...params, frequency_penalty: v })}
					/>
					<ParamSlider
						label="Pres Penalty"
						value={params.presence_penalty}
						min={-2}
						max={2}
						step={0.01}
						disabled={isParamDisabled(providerName, "presence_penalty")}
						disabledReason={
							getParamIncompatibility(providerName, "presence_penalty") ??
							undefined
						}
						onChange={(v) => onChange({ ...params, presence_penalty: v })}
					/>
				</div>

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
							Reset all
						</button>
					)}
					<div />
					<button
						type="button"
						onClick={onClose}
						className="ui-btn ui-btn-primary text-xs px-3 py-1"
					>
						Done
					</button>
				</div>
			</div>
		</Modal>
	);
}
