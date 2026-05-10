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
	onSetModalNano: (
		usage: NonNullable<ReturnType<typeof useQuotaData>["nanogptUsage"]>,
	) => void;
	onSetModalZaiCoding: (
		usage: NonNullable<ReturnType<typeof useQuotaData>["zaiCodingUsage"]>,
	) => void;
	onSetModalOpenRouter: (
		balance: NonNullable<ReturnType<typeof useQuotaData>["openrouterBalance"]>,
	) => void;
	toast: (msg: string, type: "success" | "error" | "info") => void;
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
	onSetModalOpenRouter,
	toast,
}: ProviderCardProps) {
	return (
		<div
			className={`ui-card p-6 flex flex-col ${!provider.enabled ? "opacity-50" : ""}`}
		>
			<div className="mb-4">
				<div className="flex items-center justify-between">
					<CopyablePill
						text={provider.name}
						displayText={provider.name}
						textClassName="text-lg font-semibold text-white"
						tooltip="Click to copy provider name"
					/>
				</div>
				<div className="flex items-center gap-2 mt-1">
					{!provider.enabled && (
						<span className="px-2 py-0.5 rounded-full bg-gray-600/40 text-gray-400 text-xs font-medium border border-gray-600/50">
							Disabled
						</span>
					)}
					{provider.total_tokens > 0 && (
						<span className="px-2 py-0.5 rounded-full bg-purple-500/20 text-purple-400 text-xs font-medium border border-purple-500/30 whitespace-nowrap">
							{formatTokens(provider.total_tokens)} tokens
						</span>
					)}
					{modelCount > 0 && (
						<button
							type="button"
							onClick={() => onSetModelsProvider(provider)}
							className="px-2 py-0.5 rounded-full bg-cyan-500/20 text-cyan-400 text-xs font-medium border border-cyan-500/30 cursor-pointer hover:bg-cyan-500/30 hover:border-cyan-400/50 transition-colors whitespace-nowrap"
						>
							{modelCount} {modelCount === 1 ? "model" : "models"}
						</button>
					)}
				</div>
				<CopyablePill
					text={provider.base_url}
					textClassName="text-sm text-gray-400 font-mono"
					tooltip="Click to copy API base URL"
				/>
			</div>

			<div className="space-y-2 text-sm">
				<div className="flex justify-between">
					<span className="text-gray-500">Created</span>
					<span className="text-gray-300">
						{formatTimestamp(provider.created_at)}
					</span>
				</div>
				<div className="flex justify-between">
					<span className="text-gray-500">API Key</span>
					<span className="font-mono text-gray-300">{provider.masked_key}</span>
				</div>
				<div className="flex justify-between">
					<span className="text-gray-500">Last Used</span>
					<span className="text-gray-300">
						{provider.last_used_at
							? formatTimestamp(provider.last_used_at)
							: "N/A"}
					</span>
				</div>
				{provider.last_discovered_at && (
					<div className="flex justify-between">
						<span className="text-gray-500">Last Discovery</span>
						<span className="text-gray-300">
							{formatTimestamp(provider.last_discovered_at)}
						</span>
					</div>
				)}
			</div>

			<div className="mt-auto pt-4 flex flex-wrap items-center justify-between gap-x-2 gap-y-3">
				<div className="flex items-center gap-2 min-h-7">
					<QuotaBadges
						quotaData={quotaData}
						variant="card"
						providerBaseUrl={provider.base_url}
						onNanoClick={() =>
							quotaData.nanogptUsage && onSetModalNano(quotaData.nanogptUsage)
						}
						onZaiCodingClick={() =>
							quotaData.zaiCodingUsage &&
							onSetModalZaiCoding(quotaData.zaiCodingUsage)
						}
						onDeepseekClick={async () => {
							try {
								await quotaData.refetchDeepseek();
								toast("Balance refreshed", "success");
							} catch {
								toast("Failed to refresh balance", "error");
							}
						}}
						onOpenRouterClick={() =>
							quotaData.openrouterBalance &&
							onSetModalOpenRouter(quotaData.openrouterBalance)
						}
						onOllamaCloudClick={async () => {
							try {
								await quotaData.refetchOllamaCloud();
								toast("Account info refreshed", "success");
							} catch {
								toast("Failed to refresh account info", "error");
							}
						}}
					/>
				</div>
				<div className="flex gap-2">
					<button
						type="button"
						onClick={() => onEdit(provider)}
						className="ui-btn ui-btn-secondary"
					>
						Edit
					</button>
					<button
						type="button"
						onClick={() => onDiscover(provider.id)}
						disabled={discoveringId !== null || discoverAllIsPending}
						className={`px-3 py-1.5 text-xs rounded-full border transition-all ${
							discoveringId === provider.id ||
							discoverAllCurrentId === provider.id
								? "bg-(--accent-lighter) text-(--accent)/50 border-(--accent-light) cursor-not-allowed"
								: discoveringId !== null || discoverAllIsPending
									? "bg-gray-800/50 text-gray-600 border-gray-700/30 cursor-not-allowed"
									: "bg-(--accent-light) text-(--accent) border-(--accent-lighter) cursor-pointer hover:brightness-125"
						}`}
					>
						{discoveringId === provider.id ||
						discoverAllCurrentId === provider.id ? (
							<>
								<Spinner /> Discovering...
							</>
						) : (
							"Discover Models"
						)}
					</button>
					<button
						type="button"
						onClick={() => onDelete(provider)}
						className="ui-btn ui-btn-danger"
					>
						Delete
					</button>
				</div>
			</div>
		</div>
	);
}
