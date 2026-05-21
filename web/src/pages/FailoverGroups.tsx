import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { CheckSquare, ChevronRight, Shuffle, Square } from "lucide-react";
import { useState } from "react";
import { api } from "../api/client";
import type { FailoverGroup } from "../api/types";
import { DeleteConfirmModal } from "../components/DeleteConfirmModal";
import { EmptyState } from "../components/EmptyState";
import { FilterDropdown } from "../components/FilterDropdown";
import { FilterInput } from "../components/FilterInput";
import { PageHeader } from "../components/PageHeader";
import { Spinner } from "../components/Spinner";
import { useToast } from "../context/ToastContext";
import { countLabel, formatTimestamp } from "../utils/format";
import { CreateGroupModal } from "./FailoverGroups/CreateGroupModal";
import { FailoverGroupCard } from "./FailoverGroups/FailoverGroupCard";

export function FailoverGroups() {
	const { toast } = useToast();
	const queryClient = useQueryClient();

	const [showCreateModal, setShowCreateModal] = useState(false);
	const [deleteGroup, setDeleteGroup] = useState<FailoverGroup | null>(null);
	const [bulkDeleteIds, setBulkDeleteIds] = useState<Set<string> | null>(null);
	const [isBulkDeleting, setIsBulkDeleting] = useState(false);
	const [searchQuery, setSearchQuery] = useState("");
	const [providerFilter, setProviderFilter] = useState("");
	const [enabledFilter, setEnabledFilter] = useState<string>("");
	const [originFilter, setOriginFilter] = useState<string>("");
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
		const matchesEnabled =
			enabledFilter === "" ||
			(enabledFilter === "enabled" && g.group_enabled) ||
			(enabledFilter === "disabled" && !g.group_enabled);
		const matchesOrigin =
			originFilter === "" ||
			(originFilter === "auto" && g.auto_created) ||
			(originFilter === "manual" && !g.auto_created);
		return matchesModel && matchesProvider && matchesEnabled && matchesOrigin;
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

	const confirmBulkDelete = async () => {
		if (!bulkDeleteIds || bulkDeleteIds.size === 0) return;
		const ids = [...bulkDeleteIds];
		setIsBulkDeleting(true);
		const results = await Promise.allSettled(
			ids.map((id) => api.failoverGroups.delete(id)),
		);
		const succeeded = results.filter((r) => r.status === "fulfilled").length;
		const failed = results.length - succeeded;
		queryClient.invalidateQueries({ queryKey: ["failover-groups"] });
		if (failed === 0) {
			toast(`Deleted ${succeeded} group${succeeded > 1 ? "s" : ""}`, "success");
		} else {
			toast(
				`Deleted ${succeeded} of ${ids.length} groups (${failed} failed)`,
				"warning",
			);
		}
		setIsBulkDeleting(false);
		setBulkDeleteIds(null);
		setSelectedGroupIds(new Set());
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
			<PageHeader
				icon={Shuffle}
				title={countLabel(
					allGroups?.length,
					"Failover Group",
					"Failover Groups",
				)}
				description={
					<>
						Route requests through multiple providers in priority order via{" "}
						<code className="text-(--accent)">hotel/model</code>
					</>
				}
				badge={
					!allSameState && groups && groups.length > 0 ? (
						<span className="inline-flex items-center gap-2 px-2.5 py-1 rounded-full text-xs font-medium bg-gray-700/60 border border-gray-600/50">
							<span className="text-green-400">{totalEnabled} enabled</span>
							<span className="text-gray-600">/</span>
							<span className="text-red-400">{totalDisabled} disabled</span>
						</span>
					) : undefined
				}
				actions={
					<>
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
					</>
				}
			/>
			<p className="text-(--text-muted) text-xs flex items-center gap-1.5 -mt-4">
				<span className="shrink-0" aria-hidden="true">
					⠿
				</span>
				Drag models by the handle (⠿) to reorder priority
			</p>

			<div className="flex items-center gap-3 flex-wrap">
				<FilterInput
					value={searchQuery}
					onChange={setSearchQuery}
					placeholder="Filter hotel/model…"
					className="w-[260px]"
					autoFocus
				/>
				<FilterDropdown
					value={providerFilter}
					onChange={setProviderFilter}
					placeholder="All providers"
					allLabel="All providers"
					options={providerNames.map((name) => ({ value: name, label: name }))}
					className="w-[220px] shrink-0"
				/>
				<FilterDropdown
					value={enabledFilter}
					onChange={setEnabledFilter}
					placeholder="All states"
					allLabel="All states"
					options={[
						{ value: "enabled", label: "Enabled" },
						{ value: "disabled", label: "Disabled" },
					]}
					className="w-[160px] shrink-0"
				/>
				<FilterDropdown
					value={originFilter}
					onChange={setOriginFilter}
					placeholder="All origins"
					allLabel="All origins"
					options={[
						{ value: "auto", label: "Auto" },
						{ value: "manual", label: "Manual" },
					]}
					className="w-[160px] shrink-0"
				/>
				<button
					type="button"
					onClick={() => {
						if (selectedGroupIds.size > 0) {
							setSelectedGroupIds(new Set());
						} else if (groups) {
							setSelectedGroupIds(new Set(groups.map((g) => g.id)));
						}
					}}
					className="ml-auto text-gray-400 hover:text-(--accent) hover:drop-shadow-[0_0_8px_var(--accent)] transition-all cursor-pointer"
					aria-label={selectedGroupIds.size > 0 ? "Deselect all" : "Select all"}
					title={selectedGroupIds.size > 0 ? "Deselect all" : "Select all"}
				>
					{selectedGroupIds.size > 0 ? (
						<CheckSquare size={18} />
					) : (
						<Square size={18} />
					)}
				</button>
				{selectedGroupIds.size > 0 && (
					<>
						<span className="text-sm text-gray-400">
							{selectedGroupIds.size} selected
						</span>
						<button
							type="button"
							onClick={() => handleBulkModelToggle(true)}
							className="ui-btn ui-btn-secondary text-xs"
						>
							Enable all
						</button>
						<button
							type="button"
							onClick={() => handleBulkModelToggle(false)}
							className="ui-btn ui-btn-secondary text-xs"
						>
							Disable all
						</button>
						<button
							type="button"
							onClick={() => setBulkDeleteIds(new Set(selectedGroupIds))}
							className="ui-btn ui-btn-danger text-xs"
						>
							Delete all
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
				originFilter ? (
					<EmptyState
						message={`No ${originFilter === "auto" ? "auto-discovered" : "manually created"} groups found`}
						action={{
							label:
								originFilter === "manual"
									? "Create New Group"
									: "Clear filters",
							onClick: () =>
								originFilter === "manual"
									? setShowCreateModal(true)
									: setOriginFilter(""),
						}}
					/>
				) : searchQuery || providerFilter || enabledFilter ? (
					<EmptyState
						message="No groups matching filters"
						action={{
							label: "Clear filters",
							onClick: () => {
								setSearchQuery("");
								setProviderFilter("");
								setEnabledFilter("");
								setOriginFilter("");
							},
						}}
					/>
				) : (
					<EmptyState
						message="No failover groups configured"
						action={{
							label: "Auto-discover from models",
							onClick: () => syncMutation.mutate(),
						}}
					/>
				)
			) : (
				<div className="relative flex gap-4">
					<div className="flex-1 space-y-6">
						{sortedLetters.map((letter) => (
							<section key={letter} id={`failover-section-${letter}`}>
								<button
									type="button"
									onClick={() => toggleLetterCollapse(letter)}
									className="flex items-center gap-3 mb-3 w-full text-left group cursor-pointer"
								>
									<ChevronRight
										size={16}
										className={`text-gray-500 transition-transform group-hover:text-(--accent) group-hover:drop-shadow-[0_0_8px_var(--accent)] ${collapsedLetters.has(letter) ? "" : "rotate-90"}`}
									/>
									<span className="text-lg font-bold text-(--accent) group-hover:[text-shadow:0_0_8px_var(--accent)]">
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
									className="text-xs font-medium text-gray-500 hover:text-(--accent) hover:[text-shadow:0_0_8px_var(--accent)] transition-all cursor-pointer px-1.5 py-0.5 rounded"
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
				<DeleteConfirmModal
					entityName={`hotel/${deleteGroup.display_model}`}
					entityType="failover group"
					isPending={deleteMutation.isPending}
					onConfirm={confirmDelete}
					onCancel={() => setDeleteGroup(null)}
				/>
			)}

			{bulkDeleteIds && (
				<DeleteConfirmModal
					entityName={`${bulkDeleteIds.size} failover group${bulkDeleteIds.size > 1 ? "s" : ""}`}
					entityType="failover groups"
					isPending={isBulkDeleting}
					onConfirm={confirmBulkDelete}
					onCancel={() => setBulkDeleteIds(null)}
				/>
			)}
		</div>
	);
}
