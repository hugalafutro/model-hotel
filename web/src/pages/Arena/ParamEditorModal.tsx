import { useTranslation } from "react-i18next";
import type { GenerationParams } from "../../api/types";
import { GenerationParamSliders } from "../../components/GenerationParamSliders";
import { Modal } from "../../components/Modal";
import { providerFromModelID } from "../../utils/model";
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

	return (
		<Modal title={modelId} onClose={onClose} maxWidth="max-w-sm">
			<div className="space-y-4">
				<GenerationParamSliders
					modelId={modelId}
					provider={providerName}
					params={params}
					onChange={onChange}
					reasoning={reasoning}
					sliderGap="space-y-3"
				/>

				<div className="flex items-center justify-between pt-2">
					{hasAnyParam(params) && (
						<button
							type="button"
							onClick={() => onChange({})}
							className="text-[11px] text-red-400 hover:text-red-300 transition-colors"
						>
							{t("arena.params.resetAll")}
						</button>
					)}
					<div />
					<button
						type="button"
						onClick={onClose}
						className="ui-btn ui-btn-primary"
					>
						{t("arena.params.done")}
					</button>
				</div>
			</div>
		</Modal>
	);
}
