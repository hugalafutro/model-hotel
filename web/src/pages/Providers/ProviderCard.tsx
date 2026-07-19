import { useTranslation } from "react-i18next";
import type { Provider } from "../../api/types";
import { CopyablePill } from "../../components/CopyablePill";
import { QuotaBadges } from "../../components/QuotaBadge";
import { Spinner } from "../../components/Spinner";
import type { useQuotaData } from "../../hooks/useQuotaData";
import { formatTimestamp, formatTokens } from "../../utils/format";

interface ProviderCardProps {
	provider: Provider;
	modelCount: number;
	quotaData: ReturnType<typeof useQuotaData>;
	discoveringId: string | null;
	discoverAllCurrentId: string | null;
	discoverAllIsPending: boolean;
	onEdit: (provider: Provider) => void;
	onDiscover: (id: string) => void;
	onDelete: (provider: Provider) => void;
	onSetModelsProvider: (provider: Provider) => void;
	onSetModalNano: () => void;
	onSetModalZaiCoding: () => void;
	onSetModalKimiCode: () => void;
	onSetModalOpenRouter: () => void;
	onSetModalNeuralwatt: () => void;
	toast: (msg: string, type: "success" | "error" | "info") => void;
	/** When true this provider is managed by the fleet primary: edit and delete
	 * are hidden, since local changes are replaced on the next config sync. */
	managed?: boolean;
}

