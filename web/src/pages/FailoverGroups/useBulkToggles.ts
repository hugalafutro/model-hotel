import { useState } from "react";
import { useTranslation } from "react-i18next";
import { api } from "../../api/client";
import type { FailoverGroup } from "../../api/types";
import { useToast } from "../../context/ToastContext";
import { entryEnabledMapOf, entryToggleUpdate } from "./groupDerivations";

/**
 * The three many-groups-at-once toggles: the selected groups, every group on
 * the filtered provider, and the provider modal's switch. Each writes one
 * update per group through `entryToggleUpdate` (which carries the
 * 2-routable-member rule) and re-reads the groups whether the batch
 * succeeded or not.
 */
export function useBulkToggles({
	allGroups,
	providerFilter,
	selectedGroupIds,
	clearSelection,
	refreshGroups,
}: {
	allGroups: FailoverGroup[] | undefined;
	providerFilter: string;
	selectedGroupIds: Set<string>;
	clearSelection: () => void;
	refreshGroups: () => void;
}) {
	const { toast } = useToast();
	const { t } = useTranslation();
	const [isProviderToggling, setIsProviderToggling] = useState(false);

	const handleBulkModelToggle = async (enabled: boolean) => {
		if (!allGroups) return;
		const targets = allGroups.filter((g) => selectedGroupIds.has(g.id));
		if (targets.length === 0) return;

		const promises = targets.map((group) =>
			api.failoverGroups.update(
				group.id,
				entryToggleUpdate(
					group,
					entryEnabledMapOf(group, () => enabled),
				),
			),
		);

		try {
			await Promise.all(promises);
			refreshGroups();
			clearSelection();
			toast(
				t("failover.toast_bulk_toggle_success", {
					action: enabled ? t("common.enabled") : t("common.disabled"),
					count: targets.length,
				}),
				"success",
			);
		} catch {
			refreshGroups();
			toast(t("failover.toast_bulk_toggle_failed"), "error");
		}
	};

	/**
	 * Switch every entry on a matching provider across the groups that have
	 * one. The bulk bar matches a name fragment from the provider filter, the
	 * provider modal an exact name.
	 */
	const toggleProviderEntries = async (
		matches: (providerName: string) => boolean,
		providerLabel: string,
		enabled: boolean,
		opts: { toastWhenNoGroups?: boolean; trackBusy?: boolean } = {},
	) => {
		if (!allGroups) return;
		const affectedGroups = allGroups.filter((g) =>
			g.entries.some((e) => matches(e.provider_name)),
		);
		if (affectedGroups.length === 0) {
			if (opts.toastWhenNoGroups) {
				toast(
					t("failover.toast_provider_toggle_no_groups", {
						provider: providerLabel,
					}),
					"info",
				);
			}
			return;
		}

		if (opts.trackBusy) setIsProviderToggling(true);
		try {
			await Promise.all(
				affectedGroups.map((group) =>
					api.failoverGroups.update(
						group.id,
						entryToggleUpdate(
							group,
							entryEnabledMapOf(group, (e) =>
								matches(e.provider_name) ? enabled : e.enabled,
							),
						),
					),
				),
			);
			toast(
				t("failover.toast_provider_toggle_success", {
					action: enabled ? t("common.enabled") : t("common.disabled"),
					provider: providerLabel,
					count: affectedGroups.length,
				}),
				"success",
			);
		} catch {
			toast(t("failover.toast_provider_toggle_failed"), "error");
		} finally {
			// Re-fetch either way; disabledProviders is derived from the result.
			refreshGroups();
			if (opts.trackBusy) setIsProviderToggling(false);
		}
	};

	const handleBulkProviderToggle = async (enabled: boolean) => {
		if (!providerFilter) return;
		const providerLower = providerFilter.toLowerCase();
		await toggleProviderEntries(
			(name) => name.toLowerCase().includes(providerLower),
			providerFilter,
			enabled,
		);
	};

	// Provider modal toggle
	const handleProviderToggle = (providerName: string, enabled: boolean) =>
		toggleProviderEntries((n) => n === providerName, providerName, enabled, {
			toastWhenNoGroups: true,
			trackBusy: true,
		});

	return {
		handleBulkModelToggle,
		handleBulkProviderToggle,
		handleProviderToggle,
		isProviderToggling,
	};
}
