import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { CheckSquare, ChevronRight, Shuffle, Square } from "lucide-react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
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
	const { t } = useTranslation();
	const queryClient = useQueryClient();

	const [showCreateModal, setShowCreateModal] = useState(false);
	const [editGroup, setEditGroup] = useState<FailoverGroup | null>(null);
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

	// Separate custom groups (manually created) from auto groups
	const customGroups = [...(groups ?? [])]
		.filter((g) => !g.auto_created)
		.sort((a, b) => a.display_model.localeCompare(b.display_model));
	const autoGroups = [...(groups ?? [])]
		.filter((g) => g.auto_created)
		.sort((a, b) => a.display_model.localeCompare(b.display_model));

	// Auto groups grouped by first letter
	const letterGroups = autoGroups.reduce<Record<string, typeof autoGroups>>(
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
				t("failover.toast_bulk_toggle_success", {
					action: enabled ? t("common.enabled") : t("common.disabled"),
					count: targets.length,
				}),
				"success",
			);
		} catch {
			queryClient.invalidateQueries({ queryKey: ["failover-groups"] });
			toast(t("failover.toast_bulk_toggle_failed"), "error");
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
				t("failover.toast_provider_toggle_success", {
					action: enabled ? t("common.enabled") : t("common.disabled"),
					provider: providerFilter,
					count: affectedGroups.length,
				}),
				"success",
			);
		} catch {
			queryClient.invalidateQueries({ queryKey: ["failover-groups"] });
			toast(t("failover.toast_provider_toggle_failed"), "error");
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
			if (data.deleted_groups && data.deleted_groups.length > 0) {
				for (const g of data.deleted_groups) {
					const provs =
						g.provider_names.length > 0
							? ` (${g.provider_names.join(", ")})`
							: "";
					toast(
						t("failover.toast_sync_deleted", {
							model: g.display_model,
							reason: g.reason,
							providers: provs,
						}),
						"warning",
					);
				}
			}
			if (data.purged_entries && data.purged_entries.length > 0) {
				for (const p of data.purged_entries) {
					toast(
						t("failover.toast_sync_purged", {
							group: p.group_display_model,
							count: p.pruned_model_ids.length,
						}),
						"info",
					);
				}
			}
			if (
				(!data.deleted_groups || data.deleted_groups.length === 0) &&
				(!data.purged_entries || data.purged_entries.length === 0)
			) {
				toast(t("failover.toast_sync_success"), "success");
			}
		},
		onError: (err: Error) => {
			toast(t("failover.toast_sync_failed", { message: err.message }), "error");
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
			toast(
				t("failover.toast_update_failed", { message: err.message }),
				"error",
			);
		},
	});

	const deleteMutation = useMutation({
		mutationFn: (id: string) => api.failoverGroups.delete(id),
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: ["failover-groups"] });
			toast(t("failover.toast_delete_success"), "success");
		},
		onError: (err: Error) => {
			toast(
				t("failover.toast_delete_failed", { message: err.message }),
				"error",
			);
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
			toast(t("failover.toast_entry_min_one"), "error");
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
			toast(
				t("failover.toast_bulk_delete_success", { count: succeeded }),
				"success",
			);
		} else {
			toast(
				t("failover.toast_bulk_delete_warning", {
					succeeded,
					total: ids.length,
					failed,
				}),
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
				<div className="text-gray-500">{t("common.loadingDots")}</div>
			</div>
		);
	}

	return (
		<div className="space-y-6" style={{ scrollBehavior: "smooth" }}>
			<PageHeader
				icon={Shuffle}
				title={countLabel(
					allGroups?.length,
					t("failoverGroups.countLabel_one"),
					t("failoverGroups.countLabel_other"),
				)}
				description={
					<>
						{t("failover.page_description_lead")}{" "}
						<code className="text-(--accent)">
							{t("failover.page_description_code")}
						</code>
					</>
				}
				badge={
					!allSameState && groups && groups.length > 0 ? (
						<span className="inline-flex items-center gap-2 px-2.5 py-1 leading-[1.6] rounded-full text-xs font-medium bg-gray-700/60 border border-gray-600/50">
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
								{t("failover.last_sync", {
									time: lastSyncedAt ? formatTimestamp(lastSyncedAt) : "",
								})}
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
									<Spinner /> {t("failover.btn_syncing")}
								</>
							) : (
								t("failover.btn_sync")
							)}
						</button>
						<button
							type="button"
							onClick={() => setShowCreateModal(true)}
							className="ui-btn ui-btn-primary"
						>
							{t("failover.btn_new_group")}
						</button>
					</>
				}
			/>
			<p className="text-(--text-muted) text-xs flex items-center gap-1.5 -mt-4">
				<span className="shrink-0" aria-hidden="true">
					⠿
				</span>
				{t("failover.hint_drag")}
			</p>

			<div className="flex items-center gap-3 flex-wrap">
				<FilterInput
					value={searchQuery}
					onChange={setSearchQuery}
					placeholder={t("failover.filter_hotel_model")}
					className="w-[260px]"
					autoFocus
				/>
				<FilterDropdown
					value={providerFilter}
					onChange={setProviderFilter}
					placeholder={t("failover.filter_providers")}
					allLabel={t("failover.filter_providers")}
					options={providerNames.map((name) => ({ value: name, label: name }))}
					className="w-[220px] shrink-0"
				/>
				<FilterDropdown
					value={enabledFilter}
					onChange={setEnabledFilter}
					placeholder={t("failover.filter_states")}
					allLabel={t("failover.filter_states")}
					options={[
						{ value: "enabled", label: t("failover.filter_state_enabled") },
						{ value: "disabled", label: t("failover.filter_state_disabled") },
					]}
					className="w-[160px] shrink-0"
				/>
				<FilterDropdown
					value={originFilter}
					onChange={setOriginFilter}
					placeholder={t("failover.filter_origins")}
					allLabel={t("failover.filter_origins")}
					options={[
						{ value: "auto", label: t("failover.filter_origin_auto") },
						{ value: "manual", label: t("failover.filter_origin_manual") },
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
					aria-label={
						selectedGroupIds.size > 0
							? t("failover.deselect_all")
							: t("failover.select_all")
					}
					title={
						selectedGroupIds.size > 0
							? t("failover.deselect_all")
							: t("failover.select_all")
					}
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
							{t("failover.selected_count", { count: selectedGroupIds.size })}
						</span>
						<button
							type="button"
							onClick={() => handleBulkModelToggle(true)}
							className="ui-btn ui-btn-secondary text-xs"
						>
							{t("failover.btn_enable_all")}
						</button>
						<button
							type="button"
							onClick={() => handleBulkModelToggle(false)}
							className="ui-btn ui-btn-secondary text-xs"
						>
							{t("failover.btn_disable_all")}
						</button>
						<button
							type="button"
							onClick={() => setBulkDeleteIds(new Set(selectedGroupIds))}
							className="ui-btn ui-btn-danger text-xs"
						>
							{t("failover.btn_delete_all")}
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
							return t("failover.bulk_provider_count", {
								count,
								provider: providerFilter,
							});
						})()}
					</span>
					<div className="flex items-center gap-2">
						<button
							type="button"
							onClick={() => handleBulkProviderToggle(true)}
							className="ui-btn ui-btn-secondary text-xs"
						>
							{t("failover.bulk_provider_enable", { provider: providerFilter })}
						</button>
						<button
							type="button"
							onClick={() => handleBulkProviderToggle(false)}
							className="ui-btn ui-btn-secondary text-xs"
						>
							{t("failover.bulk_provider_disable", {
								provider: providerFilter,
							})}
						</button>
					</div>
				</div>
			)}

			{groups && groups.length === 0 ? (
				originFilter && !searchQuery && !providerFilter && !enabledFilter ? (
					<EmptyState
						message={
							originFilter === "auto"
								? t("failover.empty_no_auto")
								: t("failover.empty_no_manual")
						}
						action={{
							label:
								originFilter === "manual"
									? t("failover.empty_create_group")
									: t("failover.empty_clear_filters"),
							onClick: () =>
								originFilter === "manual"
									? setShowCreateModal(true)
									: setOriginFilter(""),
						}}
					/>
				) : searchQuery || providerFilter || enabledFilter || originFilter ? (
					<EmptyState
						message={t("failover.empty_no_match")}
						action={{
							label: t("failover.empty_clear_filters"),
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
						message={t("failover.empty_no_groups")}
						action={{
							label: t("failover.empty_auto_discover"),
							onClick: () => syncMutation.mutate(),
						}}
					/>
				)
			) : (
				<div className="relative flex gap-4">
					<div className="flex-1 space-y-6">
						{/* Custom groups section (manually created) */}
						{customGroups.length > 0 && (
							<section id="failover-section-custom">
								<button
									type="button"
									onClick={() => toggleLetterCollapse("custom")}
									className="flex items-center gap-3 mb-3 w-full text-left group cursor-pointer"
								>
									<ChevronRight
										size={16}
										className={`text-gray-500 transition-transform group-hover:text-(--accent) group-hover:drop-shadow-[0_0_8px_var(--accent)] ${collapsedLetters.has("custom") ? "" : "rotate-90"}`}
									/>
									<span className="text-lg font-bold text-(--accent) group-hover:[text-shadow:0_0_8px_var(--accent)]">
										{t("failover.section_custom")}
									</span>
									<div className="flex-1 h-px bg-gray-700/50" />
									<span className="text-xs text-gray-500">
										{t("failover.group_count", {
											count: customGroups.length,
										})}
									</span>
								</button>
								<div
									className="grid transition-[grid-template-rows] duration-200 ease-in-out"
									style={{
										gridTemplateRows: collapsedLetters.has("custom")
											? "0fr"
											: "1fr",
									}}
								>
									<div className="overflow-hidden">
										<div className="grid grid-cols-1 lg:grid-cols-2 xl:grid-cols-3 gap-4">
											{customGroups.map((group) => (
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
													onEdit={() => setEditGroup(group)}
												/>
											))}
										</div>
									</div>
								</div>
							</section>
						)}

						{/* Auto groups grouped by first letter */}
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
										{t("failover.group_count", {
											count: letterGroups[letter].length,
										})}
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
					{(sortedLetters.length > 3 || customGroups.length > 0) && (
						<nav
							aria-label={t("failoverGroups.alphabetSidebar")}
							className="hidden xl:flex flex-col items-center gap-1 pt-2 sticky top-4 self-start"
						>
							{customGroups.length > 0 && (
								<button
									type="button"
									onClick={() =>
										document
											.getElementById("failover-section-custom")
											?.scrollIntoView({ behavior: "smooth", block: "start" })
									}
									className="text-xs font-medium text-(--accent) hover:[text-shadow:0_0_8px_var(--accent)] transition-all cursor-pointer px-1.5 py-0.5 rounded"
									aria-label={t("failover.nav_custom")}
								>
									★
								</button>
							)}
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

			{editGroup && candidates && (
				<CreateGroupModal
					candidates={candidates}
					group={editGroup}
					onClose={() => setEditGroup(null)}
					onUpdated={() => setEditGroup(null)}
				/>
			)}

			{deleteGroup && (
				<DeleteConfirmModal
					entityName={`hotel/${deleteGroup.display_model}`}
					entityType={t("failover.delete_confirm_type")}
					isPending={deleteMutation.isPending}
					onConfirm={confirmDelete}
					onCancel={() => setDeleteGroup(null)}
				/>
			)}

			{bulkDeleteIds && (
				<DeleteConfirmModal
					entityName={t("failover.delete_confirm_bulk_title", {
						count: bulkDeleteIds.size,
					})}
					entityType="failover groups"
					isPending={isBulkDeleting}
					onConfirm={confirmBulkDelete}
					onCancel={() => setBulkDeleteIds(null)}
				/>
			)}
		</div>
	);
}
