import {
	closestCenter,
	DndContext,
	type DragEndEvent,
	KeyboardSensor,
	PointerSensor,
	useSensor,
	useSensors,
} from "@dnd-kit/core";
import {
	arrayMove,
	SortableContext,
	sortableKeyboardCoordinates,
	useSortable,
	verticalListSortingStrategy,
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ChevronRight, Shuffle } from "lucide-react";
import { useState } from "react";
import { api } from "../api/client";
import type { CandidateModel, FailoverGroup } from "../api/types";
import { FilterInput } from "../components/FilterInput";
import { Modal } from "../components/Modal";
import { Spinner } from "../components/Spinner";
import { useToast } from "../context/ToastContext";
import { formatTimestamp, formatTokens } from "../utils/format";

interface SortableEntryProps {
	entry: FailoverGroup["entries"][0];
	onToggle: (uuid: string, enabled: boolean) => void;
}

function SortableEntry({ entry, onToggle }: SortableEntryProps) {
	const {
		attributes,
		listeners,
		setNodeRef,
		transform,
		transition,
		isDragging,
	} = useSortable({ id: entry.model_uuid });

	const style: React.CSSProperties = {
		transform: CSS.Transform.toString(transform),
		transition,
		opacity: isDragging ? 0.5 : 1,
	};

	return (
		<div
			ref={setNodeRef}
			style={style}
			className={`relative flex items-center justify-between px-2 py-1.5 rounded group text-sm ${
				entry.enabled ? "bg-gray-700" : "failover-entry-disabled"
			}`}
		>
			<div className="flex items-center gap-2 min-w-0">
				<span
					{...attributes}
					{...listeners}
					className="text-gray-500 cursor-grab active:cursor-grabbing opacity-15 hover:opacity-100 transition-opacity shrink-0"
				>
					⠿
				</span>
				<div className="truncate failover-entry-text">
					<span className="text-white">{entry.provider_name}</span>
					<span className="text-gray-500 mx-1">/</span>
					<span className="text-gray-400 truncate">{entry.model_id}</span>
				</div>
			</div>
			<button
				type="button"
				onClick={() => onToggle(entry.model_uuid, !entry.enabled)}
				className={`relative inline-flex h-4 w-7 items-center rounded-full transition-colors focus:outline-none shrink-0 ${
					entry.enabled ? "bg-(--accent)" : "bg-gray-600"
				}`}
				aria-label={entry.enabled ? "Disable provider" : "Enable provider"}
			>
				<span
					className={`inline-block h-3 w-3 transform rounded-full bg-white transition-transform ${
						entry.enabled ? "translate-x-3.5" : "translate-x-0.5"
					}`}
				/>
			</button>
		</div>
	);
}

