import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowDownAZ, ArrowUpZA, PlugZap } from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
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
	NeuralWattQuotaModal,
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
	providerTypeTranslationKeys,
} from "./Providers/constants";
import {
	type DiscoverySummaryEntry,
	DiscoverySummaryModal,
} from "./Providers/DiscoverySummaryModal";
import { EditProviderModal } from "./Providers/EditProviderModal";
import { ProviderCard } from "./Providers/ProviderCard";

export function Providers() {
	const queryClient = useQueryClient();
	const { toast } = useToast();
	const { t } = useTranslation();
	const [editProvider, setEditProvider] = useState<Provider | null>(null);
	const [deleteProvider, setDeleteProvider] = useState<Provider | null>(null);
	const [showModal, setShowModal] = useState(false);
	const [discoveringId, setDiscoveringId] = useState<string | null>(null);
	const [discoverAllCurrentId, setDiscoverAllCurrentId] = useState<
		string | null
	>(null);
	const [modelsProvider, setModelsProvider] = useState<Provider | null>(null);
	// Post-scan summary for manually triggered discovery runs only; scheduled
	// or background discovery must never pop modals (SSE events cover those).
	const [discoverySummary, setDiscoverySummary] = useState<
		DiscoverySummaryEntry[] | null
	>(null);
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
				if (m.provider_name) {
					map.set(m.provider_name, (map.get(m.provider_name) || 0) + 1);
				}
			}
		}
		return map;
	}, [models]);

	const quotaData = useQuotaData(providers, { toastErrors: toast });

	const {
		isNanoOpen,
		setNanoOpen,
		isZaiCodingOpen,
		setZaiCodingOpen,
		isOpenRouterOpen,
		setOpenRouterOpen,
		isNeuralwattOpen,
		setNeuralwattOpen,
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
				toast(
					t("providers.toast_discovery_failed_all", { failed: data.failed }),
					"error",
				);
			}
			if (data.results.length > 0) {
				setDiscoverySummary(
					data.results.map((r) => ({
						providerName: r.provider_name,
						diff: r.diff,
						error: r.error,
					})),
				);
			}
		},
		onError: (err: Error) => {
			toast(
				t("providers.toast_discover_failed", { message: err.message }),
				"error",
			);
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
					t("providers.toast.refreshedQuotas", {
						refreshed: data.refreshed,
						failed: data.failed,
						skipped: data.skipped,
					}),
					"warning",
				);
			} else if (data.refreshed === 0) {
				toast(t("providers.toast_refresh_none"), "info");
			} else {
				toast(
					t("providers.toast_refresh_success", { count: data.refreshed }),
					"success",
				);
			}
		},
		onError: (err: Error) => {
			toast(
				t("providers.toast_refresh_failed", { message: err.message }),
				"error",
			);
		},
	});

	const discoverMutation = useMutation({
		mutationFn: async (id: string) => {
			setDiscoveringId(id);
			return api.providers.discover(id);
		},
		onSuccess: (data, id) => {
			queryClient.invalidateQueries({ queryKey: ["providers"] });
			queryClient.invalidateQueries({ queryKey: ["models"] });
			const providerName = providers?.find((p) => p.id === id)?.name ?? id;
			setDiscoverySummary([{ providerName, diff: data.diff }]);
		},
		onError: (err: Error) => {
			toast(
				t("providers.toast_discover_failed", { message: err.message }),
				"error",
			);
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
			toast(t("providers.toast_provider_deleted"), "success");
		},
		onError: (err: Error) => {
			toast(
				t("providers.toast_delete_failed", { message: err.message }),
				"error",
			);
		},
		onSettled: () => {
			setDeleteProvider(null);
		},
	});

	const handleDeleteDisabledModels = useCallback(
		async (ids: string[]) => {
			let failed = 0;
			await Promise.all(
				ids.map((id) =>
					api.models.delete(id).catch(() => {
						failed++;
					}),
				),
			);
			queryClient.invalidateQueries({ queryKey: ["models"] });
			if (failed === 0) {
				toast(
					t("providers.toast_delete_models_success", { count: ids.length }),
					"success",
				);
			} else {
				toast(
					t("providers.toast_delete_models_warning", {
						kept: ids.length - failed,
						failed,
					}),
					"warning",
				);
			}
		},
		[queryClient, toast, t],
	);

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
			return a[0].localeCompare(b[0]);
		});
		return entries.map(([type, count]) => ({
			value: type,
			label: t(providerTypeTranslationKeys[type] || `providers.type_${type}`),
			count,
		}));
	}, [providers, t]);

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
				title={countLabel(
					allProvidersCount,
					t("providers.page_title_one"),
					t("providers.page_title_other"),
				)}
				description={t("providers.page_description")}
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
									<Spinner /> {t("providers.btn_discovering")}
								</>
							) : (
								t("providers.btn_discover_all")
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
									<Spinner /> {t("providers.btn_refreshing")}
								</>
							) : (
								t("providers.btn_refresh_quotas")
							)}
						</button>
						<button
							type="button"
							onClick={() => setShowModal(true)}
							className="ui-btn ui-btn-primary"
						>
							+ {t("providers.btn_add_provider")}
						</button>
					</>
				}
			/>

			<div className="flex items-center justify-between gap-2">
				<FilterInput
					value={nameFilter}
					onChange={setNameFilter}
					placeholder={t("providers.filter_placeholder")}
					className="w-[200px]"
					autoFocus
				/>
				<div className="flex items-center gap-2">
					<button
						type="button"
						onClick={() => setSortAsc((prev) => !prev)}
						title={
							sortAsc
								? t("providers.sort_title_asc")
								: t("providers.sort_title_desc")
						}
						className="p-1.5 rounded-md transition-all cursor-pointer text-(--text-tertiary) hover:text-(--accent) hover:drop-shadow-[var(--glow-accent)]"
					>
						{sortAsc ? <ArrowDownAZ size={16} /> : <ArrowUpZA size={16} />}
					</button>
					<FilterDropdown
						value={typeFilter}
						onChange={setTypeFilter}
						placeholder={t("providers.filter_provider_type")}
						allLabel={t("providers.filter_all", { count: allProvidersCount })}
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
						onSetModalNano={() => setNanoOpen(true)}
						onSetModalZaiCoding={() => setZaiCodingOpen(true)}
						onSetModalOpenRouter={() => setOpenRouterOpen(true)}
						onSetModalNeuralwatt={() => setNeuralwattOpen(true)}
						toast={toast}
					/>
				))}

				{filteredProviders?.length === 0 &&
					providers &&
					providers.length > 0 && (
						<div className="col-span-full">
							<EmptyState message={t("providers.empty_no_match")} />
						</div>
					)}
				{providers?.length === 0 && (
					<div className="col-span-full">
						<EmptyState message={t("providers.empty_no_providers")} />
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

			{discoverySummary && (
				<DiscoverySummaryModal
					results={discoverySummary}
					onClose={() => setDiscoverySummary(null)}
				/>
			)}

			{isNanoOpen && quotaData.nanogptUsage && (
				<NanoGPTQuotaModal
					usage={quotaData.nanogptUsage}
					onClose={() => setNanoOpen(false)}
					onRefresh={quotaData.refetchNano}
					isRefreshing={quotaData.isNanoRefetching}
					onToast={toast}
					lastRefreshed={quotaData.nanogptDataUpdatedAt}
				/>
			)}

			{isZaiCodingOpen && quotaData.zaiCodingUsage && (
				<ZAICodingQuotaModal
					usage={quotaData.zaiCodingUsage}
					onClose={() => setZaiCodingOpen(false)}
					onRefresh={quotaData.refetchZaiCoding}
					isRefreshing={quotaData.isZaiCodingRefetching}
					onToast={toast}
					lastRefreshed={quotaData.zaiCodingDataUpdatedAt}
				/>
			)}

			{isOpenRouterOpen && quotaData.openrouterBalance && (
				<OpenRouterQuotaModal
					balance={quotaData.openrouterBalance}
					onClose={() => setOpenRouterOpen(false)}
					onRefresh={quotaData.refetchOpenRouter}
					isRefreshing={quotaData.isOrRefetching}
					onToast={toast}
					lastRefreshed={quotaData.openrouterDataUpdatedAt}
				/>
			)}

			{isNeuralwattOpen && quotaData.neuralwattQuota && (
				<NeuralWattQuotaModal
					quota={quotaData.neuralwattQuota}
					onClose={() => setNeuralwattOpen(false)}
					onRefresh={quotaData.refetchNeuralwatt}
					isRefreshing={quotaData.isNeuralwattRefetching}
					onToast={toast}
					lastRefreshed={quotaData.neuralwattDataUpdatedAt}
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
					onDeleteDisabled={handleDeleteDisabledModels}
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
