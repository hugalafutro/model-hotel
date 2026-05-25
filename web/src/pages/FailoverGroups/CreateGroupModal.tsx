import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useMemo, useState } from "react";
import { api } from "../../api/client";
import type { CandidateModel, FailoverGroup } from "../../api/types";
import { Modal } from "../../components/Modal";
import type { ModelItem } from "../../components/ModelPicker";
import { ModelPicker } from "../../components/ModelPicker";
import { useToast } from "../../context/ToastContext";
import { proxyModelID } from "../../utils/model";

export function CreateGroupModal({
	candidates,
	group,
	onClose,
	onCreated,
	onUpdated,
}: {
	candidates: CandidateModel[];
	group?: FailoverGroup;
	onClose: () => void;
	onCreated?: () => void;
	onUpdated?: () => void;
}) {
	const { toast } = useToast();
	const queryClient = useQueryClient();
	const isEdit = !!group;

	const [displayModel, setDisplayModel] = useState(group?.display_model ?? "");
	const [displayName, setDisplayName] = useState(group?.display_name ?? "");
	const [description, setDescription] = useState(group?.description ?? "");

	// Map candidates to ModelItem format for ModelPicker
	// In edit mode, also include group entries whose providers are no longer in candidates
	// so they appear as selectable pills and aren't silently dropped on submit
	const modelItems = useMemo<ModelItem[]>(() => {
		const seen = new Set<string>();
		const items: ModelItem[] = [];
		for (const c of candidates) {
			const pid = proxyModelID(c.provider_name, c.model_id);
			if (!seen.has(pid)) {
				seen.add(pid);
				items.push({
					provider_name: c.provider_name,
					model_id: c.model_id,
					display_name: c.display_name || undefined,
				});
			}
		}
		if (group) {
			for (const e of group.entries) {
				const pid = proxyModelID(e.provider_name, e.model_id);
				if (!seen.has(pid)) {
					seen.add(pid);
					items.push({
						provider_name: e.provider_name,
						model_id: e.model_id,
						display_name: e.display_name || undefined,
					});
				}
			}
		}
		return items;
	}, [candidates, group]);

	// Build proxyID → model_uuid lookup for submission
	// Includes candidates AND group entries (edit mode) so unavailable providers aren't lost
	const proxyToUuid = useMemo(() => {
		const map = new Map<string, string>();
		for (const c of candidates) {
			map.set(proxyModelID(c.provider_name, c.model_id), c.model_uuid);
		}
		if (group) {
			for (const e of group.entries) {
				const pid = proxyModelID(e.provider_name, e.model_id);
				if (!map.has(pid)) {
					map.set(pid, e.model_uuid);
				}
			}
		}
		return map;
	}, [candidates, group]);

	// In edit mode, pre-select entries from the group
	const [selectedProxyIDs, setSelectedProxyIDs] = useState<string[]>(() => {
		if (!group) return [];
		return group.entries.map((e) => proxyModelID(e.provider_name, e.model_id));
	});

	const createMutation = useMutation({
		mutationFn: (data: {
			display_model: string;
			display_name?: string;
			description?: string;
			entry_ids: string[];
		}) => api.failoverGroups.create(data),
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: ["failover-groups"] });
			toast("Failover group created", "success");
			onCreated?.();
		},
		onError: (err: Error) => {
			toast(`Failed to create group: ${err.message}`, "error");
		},
	});

	const updateMutation = useMutation({
		mutationFn: (data: {
			id: string;
			body: Parameters<typeof api.failoverGroups.update>[1];
		}) => api.failoverGroups.update(data.id, data.body),
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: ["failover-groups"] });
			toast("Failover group updated", "success");
			onUpdated?.();
		},
		onError: (err: Error) => {
			toast(`Failed to update group: ${err.message}`, "error");
		},
	});

	const handleSubmit = (e: React.FormEvent) => {
		e.preventDefault();

		const entryUuids = selectedProxyIDs
			.map((id) => proxyToUuid.get(id))
			.filter((v): v is string => v !== undefined);

		if (isEdit) {
			if (entryUuids.length < 2) {
				toast("At least 2 entries required", "error");
				return;
			}
			updateMutation.mutate({
				id: group.id,
				body: {
					display_name: displayName.trim() || undefined,
					description: description.trim(),
					priority_order: entryUuids,
				},
			});
		} else {
			if (!displayModel.trim()) {
				toast("Display model name is required", "error");
				return;
			}
			if (entryUuids.length < 2) {
				toast("At least 2 entries required", "error");
				return;
			}
			createMutation.mutate({
				display_model: displayModel.trim(),
				display_name: displayName.trim() || undefined,
				description: description.trim() || undefined,
				entry_ids: entryUuids,
			});
		}
	};

	const isPending = createMutation.isPending || updateMutation.isPending;

	return (
		<Modal
			title={isEdit ? "Edit Failover Group" : "Create Failover Group"}
			onClose={onClose}
			maxWidth="max-w-lg"
			scrollable
		>
			<form onSubmit={handleSubmit} className="space-y-4">
				<div>
					<label
						htmlFor="display-model"
						className="block text-sm font-medium text-gray-300 mb-1"
					>
						Display Model Name
					</label>
					<input
						id="display-model"
						type="text"
						required={!isEdit}
						maxLength={128}
						value={displayModel}
						onChange={(e) => setDisplayModel(e.target.value)}
						disabled={isEdit}
						className="ui-input"
						placeholder="e.g., glm-5"
					/>
					<p className="text-gray-500 text-xs mt-1">
						{isEdit
							? "Model name cannot be changed after creation"
							: `This becomes hotel/${displayModel || "model-name"} in the model list`}
					</p>
				</div>

				<div>
					<label
						htmlFor="display-name"
						className="block text-sm font-medium text-gray-300 mb-1"
					>
						Display Name (optional)
					</label>
					<input
						id="display-name"
						type="text"
						maxLength={128}
						value={displayName}
						onChange={(e) => setDisplayName(e.target.value)}
						className="ui-input"
						placeholder="e.g., GLM-5 Failover"
					/>
				</div>

				<div>
					<label
						htmlFor="group-description"
						className="block text-sm font-medium text-gray-300 mb-1"
					>
						Description (optional)
					</label>
					<input
						id="group-description"
						type="text"
						maxLength={256}
						value={description}
						onChange={(e) => setDescription(e.target.value)}
						className="ui-input"
						placeholder="e.g., Failover group for GLM-5 models"
					/>
				</div>

				<div>
					<ModelPicker
						id={`failover-group-entries-${isEdit ? "edit" : "create"}`}
						models={modelItems}
						selected={selectedProxyIDs}
						onChange={setSelectedProxyIDs}
						multi={true}
						label="Model Entries"
						align="left"
					/>
					<p className="text-gray-500 text-xs mt-1">
						{selectedProxyIDs.length} selected
					</p>
				</div>

				<div className="flex justify-end gap-3 pt-4">
					<button
						type="button"
						onClick={onClose}
						className="ui-btn ui-btn-secondary"
					>
						Cancel
					</button>
					<button
						type="submit"
						disabled={isPending}
						className="ui-btn ui-btn-primary"
					>
						{isPending
							? isEdit
								? "Saving…"
								: "Creating…"
							: isEdit
								? "Save Changes"
								: "Create Group"}
					</button>
				</div>
			</form>
		</Modal>
	);
}
