import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowDownAZ, ArrowUpZA, PlugZap } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { api } from "../api/client";
import type { Provider } from "../api/types";
import { DeleteConfirmModal } from "../components/DeleteConfirmModal";
import { EmptyState } from "../components/EmptyState";
import { FilterDropdown } from "../components/FilterDropdown";
import { FilterInput } from "../components/FilterInput";
import { LoadingSpinner } from "../components/LoadingSpinner";
import { PageHeader } from "../components/PageHeader";
import {
	NanoGPTQuotaModal,
	OpenRouterQuotaModal,
	ZAICodingQuotaModal,
} from "../components/ProviderModals";
import { ProviderModelsModal } from "../components/ProviderModelsModal";
import { Spinner } from "../components/Spinner";
import { useQuotaModal } from "../context/QuotaModalContext";
import { useToast } from "../context/ToastContext";
import { useQuotaData } from "../hooks/useQuotaData";
import { countLabel } from "../utils/format";
import { AddProviderModal } from "./Providers/AddProviderModal";
import {
	getProviderType,
	providerTypeDisplayNames,
} from "./Providers/constants";
import { EditProviderModal } from "./Providers/EditProviderModal";
import { ProviderCard } from "./Providers/ProviderCard";

export function Providers() {
	const queryClient = useQueryClient();
	const { toast } = useToast();
	const [editProvider, setEditProvider] = useState<Provider | null>(null);
	const [deleteProvider, setDeleteProvider] = useState<Provider | null>(null);
	const [showModal, setShowModal] = useState(false);
	const [discoveringId, setDiscoveringId] = useState<string | null>(null);
	const [discoverAllCurrentId, setDiscoverAllCurrentId] = useState<
		string | null
	>(null);
	const [modelsProvider, setModelsProvider] = useState<Provider | null>(null);
	const [typeFilter, setTypeFilter] = useState("");
	const [nameFilter, setNameFilter] = useState("");
	const [sortAsc, setSortAsc] = useState(true);

	const { data: providers, isLoading } = useQuery({
		queryKey: ["providers"],
		queryFn: () => api.providers.list(),
	});

	const { data: models } = useQuery({
		queryKey: ["models"],
		queryFn: () => api.models.list(),
		staleTime: 60_000,
	});

	const { data: settings } = useQuery({
		queryKey: ["settings"],
		queryFn: () => api.settings.get(),
	});

	const modelCounts = useMemo(() => {
		const map = new Map<string, number>();
		if (models) {
			for (const m of models) {
				if (m.provider_name && m.enabled) {
					map.set(m.provider_name, (map.get(m.provider_name) || 0) + 1);
				}
			}
		}
		return map;
	}, [models]);

	const quotaData = useQuotaData(providers, { toastErrors: toast });

	const {
		nanogptUsage: modalNano,
		setNanogptUsage: setModalNano,
		zaiCodingUsage: modalZaiCoding,
		setZaiCodingUsage: setModalZaiCoding,
		openrouterBalance: modalOpenRouter,
		setOpenrouterBalance: setModalOpenRouter,
	} = useQuotaModal();

	// Track which provider is currently being scanned during Discover All
	useEffect(() => {
		const handler = (e: Event) => {
			const event = (e as CustomEvent).detail;
			if (
				event?.type === "request.discovery.provider_starting" &&
				event?.metadata?.provider_id
			) {
				setDiscoverAllCurrentId(event.metadata.provider_id as string);
			}
		};
		window.addEventListener("server-event", handler);
		return () => window.removeEventListener("server-event", handler);
	}, []);

	const discoverAllMutation = useMutation({
		mutationFn: async () => {
			return api.providers.discoverAll();
		},
		onSuccess: (data) => {
			queryClient.invalidateQueries({ queryKey: ["providers"] });
			queryClient.invalidateQueries({ queryKey: ["models"] });
			setDiscoverAllCurrentId(null);
			if (data.failed > 0 && data.succeeded === 0) {
				toast(`Discovery failed for all ${data.failed} providers`, "error");
			}
		},
		onError: (err: Error) => {
			toast(`Discover all failed: ${err.message}`, "error");
			setDiscoverAllCurrentId(null);
		},
	});

	const refreshQuotasMutation = useMutation({
		mutationFn: async () => {
			return api.providers.refreshQuotas();
		},
		onSuccess: (data) => {
			queryClient.invalidateQueries({ queryKey: ["providers"] });
			if (data.failed > 0) {
				toast(
					`Refreshed ${data.refreshed} quotas (${data.failed} failed, ${data.skipped} unsupported)`,
					"warning",
				);
			} else if (data.refreshed === 0) {
				toast("No providers with quota/balance support found", "info");
			} else {
				toast(`Refreshed ${data.refreshed} quotas/balances`, "success");
			}
		},
		onError: (err: Error) => {
			toast(`Refresh quotas failed: ${err.message}`, "error");
		},
	});

	const discoverMutation = useMutation({
		mutationFn: async (id: string) => {
			setDiscoveringId(id);
			return api.providers.discover(id);
		},
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: ["providers"] });
			queryClient.invalidateQueries({ queryKey: ["models"] });
		},
		onError: (err: Error) => {
			toast(`Discovery failed: ${err.message}`, "error");
		},
		onSettled: () => {
			setDiscoveringId(null);
		},
	});

	const deleteMutation = useMutation({
		mutationFn: (id: string) => api.providers.delete(id),
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: ["providers"] });
			queryClient.invalidateQueries({ queryKey: ["models"] });
			queryClient.invalidateQueries({ queryKey: ["nanogpt-usage"] });
			queryClient.invalidateQueries({ queryKey: ["zai-coding-usage"] });
			queryClient.invalidateQueries({ queryKey: ["deepseek-balance"] });
			queryClient.invalidateQueries({ queryKey: ["openrouter-balance"] });
			queryClient.invalidateQueries({ queryKey: ["failover-groups"] });
			toast("Provider deleted", "success");
		},
		onError: (err: Error) => {
			toast(`Failed to delete: ${err.message}`, "error");
		},
		onSettled: () => {
			setDeleteProvider(null);
		},
	});

	const typeOptions = useMemo(() => {
		if (!providers) return [];
		const typeCounts = new Map<string, number>();
		for (const p of providers) {
			const type = getProviderType(p.base_url);
			typeCounts.set(type, (typeCounts.get(type) || 0) + 1);
		}
		const entries = Array.from(typeCounts.entries());
		entries.sort((a, b) => {
			if (a[0] === "custom") return -1;
			if (b[0] === "custom") return 1;
			const labelA = providerTypeDisplayNames[a[0]] || a[0];
			const labelB = providerTypeDisplayNames[b[0]] || b[0];
			return labelA.localeCompare(labelB);
		});
		return entries.map(([type, count]) => ({
			value: type,
			label: providerTypeDisplayNames[type] || type,
			count,
		}));
	}, [providers]);

	const filteredProviders = useMemo(() => {
		if (!providers) return providers;
		const list = typeFilter
			? providers.filter((p) => getProviderType(p.base_url) === typeFilter)
			: providers;
		const nameFiltered = nameFilter
			? list.filter((p) =>
					p.name.toLowerCase().includes(nameFilter.toLowerCase()),
				)
			: list;
		return [...nameFiltered].sort((a, b) =>
			sortAsc ? a.name.localeCompare(b.name) : b.name.localeCompare(a.name),
		);
	}, [providers, typeFilter, nameFilter, sortAsc]);

	const allProvidersCount = providers?.length ?? 0;

	if (isLoading) {
		return <LoadingSpinner />;
	}

	return (
		<div className="space-y-6">
			<PageHeader
				icon={PlugZap}
				title={countLabel(allProvidersCount, "Provider", "Providers")}
				description="Manage your provider configurations"
				actions={
					<>
						<button
							type="button"
							onClick={() => discoverAllMutation.mutate()}
							disabled={discoverAllMutation.isPending || discoveringId !== null}
							className="ui-btn ui-btn-secondary"
						>
							{discoverAllMutation.isPending ? (
								<>
									<Spinner /> Discovering...
								</>
							) : (
								"Discover All Models"
							)}
						</button>
						<button
							type="button"
							onClick={() => refreshQuotasMutation.mutate()}
							disabled={refreshQuotasMutation.isPending}
							className="ui-btn ui-btn-secondary"
						>
							{refreshQuotasMutation.isPending ? (
								<>
									<Spinner /> Refreshing...
								</>
							) : (
								"Refresh Quotas/Balances"
							)}
						</button>
						<button
							type="button"
							onClick={() => setShowModal(true)}
							className="ui-btn ui-btn-primary"
						>
							+ Add Provider
						</button>
					</>
				}
			/>

			<div className="flex items-center justify-between gap-2">
				<FilterInput
					value={nameFilter}
					onChange={setNameFilter}
					placeholder="Filter providers…"
					className="w-[200px]"
					autoFocus
				/>
				<div className="flex items-center gap-2">
					<button
						type="button"
						onClick={() => setSortAsc((prev) => !prev)}
						title={
							sortAsc
								? "Sorted A-Z (click to reverse)"
								: "Sorted Z-A (click to reverse)"
						}
						className="p-1.5 rounded-md transition-all cursor-pointer text-(--text-tertiary) hover:text-(--accent) hover:drop-shadow-[var(--glow-accent)]"
					>
						{sortAsc ? <ArrowDownAZ size={16} /> : <ArrowUpZA size={16} />}
					</button>
					<FilterDropdown
						value={typeFilter}
						onChange={setTypeFilter}
						placeholder="Provider type"
						allLabel={`All (${allProvidersCount})`}
						options={typeOptions}
						className="w-44"
					/>
				</div>
			</div>

			<div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
				{filteredProviders?.map((provider) => (
					<ProviderCard
						key={provider.id}
						provider={provider}
						modelCount={modelCounts.get(provider.name) ?? 0}
						quotaData={quotaData}
						discoveringId={discoveringId}
						discoverAllCurrentId={discoverAllCurrentId}
						discoverAllIsPending={discoverAllMutation.isPending}
						onEdit={setEditProvider}
						onDiscover={(id) => discoverMutation.mutate(id)}
						onDelete={setDeleteProvider}
						onSetModelsProvider={setModelsProvider}
						onSetModalNano={(usage) => setModalNano(usage)}
						onSetModalZaiCoding={(usage) => setModalZaiCoding(usage)}
						onSetModalOpenRouter={(balance) => setModalOpenRouter(balance)}
						toast={toast}
					/>
				))}

				{filteredProviders?.length === 0 &&
					providers &&
					providers.length > 0 && (
						<div className="col-span-full">
							<EmptyState message="No providers match the selected filter." />
						</div>
					)}
				{providers?.length === 0 && (
					<div className="col-span-full">
						<EmptyState message="No providers configured. Add your first provider to get started." />
					</div>
				)}
			</div>

			{showModal && (
				<AddProviderModal
					onClose={() => setShowModal(false)}
					onToast={toast}
					settings={settings}
					providers={providers}
				/>
			)}

			{modalNano && (
				<NanoGPTQuotaModal
					usage={modalNano}
					onClose={() => setModalNano(null)}
					onRefresh={quotaData.refetchNano}
					isRefreshing={quotaData.isNanoRefetching}
					onToast={toast}
					lastRefreshed={quotaData.nanogptDataUpdatedAt}
				/>
			)}

			{modalZaiCoding && (
				<ZAICodingQuotaModal
					usage={modalZaiCoding}
					onClose={() => setModalZaiCoding(null)}
					onRefresh={quotaData.refetchZaiCoding}
					isRefreshing={quotaData.isZaiCodingRefetching}
					onToast={toast}
					lastRefreshed={quotaData.zaiCodingDataUpdatedAt}
				/>
			)}

			{modalOpenRouter && (
				<OpenRouterQuotaModal
					balance={modalOpenRouter}
					onClose={() => setModalOpenRouter(null)}
					onRefresh={quotaData.refetchOpenRouter}
					isRefreshing={quotaData.isOrRefetching}
					onToast={toast}
					lastRefreshed={quotaData.openrouterDataUpdatedAt}
				/>
			)}

			{editProvider && (
				<EditProviderModal
					provider={editProvider}
					onClose={() => setEditProvider(null)}
					onToast={toast}
				/>
			)}

			{modelsProvider && models && (
				<ProviderModelsModal
					provider={modelsProvider}
					models={models}
					onClose={() => setModelsProvider(null)}
				/>
			)}

			{deleteProvider && (
				<DeleteConfirmModal
					entityName={deleteProvider.name}
					entityType="provider"
					isPending={deleteMutation.isPending}
					onConfirm={() => deleteMutation.mutate(deleteProvider.id)}
					onCancel={() => setDeleteProvider(null)}
				/>
			)}
		</div>
	);
}
