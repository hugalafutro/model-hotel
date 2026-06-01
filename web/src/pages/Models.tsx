import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Bot } from "lucide-react";
import { useCallback, useState } from "react";
import { useTranslation } from "react-i18next";
import { api } from "../api/client";
import type { Model } from "../api/types";
import { LoadingSpinner } from "../components/LoadingSpinner";
import { ModelTable } from "../components/ModelTable";
import { PageHeader } from "../components/PageHeader";
import { VirtualModelTable } from "../components/VirtualModelTable";
import { useToast } from "../context/ToastContext";
import { useLocalStorage } from "../hooks/useLocalStorage";
import { countLabel } from "../utils/format";
import { ModelDetailModal } from "./Models/ModelDetailModal";

export function Models() {
	const { toast } = useToast();
	const { t } = useTranslation();
	const queryClient = useQueryClient();
	const [detailModel, setDetailModel] = useState<Model | null>(null);
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
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: ["models"] });
		},
		onError: (err: Error) => {
			toast(t("models.toast_update_failed", { message: err.message }), "error");
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
			const result = await api.providers.discover(providerId);
			queryClient.invalidateQueries({ queryKey: ["models"] });
			queryClient.invalidateQueries({ queryKey: ["providers"] });
			setModelRefreshTrigger((n) => n + 1);
			return result;
		},
		[queryClient],
	);

	const handleTest = useCallback(async (id: string) => {
		return api.models.test(id);
	}, []);

	const deleteMutation = useMutation({
		mutationFn: (id: string) => api.models.delete(id),
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: ["models"] });
			setModelRefreshTrigger((n) => n + 1);
			toast(t("models.toast_deleted"), "success");
		},
		onError: (err: Error) => {
			toast(t("models.toast_delete_failed", { message: err.message }), "error");
		},
	});

	const handleDeleteDisabled = useCallback(
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
			setModelRefreshTrigger((n) => n + 1);
			if (failed === 0) {
				toast(
					t("models.toast_delete_bulk_success", { count: ids.length }),
					"success",
				);
			} else {
				toast(
					t("models.toast_delete_bulk_warning", {
						kept: ids.length - failed,
						failed,
					}),
					"warning",
				);
			}
		},
		[queryClient, toast, t],
	);

	if (isLoading && viewMode === "paginate") {
		return <LoadingSpinner />;
	}

	const totalEnabled = models?.filter((m) => m.enabled).length ?? 0;
	const totalDisabled = (models?.length ?? 0) - totalEnabled;
	const allSameState = totalEnabled === 0 || totalDisabled === 0;

	const modelBadge =
		!allSameState && viewMode === "paginate" ? (
			<span className="inline-flex items-center gap-2 px-2.5 py-1 rounded-full text-xs font-medium bg-gray-700/60 border border-gray-600/50">
				<span className="text-green-400">
					{t("models.badge_enabled", { count: totalEnabled })}
				</span>
				<span className="text-gray-600">/</span>
				<span className="text-red-400">
					{t("models.badge_disabled", { count: totalDisabled })}
				</span>
			</span>
		) : undefined;

	return (
		<div className="space-y-4">
			<PageHeader
				icon={Bot}
				title={
					viewMode === "paginate"
						? countLabel(
								models?.length,
								t("models.page_title_one"),
								t("models.page_title_other"),
							)
						: countLabel(
								scrollTotal,
								t("models.page_title_one"),
								t("models.page_title_other"),
							)
				}
				description={t("models.page_description")}
				badge={modelBadge}
				actions={
					<button
						type="button"
						onClick={() =>
							setViewMode(viewMode === "scroll" ? "paginate" : "scroll")
						}
						className={`flex items-center gap-1 px-2 py-1.5 rounded-md text-xs font-medium transition-all border cursor-pointer ${
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
					onModelClick={setDetailModel}
					refreshTrigger={modelRefreshTrigger}
					onDeleteDisabled={handleDeleteDisabled}
					onTotalChange={setScrollTotal}
				/>
			) : (
				<ModelTable
					models={models ?? []}
					providers={providers}
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
