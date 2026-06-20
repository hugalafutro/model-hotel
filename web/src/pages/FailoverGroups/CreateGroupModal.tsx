import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { api } from "../../api/client";
import type { CandidateModel, FailoverGroup } from "../../api/types";
import { ConfirmDialog } from "../../components/ConfirmDialog";
import { Modal } from "../../components/Modal";
import type { ModelItem } from "../../components/ModelPicker";
import { ModelPicker } from "../../components/ModelPicker";
import { useToast } from "../../context/ToastContext";
import { isNaEntry, naReasonKey } from "../../utils/failoverEntry";
import { proxyModelID } from "../../utils/model";
import {
	type DiscoverySummaryEntry,
	DiscoverySummaryModal,
} from "../Providers/DiscoverySummaryModal";

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
	const { t } = useTranslation();
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
					// Entries absent from candidates have a disabled model or
					// provider; flag them N/A so the picker shows a badge with the
					// reason, matching the card's N/A badge.
					const reasonKey = naReasonKey(e);
					items.push({
						provider_name: e.provider_name,
						model_id: e.model_id,
						display_name: e.display_name || undefined,
						unavailable: true,
						unavailableReason: reasonKey ? t(reasonKey) : undefined,
					});
				}
			}
		}
		return items;
	}, [candidates, group, t]);

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
			toast(t("failover.toast_created"), "success");
			onCreated?.();
		},
		onError: (err: Error) => {
			if (
				err.message.includes("409") &&
				err.message.includes("already exists")
			) {
				toast(
					t("failover.toast_create_collision", {
						model:
							displayModel || t("failover.toast_create_collision_this_model"),
					}),
					"error",
				);
			} else {
				toast(
					t("failover.toast_create_failed", { message: err.message }),
					"error",
				);
			}
		},
	});

	const updateMutation = useMutation({
		mutationFn: (data: {
			id: string;
			body: Parameters<typeof api.failoverGroups.update>[1];
		}) => api.failoverGroups.update(data.id, data.body),
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: ["failover-groups"] });
			toast(t("failover.toast_updated"), "success");
			onUpdated?.();
		},
		onError: (err: Error) => {
			if (
				err.message.includes("409") &&
				err.message.includes("already exists")
			) {
				toast(t("failover.toast_update_collision"), "error");
			} else {
				toast(
					t("failover.toast_update_group_failed", { message: err.message }),
					"error",
				);
			}
		},
	});

	// N/A members of the group being edited (model or provider disabled).
	const naEntries = useMemo(
		() => (group ? group.entries.filter(isNaEntry) : []),
		[group],
	);
	// Only model-disabled members on a live provider can be re-probed; a
	// provider-off member needs the provider re-enabled first, so Retry skips it.
	const retryableEntries = useMemo(
		() => naEntries.filter((e) => e.provider_enabled && !e.model_enabled),
		[naEntries],
	);

	const [retrying, setRetrying] = useState(false);
	// Set after a Retry N/A run to feed the reused discovery diff modal.
	const [retryResult, setRetryResult] = useState<
		DiscoverySummaryEntry[] | null
	>(null);
	const [confirmDeleteGroup, setConfirmDeleteGroup] = useState(false);

	const deleteGroupMutation = useMutation({
		mutationFn: (id: string) => api.failoverGroups.delete(id),
		onSuccess: () => {
			queryClient.invalidateQueries({ queryKey: ["failover-groups"] });
			toast(t("failover.toast_delete_success"), "success");
			onUpdated?.();
		},
		onError: (err: Error) => {
			toast(
				t("failover.toast_delete_failed", { message: err.message }),
				"error",
			);
		},
	});

	// Re-check every retryable N/A member and re-enable the ones that answer, then
	// surface the outcome through the discovery diff modal (healed members show as
	// re-enabled, the rest stay listed as disabled). Members still N/A are left
	// untouched so the group is unchanged when nothing recovered.
	const handleRetryNa = async () => {
		if (!group || retryableEntries.length === 0 || retrying) return;
		setRetrying(true);
		const byProvider = new Map<
			string,
			{ healed: string[]; failed: string[] }
		>();
		for (const e of retryableEntries) {
			const bucket = byProvider.get(e.provider_name) ?? {
				healed: [],
				failed: [],
			};
			// The diff modal renders ModelChange.model_id as a mono chip, so use the
			// raw model id (matching the real discovery flow), not the display name.
			const id = e.model_id;
			let probedOk: boolean;
			try {
				probedOk = (await api.models.test(e.model_uuid, true)).success;
			} catch {
				probedOk = false;
			}
			if (!probedOk) {
				// Still didn't answer: genuinely unavailable.
				bucket.failed.push(id);
				byProvider.set(e.provider_name, bucket);
				continue;
			}
			// It answered, so it is healthy. Re-enable it; a failed write is a save
			// error, not a "still unavailable" signal, so keep it among the healed
			// and surface the write failure separately.
			bucket.healed.push(id);
			try {
				await api.models.update(e.model_uuid, { enabled: true });
			} catch (err) {
				toast(
					t("failover.toast_update_failed", {
						message: (err as Error).message,
					}),
					"error",
				);
			}
			byProvider.set(e.provider_name, bucket);
		}
		setRetrying(false);
		queryClient.invalidateQueries({ queryKey: ["failover-groups"] });
		queryClient.invalidateQueries({ queryKey: ["failover-candidates"] });
		queryClient.invalidateQueries({ queryKey: ["models"] });
		setRetryResult(
			[...byProvider].map(([providerName, b]) => ({
				providerName,
				diff: {
					reenabled: b.healed.map((id) => ({
						model_id: id,
						reason: "reappeared",
					})),
					disabled: b.failed.map((id) => ({
						model_id: id,
						reason: "not_listed",
					})),
				},
			})),
		);
	};

	// Drop the N/A members from the group. A failover group needs 2+ members, so
	// if removing them would leave fewer than two, the whole group goes instead:
	// confirm that first, since it is destructive.
	const handleDeleteNa = () => {
		if (!group) return;
		const remaining = group.entries.filter((e) => !isNaEntry(e));
		if (remaining.length < 2) {
			setConfirmDeleteGroup(true);
			return;
		}
		updateMutation.mutate({
			id: group.id,
			body: { priority_order: remaining.map((e) => e.model_uuid) },
		});
	};

	const handleSubmit = (e: React.FormEvent) => {
		e.preventDefault();

		const entryUuids = selectedProxyIDs
			.map((id) => proxyToUuid.get(id))
			.filter((v): v is string => v !== undefined);

		if (isEdit) {
			if (entryUuids.length < 2) {
				toast(t("failover.toast_min_entries"), "error");
				return;
			}
			updateMutation.mutate({
				id: group.id,
				body: {
					display_model: displayModel.trim() || undefined,
					display_name: displayName.trim(),
					description: description.trim(),
					priority_order: entryUuids,
				},
			});
		} else {
			if (!displayModel.trim()) {
				toast(t("failover.toast_display_model_required"), "error");
				return;
			}
			if (entryUuids.length < 2) {
				toast(t("failover.toast_min_entries"), "error");
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
			title={
				isEdit
					? t("failoverGroups.create.editTitle")
					: t("failoverGroups.create.createTitle")
			}
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
						{t("failoverGroups.create.displayModelName")}
					</label>
					<input
						id="display-model"
						type="text"
						required={!isEdit}
						maxLength={128}
						value={displayModel}
						onChange={(e) => setDisplayModel(e.target.value)}
						className="ui-input"
						placeholder={t("failoverGroups.create.displayModelNamePlaceholder")}
					/>
					<p className="text-gray-500 text-xs mt-1">
						{t("failoverGroups.create.displayModelNameHelper", {
							modelName: displayModel || "model-name",
						})}
					</p>
				</div>

				<div>
					<label
						htmlFor="display-name"
						className="block text-sm font-medium text-gray-300 mb-1"
					>
						{t("failoverGroups.create.displayNameOptional")}
					</label>
					<input
						id="display-name"
						type="text"
						maxLength={128}
						value={displayName}
						onChange={(e) => setDisplayName(e.target.value)}
						className="ui-input"
						placeholder={t("failoverGroups.create.displayNamePlaceholder")}
					/>
				</div>

				<div>
					<label
						htmlFor="group-description"
						className="block text-sm font-medium text-gray-300 mb-1"
					>
						{t("failoverGroups.create.descriptionOptional")}
					</label>
					<input
						id="group-description"
						type="text"
						maxLength={256}
						value={description}
						onChange={(e) => setDescription(e.target.value)}
						className="ui-input"
						placeholder={t("failoverGroups.create.descriptionPlaceholder")}
					/>
				</div>

				<div>
					<ModelPicker
						id={`failover-group-entries-${isEdit ? "edit" : "create"}`}
						models={modelItems}
						selected={selectedProxyIDs}
						onChange={setSelectedProxyIDs}
						multi={true}
						label={t("failoverGroups.create.modelEntries")}
						align="left"
						sortProvidersAlpha
					/>
					<p className="text-gray-500 text-xs mt-1">
						{t("failoverGroups.create.selectedCount", {
							count: selectedProxyIDs.length,
						})}
					</p>
				</div>

				<div className="flex justify-between gap-3 pt-4">
					<div className="flex gap-2">
						{isEdit && naEntries.length > 0 && (
							<>
								<button
									type="button"
									onClick={handleRetryNa}
									disabled={retrying || retryableEntries.length === 0}
									className="ui-btn ui-btn-secondary"
									title={
										retryableEntries.length === 0
											? t("failoverGroups.entry.retryNaProviderOff")
											: t("failoverGroups.entry.retryNaHelp")
									}
								>
									{retrying
										? t("failoverGroups.entry.retryNaRunning")
										: t("failoverGroups.entry.retryNa")}
								</button>
								<button
									type="button"
									onClick={handleDeleteNa}
									disabled={isPending}
									className="ui-btn ui-btn-secondary text-(--text-muted) hover:text-red-400"
									title={t("failoverGroups.entry.deleteNaHelp")}
								>
									{t("failoverGroups.entry.deleteNa")}
								</button>
							</>
						)}
					</div>
					<div className="flex gap-3">
						<button
							type="button"
							onClick={onClose}
							className="ui-btn ui-btn-secondary"
						>
							{t("common.cancel")}
						</button>
						<button
							type="submit"
							disabled={isPending}
							className="ui-btn ui-btn-primary"
						>
							{isPending
								? isEdit
									? t("failoverGroups.create.saving")
									: t("failoverGroups.create.creating")
								: isEdit
									? t("common.saveChanges")
									: t("failoverGroups.create.createGroup")}
						</button>
					</div>
				</div>
			</form>
			{retryResult && (
				<DiscoverySummaryModal
					results={retryResult}
					onClose={() => setRetryResult(null)}
				/>
			)}
			{confirmDeleteGroup && group && (
				<ConfirmDialog
					title={t("failoverGroups.entry.deleteNaConfirmTitle")}
					message={t("failoverGroups.entry.deleteNaConfirmMessage")}
					fields={[group.display_model]}
					confirmLabel={t("failoverGroups.entry.deleteNaConfirmLabel")}
					onConfirm={() => {
						setConfirmDeleteGroup(false);
						deleteGroupMutation.mutate(group.id);
					}}
					onCancel={() => setConfirmDeleteGroup(false)}
				/>
			)}
		</Modal>
	);
}
