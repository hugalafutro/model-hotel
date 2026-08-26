import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { api } from "../../api/client";
import type { FailoverGroup } from "../../api/types";
import { useToast } from "../../context/ToastContext";

/**
 * The page's four server mutations (sync, update, delete, circuit reset) and
 * the per-card handlers built on the update. Every one re-reads the groups
 * through `refreshGroups` on settle, since a rejected write can still have
 * landed.
 */
export function useFailoverGroupMutations(refreshGroups: () => void) {
	const { toast } = useToast();
	const { t } = useTranslation();
	const queryClient = useQueryClient();

	const sync = useMutation({
		mutationFn: () => api.failoverGroups.sync(),
		onSuccess: (data) => {
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
			if (data.disabled_groups && data.disabled_groups.length > 0) {
				for (const g of data.disabled_groups) {
					toast(
						t("failover.toast_sync_disabled", {
							group: g.display_model,
							count: g.effective_count,
						}),
						"warning",
					);
				}
			}
			if (
				(!data.deleted_groups || data.deleted_groups.length === 0) &&
				(!data.purged_entries || data.purged_entries.length === 0) &&
				(!data.disabled_groups || data.disabled_groups.length === 0)
			) {
				toast(t("failover.toast_sync_success"), "success");
			}
		},
		onError: (err: Error) => {
			toast(t("failover.toast_sync_failed", { message: err.message }), "error");
		},
		// Sync applies partially by design: it reports the groups it deleted,
		// purged and disabled, so a run that errors after any of that has still
		// changed what the list and the badge should say.
		onSettled: () => {
			refreshGroups();
		},
	});

	const update = useMutation({
		mutationFn: ({
			id,
			data,
		}: {
			id: string;
			data: Parameters<typeof api.failoverGroups.update>[1];
		}) => api.failoverGroups.update(id, data),
		onError: (err: Error) => {
			toast(
				t("failover.toast_update_failed", { message: err.message }),
				"error",
			);
		},
		// `onSettled`, matching the bulk handlers, which re-read from both their
		// try and their catch: a rejected write can still have landed.
		onSettled: () => {
			refreshGroups();
		},
	});

	const remove = useMutation({
		mutationFn: (id: string) => api.failoverGroups.delete(id),
		onSuccess: () => {
			toast(t("failover.toast_delete_success"), "success");
		},
		onError: (err: Error) => {
			toast(
				t("failover.toast_delete_failed", { message: err.message }),
				"error",
			);
		},
		// See update: a rejected DELETE can still have landed.
		onSettled: () => {
			refreshGroups();
		},
	});

	// Forces one provider's circuit closed. Available while `managed` on purpose:
	// a circuit is this instance's own runtime health, not config the fleet
	// primary owns, and a quota-pinned circuit can otherwise stay open for up to
	// 24 hours. Hidden only in read-only demo mode, where the server refuses it.
	const resetCircuit = useMutation({
		mutationFn: ({
			providerId,
		}: {
			providerId: string;
			providerName: string;
		}) => api.failoverGroups.resetCircuitBreaker(providerId),
		onSuccess: (result, { providerName }) => {
			// Prefix key: refreshes both this page's detail query and the sidebar
			// badge's aggregate one, so the cleared circuit disappears at once.
			queryClient.invalidateQueries({ queryKey: ["circuit-breaker-status"] });
			if (result.reset) {
				toast(
					t("failover.toast_cb_reset_success", { provider: providerName }),
					"success",
				);
			} else {
				// Honest no-op: the circuit had already closed on its own.
				toast(
					t("failover.toast_cb_reset_noop", { provider: providerName }),
					"info",
				);
			}
		},
		onError: (err: Error) => {
			toast(
				t("failover.toast_cb_reset_failed", { message: err.message }),
				"error",
			);
		},
	});

	const handleResetCircuit = (providerId: string, providerName: string) => {
		resetCircuit.mutate({ providerId, providerName });
	};

	const handleToggleGroup = (group: FailoverGroup, enabled: boolean) => {
		update.mutate({
			id: group.id,
			data: { group_enabled: enabled },
		});
	};

	const handleToggleEntry = (
		group: FailoverGroup,
		uuid: string,
		enabled: boolean,
	) => {
		// Count *effective* members (the toggle plus a live model and provider),
		// matching the card's "active" tally. Counting the raw enabled flag let an
		// already-N/A member (enabled flag true, model/provider dead) pad the total,
		// so the user could toggle the last live members off and reach a 0/X group.
		// A group needs 2+ routable members. Only block when this toggle actually
		// removes an active member: switching off an N/A member (already not active)
		// can't drop the count, so it must stay allowed.
		const toggled = group.entries.find((e) => e.model_uuid === uuid);
		const togglingOffActiveMember =
			!enabled &&
			!!toggled?.enabled &&
			toggled.model_enabled &&
			toggled.provider_enabled;
		const activeCount = group.entries.filter(
			(e) => e.enabled && e.model_enabled && e.provider_enabled,
		).length;
		if (togglingOffActiveMember && activeCount <= 2) {
			toast(t("failover.toast_entry_min_two"), "error");
			return;
		}
		const entryEnabledMap: Record<string, boolean> = {};
		group.entries.forEach((e) => {
			entryEnabledMap[e.model_uuid] = e.enabled;
		});
		entryEnabledMap[uuid] = enabled;
		update.mutate({
			id: group.id,
			data: { entry_enabled: entryEnabledMap },
		});
	};

	const handleReorder = (group: FailoverGroup, newOrder: string[]) => {
		update.mutate({
			id: group.id,
			data: { priority_order: newOrder },
		});
	};

	return {
		sync,
		update,
		remove,
		resetCircuit,
		handleResetCircuit,
		handleToggleGroup,
		handleToggleEntry,
		handleReorder,
	};
}