function FailoverGroupCard({
	group,
	selected,
	onToggleSelect,
	onToggleGroup,
	onToggleEntry,
	onReorder,
	onDelete,
}: {
	group: FailoverGroup;
	selected: boolean;
	onToggleSelect: (selected: boolean) => void;
	onToggleGroup: (enabled: boolean) => void;
	onToggleEntry: (uuid: string, enabled: boolean) => void;
	onReorder: (newOrder: string[]) => void;
	onDelete: () => void;
}) {
	const { toast } = useToast();
	const enabledCount = group.entries.filter((e) => e.enabled).length;
	const totalCount = group.entries.length;

	const sensors = useSensors(
		useSensor(PointerSensor),
		useSensor(KeyboardSensor, {
			coordinateGetter: sortableKeyboardCoordinates,
		}),
	);

	const handleDragEnd = (event: DragEndEvent) => {
		const { active, over } = event;
		if (over && active.id !== over.id) {
			const oldIndex = group.entries.findIndex(
				(e) => e.model_uuid === active.id,
			);
			const newIndex = group.entries.findIndex((e) => e.model_uuid === over.id);
			const newOrder = arrayMove(group.entries, oldIndex, newIndex).map(
				(e) => e.model_uuid,
			);
			onReorder(newOrder);
		}
	};

	const handleCopyModel = () => {
		const modelRef = `hotel/${group.display_model}`;
		navigator.clipboard.writeText(modelRef);
		toast(`Copied ${modelRef}`, "success");
	};

	return (
		<div
			className={`ui-card p-3 ${
				group.group_enabled ? "border-(--accent)/30" : "opacity-60"
			}`}
		>
			<div className="flex items-center justify-between mb-2">
				<div className="flex items-center gap-2 min-w-0">
					<input
						type="checkbox"
						checked={selected}
						onChange={(e) => onToggleSelect(e.target.checked)}
						className="rounded border-gray-600 text-(--accent) focus:ring-(--accent) shrink-0"
					/>
					{/* biome-ignore lint/a11y/useSemanticElements: cannot change to <button> without altering layout */}
					<div
						onClick={handleCopyModel}
						onKeyDown={(e) => {
							if (e.key === "Enter" || e.key === " ") {
								e.preventDefault();
								handleCopyModel();
							}
						}}
						role="button"
						tabIndex={0}
						className="flex items-center gap-1.5 min-w-0 select-none px-1.5 py-0.5 -mx-1.5 -my-0.5 rounded hover:bg-gray-700 transition-colors group cursor-default"
						title="Click to copy"
					>
						<h3 className="text-(--accent) font-medium text-sm truncate">
							hotel/{group.display_model}
						</h3>
						<svg
							className="w-3.5 h-3.5 text-gray-500 opacity-0 group-hover:opacity-100 transition-opacity shrink-0"
							fill="none"
							stroke="currentColor"
							viewBox="0 0 24 24"
						>
							<title>Copy</title>
							<path
								strokeLinecap="round"
								strokeLinejoin="round"
								strokeWidth={2}
								d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z"
							/>
						</svg>
					</div>
					{group.auto_created && (
						<span className="text-xs text-gray-500 shrink-0">auto</span>
					)}
				</div>
				<button
					type="button"
					onClick={() => onToggleGroup(!group.group_enabled)}
					className={`px-2 py-0.5 text-xs font-medium rounded-full transition-colors ${
						group.group_enabled
							? "bg-(--accent-light) text-(--accent) hover:bg-(--accent)/30"
							: "bg-gray-600 text-gray-300 hover:bg-gray-500"
					}`}
				>
					{group.group_enabled ? "ON" : "OFF"}
				</button>
			</div>

			<DndContext
				sensors={sensors}
				collisionDetection={closestCenter}
				onDragEnd={handleDragEnd}
			>
				<SortableContext
					items={group.entries.map((e) => e.model_uuid)}
					strategy={verticalListSortingStrategy}
				>
					<div className="space-y-1">
						{group.entries.map((entry) => (
							<SortableEntry
								key={entry.model_uuid}
								entry={entry}
								onToggle={onToggleEntry}
							/>
						))}
					</div>
				</SortableContext>
			</DndContext>

			<div className="flex items-center justify-between mt-2 text-xs text-gray-500">
				<span>
					{enabledCount}/{totalCount} active •{" "}
					{formatTokens(group.total_tokens)} tokens
				</span>
				<button
					type="button"
					onClick={() => onDelete()}
					className="text-gray-500 hover:text-red-400 cursor-pointer px-2 py-1 rounded-md hover:bg-red-400/10 transition-all"
				>
					delete
				</button>
			</div>
		</div>
	);
}

