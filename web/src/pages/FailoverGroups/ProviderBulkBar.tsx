import { useTranslation } from "react-i18next";

/**
 * Shown while a provider filter is set: how many groups it touches, and the
 * two buttons that enable or disable that provider's entries across all of
 * them.
 */
export function ProviderBulkBar({
	providerFilter,
	count,
	onToggle,
}: {
	providerFilter: string;
	count: number;
	onToggle: (enabled: boolean) => void;
}) {
	const { t } = useTranslation();
	return (
		<div className="flex items-center justify-between bg-gray-800/50 rounded-lg px-4 py-2 border border-gray-700">
			<span className="text-sm text-gray-300">
				{t("failover.bulk_provider_count", { count, provider: providerFilter })}
			</span>
			<div className="flex items-center gap-2">
				<button
					type="button"
					onClick={() => onToggle(true)}
					className="ui-btn ui-btn-secondary"
				>
					{t("failover.bulk_provider_enable", { provider: providerFilter })}
				</button>
				<button
					type="button"
					onClick={() => onToggle(false)}
					className="ui-btn ui-btn-secondary"
				>
					{t("failover.bulk_provider_disable", { provider: providerFilter })}
				</button>
			</div>
		</div>
	);
}
