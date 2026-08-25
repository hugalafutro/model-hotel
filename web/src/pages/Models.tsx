import {
	keepPreviousData,
	useMutation,
	useQuery,
	useQueryClient,
} from "@tanstack/react-query";
import { useCallback, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { Bot } from "@/lib/icons";
import { api } from "../api/client";
import type { Model } from "../api/types";
import { FilterDropdown } from "../components/FilterDropdown";
import { LoadingSpinner } from "../components/LoadingSpinner";
import { ModelTable } from "../components/ModelTable";
import { PageHeader } from "../components/PageHeader";
import { ViewModeToggle } from "../components/ViewModeToggle";
import {
	type ModelCounts,
	VirtualModelTable,
} from "../components/VirtualModelTable";
import { useToast } from "../context/ToastContext";
import { useLocalStorage } from "../hooks/useLocalStorage";
import { useRefreshDiscoveryBadge } from "../hooks/useRefreshDiscoveryBadge";
import { countLabel } from "../utils/format";
import { ModelDetailModal } from "./Models/ModelDetailModal";

/**
 * Which providers' models the page shows. "active" is the default because the
 * page exists to answer "what can the proxy serve right now": a disabled
 * provider keeps its rows (pins, prices, failover memberships survive a
 * re-enable) but /v1/models does not advertise them, so counting them here
 * would report models nobody can call. "disabled" shows only those parked
 * rows; "all" is the old unfiltered view.
 */
type ProviderScope = "active" | "disabled" | "all";

const PROVIDER_SCOPES: ProviderScope[] = ["active", "disabled", "all"];

function scopeToEnabled(scope: ProviderScope): boolean | undefined {
	if (scope === "all") return undefined;
	return scope === "active";
}

export function Models() {
	const { toast } = useToast();
	const { t } = useTranslation();
	const queryClient = useQueryClient();
	// Discovering and deleting both move the claim set behind the Models nav
	// badge, and the badge is a 60s poll that nothing here otherwise touches.
	// See useRefreshDiscoveryBadge.
	const refreshBadge = useRefreshDiscoveryBadge();
	const [detailModel, setDetailModel] = useState<Model | null>(null);
	const [providerFilter, setProviderFilter] = useState("");
	const [providerScope, setProviderScope] = useState<ProviderScope>("active");
	const providerEnabled = scopeToEnabled(providerScope);
	const [modelRefreshTrigger, setModelRefreshTrigger] = useState(0);
	// Scroll mode only has one page of rows, so the usable count comes from the
	// server alongside the row total (see VirtualModelTable.onTotalChange).
	const [scrollCounts, setScrollCounts] = useState<ModelCounts | undefined>(
		undefined,
	);
	const [viewMode, setViewMode] = useLocalStorage<"scroll" | "paginate">(
		"modelsViewMode",
		"scroll",
	);

	const { data: models, isLoading } = useQuery({
		queryKey: ["models", { providerEnabled }],
		queryFn: () => api.models.list(undefined, providerEnabled),
		enabled: viewMode === "paginate",
		// A scope change swaps the key; keep the old rows on screen instead of
		// unmounting the header (and the dropdown just clicked) behind a spinner.
		placeholderData: keepPreviousData,
	});

	const { data: allProviders } = useQuery({
		queryKey: ["providers"],
		queryFn: () => api.providers.list(),
	});

	// The provider dropdown follows the scope so it cannot offer a provider
	// whose rows the scope hides (an "active" view listing a disabled provider
	// would filter to an empty table with no explanation).
	const providers = useMemo(() => {
		if (allProviders === undefined || providerEnabled === undefined) {
			return allProviders;
		}
		return allProviders.filter((p) => p.enabled === providerEnabled);
	}, [allProviders, providerEnabled]);

	const handleScopeChange = useCallback(
		(value: string) => {
			const scope = value as ProviderScope;
			setProviderScope(scope);
			const enabled = scopeToEnabled(scope);
			const picked = allProviders?.find((p) => p.id === providerFilter);
			if (picked && enabled !== undefined && picked.enabled !== enabled) {
				setProviderFilter("");
			}
		},
		[allProviders, providerFilter],
	);

	const toggleMutation = useMutation({
		mutationFn: ({ id, enabled }: { id: string; enabled: boolean }) =>
			api.models.update(id, { enabled }),
		onError: (err: Error) => {
			toast(t("models.toast_update_failed", { message: err.message }), "error");
		},
		onSettled: () => {
			// Toggling reclassifies the claim: enabling a Gone model makes it
			// Suspect, and Suspect does not count towards the badge, while
			// disabling it by hand drops it from the claim set outright.
			//
			// Table and badge re-read together, so a rejected PATCH that landed
			// cannot leave one showing a state the other contradicts.
			queryClient.invalidateQueries({ queryKey: ["models"] });
			refreshBadge();
		},
	});

	const updateMutation = useMutation({
		mutationFn: ({ id, data }: { id: string; data: Record<string, unknown> }) =>
			api.models.update(id, data as Parameters<typeof api.models.update>[1]),
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: ["models"] });
			toast(t("models.toast_updated"), "success");
		},
		onError: (err: Error) => {
			toast(t("models.toast_update_failed", { message: err.message }), "error");
		},
	});

	const handleToggleModel = useCallback(
		(id: string, enabled: boolean) => {
			toggleMutation.mutate(
				{ id, enabled },
				{
					onSuccess: () => {
						toast(
							enabled
								? t("models.toast_toggle_enabled")
								: t("models.toast_toggle_disabled"),
							enabled ? "success" : "error",
						);
						setDetailModel((prev) => (prev ? { ...prev, enabled } : null));
						setModelRefreshTrigger((n) => n + 1);
					},
				},
			);
		},
		[toggleMutation, toast, t],
	);

	const handleUpdateModel = useCallback(
		(id: string, updates: Partial<Model>) => {
			updateMutation.mutate(
				{ id, data: updates },
				{
					onSuccess: () => {
						setDetailModel((prev) => (prev ? { ...prev, ...updates } : null));
						setModelRefreshTrigger((n) => n + 1);
					},
				},
			);
		},
		[updateMutation],
	);

	const handleDiscover = useCallback(
		async (providerId: string) => {
			try {
				return await api.providers.discover(providerId);
			} finally {
				// `finally`, like useDiscoveryRetest: a discovery run that errors
				// partway has still upserted whatever it reached, so table and badge
				// are re-read either way. The rejection still reaches the caller.
				queryClient.invalidateQueries({ queryKey: ["models"] });
				queryClient.invalidateQueries({ queryKey: ["providers"] });
				setModelRefreshTrigger((n) => n + 1);
				refreshBadge();
			}
		},
		[queryClient, refreshBadge],
	);

	const handleTest = useCallback(async (id: string) => {
		return api.models.test(id);
	}, []);

	const deleteMutation = useMutation({
		mutationFn: (id: string) => api.models.delete(id),
		onSuccess: () => {
			toast(t("models.toast_deleted"), "success");
		},
		onError: (err: Error) => {
			toast(t("models.toast_delete_failed", { message: err.message }), "error");
		},
		onSettled: () => {
			// A deleted model takes its claim with it, and a rejected DELETE can
			// still have landed — the response is what was lost. Table and badge
			// re-read together so neither can outlive the other's version of
			// what exists.
			queryClient.invalidateQueries({ queryKey: ["models"] });
			setModelRefreshTrigger((n) => n + 1);
			refreshBadge();
		},
	});

	const handleDeleteDisabled = useCallback(
		async (ids: string[]) => {
			try {
				// One atomic request instead of one DELETE per model: a concurrent
				// burst trips the admin IP rate limiter and reports spurious failures.
				const { deleted } = await api.models.bulkDelete(ids);
				queryClient.invalidateQueries({ queryKey: ["models"] });
				refreshBadge();
				setModelRefreshTrigger((n) => n + 1);
				toast(
					t("models.toast_delete_bulk_success", { count: deleted }),
					"success",
				);
			} catch (err) {
				// The bulk delete is one request, but a failure does not prove nothing
				// was deleted, so both paths re-read what the server now has.
				queryClient.invalidateQueries({ queryKey: ["models"] });
				refreshBadge();
				setModelRefreshTrigger((n) => n + 1);
				toast(
					t("models.toast_delete_failed", { message: (err as Error).message }),
					"error",
				);
			}
		},
		[queryClient, refreshBadge, toast, t],
	);

	if (isLoading && viewMode === "paginate") {
		return <LoadingSpinner />;
	}

	// The title answers "how many models can the proxy serve right now": a row
	// counts only when the model AND its provider are enabled, the /v1/models
	// rule. The badge splits the remainder into rows switched off individually
	// and rows parked under a disabled provider, so the rows in view add up and
	// the two states stay distinguishable.
	const counts: ModelCounts =
		viewMode === "paginate"
			? {
					total: models?.length ?? 0,
					enabled:
						models?.filter((m) => m.enabled && m.provider_enabled).length ?? 0,
					parked: models?.filter((m) => !m.provider_enabled).length ?? 0,
				}
			: (scrollCounts ?? { total: 0, enabled: 0, parked: 0 });
	const usableCount = counts.enabled;
	const disabledCount = Math.max(
		0,
		counts.total - counts.enabled - counts.parked,
	);
	const parkedCount = counts.parked;

	// The title row reads "N Models [remainder] [scope]": the scope picker sits
	// beside the badge, in the badge's box, so what the count covers and what
	// is being counted are read in one glance.
	const scopePicker = (
		<FilterDropdown
			value={providerScope}
			onChange={handleScopeChange}
			allowClear={false}
			variant="badge"
			options={PROVIDER_SCOPES.map((scope) => ({
				value: scope,
				label: t(`models.scope_${scope}`),
			}))}
			// Bounded so a long localized label truncates inside the picker
			// instead of pushing the non-wrapping title row sideways.
			className="max-w-[220px] min-w-0"
		/>
	);

	const modelBadge = (
		<>
			{(disabledCount > 0 || parkedCount > 0) && (
				<span className="inline-flex items-center gap-2 px-2.5 py-1 leading-[1.6] text-xs font-medium ui-badge ui-badge-neutral">
					{disabledCount > 0 && (
						<span className="text-red-400">
							<span className="badge-text">
								{t("models.badge_disabled", { count: disabledCount })}
							</span>
						</span>
					)}
					{disabledCount > 0 && parkedCount > 0 && (
						<span className="text-gray-600">/</span>
					)}
					{parkedCount > 0 && (
						<span
							className="text-gray-400"
							title={t("models.status_parked_hint")}
						>
							<span className="badge-text">
								{t("models.badge_parked", { count: parkedCount })}
							</span>
						</span>
					)}
				</span>
			)}
			{scopePicker}
		</>
	);

	return (
		<div className="space-y-4">
			<PageHeader
				icon={Bot}
				title={countLabel(usableCount, "models.page_title")}
				description={t("models.page_description")}
				badge={modelBadge}
				actions={
					<div className="flex items-center gap-2">
						<ViewModeToggle viewMode={viewMode} onChange={setViewMode} />
					</div>
				}
			/>

			{viewMode === "scroll" ? (
				<VirtualModelTable
					providers={providers}
					providerFilter={providerFilter}
					onProviderFilterChange={setProviderFilter}
					providerEnabled={providerEnabled}
					onModelClick={setDetailModel}
					refreshTrigger={modelRefreshTrigger}
					onDeleteDisabled={handleDeleteDisabled}
					onTotalChange={setScrollCounts}
				/>
			) : (
				<ModelTable
					models={models ?? []}
					providers={providers}
					providerFilter={providerFilter}
					onProviderFilterChange={setProviderFilter}
					onModelClick={setDetailModel}
					onDeleteDisabled={handleDeleteDisabled}
				/>
			)}

			{detailModel && (
				<ModelDetailModal
					model={detailModel}
					onClose={() => setDetailModel(null)}
					onToggle={handleToggleModel}
					onDiscover={handleDiscover}
					onTest={handleTest}
					onToast={toast}
					onUpdate={handleUpdateModel}
					onDelete={(id) => deleteMutation.mutate(id)}
				/>
			)}
		</div>
	);
}
