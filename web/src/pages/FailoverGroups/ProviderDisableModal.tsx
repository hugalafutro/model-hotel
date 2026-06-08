import { useTranslation } from "react-i18next";
import { Modal } from "../../components/Modal";
import { Toggle } from "../../components/Toggle";

interface ProviderDisableModalProps {
	open: boolean;
	onClose: () => void;
	providers: { id: string; name: string }[];
	disabledProviders: Set<string>;
	onToggleProvider: (providerName: string, enabled: boolean) => void;
	isProcessing: boolean;
}

export function ProviderDisableModal({
	open,
	onClose,
	providers,
	disabledProviders,
	onToggleProvider,
	isProcessing,
}: ProviderDisableModalProps) {
	const { t } = useTranslation();

	if (!open) return null;

	return (
		<Modal
			onClose={onClose}
			title={t("failover.provider_modal_title")}
			maxWidth="max-w-lg"
		>
			<p className="text-sm text-gray-400 mb-4">
				{t("failover.provider_modal_description")}
			</p>
			<div className="max-h-96 overflow-y-auto space-y-2">
				{providers.map((provider) => (
					<div
						key={provider.id}
						className="flex items-center justify-between rounded px-3 py-2 hover:bg-gray-800/50"
					>
						<span className="font-mono text-sm">{provider.name}</span>
						<Toggle
							checked={!disabledProviders.has(provider.name)}
							onChange={(enabled) => onToggleProvider(provider.name, enabled)}
							disabled={isProcessing}
							ariaLabel={provider.name}
						/>
					</div>
				))}
			</div>
			<div className="mt-6 flex justify-end">
				<button
					type="button"
					onClick={onClose}
					className="ui-btn ui-btn-secondary text-sm px-4 py-2"
				>
					{t("common.close")}
				</button>
			</div>
		</Modal>
	);
}