function CreateGroupModal({
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

export function FailoverGroups() {
	const { toast } = useToast();
	const queryClient = useQueryClient();

	const [showCreateModal, setShowCreateModal] = useState(false);
	const [deleteGroup, setDeleteGroup] = useState<FailoverGroup | null>(null);
	const [searchQuery, setSearchQuery] = useState("");
	const [providerFilter, setProviderFilter] = useState("");
	const [selectedGroupIds, setSelectedGroupIds] = useState<Set<string>>(
		new Set(),
	);
	const [collapsedLetters, setCollapsedLetters] = useState<Set<string>>(
		new Set(),
	);

	const toggleLetterCollapse = (letter: string) => {
		setCollapsedLetters((prev) => {
			const next = new Set(prev);
			if (next.has(letter)) next.delete(letter);
			else next.add(letter);
			return next;
		});
	};

	const { data: listData, isLoading } = useQuery({
		queryKey: ["failover-groups"],
		queryFn: () => api.failoverGroups.list(),
	});

	const allGroups = listData?.groups;

	// Unique provider names for dropdown
	const providerNames = allGroups
		? [
				...new Set(
					allGroups.flatMap((g) => g.entries.map((e) => e.provider_name)),
				),
			].sort()
		: [];

	const groups = allGroups?.filter((g) => {
		const matchesModel = g.display_model
			.toLowerCase()
			.includes(searchQuery.toLowerCase());
		const matchesProvider =
			!providerFilter ||
			g.entries.some((e) =>
				e.provider_name.toLowerCase().includes(providerFilter.toLowerCase()),
			);
		return matchesModel && matchesProvider;
	});
	const lastSyncedAt = listData?.last_synced_at;

	const totalEnabled = allGroups?.filter((g) => g.group_enabled).length ?? 0;
	const totalDisabled = (allGroups?.length ?? 0) - totalEnabled;
	const allSameState = totalEnabled === 0 || totalDisabled === 0;

	// Sort groups alphabetically by display_model and group by first letter
	const sortedGroups = [...(groups ?? [])].sort((a, b) =>
		a.display_model.localeCompare(b.display_model),
	);
	const letterGroups = sortedGroups.reduce<Record<string, typeof sortedGroups>>(
		(acc, group) => {
			const letter = group.display_model.charAt(0).toUpperCase();
			if (!acc[letter]) acc[letter] = [];
			acc[letter].push(group);
			return acc;
		},
		{},
	);
	const sortedLetters = Object.keys(letterGroups).sort();

	// Bulk model enable/disable
	const toggleGroupSelect = (groupId: string, checked: boolean) => {
		setSelectedGroupIds((prev) => {
			const next = new Set(prev);
			if (checked) next.add(groupId);
			else next.delete(groupId);
			return next;
		});
	};

	const handleBulkModelToggle = async (enabled: boolean) => {
		if (!allGroups) return;
		const targets = allGroups.filter((g) => selectedGroupIds.has(g.id));
		if (targets.length === 0) return;

		const promises = targets.map((group) => {
			const entryEnabledMap: Record<string, boolean> = {};
			group.entries.forEach((e) => {
				entryEnabledMap[e.model_uuid] = enabled;
			});
			// If disabling all entries in an active group, also disable the group
			const alsoDisableGroup = !enabled && group.group_enabled;
			return api.failoverGroups.update(group.id, {
				entry_enabled: entryEnabledMap,
				...(alsoDisableGroup ? { group_enabled: false } : {}),
			});
		});

		try {
			await Promise.all(promises);
			queryClient.invalidateQueries({ queryKey: ["failover-groups"] });
			setSelectedGroupIds(new Set());
			toast(
				`${enabled ? "Enabled" : "Disabled"} all entries in ${targets.length} group${targets.length > 1 ? "s" : ""}`,
				"success",
			);
		} catch {
			queryClient.invalidateQueries({ queryKey: ["failover-groups"] });
			toast("Bulk toggle failed for some groups", "error");
		}
	};

	// Bulk provider enable/disable
	const handleBulkProviderToggle = async (enabled: boolean) => {
		if (!allGroups || !providerFilter) return;
		const providerLower = providerFilter.toLowerCase();
		const affectedGroups = allGroups.filter((g) =>
			g.entries.some((e) =>
				e.provider_name.toLowerCase().includes(providerLower),
			),
		);
		if (affectedGroups.length === 0) return;

		const promises = affectedGroups.map((group) => {
			const entryEnabledMap: Record<string, boolean> = {};
			group.entries.forEach((e) => {
				entryEnabledMap[e.model_uuid] = e.provider_name
					.toLowerCase()
					.includes(providerLower)
					? enabled
					: e.enabled;
			});
			// If disabling all entries in an active group, also disable the group
			const remainingEnabled =
				Object.values(entryEnabledMap).filter(Boolean).length;
			const alsoDisableGroup =
				!enabled && remainingEnabled === 0 && group.group_enabled;
			return api.failoverGroups.update(group.id, {
				entry_enabled: entryEnabledMap,
				...(alsoDisableGroup ? { group_enabled: false } : {}),
			});
		});

		try {
			await Promise.all(promises);
			queryClient.invalidateQueries({ queryKey: ["failover-groups"] });
			toast(
				`${enabled ? "Enabled" : "Disabled"} ${providerFilter} across ${affectedGroups.length} group${affectedGroups.length > 1 ? "s" : ""}`,
				"success",
			);
		} catch {
			queryClient.invalidateQueries({ queryKey: ["failover-groups"] });
			toast("Bulk provider toggle failed for some groups", "error");
		}
	};

	const { data: candidates } = useQuery({
		queryKey: ["failover-candidates"],
		queryFn: () => api.failoverGroups.candidates(),
	});

	const syncMutation = useMutation({
		mutationFn: () => api.failoverGroups.sync(),
		onSuccess: (data) => {
			queryClient.invalidateQueries({ queryKey: ["failover-groups"] });
			if (data.disabled_groups && data.disabled_groups.length > 0) {
				for (const g of data.disabled_groups) {
					const provs =
						g.provider_names.length > 0
							? ` (${g.provider_names.join(", ")})`
							: "";
					toast(
						`hotel/${g.display_model} disabled: ${g.reason}${provs}`,
						"warning",
					);
				}
			} else {
				toast("Failover groups synced", "success");
			}
		},
		onError: (err: Error) => {
			toast(`Failed to sync: ${err.message}`, "error");
		},
	});

	const updateMutation = useMutation({
		mutationFn: ({
			id,
			data,
		}: {
			id: string;
			data: Parameters<typeof api.failoverGroups.update>[1];
		}) => api.failoverGroups.update(id, data),
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: ["failover-groups"] });
		},
		onError: (err: Error) => {
			toast(`Failed to update: ${err.message}`, "error");
		},
	});

	const deleteMutation = useMutation({
		mutationFn: (id: string) => api.failoverGroups.delete(id),
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: ["failover-groups"] });
			toast("Group deleted", "success");
		},
		onError: (err: Error) => {
			toast(`Failed to delete: ${err.message}`, "error");
		},
	});

	const handleToggleGroup = (group: FailoverGroup, enabled: boolean) => {
		updateMutation.mutate({
			id: group.id,
			data: { group_enabled: enabled },
		});
	};

	const handleToggleEntry = (
		group: FailoverGroup,
		uuid: string,
		enabled: boolean,
	) => {
		const enabledCount = group.entries.filter((e) => e.enabled).length;
		if (!enabled && enabledCount <= 1) {
			toast("At least one provider must remain active", "error");
			return;
		}
		const entryEnabledMap: Record<string, boolean> = {};
		group.entries.forEach((e) => {
			entryEnabledMap[e.model_uuid] = e.enabled;
		});
		entryEnabledMap[uuid] = enabled;
		updateMutation.mutate({
			id: group.id,
			data: { entry_enabled: entryEnabledMap },
		});
	};

	const handleReorder = (group: FailoverGroup, newOrder: string[]) => {
		updateMutation.mutate({
			id: group.id,
			data: { priority_order: newOrder },
		});
	};

	const handleDelete = (group: FailoverGroup) => {
		setDeleteGroup(group);
	};

	const confirmDelete = () => {
		if (deleteGroup) {
			deleteMutation.mutate(deleteGroup.id);
			setDeleteGroup(null);
		}
	};

	if (isLoading) {
		return (
			<div className="flex items-center justify-center h-64">
				<div className="text-gray-500">Loading...</div>
			</div>
		);
	}

	return (
		<div className="space-y-6" style={{ scrollBehavior: "smooth" }}>
			<div className="flex justify-between items-center">
				<div>
					<div className="flex items-center gap-3">
						<Shuffle size={28} strokeWidth={2} className="text-(--accent)" />
						<h1 className="text-2xl font-bold text-(--text-primary)">
							Failover Groups
						</h1>
						{!allSameState && groups && groups.length > 0 && (
							<span className="inline-flex items-center gap-2 px-2.5 py-1 rounded-full text-xs font-medium bg-gray-700/60 border border-gray-600/50">
								<span className="text-green-400">{totalEnabled} enabled</span>
								<span className="text-gray-600">/</span>
								<span className="text-red-400">{totalDisabled} disabled</span>
							</span>
						)}
					</div>
					<p className="text-gray-400">
						Route requests through multiple providers in priority order via{" "}
						<code className="text-(--accent)">hotel/model</code>
					</p>
					<p className="text-(--text-muted) text-xs flex items-center gap-1.5 mt-0.5">
						<span className="shrink-0" aria-hidden="true">
							⠿
						</span>
						Drag models by the handle (⠿) to reorder priority
					</p>
				</div>
				<div className="flex items-center gap-3">
					{lastSyncedAt && (
						<span className="text-xs text-gray-500">
							Last sync: {lastSyncedAt ? formatTimestamp(lastSyncedAt) : ""}
						</span>
					)}
					<button
						type="button"
						onClick={() => syncMutation.mutate()}
						disabled={syncMutation.isPending}
						className="ui-btn ui-btn-secondary"
					>
						{syncMutation.isPending ? (
							<>
								<Spinner /> Syncing…
							</>
						) : (
							"Sync"
						)}
					</button>
					<button
						type="button"
						onClick={() => setShowCreateModal(true)}
						className="ui-btn ui-btn-primary"
					>
						+ New Group
					</button>
				</div>
			</div>

			<div className="flex items-center gap-3 flex-wrap">
				<FilterInput
					value={searchQuery}
					onChange={setSearchQuery}
					placeholder="Filter hotel/model…"
					className="w-[260px]"
					autoFocus
				/>
				<select
					value={providerFilter}
					onChange={(e) => setProviderFilter(e.target.value)}
					className="ui-input w-auto max-w-[220px] shrink-0"
				>
					<option value="">All providers</option>
					{providerNames.map((name) => (
						<option key={name} value={name}>
							{name}
						</option>
					))}
				</select>
				{selectedGroupIds.size > 0 && (
					<>
						<span className="text-sm text-gray-400 ml-auto">
							{selectedGroupIds.size} group
							{selectedGroupIds.size > 1 ? "s" : ""} selected
						</span>
						<button
							type="button"
							onClick={() => handleBulkModelToggle(true)}
							className="ui-btn ui-btn-secondary text-xs"
						>
							Enable all entries
						</button>
						<button
							type="button"
							onClick={() => handleBulkModelToggle(false)}
							className="ui-btn ui-btn-secondary text-xs"
						>
							Disable all entries
						</button>
						<button
							type="button"
							onClick={() => setSelectedGroupIds(new Set())}
							className="ui-btn ui-btn-secondary text-xs"
						>
							Deselect
						</button>
					</>
				)}
			</div>

			{providerFilter && allGroups && (
				<div className="flex items-center justify-between bg-gray-800/50 rounded-lg px-4 py-2 border border-gray-700">
					<span className="text-sm text-gray-300">
						{(() => {
							const count = allGroups.filter((g) =>
								g.entries.some((e) =>
									e.provider_name
										.toLowerCase()
										.includes(providerFilter.toLowerCase()),
								),
							).length;
							return `${count} group${count !== 1 ? "s" : ""} with ${providerFilter} entries`;
						})()}
					</span>
					<div className="flex items-center gap-2">
						<button
							type="button"
							onClick={() => handleBulkProviderToggle(true)}
							className="ui-btn ui-btn-secondary text-xs"
						>
							Enable all {providerFilter}
						</button>
						<button
							type="button"
							onClick={() => handleBulkProviderToggle(false)}
							className="ui-btn ui-btn-secondary text-xs"
						>
							Disable all {providerFilter}
						</button>
					</div>
				</div>
			)}

			{groups && groups.length === 0 ? (
				searchQuery || providerFilter ? (
					<div className="text-center py-12">
						<div className="text-gray-500 mb-4">No groups matching filters</div>
						<button
							type="button"
							onClick={() => {
								setSearchQuery("");
								setProviderFilter("");
							}}
							className="ui-btn ui-btn-primary"
						>
							Clear filters
						</button>
					</div>
				) : (
					<div className="text-center py-12">
						<div className="text-gray-500 mb-4">
							No failover groups configured
						</div>
						<button
							type="button"
							onClick={() => syncMutation.mutate()}
							className="ui-btn ui-btn-primary"
						>
							Auto-discover from models
						</button>
					</div>
				)
			) : (
				<div className="relative flex gap-4">
					<div className="flex-1 space-y-6">
						{sortedLetters.map((letter) => (
							<section key={letter} id={`failover-section-${letter}`}>
								<button
									type="button"
									onClick={() => toggleLetterCollapse(letter)}
									className="flex items-center gap-3 mb-3 w-full text-left group"
								>
									<ChevronRight
										size={16}
										className={`text-gray-500 transition-transform ${collapsedLetters.has(letter) ? "" : "rotate-90"}`}
									/>
									<span className="text-lg font-bold text-(--accent)">
										{letter}
									</span>
									<div className="flex-1 h-px bg-gray-700/50" />
									<span className="text-xs text-gray-500">
										{letterGroups[letter].length} group
										{letterGroups[letter].length > 1 ? "s" : ""}
									</span>
								</button>
								<div
									className="grid transition-[grid-template-rows] duration-200 ease-in-out"
									style={{
										gridTemplateRows: collapsedLetters.has(letter)
											? "0fr"
											: "1fr",
									}}
								>
									<div className="overflow-hidden">
										<div className="grid grid-cols-1 lg:grid-cols-2 xl:grid-cols-3 gap-4">
											{letterGroups[letter].map((group) => (
												<FailoverGroupCard
													key={group.id}
													group={group}
													selected={selectedGroupIds.has(group.id)}
													onToggleSelect={(checked) =>
														toggleGroupSelect(group.id, checked)
													}
													onToggleGroup={(enabled) =>
														handleToggleGroup(group, enabled)
													}
													onToggleEntry={(uuid, enabled) =>
														handleToggleEntry(group, uuid, enabled)
													}
													onReorder={(newOrder) =>
														handleReorder(group, newOrder)
													}
													onDelete={() => handleDelete(group)}
												/>
											))}
										</div>
									</div>
								</div>
							</section>
						))}
					</div>

					{/* Alphabet sidebar */}
					{sortedLetters.length > 3 && (
						<nav className="hidden xl:flex flex-col items-center gap-1 pt-2 sticky top-4 self-start">
							{sortedLetters.map((letter) => (
								<button
									key={letter}
									type="button"
									onClick={() =>
										document
											.getElementById(`failover-section-${letter}`)
											?.scrollIntoView({ behavior: "smooth", block: "start" })
									}
									className="text-xs font-medium text-gray-500 hover:text-(--accent) hover:drop-shadow-[0_0_6px_var(--accent)] transition-all px-1.5 py-0.5 rounded"
								>
									{letter}
								</button>
							))}
						</nav>
					)}
				</div>
			)}

			{showCreateModal && candidates && (
				<CreateGroupModal
					candidates={candidates}
					onClose={() => setShowCreateModal(false)}
					onCreated={() => setShowCreateModal(false)}
				/>
			)}

			{deleteGroup && (
				<Modal
					title="Delete Failover Group"
					onClose={() => setDeleteGroup(null)}
					maxWidth="max-w-sm"
				>
					<p className="text-sm text-gray-300 mb-4">
						Are you sure you want to delete{" "}
						<span className="text-white font-medium">
							hotel/{deleteGroup.display_model}
						</span>
						? This cannot be undone.
					</p>
					<div className="flex gap-3 justify-end">
						<button
							type="button"
							onClick={() => setDeleteGroup(null)}
							className="ui-btn ui-btn-secondary"
						>
							Cancel
						</button>
						<button
							type="button"
							onClick={confirmDelete}
							disabled={deleteMutation.isPending}
							className="ui-btn ui-btn-danger"
						>
							{deleteMutation.isPending ? "Deleting…" : "Delete"}
						</button>
					</div>
				</Modal>
			)}
		</div>
	);
}
