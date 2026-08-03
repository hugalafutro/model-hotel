import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useCallback, useState } from "react";
import { useTranslation } from "react-i18next";
import { Bot } from "@/lib/icons";
import { api } from "../api/client";
import type { Model } from "../api/types";
import { LoadingSpinner } from "../components/LoadingSpinner";
import { ModelTable } from "../components/ModelTable";
import { PageHeader } from "../components/PageHeader";
import { VirtualModelTable } from "../components/VirtualModelTable";
import { useToast } from "../context/ToastContext";
import { useLocalStorage } from "../hooks/useLocalStorage";
import { useRefreshDiscoveryBadge } from "../hooks/useRefreshDiscoveryBadge";
import { countLabel } from "../utils/format";
import { ModelDetailModal } from "./Models/ModelDetailModal";

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
	const [modelRefreshTrigger, setModelRefreshTrigger] = useState(0);
	const [scrollTotal, setScrollTotal] = useState<number | undefined>(undefined);
	const [viewMode, setViewMode] = useLocalStorage<"scroll" | "paginate">(
		"modelsViewMode",
		"scroll",
	);

	const { data: models, isLoading } = useQuery({
		queryKey: ["models"],
		queryFn: () => api.models.list(),
		enabled: viewMode === "paginate",
	});

	const { data: providers } = useQuery({
		queryKey: ["providers"],
		queryFn: () => api.providers.list(),
	});

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

	const totalEnabled = models?.filter((m) => m.enabled).length ?? 0;
	const totalDisabled = (models?.length ?? 0) - totalEnabled;
	const allSameState = totalEnabled === 0 || totalDisabled === 0;

	const modelBadge =
		!allSameState && viewMode === "paginate" ? (
			<span className="inline-flex items-center gap-2 px-2.5 py-1 leading-[1.6] text-xs font-medium ui-badge ui-badge-neutral">
				<span className="text-green-400">
					<span className="badge-text">
						{t("models.badge_enabled", { count: totalEnabled })}
					</span>
				</span>
				<span className="text-gray-600">/</span>
				<span className="text-red-400">
					<span className="badge-text">
						{t("models.badge_disabled", { count: totalDisabled })}
					</span>
				</span>
			</span>
		) : undefined;

	return (
		<div className="space-y-4">
			<PageHeader
				icon={Bot}
				title={countLabel(
					viewMode === "paginate" ? models?.length : scrollTotal,
					"models.page_title",
				)}
				description={t("models.page_description")}
				badge={modelBadge}
				actions={
					<button
						type="button"
						onClick={() =>
							setViewMode(viewMode === "scroll" ? "paginate" : "scroll")
						}
						className={`ui-tab flex items-center gap-1 px-2 py-1.5 text-xs font-medium transition-all border ${
							viewMode === "scroll"
								? "bg-(--accent)/20 text-(--accent) border-(--accent)/40"
								: "text-gray-400 border-gray-700 hover:text-white hover:border-gray-500"
						}`}
						title={
							viewMode === "scroll"
								? t("models.switch_to_pagination")
								: t("models.switch_to_scroll")
						}
						aria-label={
							viewMode === "scroll"
								? t("models.switch_to_pagination")
								: t("models.switch_to_scroll")
						}
					>
						{viewMode === "scroll"
							? t("models.view_mode_pages")
							: t("models.view_mode_scroll")}
					</button>
				}
			/>

			{viewMode === "scroll" ? (
				<VirtualModelTable
					providers={providers}
					providerFilter={providerFilter}
					onProviderFilterChange={setProviderFilter}
					onModelClick={setDetailModel}
					refreshTrigger={modelRefreshTrigger}
					onDeleteDisabled={handleDeleteDisabled}
					onTotalChange={setScrollTotal}
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
