import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Bot } from "lucide-react";
import { useCallback, useState } from "react";
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
	const queryClient = useQueryClient();
	const [detailModel, setDetailModel] = useState<Model | null>(null);
	const [viewMode, setViewMode] = useLocalStorage<"scroll" | "paginate">(
		"modelsViewMode",
		"scroll",
	);

	const { data: models, isLoading } = useQuery({
		queryKey: ["models"],
		queryFn: () => api.models.list(),
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
			toast(`Failed to update model: ${err.message}`, "error");
		},
	});

	const updateMutation = useMutation({
		mutationFn: ({ id, data }: { id: string; data: Record<string, unknown> }) =>
			api.models.update(id, data as Parameters<typeof api.models.update>[1]),
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: ["models"] });
			toast("Model updated", "success");
		},
		onError: (err: Error) => {
			toast(`Failed to update model: ${err.message}`, "error");
		},
	});

	const handleToggleModel = useCallback(
		(id: string, enabled: boolean) => {
			toggleMutation.mutate(
				{ id, enabled },
				{
					onSuccess: () => {
						toast(
							enabled ? "Model enabled" : "Model disabled",
							enabled ? "success" : "error",
						);
						setDetailModel((prev) => (prev ? { ...prev, enabled } : null));
					},
				},
			);
		},
		[toggleMutation, toast],
	);

	const handleUpdateModel = useCallback(
		(id: string, updates: Partial<Model>) => {
			updateMutation.mutate(
				{ id, data: updates },
				{
					onSuccess: () => {
						setDetailModel((prev) => (prev ? { ...prev, ...updates } : null));
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
			toast("Model deleted successfully", "success");
		},
		onError: (err: Error) => {
			toast(`Failed to delete model: ${err.message}`, "error");
		},
	});

	if (isLoading) {
		return <LoadingSpinner />;
	}

	const totalEnabled = models?.filter((m) => m.enabled).length ?? 0;
	const totalDisabled = (models?.length ?? 0) - totalEnabled;
	const allSameState = totalEnabled === 0 || totalDisabled === 0;

	const modelBadge = !allSameState ? (
		<span className="inline-flex items-center gap-2 px-2.5 py-1 rounded-full text-xs font-medium bg-gray-700/60 border border-gray-600/50">
			<span className="text-green-400">{totalEnabled} enabled</span>
			<span className="text-gray-600">/</span>
			<span className="text-red-400">{totalDisabled} disabled</span>
		</span>
	) : undefined;

	return (
		<div className="space-y-4">
			<PageHeader
				icon={Bot}
				title={countLabel(models?.length, "Model", "Models")}
				description="Discovered models from your providers"
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
								? "Switch to pagination mode"
								: "Switch to scroll mode"
						}
						aria-label={
							viewMode === "scroll"
								? "Switch to pagination mode"
								: "Switch to scroll mode"
						}
					>
						{viewMode === "scroll" ? "⬡ Pages" : "⇊ Scroll"}
					</button>
				}
			/>

			{viewMode === "scroll" ? (
				<VirtualModelTable
					providers={providers}
					onModelClick={setDetailModel}
				/>
			) : (
				<ModelTable
					models={models ?? []}
					providers={providers}
					onModelClick={setDetailModel}
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