export function ProviderCard({
	provider,
	modelCount,
	quotaData,
	discoveringId,
	discoverAllCurrentId,
	discoverAllIsPending,
	onEdit,
	onDiscover,
	onDelete,
	onSetModelsProvider,
	onSetModalNano,
	onSetModalZaiCoding,
	onSetModalKimiCode,
	onSetModalOpenRouter,
	onSetModalNeuralwatt,
	toast,
	managed,
}: ProviderCardProps) {
	const { t } = useTranslation();

	return (
		<div
			className={`ui-card px-6 pt-5 pb-6 flex flex-col ${!provider.enabled ? "opacity-50" : ""} ${provider.enabled && !provider.autodiscovery_enabled ? "border-red-500/20 bg-red-500/[0.03]" : ""}`}
		>
			<div className="mb-2">
				<div className="flex items-center justify-between">
					<CopyablePill
						text={provider.name}
						displayText={provider.name}
						textClassName="text-lg font-semibold text-white"
						tooltip={t("providers.card_copy_name")}
					/>
				</div>
				<div className="flex flex-wrap items-center gap-2 mt-1">
					{!provider.enabled && (
						<span className="px-2 py-px leading-[1.6] text-xs font-medium ui-badge ui-badge-neutral">
							<span className="badge-text">{t("providers.card_disabled")}</span>
						</span>
					)}
					{provider.enabled && !provider.autodiscovery_enabled && (
						<span className="px-2 py-px leading-[1.6] text-xs font-medium ui-badge ui-badge-error">
							<span className="badge-text">
								{t("providers.card_autodiscovery_off")}
							</span>
						</span>
					)}
					{provider.total_tokens > 0 && (
						<span className="px-2 py-px leading-[1.6] text-xs font-medium whitespace-nowrap ui-badge ui-badge-purple">
							<span className="badge-text">
								{t("providers.card_tokens", {
									tokens: formatTokens(provider.total_tokens),
								})}
							</span>
						</span>
					)}
					{modelCount > 0 && (
						<button
							type="button"
							onClick={() => onSetModelsProvider(provider)}
							className="px-2 py-px leading-[1.6] text-xs font-medium hover:brightness-125 transition-colors whitespace-nowrap ui-badge ui-badge-cyan"
						>
							<span className="badge-text">
								{t("providers.card_models", { count: modelCount })}
							</span>
						</button>
					)}
					<span className="ml-auto inline-flex items-center gap-2">
						<QuotaBadges
							quotaData={quotaData}
							variant="card"
							providerBaseUrl={provider.base_url}
							onNanoClick={() => onSetModalNano()}
							onZaiCodingClick={() => onSetModalZaiCoding()}
							onKimiCodeClick={() => onSetModalKimiCode()}
							onDeepseekClick={async () => {
								try {
									await quotaData.refetchDeepseek();
									toast(t("providers.toast_quota_refreshed"), "success");
								} catch {
									toast(t("providers.toast_quota_refresh_failed"), "error");
								}
							}}
							onOpenRouterClick={() => onSetModalOpenRouter()}
							onOllamaCloudClick={async () => {
								try {
									await quotaData.refetchOllamaCloud();
									toast(t("providers.toast_account_refreshed"), "success");
								} catch {
									toast(t("providers.toast_account_refresh_failed"), "error");
								}
							}}
							onNeuralwattClick={() => onSetModalNeuralwatt()}
						/>
					</span>
				</div>
				<CopyablePill
					text={provider.base_url}
					textClassName="text-sm text-gray-400 font-mono"
					tooltip={t("providers.card_copy_url")}
				/>
			</div>

			<div className="space-y-2 text-sm">
				<div className="flex justify-between">
					<span className="text-gray-500">{t("providers.card_created")}</span>
					<span className="text-gray-300">
						{formatTimestamp(provider.created_at)}
					</span>
				</div>
				<div className="flex justify-between">
					<span className="text-gray-500">{t("providers.card_api_key")}</span>
					<span className="font-mono text-gray-300">{provider.masked_key}</span>
				</div>
				<div className="flex justify-between">
					<span className="text-gray-500">{t("providers.card_last_used")}</span>
					<span className="text-gray-300">
						{provider.last_used_at
							? formatTimestamp(provider.last_used_at)
							: t("common.n_a")}
					</span>
				</div>
				{provider.last_discovered_at && (
					<div className="flex justify-between">
						<span className="text-gray-500">
							{t("providers.card_last_discovery")}
						</span>
						<span className="text-gray-300">
							{formatTimestamp(provider.last_discovered_at)}
						</span>
					</div>
				)}
			</div>

			<div className="mt-auto pt-4 flex flex-wrap items-center justify-end gap-x-2 gap-y-3">
				<div className="flex gap-2">
					{!managed && (
						<button
							type="button"
							onClick={() => onEdit(provider)}
							className="ui-btn ui-btn-secondary"
						>
							{t("providers.card_btn_edit")}
						</button>
					)}
					<button
						type="button"
						onClick={() => onDiscover(provider.id)}
						disabled={
							discoveringId !== null ||
							discoverAllIsPending ||
							!provider.enabled ||
							!provider.autodiscovery_enabled
						}
						className={`px-3 py-1.5 text-xs border transition-all ui-btn ui-btn-primary ${
							discoveringId === provider.id ||
							discoverAllCurrentId === provider.id
								? "bg-(--accent-lighter) text-(--accent)/50 border-(--accent-light) cursor-not-allowed"
								: discoveringId !== null || discoverAllIsPending
									? "bg-gray-800/50 text-gray-600 border-gray-700/30 cursor-not-allowed"
									: !provider.enabled || !provider.autodiscovery_enabled
										? "bg-gray-800/50 text-gray-600 border-gray-700/30 cursor-not-allowed"
										: "bg-(--accent-light) text-(--accent) border-(--accent-lighter) hover:brightness-125"
						}`}
					>
						{discoveringId === provider.id ||
						discoverAllCurrentId === provider.id ? (
							<>
								<Spinner /> {t("providers.card_btn_discovering")}
							</>
						) : (
							t("providers.card_btn_discover")
						)}
					</button>
					{!managed && (
						<button
							type="button"
							onClick={() => onDelete(provider)}
							className="ui-btn ui-btn-danger"
						>
							{t("providers.card_btn_delete")}
						</button>
					)}
				</div>
			</div>
		</div>
	);
}
