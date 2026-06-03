import { useTranslation } from "react-i18next";
import type { Model, Provider } from "../api/types";
import { Modal } from "./Modal";
import { ModelTable } from "./ModelTable";

interface ProviderModelsModalProps {
	provider: Provider;
	models: Model[];
	onClose: () => void;
	/** When provided, shows a "Delete disabled" button for this provider's disabled models. */
	onDeleteDisabled?: (ids: string[]) => void;
}

export function ProviderModelsModal({
	provider,
	models,
	onClose,
	onDeleteDisabled,
}: ProviderModelsModalProps) {
	const { t } = useTranslation();
	// Filter models to only those belonging to this provider
	const providerModels = models.filter((m) => m.provider_id === provider.id);

	return (
		<Modal
			onClose={onClose}
			maxWidth="max-w-6xl"
			scrollable
			header={
				<div className="flex items-center gap-3">
					<h2 className="text-lg font-semibold text-white">{provider.name}</h2>
					<span className="px-2 py-px leading-[1.6] rounded-full bg-cyan-500/20 text-cyan-400 text-xs font-medium border border-cyan-500/30">
						{providerModels.length}{" "}
						{t("components.providerModelsModal.modelCount", {
							count: providerModels.length,
						})}
					</span>
				</div>
			}
		>
			<ModelTable models={providerModels} onDeleteDisabled={onDeleteDisabled} />
		</Modal>
	);
}
