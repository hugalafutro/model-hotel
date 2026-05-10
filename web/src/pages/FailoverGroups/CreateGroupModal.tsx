import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { api } from "../../api/client";
import type { CandidateModel } from "../../api/types";
import { Modal } from "../../components/Modal";
import { useToast } from "../../context/ToastContext";

export function CreateGroupModal({
	candidates,
	onClose,
	onCreated,
}: {
	candidates: CandidateModel[];
	onClose: () => void;
	onCreated: () => void;
}) {
	const { toast } = useToast();
	const queryClient = useQueryClient();
	const [displayModel, setDisplayModel] = useState("");
	const [displayName, setDisplayName] = useState("");
	const [selectedEntries, setSelectedEntries] = useState<string[]>([]);
	const [search, setSearch] = useState("");

	const createMutation = useMutation({
		mutationFn: (data: {
			display_model: string;
			display_name?: string;
			entry_ids: string[];
		}) => api.failoverGroups.create(data),
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: ["failover-groups"] });
			toast("Failover group created", "success");
			onCreated();
		},
		onError: (err: Error) => {
			toast(`Failed to create group: ${err.message}`, "error");
		},
	});

	const filteredCandidates = candidates.filter((c) =>
		`${c.provider_name.replace(/ /g, "-")}/${c.model_id}`
			.toLowerCase()
			.includes(search.toLowerCase()),
	);

	const grouped = filteredCandidates.reduce(
		(acc, c) => {
			const key = c.model_id;
			if (!acc[key]) acc[key] = [];
			acc[key].push(c);
			return acc;
		},
		{} as Record<string, CandidateModel[]>,
	);

	const handleSubmit = (e: React.FormEvent) => {
		e.preventDefault();
		if (!displayModel.trim()) {
			toast("Display model name is required", "error");
			return;
		}
		if (selectedEntries.length < 2) {
			toast("At least 2 entries required", "error");
			return;
		}
		createMutation.mutate({
			display_model: displayModel.trim(),
			display_name: displayName.trim() || undefined,
			entry_ids: selectedEntries,
		});
	};

	return (
		<Modal
			title="Create Failover Group"
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
						required
						maxLength={128}
						value={displayModel}
						onChange={(e) => setDisplayModel(e.target.value)}
						className="ui-input"
						placeholder="e.g., glm-5"
					/>
					<p className="text-gray-500 text-xs mt-1">
						This becomes hotel/{displayModel || "model-name"} in the model list
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
						htmlFor="create-group-search"
						className="block text-sm font-medium text-gray-300 mb-1"
					>
						Model Entries
					</label>
					<input
						id="create-group-search"
						type="text"
						value={search}
						onChange={(e) => setSearch(e.target.value)}
						className="ui-input mb-2"
						placeholder="Search providers/models…"
					/>
					<div className="max-h-48 overflow-y-auto bg-gray-900 rounded-lg p-2 space-y-1">
						{Object.entries(grouped).map(([modelId, models]) => (
							<div key={modelId} className="space-y-0.5">
								<div className="text-xs text-gray-500 px-1 pt-1">{modelId}</div>
								{models.map((m) => (
									<label
										key={m.model_uuid}
										className="flex items-center gap-2 px-2 py-1.5 rounded hover:bg-gray-800 cursor-pointer"
									>
										<input
											type="checkbox"
											checked={selectedEntries.includes(m.model_uuid)}
											onChange={(e) => {
												if (e.target.checked) {
													setSelectedEntries([
														...selectedEntries,
														m.model_uuid,
													]);
												} else {
													setSelectedEntries(
														selectedEntries.filter((id) => id !== m.model_uuid),
													);
												}
											}}
											className="rounded border-gray-600 text-(--accent) focus:ring-(--accent)"
										/>
										<span className="text-sm text-gray-300">
											{m.provider_name}
											<span className="text-gray-500 ml-1 text-xs">
												({m.display_name || modelId})
											</span>
										</span>
									</label>
								))}
							</div>
						))}
					</div>
					<p className="text-gray-500 text-xs mt-1">
						{selectedEntries.length} selected
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
						disabled={createMutation.isPending}
						className="ui-btn ui-btn-primary"
					>
						{createMutation.isPending ? "Creating…" : "Create Group"}
					</button>
				</div>
			</form>
		</Modal>
	);
}
