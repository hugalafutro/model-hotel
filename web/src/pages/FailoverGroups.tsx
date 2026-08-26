import { useQuery, useQueryClient } from "@tanstack/react-query";
import { type ComponentProps, useCallback, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { ShieldOff, Shuffle } from "@/lib/icons";
import { api } from "../api/client";
import type { CircuitBreakerProviderStatus, FailoverGroup } from "../api/types";
import { DeleteConfirmModal } from "../components/DeleteConfirmModal";
import { ManagedBanner } from "../components/ManagedBanner";
import { PageHeader } from "../components/PageHeader";
import { Spinner } from "../components/Spinner";
import { useToast } from "../context/ToastContext";
import { useManaged } from "../hooks/useManaged";
import { useReadOnly } from "../hooks/useReadOnly";
import { useRefreshDiscoveryBadge } from "../hooks/useRefreshDiscoveryBadge";
import { countLabel, formatTimestamp } from "../utils/format";
import { AlphabetSidebar } from "./FailoverGroups/AlphabetSidebar";
import { CreateGroupModal } from "./FailoverGroups/CreateGroupModal";
import { EmptyGroups } from "./FailoverGroups/EmptyGroups";
import { FailoverGroupCard } from "./FailoverGroups/FailoverGroupCard";
import { FiltersBar } from "./FailoverGroups/FiltersBar";
import { GroupSection } from "./FailoverGroups/GroupSection";
import {
	deriveDisabledProviders,
	filterGroups,
	type GroupFilters,
	groupsMatchingProvider,
	providerNamesOf,
	splitByOrigin,
} from "./FailoverGroups/groupDerivations";
import { ProviderBulkBar } from "./FailoverGroups/ProviderBulkBar";
import { ProviderDisableModal } from "./FailoverGroups/ProviderDisableModal";
import { useBulkToggles } from "./FailoverGroups/useBulkToggles";
import { useFailoverGroupMutations } from "./FailoverGroups/useFailoverGroupMutations";

const NO_FILTERS: GroupFilters = {
	searchQuery: "",
	providerFilter: "",
	enabledFilter: "",
	originFilter: "",
};

export function FailoverGroups() {
	const { toast } = useToast();
	const { t } = useTranslation();
	const readOnly = useReadOnly();
	const managed = useManaged();
	const queryClient = useQueryClient();
	const refreshBadge = useRefreshDiscoveryBadge();

	// Group state and the Models nav badge move together: an auto-disabled group
	// IS a counted claim (see useRefreshDiscoveryBadge), so re-enabling or
	// deleting one changes claim_count. Paired here rather than at each site so
	// the next path that re-reads groups cannot forget the badge.
	const refreshGroups = useCallback(() => {
		queryClient.invalidateQueries({ queryKey: ["failover-groups"] });
		refreshBadge();
	}, [queryClient, refreshBadge]);

	const [showCreateModal, setShowCreateModal] = useState(false);
	const [editGroup, setEditGroup] = useState<FailoverGroup | null>(null);
	const [deleteGroup, setDeleteGroup] = useState<FailoverGroup | null>(null);
	const [bulkDeleteIds, setBulkDeleteIds] = useState<Set<string> | null>(null);
	const [isBulkDeleting, setIsBulkDeleting] = useState(false);
	const [filters, setFilters] = useState<GroupFilters>(NO_FILTERS);
	const [selectedGroupIds, setSelectedGroupIds] = useState<Set<string>>(
		new Set(),
	);
	const [collapsedLetters, setCollapsedLetters] = useState<Set<string>>(
		new Set(),
	);
	const [showProviderModal, setShowProviderModal] = useState(false);

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

	const { data: cbStatus } = useQuery({
		queryKey: ["circuit-breaker-status", "detail"],
		queryFn: () => api.failoverGroups.circuitBreakerStatus(true),
		refetchInterval: 15_000,
	});

	// Build a map of provider_id -> provider CB status for quick lookup
	const cbProviderMap = new Map<string, CircuitBreakerProviderStatus>();
	if (cbStatus?.providers) {
		for (const p of cbStatus.providers) {
			if (p.state !== "closed") {
				cbProviderMap.set(p.provider_id, p);
			}
		}
	}

	const allGroups = listData?.groups;
	const disabledProviders = useMemo(
		() => deriveDisabledProviders(allGroups),
		[allGroups],
	);
	const providerNames = providerNamesOf(allGroups);
	const groups = allGroups ? filterGroups(allGroups, filters) : undefined;
	const lastSyncedAt = listData?.last_synced_at;

	const totalEnabled = allGroups?.filter((g) => g.group_enabled).length ?? 0;
	const totalDisabled = (allGroups?.length ?? 0) - totalEnabled;
	const allSameState = totalEnabled === 0 || totalDisabled === 0;

	const { customGroups, letterGroups, sortedLetters } = splitByOrigin(
		groups ?? [],
	);

	const toggleGroupSelect = (groupId: string, checked: boolean) => {
		setSelectedGroupIds((prev) => {
			const next = new Set(prev);
			if (checked) next.add(groupId);
			else next.delete(groupId);
			return next;
		});
	};

	const {
		sync: syncMutation,
		remove: deleteMutation,
		resetCircuit: resetCircuitMutation,
		handleResetCircuit,
		handleToggleGroup,
		handleToggleEntry,
		handleReorder,
	} = useFailoverGroupMutations(refreshGroups);

	const {
		handleBulkModelToggle,
		handleBulkProviderToggle,
		handleProviderToggle,
		isProviderToggling,
	} = useBulkToggles({
		allGroups,
		providerFilter: filters.providerFilter,
		selectedGroupIds,
		clearSelection: () => setSelectedGroupIds(new Set()),
		refreshGroups,
	});

	const { data: providers } = useQuery({
		queryKey: ["providers"],
		queryFn: () => api.providers.list(),
	});

	const { data: candidates } = useQuery({
		queryKey: ["failover-candidates"],
		queryFn: () => api.failoverGroups.candidates(),
	});

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
		refreshGroups();
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

	// Everything a card needs that does not depend on which group it shows.
	// The custom section adds onEdit: auto groups are not editable.
	const cardProps = (
		group: FailoverGroup,
	): ComponentProps<typeof FailoverGroupCard> => ({
		group,
		selected: selectedGroupIds.has(group.id),
		onToggleSelect: (checked) => toggleGroupSelect(group.id, checked),
		onToggleGroup: (enabled) => handleToggleGroup(group, enabled),
		onToggleEntry: (uuid, enabled) => handleToggleEntry(group, uuid, enabled),
		onReorder: (newOrder) => handleReorder(group, newOrder),
		onDelete: () => setDeleteGroup(group),
		managed,
		cbProviderMap,
		onResetCircuit: readOnly ? undefined : handleResetCircuit,
		resetPendingProviderId: resetCircuitMutation.isPending
			? resetCircuitMutation.variables?.providerId
			: undefined,
	});

	if (isLoading) {
		return (
			<div className="flex items-center justify-center h-64">
				<div className="text-gray-500">{t("common.loadingDots")}</div>
			</div>
		);
	}

	return (
		<div className="space-y-6 pb-6" style={{ scrollBehavior: "smooth" }}>
			<PageHeader
				icon={Shuffle}
				title={countLabel(allGroups?.length, "failoverGroups.countLabel")}
				description={
					<>
						{t("failover.page_description_lead")}{" "}
						<code className="text-(--accent) whitespace-nowrap">
							{t("failover.page_description_code")}
						</code>
					</>
				}
				badge={
					!allSameState && groups && groups.length > 0 ? (
						<span className="inline-flex items-center gap-2 px-2.5 py-1 leading-[1.6] text-xs font-medium ui-badge ui-badge-neutral">
							<span className="text-green-400">
								<span className="badge-text">
									{t("failover.badge_enabled", { count: totalEnabled })}
								</span>
							</span>
							<span className="text-gray-600">/</span>
							<span className="text-red-400">
								<span className="badge-text">
									{t("failover.badge_disabled", { count: totalDisabled })}
								</span>
							</span>
						</span>
					) : undefined
				}
				actions={
					<>
						{lastSyncedAt && (
							<span className="text-xs text-(--text-muted)">
								<span className="whitespace-nowrap">
									{t("failover.last_sync_label")}
								</span>{" "}
								<span className="whitespace-nowrap">
									{formatTimestamp(lastSyncedAt)}
								</span>
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
						{!readOnly && !managed && (
							<>
								<button
									type="button"
									onClick={() => setShowCreateModal(true)}
									className="ui-btn ui-btn-primary"
								>
									{t("failover.btn_new_group")}
								</button>
								<button
									type="button"
									onClick={() => setShowProviderModal(true)}
									className="ui-btn ui-btn-secondary"
								>
									<ShieldOff className="h-4 w-4" />
									{t("failover.btn_manage_providers")}
								</button>
							</>
						)}
					</>
				}
			/>
			<p className="text-(--text-muted) text-xs flex items-center gap-1.5 -mt-4">
				<span className="shrink-0" aria-hidden="true">
					⠿
				</span>
				{t("failover.hint_drag")}
			</p>

			<ManagedBanner />

			<FiltersBar
				filters={filters}
				onFilterChange={(patch) => setFilters((f) => ({ ...f, ...patch }))}
				providerNames={providerNames}
				managed={managed}
				selectedCount={selectedGroupIds.size}
				onToggleSelectAll={() => {
					if (selectedGroupIds.size > 0) {
						setSelectedGroupIds(new Set());
					} else if (groups) {
						setSelectedGroupIds(new Set(groups.map((g) => g.id)));
					}
				}}
				onBulkToggle={handleBulkModelToggle}
				onBulkDelete={() => setBulkDeleteIds(new Set(selectedGroupIds))}
			/>

			{filters.providerFilter && allGroups && (
				<ProviderBulkBar
					providerFilter={filters.providerFilter}
					count={
						groupsMatchingProvider(allGroups, filters.providerFilter).length
					}
					onToggle={handleBulkProviderToggle}
				/>
			)}

			{groups && groups.length === 0 ? (
				<EmptyGroups
					filters={filters}
					onCreate={() => setShowCreateModal(true)}
					onClearFilters={() => setFilters(NO_FILTERS)}
					onSync={() => syncMutation.mutate()}
				/>
			) : (
				<div className="relative flex gap-4">
					<div className="flex-1 space-y-6">
						{customGroups.length > 0 && (
							<GroupSection
								id="failover-section-custom"
								title={t("failover.section_custom")}
								count={customGroups.length}
								collapsed={collapsedLetters.has("custom")}
								onToggle={() => toggleLetterCollapse("custom")}
							>
								{customGroups.map((group) => (
									<FailoverGroupCard
										key={group.id}
										{...cardProps(group)}
										onEdit={() => setEditGroup(group)}
									/>
								))}
							</GroupSection>
						)}
						{sortedLetters.map((letter) => (
							<GroupSection
								key={letter}
								id={`failover-section-${letter}`}
								title={letter}
								count={letterGroups[letter].length}
								collapsed={collapsedLetters.has(letter)}
								onToggle={() => toggleLetterCollapse(letter)}
							>
								{letterGroups[letter].map((group) => (
									<FailoverGroupCard key={group.id} {...cardProps(group)} />
								))}
							</GroupSection>
						))}
					</div>
					<AlphabetSidebar
						letters={sortedLetters}
						hasCustom={customGroups.length > 0}
					/>
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
					entityType={t("failover.delete_confirm_type_plural")}
					isPending={isBulkDeleting}
					onConfirm={confirmBulkDelete}
					onCancel={() => setBulkDeleteIds(null)}
				/>
			)}

			{showProviderModal && (
				<ProviderDisableModal
					open={showProviderModal}
					onClose={() => setShowProviderModal(false)}
					providers={(providers ?? [])
						.filter((p) => providerNames.includes(p.name))
						.map((p) => ({
							id: p.id,
							name: p.name,
						}))}
					disabledProviders={disabledProviders}
					onToggleProvider={handleProviderToggle}
					isProcessing={isProviderToggling}
				/>
			)}
		</div>
	);
}
