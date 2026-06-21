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

	const sorted = [...providers].sort((a, b) => a.name.localeCompare(b.name));

	return (
		<Modal
			onClose={onClose}
			title={t("failover.provider_modal_title")}
			maxWidth="max-w-lg"
		>
			<p className="text-sm text-(--text-secondary) mb-4">
				{t("failover.provider_modal_description")}
			</p>
			<div className="max-h-96 overflow-y-auto space-y-2 pr-1">
				{sorted.map((provider) => {
					const enabled = !disabledProviders.has(provider.name);
					return (
						<div
							key={provider.id}
							className="flex items-center justify-between gap-3 px-3 py-2.5 ui-detail-tile"
						>
							<span className="flex items-center gap-2 min-w-0">
								<span
									className={`size-1.5 shrink-0 rounded-full ${enabled ? "bg-emerald-500" : "bg-(--text-tertiary)"}`}
								/>
								<span className="font-mono text-sm text-(--text-primary) truncate">
									{provider.name}
								</span>
							</span>
							<Toggle
								checked={enabled}
								onChange={(next) => onToggleProvider(provider.name, next)}
								disabled={isProcessing}
								ariaLabel={provider.name}
							/>
						</div>
					);
				})}
			</div>
		</Modal>
	);
}
